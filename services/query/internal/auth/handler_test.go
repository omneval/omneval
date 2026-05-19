package auth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	internalauth "github.com/omneval/omneval/internal/auth"
	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/fake"
	"github.com/omneval/omneval/services/query/internal/auth"
)

// setupAuthServer creates an auth handler and HTTP test server.
func setupAuthServer(t *testing.T, adminEmail string) (*fake.FakeMetadataStore, *httptest.Server) {
	t.Helper()
	store := fake.NewFakeMetadataStore()
	handler := auth.NewHandler(store, false, 1*time.Hour, adminEmail, "")

	mux := http.NewServeMux()
	handler.Register(mux)

	ts := httptest.NewServer(mux)
	return store, ts
}

// setupAuthServerWithMiddleware creates an auth handler and HTTP test server
// with session middleware applied to protected routes (invite, change password).
func setupAuthServerWithMiddleware(t *testing.T, adminEmail string) (*fake.FakeMetadataStore, *httptest.Server) {
	t.Helper()
	store := fake.NewFakeMetadataStore()
	handler := auth.NewHandler(store, false, 1*time.Hour, adminEmail, "")

	// Create main mux for public routes (login, logout)
	publicMux := http.NewServeMux()
	handler.Register(publicMux)

	// Wrap protected routes with session middleware
	sessionMw := auth.RequireAuth(store, false, 1*time.Hour)

	// Create the server with middleware for protected routes
	wrapper := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/users/invite",
			"/api/v1/users/me/password":
			sessionMw(publicMux).ServeHTTP(w, r)
		default:
			publicMux.ServeHTTP(w, r)
		}
	})

	ts := httptest.NewServer(wrapper)
	return store, ts
}

// loginAndGetCookie performs a login and returns the session cookie.
func loginAndGetCookie(t *testing.T, ts *httptest.Server, email, password string) string {
	t.Helper()
	payload, _ := json.Marshal(map[string]string{
		"email":    email,
		"password": password,
	})
	req, _ := http.NewRequest("POST", ts.URL+"/login", bytes.NewReader(payload))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	defer resp.Body.Close()

	for _, c := range resp.Cookies() {
		if c.Name == "omneval_session" {
			return c.Value
		}
	}
	t.Fatal("no session cookie")
	return ""
}

