package fake_test

import (
	"context"
	"testing"

	"github.com/zbloss/lantern/internal/domain"
	"github.com/zbloss/lantern/internal/fake"
	"golang.org/x/crypto/bcrypt"
)

func TestFakeStore_CountUsers(t *testing.T) {
	store := fake.NewFakeMetadataStore()

	// Initially zero
	count, err := store.CountUsers(context.Background())
	if err != nil {
		t.Fatalf("count users: %v", err)
	}
	if count != 0 {
		t.Errorf("count: got %d, want 0", count)
	}

	store.CreateUser(context.Background(), &domain.User{
		UserID:       "u1",
		OrgID:        "org-1",
		Email:        "a@x.com",
		PasswordHash: "p1",
	})
	store.CreateUser(context.Background(), &domain.User{
		UserID:       "u2",
		OrgID:        "org-1",
		Email:        "b@x.com",
		PasswordHash: "p2",
	})

	count, err = store.CountUsers(context.Background())
	if err != nil {
		t.Fatalf("count users: %v", err)
	}
	if count != 2 {
		t.Errorf("count: got %d, want 2", count)
	}
}

func TestFakeStore_UpdatePassword(t *testing.T) {
	store := fake.NewFakeMetadataStore()

	store.CreateUser(context.Background(), &domain.User{
		UserID:       "u1",
		OrgID:        "org-1",
		Email:        "a@x.com",
		PasswordHash: "old-pw",
	})

	err := store.UpdateUserPassword(context.Background(), "u1", "new-pw")
	if err != nil {
		t.Fatalf("update password: %v", err)
	}

	user, err := store.GetUserByEmail(context.Background(), "a@x.com")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if err := store.CheckPassword(user.PasswordHash, "old-pw"); err == nil {
		t.Error("old password should not match")
	}
	if err := store.CheckPassword(user.PasswordHash, "new-pw"); err != nil {
		t.Errorf("new password should match: %v", err)
	}
}

func TestFakeStore_GetUserByID(t *testing.T) {
	store := fake.NewFakeMetadataStore()

	store.CreateUser(context.Background(), &domain.User{
		UserID:       "u1",
		OrgID:        "org-1",
		Email:        "a@x.com",
		PasswordHash: "p1",
	})

	user, err := store.GetUserByID(context.Background(), "u1")
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

func TestFakeStore_CheckPassword(t *testing.T) {
	store := fake.NewFakeMetadataStore()

	hash, _ := bcrypt.GenerateFromPassword([]byte("correct"), bcrypt.DefaultCost)

	if err := store.CheckPassword(string(hash), "correct"); err != nil {
		t.Error("correct password should match")
	}
	if err := store.CheckPassword(string(hash), "wrong"); err == nil {
		t.Error("wrong password should not match")
	}
}

func TestFakeStore_CreateUser_DuplicateEmail(t *testing.T) {
	store := fake.NewFakeMetadataStore()

	store.CreateUser(context.Background(), &domain.User{
		UserID:       "u1",
		OrgID:        "org-1",
		Email:        "a@x.com",
		PasswordHash: "p1",
	})

	err := store.CreateUser(context.Background(), &domain.User{
		UserID:       "u2",
		OrgID:        "org-1",
		Email:        "a@x.com", // same email
		PasswordHash: "p2",
	})
	if err == nil {
		t.Error("expected error for duplicate email")
	}
}

func TestFakeStore_SessionCounters(t *testing.T) {
	store := fake.NewFakeMetadataStore()

	store.CreateSession(context.Background(), &domain.Session{
		SessionID: "s1",
		UserID:    "u1",
	})
	store.CreateSession(context.Background(), &domain.Session{
		SessionID: "s2",
		UserID:    "u1",
	})

	if store.CreateSessionCalls != 2 {
		t.Errorf("CreateSessionCalls: got %d, want 2", store.CreateSessionCalls)
	}

	store.DeleteSession(context.Background(), "s1")
	store.DeleteSession(context.Background(), "s2")

	if store.DeleteSessionCalls != 2 {
		t.Errorf("DeleteSessionCalls: got %d, want 2", store.DeleteSessionCalls)
	}
}

func TestFakeStore_UpdatePasswordCallsCounter(t *testing.T) {
	store := fake.NewFakeMetadataStore()

	store.CreateUser(context.Background(), &domain.User{
		UserID:       "u1",
		OrgID:        "org-1",
		Email:        "a@x.com",
		PasswordHash: "p1",
	})

	store.UpdateUserPassword(context.Background(), "u1", "new")
	store.UpdateUserPassword(context.Background(), "u1", "newer")

	if store.UpdatePasswordCalls != 2 {
		t.Errorf("UpdatePasswordCalls: got %d, want 2", store.UpdatePasswordCalls)
	}
}

func TestFakeStore_CountUsersCallsCounter(t *testing.T) {
	store := fake.NewFakeMetadataStore()

	store.CountUsers(context.Background())
	store.CountUsers(context.Background())

	if store.CountUsersCalls != 2 {
		t.Errorf("CountUsersCalls: got %d, want 2", store.CountUsersCalls)
	}
}
