package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"

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
	if err := run(); err != nil {
		slog.Error("homeapp API stopped with an error", "error", err)
		os.Exit(1)
	}
}

// run owns all resources so every error path executes defers and returns a
// non-nil error to main. Only normal/signal-driven shutdown returns nil.
func run() error {
	cfg := config.Load()
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.SlogLevel(),
	})))
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	slog.Info("starting homeapp API", "env", cfg.AppEnv, "port", cfg.AppPort)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("database connection failed: %w", err)
	}
	defer db.Close()
	if err := store.EnsureRuntimeSchema(ctx, db); err != nil {
		return fmt.Errorf("runtime schema check failed: %w", err)
	}

	backgroundCtx, cancelBackground := context.WithCancel(ctx)
	var backgroundWG sync.WaitGroup
	backgroundWG.Add(1)
	go func() {
		defer backgroundWG.Done()
		engine.RunCleaner(backgroundCtx, db)
	}()
	if cfg.StartBackgroundEngine {
		slog.Info("starting background automation engine (Telegram bot + Crons)")
		eng := engine.New(cfg, db)
		backgroundWG.Add(1)
		go func() {
			defer backgroundWG.Done()
			eng.Run(backgroundCtx)
		}()
	}

	srv := server.New(cfg, db)
	serveErr := srv.ListenAndServe(ctx)
	cancelBackground()
	backgroundWG.Wait()
	if serveErr != nil {
		return serveErr
	}
	slog.Info("homeapp API stopped cleanly")
	return nil
}
