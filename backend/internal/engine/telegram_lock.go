package engine

import (
	"context"
	"log/slog"
	"time"
)

const telegramPollerLockKey int64 = 240603409

func (e *Engine) loopTelegramWithLock(ctx context.Context) {
	if e.db == nil || e.db.Pool == nil {
		e.loopTelegram(ctx)
		return
	}

	retry := time.NewTicker(15 * time.Second)
	defer retry.Stop()

	waitingLogged := false
	for {
		release, acquired, err := e.tryAcquireTelegramLock(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Warn("telegram poller lock check failed", "error", err)
		} else if acquired {
			slog.Info("telegram poller lock acquired")
			defer release()
			e.loopTelegram(ctx)
			return
		} else if !waitingLogged {
			slog.Warn("telegram poller lock busy; another backend instance owns getUpdates")
			waitingLogged = true
		}

		select {
		case <-ctx.Done():
			return
		case <-retry.C:
		}
	}
}

func (e *Engine) tryAcquireTelegramLock(ctx context.Context) (func(), bool, error) {
	conn, err := e.db.Pool.Acquire(ctx)
	if err != nil {
		return nil, false, err
	}

	var acquired bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, telegramPollerLockKey).Scan(&acquired); err != nil {
		conn.Release()
		return nil, false, err
	}

	if !acquired {
		conn.Release()
		return nil, false, nil
	}

	released := false
	release := func() {
		if released {
			return
		}
		released = true
		defer conn.Release()

		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := conn.Exec(unlockCtx, `SELECT pg_advisory_unlock($1)`, telegramPollerLockKey); err != nil {
			slog.Warn("telegram poller lock release failed", "error", err)
		}
	}

	return release, true, nil
}
