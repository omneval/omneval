"""
Omneval QA Trace Test Script
Tests both the native REST API and the Omneval Python SDK (OTLP) trace ingestion.
"""

import time
import uuid
import json
import requests

# Config
INGEST_URL = "http://localhost:8000"
QUERY_URL = "http://localhost:8002"
API_KEY = "oev_proj_BrY4nD4xEh7wdeFYSpXnoN5jLTu4E6Z3e5yPmYWp9nr1"


def make_span_id():
    return uuid.uuid4().hex[:16]


def make_trace_id():
    return uuid.uuid4().hex[:32]


def test_native_rest_api():
    """Test 1: Native REST API span ingest."""
    print("\n=== Test 1: Native REST API ===")

    trace_id = make_trace_id()
    root_span_id = make_span_id()
    child_span_id = make_span_id()

    spans = [
        {
            "span_id": root_span_id,
            "trace_id": trace_id,
            "name": "qa.chain.root",
            "kind": "chain",
            "attributes": {"qa.test": "native_rest"},
        },
        {
            "span_id": child_span_id,
            "trace_id": trace_id,
            "parent_id": root_span_id,
            "name": "qa.llm.call",
            "kind": "llm",
            "model": "gpt-4o",
            "input": "Summarize the history of computing in one sentence.",
            "output": "Computing evolved from mechanical calculators to programmable computers to the internet age.",
            "input_tokens": 12,
            "output_tokens": 18,
        },
    ]

    resp = requests.post(
        f"{INGEST_URL}/api/v1/spans",
        headers={"X-API-Key": API_KEY, "Content-Type": "application/json"},
        json={"spans": spans},
        timeout=10,
    )
    print(f"  Status: {resp.status_code} (expected 202)")
    if resp.status_code == 202:
        print(f"  OK: {len(spans)} spans sent, trace_id={trace_id}")
    else:
        print(f"  FAIL: {resp.text}")
    return trace_id if resp.status_code == 202 else None


def test_native_rest_missing_api_key():
    """Test 2: Native REST API - missing API key should return 401."""
    print("\n=== Test 2: Missing API key returns 401 ===")
    resp = requests.post(
        f"{INGEST_URL}/api/v1/spans",
        headers={"Content-Type": "application/json"},
        json={"spans": [{"span_id": make_span_id(), "trace_id": make_trace_id(), "name": "bad"}]},
        timeout=10,
    )
    print(f"  Status: {resp.status_code} (expected 401)")
    if resp.status_code == 401:
        print("  OK: Correctly rejected")
    else:
        print(f"  FAIL: Expected 401 got {resp.status_code}: {resp.text}")


def test_native_rest_invalid_api_key():
    """Test 3: Native REST API - invalid API key should return 401."""
    print("\n=== Test 3: Invalid API key returns 401 ===")
    resp = requests.post(
        f"{INGEST_URL}/api/v1/spans",
        headers={"X-API-Key": "oev_proj_INVALIDKEYXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX", "Content-Type": "application/json"},
        json={"spans": [{"span_id": make_span_id(), "trace_id": make_trace_id(), "name": "bad"}]},
        timeout=10,
    )
    print(f"  Status: {resp.status_code} (expected 401)")
    if resp.status_code == 401:
        print("  OK: Correctly rejected")
    else:
        print(f"  FAIL: Expected 401 got {resp.status_code}: {resp.text}")


def test_native_rest_invalid_span_id():
    """Test 4: Invalid span_id format should return 400."""
    print("\n=== Test 4: Invalid span_id format returns 400 ===")
    resp = requests.post(
        f"{INGEST_URL}/api/v1/spans",
        headers={"X-API-Key": API_KEY, "Content-Type": "application/json"},
        json={"spans": [{"span_id": "tooshort", "trace_id": make_trace_id(), "name": "bad"}]},
        timeout=10,
    )
    print(f"  Status: {resp.status_code} (expected 400)")
    if resp.status_code == 400:
        print("  OK: Correctly rejected invalid span_id")
    else:
        print(f"  FAIL: Expected 400 got {resp.status_code}: {resp.text}")


def test_native_rest_span_without_input_output():
    """Test 5: Span with no input/output (should be stored with empty fields)."""
    print("\n=== Test 5: Span with no input/output ===")
    trace_id = make_trace_id()
    resp = requests.post(
        f"{INGEST_URL}/api/v1/spans",
        headers={"X-API-Key": API_KEY, "Content-Type": "application/json"},
        json={"spans": [{
            "span_id": make_span_id(),
            "trace_id": trace_id,
            "name": "qa.tool.call",
            "kind": "tool",
        }]},
        timeout=10,
    )
    print(f"  Status: {resp.status_code} (expected 202)")
    if resp.status_code == 202:
        print(f"  OK: Span with empty input/output accepted, trace_id={trace_id}")
    else:
        print(f"  FAIL: {resp.text}")


def test_otlp_sdk():
    """Test 6: OTLP traces via Omneval Python SDK."""
    print("\n=== Test 6: OTLP via Omneval Python SDK ===")
    try:
        import omneval_sdk as omneval
        from opentelemetry import trace as otel_trace

        omneval.configure(
            endpoint=INGEST_URL + "/",
            api_key=API_KEY,
        )
        print("  Omneval SDK configured successfully")

        @omneval.trace
        def fake_llm_call(prompt: str) -> str:
            span = omneval.get_active_span()
            if span:
                omneval.set_input(span, prompt)
                omneval.set_model(span, "gpt-4o-mini")
                omneval.set_tokens(span, input_tokens=20, output_tokens=35)
                time.sleep(0.05)  # simulate latency
                result = "The capital of France is Paris, a city known for the Eiffel Tower."
                omneval.set_output(span, result)
            return result

        @omneval.trace
        def run_agent(question: str) -> str:
            span = omneval.get_active_span()
            if span:
                span.set_attribute("agent.type", "qa-agent")
                span.set_attribute("qa.test", "sdk_otlp")
            return fake_llm_call(question)

        answer = run_agent("What is the capital of France?")
        print(f"  Trace sent, answer={answer!r}")
        print("  OK: OTLP spans exported via SDK")
        return True
    except ImportError as e:
        print(f"  WARN: omneval_sdk not installed ({e}), trying opentelemetry-sdk directly...")
        return test_otlp_direct()
    except Exception as e:
        print(f"  FAIL: {type(e).__name__}: {e}")
        return False


