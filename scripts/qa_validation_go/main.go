// qa_validation_go — End-to-end QA for the Omneval Go SDK.
//
// Runs a suite of tests against a locally running Omneval stack
// (ingest at :8000, query at :8002).
//
// Usage:
//
//	cd scripts/qa_validation_go
//	go run main.go
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	omneval "github.com/omneval/omneval/sdk/go/omneval"
)

// ─────────────────────────────── Config ───────────────────────────────────

const (
	apiKey    = "oev_proj_HqP4ESPKqdcTPqsj1eGG4vD2THLPL3tge8b85NHsBTKv"
	ingestURL = "http://localhost:8000"
	queryURL  = "http://localhost:8002"
	adminUser = "admin@omneval.com"
	adminPass = "admin"
)

// ─────────────────────────────── Helpers ──────────────────────────────────

type result struct {
	name   string
	status string
	detail string
}

var results []result

func pass(name string, detail ...string) {
	d := ""
	if len(detail) > 0 {
		d = detail[0]
	}
	msg := fmt.Sprintf("  [PASS] %s", name)
	if d != "" {
		msg += ": " + d
	}
	fmt.Println(msg)
	results = append(results, result{name, "PASS", d})
}

func fail(name, detail string) {
	fmt.Printf("  [FAIL] %s: %s\n", name, detail)
	results = append(results, result{name, "FAIL", detail})
}

func skip(name, reason string) {
	fmt.Printf("  [SKIP] %s: %s\n", name, reason)
	results = append(results, result{name, "SKIP", reason})
}

func jsonPost(client *http.Client, url string, body interface{}, headers map[string]string) (*http.Response, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest("POST", url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return client.Do(req)
}

func jsonGet(client *http.Client, url string) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	return client.Do(req)
}

func readBody(r *http.Response) string {
	if r == nil {
		return ""
	}
	b, _ := io.ReadAll(r.Body)
	r.Body.Close()
	return string(b)
}

func ingestHeaders() map[string]string {
	return map[string]string{"X-API-Key": apiKey}
}

// makeSpan builds a minimal native REST span payload.
func makeSpan(name, kind, model string) map[string]interface{} {
	return map[string]interface{}{
		"span_id":       randomHex(16),
		"trace_id":      randomHex(32),
		"name":          name,
		"kind":          kind,
		"model":         model,
		"input":         "User asked: " + name,
		"output":        "Assistant responded about " + name,
		"input_tokens":  25,
		"output_tokens": 50,
		"attributes":    map[string]string{"env": "go-qa"},
	}
}

func randomHex(n int) string {
	b := make([]byte, n/2+1)
	// Use time-based pseudo-random for simplicity (no crypto needed for test IDs)
	seed := time.Now().UnixNano()
	for i := range b {
		seed = seed*6364136223846793005 + 1442695040888963407
		b[i] = byte(seed >> 56)
	}
	return fmt.Sprintf("%x", b)[:n]
}

// ─────────────────────────────── Main ─────────────────────────────────────

