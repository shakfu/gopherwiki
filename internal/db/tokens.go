package db

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"time"
)

// Hand-written queries for the api_tokens table, which is created by a
// versioned migration and is not part of the sqlc schema.

// APIToken is a bearer credential for the JSON API, bound to a user.
type APIToken struct {
	ID       int64
	UserID   int64
	UserName string
	Label    string
	// WritePrefix is the page path the token may write under.
	WritePrefix string
	CreatedAt   time.Time
	LastUsedAt  sql.NullTime
}

const apiTokenSelect = `SELECT t.id, t.user_id, u.name, t.label, t.write_prefix, t.created_at, t.last_used_at
	FROM api_tokens t JOIN user u ON u.id = t.user_id`

// hashAPIToken returns the stored form of a token. Tokens are 32 random bytes,
// so an unsalted SHA-256 is sufficient.
func hashAPIToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// CreateAPIToken stores a new token and returns its plaintext. Only the hash
// is kept, so the plaintext cannot be recovered later.
func (q *Queries) CreateAPIToken(ctx context.Context, userID int64, label, writePrefix string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := "gw_" + base64.RawURLEncoding.EncodeToString(b)
	_, err := q.db.ExecContext(ctx,
		`INSERT INTO api_tokens (user_id, label, token_hash, write_prefix, created_at) VALUES (?, ?, ?, ?, ?)`,
		userID, label, hashAPIToken(token), writePrefix, time.Now())
	if err != nil {
		return "", err
	}
	return token, nil
}

// GetAPIToken looks a token up by its plaintext. It returns sql.ErrNoRows when
// the token is unknown.
func (q *Queries) GetAPIToken(ctx context.Context, token string) (APIToken, error) {
	var t APIToken
	err := q.db.QueryRowContext(ctx, apiTokenSelect+` WHERE t.token_hash = ?`, hashAPIToken(token)).
		Scan(&t.ID, &t.UserID, &t.UserName, &t.Label, &t.WritePrefix, &t.CreatedAt, &t.LastUsedAt)
	return t, err
}

// ListAPITokens returns all tokens, oldest first.
func (q *Queries) ListAPITokens(ctx context.Context) ([]APIToken, error) {
	rows, err := q.db.QueryContext(ctx, apiTokenSelect+` ORDER BY t.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tokens := []APIToken{}
	for rows.Next() {
		var t APIToken
		if err := rows.Scan(&t.ID, &t.UserID, &t.UserName, &t.Label, &t.WritePrefix, &t.CreatedAt, &t.LastUsedAt); err != nil {
			return nil, err
		}
		tokens = append(tokens, t)
	}
	return tokens, rows.Err()
}

// TouchAPIToken records that a token was just used.
func (q *Queries) TouchAPIToken(ctx context.Context, id int64) error {
	_, err := q.db.ExecContext(ctx, `UPDATE api_tokens SET last_used_at = ? WHERE id = ?`, time.Now(), id)
	return err
}

// DeleteAPIToken revokes a token.
func (q *Queries) DeleteAPIToken(ctx context.Context, id int64) error {
	_, err := q.db.ExecContext(ctx, `DELETE FROM api_tokens WHERE id = ?`, id)
	return err
}
