package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/omneval/omneval/internal/domain"
)

// ---- Bookmarks ----

// SetBookmark stars a trace. Idempotent: re-bookmarking an already
// bookmarked trace keeps the original created_at.
func (s *Store) SetBookmark(ctx context.Context, b *domain.Bookmark) error {
	createdAt := b.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO bookmarks (project_id, trace_id, created_at)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (project_id, trace_id) DO NOTHING`,
		b.ProjectID, b.TraceID, createdAt,
	)
	if err != nil {
		return fmt.Errorf("postgres: set bookmark: %w", err)
	}
	return nil
}

// RemoveBookmark unstars a trace. Removing a non-existent bookmark is a no-op.
func (s *Store) RemoveBookmark(ctx context.Context, projectID, traceID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM bookmarks WHERE project_id = $1 AND trace_id = $2`,
		projectID, traceID,
	)
	if err != nil {
		return fmt.Errorf("postgres: remove bookmark: %w", err)
	}
	return nil
}

// IsBookmarked reports whether the trace is starred in the project.
func (s *Store) IsBookmarked(ctx context.Context, projectID, traceID string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM bookmarks WHERE project_id = $1 AND trace_id = $2`,
		projectID, traceID,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("postgres: is bookmarked: %w", err)
	}
	return count > 0, nil
}

// RemoveBookmarksForProject deletes every bookmark in the project.
func (s *Store) RemoveBookmarksForProject(ctx context.Context, projectID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM bookmarks WHERE project_id = $1`,
		projectID,
	)
	if err != nil {
		return fmt.Errorf("postgres: remove bookmarks for project: %w", err)
	}
	return nil
}

// ListBookmarkedTraceIDs returns every starred trace ID in the project.
func (s *Store) ListBookmarkedTraceIDs(ctx context.Context, projectID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT trace_id FROM bookmarks WHERE project_id = $1 ORDER BY created_at DESC`,
		projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres: list bookmarks: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("postgres: scan bookmark: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
