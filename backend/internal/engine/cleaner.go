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

// StartCleaner starts a background task that periodically cleans up old database logs and temp files.
func StartCleaner(ctx context.Context, db *store.DB) {
	slog.Info("starting background cleaner service", "interval", "24h")
	
	ticker := time.NewTicker(24 * time.Hour)
	go func() {
		defer ticker.Stop()
		// Perform the initial cleanup inside the goroutine so startup is non-blocking
		performCleanup(ctx, db)
		for {
			select {
			case <-ctx.Done():
				slog.Info("background cleaner stopped")
				return
			case <-ticker.C:
				performCleanup(ctx, db)
			}
		}
	}()
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
