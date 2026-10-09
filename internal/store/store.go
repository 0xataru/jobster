// Package store persists seen jobs in SQLite for deduplication across runs.
//
// A job is identified by job.Key (company + title), and additionally by URL so
// a retitled posting is not reported twice. A job is reported once: it stays
// pending until MarkNotified, so a failed notification is retried next run.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/0xataru/jobster/internal/job"

	_ "modernc.org/sqlite" // pure-Go driver keeps the binary CGO-free
)

const schema = `
CREATE TABLE IF NOT EXISTS jobs (
	key         TEXT PRIMARY KEY,
	url         TEXT NOT NULL,
	source      TEXT NOT NULL,
	title       TEXT NOT NULL,
	company     TEXT NOT NULL,
	location    TEXT NOT NULL,
	posted_at   INTEGER NOT NULL, -- unix seconds, 0 when unknown
	salary_min  INTEGER NOT NULL,
	salary_max  INTEGER NOT NULL,
	currency    TEXT NOT NULL,
	score       INTEGER NOT NULL,
	reasons     TEXT NOT NULL,    -- newline-separated
	first_seen  INTEGER NOT NULL,
	last_seen   INTEGER NOT NULL,
	notified_at INTEGER           -- NULL until reported
);
CREATE INDEX IF NOT EXISTS jobs_url ON jobs(url);
CREATE INDEX IF NOT EXISTS jobs_pending ON jobs(notified_at, last_seen);
`

// Store is a SQLite-backed job history.
type Store struct {
	db *sql.DB
}

// Open opens or creates the database at path.
func Open(ctx context.Context, path string) (*Store, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}
	return &Store{db: db}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Prune forgets jobs not seen since before cutoff, so a role re-posted much
// later is reported again.
func (s *Store) Prune(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM jobs WHERE last_seen < ?`, cutoff.Unix())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// Upsert records jobs as seen at now and returns how many were new. Known
// jobs get their score and details refreshed but keep their notification
// state.
func (s *Store) Upsert(ctx context.Context, jobs []job.Scored, now time.Time) (added int, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	for _, j := range jobs {
		key, err := existingKey(ctx, tx, j)
		if err != nil {
			return 0, err
		}
		if key == "" {
			key = j.Key()
			added++
			_, err = tx.ExecContext(ctx, `
				INSERT INTO jobs (key, url, source, title, company, location, posted_at,
					salary_min, salary_max, currency, score, reasons, first_seen, last_seen)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				key, j.URL, j.Source, j.Title, j.Company, j.Location, unix(j.PostedAt),
				j.SalaryMin, j.SalaryMax, j.Currency, j.Score, strings.Join(j.Reasons, "\n"),
				now.Unix(), now.Unix())
		} else {
			_, err = tx.ExecContext(ctx, `
				UPDATE jobs SET url = ?, source = ?, title = ?, company = ?, location = ?,
					posted_at = ?, salary_min = ?, salary_max = ?, currency = ?, score = ?,
					reasons = ?, last_seen = ?
				WHERE key = ?`,
				j.URL, j.Source, j.Title, j.Company, j.Location, unix(j.PostedAt),
				j.SalaryMin, j.SalaryMax, j.Currency, j.Score, strings.Join(j.Reasons, "\n"),
				now.Unix(), key)
		}
		if err != nil {
			return 0, fmt.Errorf("upsert %q: %w", j.URL, err)
		}
	}
	return added, tx.Commit()
}

// existingKey returns the key of the stored row matching j by key or URL, or
// "" if j is new.
func existingKey(ctx context.Context, tx *sql.Tx, j job.Scored) (string, error) {
	var key string
	err := tx.QueryRowContext(ctx,
		`SELECT key FROM jobs WHERE key = ? OR url = ? ORDER BY key = ? DESC LIMIT 1`,
		j.Key(), j.URL, j.Key()).Scan(&key)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return key, err
}

// Pending returns jobs seen at or after since that scored at least minScore
// and have not been notified, best first. Descriptions are not stored, so
// they come back empty.
func (s *Store) Pending(ctx context.Context, since time.Time, minScore, limit int) ([]job.Scored, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT url, source, title, company, location, posted_at, salary_min, salary_max,
			currency, score, reasons
		FROM jobs
		WHERE notified_at IS NULL AND last_seen >= ? AND score >= ?
		ORDER BY score DESC, posted_at DESC
		LIMIT ?`, since.Unix(), minScore, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []job.Scored
	for rows.Next() {
		var j job.Scored
		var posted int64
		var reasons string
		if err := rows.Scan(&j.URL, &j.Source, &j.Title, &j.Company, &j.Location, &posted,
			&j.SalaryMin, &j.SalaryMax, &j.Currency, &j.Score, &reasons); err != nil {
			return nil, err
		}
		if posted > 0 {
			j.PostedAt = time.Unix(posted, 0)
		}
		if reasons != "" {
			j.Reasons = strings.Split(reasons, "\n")
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// MarkNotified records that jobs were reported at now.
func (s *Store) MarkNotified(ctx context.Context, jobs []job.Scored, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, j := range jobs {
		if _, err := tx.ExecContext(ctx,
			`UPDATE jobs SET notified_at = ? WHERE key = ? OR url = ?`,
			now.Unix(), j.Key(), j.URL); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func unix(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}
