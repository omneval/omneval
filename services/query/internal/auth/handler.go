package auth

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/zbloss/lantern/internal/auth"
	"github.com/zbloss/lantern/internal/domain"
	"github.com/zbloss/lantern/internal/metadata"
	"github.com/rs/xid"
)

// LoginRequest is the body accepted by POST /login.
type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// LoginResponse is returned by POST /login on success.
type LoginResponse struct {
	SessionID string `json:"session_id"`
}

// InviteRequest is the body accepted by POST /api/v1/users/invite.
type InviteRequest struct {
	Email string `json:"email"`
	OrgID string `json:"org_id"`
}

// InviteResponse is returned by POST /api/v1/users/invite.
type InviteResponse struct {
	UserID               string `json:"user_id"`
	Email                string `json:"email"`
	PasswordResetToken   string `json:"password_reset_token"`
}

// PasswordChangeRequest is the body accepted by PUT /api/v1/users/me/password.
type PasswordChangeRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// PasswordResetRequest is the body accepted by POST /api/v1/users/reset-password.
type PasswordResetRequest struct {
	Token       string `json:"token"`
	NewPassword string `json:"new_password"`
}

// CreateProjectRequest is the body accepted by POST /api/v1/projects.
type CreateProjectRequest struct {
	Name string `json:"name"`
}

// CreateProjectResponse is returned by POST /api/v1/projects on success.
type CreateProjectResponse struct {
	ProjectID string `json:"project_id"`
	Name      string `json:"name"`
}

// GenerateAPIKeyRequest is the body accepted by POST /api/v1/projects/{id}/api-keys.
type GenerateAPIKeyRequest struct {
	Kind        domain.APIKeyKind `json:"kind"`
	ServiceName string            `json:"service_name,omitempty"` // required for service-scoped keys
}

// GenerateAPIKeyResponse is returned by POST /api/v1/projects/{id}/api-keys on success.
type GenerateAPIKeyResponse struct {
	KeyID       string            `json:"key_id"`
	ProjectID   string            `json:"project_id"`
	Kind        domain.APIKeyKind `json:"kind"`
	ServiceName string            `json:"service_name,omitempty"`
	RawKey      string            `json:"raw_key"`      // shown only once
	CreatedAt   time.Time         `json:"created_at"`
}

