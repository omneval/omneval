# Trace Ingestion

This document covers how to send traces to the Omneval Ingest API, including authentication, project scoping, and endpoint details.

## Sending Traces

The Ingest API (port `8000`) provides two endpoints for sending trace spans. Both require an API key for authentication.

| Endpoint | Method | Content Types | Description |
|----------|--------|---------------|-------------|
| `/v1/traces` | POST | `application/x-protobuf`, `application/json` | OTLP trace ingest — compatible with any OpenTelemetry SDK |
| `/v1/traces` | POST | `application/x-protobuf` (gzip) | OTLP trace ingest with gzip compression |
| `/api/v1/spans` | POST | `application/json` | Native Omneval REST format — `{"spans": [...]}` |

### OTLP Endpoint (`POST /v1/traces`)

Accepts standard OpenTelemetry Protocol (OTLP/HTTP) trace exports. Both protobuf-encoded and JSON-encoded payloads are supported, with optional gzip compression via the `Content-Encoding: gzip` header.

**Request:**

```
POST /v1/traces
Content-Type: application/x-protobuf
X-API-Key: oev_proj_abcdef123...
```

**Response:** `202 Accepted` on success. The OTLP response body mirrors the content type of the request.

### Native REST Endpoint (`POST /api/v1/spans`)

Accepts a JSON body with a `spans` array. Each span follows the native Omneval schema.

**Request:**

```
POST /api/v1/spans
Content-Type: application/json
X-API-Key: oev_proj_abcdef123...

{
  "spans": [
    {
      "trace_id": "0102030405060708090a0b0c0d0e0f10",
      "span_id": "1112131415161718",
      "name": "chat.completion",
      "kind": "llm",
      "model": "gpt-4",
      "input_tokens": 120,
      "output_tokens": 340
    }
  ]
}
```

**Response:** `202 Accepted` on success.

**Span ID format:** `trace_id` must be a 32-character lowercase hex string; `span_id` must be a 16-character lowercase hex string.

> **Note:** There is no gRPC endpoint. Only HTTP is supported.

## Authentication

### X-API-Key Header

All ingest requests require the **`X-API-Key`** header. This is not the OTLP-standard `Authorization: Bearer` — although the Ingest API also accepts `Authorization: Bearer <key>` as a fallback, the canonical header is `X-API-Key`.

```
X-API-Key: oev_proj_abcdef123...
```

Requests without a valid API key receive a `401 Unauthorized` response.

When both headers are present, `X-API-Key` takes precedence.

### Configuring an OTLP SDK

To authenticate an OpenTelemetry OTLP exporter, use the `OTEL_EXPORTER_OTLP_HEADERS` environment variable:

```
OTEL_EXPORTER_OTLP_HEADERS=x-api-key=oev_proj_abcdef123...
```

## Project Model

### Server-Derived Project ID

**The project is inferred from the API key on the server — clients must not supply a `project_id` in the payload or in OTLP resource attributes.**

When a span arrives, the Ingest API:

1. Reads the API key from the `X-API-Key` header (or `Authorization: Bearer` as fallback).
2. Validates the key against the metadata store (via a cached validator).
3. Derives the `project_id` from the validated key (`vk.ProjectID`).
4. Attaches the `project_id` to every span in the batch.

A consumer who expects to pass `project_id` in the request will be confused — the field is intentionally omitted from client-side control to ensure spans always land in the correct project.

### API Key Kinds

API keys are scoped to a project and come in two kinds:

| Kind | Prefix | Scope | Description |
|------|--------|-------|-------------|
| **Project** | `oev_proj_` | Entire project | Can ingest spans for any service within the project |
| **Service** | `oev_svc_` | Single service | Can ingest spans only for the named service; the `service_name` is attached to every span |

Both key kinds have full access to the same project; the service-scoped key simply adds a `service_name` tag to each ingested span for attribution.

### Key Creation

Create an API key via the management API:

**Project key:**

```bash
POST /api/v1/projects/{project_id}/api-keys
Content-Type: application/json
Authorization: Bearer <session-token>

{"kind": "project"}
```

**Service key:**

```bash
POST /api/v1/projects/{project_id}/api-keys
Content-Type: application/json
Authorization: Bearer <session-token>

{"kind": "service", "service_name": "web-frontend"}
```

The response includes the `raw_key` (e.g., `oev_proj_abcdef123...`). **This raw key is shown only once** — it cannot be retrieved later. Store it securely.

The server stores only a SHA-256 hash of the key.

### Key Revocation

Revoke a key via the management API:

```bash
DELETE /api/v1/projects/{project_id}/api-keys/{key_id}
Authorization: Bearer <session-token>
```

Revoked keys are rejected immediately. A short-lived in-memory cache (60-second TTL) may allow a revoked key to be accepted for up to 60 seconds after revocation.

## OTLP Configuration Example

To wire an OpenTelemetry instrumented application to Omneval, set these environment variables:

```bash
OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
OTEL_EXPORTER_OTLP_ENDPOINT=http://ingest-host:8000
OTEL_EXPORTER_OTLP_HEADERS=x-api-key=oev_proj_abcdef123...
```

The SDK appends `/v1/traces` to the endpoint URL automatically. With `http/protobuf`, the exporter sends gzip-compressed protobuf payloads by default (the Ingest API decompresses these transparently).

For JSON-based export:

```bash
OTEL_EXPORTER_OTLP_PROTOCOL=http/json
OTEL_EXPORTER_OTLP_ENDPOINT=http://ingest-host:8000
OTEL_EXPORTER_OTLP_HEADERS=x-api-key=oev_proj_abcdef123...
```

### Complete Example with Service Name

For an application using a service-scoped key:

```bash
OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
OTEL_EXPORTER_OTLP_ENDPOINT=http://ingest-host:8000
OTEL_EXPORTER_OTLP_HEADERS=x-api-key=oev_svc_abcdef123...
OTEL_SERVICE_NAME=web-frontend
```

The `service_name` from the validated key takes priority over any `OTEL_SERVICE_NAME` override when resolving span attribution.

## Error Responses

| Status | Condition |
|--------|-----------|
| `401 Unauthorized` | Missing or invalid API key |
| `400 Bad Request` | Invalid payload, unsupported content type, or malformed span data |
| `405 Method Not Allowed` | Non-POST request to ingest endpoints |
| `503 Service Unavailable` | Redis enqueue failure |

## Response Codes

Successful ingest always returns `202 Accepted`. The response body mirrors the content type of the request (for OTLP) or is empty (for native REST).
