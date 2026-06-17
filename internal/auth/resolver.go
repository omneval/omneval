package auth

import (
	"context"
	"net/http"
	"strings"
)

// ResponseWriterKey is the context key for storing a ResponseWriter.
// Used by ProjectResolver implementations that need to write errors on failure.
type ResponseWriterKey struct{}

// WithResponseWriter returns a new request with the ResponseWriter stored in
// its context, retrievable via ResponseWriterFromContext.
func WithResponseWriter(r *http.Request, w http.ResponseWriter) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), ResponseWriterKey{}, w))
}

// ResponseWriterFromContext retrieves the ResponseWriter from a request's context.
func ResponseWriterFromContext(r *http.Request) (http.ResponseWriter, bool) {
	w, ok := r.Context().Value(ResponseWriterKey{}).(http.ResponseWriter)
	return w, ok
}

// ExplicitProjectIDKey is the context key for storing an explicit project ID
// (e.g. from the UI project switcher body or query param) before resolution.
type ExplicitProjectIDKey struct{}

// WithExplicitProjectID returns a new request with the explicit project ID
// stored in its context.
func WithExplicitProjectID(r *http.Request, id string) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), ExplicitProjectIDKey{}, id))
}

// ProjectResolver is the abstraction every service uses to extract and resolve
// project ID from an HTTP request. Implementations are expected to try multiple
// resolution strategies in order (API-key context → session → query param) and
// return the first successful result.
type ProjectResolver interface {
	ProjectID(r *http.Request) (string, bool)
}

// ProjectResolverFunc is an adapter that allows using a function as a
// ProjectResolver.
type ProjectResolverFunc func(r *http.Request) (string, bool)

// ProjectID implements ProjectResolver.
func (f ProjectResolverFunc) ProjectID(r *http.Request) (string, bool) {
	return f(r)
}

// SessionStoreResolver wraps a ProjectIDProvider (any type with a ProjectID
// method) and delegates ProjectID calls to it. This allows services to share
// a single ProjectResolver implementation that satisfies the ProjectResolver
// interface.
type SessionStoreResolver struct {
	ProjectIDProvider
}

// ProjectIDProvider is the minimal interface ProjectStoreResolver needs.
type ProjectIDProvider interface {
	ProjectID(r *http.Request) (string, bool)
}

// NewSessionStoreResolver creates a ProjectResolver that delegates to the
// given ProjectIDProvider.
func NewSessionStoreResolver(p ProjectIDProvider) *SessionStoreResolver {
	return &SessionStoreResolver{ProjectIDProvider: p}
}

// ProjectID extracts the project ID from the request context. It first checks
// the API-key derived project ID (set by RequireAuth/RequireSessionOrAPIKey
// middleware), then the session store resolver, then the explicit project ID,
// and finally the ?project_id query parameter. It is a convenience wrapper that
// every handler should use to obtain the project ID.
func ProjectID(r *http.Request) (string, bool) {
	// 1. Check API-key derived project ID from context first.
	if pid, ok := r.Context().Value(APIKeyProjectIDContextKey).(string); ok && pid != "" {
		return pid, true
	}
	// 2. Check explicit project ID from context.
	if eid, ok := r.Context().Value(ExplicitProjectIDKey{}).(string); ok && eid != "" {
		return eid, true
	}
	// 3. Fall back to ?project_id query param.
	if pid := r.URL.Query().Get("project_id"); pid != "" {
		return pid, true
	}
	return "", false
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

// ProjectIDWithError is like ProjectID but writes a 401 "project ID not found"
// response to the provided ResponseWriter on failure. Handlers call this
// instead of their own resolveProjectID methods to ensure a single, consistent
// project-ID extraction pattern across all services.
func ProjectIDWithError(w http.ResponseWriter, r *http.Request) (string, bool) {
	if pid, ok := ProjectID(r); ok {
		return pid, true
	}
	http.Error(w, "project ID not found", http.StatusUnauthorized)
	return "", false
}

// WriteJSONError writes a JSON error response with the given status code.
func WriteJSONError(w http.ResponseWriter, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"error":"` + strings.ReplaceAll(message, `"`, `\"`) + `"}`))
}