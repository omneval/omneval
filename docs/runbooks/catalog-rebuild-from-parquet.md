# Rebuild Catalog from Parquet

**Status:** approved procedure  
**Applies to:** all `CatalogDriverLocal` (DuckDB-file catalog) deployments  
**Related:** ADR-0006 — DuckDB-native catalog as the production default; ADR-0007 — Catalog backup is the operator's responsibility; CONTEXT.md — Disaster Recovery entry

## Overview

This procedure rebuilds an Omneval Catalog from the Lake's existing Parquet files. The Catalog's DuckLake snapshot and time-travel history is discarded because that history lives only in the Catalog file and is not surfaced in the product today.

### This is the last resort

**If you have a VolumeSnapshot or equivalent PVC backup available, restore that instead.** This procedure exists for the case where the Catalog file is lost or corrupted and no backup exists.

When the PVC survives (the normal case for a StatefulSet's `volumeClaimTemplate` on any network-attached storage class), Kubernetes reattaches the same PVC to the rescheduled pod and the Quack Server reopens the existing Catalog file — DuckDB replays its own WAL for crash consistency. A plain pod restart (eviction, OOM, crash) with the PVC intact needs no recovery action at all.

This procedure applies when the Catalog file itself (the DuckDB file on the PVC at `/data/lake/catalog.ducklake`) is missing, corrupted, or irrecoverable.

## Prerequisites

- `kubectl` access to the target cluster with permissions to manage Deployments and StatefulSets in the Omneval release namespace.
- Helm 3 installed (used for re-deploying the Quack Server).
- Access to the S3 bucket where the Lake's Parquet files are stored.
- The release name (the Helm release prefix for all resources, typically `omneval` or `omneval-<environment>`).
- The Helm values file or values overrides used for the production deployment.
- The Quack Server auth token (the value of `quack.client.token` / `OMNEVAL_QUACK_CLIENT_TOKEN`).

## Key configuration references

| Setting | Helm value | Environment variable | Description |
|---------|-----------|---------------------|-------------|
| Catalog driver | `quack.server.catalogDriver` | `OMNEVAL_QUACK_SERVER_CATALOG_DRIVER` | Must be `"duckdb"` |
| Catalog DSN | `quack.server.catalogDSN` | `OMNEVAL_QUACK_SERVER_CATALOG_DSN` | Empty — defaults to `lake/catalog.ducklake` on the PVC |
| Lake data path | `quack.server.dataPath` / `quack.client.dataPath` | `OMNEVAL_QUACK_SERVER_DATA_PATH` / `OMNEVAL_QUACK_CLIENT_DATA_PATH` | `s3://<bucket>/lake` for production; `/data/lake` for local PVC |
| Table Maintenance interval | `quack.server.maintenanceInterval` | `OMNEVAL_QUACK_SERVER_MAINTENANCE_INTERVAL` | Table Maintenance cadence (default `"5m"`) |

The Quack Server statefulset mounts the PVC at `/data`; the default catalog file path when `catalogDSN` is empty and `catalogDriver` is `duckdb` is `lake/catalog.ducklake` relative to the data path (i.e. `/data/lake/catalog.ducklake` on the PVC).

## Phase 1 — Pre-flight: confirm the Catalog file is unrecoverable

Before taking any action, verify that the Catalog file is truly unrecoverable:

```bash
# 1. Check whether the Quack Server PVC still exists
kubectl get pvc -l app.kubernetes.io/component=quack-server -n <namespace>

# 2. Check whether the catalog file is present on the PVC
kubectl exec -it <release>-quack-server-0 -n <namespace> -- \
  ls -la /data/lake/catalog.ducklake

# 3. Check whether the current Quack Server is healthy and serving queries
kubectl exec -it <release>-query-0 -n <namespace> -- \
  curl -s http://localhost:8002/api/v1/spans/query \
  -H "Authorization: Bearer <admin-token>" | jq '.total'
```

**If the catalog file exists and queries return data, the Catalog is not lost.** A plain pod restart needs no recovery — scale the Quack Server pod down and back up, or wait for Kubernetes to reschedule it. The PVC survives pod eviction on a StatefulSet with network-attached storage.

Only proceed to Phase 2 if:

- The PVC itself is lost (no PVC found, or the PVC is `Pending`), **or**
- The catalog file is missing/corrupted and a VolumeSnapshot restore is not available, **or**
- The Catalog is unrecoverable through any other means (restored from backup, etc.).

## Phase 2 — Stop Writer pods

No Writer pods may be committing to the Lake while the Catalog is being rebuilt. All in-flight batches must be drained.

```bash
# Scale all Writer pods to zero
kubectl scale deployment <release>-writer -n <namespace> --replicas=0

# Wait for all Writer pods to terminate
kubectl rollout status deployment/<release>-writer -n <namespace> --timeout=300s
```

**Verify:**

```bash
kubectl get pods -n <namespace> -l app.kubernetes.io/component=writer -w
# Confirm: no writer pods remain running (terminated or pending only)
```

**Expected downtime window begins.** From this point forward, no new spans or scores can be committed to the Lake. The window lasts until Phase 6 completes.

## Phase 3 — Deploy Quack Server with `catalogDriver: duckdb`

Deploy a fresh Quack Server that attaches to an empty (or newly created) Catalog file. This server uses the Quack protocol — every Quack client (Writer, Query API, Eval) reaches the Lake exclusively through this gateway (ADR-0005).

### Helm values update

Update the Helm values override to ensure `catalogDriver` is `duckdb`:

```yaml
quack:
  server:
    catalogDriver: "duckdb"
    # catalogDSN is empty — defaults to lake/catalog.ducklake on the PVC
    catalogDSN: ""
    # dataPath is empty — derived from storage.bucket → s3://<bucket>/lake
    dataPath: ""
```

### Deploy

```bash
# Dry-run first
helm upgrade <release> omneval/helm -n <namespace> \
  --values=<your-values-file> --dry-run --debug

# Apply the upgrade
helm upgrade <release> omneval/helm -n <namespace> \
  --values=<your-values-file>

# Wait for the Quack Server to become ready
kubectl rollout status statefulset/<release>-quack-server -n <namespace> \
  --timeout=300s
```

### Verify the catalog driver is active

```bash
kubectl exec -it <release>-quack-server-0 -n <namespace> -- \
  cat /etc/omneval/omneval.yaml | grep catalog_driver
# Expected output: catalog_driver: duckdb
```

The PVC is re-provisioned automatically when the StatefulSet is recreated (the PVC claim is part of the Helm template). If the PVC already existed, the catalog file will persist at its prior path (`/data/lake/catalog.ducklake`) — however, if the Catalog was lost, this file may be missing or corrupted. In that case, the Quack Server creates a fresh Catalog file on first use by a Quack client.

## Phase 4 — Re-register existing Parquet files

A freshly deployed `CatalogDriverLocal` does not yet know about the existing `spans` and `scores` data in the Lake's Parquet files. You must re-import that data into the new Catalog.

### How re-registration works

Every Quack client attach runs `internal/lake.Lake.Open`'s `ensureTables()` step, which idempotently creates `lake.spans` and `lake.scores` with the exact current schema and partitioning. **Do not hand-write `CREATE TABLE` DDL** for these tables — the schema evolves and hand-written DDL drifts from what clients actually expect.

The `ensureTables()` logic creates:

**`lake.spans`** (partitioned by `project_id`, `year(start_time)`, `month(start_time)`, `day(start_time)`):

```sql
CREATE TABLE IF NOT EXISTS lake.spans (
    span_id           VARCHAR      NOT NULL,
    trace_id          VARCHAR      NOT NULL,
    parent_id         VARCHAR,
    conversation_id   VARCHAR,
    project_id        VARCHAR      NOT NULL,
    service_name      VARCHAR,
    name              VARCHAR,
    kind              VARCHAR,
    start_time        TIMESTAMPTZ  NOT NULL,
    end_time          TIMESTAMPTZ,
    model             VARCHAR,
    input             VARCHAR,
    output            VARCHAR,
    input_tokens      BIGINT,
    output_tokens     BIGINT,
    cost_usd          DOUBLE,
    prompt_name       VARCHAR,
    prompt_version    BIGINT,
    status_code       VARCHAR,
    status_message    VARCHAR,
    attributes        VARCHAR
)
```

**`lake.scores`** (partitioned by `project_id`, `year(span_start_time)`, `month(span_start_time)`, `day(span_start_time)`):

```sql
CREATE TABLE IF NOT EXISTS lake.scores (
    score_id        VARCHAR      NOT NULL,
    span_id         VARCHAR      NOT NULL,
    trace_id        VARCHAR      NOT NULL,
    project_id      VARCHAR      NOT NULL,
    eval_name       VARCHAR,
    value           DOUBLE,
    reasoning       VARCHAR,
    judge_model     VARCHAR,
    prompt_name     VARCHAR,
    prompt_version  BIGINT,
    created_at      TIMESTAMPTZ  NOT NULL,
    span_start_time TIMESTAMPTZ  NOT NULL
)
```

### ATTACH and read Parquet data

First, open a DuckDB shell that attaches to the Quack Server through the Quack protocol and verify the data is reachable:

```bash
QUACK_ADDR="<release>-quack-server.<namespace>.svc.cluster.local:9494"
QUACK_TOKEN="<quack-token>"
DATA_PATH="<data-path>"   # s3://<bucket>/lake for production; /data/lake for local PVC

kubectl exec -it <release>-quack-server-0 -n <namespace> -- \
  duckdb -c "
    ATTACH IF NOT EXISTS 'ducklake:quack:${QUACK_ADDR}' AS lake (
      DATA_PATH '${DATA_PATH}',
      META_TOKEN '${QUACK_TOKEN}',
      META_DISABLE_SSL true
    );

    -- Verify the tables exist (ensureTables created them above)
    SHOW TABLES FROM lake;

    -- Check how many Parquet files DuckLake sees (may be empty if nothing is
    -- yet registered in the catalog metadata)
    SELECT COUNT(*) AS file_count FROM lake.spans;
    SELECT COUNT(*) AS file_count FROM lake.scores;
  "
```

The `ensureTables()` step creates empty tables with the correct schema, but does not register existing Parquet files as data sources. To make the Parquet files queryable through the Catalog, you need to register them as DuckLake data files.

### Re-register Parquet files as DuckLake data files

DuckLake stores Parquet files under the data path, partitioned by Hive-style directories:

```
s3://<bucket>/lake/           ← DATA_PATH root (production)
  project_id=<id>/
    year=<YYYY>/
      month=<M>/
        day=<D>/
          <uuid>.parquet       ← individual Parquet data files
```

The `ATTACH IF NOT EXISTS` statement in `attachSQL()` (internal/lake/lake.go) creates the DuckLake catalog attachment with the data path and auth token. Once the Catalog exists with `lake.spans` and `lake.scores` tables (created idempotently by `ensureTables()`), existing Parquet files are discoverable through the Lake's partition structure.

For most cases, the first Writer to call `Open()` after deploy will run `ensureTables()` which creates the tables, and the existing Parquet files on S3 will be picked up automatically by DuckLake's scan logic when queries are executed. However, if the Catalog was lost and the Parquet files were written by a prior version that used a different partition layout or DATA_PATH, you may need to explicitly import the data:

```bash
DATA_PATH="<data-path>"   # s3://<bucket>/lake for production; /data/lake for local PVC
QUACK_ADDR="<release>-quack-server.<namespace>.svc.cluster.local:9494"
QUACK_TOKEN="<quack-token>"

kubectl exec -it <release>-quack-server-0 -n <namespace> -- \
  duckdb -c "
    ATTACH IF NOT EXISTS 'ducklake:quack:${QUACK_ADDR}' AS lake (
      DATA_PATH '${DATA_PATH}',
      META_TOKEN '${QUACK_TOKEN}',
      META_DISABLE_SSL true
    );

    -- Insert all existing Parquet files into the spans table.
    -- DuckLake's partitioned Parquet files live under project_id=.../year=.../
    -- Directories. The read_parquet() wildcard expression covers all
    -- partitions in a bucket. The CASTs ensure types match the ensureTables()
    -- schema exactly (internal/lake/lake.go:ensureTables).
    INSERT INTO lake.spans
      (span_id, trace_id, parent_id, conversation_id, project_id,
       service_name, name, kind, start_time, end_time, model,
       input, output, input_tokens, output_tokens, cost_usd,
       prompt_name, prompt_version, status_code, status_message, attributes)
    SELECT
      span_id, trace_id, parent_id, conversation_id, project_id,
      service_name, name, kind, start_time, end_time, model,
      input, output, input_tokens, output_tokens, cost_usd,
      prompt_name, prompt_version, status_code, status_message, attributes
    FROM read_parquet(
      '${DATA_PATH}/*/year=*/month=*/day=*/*.parquet'
    );
  "
```

Repeat for the `scores` table:

```bash
kubectl exec -it <release>-quack-server-0 -n <namespace> -- \
  duckdb -c "
    ATTACH IF NOT EXISTS 'ducklake:quack:${QUACK_ADDR}' AS lake (
      DATA_PATH '${DATA_PATH}',
      META_TOKEN '${QUACK_TOKEN}',
      META_DISABLE_SSL true
    );

    -- Insert all existing Parquet files into the scores table.
    -- Scores partition by span_start_time (ADR-0002): a score sits next to its
    -- annotated span so both share the same project_id / date partition.
    INSERT INTO lake.scores
      (score_id, span_id, trace_id, project_id, eval_name,
       value, reasoning, judge_model, prompt_name, prompt_version,
       created_at, span_start_time)
    SELECT
      score_id, span_id, trace_id, project_id, eval_name,
      value, reasoning, judge_model, prompt_name, prompt_version,
      created_at, span_start_time
    FROM read_parquet(
      '${DATA_PATH}/*/year=*/month=*/day=*/*.parquet'
    );
  "
```

**Note:** The `read_parquet()` path pattern `${DATA_PATH}/*/year=*/month=*/day=*/*.parquet` covers all partition subdirectories under the data path. If your deployment uses a local PVC (`/data/lake`), the same pattern applies — the `*/` glob matches `project_id=<id>` directories.

For S3 paths, you may need to install the S3 extension in DuckDB first:

```bash
kubectl exec -it <release>-quack-server-0 -n <namespace> -- \
  duckdb -c "
    INSTALL httpfs;
    LOAD httpfs;

    ATTACH IF NOT EXISTS 'ducklake:quack:${QUACK_ADDR}' AS lake (
      DATA_PATH '${DATA_PATH}',
      META_TOKEN '${QUACK_TOKEN}',
      META_DISABLE_SSL true
    );
  "
```

### Validate counts

After re-registration, verify that the Parquet files are now queryable:

```bash
QUACK_ADDR="<release>-quack-server.<namespace>.svc.cluster.local:9494"
QUACK_TOKEN="<quack-token>"

kubectl exec -it <release>-quack-server-0 -n <namespace> -- \
  duckdb -c "
    ATTACH IF NOT EXISTS 'ducklake:quack:${QUACK_ADDR}' AS lake (
      DATA_PATH '${DATA_PATH}',
      META_TOKEN '${QUACK_TOKEN}',
      META_DISABLE_SSL true
    );

    -- Count all spans and scores visible through the rebuilt Catalog
    SELECT COUNT(*) AS span_count FROM lake.spans;
    SELECT COUNT(*) AS score_count FROM lake.scores;
  "
```

Record the span and score counts. These are the figures the post-rebuild validation must match.

## Phase 5 — Validate spans and scores are queryable

Run the same end-to-end validation checks that verify the rebuilt Catalog is fully operational:

### Check 1 — Span write (write a test span)

Write a test span through the Writer's OTLP ingest endpoint to verify the Catalog can accept writes:

```bash
# 1. Write a test span via the OTLP traces endpoint
curl -X POST http://<release>-writer.<namespace>.svc.cluster.local:8001/v1/traces \
  -H 'Content-Type: application/json' \
  -d '{
    "resourceSpans": [{
      "resource": {
        "attributes": [
          {"key": "service.name", "value": {"stringValue": "catalog-rebuild-validation"}}
        ]
      },
      "scopeSpans": [{
        "spans": [{
          "spanId": "cutover-validation-span",
          "traceId": "cutover-validation-trace",
          "name": "catalog-rebuild-validation",
          "kind": 3,
          "startTimeUnixNano": "'"$(date -u +%s)000000000"'",
          "endTimeUnixNano": "'"$(date -u +%s)000000000"'"
        }]
      }]
    }]
  }'

# 2. Verify the span appears in the Lake
kubectl exec -it <release>-query-0 -n <namespace> -- \
  curl -s http://localhost:8002/api/v1/spans/query \
  -H "Authorization: Bearer <admin-token>" | jq '.total'
# Expected: total == <pre-rebuild span count> + 1
```

**If this check fails:** the `lake.spans` table registration is missing or the Writer cannot write through the Quack client. Check that the Writer's `quack.client.token` matches the Quack Server's token and that the `spans` table exists in the new Catalog.

### Check 2 — Score write-back (write a score to an existing span)

Confirm that the Eval service can write scores to spans already in the Lake:

```bash
# 1. Find a span ID to score
SPAN_ID=$(kubectl exec -it <release>-query-0 -n <namespace> -- \
  curl -s http://localhost:8002/api/v1/spans/query \
  -H "Authorization: Bearer <admin-token>" | jq -r '.spans[0].span_id')

# 2. Write an eval score via the internal score endpoint
curl -X POST http://<release>-writer.<namespace>.svc.cluster.local:8001/internal/v1/scores \
  -H 'Content-Type: application/json' \
  -d '{
    "span_id": "'"${SPAN_ID}"'",
    "project_id": "<project-id>",
    "value": 0.95,
    "reasoning": "rebuild-validation-test"
  }'

# 3. Verify the score appears in the Lake
kubectl exec -it <release>-query-0 -n <namespace> -- \
  curl -s http://localhost:8002/api/v1/spans/${SPAN_ID} \
  -H "Authorization: Bearer <admin-token>" | jq '.scores'
# Expected: score object with value 0.95 and reasoning "rebuild-validation-test"
```

**If this check fails:** the scores table registration is missing or the Writer cannot write through the Quack client. Check that the Writer's `quack.client.token` matches the Quack Server's token and that the `scores` table exists in the new Catalog.

### Check 3 — Query read all (read back the full span set)

Confirm that the Query API can read all existing spans and scores through the rebuilt Catalog:

```bash
# 1. Read the total span count
SPAN_COUNT=$(kubectl exec -it <release>-query-0 -n <namespace> -- \
  curl -s http://localhost:8002/api/v1/spans/query \
  -H "Authorization: Bearer <admin-token>" | jq '.total')

# 2. Compare against the pre-rebuild count recorded in Phase 4
echo "Pre-rebuild span count: <pre-rebuild count>"
echo "Post-rebuild span count: ${SPAN_COUNT}"
# Must match exactly (or differ by +1 if Check 1 added a span)

# 3. Read a specific trace waterfall (end-to-end trace read)
TRACE_ID=$(kubectl exec -it <release>-query-0 -n <namespace> -- \
  curl -s http://localhost:8002/api/v1/spans/query \
  -H "Authorization: Bearer <admin-token>" | jq -r '.spans[0].trace_id')

kubectl exec -it <release>-query-0 -n <namespace> -- \
  curl -s http://localhost:8002/api/v1/traces/${TRACE_ID} \
  -H "Authorization: Bearer <admin-token>" | jq '.traces' | head -20
# Expected: a list of spans forming a valid trace waterfall

# 4. Verify score write-back from Check 2 is visible in the trace detail
echo "Score write-back visible: $(kubectl exec -it <release>-query-0 -n <namespace> -- \
  curl -s http://localhost:8002/api/v1/traces/${TRACE_ID} \
  -H "Authorization: Bearer <admin-token>" | jq '.traces[0].scores')"
```

**If this check fails:** the data path (`dataPath`) in the ATTACH statement does not point to the correct S3 prefix, or the Parquet file paths in the Catalog metadata are stale.

## Phase 6 — Resume Writer pods

Only after all three validation checks pass:

```bash
# Scale Writers back to production replica count
kubectl scale deployment <release>-writer -n <namespace> --replicas=<production-count>

# Verify Writers are healthy and committing
kubectl rollout status deployment/<release>-writer -n <namespace> --timeout=300s

# Watch Writer logs for successful Lake commits
kubectl logs -f -l app.kubernetes.io/component=writer -n <namespace> --tail=20
# Look for: "committed batch" or "inserted N spans into lake"
```

### Final verification

```bash
# 1. Verify all services are healthy
kubectl get pods -n <namespace> -l app.kubernetes.io/name=omneval -o wide
# Expected: all pods Running, Ready 1/1

# 2. Verify Table Maintenance is running (if configured)
kubectl logs <release>-quack-server-0 -n <namespace> --tail=30 | grep -i "table maintenance\|checkpoint"
# Expected: periodic checkpoint/maintenance messages

# 3. Configure your own PVC backup policy (e.g. Kubernetes VolumeSnapshot)
#    for the Quack Server's /data mount — the Helm chart no longer provides
#    S3 catalog backups. See the README's upgrade notes for details.

# 4. Verify the Helm release state
helm status <release> -n <namespace>
# Expected: STATUS: deployed, REVISION incremented from pre-rebuild

# 5. Check the catalog driver
kubectl exec -it <release>-quack-server-0 -n <namespace> -- \
  grep catalog_driver /etc/omneval/omneval.yaml
# Expected: catalog_driver: duckdb
```

## Post-rebuild

1. **Monitor Writer logs** for at least 30 minutes after resuming traffic. Look for:
   - No `tx conflict` or `attach race` errors (transient; should self-resolve)
   - Successful batch commits at the expected cadence (~5 seconds / ~16MB)
   - No `failed to insert spans` or `failed to insert scores` errors

2. **Verify Eval scores** are flowing in for the next 10 minutes by checking the Query API's trace-detail endpoint for new scores.

3. **Configure PVC backup:** set up your own PVC backup policy (e.g. Kubernetes VolumeSnapshot) for the Quack Server's `/data` mount — the Helm chart no longer provides S3 catalog backups. See the README's upgrade notes for details.

## Caveat: Snapshot/time-travel history is lost

DuckLake 1.5 stores snapshot and time-travel metadata in the Catalog file itself. When the Catalog is rebuilt from scratch, this history is discarded. The product does not currently surface snapshot or time-travel functionality to users, so this is acceptable.

## What to do instead (restore from backup)

**If you have a VolumeSnapshot or equivalent PVC backup, skip this entire procedure.** Restore the PVC from the snapshot and the Quack Server will pick up the existing Catalog file automatically — no rebuild needed.

This procedure is strictly for the case where no backup exists.