func TestHandler_Login_Success(t *testing.T) {
	store, ts := setupAuthServer(t, "admin@example.com")
	if err := store.CreateUser(nil, &domain.User{
		UserID:       "user-1",
		OrgID:        "org-1",
		Email:        "alice@example.com",
		PasswordHash: "correct-password",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}

	sessionID := loginAndGetCookie(t, ts, "alice@example.com", "correct-password")
	if sessionID == "" {
		t.Fatal("expected session ID")
	}

	// Verify session was created in store
	_, err := store.GetSession(nil, sessionID)
	if err != nil {
		t.Errorf("session should exist: %v", err)
	}
}

func TestHandler_Login_401InvalidPassword(t *testing.T) {
	store, ts := setupAuthServer(t, "admin@example.com")
	_ = store.CreateUser(nil, &domain.User{
		UserID:       "user-1",
		OrgID:        "org-1",
		Email:        "alice@example.com",
		PasswordHash: "correct-password",
	})

	payload, _ := json.Marshal(map[string]string{
		"email":    "alice@example.com",
		"password": "wrong-password",
	})
	req, _ := http.NewRequest("POST", ts.URL+"/login", bytes.NewReader(payload))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestHandler_Login_401UnknownUser(t *testing.T) {
	_, ts := setupAuthServer(t, "admin@example.com")

	payload, _ := json.Marshal(map[string]string{
		"email":    "nobody@example.com",
		"password": "any-password",
	})
	req, _ := http.NewRequest("POST", ts.URL+"/login", bytes.NewReader(payload))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestHandler_Login_400MissingFields(t *testing.T) {
	_, ts := setupAuthServer(t, "admin@example.com")

	payload := []byte(`{}`)
	req, _ := http.NewRequest("POST", ts.URL+"/login", bytes.NewReader(payload))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestHandler_Logout_Success(t *testing.T) {
	store, ts := setupAuthServer(t, "admin@example.com")
	_ = store.CreateUser(nil, &domain.User{
		UserID:       "user-1",
		OrgID:        "org-1",
		Email:        "alice@example.com",
		PasswordHash: "correct-password",
	})

	sessionID := loginAndGetCookie(t, ts, "alice@example.com", "correct-password")

	// Logout
	req, _ := http.NewRequest("POST", ts.URL+"/logout", nil)
	req.AddCookie(&http.Cookie{Name: "omneval_session", Value: sessionID})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("logout request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// Verify session was deleted
	_, err = store.GetSession(nil, sessionID)
	if err == nil {
		t.Error("session should have been deleted")
	}
}

func TestHandler_Logout_NotLoggedIn(t *testing.T) {
	_, ts := setupAuthServer(t, "admin@example.com")

	req, _ := http.NewRequest("POST", ts.URL+"/logout", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestHandler_Invite_Success(t *testing.T) {
	store, ts := setupAuthServerWithMiddleware(t, "admin@example.com")
	_ = store.CreateUser(nil, &domain.User{
		UserID:       "admin-1",
		OrgID:        "org-1",
		Email:        "admin@example.com",
		PasswordHash: "admin-password",
	})

	sessionID := loginAndGetCookie(t, ts, "admin@example.com", "admin-password")

	invitePayload, _ := json.Marshal(map[string]string{
		"email":  "newuser@example.com",
		"org_id": "org-1",
	})
	inviteReq, _ := http.NewRequest("POST", ts.URL+"/api/v1/users/invite", bytes.NewReader(invitePayload))
	inviteReq.AddCookie(&http.Cookie{Name: "omneval_session", Value: sessionID})
	inviteResp, err := http.DefaultClient.Do(inviteReq)
	if err != nil {
		t.Fatalf("invite request failed: %v", err)
	}
	defer inviteResp.Body.Close()

	if inviteResp.StatusCode != http.StatusCreated {
		t.Errorf("status: got %d, want %d", inviteResp.StatusCode, http.StatusCreated)
		return
	}

	var body auth.InviteResponse
	if err := json.NewDecoder(inviteResp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.UserID == "" {
		t.Error("expected user_id in invite response")
	}
	if body.Email != "newuser@example.com" {
		t.Errorf("email: got %q, want %q", body.Email, "newuser@example.com")
	}
	if body.PasswordResetToken == "" {
		t.Error("expected password_reset_token in invite response")
	}

	// Verify user was created
	users, _ := store.ListUsers(nil, "org-1")
	if len(users) != 2 {
		t.Errorf("expected 2 users, got %d", len(users))
	}
}

func TestHandler_Invite_ForbiddenNonAdmin(t *testing.T) {
	store, ts := setupAuthServerWithMiddleware(t, "admin@example.com")
	_ = store.CreateUser(nil, &domain.User{
		UserID:       "user-1",
		OrgID:        "org-1",
		Email:        "regular@example.com",
		PasswordHash: "regular-password",
	})

	sessionID := loginAndGetCookie(t, ts, "regular@example.com", "regular-password")

	invitePayload, _ := json.Marshal(map[string]string{
		"email":  "newuser@example.com",
		"org_id": "org-1",
	})
	inviteReq, _ := http.NewRequest("POST", ts.URL+"/api/v1/users/invite", bytes.NewReader(invitePayload))
	inviteReq.AddCookie(&http.Cookie{Name: "omneval_session", Value: sessionID})
	inviteResp, err := http.DefaultClient.Do(inviteReq)
	if err != nil {
		t.Fatalf("invite request failed: %v", err)
	}
	defer inviteResp.Body.Close()

	if inviteResp.StatusCode != http.StatusForbidden {
		t.Errorf("status: got %d, want %d", inviteResp.StatusCode, http.StatusForbidden)
	}
}

func TestHandler_ChangePassword_Success(t *testing.T) {
	store, ts := setupAuthServerWithMiddleware(t, "admin@example.com")
	_ = store.CreateUser(nil, &domain.User{
		UserID:       "user-1",
		OrgID:        "org-1",
		Email:        "alice@example.com",
		PasswordHash: "old-password",
	})

	sessionID := loginAndGetCookie(t, ts, "alice@example.com", "old-password")

	changePayload, _ := json.Marshal(map[string]string{
		"current_password": "old-password",
		"new_password":     "new-password",
	})
	changeReq, _ := http.NewRequest("PUT", ts.URL+"/api/v1/users/me/password", bytes.NewReader(changePayload))
	changeReq.AddCookie(&http.Cookie{Name: "omneval_session", Value: sessionID})
	changeResp, err := http.DefaultClient.Do(changeReq)
	if err != nil {
		t.Fatalf("change password request failed: %v", err)
	}
	defer changeResp.Body.Close()

	if changeResp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want %d", changeResp.StatusCode, http.StatusOK)
	}

	// Verify old password no longer works
	loginPayload, _ := json.Marshal(map[string]string{
		"email":    "alice@example.com",
		"password": "old-password",
	})
	req, _ := http.NewRequest("POST", ts.URL+"/login", bytes.NewReader(loginPayload))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("login request failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		t.Error("old password should no longer work after change")
	}

	// Verify new password works
	loginPayload2, _ := json.Marshal(map[string]string{
		"email":    "alice@example.com",
		"password": "new-password",
	})
	req2, _ := http.NewRequest("POST", ts.URL+"/login", bytes.NewReader(loginPayload2))
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("login request failed: %v", err)
	}
	resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		t.Errorf("new password should work, got status %d", resp2.StatusCode)
	}
}

func TestHandler_ChangePassword_WrongCurrent(t *testing.T) {
	store, ts := setupAuthServerWithMiddleware(t, "admin@example.com")
	_ = store.CreateUser(nil, &domain.User{
		UserID:       "user-1",
		OrgID:        "org-1",
		Email:        "alice@example.com",
		PasswordHash: "correct-password",
	})

	sessionID := loginAndGetCookie(t, ts, "alice@example.com", "correct-password")

	changePayload, _ := json.Marshal(map[string]string{
		"current_password": "wrong-password",
		"new_password":     "new-password",
	})
	changeReq, _ := http.NewRequest("PUT", ts.URL+"/api/v1/users/me/password", bytes.NewReader(changePayload))
	changeReq.AddCookie(&http.Cookie{Name: "omneval_session", Value: sessionID})
	changeResp, err := http.DefaultClient.Do(changeReq)
	if err != nil {
		t.Fatalf("change password request failed: %v", err)
	}
	defer changeResp.Body.Close()

	if changeResp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", changeResp.StatusCode, http.StatusUnauthorized)
	}
}

func TestHandler_ChangePassword_MissingFields(t *testing.T) {
	store, ts := setupAuthServerWithMiddleware(t, "admin@example.com")
	_ = store.CreateUser(nil, &domain.User{
		UserID:       "user-1",
		OrgID:        "org-1",
		Email:        "alice@example.com",
		PasswordHash: "password",
	})

	sessionID := loginAndGetCookie(t, ts, "alice@example.com", "password")

	changePayload, _ := json.Marshal(map[string]string{})
	changeReq, _ := http.NewRequest("PUT", ts.URL+"/api/v1/users/me/password", bytes.NewReader(changePayload))
	changeReq.AddCookie(&http.Cookie{Name: "omneval_session", Value: sessionID})
	changeResp, err := http.DefaultClient.Do(changeReq)
	if err != nil {
		t.Fatalf("change password request failed: %v", err)
	}
	defer changeResp.Body.Close()

	if changeResp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", changeResp.StatusCode, http.StatusBadRequest)
	}
}

func TestHandler_SetSessionCookie_HttpOnly(t *testing.T) {
	w := httptest.NewRecorder()
	auth.SetSessionCookie(w, "test-session", false, 1*time.Hour)

	cookies := w.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected session cookie")
	}
	c := cookies[0]
	if !c.HttpOnly {
		t.Error("expected HttpOnly flag")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite: got %v, want %v", c.SameSite, http.SameSiteLaxMode)
	}
	if c.Path != "/" {
		t.Errorf("Path: got %q, want %q", c.Path, "/")
	}
}

func TestHandler_SetSessionCookie_SecureFlag(t *testing.T) {
	w := httptest.NewRecorder()
	auth.SetSessionCookie(w, "test-session", true, 1*time.Hour)

	cookies := w.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected session cookie")
	}
	c := cookies[0]
	if !c.Secure {
		t.Error("expected Secure flag")
	}
}

func TestHandler_ClearSessionCookie(t *testing.T) {
	w := httptest.NewRecorder()
	auth.ClearSessionCookie(w, false)

	cookies := w.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected session cookie")
	}
	c := cookies[0]
	if c.Name != "omneval_session" {
		t.Errorf("cookie name: got %q, want %q", c.Name, "omneval_session")
	}
	if c.MaxAge != -1 {
		t.Errorf("MaxAge: got %d, want -1", c.MaxAge)
	}
}

