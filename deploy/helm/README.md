# omneval — Helm Chart

Production-grade deployment of [Omneval](https://github.com/omneval/omneval), a self-hostable LLM/Agent tracing platform.

## Quick start

```bash
helm repo add omneval https://omneval.github.io/charts   # when published
helm install omneval ./deploy/helm --values values.custom.yaml
helm upgrade omneval ./deploy/helm --values values.custom.yaml
helm uninstall omneval
```

## Infrastructure profile

Omneval ships with **three deployment profiles** — choose one by setting `database.driver` and `storage.bucket/endpoint`:

| Profile | `database.driver` | `storage.bucket` | `storage.endpoint` | When to use |
|---|---|---|---|---|
| **Demo** (default) | `sqlite` | (empty) | (empty) | Local development, prototyping |
| **Full (MinIO)** | `postgres` | (any) | `http://<release>-minio:9000` | Self-contained cluster, no external storage |
| **Production** | `postgres` | `my-bucket` | `https://s3.amazonaws.com` | External Postgres + S3-compatible store |

## Configuration

All overridable values are listed in [values.yaml](values.yaml) with inline comments. The most common customisations:

### Quack Server auth token (existingSecret)

The Quack Server token authenticates every client (Writer, Query, Eval) to the Lake. To avoid committing the token to your HelmRelease, source it from an externally-managed Secret:

```yaml
quack:
  server:
    # Pin a value in a pre-existing Secret and have all pods source the token from it.
    # This is the recommended approach for GitOps (Flux, ArgoCD) because the token lives
    # in a separate Secret resource that can be managed by a vault sync, SOPS, or similar.
    # When this is set, the chart does NOT write quack-token into omneval-secret.
    existingSecret: my-quack-token-secret
    existingSecretKey: token    # key inside the Secret holding the token value

    # ───────────────────────────────────────────────────────────────────────
    # Three ways to provide the token, in precedence order:
    #
    # 1. existingSecret (recommended) — token lives in a separate Secret,
    #    never appears in your HelmRelease or omneval-secret.
    #
    # 2. token — pin a plaintext token in values or valuesFrom. The chart
    #    writes it to omneval-secret on install and reuses it on upgrade.
    #    Simpler but the token is committed to git.
    #
    # 3. (unset) — the chart generates a random token on first install and
    #    persists it in omneval-secret for subsequent upgrades. Avoid this
    #    in production because the generated value can drift between pods
    #    during rolling upgrades.
    # ───────────────────────────────────────────────────────────────────────
    token: ""                   # plaintext — used only when existingSecret is empty
```

### External infrastructure

Set `<component>.enabled: false` and populate `<component>.external.*` to use your own backing services:

```yaml
redis:
  enabled: false
  external:
    addr: "redis.mycompany.com:6379"
    password: "s3cr3t"

postgresql:
  enabled: false
  external:
    host: "postgres.mycompany.com"
    port: 5432
    database: "omneval"
    user: "omneval"
    password: "s3cr3t"
    sslmode: "require"

minio:
  enabled: false
  external:
    endpoint: "https://s3.amazonaws.com"
    bucket: "omneval-lake"
    region: "us-east-1"
    accessKey: "AKIA..."
    secretKey: "..."
```

### Bootstrap admin credentials (Query API)

Auto-create the first admin user on initial install:

```yaml
query:
  adminEmail: "admin@example.com"
  adminPassword: "s3cr3t"

  # Or source from an existing Secret (same pattern as quack):
  existingSecret: "query-admin-secret"
  emailKey: "admin-email"
  passwordKey: "admin-password"
```

## Chart templates

| Template | Kind | Description |
|---|---|---|
| `configmap.yaml` | ConfigMap | Shared Omneval YAML config for all pods |
| `secret.yaml` | Secret | S3 credentials, Redis password, LLM API key, Quack token, DSN |
| `quack-server-statefulset.yaml` | StatefulSet + Service | Quack Server (Lake gateway) |
| `ingest-deployment.yaml` | Deployment + Service | Ingest API |
| `writer-deployment.yaml` | Deployment | Writer Service |
| `query-deployment.yaml` | Deployment | Query API + UI |
| `eval-deployment.yaml` | Deployment | Eval Workers |
| `redis-deployment.yaml` | Deployment | Redis broker |
| `postgresql-statefulset.yaml` | StatefulSet | PostgreSQL metadata store |
| `minio-deployment.yaml` | Deployment | MinIO object store |

## Helm unittest

Unit tests live in [tests/](tests/) using [helm-unittest](https://github.com/helm-unittest/helm-unittest):

```bash
helm plugin install https://github.com/helm-unittest/helm-unittest  # if not installed
helm unittest deploy/helm
```