package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/Jeffreasy/JeffriesBackend/internal/config"
	"github.com/Jeffreasy/JeffriesBackend/internal/engine"
	"github.com/Jeffreasy/JeffriesBackend/internal/server"
	"github.com/Jeffreasy/JeffriesBackend/internal/store"
)

// @title Jeffries Homeapp API
// @version 1.0
// @description Backend REST API for Jeffries Homeapp.
// @host localhost:8000
// @BasePath /api/v1
// @securityDefinitions.apikey ApiKeyAuth
// @in header
// @name X-API-Key
func main() {
	cfg := config.Load()

	// Configure structured logging
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.SlogLevel(),
	})))

	slog.Info("starting homeapp API", "env", cfg.AppEnv, "port", cfg.AppPort)

	// Connect to database
	ctx := context.Background()
	db, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("database connection failed", "error", err)
		os.Exit(1)
	}
	if err := store.EnsureRuntimeSchema(ctx, db); err != nil {
		slog.Error("runtime schema check failed", "error", err)
		os.Exit(1)
	}

	// Start background automation engine if enabled (e.g. on Render)
	if cfg.StartBackgroundEngine {
		slog.Info("starting background automation engine (Telegram bot + Crons)")
		eng := engine.New(cfg, db)

		engineCtx, cancelEngine := context.WithCancel(context.Background())
		defer cancelEngine()

		go eng.Run(engineCtx)
	}

	// Start HTTP server (blocks until shutdown)
	srv := server.New(cfg, db)
	srv.ListenAndServe()
}
