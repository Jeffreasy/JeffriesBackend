package store

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DB wraps a pgxpool.Pool and provides convenience methods.
type DB struct {
	Pool *pgxpool.Pool
}

// New creates a new database connection pool.
func New(ctx context.Context, databaseURL string) (*DB, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}

	cfg.MinConns = 2
	cfg.MaxConns = 20

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create connection pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	slog.Info("database connected", "url", sanitizeURL(databaseURL))
	return &DB{Pool: pool}, nil
}

// Close shuts down the connection pool.
func (db *DB) Close() {
	db.Pool.Close()
}

// Ping verifies the database connection is alive.
func (db *DB) Ping(ctx context.Context) error {
	return db.Pool.Ping(ctx)
}

// sanitizeURL hides the password in log output.
func sanitizeURL(url string) string {
	// Basic sanitization — hide password between : and @
	result := []byte(url)
	inPassword := false
	atFound := false
	for i := len(result) - 1; i >= 0; i-- {
		if result[i] == '@' && !atFound {
			atFound = true
			inPassword = true
			continue
		}
		if inPassword && result[i] == ':' {
			inPassword = false
			continue
		}
		if inPassword {
			result[i] = '*'
		}
	}
	return string(result)
}