func TestHandler_Login_ReturnsJSONContentType(t *testing.T) {
	store, ts := setupAuthServer(t, "admin@example.com")
	_ = store.CreateUser(nil, &domain.User{
		UserID:       "user-1",
		OrgID:        "org-1",
		Email:        "alice@example.com",
		PasswordHash: "correct-password",
	})

	payload, _ := json.Marshal(map[string]string{
		"email":    "alice@example.com",
		"password": "correct-password",
	})
	req, _ := http.NewRequest("POST", ts.URL+"/login", bytes.NewReader(payload))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type: got %q, want %q", ct, "application/json")
	}

	var body auth.LoginResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.SessionID == "" {
		t.Error("expected session_id in response")
	}
}

func TestHandler_Logout_ReturnsJSON(t *testing.T) {
	store, ts := setupAuthServer(t, "admin@example.com")
	_ = store.CreateUser(nil, &domain.User{
		UserID:       "user-1",
		OrgID:        "org-1",
		Email:        "alice@example.com",
		PasswordHash: "correct-password",
	})

	sessionID := loginAndGetCookie(t, ts, "alice@example.com", "correct-password")

	req, _ := http.NewRequest("POST", ts.URL+"/logout", nil)
	req.AddCookie(&http.Cookie{Name: "omneval_session", Value: sessionID})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.Header.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type: got %q, want %q", resp.Header.Get("Content-Type"), "application/json")
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["status"] != "logged_out" {
		t.Errorf("status: got %q, want %q", body["status"], "logged_out")
	}
}

func TestHandler_Invite_GeneratesResetToken(t *testing.T) {
	store, ts := setupAuthServerWithMiddleware(t, "admin@example.com")
	_ = store.CreateUser(nil, &domain.User{
		UserID:       "admin-1",
		OrgID:        "org-1",
		Email:        "admin@example.com",
		PasswordHash: "admin-password",
	})

	sessionID := loginAndGetCookie(t, ts, "admin@example.com", "admin-password")

	invitePayload, _ := json.Marshal(map[string]string{
		"email":  "newuser@example.com",
		"org_id": "org-1",
	})
	inviteReq, _ := http.NewRequest("POST", ts.URL+"/api/v1/users/invite", bytes.NewReader(invitePayload))
	inviteReq.AddCookie(&http.Cookie{Name: "omneval_session", Value: sessionID})
	inviteResp, err := http.DefaultClient.Do(inviteReq)
	if err != nil {
		t.Fatalf("invite request failed: %v", err)
	}
	defer inviteResp.Body.Close()

	if inviteResp.StatusCode != http.StatusCreated {
		t.Fatalf("status: got %d, want %d", inviteResp.StatusCode, http.StatusCreated)
	}

	var body auth.InviteResponse
	if err := json.NewDecoder(inviteResp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if body.PasswordResetToken == "" {
		t.Fatal("expected password_reset_token in response")
	}

	// Verify the reset token can be used to set a password and then login
	resetPayload, _ := json.Marshal(map[string]string{
		"token":        body.PasswordResetToken,
		"new_password": "new-password",
	})
	resetReq, _ := http.NewRequest("POST", ts.URL+"/api/v1/users/reset-password", bytes.NewReader(resetPayload))
	resetReq.Header.Set("Content-Type", "application/json")
	resetResp, err := http.DefaultClient.Do(resetReq)
	if err != nil {
		t.Fatalf("reset password request failed: %v", err)
	}
	defer resetResp.Body.Close()

	if resetResp.StatusCode != http.StatusOK {
		t.Errorf("reset password should succeed, got status %d", resetResp.StatusCode)
	}

	// Debug: check what the user's password hash looks like in the store
	user, err := store.GetUserByEmail(nil, "newuser@example.com")
	if err != nil {
		t.Fatalf("get user by email after reset: %v", err)
	}
	if user.PasswordHash == "" {
		t.Fatal("user password_hash is empty after reset (this means update failed)")
	}
	if len(user.PasswordHash) < 10 {
		t.Fatalf("user password_hash looks wrong: %q", user.PasswordHash)
	}

	// Now login with the new password
	loginPayload, _ := json.Marshal(map[string]string{
		"email":    "newuser@example.com",
		"password": "new-password",
	})
	loginReq, _ := http.NewRequest("POST", ts.URL+"/login", bytes.NewReader(loginPayload))
	loginResp, err := http.DefaultClient.Do(loginReq)
	if err != nil {
		t.Fatalf("login request failed: %v", err)
	}
	loginResp.Body.Close()

	if loginResp.StatusCode != http.StatusOK {
		// Debug: read the response body
		var buf bytes.Buffer
		buf.ReadFrom(loginResp.Body)
		t.Logf("login response body: %q, status: %d", buf.String(), loginResp.StatusCode)
		t.Errorf("login with new password should succeed, got status %d", loginResp.StatusCode)
	}
}

func TestHandler_AdminBootstrap_CreatesAdmin(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	h := auth.NewHandler(store, false, 1*time.Hour, "admin@example.com", "admin-password")

	// No users exist, bootstrap should create one
	created, err := h.BootstrapAdmin(context.Background())
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if !created {
		t.Error("expected admin to be created")
	}

	count, err := store.CountUsers(context.Background())
	if err != nil {
		t.Fatalf("count users: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 user, got %d", count)
	}

	// Verify we can login with the admin
	user, err := store.GetUserByEmail(context.Background(), "admin@example.com")
	if err != nil {
		t.Fatalf("get admin: %v", err)
	}
	if err := store.CheckPassword(user.PasswordHash, "admin-password"); err != nil {
		t.Fatalf("admin password should match")
	}
}

func TestHandler_AdminBootstrap_NoOpWhenUsersExist(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	h := auth.NewHandler(store, false, 1*time.Hour, "admin@example.com", "admin-password")

	// Create a user first
	_ = store.CreateUser(nil, &domain.User{
		UserID:       "existing-user",
		OrgID:        "org-1",
		Email:        "existing@example.com",
		PasswordHash: "password",
	})

	// Bootstrap should be a no-op
	created, err := h.BootstrapAdmin(context.Background())
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if created {
		t.Error("expected bootstrap to be a no-op when users exist")
	}

	count, err := store.CountUsers(context.Background())
	if err != nil {
		t.Fatalf("count users: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 user, got %d", count)
	}
}

func TestHandler_AdminBootstrap_NoConfigNoOp(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	h := auth.NewHandler(store, false, 1*time.Hour, "", "")

	// No admin config, bootstrap should be a no-op
	created, err := h.BootstrapAdmin(context.Background())
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if created {
		t.Error("expected bootstrap to be a no-op without admin config")
	}

	count, err := store.CountUsers(context.Background())
	if err != nil {
		t.Fatalf("count users: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 users, got %d", count)
	}
}

// setupAuthServerWithAllMiddleware creates an auth handler and HTTP test server
// with session middleware applied to all protected routes.
func setupAuthServerWithAllMiddleware(t *testing.T, adminEmail string) (*fake.FakeMetadataStore, *httptest.Server) {
	t.Helper()
	store := fake.NewFakeMetadataStore()
	handler := auth.NewHandler(store, false, 1*time.Hour, adminEmail, "")

	// Create main mux for public routes (login, logout)
	publicMux := http.NewServeMux()
	handler.Register(publicMux)

	// Wrap ALL non-public routes with session middleware
	sessionMw := auth.RequireAuth(store, false, 1*time.Hour)

	// Route-based wrapper: public routes pass through, protected routes get session middleware
	wrapper := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "POST /login", "POST /logout":
			publicMux.ServeHTTP(w, r)
		default:
			sessionMw(publicMux).ServeHTTP(w, r)
		}
	})

	ts := httptest.NewServer(wrapper)
	return store, ts
}

