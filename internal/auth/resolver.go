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

// ProjectAuthorizer is an optional interface that a resolver may implement to
// authorize a project ID that differs from the resolver's default.  When
// present, ResolveProjectID calls it before rejecting an explicit project ID
// with 403.
type ProjectAuthorizer interface {
	ProjectResolver
	// AuthorizeProject reports whether the current request is allowed to
	// access the given projectID.
	AuthorizeProject(projectID string) bool
}

// SessionStoreResolver wraps a ProjectIDProvider (any type with a ProjectID
// method) and delegates ProjectID calls to it. This allows services to share
// a single ProjectResolver implementation that satisfies the ProjectResolver
// interface.
//
// SessionStoreResolver also implements ProjectAuthorizer when the underlying
// provider does, so callers can authorize project IDs through the wrapper.
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

// AuthorizeProject delegates to the underlying provider if it implements
// ProjectAuthorizer; otherwise, it only allows the provider's own ProjectID.
func (r *SessionStoreResolver) AuthorizeProject(projectID string) bool {
	if auth, ok := r.ProjectIDProvider.(ProjectAuthorizer); ok {
		return auth.AuthorizeProject(projectID)
	}
	// Without an explicit authorizer, allow only the resolver's own default.
	if pid, ok := r.ProjectIDProvider.ProjectID(nil); ok {
		return pid == projectID
	}
	return false
}

// MultiResolver chains multiple ProjectResolvers: it tries each in order and
// returns the first non-empty project ID found. If the first resolver is nil,
// it is skipped.
type MultiResolver struct {
	resolvers []ProjectResolver
}

// NewMultiResolver creates a ProjectResolver that tries each provided
// resolver in order, returning the first non-empty project ID found.
func NewMultiResolver(resolvers ...ProjectResolver) *MultiResolver {
	// Filter out nil resolvers.
	var filtered []ProjectResolver
	for _, r := range resolvers {
		if r != nil {
			filtered = append(filtered, r)
		}
	}
	return &MultiResolver{resolvers: filtered}
}

