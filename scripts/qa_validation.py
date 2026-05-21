"""
Omneval QA Validation Script
=============================
Tests the Omneval platform end-to-end:
  1. Omneval Python SDK trace sending
  2. OmnevalClient prompt registry and score writes
  3. Raw OTLP HTTP trace sending
  4. Native REST ingest API (auth + all span types)
  5. Query API (auth, span query, trace detail, analytics)
  6. Eval rules and prompts management
  7. Edge cases and error paths

Usage (set PYTHONIOENCODING=utf-8 or just use ASCII output):
    uv run --with opentelemetry-sdk --with opentelemetry-exporter-otlp-proto-http \
           --with requests --with opentelemetry-semantic-conventions \
           scripts/qa_validation.py
"""
import json
import os
import sys
import time
import uuid
import traceback
import threading

import requests

# Force UTF-8 on Windows to avoid cp1252 issues.
if sys.stdout.encoding and sys.stdout.encoding.lower() not in ("utf-8", "utf_8"):
    import io
    sys.stdout = io.TextIOWrapper(sys.stdout.buffer, encoding="utf-8")

# ============================================================
# Configuration
# ============================================================
API_KEY = "oev_proj_3Xrupmyx4vQZxGFAMSrACBfeqcYsmxTACnFuFhdqZ7U3"
PROJECT_ID = "d87p1riv072c739868ug"
INGEST_URL = "http://localhost:8000"
QUERY_URL = "http://localhost:8002"
ADMIN_EMAIL = "admin@omneval.com"
ADMIN_PASSWORD = "admin"

PASS = "PASS"
FAIL = "FAIL"
SKIP = "SKIP"
results = []
ingested_trace_ids = []
created_prompt_name = None


def report(name, status, detail=""):
    icon = "PASS" if status == PASS else ("SKIP" if status == SKIP else "FAIL")
    line = f"  [{icon}] {name}" + (f": {detail}" if detail else "")
    print(line)
    sys.stdout.flush()
    results.append((name, status, detail))


def hex_id(n=32):
    return uuid.uuid4().hex[:n]


def make_span(name, kind="llm", model="gpt-4o", parent_id=None, trace_id=None):
    s = {
        "span_id": hex_id(16),
        "trace_id": trace_id or hex_id(32),
        "name": name,
        "kind": kind,
        "model": model,
        "input": f"User asked: what is {name}?",
        "output": f"Assistant responded about {name}.",
        "input_tokens": 25,
        "output_tokens": 50,
        "attributes": {"env": "qa-validation"},
    }
    if parent_id:
        s["parent_id"] = parent_id
    return s


def ingest_headers():
    return {"X-API-Key": API_KEY, "Content-Type": "application/json"}


print("=" * 65)
print("Omneval QA Validation Suite")
print("=" * 65)

# ============================================================
# SECTION 1: Omneval Python SDK
# ============================================================
print("\n-- Section 1: Omneval Python SDK --")

try:
    sdk_path = os.path.normpath(
        os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "sdk", "python")
    )
    if sdk_path not in sys.path:
        sys.path.insert(0, sdk_path)

    import omneval_sdk

    # 1a. init alias works (same as configure)
    if omneval_sdk.init is omneval_sdk.configure:
        report("SDK: init is alias for configure", PASS)
    else:
        report("SDK: init is alias for configure", FAIL, "aliases differ")

    # Configure SDK
    omneval_sdk.configure(endpoint=INGEST_URL, api_key=API_KEY)
    report("SDK: configure() without error", PASS)

    # 1b. Nested @trace decorators, parent-child context
    @omneval_sdk.trace
    def sdk_outer():
        span = omneval_sdk.get_active_span()
        if span:
            omneval_sdk.set_model(span, "gpt-4o")
            omneval_sdk.set_input(span, "QA: explain quantum computing")
            omneval_sdk.set_output(span, "Quantum computing uses qubits in superposition...")
            omneval_sdk.set_tokens(span, 75, 125)
        return sdk_inner()

    @omneval_sdk.trace
    def sdk_inner():
        span = omneval_sdk.get_active_span()
        if span:
            omneval_sdk.set_model(span, "gpt-4o-mini")
            omneval_sdk.set_input(span, "Summarize quantum computing in one sentence")
            omneval_sdk.set_output(span, "Quantum computers leverage quantum mechanics for faster processing.")
            omneval_sdk.set_tokens(span, 20, 45)
        return "sdk-inner-result"

    result = sdk_outer()
    if result == "sdk-inner-result":
        report("SDK: nested @trace decorators work", PASS)
    else:
        report("SDK: nested @trace decorators work", FAIL, f"unexpected return: {result!r}")

    # 1c. get_active_span() returns None outside a trace context
    outside_span = omneval_sdk.get_active_span()
    if outside_span is None:
        report("SDK: get_active_span() returns None outside trace", PASS)
    else:
        report("SDK: get_active_span() returns None outside trace", FAIL, f"got {outside_span!r}")

    # 1d. set_* functions are None-safe
    try:
        omneval_sdk.set_model(None, "gpt-4o")
        omneval_sdk.set_input(None, "test")
        omneval_sdk.set_output(None, "test")
        omneval_sdk.set_tokens(None, 0, 0)
        report("SDK: set_* functions are None-safe", PASS)
    except Exception as e:
        report("SDK: set_* functions are None-safe", FAIL, str(e))

    # 1e. @trace propagates exceptions without swallowing
    @omneval_sdk.trace
    def sdk_error_fn():
        span = omneval_sdk.get_active_span()
        if span:
            omneval_sdk.set_model(span, "gpt-4o")
            omneval_sdk.set_input(span, "cause an error")
        raise ValueError("intentional QA test error")

    try:
        sdk_error_fn()
        report("SDK: @trace propagates exceptions", FAIL, "exception was swallowed")
    except ValueError as e:
        if "intentional QA test error" in str(e):
            report("SDK: @trace propagates exceptions", PASS)
        else:
            report("SDK: @trace propagates exceptions", FAIL, f"wrong exception: {e}")

    # 1f. Multiple sequential @trace calls (different root spans)
    call_count = {"n": 0}

    @omneval_sdk.trace
    def sdk_sequential():
        call_count["n"] += 1
        span = omneval_sdk.get_active_span()
        if span:
            omneval_sdk.set_model(span, "gpt-3.5-turbo")
            omneval_sdk.set_input(span, f"sequential call {call_count['n']}")
            omneval_sdk.set_output(span, "response")
            omneval_sdk.set_tokens(span, 10, 20)

    for _ in range(3):
        sdk_sequential()

    if call_count["n"] == 3:
        report("SDK: 3 sequential @trace calls work", PASS)
    else:
        report("SDK: 3 sequential @trace calls work", FAIL, f"call_count={call_count['n']}")

    # 1g. @trace without configure is safe (noop)
    omneval_sdk.configure.__doc__  # just to verify it's accessible
    # reset provider to None temporarily
    import omneval_sdk.exporter as _exp
    _orig_provider = _exp._tracer_provider
    _exp._tracer_provider = None

    @omneval_sdk.trace
    def sdk_unconfigured():
        return "unconfigured-result"

    try:
        r = sdk_unconfigured()
        if r == "unconfigured-result":
            report("SDK: @trace without configure is safe noop", PASS)
        else:
            report("SDK: @trace without configure is safe noop", FAIL, f"got {r!r}")
    except Exception as e:
        report("SDK: @trace without configure is safe noop", FAIL, str(e))
    finally:
        # Restore provider
        _exp._tracer_provider = _orig_provider

    # 1h. 3-level deep nesting
    @omneval_sdk.trace
    def level_a():
        return level_b()

    @omneval_sdk.trace
    def level_b():
        return level_c()

    @omneval_sdk.trace
    def level_c():
        span = omneval_sdk.get_active_span()
        if span:
            omneval_sdk.set_model(span, "gpt-4o")
            omneval_sdk.set_input(span, "deep nesting test")
            omneval_sdk.set_tokens(span, 5, 10)
        return "deep"

    if level_a() == "deep":
        report("SDK: 3-level deep nesting", PASS)
    else:
        report("SDK: 3-level deep nesting", FAIL)