// APIKeyInfo represents an API key as returned by list endpoints (never raw key).
type APIKeyInfo struct {
	KeyID       string            `json:"key_id"`
	Kind        domain.APIKeyKind `json:"kind"`
	ServiceName string            `json:"service_name,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	RevokedAt   *time.Time        `json:"revoked_at,omitempty"`
}

// Handler handles authentication endpoints.
type Handler struct {
	store         metadata.Store
	secure        bool
	sessionTTL    time.Duration
	adminEmail    string
	adminPassword string
}

// NewHandler creates a new auth handler.
func NewHandler(store metadata.Store, secure bool, sessionTTL time.Duration, adminEmail, adminPassword string) *Handler {
	return &Handler{
		store:         store,
		secure:        secure,
		sessionTTL:    sessionTTL,
		adminEmail:    adminEmail,
		adminPassword: adminPassword,
	}
}

// Register mounts auth routes on the given mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /login", h.Login)
	mux.HandleFunc("POST /logout", h.Logout)
	mux.HandleFunc("POST /api/v1/users/invite", h.Invite)
	mux.HandleFunc("POST /api/v1/users/reset-password", h.ResetPassword)
	mux.HandleFunc("PUT /api/v1/users/me/password", h.ChangePassword)
	mux.HandleFunc("GET /api/v1/projects", h.HandleProjects)
	mux.HandleFunc("POST /api/v1/projects", h.HandleCreateProject)
	mux.HandleFunc("POST /api/v1/projects/{id}/api-keys", h.HandleGenerateAPIKey)
	mux.HandleFunc("GET /api/v1/projects/{id}/api-keys", h.HandleListAPIKeys)
	mux.HandleFunc("DELETE /api/v1/projects/{id}/api-keys/{keyId}", h.HandleRevokeAPIKey)
}

// BootstrapAdmin creates the initial admin user if no users exist and
// admin credentials are configured. Returns true if a user was created.
func (h *Handler) BootstrapAdmin(ctx context.Context) (bool, error) {
	if h.adminEmail == "" || h.adminPassword == "" {
		return false, nil
	}

	count, err := h.store.CountUsers(ctx)
	if err != nil {
		return false, fmt.Errorf("auth: count users for bootstrap: %w", err)
	}
	if count > 0 {
		return false, nil
	}

	// Ensure the default org exists before creating the user (FK constraint).
	_, err = h.store.GetOrganization(ctx, "default")
	if errors.Is(err, metadata.ErrNotFound) {
		if err := h.store.CreateOrganization(ctx, &domain.Organization{
			OrgID:     "default",
			Name:      "Default",
			CreatedAt: time.Now(),
		}); err != nil {
			return false, fmt.Errorf("auth: create default org: %w", err)
		}
	} else if err != nil {
		return false, fmt.Errorf("auth: get default org: %w", err)
	}

	// Create the first admin user
	user := &domain.User{
		UserID:       xid.New().String(),
		OrgID:        "default",
		Email:        h.adminEmail,
		PasswordHash: h.adminPassword,
		CreatedAt:    time.Now(),
	}
	if err := h.store.CreateUser(ctx, user); err != nil {
		return false, fmt.Errorf("auth: create bootstrap admin: %w", err)
	}

	return true, nil
}

// Login handles POST /login. Validates email + password, creates a session,
// sets the lantern_session cookie, and returns the session ID.
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if req.Email == "" || req.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "email and password are required"})
		return
	}

	user, err := h.store.GetUserByEmail(r.Context(), req.Email)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid email or password"})
		return
	}

	if err := h.store.CheckPassword(user.PasswordHash, req.Password); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid email or password"})
		return
	}

	// Create session
	sessionID := xid.New().String()
	expiresAt := time.Now().Add(h.sessionTTL)
	session := &domain.Session{
		SessionID: sessionID,
		UserID:    user.UserID,
		ExpiresAt: expiresAt,
	}
	if err := h.store.CreateSession(r.Context(), session); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create session"})
		return
	}

	// Set session cookie
	SetSessionCookie(w, sessionID, h.secure, h.sessionTTL)

	writeJSON(w, http.StatusOK, LoginResponse{SessionID: sessionID})
}

// Logout handles POST /logout. Deletes the session and clears the cookie.
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("lantern_session")
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not logged in"})
		return
	}

	_ = h.store.DeleteSession(r.Context(), cookie.Value)
	ClearSessionCookie(w, h.secure)

	writeJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}

// HandleProjects handles GET /api/v1/projects.
// Returns the list of projects for the authenticated user's organization.
// Used by the frontend project switcher dropdown.
func (h *Handler) HandleProjects(w http.ResponseWriter, r *http.Request) {
	user := CurrentUserFromContext(r)
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	// Look up the user to get their org ID
	u, err := h.store.GetUserByID(r.Context(), user.UserID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to look up user"})
		return
	}

	// List all projects for the user's org
	projects, err := h.store.ListProjects(r.Context(), u.OrgID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list projects"})
		return
	}

	writeJSON(w, http.StatusOK, projects)
}

// HandleCreateProject handles POST /api/v1/projects.
// Creates a new project for the authenticated user's organization.
func (h *Handler) HandleCreateProject(w http.ResponseWriter, r *http.Request) {
	user := CurrentUserFromContext(r)
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	var req CreateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if req.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}

	u, err := h.store.GetUserByID(r.Context(), user.UserID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to look up user"})
		return
	}

	projectID := xid.New().String()
	project := &domain.Project{
		ProjectID: projectID,
		OrgID:     u.OrgID,
		Name:      req.Name,
		CreatedAt: time.Now().UTC(),
	}

	if err := h.store.CreateProject(r.Context(), project); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create project"})
		return
	}

	writeJSON(w, http.StatusCreated, CreateProjectResponse{
		ProjectID: project.ProjectID,
		Name:      project.Name,
	})
}

// parseProjectID extracts the project ID from a URL path like
// "/api/v1/projects/{id}/api-keys" or "/api/v1/projects/{id}/api-keys/{keyId}".
func parseProjectID(path string) (string, bool) {
	prefix := "/api/v1/projects/"
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	rest := path[len(prefix):]
	idx := strings.Index(rest, "/")
	if idx == -1 {
		return "", false
	}
	return rest[:idx], true
}

// HandleGenerateAPIKey handles POST /api/v1/projects/{id}/api-keys.
// Generates a new API key for the specified project.
func (h *Handler) HandleGenerateAPIKey(w http.ResponseWriter, r *http.Request) {
	user := CurrentUserFromContext(r)
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	projectID, ok := parseProjectID(r.URL.Path)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid project ID"})
		return
	}

	var req GenerateAPIKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	// Validate kind
	if req.Kind != domain.APIKeyKindProject && req.Kind != domain.APIKeyKindService {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "kind must be 'project' or 'service'"})
		return
	}

	// Service-scoped keys require a service name
	if req.Kind == domain.APIKeyKindService && req.ServiceName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "service_name is required for service-scoped keys"})
		return
	}

	// Verify project exists
	_, err := h.store.GetProject(r.Context(), projectID)
	if err != nil {
		if errors.Is(err, metadata.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "project not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to look up project"})
		return
	}

	// Generate the API key
	rawKey, hashedKey, err := auth.Generate(req.Kind)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to generate key"})
		return
	}

	// Create the key record
	keyID := xid.New().String()
	apiKey := &domain.APIKey{
		KeyID:       keyID,
		ProjectID:   projectID,
		Kind:        req.Kind,
		ServiceName: req.ServiceName,
		HashedKey:   hashedKey,
		CreatedAt:   time.Now().UTC(),
	}

	if err := h.store.CreateAPIKey(r.Context(), apiKey); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to store key"})
		return
	}

	writeJSON(w, http.StatusCreated, GenerateAPIKeyResponse{
		KeyID:       keyID,
		ProjectID:   projectID,
		Kind:        req.Kind,
		ServiceName: req.ServiceName,
		RawKey:      rawKey,
		CreatedAt:   apiKey.CreatedAt,
	})
}

// HandleListAPIKeys handles GET /api/v1/projects/{id}/api-keys.
// Lists all API keys for the specified project (never returns raw keys).
func (h *Handler) HandleListAPIKeys(w http.ResponseWriter, r *http.Request) {
	user := CurrentUserFromContext(r)
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	projectID, ok := parseProjectID(r.URL.Path)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid project ID"})
		return
	}

	_, err := h.store.GetProject(r.Context(), projectID)
	if err != nil {
		if errors.Is(err, metadata.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "project not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to look up project"})
		return
	}

	// List API keys for the project
	keys, err := h.store.ListAPIKeys(r.Context(), projectID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list API keys"})
		return
	}

	// Convert to public-facing format (strip hashed key)
	result := make([]APIKeyInfo, len(keys))
	for i, k := range keys {
		result[i] = APIKeyInfo{
			KeyID:       k.KeyID,
			Kind:        k.Kind,
			ServiceName: k.ServiceName,
			CreatedAt:   k.CreatedAt,
			RevokedAt:   k.RevokedAt,
		}
	}

	writeJSON(w, http.StatusOK, result)
}

// HandleRevokeAPIKey handles DELETE /api/v1/projects/{id}/api-keys/{keyId}.
// Revokes an API key for the specified project.
func (h *Handler) HandleRevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	user := CurrentUserFromContext(r)
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	// /api/v1/projects/{id}/api-keys/{keyId}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/projects/")
	parts := strings.Split(path, "/")
	if len(parts) < 3 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid path"})
		return
	}
	projectID := parts[0]
	keyID := parts[2]

	// Verify the key belongs to this project.
	keys, err := h.store.ListAPIKeys(r.Context(), projectID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to look up keys"})
		return
	}

	var found bool
	for _, k := range keys {
		if k.KeyID == keyID {
			found = true
			break
		}
	}
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "API key not found"})
		return
	}

	if err := h.store.RevokeAPIKey(r.Context(), keyID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to revoke key"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

// Invite handles POST /api/v1/users/invite (admin only). Creates a new user
// and generates a one-time password reset token. The token is returned in the
// response and can be used with POST /api/v1/users/reset-password to set a
// new password. The token expires after 24 hours.
func (h *Handler) Invite(w http.ResponseWriter, r *http.Request) {
	// Check admin
	if !IsAdmin(r, h.adminEmail) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	var req InviteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if req.Email == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "email is required"})
		return
	}
	if req.OrgID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "org_id is required"})
		return
	}

	// Generate password reset token (43 characters of base58-safe random data)
	resetToken := generateResetToken()
	resetExpiry := time.Now().Add(24 * time.Hour)

	user := &domain.User{
		UserID:               xid.New().String(),
		OrgID:                req.OrgID,
		Email:                req.Email,
		PasswordHash:         "", // no initial password; user sets it via reset token
		PasswordResetToken:   resetToken,
		ResetTokenExpiry:     resetExpiry,
	}

	if err := h.store.CreateUser(r.Context(), user); err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "user already exists"})
		return
	}

	writeJSON(w, http.StatusCreated, InviteResponse{
		UserID:               user.UserID,
		Email:                user.Email,
		PasswordResetToken:   resetToken,
	})
}

// ChangePassword handles PUT /api/v1/users/me/password. Validates the current
// password before updating to the new one.
func (h *Handler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	user := CurrentUserFromContext(r)
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	var req PasswordChangeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if req.CurrentPassword == "" || req.NewPassword == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "current_password and new_password are required"})
		return
	}

	// Get user from store to verify current password
	storedUser, err := h.store.GetUserByID(r.Context(), user.UserID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}

	if err := h.store.CheckPassword(storedUser.PasswordHash, req.CurrentPassword); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "current password is incorrect"})
		return
	}

	// Update password
	if err := h.store.UpdateUserPassword(r.Context(), user.UserID, req.NewPassword); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update password"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "password_updated"})
}

// generateResetToken creates a 43-character base58-safe random token suitable
// for URL-safe password reset links. Failure to read from crypto/rand is
// extremely unlikely and results in a panic.
func generateResetToken() string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	bytes := make([]byte, 43)
	if _, err := rand.Read(bytes); err != nil {
		panic(fmt.Sprintf("auth: cannot read from crypto/rand: %v", err))
	}
	result := make([]byte, 43)
	for i := range result {
		result[i] = chars[int(bytes[i])%len(chars)]
	}
	return string(result)
}

// ResetPassword handles POST /api/v1/users/reset-password. Accepts a
// password reset token and a new password. Validates the token (exists, not
// expired, unused), hashes the new password, stores it, and invalidates the
// token. Single-use: the same token cannot be used twice.
func (h *Handler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	var req PasswordResetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if req.Token == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "token is required"})
		return
	}
	if req.NewPassword == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "new_password is required"})
		return
	}

	// Look up user by reset token (also validates expiry)
	user, err := h.store.GetUserByResetToken(r.Context(), req.Token)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid or expired reset token"})
		return
	}

	// Update password (store handles bcrypt hashing) and clear the reset token (single-use)
	if err := h.store.UpdateUserPassword(r.Context(), user.UserID, req.NewPassword); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update password"})
		return
	}
	if err := h.store.UpdateUserResetToken(r.Context(), user.UserID, "", time.Time{}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to invalidate reset token"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "password_reset"})
}

// ListProjects returns all projects for the authenticated user's organization.
// Used by the sessionStore interface for the projects endpoint.
func (h *Handler) ListProjects(r *http.Request) ([]*domain.Project, error) {
	user := CurrentUserFromContext(r)
	if user == nil {
		return nil, fmt.Errorf("unauthorized")
	}

	u, err := h.store.GetUserByID(r.Context(), user.UserID)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}

	return h.store.ListProjects(r.Context(), u.OrgID)
}

// ProjectID returns the project ID associated with the request.
// For authenticated users, it looks up the user's organization and returns
// the first project for that org. Falls back to ?project_id query param for development.
func (h *Handler) ProjectID(r *http.Request) (string, bool) {
	// First try: get current user from context, look up their org
	user := CurrentUserFromContext(r)
	if user != nil {
		// Look up the user to get their org ID
		u, err := h.store.GetUserByID(r.Context(), user.UserID)
		if err == nil {
			projects, err := h.store.ListProjects(r.Context(), u.OrgID)
			if err == nil && len(projects) > 0 {
				return projects[0].ProjectID, true
			}
		}
	}

	// Fallback: accept project_id from query param for development.
	if pid := r.URL.Query().Get("project_id"); pid != "" {
		return pid, true
	}

	return "", false
}
