package auth

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

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
	UserID   string `json:"user_id"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

// PasswordChangeRequest is the body accepted by PUT /api/v1/users/me/password.
type PasswordChangeRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
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
	mux.HandleFunc("PUT /api/v1/users/me/password", h.ChangePassword)
	mux.HandleFunc("GET /api/v1/projects", h.HandleProjects)
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

// Invite handles POST /api/v1/users/invite (admin only). Creates a new user
// with a randomly-generated temporary password, returned once in the response.
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

	// Generate temporary password
	tempPassword := generateTempPassword()

	user := &domain.User{
		UserID:       xid.New().String(),
		OrgID:        req.OrgID,
		Email:        req.Email,
		PasswordHash: tempPassword,
	}

	if err := h.store.CreateUser(r.Context(), user); err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "user already exists"})
		return
	}

	writeJSON(w, http.StatusCreated, InviteResponse{
		UserID:   user.UserID,
		Email:    user.Email,
		Password: tempPassword,
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

// generateTempPassword creates a 16-character random alphanumeric password
// from crypto/rand. Failure to read from crypto/rand is extremely unlikely
// (kernel entropy exhaustion) and results in a panic.
func generateTempPassword() string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		panic(fmt.Sprintf("auth: cannot read from crypto/rand: %v", err))
	}
	result := make([]byte, 16)
	for i := range result {
		result[i] = chars[int(bytes[i])%len(chars)]
	}
	return string(result)
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
