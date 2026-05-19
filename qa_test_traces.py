"""
Lantern QA Test Script
======================
Sends traces to Lantern via the native REST API and OTLP/OTel path,
then validates the data is queryable through the Query API.

Prerequisites:
    pip install requests opentelemetry-sdk opentelemetry-exporter-otlp-proto-http

Usage:
    python qa_test_traces.py

The stack must be running at localhost:8000 (ingest) and localhost:8002 (query).
Set API_KEY and PROJECT_ID below to match your environment.
"""
import json
import time
import uuid
import sys
import datetime

try:
    import requests
except ImportError:
    print("ERROR: requests not installed. Run: pip install requests")
    sys.exit(1)

# ── Configuration ──────────────────────────────────────────────────────────────
API_KEY = "ltn_proj_83JH6C61EWBw7KAGDkpKw6X7eqy8fHLZeT3XaHp7o2CH"
PROJECT_ID = "d81rkuaodocs73apq400"
INGEST_URL = "http://localhost:8000"
QUERY_URL = "http://localhost:8002"
ADMIN_EMAIL = "admin@omneval.com"
ADMIN_PASSWORD = "admin"

PASS = "PASS"
FAIL = "FAIL"
SKIP = "SKIP"
results = []


def report(name, status, detail=""):
    icon = "✓" if status == PASS else ("⚠" if status == SKIP else "✗")
    print(f"  [{status}] {icon} {name}" + (f": {detail}" if detail else ""))
    results.append((name, status, detail))


def hex_span_id():
    return uuid.uuid4().hex[:16]


def hex_trace_id():
    return uuid.uuid4().hex[:32]


def make_span(name, kind="llm", model="gpt-4o", parent_id=None, trace_id=None):
    return {
        "span_id": hex_span_id(),
        "trace_id": trace_id or hex_trace_id(),
        "name": name,
        "kind": kind,
        "model": model,
        "input": f"User asked: what is {name}?",
        "output": f"Assistant responded about {name}.",
        "input_tokens": 25,
        "output_tokens": 50,
        "attributes": {"env": "qa-test"},
        **({"parent_id": parent_id} if parent_id else {}),
    }


def ingest_headers():
    return {"X-API-Key": API_KEY, "Content-Type": "application/json"}


print("=" * 65)
print("Lantern QA Test Suite")
print("=" * 65)

# ═══════════════════════════════════════════════════════════════════
# SECTION 1: Ingest API — Native REST
# ═══════════════════════════════════════════════════════════════════
print("\n── Section 1: Native REST Ingest (/api/v1/spans) ──────────────")

# 1a. Auth rejection — no API key
try:
    r = requests.post(f"{INGEST_URL}/api/v1/spans",
                      json={"spans": [make_span("auth-test")]}, timeout=10)
    if r.status_code == 401:
        report("Auth: no API key rejected with 401", PASS)
    else:
        report("Auth: no API key rejected with 401", FAIL, f"got {r.status_code}: {r.text[:200]}")
except Exception as e:
    report("Auth: no API key rejected with 401", FAIL, str(e))

# 1b. Auth rejection — bad API key
try:
    r = requests.post(f"{INGEST_URL}/api/v1/spans",
                      headers={"X-API-Key": "ltn_proj_invalid00000000000000000000000000000000000"},
                      json={"spans": [make_span("bad-key-test")]}, timeout=10)
    if r.status_code == 401:
        report("Auth: invalid API key rejected with 401", PASS)
    else:
        report("Auth: invalid API key rejected with 401", FAIL, f"got {r.status_code}: {r.text[:200]}")
except Exception as e:
    report("Auth: invalid API key rejected with 401", FAIL, str(e))

# 1c. Validation — span_id must be 16-char hex
try:
    bad = make_span("invalid-span-id")
    bad["span_id"] = "tooshort"
    r = requests.post(f"{INGEST_URL}/api/v1/spans",
                      headers=ingest_headers(), json={"spans": [bad]}, timeout=10)
    if r.status_code == 400:
        report("Validation: span_id < 16 hex chars rejected with 400", PASS)
    else:
        report("Validation: span_id < 16 hex chars rejected with 400", FAIL, f"got {r.status_code}: {r.text[:200]}")
