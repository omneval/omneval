package spansegment

import (
	"net/http"
	"testing"
)

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