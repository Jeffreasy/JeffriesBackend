package engine

import (
	"context"
	"testing"
)

func TestRunCleanerAlreadyCancelledReturnsWithoutTouchingDB(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		RunCleaner(ctx, nil)
	}()
	<-done
}
