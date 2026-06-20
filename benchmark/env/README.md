# Benchmark Environment — Single-Node, Real-S3

A reproducible deployment of omneval sized to represent a realistic, modest self-host shape — not a maxed-out benchmark rig — so the numbers mean something to prospective self-hosters.

## Topology

| Component | Replicas |
|---|---|
| Ingest API | 1 |
| Writer Service | 1 |
| Query API | 1 |
| Quack Server (DuckDB catalog) | 1 |
| Eval Workers | 0 (no eval load) |
| PostgreSQL | 1 (internal) |
| Redis | 1 (internal) |
| MinIO | disabled (real S3) |

Every component runs as a single pod on one Kubernetes node.

## Why This Shape

### Node selection: single node, ~4 vCPU / 8 GiB

We chose a single modest node rather than a multi-node cluster because:

- **Representative, not maximal.** A typical self-hoster runs omneval on a single box — a home lab node, a small cloud instance, or a spare machine. This shape mirrors that reality, so throughput and latency numbers are directly meaningful.
- **Self-hosters don't have auto-scaling.** The numbers from a single-node deployment reflect what a self-hosted operator can actually achieve, without hiding behind horizontal scaling.
- **Cost-aware.** A single modest node costs very little while running, which is realistic for a self-host profile.
- **Simplicity.** A single-node environment removes node-pinning, affinity, and scheduling as variables, making results easier to reproduce.

A concrete example of the kind of node we had in mind:

- **Cloud provider equivalent:** AWS `m6i.large` (2 vCPU / 8 GiB) or `t3a.medium` (2 vCPU / 4 GiB).
- **Homelab equivalent:** A mini-PC with 4 physical cores, 16 GiB RAM, and SSD storage.

The Helm values set resource **requests** and **limits** conservatively so the node can comfortably schedule all pods simultaneously without contention. Total resource demand across all pods:

| Component | CPU request | CPU limit | Memory request | Memory limit |
|---|---|---|---|---|
| Ingest API | 100m | 500m | 128 Mi | 256 Mi |
| Writer | 200m | 1 | 256 Mi | 512 Mi |
| Query API | 100m | 500m | 128 Mi | 256 Mi |
| Quack Server | 200m | 1 | 256 Mi | 1 Gi |
| PostgreSQL | 100m | 500m | 256 Mi | 512 Mi |
| Redis | 100m | 500m | 128 Mi | 256 Mi |
| **Totals** | **700m** | **3.1** | **1.1 Gi** | **2.8 Gi** |

A single node with 4 vCPU / 8 GiB has more than enough headroom, but the node type is chosen to be *representative* of a realistic self-host, not over-provisioned.

## Catalog driver

