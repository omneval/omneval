"""
Omneval QA Validation Script
=============================
Tests the Omneval platform end-to-end:
  1. Omneval Python SDK trace sending
  2. Raw OTLP HTTP trace sending
  3. Native REST ingest API
  4. Query API (auth, span query, trace detail, analytics)
  5. Eval rules and prompts management

Usage:
    uv run --with opentelemetry-sdk --with opentelemetry-exporter-otlp-proto-http \
           --with requests --with opentelemetry-semantic-conventions \
           qa_validation.py
"""
import json
import sys
import time
import uuid
import traceback

import requests

# ── Configuration ─────────────────────────────────────────────────────────────
API_KEY = "oev_proj_HqP4ESPKqdcTPqsj1eGG4vD2THLPL3tge8b85NHsBTKv"
PROJECT_ID = "d81rkuaodocs73apq400"
INGEST_URL = "http://localhost:8000"
QUERY_URL = "http://localhost:8002"
ADMIN_EMAIL = "admin@omneval.com"
ADMIN_PASSWORD = "admin"

PASS = "PASS"
FAIL = "FAIL"
SKIP = "SKIP"
results = []
ingested_trace_ids = []


def report(name, status, detail=""):
    icon = "✓" if status == PASS else ("⚠" if status == SKIP else "✗")
    line = f"  [{status}] {icon} {name}" + (f": {detail}" if detail else "")
    print(line)
    results.append((name, status, detail))


def hex_id(n):
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

# ═══════════════════════════════════════════════════════════════════
# SECTION 1: Omneval Python SDK
# ═══════════════════════════════════════════════════════════════════
print("\n── Section 1: Omneval Python SDK ─────────────────────────────")

try:
    sys.path.insert(0, "/c/Users/altoz/Projects/omneval/sdk/python")
    import omneval_sdk

    omneval_sdk.configure(endpoint=INGEST_URL, api_key=API_KEY)

    @omneval_sdk.trace
    def sdk_outer():
        span = omneval_sdk.get_active_span()
        if span:
            omneval_sdk.set_model(span, "gpt-4o")
            omneval_sdk.set_input(span, "QA SDK outer call: explain quantum computing")
            omneval_sdk.set_output(span, "Quantum computing uses quantum bits (qubits) that can be in superposition...")
            omneval_sdk.set_tokens(span, 75, 125)
        return sdk_inner()

    @omneval_sdk.trace
    def sdk_inner():
        span = omneval_sdk.get_active_span()
        if span:
            omneval_sdk.set_model(span, "gpt-4o-mini")
            omneval_sdk.set_input(span, "Summarize quantum computing in one sentence")
            omneval_sdk.set_output(span, "Quantum computers leverage quantum mechanics to process information exponentially faster.")
            omneval_sdk.set_tokens(span, 20, 45)
        return "sdk-inner-result"

    result = sdk_outer()
    if result == "sdk-inner-result":
        report("SDK: nested @trace decorators work", PASS)
    else:
        report("SDK: nested @trace decorators work", FAIL, f"unexpected result: {result!r}")

    # Test error handling
    @omneval_sdk.trace
    def sdk_error_fn():
        raise ValueError("intentional QA test error")

    try:
        sdk_error_fn()
        report("SDK: error span recorded", FAIL, "exception was swallowed")
    except ValueError:
        report("SDK: error span recorded (exception propagates)", PASS)

except ImportError as e:
    report("SDK: import", SKIP, f"omneval_sdk not importable: {e}")
except Exception as e:
    report("SDK: unexpected error", FAIL, traceback.format_exc()[:300])

# ═══════════════════════════════════════════════════════════════════
# SECTION 2: OTLP HTTP Direct
# ═══════════════════════════════════════════════════════════════════
print("\n── Section 2: OTLP HTTP Ingest (/v1/traces) ──────────────────")

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
except ImportError as e:
    report("OTLP: import", SKIP, f"opentelemetry not installed: {e}")
except Exception as e:
    report("OTLP: export failed", FAIL, str(e)[:200])

# ═══════════════════════════════════════════════════════════════════
# SECTION 3: Native REST Ingest API
# ═══════════════════════════════════════════════════════════════════
print("\n── Section 3: Native REST Ingest (/api/v1/spans) ─────────────")

# 3a. Auth: no API key → 401
try:
    r = requests.post(f"{INGEST_URL}/api/v1/spans",
                      json={"spans": [make_span("auth-test")]}, timeout=10)
    if r.status_code == 401:
        report("Auth: missing API key → 401", PASS)
    else:
        report("Auth: missing API key → 401", FAIL, f"got {r.status_code}: {r.text[:100]}")