func TestHandler_CreateProject_Success(t *testing.T) {
	store, ts := setupAuthServerWithAllMiddleware(t, "admin@example.com")

	// Create org and user
	_ = store.CreateOrganization(nil, &domain.Organization{OrgID: "org-1", Name: "Test Org"})
	_ = store.CreateUser(nil, &domain.User{
		UserID:       "user-1",
		OrgID:        "org-1",
		Email:        "alice@example.com",
		PasswordHash: "password",
	})

	sessionID := loginAndGetCookie(t, ts, "alice@example.com", "password")

	// Create a project
	projectPayload, _ := json.Marshal(map[string]string{"name": "My Project"})
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/projects", bytes.NewReader(projectPayload))
	req.AddCookie(&http.Cookie{Name: "omneval_session", Value: sessionID})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create project request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusCreated)
	}

	var body auth.CreateProjectResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.ProjectID == "" {
		t.Error("expected project_id in response")
	}
	if body.Name != "My Project" {
		t.Errorf("name: got %q, want %q", body.Name, "My Project")
	}

	// Verify project was stored
	projects, err := store.ListProjects(nil, "org-1")
	if err != nil {
		t.Fatalf("list projects: %v", err)
	}
	if len(projects) != 1 {
		t.Errorf("expected 1 project, got %d", len(projects))
	}
}