except Exception as e:
    report("Validation: span_id < 16 hex chars rejected with 400", FAIL, str(e))

# 1d. Validation — trace_id must be 32-char hex
try:
    bad = make_span("invalid-trace-id")
    bad["trace_id"] = "short"
    r = requests.post(f"{INGEST_URL}/api/v1/spans",
                      headers=ingest_headers(), json={"spans": [bad]}, timeout=10)
    if r.status_code == 400:
        report("Validation: trace_id < 32 hex chars rejected with 400", PASS)
    else:
        report("Validation: trace_id < 32 hex chars rejected with 400", FAIL, f"got {r.status_code}: {r.text[:200]}")
except Exception as e:
    report("Validation: trace_id < 32 hex chars rejected with 400", FAIL, str(e))

# 1e. Single LLM span accepted
try:
    span = make_span("qa-single-llm", kind="llm", model="gpt-4o")
    sent_trace_id = span["trace_id"]
    r = requests.post(f"{INGEST_URL}/api/v1/spans",
                      headers=ingest_headers(), json={"spans": [span]}, timeout=10)
    if r.status_code == 202:
        report("Single LLM span accepted (202)", PASS, f"trace_id={sent_trace_id}")
    else:
        report("Single LLM span accepted (202)", FAIL, f"got {r.status_code}: {r.text[:200]}")
except Exception as e:
    report("Single LLM span accepted (202)", FAIL, str(e))

# 1f. Parent-child trace (agent → tool)
try:
    root_trace_id = hex_trace_id()
    root_span = make_span("qa-agent-root", kind="agent", model="gpt-4o", trace_id=root_trace_id)
    child_span = make_span("qa-tool-call", kind="tool", trace_id=root_trace_id,
                            parent_id=root_span["span_id"])
    child_span["model"] = ""  # tools may have no model

    r = requests.post(f"{INGEST_URL}/api/v1/spans",
                      headers=ingest_headers(),
                      json={"spans": [root_span, child_span]}, timeout=10)
    if r.status_code == 202:
        report("Parent-child trace batch accepted (202)", PASS, f"trace_id={root_trace_id}")
    else:
        report("Parent-child trace batch accepted (202)", FAIL, f"got {r.status_code}: {r.text[:200]}")
except Exception as e:
    report("Parent-child trace batch accepted (202)", FAIL, str(e))

# 1g. All span kinds accepted
try:
    kinds = ["llm", "agent", "tool", "chain", "internal"]
    spans = [make_span(f"qa-kind-{k}", kind=k) for k in kinds]
    r = requests.post(f"{INGEST_URL}/api/v1/spans",
                      headers=ingest_headers(), json={"spans": spans}, timeout=10)
    if r.status_code == 202:
        report("All span kinds accepted in one batch (202)", PASS)
    else:
        report("All span kinds accepted in one batch (202)", FAIL, f"got {r.status_code}: {r.text[:200]}")
except Exception as e:
    report("All span kinds accepted in one batch (202)", FAIL, str(e))

# ═══════════════════════════════════════════════════════════════════
# SECTION 2: OTLP HTTP endpoint
# ═══════════════════════════════════════════════════════════════════
print("\n── Section 2: OTLP HTTP Ingest (/v1/traces) ──────────────────")

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

    with tracer.start_as_current_span("otlp-qa-root") as root:
        root.set_attribute("gen_ai.request.model", "claude-3-5-sonnet")
        root.set_attribute("gen_ai.usage.input_tokens", 100)
        root.set_attribute("gen_ai.usage.output_tokens", 200)
        root.set_attribute("gen_ai.prompt.0.role", "user")
        root.set_attribute("gen_ai.prompt.0.content", "Hello from OTLP QA test")
        root.set_attribute("gen_ai.completion.0.role", "assistant")
        root.set_attribute("gen_ai.completion.0.content", "OTLP trace received")

        with tracer.start_as_current_span("otlp-qa-child") as child:
            child.set_attribute("gen_ai.request.model", "claude-3-haiku")
            child.set_attribute("gen_ai.usage.input_tokens", 50)
            child.set_attribute("gen_ai.usage.output_tokens", 30)

    provider.shutdown()
    report("OTLP parent-child span exported without error", PASS)

