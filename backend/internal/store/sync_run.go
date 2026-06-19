package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// SyncRun is one execution of a background sync job (gmail, schedule, personal,
// pending-calendar). Recorded best-effort so a history of outcomes is visible
// instead of only the latest-snapshot freshness shown by /sync/status.
type SyncRun struct {
	ID         int64     `json:"id"`
	Source     string    `json:"source"`
	StartedAt  time.Time `json:"startedAt"`
	DurationMs int       `json:"durationMs"`
	OK         bool      `json:"ok"`
	Error      string    `json:"error,omitempty"`
}

type SyncRunStore struct{ db *DB }

func NewSyncRunStore(db *DB) *SyncRunStore { return &SyncRunStore{db: db} }

const syncRunErrMax = 500

// Record inserts one sync-run row. Best-effort: callers log on error.
func (s *SyncRunStore) Record(ctx context.Context, run SyncRun) error {
	if r := []rune(run.Error); len(r) > syncRunErrMax {
		run.Error = string(r[:syncRunErrMax])
	}
	if run.StartedAt.IsZero() {
		run.StartedAt = time.Now()
	}
	_, err := s.db.Pool.Exec(ctx,
		`INSERT INTO sync_runs (source, started_at, duration_ms, ok, error)
		 VALUES ($1, $2, $3, $4, NULLIF($5, ''))`,
		run.Source, run.StartedAt, run.DurationMs, run.OK, run.Error)
	return err
}

// Recent returns the most recent sync runs across all sources, newest first.
func (s *SyncRunStore) Recent(ctx context.Context, limit int) ([]SyncRun, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, source, started_at, duration_ms, ok, COALESCE(error, '')
		   FROM sync_runs
		  ORDER BY started_at DESC
		  LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (SyncRun, error) {
		var r SyncRun
		err := row.Scan(&r.ID, &r.Source, &r.StartedAt, &r.DurationMs, &r.OK, &r.Error)
		return r, err
	})
}

// DeleteOlderThan prunes sync-run history so the table doesn't grow unbounded.
func (s *SyncRunStore) DeleteOlderThan(ctx context.Context, olderThan time.Duration) (int, error) {
	cutoff := time.Now().UTC().Add(-olderThan)
	tag, err := s.db.Pool.Exec(ctx,
		`DELETE FROM sync_runs WHERE started_at < $1`, cutoff)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}