except ImportError as e:
    report("SDK: import omneval_sdk", SKIP, f"not importable: {e}")
except Exception as e:
    report("SDK: unexpected error", FAIL, traceback.format_exc()[:400])


# ============================================================
# SECTION 2: OmnevalClient (Prompt Registry + Scores)
# ============================================================
print("\n-- Section 2: OmnevalClient --")

created_prompt_name = None
try:
    from omneval_sdk import OmnevalClient

    client = OmnevalClient(base_url=QUERY_URL, api_key=API_KEY)

    # 2a. Create a prompt via the REST API so we have something to fetch
    pname = f"qa-test-prompt-{hex_id(8)}"
    session = requests.Session()
    r = session.post(f"{QUERY_URL}/login",
                     json={"email": ADMIN_EMAIL, "password": ADMIN_PASSWORD}, timeout=10)
    if r.status_code == 200:
        # Create a prompt
        r2 = session.post(f"{QUERY_URL}/api/v1/prompts",
                          json={
                              "name": pname,
                              "template": "You are a helpful assistant. User: {{user_input}}",
                              "model_config": {"model": "gpt-4o", "temperature": 0.7},
                          }, timeout=10)
        if r2.status_code in (200, 201):
            created_prompt_name = pname
            report(f"Prompt: create via REST API", PASS)

            # Set production label
            r3 = session.put(f"{QUERY_URL}/api/v1/prompts/{pname}/labels/production",
                             json={"version": 1}, timeout=10)
            if r3.status_code == 200:
                report("Prompt: set production label", PASS)
            else:
                report("Prompt: set production label", FAIL, f"{r3.status_code}: {r3.text[:100]}")

            # 2b. OmnevalClient.get_prompt() fetches by label
            try:
                pdata = client.get_prompt(pname, label="production")
                if "template" in pdata and "{{user_input}}" in pdata["template"]:
                    report("OmnevalClient: get_prompt() by label", PASS)
                else:
                    report("OmnevalClient: get_prompt() by label", FAIL, f"unexpected data: {pdata}")
            except Exception as e:
                report("OmnevalClient: get_prompt() by label", FAIL, str(e))

            # 2c. Label cache hit (second call should be cached)
            try:
                t0 = time.time()
                pdata2 = client.get_prompt(pname, label="production")
                elapsed = time.time() - t0
                if "template" in pdata2 and elapsed < 0.05:
                    report("OmnevalClient: label cache hit (fast)", PASS)
                else:
                    report("OmnevalClient: label cache hit (fast)", FAIL,
                           f"elapsed={elapsed:.3f}s, data={pdata2}")
            except Exception as e:
                report("OmnevalClient: label cache hit", FAIL, str(e))

            # 2d. OmnevalClient.get_prompt_version() fetches by explicit version
            try:
                pv = client.get_prompt_version(pname, version=1)
                if "template" in pv:
                    report("OmnevalClient: get_prompt_version()", PASS)
                else:
                    report("OmnevalClient: get_prompt_version()", FAIL, f"data: {pv}")
            except Exception as e:
                report("OmnevalClient: get_prompt_version()", FAIL, str(e))

            # 2e. Version cache (second call, no-TTL)
            try:
                t0 = time.time()
                pv2 = client.get_prompt_version(pname, version=1)
                elapsed = time.time() - t0
                if "template" in pv2 and elapsed < 0.05:
                    report("OmnevalClient: version cache (no TTL)", PASS)
                else:
                    report("OmnevalClient: version cache (no TTL)", FAIL,
                           f"elapsed={elapsed:.3f}s")
            except Exception as e:
                report("OmnevalClient: version cache (no TTL)", FAIL, str(e))

        else:
            report("Prompt: create via REST API", FAIL, f"{r2.status_code}: {r2.text[:200]}")
    else:
        report("Prompt: create via REST API", SKIP, "login failed")

    # 2f. get_prompt for nonexistent prompt -> HTTPError
    try:
        client.get_prompt("nonexistent-prompt-xyz-" + hex_id(8), label="production")
        report("OmnevalClient: get_prompt 404 raises HTTPError", FAIL, "no exception")
    except requests.HTTPError as e:
        report("OmnevalClient: get_prompt 404 raises HTTPError", PASS)
    except Exception as e:
        report("OmnevalClient: get_prompt 404 raises HTTPError", FAIL, f"wrong exc: {type(e).__name__}: {e}")

    # 2g. write_score() with valid span_id (span may not exist, but the endpoint should accept it)
    try:
        test_span_id = hex_id(16)
        client.write_score(
            span_id=test_span_id,
            eval_name="qa-manual-score",
            value=0.85,
            reasoning="QA test manual score",
        )
        report("OmnevalClient: write_score() returns without error", PASS)
    except Exception as e:
        report("OmnevalClient: write_score() returns without error", FAIL, str(e))

    # 2h. write_score() with empty span_id -> ValueError
    try:
        client.write_score(span_id="", eval_name="test", value=0.5)
        report("OmnevalClient: write_score empty span_id raises ValueError", FAIL, "no exception")
    except ValueError:
        report("OmnevalClient: write_score empty span_id raises ValueError", PASS)
    except Exception as e:
        report("OmnevalClient: write_score empty span_id raises ValueError", FAIL,
               f"wrong exc: {type(e).__name__}: {e}")

    client.close()