except ImportError as e:
    report("OTLP export", SKIP, f"opentelemetry not installed ({e}). pip install opentelemetry-sdk opentelemetry-exporter-otlp-proto-http")

# ═══════════════════════════════════════════════════════════════════
# SECTION 3: Lantern Python SDK
# ═══════════════════════════════════════════════════════════════════
print("\n── Section 3: Lantern Python SDK ─────────────────────────────")

try:
    import lantern_sdk

    lantern_sdk.configure(endpoint=INGEST_URL, api_key=API_KEY)

    @lantern_sdk.trace
    def sdk_outer():
        span = lantern_sdk.get_active_span()
        if span:
            lantern_sdk.set_model(span, "gpt-4o")
            lantern_sdk.set_input(span, "QA SDK outer call input")
            lantern_sdk.set_output(span, "QA SDK outer call output")
            lantern_sdk.set_tokens(span, 75, 125)
        return sdk_inner()

    @lantern_sdk.trace
    def sdk_inner():
        span = lantern_sdk.get_active_span()
        if span:
            lantern_sdk.set_model(span, "gpt-4o-mini")
            lantern_sdk.set_input(span, "QA SDK inner call input")
            lantern_sdk.set_output(span, "QA SDK inner call output")
        return "sdk-inner-result"

    result = sdk_outer()
    if result == "sdk-inner-result":
        report("Lantern SDK: nested trace decorators work", PASS)
    else:
        report("Lantern SDK: nested trace decorators work", FAIL, f"unexpected result: {result!r}")

except ImportError as e:
    report("Lantern SDK", SKIP, f"lantern_sdk not installed ({e}). pip install -e sdk/python")

# ═══════════════════════════════════════════════════════════════════
# SECTION 4: Query API — Authentication
# ═══════════════════════════════════════════════════════════════════
print("\n── Section 4: Query API — Auth ────────────────────────────────")

session = requests.Session()

# 4a. Login
try:
    r = session.post(f"{QUERY_URL}/login",
                     json={"email": ADMIN_EMAIL, "password": ADMIN_PASSWORD}, timeout=10)
    if r.status_code == 200 and "session_id" in r.json():
        report("Login returns 200 with session_id", PASS)
    else:
        report("Login returns 200 with session_id", FAIL, f"got {r.status_code}: {r.text[:200]}")
except Exception as e:
    report("Login returns 200 with session_id", FAIL, str(e))

# 4b. Bad credentials
try:
    r = requests.post(f"{QUERY_URL}/login",
                      json={"email": "noone@example.com", "password": "wrong"}, timeout=10)
    if r.status_code == 401:
        report("Login: bad credentials rejected with 401", PASS)
    else:
        report("Login: bad credentials rejected with 401", FAIL, f"got {r.status_code}: {r.text[:200]}")
except Exception as e:
    report("Login: bad credentials rejected with 401", FAIL, str(e))

# 4c. Projects list
try:
    r = session.get(f"{QUERY_URL}/api/v1/projects", timeout=10)
    if r.status_code == 200:
        projects = r.json()
        has_snake = any("project_id" in p for p in projects)
        has_pascal = any("ProjectID" in p for p in projects)
        if has_snake:
            report("Projects list: snake_case JSON keys", PASS)
        elif has_pascal:
            report("Projects list: snake_case JSON keys", FAIL,
                   "Response uses PascalCase (ProjectID, OrgID) — domain.Project struct missing JSON tags")
        else:
            report("Projects list: snake_case JSON keys", FAIL, f"unexpected format: {projects[:1]}")
    else:
        report("Projects list returns 200", FAIL, f"got {r.status_code}: {r.text[:200]}")
except Exception as e:
    report("Projects list returns 200", FAIL, str(e))

# 4d. Unauthenticated span query rejected
try:
    r = requests.post(f"{QUERY_URL}/api/v1/spans/query",
                      json={}, timeout=10)
    if r.status_code == 401:
        report("Span query: unauthenticated request rejected with 401", PASS)
    else:
        report("Span query: unauthenticated request rejected with 401", FAIL, f"got {r.status_code}")
except Exception as e:
    report("Span query: unauthenticated request rejected with 401", FAIL, str(e))