// ProjectID iterates through the chained resolvers and returns the first
// non-empty project ID found.
func (m *MultiResolver) ProjectID(r *http.Request) (string, bool) {
	for _, resolver := range m.resolvers {
		if pid, ok := resolver.ProjectID(r); ok {
			return pid, true
		}
	}
	return "", false
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

// ResolveProjectID extracts the project ID from the request by first
// checking the API-key context, then delegating to the provided resolver (and
// validating any explicit project ID against it when present), and finally
// falling back to the ?project_id query param.  If resolver is nil, the
// explicit project ID from context (set by WithExplicitProjectID) is used as
// a raw fallback.
//
// When the resolver is a ProjectAuthorizer, ResolveProjectID checks whether the
// caller has access to an explicit project ID that differs from the resolver's
// default.  Only authorized explicit IDs are returned; unauthorized ones cause
// resolution to fail so the caller's error handler can return 403.
func ResolveProjectID(r *http.Request, resolver ProjectResolver, explicitID string) (string, bool) {
	// 1. Check API-key derived project ID from context first.
	if pid, ok := r.Context().Value(APIKeyProjectIDContextKey).(string); ok && pid != "" {
		return pid, true
	}
	// Capture the explicit project ID before delegating to the resolver.
	explicitEID, _ := r.Context().Value(ExplicitProjectIDKey{}).(string)

	if resolver != nil {
		if pid, ok := resolver.ProjectID(r); ok {
			if explicitEID != "" && pid != explicitEID {
				// Resolver's default differs from explicit ID — try to
				// authorize the explicit one before rejecting it.
				if authorizer, ok := resolver.(ProjectAuthorizer); ok && authorizer.AuthorizeProject(explicitEID) {
					return explicitEID, true
				}
				return "", false // resolution fails; error handler sees ExplicitProjectIDKey → 403
			}
			return pid, true
		}
		// Resolver returned no default but the caller set an explicit ID —
		// try to authorize it.
		if explicitEID != "" {
			if authorizer, ok := resolver.(ProjectAuthorizer); ok && authorizer.AuthorizeProject(explicitEID) {
				return explicitEID, true
			}
		}
	}

	// 2. Fallbacks when no authorized project was found.
	if explicitEID != "" {
		return explicitEID, true
	}
	if explicitID != "" {
		return explicitID, true
	}
	if pid := r.URL.Query().Get("project_id"); pid != "" {
		return pid, true
	}
	return "", false
}

// ProjectIDError is the status code and message returned when project ID
// resolution fails.
type ProjectIDError struct {
	StatusCode int
	Message    string
}

// projectIDErrors maps resolution outcomes to appropriate HTTP status codes
// and messages.
var (
	projectIDUnauthorized = ProjectIDError{http.StatusUnauthorized, "project ID not found"}
	projectIDForbidden    = ProjectIDError{http.StatusForbidden, "project access denied"}
)

// ProjectIDWithError is like ProjectID but writes an error response to the
// provided ResponseWriter on failure.  Returns 401 for unauthenticated users
// and 400 for authenticated users with no project.
func ProjectIDWithError(w http.ResponseWriter, r *http.Request) (string, bool) {
	if pid, ok := ProjectID(r); ok {
		return pid, true
	}
	if CurrentUserFromContext(r) != nil {
		WriteJSONError(w, "no project found, contact your administrator", http.StatusBadRequest)
	} else {
		WriteJSONError(w, "project ID not found", http.StatusUnauthorized)
	}
	return "", false
}

// ProjectIDWithErrorWithResolver is like ProjectIDWithError but also consults
// the provided ProjectResolver. When resolver is nil, it behaves exactly like
// ProjectIDWithError (for backwards compatibility with tests that inject API-key
// context directly). When resolver is non-nil, it tries the resolver as an
// additional fallback source before giving up.
func ProjectIDWithErrorWithResolver(w http.ResponseWriter, r *http.Request, resolver ProjectResolver) (string, bool) {
	if pid, ok := ResolveProjectID(r, resolver, ""); ok {
		return pid, true
	}
	if err := projectIDResolutionError(r, resolver); err != nil {
		WriteJSONError(w, err.Message, err.StatusCode)
	} else if CurrentUserFromContext(r) != nil {
		WriteJSONError(w, "no project found, contact your administrator", http.StatusBadRequest)
	} else {
		WriteJSONError(w, "project ID not found", http.StatusUnauthorized)
	}
	return "", false
}

// projectIDResolutionError returns a ProjectIDError when resolution fails but
// the caller already has a valid auth context.  It distinguishes a rejected
// explicit project ID (403) from a plain "no project" (400).  Returns nil on
// success so callers that check for it don't double-determine the error.
func projectIDResolutionError(r *http.Request, resolver ProjectResolver) *ProjectIDError {
	if _, ok := ResolveProjectID(r, resolver, ""); ok {
		return nil
	}
	if CurrentUserFromContext(r) == nil {
		return nil // let ProjectIDWithErrorWithResolver decide in the caller
	}
	if _, hasExplicit := r.Context().Value(ExplicitProjectIDKey{}).(string); hasExplicit {
		return &projectIDForbidden
	}
	return &ProjectIDError{http.StatusBadRequest, "no project found, contact your administrator"}
}

// ProjectIDErrorWithResolver is like ProjectIDWithErrorWithResolver but returns
// an error struct instead of writing the response directly, so callers can
// customise the response body or status code.
func ProjectIDErrorWithResolver(w http.ResponseWriter, r *http.Request, resolver ProjectResolver) *ProjectIDError {
	if err := projectIDResolutionError(r, resolver); err != nil {
		return err
	}
	if CurrentUserFromContext(r) == nil {
		return &projectIDUnauthorized
	}
	return &ProjectIDError{http.StatusBadRequest, "no project found, contact your administrator"}
}

// WriteJSONError writes a JSON error response with the given status code.
func WriteJSONError(w http.ResponseWriter, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"error":"` + strings.ReplaceAll(message, `"`, `\"`) + `"}`))
}