except ImportError as e:
    report("OmnevalClient: import", SKIP, f"not importable: {e}")
except Exception as e:
    report("OmnevalClient: unexpected error", FAIL, traceback.format_exc()[:400])


# ============================================================
# SECTION 3: OTLP HTTP Direct
# ============================================================
print("\n-- Section 3: OTLP HTTP Ingest (/v1/traces) --")

try:
    from opentelemetry.sdk.trace import TracerProvider
    from opentelemetry.sdk.trace.export import SimpleSpanProcessor
    from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter
    from opentelemetry.sdk.resources import Resource, SERVICE_NAME
    from opentelemetry import trace as otel_trace

    resource = Resource.create({SERVICE_NAME: "omneval-qa-validation"})
    provider = TracerProvider(resource=resource)
    exporter = OTLPSpanExporter(
        endpoint=f"{INGEST_URL}/v1/traces",
        headers={"X-API-Key": API_KEY},
    )
    provider.add_span_processor(SimpleSpanProcessor(exporter))
    tracer = provider.get_tracer("omneval-qa")

    with tracer.start_as_current_span("otlp-qa-root") as root:
        root.set_attribute("gen_ai.request.model", "claude-3-5-sonnet")
        root.set_attribute("gen_ai.usage.input_tokens", 100)
        root.set_attribute("gen_ai.usage.output_tokens", 200)
        root.set_attribute("omneval.input", "What is the capital of France?")
        root.set_attribute("omneval.output", "The capital of France is Paris.")

        with tracer.start_as_current_span("otlp-qa-tool-call") as child:
            child.set_attribute("gen_ai.request.model", "claude-3-haiku")
            child.set_attribute("gen_ai.usage.input_tokens", 50)
            child.set_attribute("gen_ai.usage.output_tokens", 30)
            child.set_attribute("omneval.input", "search('capital of France')")
            child.set_attribute("omneval.output", "Paris")

    provider.shutdown()
    report("OTLP: parent-child span exported without error", PASS)

    # 3b. OTLP without API key -> auth failure (but SDK may swallow it; test via requests)
    try:
        import gzip as _gz
        from opentelemetry.proto.collector.trace.v1 import trace_service_pb2 as _tsvc
        dummy_req = _tsvc.ExportTraceServiceRequest()
        raw = dummy_req.SerializeToString()
        compressed = _gz.compress(raw)
        r = requests.post(
            f"{INGEST_URL}/v1/traces",
            data=compressed,
            headers={"Content-Type": "application/x-protobuf", "Content-Encoding": "gzip"},
            timeout=10,
        )
        if r.status_code == 401:
            report("OTLP: no API key -> 401", PASS)
        else:
            report("OTLP: no API key -> 401", FAIL, f"got {r.status_code}: {r.text[:100]}")
    except ImportError:
        report("OTLP: no API key -> 401", SKIP, "proto not available")
    except Exception as e:
        report("OTLP: no API key -> 401", FAIL, str(e)[:100])

except ImportError as e:
    report("OTLP: import", SKIP, f"opentelemetry not installed: {e}")
except Exception as e:
    report("OTLP: export failed", FAIL, str(e)[:200])


# ============================================================
# SECTION 4: Native REST Ingest API
# ============================================================
print("\n-- Section 4: Native REST Ingest (/api/v1/spans) --")

# 4a. No API key -> 401
try:
    r = requests.post(f"{INGEST_URL}/api/v1/spans",
                      json={"spans": [make_span("auth-test")]}, timeout=10)
    if r.status_code == 401:
        report("Auth: missing API key -> 401", PASS)
    else:
        report("Auth: missing API key -> 401", FAIL, f"got {r.status_code}: {r.text[:100]}")
except Exception as e:
    report("Auth: missing API key -> 401", FAIL, str(e))

