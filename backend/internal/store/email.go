package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
)

type EmailStore struct{ db *DB }

func NewEmailStore(db *DB) *EmailStore { return &EmailStore{db: db} }

const emailColumns = `id, user_id, gmail_id, thread_id, from_addr, to_addr, cc, bcc,
	subject, snippet, datum::text, ontvangen, is_gelezen, is_ster, is_verwijderd, is_draft,
	label_ids, categorie, heeft_bijlagen, bijlagen_count, search_text, synced_at, created_at`

// List returns emails for a user, ordered by most recent first.
func (s *EmailStore) List(ctx context.Context, userID string, limit, offset int) ([]model.Email, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Pool.Query(ctx,
		`SELECT `+emailColumns+` FROM emails
		  WHERE user_id = $1 AND is_verwijderd = false
		  ORDER BY ontvangen DESC
		  LIMIT $2 OFFSET $3`, userID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, scanEmail)
}

// ListByCategorie returns emails filtered by category.
func (s *EmailStore) ListByCategorie(ctx context.Context, userID, categorie string, limit, offset int) ([]model.Email, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Pool.Query(ctx,
		`SELECT `+emailColumns+` FROM emails
		  WHERE user_id = $1 AND categorie = $2 AND is_verwijderd = false
		  ORDER BY ontvangen DESC
		  LIMIT $3 OFFSET $4`, userID, categorie, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, scanEmail)
}

// Search performs full-text search on emails.
func (s *EmailStore) Search(ctx context.Context, userID, query string, limit int) ([]model.Email, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.Pool.Query(ctx,
		`SELECT `+emailColumns+` FROM emails
		  WHERE user_id = $1 AND is_verwijderd = false
		    AND to_tsvector('dutch', search_text) @@ plainto_tsquery('dutch', $2)
		  ORDER BY ontvangen DESC
		  LIMIT $3`, userID, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, scanEmail)
}

