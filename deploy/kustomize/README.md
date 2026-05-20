# Omneval Kustomize Base

Kustomize-native Kubernetes manifests for the Omneval tracing platform. Designed for GitOps deployments via **Flux CD** or **ArgoCD**.

## Structure

```
deploy/kustomize/
├── base/                          # Core manifests (no per-env overrides)
│   ├── kustomization.yaml         # Kustomization referencing all resources
│   ├── namespace.yaml             # Namespace: omneval
│   ├── configmap.yaml             # ConfigMap: omneval-config (omneval.yaml)
│   ├── secret.yaml                # Secret: omneval-secret (credentials)
│   ├── redis-deployment.yaml      # Redis Deployment + Service + PVC
│   ├── postgresql-statefulset.yaml # PostgreSQL StatefulSet + Service + ConfigMap
│   ├── minio-deployment.yaml      # MinIO Deployment + Service + PVC + init Job
│   ├── ingest-deployment.yaml     # Ingest API Deployment (port 8000)
│   ├── writer-statefulset.yaml    # Writer StatefulSet with PVC (port 8001)
│   ├── writer-snapshot-cronjob.yaml  # CronJob: PVC VolumeSnapshot (daily at 02:00 UTC)
│   ├── volume-snapshot-configmap.yaml # ConfigMap: snapshot class configuration
│   ├── volume-snapshot-rbac.yaml      # RBAC: ServiceAccount + ClusterRole for snapshots
│   ├── query-deployment.yaml      # Query API Deployment (port 8002)
│   ├── eval-deployment.yaml       # Eval Workers Deployment (no HTTP port)
│   ├── ingest-service.yaml        # Ingest ClusterIP Service
│   ├── writer-service.yaml        # Writer ClusterIP Service
│   ├── query-service.yaml         # Query ClusterIP Service
│   └── eval-service.yaml          # Eval metrics-only ClusterIP Service
├── overlays/
│   ├── production/                # Production: 3+ replicas, HPAs enabled
│   │   └── kustomization.yaml     # Replicas + resource patches + HPAs
│   ├── staging/                   # Staging: 2 replicas, moderate resources
│   │   └── kustomization.yaml     # Replicas + resource patches
│   └── dev/                       # Development: 1 replica, small resources
│       └── kustomization.yaml     # Replicas + resource patches + PVC → emptyDir
```

## Quick Start

### Render raw manifests

```bash
# Base (useful for debugging)
kustomize build deploy/kustomize/base

# Production overlay
kustomize build deploy/kustomize/overlays/production

# Staging overlay
kustomize build deploy/kustomize/overlays/staging

# Dev overlay
kustomize build deploy/kustomize/overlays/dev
```

### Apply directly

```bash
kustomize build deploy/kustomize/overlays/production | kubectl apply -f -
```

## Infrastructure Options

The Omneval application requires three pieces of infrastructure:

1. **Redis** — Message broker / queue store for inter-service communication
2. **PostgreSQL** — Metadata store (prompts, eval rules, API keys, sessions)
3. **S3-compatible storage** — Cold storage for traces (via MinIO or any S3 provider)

Each can be deployed by our overlays OR you can use your own infrastructure:

### Deploying All Infrastructure (Default)

The base manifests include Redis, PostgreSQL, and MinIO. Just apply an overlay:

```bash
kustomize build deploy/kustomize/overlays/production | kubectl apply -f -
```

### Using External Infrastructure

To use your own infrastructure, disable the internal components by setting `replicas: 0` and configure the addresses in the ConfigMap:

```yaml
# overlays/staging/kustomization.yaml
resources:
  - ../../base

namespace: omneval

replicas:
  - name: omneval-redis
    count: 0
  - name: omneval-postgresql
    count: 0
  - name: omneval-minio
    count: 0

patches:
  - target:
      kind: ConfigMap
      name: omneval-config
    patch: |-
      - op: replace
        path: /data/omneval.yaml
        value: |
          database:
            driver: postgres
            dsn: postgres://omneval:secret@your-postgres-host:5432/omneval?sslmode=disable
          redis:
            addr: your-redis-host:6379
          storage:
            endpoint: https://s3.amazonaws.com
            bucket: your-omneval-bucket
            region: us-east-1
```

You can mix and match — run Redis internally while using an external PostgreSQL and S3.

### Using SQLite Instead of PostgreSQL

For minimal deployments (no metadata persistence needed), use SQLite:

```yaml
patches:
  - target:
      kind: ConfigMap
      name: omneval-config
    patch: |-
      - op: replace
        path: /data/omneval.yaml
        value: |
          database:
            driver: sqlite
          redis:
            addr: omneval-redis:6379
          storage: {}
```

## Flux CD Integration

Create a `Kustomization` resource in your flux-system namespace:

```yaml
apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: omneval-production
  namespace: flux-system
spec:
  # Interval for reconciliation (how often Flux re-applies)
  interval: 10m
  # Path relative to the GitRepository root
  path: ./deploy/kustomize/overlays/production
  # Delete resources not in the Git repo (Prune)
  prune: true
  # Wait for all resources to become healthy
  wait: true
  # Depends on infrastructure being ready first
  dependsOn:
    - name: redis
      namespace: flux-system
    - name: postgresql
      namespace: flux-system
  # Source of the Git repo
  sourceRef:
    kind: GitRepository
    name: flux-system
    namespace: flux-system
```

### Minimal dev Kustomization (SQLite + local Redis)

```yaml
apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: omneval-dev
  namespace: flux-system
spec:
  interval: 5m
  path: ./deploy/kustomize/overlays/dev
  prune: true
  wait: true
  sourceRef:
    kind: GitRepository
    name: flux-system
    namespace: flux-system
```