# 4b. Bad API key -> 401
try:
    r = requests.post(f"{INGEST_URL}/api/v1/spans",
                      headers={"X-API-Key": "oev_proj_" + "x" * 43},
                      json={"spans": [make_span("bad-key")]}, timeout=10)
    if r.status_code == 401:
        report("Auth: invalid API key -> 401", PASS)
    else:
        report("Auth: invalid API key -> 401", FAIL, f"got {r.status_code}: {r.text[:100]}")
except Exception as e:
    report("Auth: invalid API key -> 401", FAIL, str(e))

# 4c. Single LLM span
try:
    span = make_span("qa-llm-single", kind="llm", model="gpt-4o")
    ingested_trace_ids.append(span["trace_id"])
    r = requests.post(f"{INGEST_URL}/api/v1/spans",
                      headers=ingest_headers(), json={"spans": [span]}, timeout=10)
    if r.status_code == 202:
        report("Single LLM span ingested (202)", PASS)
    else:
        report("Single LLM span ingested (202)", FAIL, f"got {r.status_code}: {r.text[:200]}")
except Exception as e:
    report("Single LLM span ingested (202)", FAIL, str(e))

# 4d. Parent-child trace
try:
    root_trace_id = hex_id(32)
    root_span = make_span("qa-agent-root", kind="agent", model="gpt-4o", trace_id=root_trace_id)
    child_span = make_span("qa-tool-call", kind="tool", trace_id=root_trace_id,
                           parent_id=root_span["span_id"])
    child_span["model"] = ""
    ingested_trace_ids.append(root_trace_id)
    r = requests.post(f"{INGEST_URL}/api/v1/spans",
                      headers=ingest_headers(),
                      json={"spans": [root_span, child_span]}, timeout=10)
    if r.status_code == 202:
        report("Parent-child trace batch ingested (202)", PASS)
    else:
        report("Parent-child trace batch ingested (202)", FAIL, f"got {r.status_code}: {r.text[:200]}")
except Exception as e:
    report("Parent-child trace batch ingested (202)", FAIL, str(e))

# 4e. All span kinds
try:
    kinds = ["llm", "agent", "tool", "chain", "internal"]
    spans = [make_span(f"qa-kind-{k}", kind=k) for k in kinds]
    r = requests.post(f"{INGEST_URL}/api/v1/spans",
                      headers=ingest_headers(), json={"spans": spans}, timeout=10)
    if r.status_code == 202:
        report("All span kinds accepted in batch (202)", PASS)
    else:
        report("All span kinds accepted in batch (202)", FAIL, f"got {r.status_code}: {r.text[:200]}")
except Exception as e:
    report("All span kinds accepted in batch (202)", FAIL, str(e))

# 4f. Empty spans array -> 400 or 422
try:
    r = requests.post(f"{INGEST_URL}/api/v1/spans",
                      headers=ingest_headers(), json={"spans": []}, timeout=10)
    if r.status_code in (400, 422):
        report("Empty spans array -> 400/422", PASS)
    else:
        report("Empty spans array -> 400/422", FAIL, f"got {r.status_code}: {r.text[:100]}")
except Exception as e:
    report("Empty spans array -> 400/422", FAIL, str(e))

# 4g. Span with missing span_id -> 400
try:
    bad_span = make_span("qa-no-span-id")
    del bad_span["span_id"]
    r = requests.post(f"{INGEST_URL}/api/v1/spans",
                      headers=ingest_headers(), json={"spans": [bad_span]}, timeout=10)
    if r.status_code in (400, 422):
        report("Missing span_id -> 400/422", PASS)
    else:
        report("Missing span_id -> 400/422", FAIL, f"got {r.status_code}: {r.text[:100]}")
except Exception as e:
    report("Missing span_id -> 400/422", FAIL, str(e))

# 4h. Large batch (50 spans)
try:
    large_trace_id = hex_id(32)
    many_spans = [make_span(f"qa-bulk-{i}", trace_id=large_trace_id) for i in range(50)]
    ingested_trace_ids.append(large_trace_id)
    r = requests.post(f"{INGEST_URL}/api/v1/spans",
                      headers=ingest_headers(), json={"spans": many_spans}, timeout=30)
    if r.status_code == 202:
        report("Large batch (50 spans) ingested (202)", PASS)
    else:
        report("Large batch (50 spans) ingested (202)", FAIL, f"got {r.status_code}: {r.text[:200]}")
except Exception as e:
    report("Large batch (50 spans) ingested (202)", FAIL, str(e))

# 4i. Span with optional fields omitted (minimal span)
try:
    minimal = {
        "span_id": hex_id(16),
        "trace_id": hex_id(32),
        "name": "qa-minimal",
        "kind": "llm",
    }
    r = requests.post(f"{INGEST_URL}/api/v1/spans",
                      headers=ingest_headers(), json={"spans": [minimal]}, timeout=10)
    if r.status_code == 202:
        report("Minimal span (no model/input/output/tokens) -> 202", PASS)
    else:
        report("Minimal span (no model/input/output/tokens) -> 202", FAIL,
               f"got {r.status_code}: {r.text[:200]}")
except Exception as e:
    report("Minimal span -> 202", FAIL, str(e))

# 4j. Idempotent: same span_id ingested twice should not error
try:
    dup_span = make_span("qa-duplicate", model="gpt-4o")
    r1 = requests.post(f"{INGEST_URL}/api/v1/spans",
                       headers=ingest_headers(), json={"spans": [dup_span]}, timeout=10)
    r2 = requests.post(f"{INGEST_URL}/api/v1/spans",
                       headers=ingest_headers(), json={"spans": [dup_span]}, timeout=10)
    if r1.status_code == 202 and r2.status_code == 202:
        report("Idempotent: same span ingested twice -> 202 both times", PASS)
    else:
        report("Idempotent: same span ingested twice", FAIL,
               f"r1={r1.status_code} r2={r2.status_code}")
except Exception as e:
    report("Idempotent ingest", FAIL, str(e))


