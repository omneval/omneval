# Omneval Python SDK

Python SDK for Omneval LLM tracing and evaluation.

## Installation

```bash
pip install omneval-sdk
```

## Quick Start

```python
import omneval_sdk

# Configure the SDK at application startup
omneval_sdk.configure(
    endpoint="http://localhost:4318",  # Omneval ingest endpoint
    api_key="oev_proj_your_api_key",
)

# Decorate functions to trace them
@omneval_sdk.trace
def call_llm(prompt):
    span = omneval_sdk.get_active_span()
    omneval_sdk.set_model(span, "gpt-4o")
    omneval_sdk.set_input(span, prompt)
    omneval_sdk.set_output(span, "Hello!")
    return "done"

result = call_llm("What is 2+2?")
```

## API Reference

### `omneval_sdk.configure(endpoint, api_key)`

Configure the OTLP HTTP exporter pointing at the Omneval ingest API. Must be called once at startup.

### `@omneval_sdk.trace`

Decorator that wraps a function in an OTel span sent to Omneval. Nested decorated functions produce correctly linked parent-child span trees.

### `omneval_sdk.set_input(span, input_value)`

Attach input data to a span as the `omneval.input` attribute.

### `omneval_sdk.set_output(span, output_value)`

Attach output data to a span as the `omneval.output` attribute.

### `omneval_sdk.set_model(span, model)`

Attach the model name to a span as the `gen_ai.request.model` attribute.

### `omneval_sdk.set_tokens(span, input_tokens, output_tokens)`

Attach token counts to a span as `gen_ai.usage.input_tokens` and `gen_ai.usage.output_tokens`.

### `omneval_sdk.get_active_span()`

Return the currently active span from the context variable. Returns `None` outside a decorated function.

### `omneval_sdk.OmnevalClient(base_url, api_key, project_id=None)`

HTTP client for prompt fetching and manual score writes.

The `project_id` is automatically extracted from the API key suffix
(e.g. `oev_proj_my-project` → `my-project`). Pass `project_id` explicitly
to override this behavior.

```python
client = omneval_sdk.OmnevalClient("http://localhost:8080", "oev_proj_my-project")
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

All key helper functions are exported from the top-level `omneval_sdk` package:

```python
from omneval_sdk import (
    trace,           # @trace decorator
    configure,       # configure endpoint
    OmnevalClient,   # HTTP client
    set_input,       # set input on span
    set_output,      # set output on span
    set_model,       # set model on span
    set_tokens,      # set token counts on span
    get_active_span, # get current span
)
```