func TestHandler_CreateProject_RequiresName(t *testing.T) {
	store, ts := setupAuthServerWithAllMiddleware(t, "admin@example.com")
	_ = store.CreateUser(nil, &domain.User{
		UserID:       "user-1",
		OrgID:        "org-1",
		Email:        "alice@example.com",
		PasswordHash: "password",
	})

	sessionID := loginAndGetCookie(t, ts, "alice@example.com", "password")

	// Empty name
	projectPayload, _ := json.Marshal(map[string]string{"name": ""})
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/projects", bytes.NewReader(projectPayload))
	req.AddCookie(&http.Cookie{Name: "omneval_session", Value: sessionID})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create project request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestHandler_CreateProject_Unauthorized(t *testing.T) {
	_, ts := setupAuthServerWithAllMiddleware(t, "admin@example.com")

	projectPayload, _ := json.Marshal(map[string]string{"name": "My Project"})
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/projects", bytes.NewReader(projectPayload))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestHandler_GenerateAPIKey_ProjectKey(t *testing.T) {
	store, ts := setupAuthServerWithAllMiddleware(t, "admin@example.com")

	// Create org, user, and project
	_ = store.CreateOrganization(nil, &domain.Organization{OrgID: "org-1", Name: "Test Org"})
	_ = store.CreateUser(nil, &domain.User{
		UserID:       "user-1",
		OrgID:        "org-1",
		Email:        "alice@example.com",
		PasswordHash: "password",
	})
	project := &domain.Project{ProjectID: "proj-1", OrgID: "org-1", Name: "Test Project"}
	_ = store.CreateProject(nil, project)

	sessionID := loginAndGetCookie(t, ts, "alice@example.com", "password")

	// Generate a project-scoped API key
	keyPayload, _ := json.Marshal(map[string]string{"kind": "project"})
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/projects/proj-1/api-keys", bytes.NewReader(keyPayload))
	req.AddCookie(&http.Cookie{Name: "omneval_session", Value: sessionID})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("generate key request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusCreated)
		t.Logf("body: %s", readBody(t, resp))
	}

	var body auth.GenerateAPIKeyResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.ProjectID != "proj-1" {
		t.Errorf("project_id: got %q, want %q", body.ProjectID, "proj-1")
	}
	if body.Kind != domain.APIKeyKindProject {
		t.Errorf("kind: got %q, want %q", body.Kind, domain.APIKeyKindProject)
	}
	if body.RawKey == "" {
		t.Error("expected raw_key in response")
	}
	if !strings.HasPrefix(body.RawKey, "oev_proj_") && !strings.HasPrefix(body.RawKey, "oev_svc_") {
		t.Errorf("raw_key prefix: got %q, expected oev_proj_ or oev_svc_", body.RawKey)
	}
}

func TestHandler_GenerateAPIKey_ServiceKey(t *testing.T) {
	store, ts := setupAuthServerWithAllMiddleware(t, "admin@example.com")
	_ = store.CreateOrganization(nil, &domain.Organization{OrgID: "org-1", Name: "Test Org"})
	_ = store.CreateUser(nil, &domain.User{
		UserID:       "user-1",
		OrgID:        "org-1",
		Email:        "alice@example.com",
		PasswordHash: "password",
	})
	_ = store.CreateProject(nil, &domain.Project{ProjectID: "proj-1", OrgID: "org-1", Name: "Test"})

	sessionID := loginAndGetCookie(t, ts, "alice@example.com", "password")

	keyPayload, _ := json.Marshal(map[string]string{
		"kind":         "service",
		"service_name": "my-agent",
	})
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/projects/proj-1/api-keys", bytes.NewReader(keyPayload))
	req.AddCookie(&http.Cookie{Name: "omneval_session", Value: sessionID})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("generate key request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusCreated)
	}

	var body auth.GenerateAPIKeyResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Kind != domain.APIKeyKindService {
		t.Errorf("kind: got %q, want %q", body.Kind, domain.APIKeyKindService)
	}
	if body.ServiceName != "my-agent" {
		t.Errorf("service_name: got %q, want %q", body.ServiceName, "my-agent")
	}
	if body.RawKey == "" {
		t.Error("expected raw_key")
	}
}

func TestHandler_GenerateAPIKey_ServiceKeyRequiresServiceName(t *testing.T) {
	store, ts := setupAuthServerWithAllMiddleware(t, "admin@example.com")
	_ = store.CreateOrganization(nil, &domain.Organization{OrgID: "org-1", Name: "Test Org"})
	_ = store.CreateUser(nil, &domain.User{
		UserID:       "user-1",
		OrgID:        "org-1",
		Email:        "alice@example.com",
		PasswordHash: "password",
	})
	_ = store.CreateProject(nil, &domain.Project{ProjectID: "proj-1", OrgID: "org-1", Name: "Test"})

	sessionID := loginAndGetCookie(t, ts, "alice@example.com", "password")

	keyPayload, _ := json.Marshal(map[string]string{"kind": "service"})
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/projects/proj-1/api-keys", bytes.NewReader(keyPayload))
	req.AddCookie(&http.Cookie{Name: "omneval_session", Value: sessionID})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestHandler_GenerateAPIKey_ProjectNotFound(t *testing.T) {
	store, ts := setupAuthServerWithAllMiddleware(t, "admin@example.com")
	_ = store.CreateUser(nil, &domain.User{
		UserID:       "user-1",
		OrgID:        "org-1",
		Email:        "alice@example.com",
		PasswordHash: "password",
	})

	sessionID := loginAndGetCookie(t, ts, "alice@example.com", "password")

	keyPayload, _ := json.Marshal(map[string]string{"kind": "project"})
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/projects/nonexistent/api-keys", bytes.NewReader(keyPayload))
	req.AddCookie(&http.Cookie{Name: "omneval_session", Value: sessionID})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

func TestHandler_ListAPIKeys(t *testing.T) {
	store, ts := setupAuthServerWithAllMiddleware(t, "admin@example.com")
	_ = store.CreateOrganization(nil, &domain.Organization{OrgID: "org-1", Name: "Test Org"})
	_ = store.CreateUser(nil, &domain.User{
		UserID:       "user-1",
		OrgID:        "org-1",
		Email:        "alice@example.com",
		PasswordHash: "password",
	})
	_ = store.CreateProject(nil, &domain.Project{ProjectID: "proj-1", OrgID: "org-1", Name: "Test"})

	// Pre-seed two API keys
	rawKey1, hashedKey1, _ := internalauth.Generate(domain.APIKeyKindProject)
	hashedKey2 := "hash-2"
	_ = store.CreateAPIKey(nil, &domain.APIKey{KeyID: "key-1", ProjectID: "proj-1", Kind: domain.APIKeyKindProject, HashedKey: hashedKey1})
	_ = store.CreateAPIKey(nil, &domain.APIKey{KeyID: "key-2", ProjectID: "proj-1", Kind: domain.APIKeyKindService, ServiceName: "agent-1", HashedKey: hashedKey2})
	_ = rawKey1 // compiler: rawKey1 is intentionally unused (only the hash is stored in the DB)

	sessionID := loginAndGetCookie(t, ts, "alice@example.com", "password")

	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/projects/proj-1/api-keys", nil)
	req.AddCookie(&http.Cookie{Name: "omneval_session", Value: sessionID})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("list keys request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var keys []auth.APIKeyInfo
	if err := json.NewDecoder(resp.Body).Decode(&keys); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(keys) != 2 {
		t.Errorf("expected 2 keys, got %d", len(keys))
	}
}

func TestHandler_RevokeAPIKey(t *testing.T) {
	store, ts := setupAuthServerWithAllMiddleware(t, "admin@example.com")
	_ = store.CreateOrganization(nil, &domain.Organization{OrgID: "org-1", Name: "Test Org"})
	_ = store.CreateUser(nil, &domain.User{
		UserID:       "user-1",
		OrgID:        "org-1",
		Email:        "alice@example.com",
		PasswordHash: "password",
	})
	_ = store.CreateProject(nil, &domain.Project{ProjectID: "proj-1", OrgID: "org-1", Name: "Test"})
	_ = store.CreateAPIKey(nil, &domain.APIKey{KeyID: "key-1", ProjectID: "proj-1", Kind: domain.APIKeyKindProject, HashedKey: "hash-1"})

	sessionID := loginAndGetCookie(t, ts, "alice@example.com", "password")

	req, _ := http.NewRequest("DELETE", ts.URL+"/api/v1/projects/proj-1/api-keys/key-1", nil)
	req.AddCookie(&http.Cookie{Name: "omneval_session", Value: sessionID})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("revoke request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "revoked" {
		t.Errorf("status: got %q, want %q", body["status"], "revoked")
	}

	// Verify key is now revoked
	keys, _ := store.ListAPIKeys(nil, "proj-1")
	if len(keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(keys))
	}
	if keys[0].RevokedAt == nil {
		t.Error("expected key to be revoked")
	}
}

func TestHandler_RevokeAPIKey_NotFound(t *testing.T) {
	store, ts := setupAuthServerWithAllMiddleware(t, "admin@example.com")
	_ = store.CreateUser(nil, &domain.User{
		UserID:       "user-1",
		OrgID:        "org-1",
		Email:        "alice@example.com",
		PasswordHash: "password",
	})

	sessionID := loginAndGetCookie(t, ts, "alice@example.com", "password")

	req, _ := http.NewRequest("DELETE", ts.URL+"/api/v1/projects/proj-1/api-keys/nonexistent", nil)
	req.AddCookie(&http.Cookie{Name: "omneval_session", Value: sessionID})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

func TestHandler_ListAPIKeys_ProjectNotFound(t *testing.T) {
	store, ts := setupAuthServerWithAllMiddleware(t, "admin@example.com")
	_ = store.CreateUser(nil, &domain.User{
		UserID:       "user-1",
		OrgID:        "org-1",
		Email:        "alice@example.com",
		PasswordHash: "password",
	})

	sessionID := loginAndGetCookie(t, ts, "alice@example.com", "password")

	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/projects/nonexistent/api-keys", nil)
	req.AddCookie(&http.Cookie{Name: "omneval_session", Value: sessionID})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

func TestHandler_GenerateAPIKey_ReturnsRawKeyOnce(t *testing.T) {
	store, ts := setupAuthServerWithAllMiddleware(t, "admin@example.com")
	_ = store.CreateOrganization(nil, &domain.Organization{OrgID: "org-1", Name: "Test Org"})
	_ = store.CreateUser(nil, &domain.User{
		UserID:       "user-1",
		OrgID:        "org-1",
		Email:        "alice@example.com",
		PasswordHash: "password",
	})
	_ = store.CreateProject(nil, &domain.Project{ProjectID: "proj-1", OrgID: "org-1", Name: "Test"})

	sessionID := loginAndGetCookie(t, ts, "alice@example.com", "password")

	// Generate a key
	keyPayload, _ := json.Marshal(map[string]string{"kind": "project"})
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/projects/proj-1/api-keys", bytes.NewReader(keyPayload))
	req.AddCookie(&http.Cookie{Name: "omneval_session", Value: sessionID})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var body auth.GenerateAPIKeyResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Verify the raw key has the correct prefix
	if len(body.RawKey) < 10 {
		t.Errorf("raw_key too short: %q", body.RawKey)
	}
	if body.RawKey[:9] != "oev_proj_" {
		t.Errorf("raw_key prefix: got %q, want %q", body.RawKey[:9], "oev_proj_")
	}
}

func TestHandler_Invite_NoPasswordInResponse(t *testing.T) {
	store, ts := setupAuthServerWithMiddleware(t, "admin@example.com")
	_ = store.CreateUser(nil, &domain.User{
		UserID:       "admin-1",
		OrgID:        "org-1",
		Email:        "admin@example.com",
		PasswordHash: "admin-password",
	})

	sessionID := loginAndGetCookie(t, ts, "admin@example.com", "admin-password")

	invitePayload, _ := json.Marshal(map[string]string{
		"email":  "newuser@example.com",
		"org_id": "org-1",
	})
	inviteReq, _ := http.NewRequest("POST", ts.URL+"/api/v1/users/invite", bytes.NewReader(invitePayload))
	inviteReq.AddCookie(&http.Cookie{Name: "omneval_session", Value: sessionID})
	inviteResp, err := http.DefaultClient.Do(inviteReq)
	if err != nil {
		t.Fatalf("invite request failed: %v", err)
	}
	defer inviteResp.Body.Close()

	if inviteResp.StatusCode != http.StatusCreated {
		t.Fatalf("status: got %d, want %d", inviteResp.StatusCode, http.StatusCreated)
	}

	// Read the raw body
	var rawBody map[string]any
	if err := json.NewDecoder(inviteResp.Body).Decode(&rawBody); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	// Password must NEVER appear in the invite response
	if _, hasPassword := rawBody["password"]; hasPassword {
		t.Error("invite response must not include a 'password' field (security risk)")
	}

	// Response should include user_id and email
	if _, hasUserID := rawBody["user_id"]; !hasUserID {
		t.Error("expected 'user_id' in invite response")
	}
	if _, hasEmail := rawBody["email"]; !hasEmail {
		t.Error("expected 'email' in invite response")
	}

	// Response should include a password_reset_token
	if _, hasToken := rawBody["password_reset_token"]; !hasToken {
		t.Error("expected 'password_reset_token' in invite response")
	}
}

func TestHandler_Invite_UsesResetToken(t *testing.T) {
	store, ts := setupAuthServerWithMiddleware(t, "admin@example.com")
	_ = store.CreateUser(nil, &domain.User{
		UserID:       "admin-1",
		OrgID:        "org-1",
		Email:        "admin@example.com",
		PasswordHash: "admin-password",
	})

	sessionID := loginAndGetCookie(t, ts, "admin@example.com", "admin-password")

	invitePayload, _ := json.Marshal(map[string]string{
		"email":  "newuser@example.com",
		"org_id": "org-1",
	})
	inviteReq, _ := http.NewRequest("POST", ts.URL+"/api/v1/users/invite", bytes.NewReader(invitePayload))
	inviteReq.AddCookie(&http.Cookie{Name: "omneval_session", Value: sessionID})
	inviteResp, err := http.DefaultClient.Do(inviteReq)
	if err != nil {
		t.Fatalf("invite request failed: %v", err)
	}
	defer inviteResp.Body.Close()

	if inviteResp.StatusCode != http.StatusCreated {
		t.Fatalf("status: got %d, want %d", inviteResp.StatusCode, http.StatusCreated)
	}

	var body auth.InviteResponse
	if err := json.NewDecoder(inviteResp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if body.UserID == "" {
		t.Error("expected user_id in invite response")
	}
	if body.Email != "newuser@example.com" {
		t.Errorf("email: got %q, want %q", body.Email, "newuser@example.com")
	}
	if body.PasswordResetToken == "" {
		t.Error("expected password_reset_token in invite response")
	}

	// Verify the user was created in the store
	users, _ := store.ListUsers(nil, "org-1")
	if len(users) != 2 {
		t.Errorf("expected 2 users, got %d", len(users))
	}
}

func TestHandler_ResetPassword_ValidToken(t *testing.T) {
	store, ts := setupAuthServerWithMiddleware(t, "admin@example.com")
	_ = store.CreateUser(nil, &domain.User{
		UserID:       "admin-1",
		OrgID:        "org-1",
		Email:        "admin@example.com",
		PasswordHash: "admin-password",
	})

	sessionID := loginAndGetCookie(t, ts, "admin@example.com", "admin-password")

	// Step 1: Invite the new user to get a reset token
	invitePayload, _ := json.Marshal(map[string]string{
		"email":  "newuser@example.com",
		"org_id": "org-1",
	})
	inviteReq, _ := http.NewRequest("POST", ts.URL+"/api/v1/users/invite", bytes.NewReader(invitePayload))
	inviteReq.AddCookie(&http.Cookie{Name: "omneval_session", Value: sessionID})
	inviteResp, err := http.DefaultClient.Do(inviteReq)
	if err != nil {
		t.Fatalf("invite request failed: %v", err)
	}
	var inviteBody auth.InviteResponse
	if err := json.NewDecoder(inviteResp.Body).Decode(&inviteBody); err != nil {
		t.Fatalf("decode invite: %v", err)
	}
	inviteResp.Body.Close()

	if inviteBody.PasswordResetToken == "" {
		t.Fatal("invite response must include a password_reset_token")
	}

	// Step 2: Use the reset token to set a new password
	resetPayload, _ := json.Marshal(map[string]string{
		"token":   inviteBody.PasswordResetToken,
		"new_password": "new-password",
	})
	resetReq, _ := http.NewRequest("POST", ts.URL+"/api/v1/users/reset-password", bytes.NewReader(resetPayload))
	resetReq.Header.Set("Content-Type", "application/json")
	resetResp, err := http.DefaultClient.Do(resetReq)
	if err != nil {
		t.Fatalf("reset password request failed: %v", err)
	}
	defer resetResp.Body.Close()

	if resetResp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want %d", resetResp.StatusCode, http.StatusOK)
	}

	// Step 3: Verify the user can log in with the new password
	loginPayload, _ := json.Marshal(map[string]string{
		"email":    "newuser@example.com",
		"password": "new-password",
	})
	loginReq, _ := http.NewRequest("POST", ts.URL+"/login", bytes.NewReader(loginPayload))
	loginResp, err := http.DefaultClient.Do(loginReq)
	if err != nil {
		t.Fatalf("login request failed: %v", err)
	}
	loginResp.Body.Close()

	if loginResp.StatusCode != http.StatusOK {
		t.Errorf("login with new password should succeed, got status %d", loginResp.StatusCode)
	}

	// Step 4: Verify the token is now consumed (single use)
	resetPayload2, _ := json.Marshal(map[string]string{
		"token":   inviteBody.PasswordResetToken,
		"new_password": "another-password",
	})
	resetReq2, _ := http.NewRequest("POST", ts.URL+"/api/v1/users/reset-password", bytes.NewReader(resetPayload2))
	resetReq2.Header.Set("Content-Type", "application/json")
	resetResp2, err := http.DefaultClient.Do(resetReq2)
	if err != nil {
		t.Fatalf("reset password request failed: %v", err)
	}
	defer resetResp2.Body.Close()

	if resetResp2.StatusCode != http.StatusBadRequest {
		t.Errorf("second use of reset token should be rejected, got status %d", resetResp2.StatusCode)
	}
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	var buf bytes.Buffer
	buf.ReadFrom(resp.Body)
	return buf.String()
}

// ── /me endpoint tests ───────────────────────────────────────────

func TestHandler_Me_Success(t *testing.T) {
	store, ts := setupAuthServerWithAllMiddleware(t, "admin@example.com")

	// Create org, user, and project
	_ = store.CreateOrganization(nil, &domain.Organization{OrgID: "org-1", Name: "Test Org"})
	_ = store.CreateUser(nil, &domain.User{
		UserID:       "user-1",
		OrgID:        "org-1",
		Email:        "alice@example.com",
		PasswordHash: "password",
	})
	_ = store.CreateProject(nil, &domain.Project{ProjectID: "proj-1", OrgID: "org-1", Name: "Test Project"})

	sessionID := loginAndGetCookie(t, ts, "alice@example.com", "password")

	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/me", nil)
	req.AddCookie(&http.Cookie{Name: "omneval_session", Value: sessionID})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("me request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusOK)
		t.Logf("body: %s", readBody(t, resp))
		return
	}

	var body auth.MeResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if body.UserID != "user-1" {
		t.Errorf("user_id: got %q, want %q", body.UserID, "user-1")
	}
	if body.Email != "alice@example.com" {
		t.Errorf("email: got %q, want %q", body.Email, "alice@example.com")
	}
	if len(body.Projects) != 1 {
		t.Errorf("projects length: got %d, want 1", len(body.Projects))
	}
	if len(body.Projects) > 0 && body.Projects[0].ProjectID != "proj-1" {
		t.Errorf("projects[0].project_id: got %q, want %q", body.Projects[0].ProjectID, "proj-1")
	}
}

func TestHandler_Me_401Unauthenticated(t *testing.T) {
	_, ts := setupAuthServerWithAllMiddleware(t, "admin@example.com")

	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/me", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("me request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["error"] != "unauthorized" {
		t.Errorf("error: got %q, want %q", body["error"], "unauthorized")
	}
}

func TestHandler_Me_JSONContentType(t *testing.T) {
	store, ts := setupAuthServerWithAllMiddleware(t, "admin@example.com")

	_ = store.CreateOrganization(nil, &domain.Organization{OrgID: "org-1", Name: "Test Org"})
	_ = store.CreateUser(nil, &domain.User{
		UserID:       "user-1",
		OrgID:        "org-1",
		Email:        "alice@example.com",
		PasswordHash: "password",
	})
	_ = store.CreateProject(nil, &domain.Project{ProjectID: "proj-1", OrgID: "org-1", Name: "Test Project"})

	sessionID := loginAndGetCookie(t, ts, "alice@example.com", "password")

	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/me", nil)
	req.AddCookie(&http.Cookie{Name: "omneval_session", Value: sessionID})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("me request failed: %v", err)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type: got %q, want %q", ct, "application/json")
	}
}

func TestHandler_GenerateAPIKey_EndToEnd(t *testing.T) {
	store, ts := setupAuthServerWithAllMiddleware(t, "admin@example.com")

	// Create org, user, and project
	_ = store.CreateOrganization(nil, &domain.Organization{OrgID: "org-1", Name: "Test Org"})
	_ = store.CreateUser(nil, &domain.User{
		UserID:       "user-1",
		OrgID:        "org-1",
		Email:        "alice@example.com",
		PasswordHash: "password",
	})
	project := &domain.Project{ProjectID: "proj-1", OrgID: "org-1", Name: "Test Project"}
	_ = store.CreateProject(nil, project)

	sessionID := loginAndGetCookie(t, ts, "alice@example.com", "password")

	// Generate a project-scoped API key via POST
	keyPayload, _ := json.Marshal(map[string]string{"kind": "project"})
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/projects/proj-1/api-keys", bytes.NewReader(keyPayload))
	req.AddCookie(&http.Cookie{Name: "omneval_session", Value: sessionID})
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("generate key request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status: got %d, want %d (created)", resp.StatusCode, http.StatusCreated)
	}

	var body auth.GenerateAPIKeyResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	// Verify the response has the correct structure
	if body.KeyID == "" {
		t.Error("expected key_id in response")
	}
	if body.ProjectID != "proj-1" {
		t.Errorf("project_id: got %q, want %q", body.ProjectID, "proj-1")
	}
	if body.Kind != domain.APIKeyKindProject {
		t.Errorf("kind: got %q, want %q", body.Kind, domain.APIKeyKindProject)
	}
	if body.RawKey == "" {
		t.Fatal("expected raw_key in response (this is what the frontend uses)")
	}

	// Verify the raw key format: prefix + 43 base58 chars
	expectedPrefix := "oev_proj_"
	if len(body.RawKey) < len(expectedPrefix)+43 {
		t.Errorf("raw_key too short: %q (expected at least %d chars)", body.RawKey, len(expectedPrefix)+43)
	}
	if body.RawKey[:len(expectedPrefix)] != expectedPrefix {
		t.Errorf("raw_key prefix: got %q, want %q", body.RawKey[:len(expectedPrefix)], expectedPrefix)
	}

	// Verify the key was stored in the database (hashed only, not raw)
	keys, err := store.ListAPIKeys(nil, "proj-1")
	if err != nil {
		t.Fatalf("list api keys: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("expected 1 key in store, got %d", len(keys))
	}
	if keys[0].KeyID != body.KeyID {
		t.Errorf("stored key_id: got %q, want %q", keys[0].KeyID, body.KeyID)
	}
	if keys[0].HashedKey == "" {
		t.Fatal("stored key should have a hashed_key")
	}
}

func TestHandler_GenerateAPIKey_ResponseFormat(t *testing.T) {
	store, ts := setupAuthServerWithAllMiddleware(t, "admin@example.com")

	// Create org, user, and project
	_ = store.CreateOrganization(nil, &domain.Organization{OrgID: "org-1", Name: "Test Org"})
	_ = store.CreateUser(nil, &domain.User{
		UserID:       "user-1",
		OrgID:        "org-1",
		Email:        "alice@example.com",
		PasswordHash: "password",
	})
	_ = store.CreateProject(nil, &domain.Project{ProjectID: "proj-1", OrgID: "org-1", Name: "Test"})

	sessionID := loginAndGetCookie(t, ts, "alice@example.com", "password")

	// Generate a key with the exact payload the frontend sends
	keyPayload, _ := json.Marshal(map[string]string{"kind": "project"})
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/projects/proj-1/api-keys", bytes.NewReader(keyPayload))
	req.AddCookie(&http.Cookie{Name: "omneval_session", Value: sessionID})
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status: got %d, want %d", resp.StatusCode, http.StatusCreated)
	}

	// Read the raw body to check JSON format
	var rawBody map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&rawBody); err != nil {
		t.Fatalf("decode response body: %v", err)
	}

	// The frontend reads data.raw_key
	if _, exists := rawBody["raw_key"]; !exists {
		t.Fatal("response must include 'raw_key' field for the frontend")
	}

	rawKey, ok := rawBody["raw_key"].(string)
	if !ok {
		t.Fatalf("raw_key must be a string, got %T: %v", rawBody["raw_key"], rawBody["raw_key"])
	}
	if rawKey == "" {
		t.Fatal("raw_key must not be empty")
	}
	if len(rawKey) < 50 {
		t.Errorf("raw_key too short: %q (expected at least 50 chars: oev_proj_ + 43 base58)", rawKey)
	}
}