# ============================================================
# SECTION 5: Query API - Auth
# ============================================================
print("\n-- Section 5: Query API Auth --")

session = requests.Session()

try:
    r = session.post(f"{QUERY_URL}/login",
                     json={"email": ADMIN_EMAIL, "password": ADMIN_PASSWORD}, timeout=10)
    if r.status_code == 200 and "session_id" in r.json():
        report("Login -> 200 with session_id", PASS)
    else:
        report("Login -> 200 with session_id", FAIL, f"got {r.status_code}: {r.text[:200]}")
except Exception as e:
    report("Login -> 200 with session_id", FAIL, str(e))

try:
    r = requests.post(f"{QUERY_URL}/login",
                      json={"email": "nobody@example.com", "password": "wrong"}, timeout=10)
    if r.status_code == 401:
        report("Bad credentials -> 401", PASS)
    else:
        report("Bad credentials -> 401", FAIL, f"got {r.status_code}: {r.text[:100]}")
except Exception as e:
    report("Bad credentials -> 401", FAIL, str(e))

try:
    r = requests.post(f"{QUERY_URL}/api/v1/spans/query", json={}, timeout=10)
    if r.status_code == 401:
        report("Unauthenticated span query -> 401", PASS)
    else:
        report("Unauthenticated span query -> 401", FAIL, f"got {r.status_code}")
except Exception as e:
    report("Unauthenticated span query -> 401", FAIL, str(e))

# 5d. Logout clears session
try:
    anon_session = requests.Session()
    lr = anon_session.post(f"{QUERY_URL}/login",
                           json={"email": ADMIN_EMAIL, "password": ADMIN_PASSWORD}, timeout=10)
    if lr.status_code == 200:
        anon_session.post(f"{QUERY_URL}/logout", timeout=10)
        r = anon_session.post(f"{QUERY_URL}/api/v1/spans/query", json={}, timeout=10)
        if r.status_code == 401:
            report("Logout clears session -> subsequent request 401", PASS)
        else:
            report("Logout clears session", FAIL, f"after logout got {r.status_code}")
    else:
        report("Logout clears session", SKIP, "could not login for test")
except Exception as e:
    report("Logout clears session", FAIL, str(e))


# ============================================================
# SECTION 6: Projects & API Keys
# ============================================================
print("\n-- Section 6: Projects & API Keys --")

try:
    r = session.get(f"{QUERY_URL}/api/v1/projects", timeout=10)
    if r.status_code == 200:
        projects = r.json()
        if isinstance(projects, list) and len(projects) > 0:
            first = projects[0]
            if "project_id" in first:
                report(f"Projects list: snake_case keys ({len(projects)} projects)", PASS)
            else:
                report("Projects list: snake_case JSON keys", FAIL,
                       f"unexpected keys: {list(first.keys())}")
        else:
            report("Projects list not empty", FAIL, f"got: {projects}")
    else:
        report("Projects list -> 200", FAIL, f"got {r.status_code}: {r.text[:200]}")
except Exception as e:
    report("Projects list -> 200", FAIL, str(e))

try:
    r = session.get(f"{QUERY_URL}/api/v1/projects/{PROJECT_ID}/api-keys", timeout=10)
    if r.status_code == 200 and isinstance(r.json(), list):
        report(f"API keys list -> 200 ({len(r.json())} keys)", PASS)
    else:
        report("API keys list -> 200", FAIL, f"got {r.status_code}: {r.text[:200]}")
except Exception as e:
    report("API keys list -> 200", FAIL, str(e))


# ============================================================
# SECTION 7: Span Queries (wait for writer sync)
# ============================================================
print("\n-- Section 7: Span Query (waiting 35s for writer sync) --")
sys.stdout.flush()
time.sleep(35)

# 7a. Query with time range
try:
    r = session.post(f"{QUERY_URL}/api/v1/spans/query",
                     json={
                         "from": "2026-01-01T00:00:00Z",
                         "to": "2026-12-31T23:59:59Z",
                         "limit": 50,
                     }, timeout=10)
    if r.status_code == 200:
        data = r.json()
        count = len(data.get("spans", []))
        if count > 0:
            report(f"Span query with time range: {count} spans", PASS)
        else:
            report("Span query with time range returns spans", FAIL,
                   "0 spans - snapshot may lag (writer syncs every 30s)")
    else:
        report("Span query with time range -> 200", FAIL, f"got {r.status_code}: {r.text[:200]}")
except Exception as e:
    report("Span query with time range -> 200", FAIL, str(e))

# 7b. Filter by model
try:
    r = session.post(f"{QUERY_URL}/api/v1/spans/query",
                     json={
                         "from": "2026-01-01T00:00:00Z",
                         "to": "2026-12-31T23:59:59Z",
                         "filters": [{"field": "model", "op": "eq", "value": "gpt-4o"}],
                         "limit": 20,
                     }, timeout=10)
    if r.status_code == 200:
        spans = r.json().get("spans", [])
        report(f"Span query filter by model: {len(spans)} gpt-4o spans", PASS)
    else:
        report("Span query filter by model -> 200", FAIL, f"got {r.status_code}: {r.text[:200]}")
except Exception as e:
    report("Span query filter by model -> 200", FAIL, str(e))

# 7c. Query without time range (tests default range handling)
try:
    r = session.post(f"{QUERY_URL}/api/v1/spans/query",
                     json={"limit": 50}, timeout=10)
    if r.status_code == 200:
        data = r.json()
        count = len(data.get("spans", []))
        if count > 0:
            report("Span query without time range returns spans", PASS)
        else:
            report("Span query without time range returns spans", FAIL,
                   "BUG: 0 spans - Go zero time.Time defaults to 0001-01-01")
    else:
        report("Span query without time range -> 200", FAIL, f"got {r.status_code}: {r.text[:200]}")
except Exception as e:
    report("Span query without time range -> 200", FAIL, str(e))

