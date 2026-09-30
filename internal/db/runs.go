package db

import (
	"context"
	"time"
)

// Hand-written queries for the report_runs table, which is created by a
// versioned migration and is not part of the sqlc schema.

// ReportRun is one execution of a computational page for a period. Runs are
// permanent records: running a period again adds a run and keeps the old one.
type ReportRun struct {
	ID int64
	// Filename is the page's source file. It identifies the page independently
	// of the case used in its URL.
	Filename       string
	Period         string
	SourceHash     string
	SourceRevision string
	RunBy          string
	RunAt          time.Time
	// HTML and Markdown are filled by GetReportRun only.
	HTML     []byte
	Markdown string
}

// CreateReportRun stores a run and returns its ID.
func (q *Queries) CreateReportRun(ctx context.Context, run ReportRun) (int64, error) {
	res, err := q.db.ExecContext(ctx,
		`INSERT INTO report_runs (filename, period, source_hash, source_revision, html, markdown, run_by, run_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		run.Filename, run.Period, run.SourceHash, run.SourceRevision, run.HTML, run.Markdown, run.RunBy, time.Now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// ListReportRuns returns a page's runs, newest first, without their output.
func (q *Queries) ListReportRuns(ctx context.Context, filename string) ([]ReportRun, error) {
	rows, err := q.db.QueryContext(ctx,
		`SELECT id, filename, period, source_hash, source_revision, run_by, run_at
		 FROM report_runs WHERE filename = ? ORDER BY id DESC`, filename)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	runs := []ReportRun{}
	for rows.Next() {
		var r ReportRun
		if err := rows.Scan(&r.ID, &r.Filename, &r.Period, &r.SourceHash, &r.SourceRevision, &r.RunBy, &r.RunAt); err != nil {
			return nil, err
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

// ReportRunMarkdown returns a run's executed markdown without loading its
// HTML. It returns sql.ErrNoRows when the page has no run with that ID.
func (q *Queries) ReportRunMarkdown(ctx context.Context, filename string, id int64) (string, error) {
	var markdown string
	err := q.db.QueryRowContext(ctx,
		`SELECT markdown FROM report_runs WHERE filename = ? AND id = ?`, filename, id).Scan(&markdown)
	return markdown, err
}

// GetReportRun returns one run with its output. It returns sql.ErrNoRows when
// the page has no run with that ID.
func (q *Queries) GetReportRun(ctx context.Context, filename string, id int64) (ReportRun, error) {
	var r ReportRun
	err := q.db.QueryRowContext(ctx,
		`SELECT id, filename, period, source_hash, source_revision, run_by, run_at, html, markdown
		 FROM report_runs WHERE filename = ? AND id = ?`, filename, id).
		Scan(&r.ID, &r.Filename, &r.Period, &r.SourceHash, &r.SourceRevision, &r.RunBy, &r.RunAt, &r.HTML, &r.Markdown)
	return r, err
}