except Exception as e:
    report("Auth: missing API key → 401", FAIL, str(e))

# 3b. Auth: bad API key → 401
try:
    r = requests.post(f"{INGEST_URL}/api/v1/spans",
                      headers={"X-API-Key": "oev_proj_" + "x" * 43},
                      json={"spans": [make_span("bad-key")]}, timeout=10)
    if r.status_code == 401:
        report("Auth: invalid API key → 401", PASS)
    else:
        report("Auth: invalid API key → 401", FAIL, f"got {r.status_code}: {r.text[:100]}")
except Exception as e:
    report("Auth: invalid API key → 401", FAIL, str(e))

# 3c. Single LLM span
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

# 3d. Parent-child trace
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

# 3e. All span kinds
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

# ═══════════════════════════════════════════════════════════════════
# SECTION 4: Query API — Auth
# ═══════════════════════════════════════════════════════════════════
print("\n── Section 4: Query API — Auth ────────────────────────────────")

session = requests.Session()

try:
    r = session.post(f"{QUERY_URL}/login",
                     json={"email": ADMIN_EMAIL, "password": ADMIN_PASSWORD}, timeout=10)
    if r.status_code == 200 and "session_id" in r.json():
        report("Login → 200 with session_id", PASS)
    else:
        report("Login → 200 with session_id", FAIL, f"got {r.status_code}: {r.text[:200]}")
except Exception as e:
    report("Login → 200 with session_id", FAIL, str(e))

try:
    r = requests.post(f"{QUERY_URL}/login",
                      json={"email": "nobody@example.com", "password": "wrong"}, timeout=10)
    if r.status_code == 401:
        report("Bad credentials → 401", PASS)
    else:
        report("Bad credentials → 401", FAIL, f"got {r.status_code}: {r.text[:100]}")
except Exception as e:
    report("Bad credentials → 401", FAIL, str(e))

try:
    r = requests.post(f"{QUERY_URL}/api/v1/spans/query", json={}, timeout=10)
    if r.status_code == 401:
        report("Unauthenticated span query → 401", PASS)
    else:
        report("Unauthenticated span query → 401", FAIL, f"got {r.status_code}")
except Exception as e:
    report("Unauthenticated span query → 401", FAIL, str(e))

# ═══════════════════════════════════════════════════════════════════
# SECTION 5: Query API — Projects & API Keys
# ═══════════════════════════════════════════════════════════════════
print("\n── Section 5: Projects & API Keys ────────────────────────────")

try:
    r = session.get(f"{QUERY_URL}/api/v1/projects", timeout=10)
    if r.status_code == 200:
        projects = r.json()
        if isinstance(projects, list) and len(projects) > 0:
            first = projects[0]
            if "project_id" in first:
                report(f"Projects list: snake_case JSON keys ({len(projects)} projects)", PASS)
            else:
                report("Projects list: snake_case JSON keys", FAIL,
                       f"unexpected keys: {list(first.keys())}")
        else:
            report("Projects list not empty", FAIL, f"got: {projects}")
    else:
        report("Projects list → 200", FAIL, f"got {r.status_code}: {r.text[:200]}")
except Exception as e:
    report("Projects list → 200", FAIL, str(e))

try:
    r = session.get(f"{QUERY_URL}/api/v1/projects/{PROJECT_ID}/api-keys", timeout=10)
    if r.status_code == 200 and isinstance(r.json(), list):
        report(f"API keys list → 200 ({len(r.json())} keys)", PASS)
    else:
        report("API keys list → 200", FAIL, f"got {r.status_code}: {r.text[:200]}")
except Exception as e:
    report("API keys list → 200", FAIL, str(e))

# ═══════════════════════════════════════════════════════════════════
# SECTION 6: Query API — Span Queries (wait for snapshot)
# ═══════════════════════════════════════════════════════════════════
print("\n── Section 6: Span Query ──────────────────────────────────────")
print("  [waiting 10s for writer sync interval...]")
time.sleep(10)

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
            report(f"Span query with time range: {count} spans returned", PASS)
        else:
            report("Span query with time range returns spans", FAIL,
                   "0 spans — snapshot may not yet include new data (writer syncs every 30s)")
    else:
        report("Span query with time range → 200", FAIL, f"got {r.status_code}: {r.text[:200]}")
except Exception as e:
    report("Span query with time range → 200", FAIL, str(e))