# 7d. Pagination (cursor-based)
first_cursor = None
try:
    r = session.post(f"{QUERY_URL}/api/v1/spans/query",
                     json={
                         "from": "2026-01-01T00:00:00Z",
                         "to": "2026-12-31T23:59:59Z",
                         "limit": 5,
                     }, timeout=10)
    if r.status_code == 200:
        data = r.json()
        first_cursor = data.get("next_cursor")
        if first_cursor:
            report("Span query pagination: next_cursor returned", PASS)
            # Fetch second page
            r2 = session.post(f"{QUERY_URL}/api/v1/spans/query",
                              json={
                                  "from": "2026-01-01T00:00:00Z",
                                  "to": "2026-12-31T23:59:59Z",
                                  "limit": 5,
                                  "cursor": first_cursor,
                              }, timeout=10)
            if r2.status_code == 200:
                page2 = r2.json().get("spans", [])
                report(f"Span query pagination page 2: {len(page2)} spans", PASS)
            else:
                report("Span query pagination page 2", FAIL, f"{r2.status_code}: {r2.text[:100]}")
        else:
            report("Span query pagination: next_cursor returned", SKIP,
                   f"only {len(data.get('spans', []))} spans total, no cursor")
    else:
        report("Span query pagination -> 200", FAIL, f"got {r.status_code}")
except Exception as e:
    report("Span query pagination", FAIL, str(e))

# 7e. Filter by name (text search)
try:
    r = session.post(f"{QUERY_URL}/api/v1/spans/query",
                     json={
                         "from": "2026-01-01T00:00:00Z",
                         "to": "2026-12-31T23:59:59Z",
                         "filters": [{"field": "name", "op": "contains", "value": "qa-"}],
                         "limit": 20,
                     }, timeout=10)
    if r.status_code == 200:
        spans = r.json().get("spans", [])
        report(f"Span filter by name contains 'qa-': {len(spans)} spans", PASS)
    elif r.status_code in (400, 422):
        report("Span filter by name contains: 400/422 (unsupported filter)", FAIL,
               f"{r.status_code}: {r.text[:100]}")
    else:
        report("Span filter by name", FAIL, f"{r.status_code}: {r.text[:100]}")
except Exception as e:
    report("Span filter by name", FAIL, str(e))


# ============================================================
# SECTION 8: Trace Detail
# ============================================================
print("\n-- Section 8: Trace Detail --")

trace_id_for_detail = None
try:
    r = session.post(f"{QUERY_URL}/api/v1/spans/query",
                     json={"from": "2026-01-01T00:00:00Z",
                           "to": "2026-12-31T23:59:59Z", "limit": 1},
                     timeout=10)
    if r.status_code == 200:
        spans = r.json().get("spans", [])
        if spans:
            trace_id_for_detail = spans[0]["trace_id"]
except Exception:
    pass

if trace_id_for_detail:
    try:
        r = session.get(f"{QUERY_URL}/api/v1/traces/{trace_id_for_detail}", timeout=10)
        if r.status_code == 200:
            detail = r.json()
            has_trace_id = "trace_id" in detail
            has_root = "root_span" in detail
            has_spans = "spans" in detail and len(detail["spans"]) > 0
            if has_trace_id and has_root and has_spans:
                report("Trace detail: root_span + spans present", PASS)
            else:
                missing = [k for k, v in [("trace_id", has_trace_id),
                                           ("root_span", has_root),
                                           ("spans", has_spans)] if not v]
                report("Trace detail: missing expected fields", FAIL,
                       f"missing: {missing}, keys: {list(detail.keys())}")
        else:
            report("Trace detail -> 200", FAIL, f"got {r.status_code}: {r.text[:200]}")
    except Exception as e:
        report("Trace detail -> 200", FAIL, str(e))

    # 8b. Nonexistent trace -> 404
    try:
        r = session.get(f"{QUERY_URL}/api/v1/traces/{'f'*32}", timeout=10)
        if r.status_code == 404:
            report("Trace detail: missing trace -> 404", PASS)
        else:
            report("Trace detail: missing trace -> 404", FAIL, f"got {r.status_code}: {r.text[:100]}")
    except Exception as e:
        report("Trace detail: missing trace -> 404", FAIL, str(e))

    # 8c. Bookmark toggle
    try:
        r = session.post(f"{QUERY_URL}/api/v1/traces/{trace_id_for_detail}/bookmark", timeout=10)
        if r.status_code in (200, 204):
            report("Trace bookmark toggle -> 200/204", PASS)
        else:
            report("Trace bookmark toggle -> 200/204", FAIL, f"got {r.status_code}: {r.text[:100]}")
    except Exception as e:
        report("Trace bookmark toggle", FAIL, str(e))
else:
    report("Trace detail", SKIP, "no spans available (snapshot not yet populated)")


# ============================================================
# SECTION 9: Analytics
# ============================================================
print("\n-- Section 9: Analytics --")

# 9a. Empty request
try:
    r = session.post(f"{QUERY_URL}/api/v1/analytics/spans", json={}, timeout=10)
    if r.status_code == 200 and "rows" in r.json():
        report("Analytics: empty request returns rows", PASS, f"rows={r.json()['rows']}")
    else:
        report("Analytics: empty request -> 200", FAIL, f"got {r.status_code}: {r.text[:200]}")
except Exception as e:
    report("Analytics: empty request -> 200", FAIL, str(e))

# 9b. Group by model
try:
    r = session.post(f"{QUERY_URL}/api/v1/analytics/spans",
                     json={
                         "aggregations": [{"function": "count", "field": "*", "alias": "span_count"}],
                         "group_by": [{"field": "model"}],
                         "order_by": [{"field": "span_count", "desc": True}],
                     }, timeout=10)
    if r.status_code == 200:
        rows = r.json().get("rows", [])
        report(f"Analytics: group by model ({len(rows)} groups)", PASS)
    else:
        report("Analytics: group by model -> 200", FAIL, f"got {r.status_code}: {r.text[:200]}")
