package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"sync"
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

	if err := cfg.Validate(); err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	slog.Info("============================================================")
	slog.Info("🤖 Homeapp Automation Engine (PostgreSQL native)")
	if len(cfg.HomeappUserID) > 12 {
		slog.Info("   User:   " + cfg.HomeappUserID[:12] + "...")
	}
	slog.Info("============================================================")

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if cfg.BridgeAPIURL != "" {
		slog.Info("starting local cloud bridge mode", "api", cfg.BridgeAPIURL)
		engine.RunCloudBridge(ctx, cfg)
		slog.Info("✅ cloud bridge cleanly stopped")
		return
	}

	db, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("database connection failed", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	if err := store.EnsureRuntimeSchema(ctx, db); err != nil {
		slog.Error("runtime schema check failed", "error", err)
		os.Exit(1)
	}

	eng := engine.New(cfg, db)
	var cleanerWG sync.WaitGroup
	cleanerWG.Add(1)
	go func() {
		defer cleanerWG.Done()
		engine.RunCleaner(ctx, db)
	}()

	eng.Run(ctx)
	cleanerWG.Wait()

	slog.Info("✅ automation engine cleanly stopped")
}