# ═══════════════════════════════════════════════════════════════════
# SECTION 5: Query API — Span Query
# ═══════════════════════════════════════════════════════════════════
print("\n── Section 5: Query API — Span Query ─────────────────────────")

# Wait briefly for the snapshot to propagate (writer→S3→query)
print("  [waiting 5s for snapshot propagation...]")
time.sleep(5)

# 5a. Query with explicit time range returns spans
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
            report("Span query with time range returns spans", PASS, f"{count} spans returned")
        else:
            report("Span query with time range returns spans", FAIL, "0 spans — data may not yet be in snapshot")
    else:
        report("Span query with time range returns spans", FAIL, f"got {r.status_code}: {r.text[:200]}")
except Exception as e:
    report("Span query with time range returns spans", FAIL, str(e))

# 5b. Query without time range (known bug: returns empty due to zero time default)
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
                   "BUG: returns empty — zero Go time.Time creates 'start_time >= 0001-01-01 AND start_time <= 0001-01-01'")
    else:
        report("Span query without time range returns spans", FAIL, f"got {r.status_code}: {r.text[:200]}")
except Exception as e:
    report("Span query without time range returns spans", FAIL, str(e))

# 5c. Filter by model
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
        all_match = all(s.get("model") == "gpt-4o" for s in spans)
        if all_match:
            report("Span query filter by model works", PASS, f"{len(spans)} gpt-4o spans")
        else:
            bad = [s.get("model") for s in spans if s.get("model") != "gpt-4o"]
            report("Span query filter by model works", FAIL, f"non-gpt-4o spans in result: {bad}")
    else:
        report("Span query filter by model works", FAIL, f"got {r.status_code}: {r.text[:200]}")
except Exception as e:
    report("Span query filter by model works", FAIL, str(e))

# 5d. Invalid filter field returns 400
try:
    r = session.post(f"{QUERY_URL}/api/v1/spans/query",
                     json={
                         "filters": [{"field": "nonexistent_field", "op": "eq", "value": "x"}],
                     }, timeout=10)
    if r.status_code == 400:
        report("Span query: invalid filter field rejected with 400", PASS)
    else:
        report("Span query: invalid filter field rejected with 400", FAIL, f"got {r.status_code}: {r.text[:200]}")
except Exception as e:
    report("Span query: invalid filter field rejected with 400", FAIL, str(e))

# 5e. filters must be array (object should fail)
try:
    r = session.post(f"{QUERY_URL}/api/v1/spans/query",
                     json={"filters": {"field": "model", "op": "eq", "value": "x"}},
                     timeout=10)
    if r.status_code == 400:
        report("Span query: filters as object (not array) rejected with 400", PASS)
    else:
        report("Span query: filters as object (not array) rejected with 400", FAIL, f"got {r.status_code}: {r.text[:200]}")
except Exception as e:
    report("Span query: filters as object (not array) rejected with 400", FAIL, str(e))

# ═══════════════════════════════════════════════════════════════════
# SECTION 6: Query API — Trace Detail
# ═══════════════════════════════════════════════════════════════════
print("\n── Section 6: Query API — Trace Detail ───────────────────────")

# Get a trace_id from the span query to use for trace detail
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
                report("Trace detail returns root_span and spans", PASS, f"trace_id={trace_id_for_detail}")
            else:
                report("Trace detail returns root_span and spans", FAIL, f"missing fields: {detail.keys()}")
        else:
            report("Trace detail returns 200", FAIL, f"got {r.status_code}: {r.text[:200]}")
    except Exception as e:
        report("Trace detail returns 200", FAIL, str(e))

    try:
        r = session.get(f"{QUERY_URL}/api/v1/traces/ffffffffffffffffffffffffffffffff", timeout=10)
        if r.status_code == 404:
            report("Trace detail: missing trace returns 404", PASS)
        else:
            report("Trace detail: missing trace returns 404", FAIL, f"got {r.status_code}: {r.text[:200]}")
    except Exception as e:
        report("Trace detail: missing trace returns 404", FAIL, str(e))
else:
    report("Trace detail", SKIP, "no spans available to get a trace_id")

# ═══════════════════════════════════════════════════════════════════
# SECTION 7: Analytics
# ═══════════════════════════════════════════════════════════════════
print("\n── Section 7: Analytics (/api/v1/analytics/spans) ────────────")