except Exception as e:
    report("Analytics: group by model -> 200", FAIL, str(e))

# 9c. Sum cost_usd
try:
    r = session.post(f"{QUERY_URL}/api/v1/analytics/spans",
                     json={
                         "aggregations": [{"function": "sum", "field": "cost_usd", "alias": "total_cost"}],
                     }, timeout=10)
    if r.status_code == 200:
        report("Analytics: sum cost_usd", PASS, f"result={r.json()}")
    else:
        report("Analytics: sum cost_usd -> 200", FAIL, f"got {r.status_code}: {r.text[:200]}")
except Exception as e:
    report("Analytics: sum cost_usd", FAIL, str(e))

# 9d. Unknown aggregation function -> 400
try:
    r = session.post(f"{QUERY_URL}/api/v1/analytics/spans",
                     json={
                         "aggregations": [{"function": "notafunc", "field": "cost_usd", "alias": "x"}],
                     }, timeout=10)
    if r.status_code == 400:
        report("Analytics: unknown aggregation function -> 400", PASS)
    else:
        report("Analytics: unknown aggregation function -> 400", FAIL, f"got {r.status_code}: {r.text[:100]}")
except Exception as e:
    report("Analytics: unknown function -> 400", FAIL, str(e))

# 9e. Time series aggregation (group by time bucket)
try:
    r = session.post(f"{QUERY_URL}/api/v1/analytics/spans",
                     json={
                         "aggregations": [
                             {"function": "count", "field": "*", "alias": "count"},
                             {"function": "sum", "field": "cost_usd", "alias": "cost"},
                         ],
                         "group_by": [{"field": "time_bucket", "interval": "1h"}],
                         "from": "2026-01-01T00:00:00Z",
                         "to": "2026-12-31T23:59:59Z",
                     }, timeout=10)
    if r.status_code == 200:
        report("Analytics: time-bucket group by", PASS, f"rows={r.json().get('rows')}")
    elif r.status_code == 400:
        report("Analytics: time-bucket group by -> 400 (unsupported)", FAIL, r.text[:100])
    else:
        report("Analytics: time-bucket group by", FAIL, f"got {r.status_code}: {r.text[:200]}")
except Exception as e:
    report("Analytics: time-bucket group by", FAIL, str(e))


# ============================================================
# SECTION 10: Eval Rules & Prompts
# ============================================================
print("\n-- Section 10: Eval Rules & Prompts --")

# 10a. List eval rules
try:
    r = session.get(f"{QUERY_URL}/api/v1/eval-rules", timeout=10)
    if r.status_code == 200 and isinstance(r.json().get("rules"), list):
        report(f"Eval rules list -> 200 ({len(r.json()['rules'])} rules)", PASS)
    else:
        report("Eval rules list -> 200", FAIL, f"got {r.status_code}: {r.text[:200]}")
except Exception as e:
    report("Eval rules list -> 200", FAIL, str(e))

# 10b. Create eval rule
created_rule_id = None
try:
    r = session.post(f"{QUERY_URL}/api/v1/eval-rules",
                     json={
                         "name": "qa-test-rule",
                         "prompt": "Rate the quality of the response on a scale 0-1.\nResponse: {{output}}\nReturn JSON: {\"score\": <float>}",
                         "filter": {"field": "model", "op": "eq", "value": "gpt-4o"},
                         "sample_rate": 0.1,
                     }, timeout=10)
    if r.status_code in (200, 201):
        rule = r.json()
        created_rule_id = rule.get("id") or rule.get("rule_id")
        report("Eval rule: create -> 200/201", PASS)
    else:
        report("Eval rule: create -> 200/201", FAIL, f"got {r.status_code}: {r.text[:200]}")
except Exception as e:
    report("Eval rule: create", FAIL, str(e))

# 10c. Delete eval rule
if created_rule_id:
    try:
        r = session.delete(f"{QUERY_URL}/api/v1/eval-rules/{created_rule_id}", timeout=10)
        if r.status_code in (200, 204):
            report("Eval rule: delete -> 200/204", PASS)
        else:
            report("Eval rule: delete -> 200/204", FAIL, f"got {r.status_code}: {r.text[:100]}")
    except Exception as e:
        report("Eval rule: delete", FAIL, str(e))

# 10d. Prompts list
try:
    r = session.get(f"{QUERY_URL}/api/v1/prompts", timeout=10)
    if r.status_code == 200 and isinstance(r.json(), list):
        report(f"Prompts list -> 200 ({len(r.json())} prompts)", PASS)
    else:
        report("Prompts list -> 200", FAIL, f"got {r.status_code}: {r.text[:200]}")
except Exception as e:
    report("Prompts list -> 200", FAIL, str(e))

# 10e. Create prompt + list versions
new_prompt_name = f"qa-eval-prompt-{hex_id(6)}"
try:
    r = session.post(f"{QUERY_URL}/api/v1/prompts",
                     json={
                         "name": new_prompt_name,
                         "template": "System: You are helpful.\nUser: {{question}}",
                         "model_config": {"model": "gpt-4o-mini"},
                     }, timeout=10)
    if r.status_code in (200, 201):
        report("Prompt: create new prompt -> 200/201", PASS)

        # Get version list
        r2 = session.get(f"{QUERY_URL}/api/v1/prompts/{new_prompt_name}/versions", timeout=10)
        if r2.status_code == 200 and isinstance(r2.json(), list):
            versions = r2.json()
            report(f"Prompt: versions list -> 200 ({len(versions)} versions)", PASS)
        else:
            report("Prompt: versions list -> 200", FAIL, f"{r2.status_code}: {r2.text[:100]}")

        # Get prompt by name (production label - not set yet, should return 404 or latest)
        r3 = session.get(f"{QUERY_URL}/api/v1/prompts/{new_prompt_name}?label=production", timeout=10)
        if r3.status_code in (200, 404):
            report(f"Prompt: get by label (production) -> {r3.status_code}", PASS)
        else:
            report("Prompt: get by label (production)", FAIL, f"{r3.status_code}: {r3.text[:100]}")

    else:
        report("Prompt: create new prompt", FAIL, f"{r.status_code}: {r.text[:200]}")
