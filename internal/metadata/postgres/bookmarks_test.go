package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/omneval/omneval/internal/domain"
)

func TestBookmarks_CRUD(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(ctx, t)

	// Initially not bookmarked.
	got, err := s.IsBookmarked(ctx, "p1", "t1")
	if err != nil {
		t.Fatalf("IsBookmarked: %v", err)
	}
	if got {
		t.Error("fresh store: expected not bookmarked")
	}

	// Set, idempotently.
	b := &domain.Bookmark{ProjectID: "p1", TraceID: "t1", CreatedAt: time.Now()}
	if err := s.SetBookmark(ctx, b); err != nil {
		t.Fatalf("SetBookmark: %v", err)
	}
	if err := s.SetBookmark(ctx, b); err != nil {
		t.Fatalf("SetBookmark twice: %v", err)
	}

	got, err = s.IsBookmarked(ctx, "p1", "t1")
	if err != nil {
		t.Fatalf("IsBookmarked: %v", err)
	}
	if !got {
		t.Error("expected bookmarked after SetBookmark")
	}

	// Project isolation.
	got, err = s.IsBookmarked(ctx, "p2", "t1")
	if err != nil {
		t.Fatalf("IsBookmarked other project: %v", err)
	}
	if got {
		t.Error("bookmark leaked across projects")
	}

	// List.
	if err := s.SetBookmark(ctx, &domain.Bookmark{ProjectID: "p1", TraceID: "t2"}); err != nil {
		t.Fatalf("SetBookmark t2: %v", err)
	}
	ids, err := s.ListBookmarkedTraceIDs(ctx, "p1")
	if err != nil {
		t.Fatalf("ListBookmarkedTraceIDs: %v", err)
	}
	if len(ids) != 2 {
		t.Errorf("list: got %v, want 2 ids", ids)
	}

	// Remove (idempotent).
	if err := s.RemoveBookmark(ctx, "p1", "t1"); err != nil {
		t.Fatalf("RemoveBookmark: %v", err)
	}
	if err := s.RemoveBookmark(ctx, "p1", "t1"); err != nil {
		t.Fatalf("RemoveBookmark twice: %v", err)
	}
	got, err = s.IsBookmarked(ctx, "p1", "t1")
	if err != nil {
		t.Fatalf("IsBookmarked after remove: %v", err)
	}
	if got {
		t.Error("expected not bookmarked after RemoveBookmark")
	}
}
