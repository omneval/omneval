"""
Omneval QA test script — sends OTLP traces to test the ingest pipeline end-to-end.
Tests both the Omneval Python SDK and raw OTLP HTTP export.
"""

import time
import json
import hashlib
import random
import requests
from opentelemetry import trace
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import SimpleSpanProcessor, BatchSpanProcessor
from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter
from opentelemetry.sdk.resources import Resource
from opentelemetry.semconv.resource import ResourceAttributes

INGEST_URL = "http://localhost:8000"
API_KEY = "oev_proj_TestKey123456789ABCDEFGHIJKLMNOPQRSTUVWXYZa"
PROJECT_ID = "test-project-001"

# ─────────────────────────────────────────────────────────────────────────────
# Test 1: Raw REST span (Omneval native format)
# ─────────────────────────────────────────────────────────────────────────────
def test_native_rest():
    print("\n=== Test 1: Native REST span ingestion ===")
    trace_id = hashlib.sha256(f"test-trace-{time.time()}".encode()).hexdigest()[:32]
    span_id = hashlib.sha256(f"test-span-{time.time()}".encode()).hexdigest()[:16]

    span = {
        "span_id": span_id,
        "trace_id": trace_id,
        "name": "qa-test-native-span",
        "kind": "llm",
        "model": "gpt-4o-mini",
        "input": json.dumps([{"role": "user", "content": "Hello, world!"}]),
        "output": json.dumps([{"role": "assistant", "content": "Hi there! How can I help?"}]),
        "input_tokens": 12,
        "output_tokens": 8,
        "attributes": {"test.run": "qa-validation", "environment": "local"},
        "start_time": int((time.time() - 0.5) * 1e9),
        "end_time": int(time.time() * 1e9),
    }

    resp = requests.post(
        f"{INGEST_URL}/api/v1/spans",
        headers={"X-API-Key": API_KEY, "Content-Type": "application/json"},
        json={"spans": [span]},
        timeout=10,
    )
    print(f"  Status: {resp.status_code}")
    print(f"  Body:   {resp.text[:200]}")
    return resp.status_code == 202


# ─────────────────────────────────────────────────────────────────────────────
# Test 2: OTLP HTTP (OpenTelemetry standard export)
# ─────────────────────────────────────────────────────────────────────────────
def test_otlp():
    print("\n=== Test 2: OTLP HTTP span ingestion ===")

    resource = Resource.create({
        ResourceAttributes.SERVICE_NAME: "omneval-qa-test",
        "test.type": "otlp-validation",
    })

    exporter = OTLPSpanExporter(
        endpoint=f"{INGEST_URL}/v1/traces",
        headers={"X-API-Key": API_KEY},
    )

    provider = TracerProvider(resource=resource)
    provider.add_span_processor(SimpleSpanProcessor(exporter))
    tracer = provider.get_tracer("omneval-qa")

    with tracer.start_as_current_span("qa-otlp-parent-span") as parent:
        parent.set_attribute("gen_ai.request.model", "gpt-4o-mini")
        parent.set_attribute("gen_ai.usage.input_tokens", 25)
        parent.set_attribute("gen_ai.usage.output_tokens", 15)
        parent.set_attribute("gen_ai.prompt.0.role", "user")
        parent.set_attribute("gen_ai.prompt.0.content", "Summarize the OTLP spec")
        parent.set_attribute("gen_ai.completion.0.role", "assistant")
        parent.set_attribute("gen_ai.completion.0.content", "OTLP is the OpenTelemetry Protocol...")

        time.sleep(0.1)

        with tracer.start_as_current_span("qa-otlp-child-tool-span") as child:
            child.set_attribute("tool.name", "web_search")
            child.set_attribute("tool.input", "opentelemetry protocol specification")
            time.sleep(0.05)

    # Force flush
    result = provider.force_flush(timeout_millis=5000)
    print(f"  Flush result: {result}")
    return result


