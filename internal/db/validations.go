package db

import (
	"context"
	"time"
)

// Hand-written queries for the agent_pages and page_validations tables, which
// are created by a versioned migration and are not part of the sqlc schema.

// MarkAgentPage records that an API token wrote the page. The mark is
// permanent: the page needs validation even after the token is revoked.
func (q *Queries) MarkAgentPage(ctx context.Context, filename string) error {
	_, err := q.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO agent_pages (filename, first_written_at) VALUES (?, ?)`, filename, time.Now())
	return err
}

// IsAgentPage reports whether an API token has written the page.
func (q *Queries) IsAgentPage(ctx context.Context, filename string) (bool, error) {
	var count int
	err := q.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_pages WHERE filename = ?`, filename).Scan(&count)
	return count > 0, err
}

// ListAgentPages returns the filenames of all pages an API token has written.
func (q *Queries) ListAgentPages(ctx context.Context) ([]string, error) {
	rows, err := q.db.QueryContext(ctx, `SELECT filename FROM agent_pages ORDER BY filename`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []string
	for rows.Next() {
		var f string
		if err := rows.Scan(&f); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

// ValidatePage records that a reviewer validated one revision of a page.
func (q *Queries) ValidatePage(ctx context.Context, filename, revision, validatedBy string) error {
	_, err := q.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO page_validations (filename, revision, validated_by, validated_at) VALUES (?, ?, ?, ?)`,
		filename, revision, validatedBy, time.Now())
	return err
}

// PageValidation returns who validated a revision of a page, and when. It
// returns sql.ErrNoRows when that revision is not validated.
func (q *Queries) PageValidation(ctx context.Context, filename, revision string) (string, time.Time, error) {
	var by string
	var at time.Time
	err := q.db.QueryRowContext(ctx,
		`SELECT validated_by, validated_at FROM page_validations WHERE filename = ? AND revision = ?`,
		filename, revision).Scan(&by, &at)
	return by, at, err
}
