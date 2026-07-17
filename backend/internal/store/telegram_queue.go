package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type TelegramUpdateRecord struct {
	UpdateID int64
	Payload  []byte
	Attempts int
}

func (db *DB) LoadTelegramOffset(ctx context.Context, streamKey string) (int64, error) {
	var offset int64
	err := db.Pool.QueryRow(ctx, `SELECT next_offset FROM telegram_poll_state WHERE stream_key=$1`, streamKey).Scan(&offset)
	if err == pgx.ErrNoRows {
		return 0, nil
	}
	return offset, err
}

// PersistTelegramUpdates stores every update before advancing the durable poll
// offset. Telegram is only acknowledged on the next getUpdates call, after this
// transaction has committed.
func (db *DB) PersistTelegramUpdates(ctx context.Context, streamKey string, updates []TelegramUpdateRecord) (int64, error) {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	// Reserve and lock the offset row on this same transaction/connection. Using
	// db.Pool here would self-deadlock with MaxConns=1 and would separate the
	// acknowledgement offset from the queue insert transaction.
	if _, err := tx.Exec(ctx, `
		INSERT INTO telegram_poll_state (stream_key,next_offset,updated_at)
		VALUES ($1,0,now()) ON CONFLICT (stream_key) DO NOTHING`, streamKey); err != nil {
		return 0, err
	}
	var nextOffset int64
	if err := tx.QueryRow(ctx, `
		SELECT next_offset FROM telegram_poll_state WHERE stream_key=$1 FOR UPDATE`, streamKey).Scan(&nextOffset); err != nil {
		return 0, err
	}
	for _, update := range updates {
		if update.UpdateID < 0 || len(update.Payload) == 0 {
			continue
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO telegram_update_queue (update_id,payload,status,available_at,created_at,updated_at)
			VALUES ($1,$2,'pending',now(),now(),now()) ON CONFLICT (update_id) DO NOTHING`,
			update.UpdateID, update.Payload); err != nil {
			return 0, err
		}
		if update.UpdateID+1 > nextOffset {
			nextOffset = update.UpdateID + 1
		}
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO telegram_poll_state (stream_key,next_offset,updated_at)
		VALUES ($1,$2,now())
		ON CONFLICT (stream_key) DO UPDATE SET
			next_offset=GREATEST(telegram_poll_state.next_offset,EXCLUDED.next_offset),updated_at=now()`,
		streamKey, nextOffset); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return nextOffset, nil
}

func (db *DB) ClaimTelegramUpdate(ctx context.Context) (*TelegramUpdateRecord, error) {
	var update TelegramUpdateRecord
	err := db.Pool.QueryRow(ctx, `
		WITH head AS (
			SELECT update_id,status,available_at,updated_at
			FROM telegram_update_queue
			WHERE status IN ('pending','processing')
			ORDER BY update_id
			FOR UPDATE LIMIT 1
		), candidate AS (
			SELECT update_id FROM head
			WHERE (status='pending' AND available_at <= now())
			   OR (status='processing' AND updated_at < now() - interval '10 minutes')
		)
		UPDATE telegram_update_queue q
		SET status='processing',attempts=attempts+1,updated_at=now()
		FROM candidate c WHERE q.update_id=c.update_id
		RETURNING q.update_id,q.payload,q.attempts`).Scan(&update.UpdateID, &update.Payload, &update.Attempts)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &update, nil
}

func (db *DB) CompleteTelegramUpdate(ctx context.Context, updateID int64) error {
	tag, err := db.Pool.Exec(ctx, `
		UPDATE telegram_update_queue SET status='done',last_error=NULL,processed_at=now(),updated_at=now()
		WHERE update_id=$1 AND status='processing'`, updateID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return pgx.ErrNoRows
	}
	return nil
}

func (db *DB) RetryTelegramUpdate(ctx context.Context, updateID int64, attempts int, processingErr error) error {
	message := "telegram update verwerking mislukt"
	if processingErr != nil {
		message = strings.TrimSpace(processingErr.Error())
	}
	if len(message) > 500 {
		message = message[:500]
	}
	if attempts >= 5 {
		tag, err := db.Pool.Exec(ctx, `
			UPDATE telegram_update_queue
			SET status='failed',last_error=$2,processed_at=now(),updated_at=now()
			WHERE update_id=$1 AND status='processing'`, updateID, message)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return pgx.ErrNoRows
		}
		return nil
	}
	backoff := time.Duration(attempts*attempts) * time.Second
	tag, err := db.Pool.Exec(ctx, `
		UPDATE telegram_update_queue
		SET status='pending',last_error=$2,available_at=now()+$3::interval,updated_at=now()
		WHERE update_id=$1 AND status='processing'`, updateID, message, fmt.Sprintf("%f seconds", backoff.Seconds()))
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return pgx.ErrNoRows
	}
	return nil
}

// PruneTelegramUpdates bounds the durable inbox while retaining recent terminal
// rows for diagnostics. Pending/processing work is never deleted.
func (db *DB) PruneTelegramUpdates(ctx context.Context, before time.Time) (int64, error) {
	tag, err := db.Pool.Exec(ctx, `
		DELETE FROM telegram_update_queue
		WHERE status IN ('done','failed') AND processed_at < $1`, before.UTC())
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
