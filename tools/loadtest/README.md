# loadtest

A small load generator for the Ingest API, used to measure end-to-end span
throughput and to observe the Writer's batched Lake-commit window
(`batchFlushInterval` / `batchMaxBytes` in
`services/writer/internal/pipeline/pipeline.go`).

It sends synthetic spans to `POST /api/v1/spans` at a configurable
concurrency/rate/batch size, then reports:

- Ingest API throughput (requests/sec, spans/sec accepted, latency)
- Writer Lake-commit stats scraped from `:9091/metrics` (spans/sec actually
  committed, commits/sec, avg spans per commit, avg commit duration) — the
  numbers that matter for an apples-to-apples comparison with
  Langfuse/ClickHouse.

## Setup

Start the stack (from `deploy/docker-compose/`):

```bash
docker compose up -d
```

Create a project and API key (one-time):

```bash
# 1. Log in as the bootstrapped admin (see OMNEVAL_AUTH_ADMIN_EMAIL/PASSWORD
#    in docker-compose.yml) and save the session cookie.
curl -sc cookies.txt -X POST http://localhost:8002/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@omneval.com","password":"admin"}'

# 2. Create a project.
PROJECT_ID=$(curl -sb cookies.txt -X POST http://localhost:8002/api/v1/projects \
  -H 'Content-Type: application/json' \
  -d '{"name":"loadtest"}' | jq -r .project_id)

# 3. Generate a project-scoped API key.
API_KEY=$(curl -sb cookies.txt -X POST http://localhost:8002/api/v1/projects/$PROJECT_ID/api-keys \
  -H 'Content-Type: application/json' \
  -d '{"kind":"project"}' | jq -r .raw_key)

echo $API_KEY
```

## Run

```bash
cd tools/loadtest
go run . \
  -url http://localhost:8000 \
  -api-key "$API_KEY" \
  -duration 60s \
  -concurrency 20 \
  -batch-size 50 \
  -payload-bytes 500 \
  -writer-metrics-url http://localhost:9091/metrics
```

### Flags

| Flag | Default | Meaning |
| --- | --- | --- |
| `-url` | `http://localhost:8000` | Ingest API base URL |
| `-api-key` | (required) | `X-API-Key` for the target project |
| `-duration` | `30s` | How long to run |
| `-concurrency` | `20` | Concurrent sender goroutines |
| `-batch-size` | `50` | Spans per `POST /api/v1/spans` request |
| `-payload-bytes` | `500` | Size of each span's `input`/`output` text — use this to push the Writer's 16MB batch-size threshold |
| `-rate` | `0` (unlimited) | Target spans/sec across all workers; 0 sends as fast as possible |
| `-writer-metrics-url` | `http://localhost:9091/metrics` | Writer Service metrics endpoint (set to `""` to skip) |

## Interpreting results

The Ingest API accepts a batch (HTTP 202) as soon as it's staged to the
Ingest Buffer and queued — that's not the same as being committed to the
Lake. The "Writer Lake commit stats" section is the number to compare
against Langfuse/ClickHouse ingest throughput, since it reflects spans that
have actually landed in DuckLake and are queryable.

A few things worth varying:

- **`-rate`** below the Writer's max commit rate: commits should land on
  the ~5s timer (`batchFlushInterval`), each containing roughly
  `rate * 5s` spans.
- **`-payload-bytes` / `-batch-size`** high enough that one window's worth
  of spans exceeds 16MB (`batchMaxBytes`): commits should start landing
  faster than every 5s, bounded by Lake commit latency instead.
- **`-concurrency`**: increase until Ingest API latency or error rate
  degrades, to find the saturation point of the whole pipeline (Redis →
  Ingest Buffer → Writer → Quack).

Run multiple Writer replicas to see how commit throughput scales — the
single-replica Quack Server (ADR-0005) is expected to become the bottleneck
as the number of concurrent committers increases.
