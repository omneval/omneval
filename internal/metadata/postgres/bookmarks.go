package postgres

import (
	"context"
	"fmt"
)

// SetBookmark stars a trace for the given project. Idempotent.
func (s *Store) SetBookmark(ctx context.Context, projectID, traceID string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO bookmarks (trace_id, project_id)
		VALUES ($1, $2)
		ON CONFLICT (trace_id, project_id) DO NOTHING
	`, traceID, projectID)
	if err != nil {
		return fmt.Errorf("postgres: set bookmark: %w", err)
	}
	return nil
}

// RemoveBookmark unstars a trace for the given project. Idempotent.
func (s *Store) RemoveBookmark(ctx context.Context, projectID, traceID string) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM bookmarks WHERE trace_id = $1 AND project_id = $2
	`, traceID, projectID)
	if err != nil {
		return fmt.Errorf("postgres: remove bookmark: %w", err)
	}
	return nil
}

// IsBookmarked reports whether a trace is starred for the given project.
func (s *Store) IsBookmarked(ctx context.Context, projectID, traceID string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM bookmarks WHERE trace_id = $1 AND project_id = $2
	`, traceID, projectID).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("postgres: is bookmarked: %w", err)
	}
	return count > 0, nil
}

// ListBookmarkedTraces returns the trace IDs starred for the given project.
func (s *Store) ListBookmarkedTraces(ctx context.Context, projectID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT trace_id FROM bookmarks WHERE project_id = $1 ORDER BY created_at DESC
	`, projectID)
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
