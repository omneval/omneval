package metadata

import (
	"context"

	"github.com/omneval/omneval/internal/domain"
)

// BookmarkStore is the domain interface for bookmark (starred trace) operations.
type BookmarkStore interface {
	SetBookmark(ctx context.Context, b *domain.Bookmark) error
	RemoveBookmark(ctx context.Context, projectID, traceID string) error
	IsBookmarked(ctx context.Context, projectID, traceID string) (bool, error)
	ListBookmarkedTraceIDs(ctx context.Context, projectID string) ([]string, error)
	RemoveBookmarksForProject(ctx context.Context, projectID string) error
}