// GetByGmailID returns a single email by Gmail ID.
func (s *EmailStore) GetByGmailID(ctx context.Context, userID, gmailID string) (*model.Email, error) {
	var e model.Email
	err := s.db.Pool.QueryRow(ctx,
		`SELECT `+emailColumns+` FROM emails WHERE user_id = $1 AND gmail_id = $2`,
		userID, gmailID,
	).Scan(&e.ID, &e.UserID, &e.GmailID, &e.ThreadID, &e.FromAddr, &e.ToAddr, &e.CC, &e.BCC,
		&e.Subject, &e.Snippet, &e.Datum, &e.Ontvangen, &e.IsGelezen, &e.IsSter, &e.IsVerwijderd, &e.IsDraft,
		&e.LabelIDs, &e.Categorie, &e.HeeftBijlagen, &e.BijlagenCount, &e.SearchText, &e.SyncedAt, &e.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// BulkUpsert inserts or updates emails using ON CONFLICT.
func (s *EmailStore) BulkUpsert(ctx context.Context, emails []model.Email) (int, error) {
	if len(emails) == 0 {
		return 0, nil
	}

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var count int
	for _, e := range emails {
		if e.ID == uuid.Nil {
			e.ID = uuid.New()
		}
		tag, err := tx.Exec(ctx,
			`INSERT INTO emails (id, user_id, gmail_id, thread_id, from_addr, to_addr, cc, bcc,
			    subject, snippet, datum, ontvangen, is_gelezen, is_ster, is_verwijderd, is_draft,
			    label_ids, categorie, heeft_bijlagen, bijlagen_count, search_text, synced_at)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22)
			 ON CONFLICT (user_id, gmail_id) DO UPDATE SET
			    thread_id=EXCLUDED.thread_id, from_addr=EXCLUDED.from_addr, to_addr=EXCLUDED.to_addr,
			    cc=EXCLUDED.cc, bcc=EXCLUDED.bcc, subject=EXCLUDED.subject, snippet=EXCLUDED.snippet,
			    datum=EXCLUDED.datum, ontvangen=EXCLUDED.ontvangen,
			    is_gelezen=EXCLUDED.is_gelezen, is_ster=EXCLUDED.is_ster,
			    is_verwijderd=EXCLUDED.is_verwijderd, is_draft=EXCLUDED.is_draft,
			    label_ids=EXCLUDED.label_ids, categorie=EXCLUDED.categorie,
			    heeft_bijlagen=EXCLUDED.heeft_bijlagen, bijlagen_count=EXCLUDED.bijlagen_count,
			    search_text=EXCLUDED.search_text, synced_at=EXCLUDED.synced_at`,
			e.ID, e.UserID, e.GmailID, e.ThreadID, e.FromAddr, e.ToAddr, e.CC, e.BCC,
			e.Subject, e.Snippet, e.Datum, e.Ontvangen, e.IsGelezen, e.IsSter, e.IsVerwijderd, e.IsDraft,
			e.LabelIDs, e.Categorie, e.HeeftBijlagen, e.BijlagenCount, e.SearchText, e.SyncedAt,
		)
		if err != nil {
			return 0, err
		}
		count += int(tag.RowsAffected())
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return count, nil
}

// PurgeDeleted removes emails marked as deleted older than the given duration.
func (s *EmailStore) PurgeDeleted(ctx context.Context, userID string, olderThan time.Duration) (int, error) {
	cutoff := time.Now().Add(-olderThan)
	tag, err := s.db.Pool.Exec(ctx,
		`DELETE FROM emails WHERE user_id = $1 AND is_verwijderd = true AND synced_at < $2`,
		userID, cutoff)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

// MarkRead updates the read status of an email.
func (s *EmailStore) MarkRead(ctx context.Context, userID, gmailID string, read bool) error {
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE emails SET is_gelezen = $3 WHERE user_id = $1 AND gmail_id = $2`,
		userID, gmailID, read)
	return err
}

// MarkDeleted soft-deletes an email.
func (s *EmailStore) MarkDeleted(ctx context.Context, userID, gmailID string) error {
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE emails SET is_verwijderd = true WHERE user_id = $1 AND gmail_id = $2`,
		userID, gmailID)
	return err
}

// MarkStar updates the starred status of an email.
func (s *EmailStore) MarkStar(ctx context.Context, userID, gmailID string, starred bool) error {
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE emails SET is_ster = $3 WHERE user_id = $1 AND gmail_id = $2`,
		userID, gmailID, starred)
	return err
}

// Count returns total email count for a user (excluding deleted).
func (s *EmailStore) Count(ctx context.Context, userID string) (int, error) {
	var count int
	err := s.db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM emails WHERE user_id = $1 AND is_verwijderd = false`, userID,
	).Scan(&count)
	return count, err
}

// CountUnread returns unread email count for a user.
func (s *EmailStore) CountUnread(ctx context.Context, userID string) (int, error) {
	var count int
	err := s.db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM emails WHERE user_id = $1 AND is_verwijderd = false AND is_gelezen = false`, userID,
	).Scan(&count)
	return count, err
}

// ─── EmailSyncMeta ───────────────────────────────────────────────────────────

// GetSyncMeta returns the sync metadata for a user.
func (s *EmailStore) GetSyncMeta(ctx context.Context, userID string) (*model.EmailSyncMeta, error) {
	var m model.EmailSyncMeta
	err := s.db.Pool.QueryRow(ctx,
		`SELECT id, user_id, history_id, last_full_sync, total_synced, updated_at,
		        COALESCE(sync_status, 'ok'), COALESCE(last_error, ''), last_attempt_at
		   FROM email_sync_meta WHERE user_id = $1`, userID,
	).Scan(&m.ID, &m.UserID, &m.HistoryID, &m.LastFullSync, &m.TotalSynced, &m.UpdatedAt,
		&m.SyncStatus, &m.LastError, &m.LastAttemptAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// UpsertSyncMeta creates or updates the sync metadata after a SUCCESSFUL sync.
// It clears any prior failure state.
func (s *EmailStore) UpsertSyncMeta(ctx context.Context, userID, historyID string, lastFullSync *time.Time, totalSynced int) error {
	_, err := s.db.Pool.Exec(ctx,
		`INSERT INTO email_sync_meta (id, user_id, history_id, last_full_sync, total_synced, updated_at, sync_status, last_error, last_attempt_at)
		 VALUES ($1, $2, $3, $4, $5, NOW(), 'ok', NULL, NOW())
		 ON CONFLICT (user_id) DO UPDATE SET
		    history_id = EXCLUDED.history_id,
		    last_full_sync = EXCLUDED.last_full_sync,
		    total_synced = EXCLUDED.total_synced,
		    updated_at = NOW(),
		    sync_status = 'ok',
		    last_error = NULL,
		    last_attempt_at = NOW()`,
		uuid.New(), userID, historyID, lastFullSync, totalSynced,
	)
	return err
}

// MarkSyncFailed records a failed sync attempt WITHOUT touching the
// last-success fields (history_id, total_synced, updated_at), so current health
// can be distinguished from the last successful snapshot.
func (s *EmailStore) MarkSyncFailed(ctx context.Context, userID, errMsg string) error {
	if len(errMsg) > 500 {
		errMsg = errMsg[:500]
	}
	_, err := s.db.Pool.Exec(ctx,
		`INSERT INTO email_sync_meta (id, user_id, history_id, total_synced, updated_at, sync_status, last_error, last_attempt_at)
		 VALUES ($1, $2, '', 0, NOW(), 'failed', $3, NOW())
		 ON CONFLICT (user_id) DO UPDATE SET
		    sync_status = 'failed',
		    last_error = EXCLUDED.last_error,
		    last_attempt_at = NOW()`,
		uuid.New(), userID, errMsg,
	)
	return err
}

func scanEmail(row pgx.CollectableRow) (model.Email, error) {
	var e model.Email
	err := row.Scan(&e.ID, &e.UserID, &e.GmailID, &e.ThreadID, &e.FromAddr, &e.ToAddr, &e.CC, &e.BCC,
		&e.Subject, &e.Snippet, &e.Datum, &e.Ontvangen, &e.IsGelezen, &e.IsSter, &e.IsVerwijderd, &e.IsDraft,
		&e.LabelIDs, &e.Categorie, &e.HeeftBijlagen, &e.BijlagenCount, &e.SearchText, &e.SyncedAt, &e.CreatedAt)
	return e, err
}
