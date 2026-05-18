package engine

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// CronJob defines a repeating background task.
type CronJob struct {
	Name     string
	Interval time.Duration
	RunFunc  func(ctx context.Context) error
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
	ticker := time.NewTicker(job.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("⏹ cron job stopped", "name", job.Name)
			return
		case <-ticker.C:
			start := time.Now()
			if err := job.RunFunc(ctx); err != nil {
				slog.Warn("❌ cron job failed", "name", job.Name, "error", err, "took", time.Since(start))
			} else {
				slog.Debug("✅ cron job done", "name", job.Name, "took", time.Since(start))
			}
		}
	}
}
