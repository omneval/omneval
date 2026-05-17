# Lantern Python SDK

Python SDK for Lantern LLM tracing and evaluation.

## Installation

```bash
pip install lantern-sdk
```

## Quick Start

```python
import lantern_sdk

# Configure the SDK at application startup
lantern_sdk.configure(
    endpoint="http://localhost:4318",  # Lantern ingest endpoint
    api_key="ltn_proj_your_api_key",
)

# Decorate functions to trace them
@lantern_sdk.trace
def call_llm(prompt):
    span = lantern_sdk.get_active_span()
    lantern_sdk.set_model(span, "gpt-4o")
    lantern_sdk.set_input(span, prompt)
    lantern_sdk.set_output(span, "Hello!")
    return "done"

result = call_llm("What is 2+2?")
```

## API Reference

### `lantern_sdk.configure(endpoint, api_key)`

Configure the OTLP HTTP exporter pointing at the Lantern ingest API. Must be called once at startup.

### `@lantern_sdk.trace`

Decorator that wraps a function in an OTel span sent to Lantern. Nested decorated functions produce correctly linked parent-child span trees.

### `lantern_sdk.set_input(span, input_value)`

Attach input data to a span as the `lantern.input` attribute.

### `lantern_sdk.set_output(span, output_value)`

Attach output data to a span as the `lantern.output` attribute.

### `lantern_sdk.set_model(span, model)`

Attach the model name to a span as the `gen_ai.request.model` attribute.

### `lantern_sdk.set_tokens(span, input_tokens, output_tokens)`

Attach token counts to a span as `gen_ai.usage.input_tokens` and `gen_ai.usage.output_tokens`.

### `lantern_sdk.get_active_span()`

Return the currently active span from the context variable. Returns `None` outside a decorated function.

### `lantern_sdk.LanternClient(base_url, api_key, project_id=None)`

HTTP client for prompt fetching and manual score writes.

The `project_id` is automatically extracted from the API key suffix
(e.g. `ltn_proj_my-project` → `my-project`). Pass `project_id` explicitly
to override this behavior.

```python
client = lantern_sdk.LanternClient("http://localhost:8080", "ltn_proj_my-project")
prompt = client.get_prompt("greeting", "production")
client.write_score("span-id-123", "helpfulness", 0.8, "Good reasoning")
```

### `client.write_score(span_id, eval_name, value, reasoning="")`

Submit a manual score for a span. The `project_id` from the client
configuration is automatically included in the request.

**Args:**
- `span_id` (str, required): The span ID to score.
- `eval_name` (str): Name of the evaluation (e.g. "helpfulness", "correctness").
- `value` (float): Numeric score value (typically 0.0–1.0).
- `reasoning` (str, optional): Human-readable reasoning for the score.

**Returns:** `None` on success (201 Created).

**Raises:**
- `ValueError`: If `span_id` is empty.
- `requests.HTTPError`: If the server returns an error (e.g. 400 Bad Request,
  500 Internal Server Error).

```python
client.write_score(
    span_id="span-abc123",
    eval_name="correctness",
    value=0.9,
    reasoning="The answer correctly identified the entity",
)
```

## Exports

All key helper functions are exported from the top-level `lantern_sdk` package:

```python
from lantern_sdk import (
    trace,           # @trace decorator
    configure,       # configure endpoint
    LanternClient,   # HTTP client
    set_input,       # set input on span
    set_output,      # set output on span
    set_model,       # set model on span
    set_tokens,      # set token counts on span
    get_active_span, # get current span
)
```
