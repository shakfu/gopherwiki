package db

import (
	"context"
	"time"
)

// Hand-written queries for the code_approvals table, which is created by a
// versioned migration and is not part of the sqlc schema.

// ApproveCode records that a reviewer approved the page source with the given
// hash. Approving an already approved hash keeps the first record.
func (q *Queries) ApproveCode(ctx context.Context, hash, approvedBy string) error {
	_, err := q.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO code_approvals (hash, approved_by, approved_at) VALUES (?, ?, ?)`,
		hash, approvedBy, time.Now())
	return err
}

// IsCodeApproved reports whether the page source with the given hash is approved.
func (q *Queries) IsCodeApproved(ctx context.Context, hash string) (bool, error) {
	var count int
	err := q.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM code_approvals WHERE hash = ?`, hash).Scan(&count)
	return count > 0, err
}
