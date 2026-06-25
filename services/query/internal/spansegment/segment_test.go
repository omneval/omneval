package spansegment

import (
	"net/http"
	"testing"
	"time"

	"github.com/omneval/omneval/internal/domain"
)

// fakeSessionStore is a minimal session store implementation for tests.
type fakeSessionStore struct {
	projectID string
}

func (f *fakeSessionStore) ProjectID(_ *http.Request) (string, bool) {
	if f.projectID == "" {
		return "", false
	}
	return f.projectID, true
}

func (f *fakeSessionStore) UserProjects(_ *http.Request) ([]*domain.Project, bool) {
	return nil, false
}

func (f *fakeSessionStore) UserID(_ *http.Request) (string, bool) {
	return "", false
}

func TestAuthPolicyString(t *testing.T) {
	tests := []struct {
		policy AuthPolicy
		want   string
	}{
		{AuthPolicyPublic, "public"},
		{AuthPolicySession, "session"},
		{AuthPolicyAPIKeyOrSession, "session_or_api_key"},
		{AuthPolicyAdmin, "admin"},
		{AuthPolicy(99), "unknown"},
	}
	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			if got := tc.policy.String(); got != tc.want {
				t.Errorf("AuthPolicy(%d).String() = %q, want %q", tc.policy, got, tc.want)
			}
		})
	}
}

func TestAuthRoute(t *testing.T) {
	route := AuthRoute{
		Method:  "GET",
		Path:    "/api/v1/spans/query",
		Policy:  AuthPolicySession,
		Handler: func(w http.ResponseWriter, r *http.Request) {},
	}
	if route.Method != "GET" {
		t.Errorf("Method: got %q, want %q", route.Method, "GET")
	}
	if route.Path != "/api/v1/spans/query" {
		t.Errorf("Path: got %q, want %q", route.Path, "/api/v1/spans/query")
	}
	if route.Policy != AuthPolicySession {
		t.Errorf("Policy: got %v, want %v", route.Policy, AuthPolicySession)
	}
}

func TestBuildTraceTree_EmptySpans(t *testing.T) {
	t.Parallel()

	trace := buildTraceTree([]*domain.Span{}, nil, "", "", "scores")
	if trace.TraceID != "" {
		t.Errorf("trace_id: got %q, want empty", trace.TraceID)
	}
	if trace.RootSpan != nil {
		t.Error("root_span should be nil for empty spans")
	}
}

func TestBuildTraceTree_SingleSpan(t *testing.T) {
	t.Parallel()

	baseTime := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	spans := []*domain.Span{
		{SpanID: "span-001", TraceID: "trace-1", ParentID: "", Name: "root", StartTime: baseTime},
	}

	trace := buildTraceTree(spans, nil, "", "", "scores")

	if trace.TraceID != "trace-1" {
		t.Errorf("trace_id: got %q, want %q", trace.TraceID, "trace-1")
	}
	if trace.RootSpan == nil {
		t.Fatal("root_span is nil")
	}
	if trace.RootSpan.SpanID != "span-001" {
		t.Errorf("root span_id: got %q, want %q", trace.RootSpan.SpanID, "span-001")
	}
	if len(trace.RootSpan.Children) != 0 {
		t.Errorf("root.children: got %d, want 0", len(trace.RootSpan.Children))
	}
}

func TestBuildTraceTree_NestedChildren(t *testing.T) {
	t.Parallel()

	baseTime := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	spans := []*domain.Span{
		{SpanID: "root", TraceID: "trace-1", ParentID: "", Name: "root", StartTime: baseTime},
		{SpanID: "child1", TraceID: "trace-1", ParentID: "root", Name: "child1", StartTime: baseTime.Add(time.Second)},
		{SpanID: "child2", TraceID: "trace-1", ParentID: "root", Name: "child2", StartTime: baseTime.Add(2 * time.Second)},
		{SpanID: "grandchild", TraceID: "trace-1", ParentID: "child1", Name: "grandchild", StartTime: baseTime.Add(3 * time.Second)},
	}

	trace := buildTraceTree(spans, nil, "", "", "scores")

	if trace.RootSpan == nil {
		t.Fatal("root_span is nil")
	}
	if trace.RootSpan.SpanID != "root" {
		t.Errorf("root span_id: got %q, want %q", trace.RootSpan.SpanID, "root")
	}
	if len(trace.RootSpan.Children) != 2 {
		t.Errorf("root.children: got %d, want 2", len(trace.RootSpan.Children))
	}

	// Find child1 and verify grandchild is its child.
	var child1 *domain.Span
	for _, c := range trace.RootSpan.Children {
		if c.SpanID == "child1" {
			child1 = c
			break
		}
	}
	if child1 == nil {
		t.Fatal("child1 not found in root.children")
	}
	if len(child1.Children) != 1 {
		t.Errorf("child1.children: got %d, want 1", len(child1.Children))
	}
	if len(child1.Children) > 0 && child1.Children[0].SpanID != "grandchild" {
		t.Errorf("grandchild span_id: got %q, want %q", child1.Children[0].SpanID, "grandchild")
	}

	// Verify child2 has no children.
	var child2 *domain.Span
	for _, c := range trace.RootSpan.Children {
		if c.SpanID == "child2" {
			child2 = c
			break
		}
	}
	if child2 == nil {
		t.Fatal("child2 not found in root.children")
	}
	if len(child2.Children) != 0 {
		t.Errorf("child2.children: got %d, want 0", len(child2.Children))
	}
}

func TestBuildTraceTree_AllMissingParents(t *testing.T) {
	t.Parallel()

	// All spans have non-empty parent_id pointing to spans outside the set.
	baseTime := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	spans := []*domain.Span{
		{SpanID: "span-a", TraceID: "trace-1", ParentID: "missing-1", Name: "a", StartTime: baseTime},
		{SpanID: "span-b", TraceID: "trace-1", ParentID: "missing-2", Name: "b", StartTime: baseTime.Add(time.Second)},
	}

	trace := buildTraceTree(spans, nil, "", "", "scores")

	// With no valid root, the first span should be used as root.
	if trace.RootSpan == nil {
		t.Fatal("root_span is nil")
	}
	if trace.RootSpan.SpanID != "span-a" {
		t.Errorf("root span_id: got %q, want %q", trace.RootSpan.SpanID, "span-a")
	}
}