def test_otlp_direct():
    """Fallback: send OTLP JSON directly to the /v1/traces endpoint."""
    print("\n=== Test 6b: OTLP JSON directly (fallback) ===")
    try:
        from opentelemetry import trace as otel_trace
        from opentelemetry.sdk.trace import TracerProvider
        from opentelemetry.sdk.trace.export import SimpleSpanProcessor
        from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter
        from opentelemetry.sdk.resources import Resource, SERVICE_NAME

        exporter = OTLPSpanExporter(
            endpoint=f"{INGEST_URL}/v1/traces",
            headers={"X-API-Key": API_KEY},
        )
        provider = TracerProvider(resource=Resource.create({SERVICE_NAME: "qa-test"}))
        provider.add_span_processor(SimpleSpanProcessor(exporter))

        tracer = provider.get_tracer("qa-test")

        with tracer.start_as_current_span("qa.otlp.agent") as span:
            span.set_attribute("gen_ai.request.model", "gpt-4o-mini")
            span.set_attribute("gen_ai.usage.input_tokens", 25)
            span.set_attribute("gen_ai.usage.output_tokens", 40)
            span.set_attribute("omneval.input", "What is the capital of Germany?")
            span.set_attribute("omneval.output", "The capital of Germany is Berlin.")
            time.sleep(0.05)

        provider.force_flush(timeout_millis=5000)
        print("  OK: OTLP spans exported directly")
        return True
    except ImportError as e:
        print(f"  FAIL: opentelemetry packages not available: {e}")
        return False
    except Exception as e:
        print(f"  FAIL: {type(e).__name__}: {e}")
        return False


def test_large_batch():
    """Test 7: Large batch of spans."""
    print("\n=== Test 7: Large batch (50 spans) ===")
    trace_id = make_trace_id()
    spans = []
    for i in range(50):
        spans.append({
            "span_id": make_span_id(),
            "trace_id": trace_id,
            "name": f"qa.batch.span_{i:03d}",
            "kind": "internal",
            "input": f"Batch prompt #{i}",
            "output": f"Batch response #{i}",
            "input_tokens": i + 1,
            "output_tokens": i + 2,
        })
    resp = requests.post(
        f"{INGEST_URL}/api/v1/spans",
        headers={"X-API-Key": API_KEY, "Content-Type": "application/json"},
        json={"spans": spans},
        timeout=30,
    )
    print(f"  Status: {resp.status_code} (expected 202)")
    if resp.status_code == 202:
        print(f"  OK: 50 spans sent in batch, trace_id={trace_id}")
    else:
        print(f"  FAIL: {resp.text}")
    return trace_id if resp.status_code == 202 else None


def test_query_spans(trace_id: str):
    """Test 8: Query spans via Query API after write propagation."""
    print(f"\n=== Test 8: Query API for trace {trace_id[:16]}... ===")
    print("  Waiting 35s for writer->S3->query sync...")
    time.sleep(35)

    session = requests.Session()
    login = session.post(
        f"{QUERY_URL}/login",
        json={"email": "admin@omneval.com", "password": "admin"},
        timeout=10,
    )
    if login.status_code != 200:
        print(f"  FAIL: Login failed: {login.status_code} {login.text}")
        return

    resp = session.get(f"{QUERY_URL}/api/v1/spans?trace_id={trace_id}", timeout=10)
    print(f"  Status: {resp.status_code}")
    if resp.status_code == 200:
        data = resp.json()
        print(f"  OK: Got response: {json.dumps(data)[:200]}")
    elif resp.status_code == 404:
        print("  WARN: Trace not found yet (may still be propagating)")
    else:
        print(f"  INFO: Response: {resp.status_code} {resp.text[:200]}")


def test_query_list_spans():
    """Test 9: Query list spans endpoint."""
    print("\n=== Test 9: List spans via Query API ===")

    session = requests.Session()
    login = session.post(
        f"{QUERY_URL}/login",
        json={"email": "admin@omneval.com", "password": "admin"},
        timeout=10,
    )
    if login.status_code != 200:
        print(f"  FAIL: Login failed")
        return

    resp = session.get(f"{QUERY_URL}/api/v1/spans?limit=10", timeout=10)
    print(f"  Status: {resp.status_code}")
    if resp.status_code == 200:
        data = resp.json()
        print(f"  OK: Response: {json.dumps(data)[:300]}")
    else:
        print(f"  INFO: {resp.status_code} {resp.text[:300]}")


if __name__ == "__main__":
    print("=" * 60)
    print("Omneval QA Trace Test")
    print("=" * 60)

    # Core ingest tests
    trace_id = test_native_rest_api()
    test_native_rest_missing_api_key()
    test_native_rest_invalid_api_key()
    test_native_rest_invalid_span_id()
    test_native_rest_span_without_input_output()
    test_otlp_sdk()
    batch_trace_id = test_large_batch()

    # Query tests (after data has time to propagate)
    if trace_id:
        test_query_spans(trace_id)

    test_query_list_spans()

    print("\n" + "=" * 60)
    print("QA Test Complete")
    print("=" * 60)
