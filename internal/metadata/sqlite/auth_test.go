package sqlite_test

import (
	"context"
	"errors"
	"testing"

	"github.com/zbloss/lantern/internal/domain"
	"github.com/zbloss/lantern/internal/metadata"
)

func TestUser_CountUsers(t *testing.T) {
	s, _ := openTestStore(t)
	defer s.Close()

	// Initially zero
	count, err := s.CountUsers(context.Background())
	if err != nil {
		t.Fatalf("count users: %v", err)
	}
	if count != 0 {
		t.Errorf("count: got %d, want 0", count)
	}

	// Add users
	s.CreateOrganization(context.Background(), &domain.Organization{OrgID: "org-1", Name: "Test"})
	s.CreateUser(context.Background(), &domain.User{UserID: "u1", OrgID: "org-1", Email: "a@x.com", PasswordHash: "p1"})
	s.CreateUser(context.Background(), &domain.User{UserID: "u2", OrgID: "org-1", Email: "b@x.com", PasswordHash: "p2"})

	count, err = s.CountUsers(context.Background())
	if err != nil {
		t.Fatalf("count users: %v", err)
	}
	if count != 2 {
		t.Errorf("count: got %d, want 2", count)
	}
}

func TestUser_UpdatePassword(t *testing.T) {
	s, _ := openTestStore(t)
	defer s.Close()

	s.CreateOrganization(context.Background(), &domain.Organization{OrgID: "org-1", Name: "Test"})
	s.CreateUser(context.Background(), &domain.User{UserID: "u1", OrgID: "org-1", Email: "a@x.com", PasswordHash: "old-pw"})

	// Update password
	err := s.UpdateUserPassword(context.Background(), "u1", "new-pw")
	if err != nil {
		t.Fatalf("update password: %v", err)
	}

	// Verify old password no longer works
	user, err := s.GetUserByEmail(context.Background(), "a@x.com")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if err := s.CheckPassword(user.PasswordHash, "old-pw"); err == nil {
		t.Error("old password should not match")
	}
	if err := s.CheckPassword(user.PasswordHash, "new-pw"); err != nil {
		t.Errorf("new password should match: %v", err)
	}
}

func TestUser_UpdatePassword_UserNotFound(t *testing.T) {
	s, _ := openTestStore(t)
	defer s.Close()

	err := s.UpdateUserPassword(context.Background(), "nonexistent", "new-pw")
	if !errors.Is(err, metadata.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestUser_GetUserByID(t *testing.T) {
	s, _ := openTestStore(t)
	defer s.Close()

	s.CreateOrganization(context.Background(), &domain.Organization{OrgID: "org-1", Name: "Test"})
	s.CreateUser(context.Background(), &domain.User{UserID: "u1", OrgID: "org-1", Email: "a@x.com", PasswordHash: "p1"})

	user, err := s.GetUserByID(context.Background(), "u1")
	if err != nil {
		t.Fatalf("get user by id: %v", err)
	}
	if user.UserID != "u1" {
		t.Errorf("userID: got %q, want %q", user.UserID, "u1")
	}
	if user.Email != "a@x.com" {
		t.Errorf("email: got %q, want %q", user.Email, "a@x.com")
	}
}

func TestUser_GetUserByID_NotFound(t *testing.T) {
	s, _ := openTestStore(t)
	defer s.Close()

	_, err := s.GetUserByID(context.Background(), "nonexistent")
	if !errors.Is(err, metadata.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}
