package engine

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jeffreasy/JeffriesBackend/internal/store"
)

// RunCleaner runs the joined maintenance worker. It blocks until ctx is
// cancelled, so main can wait for it before closing the database pool.
func RunCleaner(ctx context.Context, db *store.DB) {
	if ctx.Err() != nil {
		return
	}
	slog.Info("starting background cleaner service", "cleanupInterval", "24h", "pendingInterval", "5m")
	cleanupTicker := time.NewTicker(24 * time.Hour)
	pendingTicker := time.NewTicker(5 * time.Minute)
	defer cleanupTicker.Stop()
	defer pendingTicker.Stop()

	performPendingMaintenance(ctx, db)
	performCleanup(ctx, db)
	for {
		select {
		case <-ctx.Done():
			slog.Info("background cleaner stopped")
			return
		case <-pendingTicker.C:
			performPendingMaintenance(ctx, db)
		case <-cleanupTicker.C:
			performCleanup(ctx, db)
		}
	}
}

func performPendingMaintenance(ctx context.Context, db *store.DB) {
	dbCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	pending := store.NewPendingStore(db.Pool)
	if expired, err := pending.ExpireOld(dbCtx); err != nil {
		slog.Warn("failed to expire pending actions", "error", err)
	} else if expired > 0 {
		slog.Info("expired pending actions", "count", expired)
	}
	if stale, err := pending.MarkStaleExecutingUnknown(dbCtx, 15*time.Minute); err != nil {
		slog.Warn("failed to mark interrupted pending actions", "error", err)
	} else if stale > 0 {
		slog.Warn("marked interrupted pending actions unknown", "count", stale)
	}
}

func performCleanup(ctx context.Context, db *store.DB) {
	slog.Info("running routine cleanup")

	// 1. Clean old audit logs (> 30 days)
	// We use a timeout to prevent hanging the cleanup
	dbCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	rows, err := db.CleanOldAuditLogs(dbCtx, 30)
	if err != nil {
		slog.Error("failed to clean old audit logs", "error", err)
	} else if rows > 0 {
		slog.Info("cleaned old audit logs", "deleted_rows", rows)
	}

	if _, err := db.Pool.Exec(dbCtx, `DELETE FROM cron_claim WHERE claimed_at < now() - interval '90 days'`); err != nil {
		slog.Warn("failed to prune cron claims", "error", err)
	}
	if deleted, err := db.PruneTelegramUpdates(dbCtx, time.Now().Add(-30*24*time.Hour)); err != nil {
		slog.Warn("failed to prune terminal telegram updates", "error", err)
	} else if deleted > 0 {
		slog.Info("pruned terminal telegram updates", "count", deleted)
	}

	// 2. Clean temporary files (.ogg, .csv) older than 24h
	cleanTempDir()
}

func cleanTempDir() {
	tmpDir := filepath.Join(os.TempDir(), "jeffries_homeapp")
	if _, err := os.Stat(tmpDir); os.IsNotExist(err) {
		return
	}

	cutoff := time.Now().Add(-24 * time.Hour)
	deletedFiles := 0

	err := filepath.Walk(tmpDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip files we cannot access
		}

		// Don't delete directories, only specific files
		if info.IsDir() {
			return nil
		}

		// Only target our specific temp extensions
		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".ogg" || ext == ".csv" || ext == ".tmp" {
			if info.ModTime().Before(cutoff) {
				if err := os.Remove(path); err == nil {
					deletedFiles++
				}
			}
		}
		return nil
	})

	if err != nil {
		slog.Error("failed to walk temp directory", "error", err)
	} else if deletedFiles > 0 {
		slog.Info("cleaned temporary files", "deleted_files", deletedFiles, "directory", tmpDir)
	}
}