## ArgoCD Integration

Create an `Application` resource:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: omneval-production
  namespace: argocd
spec:
  project: default
  # Point to the overlay directory (not the base)
  source:
    repoURL: https://github.com/<org>/omneval.git
    targetRevision: main
    path: deploy/kustomize/overlays/production
  destination:
    server: https://kubernetes.default.svc
    namespace: omneval
  # Auto-sync enabled for GitOps workflow
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    syncOptions:
      - CreateNamespace=true
```

### Multi-env ArgoCD setup

Use separate ArgoCD `Application` resources for each environment:

```
deploy/kustomize/
├── base/
├── overlays/
│   ├── dev/
│   ├── staging/
│   └── production/
```

Point each ArgoCD `Application` at its respective overlay directory.

## Configuration

### Secrets

The base includes a `Secret` with default values. Override per environment:

| Key | Description | Per-service env var |
|-----|-------------|---------------------|
| `storage-access-key` | S3-compatible storage access key | `OMNEVAL_STORAGE_ACCESS_KEY` |
| `storage-secret-key` | S3-compatible storage secret key | `OMNEVAL_STORAGE_SECRET_KEY` |
| `eval-llm-api-key` | Judge LLM API key | `OMNEVAL_EVAL_LLM_API_KEY` |
| `redis-password` | Redis auth password | `OMNEVAL_REDIS_PASSWORD` |
| `admin-password` | First admin user password | `OMNEVAL_AUTH_ADMIN_PASSWORD` |
| `admin-email` | First admin user email | `OMNEVAL_AUTH_ADMIN_EMAIL` |
| `postgres-dsn` | PostgreSQL connection string | `OMNEVAL_DATABASE_DSN` |
| `postgres-password` | PostgreSQL password | (via DSN) |
| `minio-root-user` | MinIO root user (internal only) | — |
| `minio-root-password` | MinIO root password (internal only) | — |

**Recommended**: Use SealedSecrets, External Secrets, or SOPS to inject real values in production. Do not commit real credentials to Git.

### ConfigMap

The `omneval-config` ConfigMap holds `omneval.yaml` with default values. Override per environment by patching the ConfigMap in your overlay:

```yaml
# overlays/staging/kustomization.yaml
patches:
  - target:
      kind: ConfigMap
      name: omneval-config
    patch: |-
      - op: replace
        path: /data/omneval.yaml
        value: |
          database:
            driver: postgres
            dsn: postgres://omneval:secret@omneval-postgresql:5432/omneval
          redis:
            addr: omneval-redis:6379
          storage:
            endpoint: http://omneval-minio:9000
            bucket: omneval-staging
          ...
```

### Image override

Override the container image in your overlay:

```yaml
# overlays/production/kustomization.yaml
images:
  - name: omneval/app
    newName: ghcr.io/myorg/omneval
    newTag: v1.2.3
```

### PVC storage class

Override the storage class for the Writer PVC in your overlay:

```yaml
patches:
  - target:
      kind: StatefulSet
      name: writer
    patch: |
      - op: replace
        path: /spec/volumeClaimTemplates/0/spec/storageClassName
        value: fast-ssd
```

### PVC snapshot configuration

The `omneval-volume-snapshot-config` ConfigMap holds the `VolumeSnapshotClass`
name. Override this in your overlay to match your cloud provider's snapshot
class:

```yaml
# overlays/production/kustomization.yaml
patches:
  - target:
      kind: ConfigMap
      name: omneval-volume-snapshot-config
    patch: |-
      - op: replace
        path: /data/omneval.yaml
        value: |
          snapshot-class: "ebs-csi-gp3"
```

Common snapshot class names by cloud provider:
| Provider | Snapshot Class |
|----------|---------------|
| AWS EBS (gp3) | `ebs-csi-gp3` |
| AWS EBS (io2) | `ebs-csi-io2` |
| GCE PD | `pd.csi.storage.gke.io` |
| Azure Disk | `disk.csi.azure.com` |
| OpenStack Cinder | `cinder.csi.openstack.org` |

See `docs/restore-from-snapshot.md` for full restore procedures.

## Horizontal Pod Autoscalers

The production overlay includes HPAs for stateless services:

| Service | Min Replicas | Max Replicas | Target CPU |
|---------|-------------|-------------|------------|
| Ingest | 2 | 10 | 70% |
| Query | 2 | 10 | 70% |
| Eval | 2 | 10 | 70% |

The Writer Service uses a StatefulSet with a single replica (DuckDB does not
support concurrent writes) and does not have an HPA.

## Architecture Notes

- **Writer** is a StatefulSet with a `ReadWriteOnce` PVC for the DuckDB file. It must remain single-replica (DuckDB does not support concurrent writes).
- **Query API** is fully stateless — reads its DuckDB snapshot from S3, never mounts the PVC.
- **Eval Workers** have no HTTP port (queue consumers). Their Service only exposes the metrics endpoint (9090) for Prometheus scraping.
- **Leader election** for Writer is disabled by default. Enable it in `omneval.yaml` for multi-replica HA (`writer.leader_election.enabled: true`).
- **Redis** is deployed as a single-instance Deployment with a PVC. For production, consider using a managed Redis service and disabling the internal deployment.
- **PostgreSQL** is deployed as a single-replica StatefulSet with a PVC. For production, consider using a managed PostgreSQL service.
- **MinIO** is deployed as a single-instance Deployment with a PVC and an init Job to create the bucket. For production, consider using a managed S3-compatible service.
- All services mount the `omneval-config` ConfigMap at `/etc/omneval/omneval.yaml` and can override individual settings via `OMNEVAL_*` environment variables.