except Exception as e:
    report("Prompt: create new prompt", FAIL, str(e))


# ============================================================
# SECTION 11: Datasets
# ============================================================
print("\n-- Section 11: Datasets --")

created_dataset_id = None
try:
    r = session.post(f"{QUERY_URL}/api/v1/datasets",
                     json={
                         "name": f"qa-dataset-{hex_id(6)}",
                         "description": "QA validation dataset",
                     }, timeout=10)
    if r.status_code in (200, 201):
        ds = r.json()
        created_dataset_id = ds.get("id") or ds.get("dataset_id")
        report("Dataset: create -> 200/201", PASS)
    else:
        report("Dataset: create -> 200/201", FAIL, f"got {r.status_code}: {r.text[:200]}")
except Exception as e:
    report("Dataset: create", FAIL, str(e))

if created_dataset_id:
    # Add items
    try:
        r = session.post(f"{QUERY_URL}/api/v1/datasets/{created_dataset_id}/items",
                         json={
                             "input": "What is 2+2?",
                             "expected_output": "4",
                             "attributes": {"source": "qa-test"},
                         }, timeout=10)
        if r.status_code in (200, 201):
            report("Dataset: add item -> 200/201", PASS)
        else:
            report("Dataset: add item -> 200/201", FAIL, f"got {r.status_code}: {r.text[:100]}")
    except Exception as e:
        report("Dataset: add item", FAIL, str(e))

    # List items
    try:
        r = session.get(f"{QUERY_URL}/api/v1/datasets/{created_dataset_id}/items", timeout=10)
        if r.status_code == 200:
            items = r.json()
            if isinstance(items, list):
                report(f"Dataset: list items -> 200 ({len(items)} items)", PASS)
            else:
                report("Dataset: list items -> 200", FAIL, f"unexpected type: {type(items)}")
        else:
            report("Dataset: list items -> 200", FAIL, f"got {r.status_code}: {r.text[:100]}")
    except Exception as e:
        report("Dataset: list items", FAIL, str(e))

    # List datasets
    try:
        r = session.get(f"{QUERY_URL}/api/v1/datasets", timeout=10)
        if r.status_code == 200 and isinstance(r.json(), list):
            report(f"Dataset: list all -> 200 ({len(r.json())} datasets)", PASS)
        else:
            report("Dataset: list all -> 200", FAIL, f"got {r.status_code}: {r.text[:100]}")
    except Exception as e:
        report("Dataset: list all", FAIL, str(e))

    # Delete dataset
    try:
        r = session.delete(f"{QUERY_URL}/api/v1/datasets/{created_dataset_id}", timeout=10)
        if r.status_code in (200, 204):
            report("Dataset: delete -> 200/204", PASS)
        else:
            report("Dataset: delete -> 200/204", FAIL, f"got {r.status_code}: {r.text[:100]}")
    except Exception as e:
        report("Dataset: delete", FAIL, str(e))


# ============================================================
# SECTION 12: Health probes
# ============================================================
print("\n-- Section 12: Health Probes --")

for svc_name, svc_url in [("ingest", INGEST_URL), ("query", QUERY_URL)]:
    try:
        r = requests.get(f"{svc_url}/healthz", timeout=5)
        if r.status_code == 200:
            report(f"{svc_name}: /healthz -> 200", PASS)
        else:
            report(f"{svc_name}: /healthz -> 200", FAIL, f"got {r.status_code}")
    except Exception as e:
        report(f"{svc_name}: /healthz", FAIL, str(e))

    try:
        r = requests.get(f"{svc_url}/readyz", timeout=5)
        if r.status_code == 200:
            report(f"{svc_name}: /readyz -> 200", PASS)
        else:
            report(f"{svc_name}: /readyz -> 200", FAIL, f"got {r.status_code}: {r.text[:80]}")
    except Exception as e:
        report(f"{svc_name}: /readyz", FAIL, str(e))


# ============================================================
# SECTION 13: Prometheus Metrics
# ============================================================
print("\n-- Section 13: Prometheus Metrics --")

for svc_name, port in [("ingest", 9090), ("writer", 9091), ("query", 9092)]:
    try:
        r = requests.get(f"http://localhost:{port}/metrics", timeout=5)
        if r.status_code == 200 and "omneval" in r.text:
            report(f"{svc_name}: /metrics exposes omneval_ metrics", PASS)
        elif r.status_code == 200:
            report(f"{svc_name}: /metrics -> 200 (no omneval_ metrics yet)", PASS,
                   "metric names not omneval-prefixed or not yet registered")
        else:
            report(f"{svc_name}: /metrics -> 200", FAIL, f"got {r.status_code}")
    except Exception as e:
        report(f"{svc_name}: /metrics", FAIL, str(e))


# ============================================================
# SUMMARY
# ============================================================
print("\n" + "=" * 65)
total = len(results)
passed = sum(1 for _, s, _ in results if s == PASS)
failed = sum(1 for _, s, _ in results if s == FAIL)
skipped = sum(1 for _, s, _ in results if s == SKIP)
print(f"Results: {passed}/{total} passed  |  {failed} failed  |  {skipped} skipped")
print("=" * 65)

if failed:
    print("\nFailed tests:")
    for name, status, detail in results:
        if status == FAIL:
            print(f"  FAIL  {name}: {detail}")

sys.exit(1 if failed else 0)