# 7a. Empty request returns COUNT(*) default
try:
    r = session.post(f"{QUERY_URL}/api/v1/analytics/spans", json={}, timeout=10)
    if r.status_code == 200 and "rows" in r.json():
        report("Analytics: empty request returns count row", PASS, f"rows={r.json()['rows']}")
    else:
        report("Analytics: empty request returns count row", FAIL, f"got {r.status_code}: {r.text[:200]}")
except Exception as e:
    report("Analytics: empty request returns count row", FAIL, str(e))

# 7b. Group by model
try:
    r = session.post(f"{QUERY_URL}/api/v1/analytics/spans",
                     json={
                         "aggregations": [{"function": "count", "field": "*", "alias": "span_count"}],
                         "group_by": [{"field": "model"}],
                         "order_by": [{"field": "span_count", "desc": True}],
                     }, timeout=10)
    if r.status_code == 200:
        rows = r.json().get("rows", [])
        report("Analytics: group by model", PASS, f"{len(rows)} model groups")
    else:
        report("Analytics: group by model", FAIL, f"got {r.status_code}: {r.text[:200]}")
except Exception as e:
    report("Analytics: group by model", FAIL, str(e))

# 7c. Sum cost_usd
try:
    r = session.post(f"{QUERY_URL}/api/v1/analytics/spans",
                     json={
                         "aggregations": [{"function": "sum", "field": "cost_usd", "alias": "total_cost"}],
                     }, timeout=10)
    if r.status_code == 200:
        report("Analytics: sum cost_usd", PASS)
    else:
        report("Analytics: sum cost_usd", FAIL, f"got {r.status_code}: {r.text[:200]}")
except Exception as e:
    report("Analytics: sum cost_usd", FAIL, str(e))

# 7d. Unknown aggregation function returns error
try:
    r = session.post(f"{QUERY_URL}/api/v1/analytics/spans",
                     json={
                         "aggregations": [{"function": "notafunc", "field": "cost_usd", "alias": "x"}],
                     }, timeout=10)
    if r.status_code == 400:
        report("Analytics: unknown function rejected with 400", PASS)
    else:
        report("Analytics: unknown function rejected with 400", FAIL, f"got {r.status_code}: {r.text[:200]}")
except Exception as e:
    report("Analytics: unknown function rejected with 400", FAIL, str(e))

# ═══════════════════════════════════════════════════════════════════
# SECTION 8: Eval Rules + Prompts
# ═══════════════════════════════════════════════════════════════════
print("\n── Section 8: Eval Rules and Prompts ─────────────────────────")

try:
    r = session.get(f"{QUERY_URL}/api/v1/eval-rules", timeout=10)
    if r.status_code == 200 and isinstance(r.json().get("rules"), list):
        report("Eval rules list returns 200", PASS, f"{len(r.json()['rules'])} rules")
    else:
        report("Eval rules list returns 200", FAIL, f"got {r.status_code}: {r.text[:200]}")
except Exception as e:
    report("Eval rules list returns 200", FAIL, str(e))

try:
    r = session.get(f"{QUERY_URL}/api/v1/prompts", timeout=10)
    if r.status_code == 200 and isinstance(r.json(), list):
        report("Prompts list returns 200", PASS, f"{len(r.json())} prompts")
    else:
        report("Prompts list returns 200", FAIL, f"got {r.status_code}: {r.text[:200]}")
except Exception as e:
    report("Prompts list returns 200", FAIL, str(e))

# ═══════════════════════════════════════════════════════════════════
# SECTION 9: API Key Management
# ═══════════════════════════════════════════════════════════════════
print("\n── Section 9: API Key Management ─────────────────────────────")

try:
    r = session.get(f"{QUERY_URL}/api/v1/projects/{PROJECT_ID}/api-keys", timeout=10)
    if r.status_code == 200 and isinstance(r.json(), list):
        report("List API keys returns 200", PASS, f"{len(r.json())} keys")
    else:
        report("List API keys returns 200", FAIL, f"got {r.status_code}: {r.text[:200]}")
except Exception as e:
    report("List API keys returns 200", FAIL, str(e))

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

print(f"\nWait ~30s then check the UI at http://localhost:8002 to see ingested spans.")
sys.exit(1 if failed else 0)
