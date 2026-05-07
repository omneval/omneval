# Lantern Local Development with Docker Compose

This directory contains a `docker-compose.yml` that starts all infrastructure
and application services needed to run Lantern locally.

## Prerequisites

- [Docker Engine](https://docs.docker.com/engine/install/) (24+)
- [Docker Compose](https://docs.docker.com/compose/) (v2+)
- ~2 GB RAM for the four Go service containers
- ~512 MB for Redis, PostgreSQL, and MinIO

## Quick Start

```bash
# Navigate to this directory
cd deploy/docker-compose

# (Optional) Copy .env.example to .env and override infrastructure credentials.
# cp .env.example .env

# Build and start all services
docker compose up --build

# Or run in detached mode
docker compose up --build -d
```

After startup, the services will be available at:

| Service | URL |
|---------|-----|
| **Query API + UI** | http://localhost:8002 |
| Ingest API | http://localhost:8000 |
| Writer Service | http://localhost:8001 |
| Eval Workers | http://localhost:8003 |
| MinIO Console | http://localhost:9001 |
| Ingest Metrics | http://localhost:9090 |
| Writer Metrics | http://localhost:9091 |
| Query Metrics | http://localhost:9092 |
| Eval Metrics | http://localhost:9093 |

## Admin Login

An admin user is bootstrapped on first start:

- **Email:** `admin@lantern.dev`
- **Password:** `admin`

## Infrastructure Services

| Service | Image | Ports | Purpose |
|---------|-------|-------|---------|
| Redis | `redis:7-alpine` | 6379 | Message queues (ingest + eval) |
| PostgreSQL | `postgres:16-alpine` | 5432 | Metadata store (projects, users, API keys, prompts) |
| MinIO | `minio/minio:latest` | 9000 / 9001 | S3-compatible object storage (snapshots + Parquet archive) |

### MinIO Credentials

| | Value |
|---|---|
| Access Key | `minio_s3_access_key` |
| Secret Key | `minio_s3_secret_key` |

The `lantern` bucket is created automatically on first start by the `minio-init`
helper container.

## Customizing Infrastructure Credentials

The `.env` file (created by copying `.env.example`) controls the credentials for
PostgreSQL and MinIO. After changing `.env`, rebuild the compose stack:

```bash
docker compose down
docker compose up --build
```

## Common Commands

```bash
# View logs from all services
docker compose logs -f

# View logs from a specific service
docker compose logs -f lantern-query

# Stop all services (preserves data in named volumes)
docker compose down

# Stop and remove all containers, networks, and volumes
docker compose down -v

# Rebuild after changing Dockerfile or source code
docker compose up --build

# Scale eval workers (horizontal scaling)
docker compose up -d --scale lantern-eval=3
```

## Service Commands

Run arbitrary commands inside a service container (useful for debugging):

```bash
# Run a shell inside the query service
docker compose run --rm lantern-query sh
```

## Data Persistence

Named volumes keep data across container restarts:

- `redis-data` — Redis persistence
- `postgres-data` — PostgreSQL database
- `minio-data` — MinIO S3 objects (snapshots + archives)
- `writer-data` — Writer's DuckDB hot store file

## Troubleshooting

### Services won't start

Check that no other services are using the required ports (8000-8003, 9000-9001,
6379, 5432, 9090-9093):

```bash
docker compose ps
```

### MinIO bucket not created

The `minio-init` container creates the `lantern` bucket once and exits. If it
fails, the application services will also fail:

```bash
docker compose logs minio-init
```

### Writer DuckDB file corruption

Delete the writer data volume to start fresh:

```bash
docker compose down -v
docker compose up -d
```

### Out of memory

Reduce eval concurrency or limit the number of running services. The default
eval concurrency is 4; set `LANTERN_EVAL_CONCURRENCY` to a lower value if needed.
