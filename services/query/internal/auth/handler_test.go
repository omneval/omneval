package auth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zbloss/lantern/internal/domain"
	"github.com/zbloss/lantern/internal/fake"
	"github.com/zbloss/lantern/services/query/internal/auth"
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
		if c.Name == "lantern_session" {
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
	req.AddCookie(&http.Cookie{Name: "lantern_session", Value: sessionID})
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
	inviteReq.AddCookie(&http.Cookie{Name: "lantern_session", Value: sessionID})
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
	if body.Password == "" {
		t.Error("expected temporary password in invite response")
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
	inviteReq.AddCookie(&http.Cookie{Name: "lantern_session", Value: sessionID})
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
	changeReq.AddCookie(&http.Cookie{Name: "lantern_session", Value: sessionID})
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
	changeReq.AddCookie(&http.Cookie{Name: "lantern_session", Value: sessionID})
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
	changeReq.AddCookie(&http.Cookie{Name: "lantern_session", Value: sessionID})
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
	if c.Name != "lantern_session" {
		t.Errorf("cookie name: got %q, want %q", c.Name, "lantern_session")
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
	req.AddCookie(&http.Cookie{Name: "lantern_session", Value: sessionID})
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

func TestHandler_Invite_GeneratesTempPassword(t *testing.T) {
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
	inviteReq.AddCookie(&http.Cookie{Name: "lantern_session", Value: sessionID})
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

	// Verify the temp password can be used for login
	loginPayload, _ := json.Marshal(map[string]string{
		"email":    "newuser@example.com",
		"password": body.Password,
	})
	loginReq, _ := http.NewRequest("POST", ts.URL+"/login", bytes.NewReader(loginPayload))
	loginResp, err := http.DefaultClient.Do(loginReq)
	if err != nil {
		t.Fatalf("login with temp password failed: %v", err)
	}
	loginResp.Body.Close()

	if loginResp.StatusCode != http.StatusOK {
		t.Errorf("login with temp password should succeed, got status %d", loginResp.StatusCode)
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

func TestHandler_Invite_DuplicateEmail(t *testing.T) {
	store, ts := setupAuthServerWithMiddleware(t, "admin@example.com")
	_ = store.CreateUser(nil, &domain.User{
		UserID:       "admin-1",
		OrgID:        "org-1",
		Email:        "admin@example.com",
		PasswordHash: "admin-password",
	})

	sessionID := loginAndGetCookie(t, ts, "admin@example.com", "admin-password")

	// First invite succeeds
	invitePayload, _ := json.Marshal(map[string]string{
		"email":  "newuser@example.com",
		"org_id": "org-1",
	})
	inviteReq, _ := http.NewRequest("POST", ts.URL+"/api/v1/users/invite", bytes.NewReader(invitePayload))
	inviteReq.AddCookie(&http.Cookie{Name: "lantern_session", Value: sessionID})
	inviteResp, _ := http.DefaultClient.Do(inviteReq)
	inviteResp.Body.Close()

	// Second invite with same email fails
	inviteReq2, _ := http.NewRequest("POST", ts.URL+"/api/v1/users/invite", bytes.NewReader(invitePayload))
	inviteReq2.AddCookie(&http.Cookie{Name: "lantern_session", Value: sessionID})
	inviteResp2, err := http.DefaultClient.Do(inviteReq2)
	if err != nil {
		t.Fatalf("second invite request failed: %v", err)
	}
	defer inviteResp2.Body.Close()

	if inviteResp2.StatusCode != http.StatusConflict {
		t.Errorf("duplicate invite status: got %d, want %d", inviteResp2.StatusCode, http.StatusConflict)
	}
}
