package auth

import "net/http"

// ProjectResolver is the abstraction every service uses to extract and resolve
// project ID from an HTTP request. Implementations are expected to try multiple
// resolution strategies in order (API-key context → session → query param) and
// return the first successful result.
type ProjectResolver interface {
	ProjectID(r *http.Request) (string, bool)
}

// ResolveProjectID extracts the project ID from the request by delegating to a
// ProjectResolver. If the request already carries a validated API-key project ID
// in the context (set by RequireSessionOrAPIKey middleware), it is returned
// immediately. Otherwise the resolver is consulted.
func ResolveProjectID(r *http.Request, resolver ProjectResolver, explicitID string) (string, bool) {
	// 1. Check API-key derived project ID from context first.
	if pid, ok := r.Context().Value(APIKeyProjectIDContextKey).(string); ok && pid != "" {
		return pid, true
	}
	// 2. Use the resolver.
	if pid, ok := resolver.ProjectID(r); ok {
		return pid, true
	}
	// 3. Fall back to explicit project ID from router path parameters.
	if explicitID != "" {
		return explicitID, true
	}
	// 4. Fall back to ?project_id query param.
	if pid := r.URL.Query().Get("project_id"); pid != "" {
		return pid, true
	}
	return "", false
}