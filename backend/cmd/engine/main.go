package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/Jeffreasy/JeffriesBackend/internal/config"
	"github.com/Jeffreasy/JeffriesBackend/internal/engine"
	"github.com/Jeffreasy/JeffriesBackend/internal/store"
)

func main() {
	cfg := config.Load()

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.SlogLevel(),
	})))

	slog.Info("============================================================")
	slog.Info("🤖 Homeapp Automation Engine (PostgreSQL native)")
	if len(cfg.HomeappUserID) > 12 {
		slog.Info("   User:   " + cfg.HomeappUserID[:12] + "...")
	}
	slog.Info("============================================================")

	// Database connection
	dbCtx := context.Background()
	db, err := store.New(dbCtx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("database connection failed", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	if err := store.EnsureRuntimeSchema(dbCtx, db); err != nil {
		slog.Error("runtime schema check failed", "error", err)
		os.Exit(1)
	}

	// Context with OS signal cancellation
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	eng := engine.New(cfg, db)

	// Start the background cleanup service
	engine.StartCleaner(ctx, db)

	eng.Run(ctx) // blocks until SIGTERM/SIGINT

	slog.Info("✅ automation engine cleanly stopped")
}