# ─────────────────────────────────────────────────────────────────────────────
# Test 3: Omneval Python SDK
# ─────────────────────────────────────────────────────────────────────────────
def test_omneval_sdk():
    print("\n=== Test 3: Omneval Python SDK ===")
    try:
        import sys
        sys.path.insert(0, "sdk/python")
        import omneval_sdk
        from omneval_sdk.trace import set_model, set_input, set_output, set_tokens, get_active_span

        omneval_sdk.configure(endpoint=INGEST_URL, api_key=API_KEY)

        @omneval_sdk.trace
        def call_llm(prompt: str) -> str:
            span = get_active_span()
            if span:
                set_model(span, "gpt-4o-mini")
                set_input(span, prompt)
                set_tokens(span, input_tokens=10, output_tokens=20)
            time.sleep(0.05)
            result = "This is a mock LLM response."
            if span:
                set_output(span, result)
            return result

        @omneval_sdk.trace
        def run_agent(task: str) -> str:
            response = call_llm(f"Complete this task: {task}")
            return response

        result = run_agent("Test the Omneval tracing SDK")
        print(f"  SDK result: {result}")
        return True
    except ImportError as e:
        print(f"  SDK import failed: {e}")
        return False
    except Exception as e:
        print(f"  SDK test failed: {e}")
        return False


# ─────────────────────────────────────────────────────────────────────────────
# Test 4: Multi-span trace (agent chain)
# ─────────────────────────────────────────────────────────────────────────────
def test_multi_span_trace():
    print("\n=== Test 4: Multi-span agent trace (native REST) ===")
    trace_id = hashlib.sha256(f"agent-trace-{time.time()}".encode()).hexdigest()[:32]

    spans = []
    root_span_id = hashlib.sha256(b"root").hexdigest()[:16]
    tool_span_id = hashlib.sha256(b"tool").hexdigest()[:16]
    llm_span_id = hashlib.sha256(b"llm").hexdigest()[:16]
    base_time = int(time.time() * 1e9)

    spans.append({
        "span_id": root_span_id, "trace_id": trace_id,
        "name": "agent-root", "kind": "agent",
        "input": "Summarize the latest AI news",
        "start_time": base_time, "end_time": base_time + 500_000_000,
        "attributes": {"agent.framework": "custom"},
    })
    spans.append({
        "span_id": tool_span_id, "trace_id": trace_id, "parent_id": root_span_id,
        "name": "web_search", "kind": "tool",
        "input": '{"query": "latest AI news"}',
        "output": '{"results": ["OpenAI releases GPT-5", "Google releases Gemini 2.5"]}',
        "start_time": base_time + 10_000_000, "end_time": base_time + 200_000_000,
    })
    spans.append({
        "span_id": llm_span_id, "trace_id": trace_id, "parent_id": root_span_id,
        "name": "summarize", "kind": "llm",
        "model": "gpt-4o-mini",
        "input": json.dumps([{"role": "user", "content": "Summarize: OpenAI releases GPT-5..."}]),
        "output": json.dumps([{"role": "assistant", "content": "AI is advancing rapidly..."}]),
        "input_tokens": 45, "output_tokens": 30,
        "start_time": base_time + 210_000_000, "end_time": base_time + 480_000_000,
    })

    resp = requests.post(
        f"{INGEST_URL}/api/v1/spans",
        headers={"X-API-Key": API_KEY, "Content-Type": "application/json"},
        json={"spans": spans},
        timeout=10,
    )
    print(f"  Status: {resp.status_code}")
    print(f"  Body:   {resp.text[:200]}")
    return resp.status_code == 202


# ─────────────────────────────────────────────────────────────────────────────
# Test 5: Error cases
# ─────────────────────────────────────────────────────────────────────────────
def test_error_cases():
    print("\n=== Test 5: Error cases ===")

    # Missing API key
    r = requests.post(f"{INGEST_URL}/api/v1/spans", json={"spans": []}, timeout=5)
    print(f"  No API key -> {r.status_code} (expect 401)")

    # Invalid API key
    r = requests.post(
        f"{INGEST_URL}/api/v1/spans",
        headers={"X-API-Key": "oev_proj_invalid"},
        json={"spans": []}, timeout=5
    )
    print(f"  Invalid key -> {r.status_code} (expect 401)")

    # Empty spans array
    r = requests.post(
        f"{INGEST_URL}/api/v1/spans",
        headers={"X-API-Key": API_KEY},
        json={"spans": []}, timeout=5
    )
    print(f"  Empty spans -> {r.status_code} (expect 202 or 400)")

    return True


if __name__ == "__main__":
    results = {}

    results["native_rest"] = test_native_rest()
    results["otlp"] = test_otlp()
    results["sdk"] = test_omneval_sdk()
    results["multi_span"] = test_multi_span_trace()
    results["errors"] = test_error_cases()

    print("\n=== RESULTS ===")
    for name, ok in results.items():
        status = "PASS" if ok else "FAIL"
        print(f"  {status}: {name}")