# Filter by model
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
        report("Span query filter by model → 200", FAIL, f"got {r.status_code}: {r.text[:200]}")
except Exception as e:
    report("Span query filter by model → 200", FAIL, str(e))

# No time range (known potential bug)
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
                   "BUG: 0 spans — zero Go time.Time likely defaults to 0001-01-01 range")
    else:
        report("Span query without time range → 200", FAIL, f"got {r.status_code}: {r.text[:200]}")
except Exception as e:
    report("Span query without time range → 200", FAIL, str(e))

# ═══════════════════════════════════════════════════════════════════
# SECTION 7: Trace Detail
# ═══════════════════════════════════════════════════════════════════
print("\n── Section 7: Trace Detail ────────────────────────────────────")

trace_id_for_detail = None
try:
    r = session.post(f"{QUERY_URL}/api/v1/spans/query",
                     json={"from": "2026-01-01T00:00:00Z", "to": "2026-12-31T23:59:59Z", "limit": 1},
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
                report(f"Trace detail: root_span + spans present", PASS)
            else:
                report("Trace detail: missing expected fields", FAIL, f"keys: {list(detail.keys())}")
        else:
            report("Trace detail → 200", FAIL, f"got {r.status_code}: {r.text[:200]}")
    except Exception as e:
        report("Trace detail → 200", FAIL, str(e))

    try:
        r = session.get(f"{QUERY_URL}/api/v1/traces/{'f'*32}", timeout=10)
        if r.status_code == 404:
            report("Trace detail: missing trace → 404", PASS)
        else:
            report("Trace detail: missing trace → 404", FAIL, f"got {r.status_code}: {r.text[:100]}")
    except Exception as e:
        report("Trace detail: missing trace → 404", FAIL, str(e))
else:
    report("Trace detail", SKIP, "no spans available (snapshot not yet populated)")

# ═══════════════════════════════════════════════════════════════════
# SECTION 8: Analytics
# ═══════════════════════════════════════════════════════════════════
print("\n── Section 8: Analytics ───────────────────────────────────────")

try:
    r = session.post(f"{QUERY_URL}/api/v1/analytics/spans", json={}, timeout=10)
    if r.status_code == 200 and "rows" in r.json():
        report("Analytics: empty request returns rows", PASS, f"rows={r.json()['rows']}")
    else:
        report("Analytics: empty request → 200", FAIL, f"got {r.status_code}: {r.text[:200]}")
except Exception as e:
    report("Analytics: empty request → 200", FAIL, str(e))

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
        report("Analytics: group by model → 200", FAIL, f"got {r.status_code}: {r.text[:200]}")
except Exception as e:
    report("Analytics: group by model → 200", FAIL, str(e))

try:
    r = session.post(f"{QUERY_URL}/api/v1/analytics/spans",
                     json={
                         "aggregations": [{"function": "notafunc", "field": "cost_usd", "alias": "x"}],
                     }, timeout=10)
    if r.status_code == 400:
        report("Analytics: unknown aggregation function → 400", PASS)
    else:
        report("Analytics: unknown aggregation function → 400", FAIL, f"got {r.status_code}: {r.text[:100]}")
except Exception as e:
    report("Analytics: unknown function → 400", FAIL, str(e))

# ═══════════════════════════════════════════════════════════════════
# SECTION 9: Eval Rules & Prompts
# ═══════════════════════════════════════════════════════════════════
print("\n── Section 9: Eval Rules & Prompts ───────────────────────────")

try:
    r = session.get(f"{QUERY_URL}/api/v1/eval-rules", timeout=10)
    if r.status_code == 200 and isinstance(r.json().get("rules"), list):
        report(f"Eval rules list → 200 ({len(r.json()['rules'])} rules)", PASS)
    else:
        report("Eval rules list → 200", FAIL, f"got {r.status_code}: {r.text[:200]}")
except Exception as e:
    report("Eval rules list → 200", FAIL, str(e))

try:
    r = session.get(f"{QUERY_URL}/api/v1/prompts", timeout=10)
    if r.status_code == 200 and isinstance(r.json(), list):
        report(f"Prompts list → 200 ({len(r.json())} prompts)", PASS)
    else:
        report("Prompts list → 200", FAIL, f"got {r.status_code}: {r.text[:200]}")
except Exception as e:
    report("Prompts list → 200", FAIL, str(e))

# ═══════════════════════════════════════════════════════════════════
# SUMMARY
# ═══════════════════════════════════════════════════════════════════
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
            print(f"  ✗ {name}: {detail}")

sys.exit(1 if failed else 0)