The Quack Server uses `catalogDriver: duckdb` (a local DuckDB file on the Quack Server's PVC) — the default since ADR-0006. The Writer, Query API, and Eval Worker attach as Quack clients via `quack://`, never holding a direct Catalog connection (ADR-0005).

## Storage — Real S3, not MinIO

S3 latency is part of the ingest/end-to-end-latency story, and in-cluster MinIO numbers don't transfer to what a real self-hoster experiences. The environment uses a real cloud S3 bucket (AWS S3, Cloudflare R2, or any S3-compatible provider).

**You must provide your own S3 bucket and credentials.** The Helm chart will create the bucket's Parquet data path (`s3://<bucket>/lake/...`) but will not create the bucket itself — that is an IAM operation.

### S3 bucket creation (one-time)

```bash
# AWS S3 example:
aws s3api create-bucket \
  --bucket my-omneval-bucket \
  --region us-east-1

# Cloudflare R2 example:
# R2 buckets are created automatically when you write to them;
# no separate creation step needed.
```

## Quick Start

### Prerequisites

- A Kubernetes cluster (k3s, EKS, GKE, AKS, DigitalOcean, local minikube/kind, etc.)
- `kubectl` configured with cluster access
- `helm` v3.x installed
- Docker (for building the omneval image)
- An S3 bucket and credentials (see above)

### Step 1 — Build the Docker image

```bash
cd /path/to/repo
docker build -t omneval/app:latest .
```

### Step 2 — Export your credentials

```bash
export OMNEVAL_S3_BUCKET="my-omneval-bucket"
export OMNEVAL_S3_ENDPOINT="https://s3.us-east-1.amazonaws.com"
# For Cloudflare R2: "https://<account-id>.r2.cloudflarestorage.com"
export OMNEVAL_S3_ACCESS_KEY="<your-access-key>"
export OMNEVAL_S3_SECRET_KEY="<your-secret-key>"
```

### Step 3 — Deploy

```bash
cd /path/to/repo/benchmark/env
./deploy_benchmark.sh
```

Or with explicit CLI args (overrides env vars):

```bash
./deploy_benchmark.sh \
  --bucket "my-omneval-bucket" \
  --endpoint "https://s3.us-east-1.amazonaws.com" \
  --access-key "<your-access-key>" \
  --secret-key "<your-secret-key>" \
  --image-tag "v0.0.8"
```

### Step 4 — Port-forward for local access

```bash
kubectl port-forward --namespace omneval-benchmark svc/omneval-benchmark-ingest 8000:8000 &
kubectl port-forward --namespace omneval-benchmark svc/omneval-benchmark-query 8002:8002 &
```

### Step 5 — Verify

```bash
./validate_benchmark.sh
```

## Teardown

```bash
./teardown_benchmark.sh
```

This removes the Helm release, all pods, PVCs, and associated Kubernetes resources. **The S3 bucket is NOT automatically deleted** (see below).

### ⚠️ Cost warning

While running, this environment incurs real cloud cost:

| Resource | Ongoing cost driver |
|---|---|
| S3 bucket | Object storage (bytes stored × duration) |
| Kubernetes node | Compute (hourly rate of the node) |
| Kubernetes persistent volume | Block storage (GiB × duration) |

The S3 bucket persists after teardown — you **must** delete it manually:

```bash
# AWS S3 — delete all objects, then the bucket:
aws s3 rb s3://my-omneval-bucket --force

# Cloudflare R2: use the R2 console or an S3-compatible CLI tool.
```

## Reproducibility

Running `./deploy_benchmark.sh` twice in a row (after a `./teardown_benchmark.sh` in between) produces an equivalent environment — no manual, undocumented steps.

The `helm upgrade --install` command is inherently idempotent: if the release already exists, it applies the current Helm chart values to update; if it does not exist, it installs. The same inputs always produce the same resources.

## Pointing the Benchmark Harness

When the follow-on benchmark-harness issue is implemented, it will need these endpoints and credentials from this environment.

### Ingest API

```
URL:    http://localhost:8000
Port:   8000 (after port-forward)
Auth:   API key via header `x-api-key` or `Authorization: Bearer <key>`
```

### Query API

```
URL:    http://localhost:8002
Port:   8002 (after port-forward)
Auth:   Session cookie (for UI flows) or API key for programmatic access
```

### Quack Server (internal)

```
URL:    omneval-benchmark-quack-server:9494
Port:   9494
Token:  sourced from the Helm-managed secret (same as Writer/Query/Eval use)
```

### Obtaining API keys

1. Log into the Query API UI at the Query API URL.
2. Navigate to **Settings → Project → API Keys**.
3. Create a new API key for the benchmark project.
4. Use this key with the benchmark harness when talking to the Ingest API.

### Obtaining project information

The Ingest API's project creation endpoint (`POST /api/v1/projects`) can be called to create a benchmark project if one doesn't exist:

```bash
curl -X POST http://localhost:8000/api/v1/projects \
  -H "Content-Type: application/json" \
  -d '{"name":"benchmark-project","description":"Automated benchmark project"}'
```

### S3 credentials (for direct Lake access)

If the benchmark harness needs to read Parquet files directly from the Lake (for ground-truth verification), use the same S3 credentials passed to the Helm deploy:

```bash
export AWS_ACCESS_KEY_ID="$OMNEVAL_S3_ACCESS_KEY"
export AWS_SECRET_ACCESS_KEY="$OMNEVAL_S3_SECRET_KEY"
export AWS_DEFAULT_REGION="us-east-1"
# or the region matching your S3 bucket
```

## Customisation

### Change the node shape

Edit `values-benchmark.yaml` and adjust resource requests/limits, replica counts, or the Quack Server PVC size. Then re-run the deploy script.

### Use external PostgreSQL or Redis

For a production-like environment with your own managed databases, set:

```bash
helm upgrade --install omneval-benchmark ./deploy/helm \
  --values ./benchmark/env/values-benchmark.yaml \
  --set postgresql.enabled=false \
  --set postgresql.external.host="your-rds-host.example.com" \
  --set postgresql.external.database="omneval" \
  --set postgresql.external.user="omneval" \
  --set postgresql.external.password="your-password" \
  --set redis.enabled=false \
  --set redis.external.addr="your-redis-host:6379"
```

### Use an external S3 credentials Secret

For GitOps workflows (Flux, ArgoCD), store S3 credentials in a pre-existing Kubernetes Secret:

```bash
kubectl create secret generic omneval-s3-creds \
  --namespace omneval-benchmark \
  --from-literal=access-key="$OMNEVAL_S3_ACCESS_KEY" \
  --from-literal=secret-key="$OMNEVAL_S3_SECRET_KEY"

# Then set storage.existingSecret: "omneval-s3-creds" in values-benchmark.yaml.
```

## File Index

| File | Purpose |
|---|---|
| `values-benchmark.yaml` | Helm values for single-replica, real-S3 topology |
| `deploy_benchmark.sh` | Idempotent deployment script |
| `teardown_benchmark.sh` | Full teardown with S3 cleanup guidance |
| `validate_benchmark.sh` | Health-check verification script |
| `README.md` | This file — full documentation |