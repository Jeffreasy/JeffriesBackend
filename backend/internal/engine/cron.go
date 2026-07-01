package engine

import (
	"context"
	"log/slog"
	"math/rand"
	"sync"
	"time"
)

// CronJob defines a repeating background task.
type CronJob struct {
	Name     string
	Interval time.Duration
	RunFunc  func(ctx context.Context) error
	// RunOnStart runs the job once shortly after boot (with a small jitter)
	// instead of waiting a full interval. Set this on idempotent sync jobs so a
	// deploy/restart doesn't leave data stale for up to a day.
	RunOnStart bool
}

// CronScheduler manages background cron jobs as goroutines.
type CronScheduler struct {
	jobs    []CronJob
	mu      sync.Mutex
	running bool
}

// NewCronScheduler creates a scheduler.
func NewCronScheduler() *CronScheduler {
	return &CronScheduler{}
}

// Register adds a cron job. Must be called before Run.
func (s *CronScheduler) Register(job CronJob) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs = append(s.jobs, job)
	slog.Info("📋 cron registered", "name", job.Name, "interval", job.Interval)
}

// Run starts all cron jobs as goroutines and blocks until context is cancelled.
func (s *CronScheduler) Run(ctx context.Context) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.mu.Unlock()

	slog.Info("🕐 cron scheduler starting", "jobs", len(s.jobs))

	var wg sync.WaitGroup
	for _, job := range s.jobs {
		wg.Add(1)
		go func(j CronJob) {
			defer wg.Done()
			s.runJob(ctx, j)
		}(job)
	}

	wg.Wait()
	slog.Info("🛑 cron scheduler stopped")
}

func (s *CronScheduler) runJob(ctx context.Context, job CronJob) {
	slog.Info("⏰ cron job started", "name", job.Name, "interval", job.Interval)

	if job.RunOnStart {
		// Small per-job jitter so all startup syncs don't fire at once.
		jitter := time.Duration(rand.Intn(8000)) * time.Millisecond
		select {
		case <-ctx.Done():
			slog.Info("⏹ cron job stopped", "name", job.Name)
			return
		case <-time.After(jitter):
			s.execJob(ctx, job)
		}
	}

	ticker := time.NewTicker(job.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("⏹ cron job stopped", "name", job.Name)
			return
		case <-ticker.C:
			s.execJob(ctx, job)
		}
	}
}

func (s *CronScheduler) execJob(ctx context.Context, job CronJob) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("🔥 cron job panicked!", "name", job.Name, "panic", r)
		}
	}()

	runCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	start := time.Now()
	if err := job.RunFunc(runCtx); err != nil {
		slog.Warn("❌ cron job failed", "name", job.Name, "error", err, "took", time.Since(start))
	} else {
		slog.Debug("✅ cron job done", "name", job.Name, "took", time.Since(start))
	}
}

