# Catalog Cutover: Rebuild-from-Parquet Procedure

**Status:** approved procedure  
**Applies to:** deployments migrating from `CatalogDriverPostgres` to `CatalogDriverLocal` (catalog driver `duckdb`)  
**Related:** ADR-0006 — DuckDB-native catalog as the production default; CONTEXT.md — Disaster Recovery entry

## Scope

This procedure describes how to move an Omneval deployment from `CatalogDriverPostgres` (Postgres-backed DuckLake Catalog) to `CatalogDriverLocal` (local DuckDB-file Catalog stored on the Quack Server's PVC) by **rebuilding** the Catalog from the Lake's existing Parquet files. This is **not** an online migration — it requires a bounded downtime window during which no Writer pods can commit spans or scores.

This document serves two purposes:

1. **Planned cutover** — the one-time migration procedure for the existing live `CatalogDriverPostgres` instance.
2. **Disaster recovery** — the permanent procedure for "Catalog file lost, no S3 backup available" (CONTEXT.md's Disaster Recovery entry). In the DR case, the Catalog file itself is missing or corrupted and must be rebuilt from scratch.

In both cases the Catalog's DuckLake snapshot and time-travel history is discarded because that history lives only in the Catalog file and is not surfaced in the product today.

## Prerequisites

- `kubectl` access to the target cluster with permissions to manage Deployments and StatefulSets in the Omneval release namespace.
- Helm 3 installed (used for re-deploying the Quack Server).
- Access to the S3 bucket where the Lake's Parquet files and Catalog backups are stored.
- The release name (the Helm release prefix for all resources, typically `omneval` or `omneval-<environment>`).
- The Helm values file or values overrides used for the production deployment.

## Key configuration references

| Setting | Helm value | Environment variable | Description |
|---------|-----------|---------------------|-------------|
| Catalog driver | `quack.server.catalogDriver` | `OMNEVAL_QUACK_SERVER_CATALOG_DRIVER` | `"postgres"` (old) → `"duckdb"` (new) |
| Catalog DSN | `quack.server.catalogDSN` | `OMNEVAL_QUACK_SERVER_CATALOG_DSN` | Postgres DSN (postgres) or local path `lake/catalog.duckdb` (duckdb) |
| Quack backup enabled | `quack.server.backup.enabled` | `OMNEVAL_QUACK_SERVER_BACKUP_ENABLED` | Controls Catalog backup scheduler (default `true`) |
| Quack backup interval | `quack.server.backup.interval` | `OMNEVAL_QUACK_SERVER_BACKUP_INTERVAL` | Backup frequency (default `"1h"`) |
| Quack backup keep count | `quack.server.backup.keepCount` | `OMNEVAL_QUACK_SERVER_BACKUP_KEEP_COUNT` | Max backups to retain (default `24`) |
| Lake data path | `quack.server.dataPath` / `quack.client.dataPath` | `OMNEVAL_QUACK_SERVER_DATA_PATH` / `OMNEVAL_QUACK_CLIENT_DATA_PATH` | `s3://<bucket>/lake` for production |

The Quack Server statefulset mounts the PVC at `/data`; the default catalog file path when `catalogDSN` is empty and `catalogDriver` is `duckdb` is `lake/catalog.duckdb` relative to the data path (i.e. `/data/lake/catalog.duckdb` on the PVC).

## Phase 0 — Pre-flight checks

Record the current state before making changes:

```bash
# 1. Verify current catalog driver
kubectl get configmap <release>-config -n <namespace> -o jsonpath='{.data.omneval\.yaml}' | grep catalog_driver

# 2. Count the Parquet files in the Lake (for post-cutover validation)
aws s3 ls s3://<bucket>/lake/ --recursive

# 3. Record the current Kubernetes revision of the Quack Server StatefulSet
kubectl get statefulset <release>-quack-server -n <namespace> -o jsonpath='{.metadata.generation}'

# 4. Record how many Writer, Query API, and Eval pods are currently running
kubectl get pods -n <namespace> -l app.kubernetes.io/component=writer -o wide
kubectl get pods -n <namespace> -l app.kubernetes.io/component=query -o wide
kubectl get pods -n <namespace> -l app.kubernetes.io/component=eval -o wide
```

## Phase 1 — Stop Writer pods

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

### Expected downtime window begins

From this point forward, no new spans or scores can be committed to the Lake. The window lasts until Phase 6 completes.

## Phase 2 — Record current Lake state

Before removing the old Catalog, record the Lake's current state from a read-only Quack-client query. This serves as the audit trail and gives you something to compare against after the rebuild.

```bash
# Open a DuckDB shell or use an existing Query API pod to run the counts
# These queries count distinct spans and scores by project
kubectl exec -it <release>-query-<podid> -n <namespace> -- \
  duckdb -c "
    ATTACH 'quack://<release>-quack-server:9494' AS lake (DATA_PATH 's3://<bucket>/lake', TOKEN '<quack-token>');
    SELECT COUNT(*) AS span_count FROM lake.spans;
    SELECT COUNT(*) AS score_count FROM lake.scores;
  "
```

Record the span and score counts. These are the figures the post-cutover validation must match.

## Phase 3 — Remove the old Quack Server (Postgres-backed Catalog)

Delete the existing Quack Server StatefulSet and its PVC to ensure a clean slate:

```bash
# Delete the StatefulSet (pods terminate, PVC persists by default)
kubectl delete statefulset <release>-quack-server -n <namespace>

# If this is the planned cutover (Catalog file not lost), you may keep the
# old PVC as an archival reference. If this is a disaster-recovery rebuild
# (catalog file corrupted), delete the PVC to start fresh:
# kubectl delete pvc -l app.kubernetes.io/component=quack-server -n <namespace>
```

The PVC will be re-provisioned automatically when the StatefulSet is recreated in Phase 4, because the PVC claim is part of the Helm template.

## Phase 4 — Deploy Quack Server with `catalogDriver: duckdb`

Update the Helm values or values override to set `catalogDriver: duckdb` and deploy the new Quack Server:

### Helm values update

Add or update the following in your Helm values override:

```yaml
quack:
  server:
    catalogDriver: "duckdb"
    # catalogDSN is empty — defaults to lake/catalog.duckdb on the PVC
    catalogDSN: ""
    dataPath: ""  # empty — derived from storage.bucket → s3://<bucket>/lake
    # Backup is meaningful only when catalogDriver is duckdb:
    backup:
      enabled: true
      interval: "1h"
      keepCount: 24
```

### Deploy

```bash
# Dry-run first
helm upgrade <release> omneval/helm -n <namespace> --values=<your-values-file> \
  --dry-run --debug

# Apply the upgrade
helm upgrade <release> omneval/helm -n <namespace> --values=<your-values-file>

# Wait for the Quack Server to become ready
kubectl rollout status statefulset/<release>-quack-server -n <namespace> --timeout=300s

# Verify the new catalog driver is active
kubectl exec -it <release>-quack-server-0 -n <namespace> -- \
  cat /etc/omneval/omneval.yaml | grep catalog_driver
# Expected output: catalog_driver: duckdb
```

### Verify the PVC is mounted and the catalog file is fresh

```bash
kubectl exec -it <release>-quack-server-0 -n <namespace> -- \
  ls -la /data/lake/catalog.duckdb
# Expected: file exists if it was created by the prior Quack Server; otherwise
# it will be created on first attach by a Quack client.
```

## Phase 5 — Re-register existing Parquet files as a DuckLake table set

A freshly deployed `CatalogDriverLocal` does not yet know about the existing `spans` and `scores` tables in the Lake's Parquet files. You must register them so the Catalog's metadata describes the Parquet data.

Run the registration from a temporary DuckDB client session (or from the Query API pod during validation) that attaches the new local-file Catalog and writes the table definitions.

### Register the `spans` table

```bash
# Register spans table metadata in the new Catalog
kubectl exec -it <release>-quack-server-0 -n <namespace> -- \
  duckdb -c "
    -- Attach the new local-file Catalog
    ATTACH 'lake/catalog.duckdb' AS catalog;
    USE catalog;

    -- Install and load the quack extension
    INSTALL quack;
    LOAD quack;

    -- Register the spans table: point DuckLake at the existing Parquet files
    -- The exact table creation syntax depends on DuckLake 1.5 API.
    -- This is illustrative — verify the actual DDL against your DuckLake version:
    CREATE TABLE lake.spans (
      span_id VARCHAR,
      trace_id VARCHAR,
      parent_id VARCHAR,
      conversation_id VARCHAR,
      project_id VARCHAR,
      kind VARCHAR,
      model VARCHAR,
      input VARCHAR,
      output VARCHAR,
      input_tokens INTEGER,
      output_tokens INTEGER,
      cost_usd DOUBLE,
      start_time TIMESTAMP,
      end_time TIMESTAMP,
      attributes VARCHAR,
      status VARCHAR
    );

    -- Set the data path for Lake data files
    CALL ducklake_set_data_path('s3://<bucket>/lake');
  "
```

### Register the `scores` table

```bash
kubectl exec -it <release>-quack-server-0 -n <namespace> -- \
  duckdb -c "
    ATTACH 'lake/catalog.duckdb' AS catalog;
    USE catalog;
    INSTALL quack;
    LOAD quack;

    CREATE TABLE lake.scores (
      span_id VARCHAR,
      project_id VARCHAR,
      value DOUBLE,
      reasoning VARCHAR,
      judge_model VARCHAR,
      judge_prompt_version VARCHAR,
      span_start_time TIMESTAMP
    );

    CALL ducklake_set_data_path('s3://<bucket>/lake');
  "
```

### Verify table registration

```bash
kubectl exec -it <release>-quack-server-0 -n <namespace> -- \
  duckdb -c "
    ATTACH 'lake/catalog.duckdb' AS catalog;
    USE catalog;
    SHOW TABLES;
    -- Expected: lake.spans, lake.scores
  "
```

## Phase 6 — Validation checklist

**Before resuming Writer pods, all three of the following checks must pass.** This is the "done" signal for the cutover.

### Check 1 — Writer commit (write a new span)

Confirm that a Writer can successfully commit spans to the rebuilt Catalog:

```bash
# 1. Temporarily start a single Writer replica
kubectl scale deployment <release>-writer -n <namespace> --replicas=1

# 2. Wait for the Writer pod to start
kubectl rollout status deployment/<release>-writer -n <namespace> --timeout=120s

# 3. Trigger a span ingest (or use the load-test tool)
# Option A: send an OTLP span via curl to the Ingest API
curl -X POST http://<release>-ingest.<namespace>.svc.cluster.local:8000/v1/traces \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer <api-key>' \
  -d '[{
    "resourceSpans": [{
      "scopeSpans": [{
        "spans": [{
          "name": "test-span",
          "startTimeUnixNano": "'$(date +%s%3N)'000000",
          "endTimeUnixNano": "'$(date +%s%3N)'000000",
          "attributes": [{"key":"test","value":{"stringValue":"cutover-validation"}}]
        }]
      }]
    }]
  }]'

# 4. Verify the span appears in the Lake (read via Query API)
sleep 10  # allow the Writer's Commit Cadence to fire

# 5. Count spans — the total should equal the pre-cutover count + 1
kubectl exec -it <release>-query-0 -n <namespace> -- \
  curl -s http://localhost:8002/api/v1/spans/query \
  -H "Authorization: Bearer <admin-token>" | jq '.total'
# Verify: total == <pre-cutover span count> + 1
```

**If this check fails:** the Catalog's table registration in Phase 5 is incomplete or the ATTACH statement is misconfigured. Review the `catalogDSN`, `dataPath`, and the CREATE TABLE statements.

### Check 2 — Eval score write-back (write a score to an existing span)

Confirm that the Eval service can write scores to spans already in the Lake:

```bash
# 1. Find a span ID to score (use the test span from Check 1 or any existing span)
SPAN_ID=$(kubectl exec -it <release>-query-0 -n <namespace> -- \
  curl -s http://localhost:8002/api/v1/spans/query \
  -H "Authorization: Bearer <admin-token>" | jq -r '.spans[0].span_id')

# 2. Write an eval score via the internal score endpoint
curl -X POST http://<release>-writer.<namespace>.svc.cluster.local:8001/internal/v1/scores \
  -H 'Content-Type: application/json' \
  -d "{
    \"span_id\": \"${SPAN_ID}\",
    \"project_id\": \"<project-id>\",
    \"value\": 0.95,
    \"reasoning\": \"cutover-validation-test\"
  }"

# 3. Verify the score appears in the Lake
kubectl exec -it <release>-query-0 -n <namespace> -- \
  curl -s http://localhost:8002/api/v1/spans/${SPAN_ID} \
  -H "Authorization: Bearer <admin-token>" | jq '.scores'
# Expected: score object with value 0.95 and reasoning "cutover-validation-test"
```

**If this check fails:** the scores table registration is missing or the Writer cannot write through the Quack client. Check that the Writer's `quack.client.token` matches the Quack Server's token and that the `scores` table exists in the new Catalog.

### Check 3 — Query read all (read back the full span set)

Confirm that the Query API can read all existing spans and scores through the rebuilt Catalog:

```bash
# 1. Read the total span count
SPAN_COUNT=$(kubectl exec -it <release>-query-0 -n <namespace> -- \
  curl -s http://localhost:8002/api/v1/spans/query \
  -H "Authorization: Bearer <admin-token>" | jq '.total')

# 2. Compare against the pre-cutover count recorded in Phase 2
echo "Pre-cutover span count: <pre-cutover count>"
echo "Post-cutover span count: ${SPAN_COUNT}"
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

## Phase 7 — Resume Writer pods

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

# 2. Check Quack Server backups are starting (if backup.enabled=true)
kubectl logs <release>-quack-server-0 -n <namespace> --tail=30 | grep -i backup
# Expected: CHECKPOINT succeeded, backup uploaded to s3://...

# 3. Verify the Helm release state
helm status <release> -n <namespace>
# Expected: STATUS: deployed, REVISION incremented from pre-cutover

# 4. Check the new catalog driver
kubectl exec -it <release>-quack-server-0 -n <namespace> -- \
  grep catalog_driver /etc/omneval/omneval.yaml
# Expected: catalog_driver: duckdb
```

## Post-cutover

1. **Monitor Writer logs** for at least 30 minutes after resuming traffic. Look for:
   - No `tx conflict` or `attach race` errors (transient; should self-resolve)
   - Successful batch commits at the expected cadence (~5 seconds / ~16MB)
   - No `failed to insert spans` or `failed to insert scores` errors

2. **Verify Eval scores** are flowing in for the next 10 minutes by checking the Query API's trace-detail endpoint for new scores.

3. **Confirm backup scheduler** is running: the Quack Server logs should show periodic `CHECKPOINT` and `backup uploaded` messages (at the configured interval, default 1 hour).

4. **Archival:** Keep the old Postgres catalog accessible (or at least record the Postgres DSN) for the first 24 hours as a rollback reference. No tool exists to replay Postgres `ducklake_*` tables back into a new Catalog file if something goes wrong.

## Rollback procedure

If the validation checks fail and the rebuilt Catalog cannot be made functional within the acceptable downtime window:

```bash
# 1. Scale Writers back to zero
kubectl scale deployment <release>-writer -n <namespace> --replicas=0

# 2. Revert Helm values to use catalogDriver: postgres
# (edit your values file or use --set on the command line)
helm upgrade <release> omneval/helm -n <namespace> \
  --set quack.server.catalogDriver=postgres \
  --set quack.server.catalogDSN=<postgis-dsn>

# 3. Wait for the Quack Server to come up with Postgres catalog
kubectl rollout status statefulset/<release>-quack-server -n <namespace> --timeout=300s

# 4. Verify the old Catalog's data is readable
kubectl exec -it <release>-query-0 -n <namespace> -- \
  curl -s http://localhost:8002/api/v1/spans/query \
  -H "Authorization: Bearer <admin-token>" | jq '.total'
# Expected: matches the pre-cutover span count (minus any batches lost during the cutover window)

# 5. Resume Writers
kubectl scale deployment <release>-writer -n <namespace> --replicas=<production-count>
```

## Notes

- **Snapshot/time-travel history is lost.** DuckLake 1.5 stores snapshot and time-travel metadata in the Catalog file itself. When the Catalog is rebuilt from scratch, this history is discarded. This is acceptable because the product does not currently surface snapshot or time-travel functionality to users.
- **Downtime is bounded by the cutover duration.** From Phase 1 (stop Writers) to Phase 7 (resume Writers), no new spans or scores can be committed. Plan the cutover during a low-traffic window.
- **Ingest Buffer replay.** Batches that were ingested but not yet committed to the Lake by the Writers at the time of cutover survive in the Ingest Buffer (S3). The Writers' reconciliation sweep will replay them once Writers resume.
- **This document is the permanent DR procedure for "Catalog file lost, no S3 backup available."** In a disaster-recovery scenario where the Catalog file is corrupted or the PVC is lost, skip Phases 0–4 (pre-flight, stop Writers, record state, remove old catalog) and proceed directly from the Writers-stop step through the rebuild steps. The Prerequisites and Phase 1 remain identical.