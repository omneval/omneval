# Lantern Kustomize Base

Kustomize-native Kubernetes manifests for the Lantern tracing platform. Designed for GitOps deployments via **Flux CD** or **ArgoCD**.

## Structure

```
deploy/kustomize/
├── base/                          # Core manifests (no per-env overrides)
│   ├── kustomization.yaml         # Kustomization referencing all resources
│   ├── namespace.yaml             # Namespace: lantern
│   ├── configmap.yaml             # ConfigMap: lantern-config (lantern.yaml)
│   ├── secret.yaml                # Secret: lantern-secret (credentials)
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
│   ├── production/                # Production: 3+ replicas, larger resources
│   │   └── kustomization.yaml     # Replicas + resource patches
│   └── dev/                       # Development: 1 replica, small resources, emptyDir
│       └── kustomization.yaml     # Replicas + resource patches + PVC → emptyDir
```

## Quick Start

### Render raw manifests

```bash
# Base (useful for debugging)
kustomize build deploy/kustomize/base

# Production overlay
kustomize build deploy/kustomize/overlays/production

# Dev overlay
kustomize build deploy/kustomize/overlays/dev
```

### Apply directly

```bash
kustomize build deploy/kustomize/overlays/production | kubectl apply -f -
```

## Flux CD Integration

Create a `Kustomization` resource in your flux-system namespace:

```yaml
apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: lantern-production
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
  # Depends on Redis and PostgreSQL being ready first
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
  name: lantern-dev
  namespace: flux-system
spec:
  interval: 5m
  path: ./deploy/kustomize/overlays/dev
  prune: true
  wait: true
  sourceRef:
    kind: GitRepository
    name: flux-system
```

## ArgoCD Integration

Create an `Application` resource:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: lantern-production
  namespace: argocd
spec:
  project: default
  # Point to the overlay directory (not the base)
  source:
    repoURL: https://github.com/<org>/lantern.git
    targetRevision: main
    path: deploy/kustomize/overlays/production
  destination:
    server: https://kubernetes.default.svc
    namespace: lantern
  # Auto-sync enabled for GitOps workflow
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    syncOptions:
      - CreateNamespace=true
```

### Multi-env ArgoCD setup

Use a single ArgoCD app with environment-override directories:

```
deploy/kustomize/
├── base/
├── overlays/
│   ├── dev/
│   ├── staging/
│   └── production/
```

Point separate ArgoCD `Application` resources at each overlay, all reading from the same Git repo.

## Configuration

### Secrets

The base includes a `Secret` with placeholder values. Override per environment:

| Key | Description | Per-service env var |
|-----|-------------|---------------------|
| `storage-access-key` | S3-compatible storage access key | `LANTERN_STORAGE_ACCESS_KEY` |
| `storage-secret-key` | S3-compatible storage secret key | `LANTERN_STORAGE_SECRET_KEY` |
| `eval-llm-api-key` | Judge LLM API key | `LANTERN_EVAL_LLM_API_KEY` |
| `redis-password` | Redis auth password | `LANTERN_REDIS_PASSWORD` |
| `admin-password` | First admin user password | `LANTERN_AUTH_ADMIN_PASSWORD` |
| `admin-email` | First admin user email | `LANTERN_AUTH_ADMIN_EMAIL` |
| `postgres-dsn` | PostgreSQL connection string | `LANTERN_DATABASE_DSN` |
| `postgres-password` | PostgreSQL password | (via DSN) |

**Recommended**: Use SealedSecrets, External Secrets, or SOPS to inject real values in production. Do not commit real credentials to Git.

### ConfigMap

The `lantern-config` ConfigMap holds `lantern.yaml` with default values. Override per environment by patching the ConfigMap in your overlay:

```yaml
# overlays/staging/kustomization.yaml
patches:
  - target:
      kind: ConfigMap
      name: lantern-config
    patch: |-
      apiVersion: v1
      kind: ConfigMap
      metadata:
        name: lantern-config
      data:
        lantern.yaml: |
          database:
            driver: postgres
            dsn: postgres://lantern:secret@lantern-postgresql:5432/lantern
          redis:
            addr: lantern-redis-master:6379
          storage:
            endpoint: http://minio:9000
            bucket: lantern-staging
          ...
```

### Image override

Override the container image in your overlay:

```yaml
# overlays/production/kustomization.yaml
images:
  - name: lantern/app
    newName: ghcr.io/myorg/lantern
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

The `lantern-volume-snapshot-config` ConfigMap holds the `VolumeSnapshotClass`
name. Override this in your overlay to match your cloud provider's snapshot
class:

```yaml
# overlays/production/kustomization.yaml
patches:
  - target:
      kind: ConfigMap
      name: lantern-volume-snapshot-config
    patch: |-
      apiVersion: v1
      kind: ConfigMap
      metadata:
        name: lantern-volume-snapshot-config
      data:
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

The CronJob creates snapshots tagged with `lantern.io/backup-type=volume-snapshot`
for easy identification and cleanup.

See `docs/restore-from-snapshot.md` for full restore procedures.

## Architecture Notes

- **Writer** is a StatefulSet with a `ReadWriteOnce` PVC for the DuckDB file. It must remain single-replica (DuckDB does not support concurrent writes).
- **Query API** is fully stateless — reads its DuckDB snapshot from S3, never mounts the PVC.
- **Eval Workers** have no HTTP port (queue consumers). Their Service only exposes the metrics endpoint (9090) for Prometheus scraping.
- **Leader election** for Writer is disabled by default. Enable it in `lantern.yaml` for multi-replica HA (`writer.leader_election.enabled: true`).
- All services mount the `lantern-config` ConfigMap at `/etc/lantern/lantern.yaml` and can override individual settings via `LANTERN_*` environment variables.