func main() {
	fmt.Println(strings.Repeat("=", 65))
	fmt.Println("Omneval Go SDK QA Validation Suite")
	fmt.Println(strings.Repeat("=", 65))

	httpClient := &http.Client{Timeout: 15 * time.Second}

	// ── Section 1: Go SDK – Configure & Basic Span ────────────────
	fmt.Println("\n-- Section 1: Go SDK Basic Spans --")

	// 1a. Configure with valid endpoint
	if err := omneval.Configure(ingestURL, apiKey); err != nil {
		fail("SDK: Configure() valid endpoint", err.Error())
	} else {
		pass("SDK: Configure() valid endpoint")
	}

	// 1b. Configure with bad URL returns error
	if err := omneval.Configure("://bad-url", apiKey); err != nil {
		pass("SDK: Configure() bad URL returns error")
	} else {
		fail("SDK: Configure() bad URL returns error", "expected error, got nil")
	}
	// Reconfigure with good endpoint
	omneval.Configure(ingestURL, apiKey)

	// 1c. StartSpan + EndSpan without panic
	ctx := omneval.StartSpan(context.Background(), "go-qa-basic")
	omneval.SetModel(ctx, "gpt-4o")
	omneval.SetInput(ctx, "What is Go?")
	omneval.SetOutput(ctx, "Go is a statically typed, compiled language.")
	omneval.SetTokens(ctx, 20, 40)
	omneval.EndSpan(ctx)
	pass("SDK: StartSpan/SetModel/SetInput/SetOutput/SetTokens/EndSpan without panic")

	// 1d. EndSpan on background context is safe (no span)
	func() {
		defer func() {
			if r := recover(); r != nil {
				fail("SDK: EndSpan on empty context is safe", fmt.Sprintf("panic: %v", r))
			} else {
				pass("SDK: EndSpan on empty context is safe")
			}
		}()
		omneval.EndSpan(context.Background())
	}()

	// ── Section 2: Go SDK – Parent-Child Spans ────────────────────
	fmt.Println("\n-- Section 2: Go SDK Parent-Child Spans --")

	// 2a. Child spans share parent context
	parentCtx := omneval.StartSpan(context.Background(), "go-qa-parent")
	omneval.SetModel(parentCtx, "gpt-4o")
	omneval.SetInput(parentCtx, "Parent: research climate change")
	omneval.SetTokens(parentCtx, 100, 200)

	child1Ctx := omneval.StartSpan(parentCtx, "go-qa-child-search")
	omneval.SetModel(child1Ctx, "claude-3-5-sonnet")
	omneval.SetInput(child1Ctx, "search('climate change CO2 levels')")
	omneval.SetOutput(child1Ctx, "CO2 levels are at 421 ppm")
	omneval.SetTokens(child1Ctx, 30, 50)
	omneval.EndSpan(child1Ctx)

	child2Ctx := omneval.StartSpan(parentCtx, "go-qa-child-summarize")
	omneval.SetModel(child2Ctx, "gpt-4o-mini")
	omneval.SetInput(child2Ctx, "Summarize the research findings")
	omneval.SetOutput(child2Ctx, "Climate change is causing CO2 to rise.")
	omneval.SetTokens(child2Ctx, 50, 100)
	omneval.EndSpan(child2Ctx)

	grandchildCtx := omneval.StartSpan(child2Ctx, "go-qa-grandchild")
	omneval.SetModel(grandchildCtx, "gpt-4o-mini")
	omneval.SetInput(grandchildCtx, "Translate to French")
	omneval.SetTokens(grandchildCtx, 15, 20)
	omneval.EndSpan(grandchildCtx)

	omneval.SetOutput(parentCtx, "Climate change research complete.")
	omneval.EndSpan(parentCtx)
	pass("SDK: 3-level nested spans (parent+2 children+grandchild)")

	// 2b. Multiple sequential root spans
	for i := 0; i < 5; i++ {
		c := omneval.StartSpan(context.Background(), fmt.Sprintf("go-qa-seq-%d", i))
		omneval.SetModel(c, "gpt-4o")
		omneval.SetInput(c, fmt.Sprintf("sequential span %d", i))
		omneval.SetOutput(c, "response")
		omneval.SetTokens(c, int64(i*10+5), int64(i*5+3))
		omneval.EndSpan(c)
	}
	pass("SDK: 5 sequential root spans")

	// 2c. Flush
	omneval.Flush()
	pass("SDK: Flush() does not panic")

	// ── Section 3: Go SDK – Shutdown & Reconfigure ────────────────
	fmt.Println("\n-- Section 3: Go SDK Shutdown & Reconfigure --")

	// 3a. Shutdown
	if err := omneval.Shutdown(); err != nil {
		fail("SDK: Shutdown() after spans", err.Error())
	} else {
		pass("SDK: Shutdown() after spans")
	}

	// 3b. Reconfigure after shutdown
	if err := omneval.Configure(ingestURL, apiKey); err != nil {
		fail("SDK: Reconfigure after Shutdown", err.Error())
	} else {
		pass("SDK: Reconfigure after Shutdown")
	}

	// 3c. StartSpan without Configure (nil tracer) is safe
	omneval.Shutdown()
	emptyCtx := omneval.StartSpan(context.Background(), "unconfigured")
	omneval.EndSpan(emptyCtx)
	pass("SDK: StartSpan without Configure is safe noop")

	// Reconfigure for remaining tests
	omneval.Configure(ingestURL, apiKey)

	// ── Section 4: Native REST Ingest via HTTP ────────────────────
	fmt.Println("\n-- Section 4: Native REST Ingest --")

	// 4a. No API key -> 401
	resp, err := jsonPost(httpClient, ingestURL+"/api/v1/spans",
		map[string]interface{}{"spans": []interface{}{makeSpan("go-auth-test", "llm", "gpt-4o")}},
		nil)
	if err != nil {
		fail("Auth: missing API key -> 401", err.Error())
	} else {
		readBody(resp)
		if resp.StatusCode == 401 {
			pass("Auth: missing API key -> 401")
		} else {
			fail("Auth: missing API key -> 401", fmt.Sprintf("got %d", resp.StatusCode))
		}
	}

	// 4b. Single LLM span
	span1 := makeSpan("go-qa-llm", "llm", "gpt-4o")
	resp, err = jsonPost(httpClient, ingestURL+"/api/v1/spans",
		map[string]interface{}{"spans": []interface{}{span1}},
		ingestHeaders())
	if err != nil {
		fail("Single LLM span -> 202", err.Error())
	} else {
		readBody(resp)
		if resp.StatusCode == 202 {
			pass("Single LLM span -> 202")
		} else {
			fail("Single LLM span -> 202", fmt.Sprintf("got %d", resp.StatusCode))
		}
	}

	// 4c. Agent + tool parent-child batch
	rootTraceID := randomHex(32)
	rootSpan := makeSpan("go-qa-agent", "agent", "gpt-4o")
	rootSpan["trace_id"] = rootTraceID
	childSpan := makeSpan("go-qa-tool", "tool", "")
	childSpan["trace_id"] = rootTraceID
	childSpan["parent_id"] = rootSpan["span_id"]

	resp, err = jsonPost(httpClient, ingestURL+"/api/v1/spans",
		map[string]interface{}{"spans": []interface{}{rootSpan, childSpan}},
		ingestHeaders())
	if err != nil {
		fail("Parent-child batch -> 202", err.Error())
	} else {
		readBody(resp)
		if resp.StatusCode == 202 {
			pass("Parent-child agent+tool batch -> 202")
		} else {
			fail("Parent-child agent+tool batch -> 202", fmt.Sprintf("got %d", resp.StatusCode))
		}
	}

	// 4d. All span kinds
	kinds := []string{"llm", "agent", "tool", "chain", "internal"}
	var kindSpans []interface{}
	for _, k := range kinds {
		kindSpans = append(kindSpans, makeSpan("go-kind-"+k, k, "gpt-4o"))
	}
	resp, err = jsonPost(httpClient, ingestURL+"/api/v1/spans",
		map[string]interface{}{"spans": kindSpans},
		ingestHeaders())
	if err != nil {
		fail("All span kinds -> 202", err.Error())
	} else {
		readBody(resp)
		if resp.StatusCode == 202 {
			pass("All 5 span kinds in batch -> 202")
		} else {
			fail("All span kinds -> 202", fmt.Sprintf("got %d", resp.StatusCode))
		}
	}

	// ── Section 5: Query API Auth ─────────────────────────────────
	fmt.Println("\n-- Section 5: Query API Auth --")

	// Use cookie jar for session
	jar := newCookieJar()
	authedClient := &http.Client{Timeout: 15 * time.Second, Jar: jar}

	// 5a. Login
	resp, err = jsonPost(authedClient, queryURL+"/login",
		map[string]string{"email": adminUser, "password": adminPass}, nil)
	if err != nil {
		fail("Login -> 200", err.Error())
	} else {
		body := readBody(resp)
		if resp.StatusCode == 200 && strings.Contains(body, "session_id") {
			pass("Login -> 200 with session_id")
		} else {
			fail("Login -> 200 with session_id", fmt.Sprintf("got %d: %s", resp.StatusCode, body[:min(len(body), 100)]))
		}
	}

	// 5b. Bad credentials -> 401
	resp, err = jsonPost(httpClient, queryURL+"/login",
		map[string]string{"email": "nobody@example.com", "password": "wrong"}, nil)
	if err != nil {
		fail("Bad credentials -> 401", err.Error())
	} else {
		readBody(resp)
		if resp.StatusCode == 401 {
			pass("Bad credentials -> 401")
		} else {
			fail("Bad credentials -> 401", fmt.Sprintf("got %d", resp.StatusCode))
		}
	}

	// 5c. Unauthenticated span query -> 401
	resp, err = jsonPost(httpClient, queryURL+"/api/v1/spans/query", map[string]interface{}{}, nil)
	if err != nil {
		fail("Unauthenticated span query -> 401", err.Error())
	} else {
		readBody(resp)
		if resp.StatusCode == 401 {
			pass("Unauthenticated span query -> 401")
		} else {
			fail("Unauthenticated span query -> 401", fmt.Sprintf("got %d", resp.StatusCode))
		}
	}

	// ── Section 6: Span Queries ───────────────────────────────────
	fmt.Println("\n-- Section 6: Span Query (waiting 35s for writer sync) --")
	fmt.Println("  [waiting 35s...]")
	time.Sleep(35 * time.Second)

	// 6a. Query with time range
	resp, err = jsonPost(authedClient, queryURL+"/api/v1/spans/query",
		map[string]interface{}{
			"from":  "2026-01-01T00:00:00Z",
			"to":    "2026-12-31T23:59:59Z",
			"limit": 50,
		}, nil)
	var querySpans []interface{}
	if err != nil {
		fail("Span query -> 200", err.Error())
	} else {
		body := readBody(resp)
		if resp.StatusCode == 200 {
			var data map[string]interface{}
			if json.Unmarshal([]byte(body), &data) == nil {
				if spans, ok := data["spans"].([]interface{}); ok {
					querySpans = spans
					if len(spans) > 0 {
						pass(fmt.Sprintf("Span query with time range: %d spans", len(spans)))
					} else {
						fail("Span query with time range: 0 spans", "snapshot may lag")
					}
				}
			}
		} else {
			fail("Span query -> 200", fmt.Sprintf("got %d: %s", resp.StatusCode, body[:min(len(body), 100)]))
		}
	}

	// 6b. Filter by model
	resp, err = jsonPost(authedClient, queryURL+"/api/v1/spans/query",
		map[string]interface{}{
			"from":    "2026-01-01T00:00:00Z",
			"to":      "2026-12-31T23:59:59Z",
			"filters": []map[string]interface{}{{"field": "model", "op": "eq", "value": "gpt-4o"}},
			"limit":   20,
		}, nil)
	if err != nil {
		fail("Span query filter by model", err.Error())
	} else {
		body := readBody(resp)
		if resp.StatusCode == 200 {
			var data map[string]interface{}
			if json.Unmarshal([]byte(body), &data) == nil {
				n := 0
				if s, ok := data["spans"].([]interface{}); ok {
					n = len(s)
				}
				pass(fmt.Sprintf("Span query filter by model=gpt-4o: %d spans", n))
			}
		} else {
			fail("Span query filter by model -> 200", fmt.Sprintf("got %d: %s", resp.StatusCode, body[:min(len(body), 100)]))
		}
	}

	// ── Section 7: Trace Detail ───────────────────────────────────
	fmt.Println("\n-- Section 7: Trace Detail --")

	if len(querySpans) > 0 {
		spanMap, ok := querySpans[0].(map[string]interface{})
		if ok {
			traceID, _ := spanMap["trace_id"].(string)
			if traceID != "" {
				resp, err = jsonGet(authedClient, queryURL+"/api/v1/traces/"+traceID)
				if err != nil {
					fail("Trace detail -> 200", err.Error())
				} else {
					body := readBody(resp)
					if resp.StatusCode == 200 {
						var detail map[string]interface{}
						if json.Unmarshal([]byte(body), &detail) == nil {
							_, hasTraceID := detail["trace_id"]
							_, hasRoot := detail["root_span"]
							spans, hasSpans := detail["spans"].([]interface{})
							if hasTraceID && hasRoot && hasSpans && len(spans) > 0 {
								pass("Trace detail: root_span + spans present")
							} else {
								fail("Trace detail: missing fields", fmt.Sprintf("keys: %v", keys(detail)))
							}
						}
					} else {
						fail("Trace detail -> 200", fmt.Sprintf("got %d: %s", resp.StatusCode, body[:min(len(body), 100)]))
					}
				}

				// Missing trace -> 404
				resp, err = jsonGet(authedClient, queryURL+"/api/v1/traces/"+strings.Repeat("f", 32))
				if err != nil {
					fail("Missing trace -> 404", err.Error())
				} else {
					readBody(resp)
					if resp.StatusCode == 404 {
						pass("Missing trace -> 404")
					} else {
						fail("Missing trace -> 404", fmt.Sprintf("got %d", resp.StatusCode))
					}
				}
			}
		}
	} else {
		skip("Trace detail", "no spans available")
	}

	// ── Section 8: Analytics ──────────────────────────────────────
	fmt.Println("\n-- Section 8: Analytics --")

	resp, err = jsonPost(authedClient, queryURL+"/api/v1/analytics/spans", map[string]interface{}{}, nil)
	if err != nil {
		fail("Analytics: empty request -> 200", err.Error())
	} else {
		body := readBody(resp)
		if resp.StatusCode == 200 && strings.Contains(body, "rows") {
			pass("Analytics: empty request -> 200")
		} else {
			fail("Analytics: empty request -> 200", fmt.Sprintf("got %d: %s", resp.StatusCode, body[:min(len(body), 100)]))
		}
	}

	resp, err = jsonPost(authedClient, queryURL+"/api/v1/analytics/spans",
		map[string]interface{}{
			"aggregations": []map[string]interface{}{
				{"function": "count", "field": "*", "alias": "span_count"},
				{"function": "sum", "field": "cost_usd", "alias": "total_cost"},
			},
			"group_by": []map[string]interface{}{
				{"field": "model"},
			},
			"order_by": []map[string]interface{}{
				{"field": "span_count", "desc": true},
			},
		}, nil)
	if err != nil {
		fail("Analytics: group by model", err.Error())
	} else {
		body := readBody(resp)
		if resp.StatusCode == 200 {
			var data map[string]interface{}
			n := 0
			if json.Unmarshal([]byte(body), &data) == nil {
				if rows, ok := data["rows"].(float64); ok {
					n = int(rows)
				}
			}
			pass(fmt.Sprintf("Analytics: group by model (%d groups)", n))
		} else {
			fail("Analytics: group by model -> 200", fmt.Sprintf("got %d: %s", resp.StatusCode, body[:min(len(body), 100)]))
		}
	}

	// Unknown aggregation -> 400
	resp, err = jsonPost(authedClient, queryURL+"/api/v1/analytics/spans",
		map[string]interface{}{
			"aggregations": []map[string]interface{}{
				{"function": "badfunction", "field": "*", "alias": "x"},
			},
		}, nil)
	if err != nil {
		fail("Analytics: unknown function -> 400", err.Error())
	} else {
		readBody(resp)
		if resp.StatusCode == 400 {
			pass("Analytics: unknown aggregation function -> 400")
		} else {
			fail("Analytics: unknown aggregation function -> 400", fmt.Sprintf("got %d", resp.StatusCode))
		}
	}

	// ── Section 9: Eval Rules ─────────────────────────────────────
	fmt.Println("\n-- Section 9: Eval Rules --")

	resp, err = jsonGet(authedClient, queryURL+"/api/v1/eval-rules")
	if err != nil {
		fail("Eval rules list -> 200", err.Error())
	} else {
		body := readBody(resp)
		if resp.StatusCode == 200 && strings.Contains(body, "rules") {
			pass("Eval rules list -> 200")
		} else {
			fail("Eval rules list -> 200", fmt.Sprintf("got %d: %s", resp.StatusCode, body[:min(len(body), 100)]))
		}
	}

	// ── Section 10: Health Probes ─────────────────────────────────
	fmt.Println("\n-- Section 10: Health Probes --")

	for svc, url := range map[string]string{
		"ingest": ingestURL + "/healthz",
		"query":  queryURL + "/healthz",
	} {
		resp, err = jsonGet(httpClient, url)
		if err != nil {
			fail(svc+": /healthz -> 200", err.Error())
		} else {
			readBody(resp)
			if resp.StatusCode == 200 {
				pass(svc + ": /healthz -> 200")
			} else {
				fail(svc+": /healthz -> 200", fmt.Sprintf("got %d", resp.StatusCode))
			}
		}
	}

	// ── Summary ───────────────────────────────────────────────────
	fmt.Println("\n" + strings.Repeat("=", 65))
	total := len(results)
	passed, failed, skipped := 0, 0, 0
	for _, r := range results {
		switch r.status {
		case "PASS":
			passed++
		case "FAIL":
			failed++
		case "SKIP":
			skipped++
		}
	}
	fmt.Printf("Results: %d/%d passed  |  %d failed  |  %d skipped\n", passed, total, failed, skipped)
	fmt.Println(strings.Repeat("=", 65))

	if failed > 0 {
		fmt.Println("\nFailed tests:")
		for _, r := range results {
			if r.status == "FAIL" {
				fmt.Printf("  FAIL  %s: %s\n", r.name, r.detail)
			}
		}
		os.Exit(1)
	}
}

// keys returns the key names of a map for debug output.
func keys(m map[string]interface{}) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ─────────────────────────────── Cookie jar ───────────────────────────────

type simpleCookieJar struct {
	cookies map[string][]*http.Cookie
}

func newCookieJar() *simpleCookieJar {
	return &simpleCookieJar{cookies: make(map[string][]*http.Cookie)}
}

func (j *simpleCookieJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	j.cookies[u.Host] = append(j.cookies[u.Host], cookies...)
}

func (j *simpleCookieJar) Cookies(u *url.URL) []*http.Cookie {
	return j.cookies[u.Host]
}
