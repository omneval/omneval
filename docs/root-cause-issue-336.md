# Root Cause Analysis: Empty LLM Message Content on Ingested Spans (Issue #336)

## Problem

LLM spans ingested via the Python SDK (and OTLP / native REST paths) render **empty** `Input` and `Output` fields in the trace detail UI. A span wrapping a 14,865-token LLM call shows:

```
Input:  [{"content": "", "role": "user"}]
Output: [{"content": "", "role": "assistant"}]
```

The span's raw attributes contain `gen_ai.response.id`, `gen_ai.tool.definitions`, and `llm.usage.total_tokens`, but no prompt/completion message text anywhere.

## Root Cause

Two independent bugs in the pipeline together produce this effect:

### Bug 1: OTLP Normalizer Only Reads Span Attributes, Not Span Events

**Location**: `internal/normalizer/otlp.go` (function `otlpSpanToRawMap`)

**Cause**: The normalizer reads message content exclusively from span **attributes** (`gen_ai.prompt.message.*` and `gen_ai.completion.message.*` attribute keys). However, many OpenTelemetry instrumentors (OpenAI, LiteLLM, Laminar) emit prompt/completion content in **span events** per the GenAI semantic conventions:

- `SpanEvent` with name `gen_ai.prompt.message` — contains `gen_ai.prompt.message.role` and `gen_ai.prompt.message.content` attributes
- `SpanEvent` with name `gen_ai.completion.message` — contains `gen_ai.completion.message.role` and `gen_ai.completion.message.content` attributes

When a span has no attribute-level `gen_ai.prompt.*` or `gen_ai.completion.*` keys, the normalizer returns `input` and `output` as empty JSON arrays (`[]`).

**Fix**: Added `buildMessagesFromEvents()` fallback that extracts message content from span events when attribute-based content is absent. This is the correct fallback because the OpenTelemetry GenAI semantic conventions define message content as events.

### Bug 2: DuckDB JSON Column Type Causes Garbled Output in Query API

**Location**: `services/query/internal/query/query.go` (SQL templates `LakeTraceSpansSQL` and `LakeTraceDetailSQL`)

**Cause**: When a DuckDB column is declared as type `JSON`, the Go driver (`github.com/marcboeker/go-duckdb`) **parses the JSON at read time** and returns a `[]interface{}` (Go map), not the original JSON string. The `strVal()` function in the query package uses `fmt.Sprintf("%v", v)` to convert values to strings:

- For a `string`: `fmt.Sprintf("%v", "raw json string")` → `"raw json string"` ✓
- For a `[]interface{}`: `fmt.Sprintf("%v", map[string]interface{}{...})` → `"[map[content:X role:Y]]"` ✗

This garbled string is what the Query API serializes into the JSON response and the UI displays.

**Fix**: Added `CAST(input AS VARCHAR)` and `CAST(output AS VARCHAR)` to the `LakeTraceSpansSQL` and `LakeTraceDetailSQL` query templates, forcing DuckDB to return the raw JSON string instead of the parsed value.

### Why the Native REST Ingest Path Was Also Affected

The native REST ingest handler (`internal/handlers/native.go`) stores message content as JSON-serialized strings in the `input`/`output` columns. When read back via the query API, the same DuckDB JSON-column bug applies: the column is declared `JSON` in the DDL, so the Go driver parses it on every read.

The CAST fix resolves this for **both** the OTLP and native REST ingest paths.

## Affected Paths

| Path | Bug 1 (Event Extraction) | Bug 2 (DuckDB CAST) |
|------|--------------------------|----------------------|
| OTLP ingest (protobuf/JSON via `/v1/traces`) | YES — message content in events, not attributes | YES — parsed JSON returns garbled string |
| Native REST ingest (via `/api/v1/ingest/traces`) | NO — content passes through as-is | YES — parsed JSON returns garbled string |

Both bugs must be fixed for the UI to show non-empty content regardless of ingest path.

## Fix Summary

1. **`internal/normalizer/otlp.go`**: Added `buildMessagesFromEvents()` that iterates span events looking for `gen_ai.prompt.message` and `gen_ai.completion.message` event names, extracting `role` and `content` from their attributes. Used as a fallback when attribute-based content is empty.

2. **`services/query/internal/query/query.go`**: Added `CAST(input AS VARCHAR)` and `CAST(output AS VARCHAR)` to all SQL queries that read the `input` and `output` columns from the `lake.spans` table. Root cause documented in a code comment on `strVal()`.

3. **Tests**: Roundtrip integration tests for both OTLP and native REST ingest paths, plus a handler-level test verifying the trace detail API returns non-empty input/output for LLM spans.

## References

- Issue: #336
- OpenTelemetry GenAI Semantic Conventions: https://opentelemetry.io/docs/specs/semconv/genai/