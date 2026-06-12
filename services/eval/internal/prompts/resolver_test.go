package prompts

import (
	"context"
	"fmt"
	"testing"

	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/metadata"
)

// fakeStore implements just the Prompt Registry reads used by the resolver.
type fakeStore struct {
	metadata.Store // panics if any other method is called

	versionCalls int
	labelCalls   int
	template     string
	err          error
}

func (f *fakeStore) GetPromptVersion(_ context.Context, projectID, name string, version int64) (*domain.PromptVersion, error) {
	f.versionCalls++
	if f.err != nil {
		return nil, f.err
	}
	return &domain.PromptVersion{ProjectID: projectID, Name: name, Version: version, Template: f.template}, nil
}

func (f *fakeStore) GetPromptByLabel(_ context.Context, projectID, name, label string) (*domain.PromptVersion, error) {
	f.labelCalls++
	if f.err != nil {
		return nil, f.err
	}
	return &domain.PromptVersion{ProjectID: projectID, Name: name, Version: 7, Template: f.template + " (" + label + ")"}, nil
}

func TestResolve_ExactVersion(t *testing.T) {
	store := &fakeStore{template: "judge it"}
	r := NewCachingResolver(store)

	got, err := r.Resolve(context.Background(), "proj-1", "judge", 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "judge it" {
		t.Errorf("template: got %q", got)
	}
	if store.versionCalls != 1 || store.labelCalls != 0 {
		t.Errorf("expected one version fetch, got version=%d label=%d", store.versionCalls, store.labelCalls)
	}
}

func TestResolve_ProductionLabelWhenUnpinned(t *testing.T) {
	store := &fakeStore{template: "judge it"}
	r := NewCachingResolver(store)

	got, err := r.Resolve(context.Background(), "proj-1", "judge", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "judge it (production)" {
		t.Errorf("template: got %q", got)
	}
	if store.labelCalls != 1 || store.versionCalls != 0 {
		t.Errorf("expected one label fetch, got version=%d label=%d", store.versionCalls, store.labelCalls)
	}
}

func TestResolve_CachesWithinTTL(t *testing.T) {
	store := &fakeStore{template: "judge it"}
	r := NewCachingResolver(store)

	for i := 0; i < 3; i++ {
		if _, err := r.Resolve(context.Background(), "proj-1", "judge", 3); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if store.versionCalls != 1 {
		t.Errorf("expected a single store fetch within the TTL, got %d", store.versionCalls)
	}
}

func TestResolve_FetchError(t *testing.T) {
	store := &fakeStore{err: fmt.Errorf("db down")}
	r := NewCachingResolver(store)

	if _, err := r.Resolve(context.Background(), "proj-1", "judge", 3); err == nil {
		t.Fatal("expected error when the store fetch fails")
	}
}
