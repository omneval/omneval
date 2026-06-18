package handler

import (
	"net/http"
	"testing"
)

func TestAuthPolicy_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		policy AuthPolicy
		want   string
	}{
		{AuthPolicyPublic, "public"},
		{AuthPolicySession, "session"},
		{AuthPolicyAPIKeyOrSession, "session_or_api_key"},
		{AuthPolicyAdmin, "admin"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			if got := tt.policy.String(); got != tt.want {
				t.Errorf("AuthPolicy(%q).String() = %q, want %q", tt.policy, got, tt.want)
			}
		})
	}
}

func TestAuthRoute(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	route := AuthRoute{
		Method:   http.MethodPost,
		Path:     "/api/v1/spans/query",
		Handler:  handler,
		Policy:   AuthPolicySession,
	}

	if route.Method != http.MethodPost {
		t.Errorf("AuthRoute.Method = %q, want %q", route.Method, http.MethodPost)
	}
	if route.Path != "/api/v1/spans/query" {
		t.Errorf("AuthRoute.Path = %q, want %q", route.Path, "/api/v1/spans/query")
	}
	if route.Policy != AuthPolicySession {
		t.Errorf("AuthRoute.Policy = %v, want %v", route.Policy, AuthPolicySession)
	}
	if route.Handler == nil {
		t.Error("AuthRoute.Handler should not be nil")
	}
}

func TestPublicPaths(t *testing.T) {
	t.Parallel()

	// Verify all expected public paths are in the map.
	expectedPublic := []string{
		"/login",
		"/logout",
		"/healthz",
		"/readyz",
		"/metrics",
		"/api/v1/scores",
	}

	for _, path := range expectedPublic {
		if !isPublicPath(path) {
			t.Errorf("isPublicPath(%q) = false, want true", path)
		}
	}
}

func TestPublicPathPrefixMatch(t *testing.T) {
	t.Parallel()

	// Health check variants should also be public.
	if !isPublicPath("/healthz/") {
		t.Error("isPublicPath(/healthz/) = false, want true")
	}
	if !isPublicPath("/readyz/") {
		t.Error("isPublicPath(/readyz/) = false, want true")
	}
}

func TestProtectedPathNotPublic(t *testing.T) {
	t.Parallel()

	protected := []string{
		"/api/v1/spans/query",
		"/api/v1/traces/abc123",
		"/api/v1/admin/status",
		"/api/v1/prompts",
		"/api/v1/datasets",
	}

	for _, path := range protected {
		if isPublicPath(path) {
			t.Errorf("isPublicPath(%q) = true, want false", path)
		}
	}
}