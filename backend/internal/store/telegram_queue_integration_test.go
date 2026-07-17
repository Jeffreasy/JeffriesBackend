package store

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPersistTelegramUpdatesUsesSingleConnectionTransaction(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	cfg.MinConns = 0
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	db := &DB{Pool: pool}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := EnsureRuntimeSchema(ctx, db); err != nil {
		t.Fatalf("ensure runtime schema: %v", err)
	}

	streamKey := "test-single-connection-telegram"
	updateID := time.Now().UnixNano()
	payload, _ := json.Marshal(map[string]any{"update_id": updateID})
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM telegram_update_queue WHERE update_id=$1`, updateID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM telegram_poll_state WHERE stream_key=$1`, streamKey)
	})

	next, err := db.PersistTelegramUpdates(ctx, streamKey, []TelegramUpdateRecord{{UpdateID: updateID, Payload: payload}})
	if err != nil {
		t.Fatalf("persist with MaxConns=1 must not self-deadlock: %v", err)
	}
	if next != updateID+1 {
		t.Fatalf("next offset = %d, want %d", next, updateID+1)
	}
}
