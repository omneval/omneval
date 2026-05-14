"""
QA Test Script: Send traces to Lantern via multiple methods.
Tests both the native REST API and the OTLP/OTel path.
"""
import json
import time
import uuid
import sys

try:
    import requests
except ImportError:
    print("ERROR: requests not installed. Run: pip install requests")
    sys.exit(1)

API_KEY = "ltn_proj_83JH6C61EWBw7KAGDkpKw6X7eqy8fHLZeT3XaHp7o2CH"
INGEST_URL = "http://localhost:8000"
PROJECT_ID = "d81rkuaodocs73apq400"

print("=" * 60)
print("Lantern QA Test Script")
print("=" * 60)

# ── Test 1: Native REST API ────────────────────────────────────
print("\n[Test 1] Native REST API - POST /api/v1/spans")

def make_span(name, model="gpt-4o", kind="llm"):
    span_id = uuid.uuid4().hex[:16]
    trace_id = uuid.uuid4().hex[:32]
    return {
        "span_id": span_id,
        "trace_id": trace_id,
        "name": name,
        "kind": kind,
        "model": model,
        "input": f"User asked: what is {name}?",
        "output": f"Assistant responded about {name}.",
        "input_tokens": 25,
        "output_tokens": 50,
        "attributes": {
            "env": "qa-test",
            "test_run": "2026-05-13",
        },
    }

spans = [
    make_span("qa-test-llm-call-1", model="gpt-4o"),
    make_span("qa-test-llm-call-2", model="gpt-4o-mini"),
    make_span("qa-test-agent-step", kind="agent"),
    make_span("qa-test-tool-call", kind="tool"),
    make_span("qa-test-chain-step", kind="chain"),
]

resp = requests.post(
    f"{INGEST_URL}/api/v1/spans",
    headers={"X-API-Key": API_KEY, "Content-Type": "application/json"},
    json={"spans": spans},
    timeout=10,
)
print(f"  Status: {resp.status_code}")
if resp.status_code == 202:
    print(f"  SUCCESS: {len(spans)} spans enqueued")
    for s in spans:
        print(f"    - span_id={s['span_id']} trace_id={s['trace_id']} name={s['name']} kind={s['kind']}")
else:
    print(f"  FAILED: {resp.text}")

# ── Test 2: OTLP HTTP ──────────────────────────────────────────
print("\n[Test 2] OTLP HTTP - POST /v1/traces")

try:
    from opentelemetry.sdk.trace import TracerProvider
    from opentelemetry.sdk.trace.export import SimpleSpanProcessor
    from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter
    from opentelemetry.sdk.resources import Resource, SERVICE_NAME
    from opentelemetry import trace as otel_trace

    resource = Resource.create({SERVICE_NAME: "lantern-qa-test"})
    provider = TracerProvider(resource=resource)
    exporter = OTLPSpanExporter(
        endpoint=f"{INGEST_URL}/v1/traces",
        headers={"X-API-Key": API_KEY},
    )
    provider.add_span_processor(SimpleSpanProcessor(exporter))
    tracer = provider.get_tracer("lantern-qa")

    with tracer.start_as_current_span("otlp-qa-root-span") as root:
        root.set_attribute("gen_ai.request.model", "claude-3-5-sonnet")
        root.set_attribute("gen_ai.usage.input_tokens", 100)
        root.set_attribute("gen_ai.usage.output_tokens", 200)
        root.set_attribute("gen_ai.prompt.0.role", "user")
        root.set_attribute("gen_ai.prompt.0.content", "Hello from QA test via OTLP")
        root.set_attribute("gen_ai.completion.0.role", "assistant")
        root.set_attribute("gen_ai.completion.0.content", "OTLP trace received successfully")

        with tracer.start_as_current_span("otlp-qa-child-span") as child:
            child.set_attribute("gen_ai.request.model", "claude-3-haiku")
            child.set_attribute("gen_ai.usage.input_tokens", 50)
            child.set_attribute("gen_ai.usage.output_tokens", 30)

    provider.shutdown()
    print("  SUCCESS: OTLP spans exported (check ingest logs for confirmation)")

except ImportError as e:
    print(f"  SKIPPED: opentelemetry not installed ({e})")
    print("  Install with: pip install opentelemetry-sdk opentelemetry-exporter-otlp-proto-http")

# ── Test 3: Lantern Python SDK ─────────────────────────────────
print("\n[Test 3] Lantern Python SDK")

try:
    import lantern_sdk

    lantern_sdk.configure(endpoint=INGEST_URL, api_key=API_KEY)

    @lantern_sdk.trace
    def sdk_outer_function():
        span = lantern_sdk.get_active_span()
        if span:
            lantern_sdk.set_model(span, "gpt-4o")
            lantern_sdk.set_input(span, "QA test input via SDK")
            lantern_sdk.set_output(span, "QA test output via SDK")
            lantern_sdk.set_tokens(span, 75, 125)
        return sdk_inner_function()

    @lantern_sdk.trace
    def sdk_inner_function():
        span = lantern_sdk.get_active_span()
        if span:
            lantern_sdk.set_model(span, "gpt-4o-mini")
            lantern_sdk.set_input(span, "Inner function input")
            lantern_sdk.set_output(span, "Inner function output")
        return "inner result"

    result = sdk_outer_function()
    print(f"  SUCCESS: SDK traces sent, result={result!r}")

except ImportError as e:
    print(f"  SKIPPED: lantern_sdk not installed ({e})")
    print("  Install with: pip install -e sdk/python")

# ── Test 4: Auth error (no API key) ───────────────────────────
print("\n[Test 4] Auth rejection - no API key")
resp = requests.post(
    f"{INGEST_URL}/api/v1/spans",
    headers={"Content-Type": "application/json"},
    json={"spans": [make_span("should-fail")]},
    timeout=10,
)
if resp.status_code == 401:
    print(f"  SUCCESS: Correctly rejected with 401 ({resp.text.strip()})")
else:
    print(f"  UNEXPECTED: Got {resp.status_code} - {resp.text}")

# ── Test 5: Invalid span_id ────────────────────────────────────
print("\n[Test 5] Validation - invalid span_id length")
bad_span = make_span("bad-span")
bad_span["span_id"] = "tooshort"
resp = requests.post(
    f"{INGEST_URL}/api/v1/spans",
    headers={"X-API-Key": API_KEY, "Content-Type": "application/json"},
    json={"spans": [bad_span]},
    timeout=10,
)
if resp.status_code == 400:
    print(f"  SUCCESS: Correctly rejected with 400 ({resp.text.strip()})")
else:
    print(f"  UNEXPECTED: Got {resp.status_code} - {resp.text}")

print("\n" + "=" * 60)
print("QA Test Complete. Wait ~30s then check the Lantern UI for traces.")
print("=" * 60)
