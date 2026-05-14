package job

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestSchedulerSkipsOverlappingRuns(t *testing.T) {
	t.Parallel()

	scheduler := NewScheduler()
	var runs atomic.Int32
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	if err := scheduler.Every("slow", 10*time.Millisecond, func(ctx context.Context) error {
		runs.Add(1)
		select {
		case started <- struct{}{}:
		default:
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-release:
			return nil
		}
	}); err != nil {
		t.Fatalf("Every() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	scheduler.Start(ctx)
	defer scheduler.Stop()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first run")
	}
	time.Sleep(50 * time.Millisecond)
	if got := runs.Load(); got != 1 {
		t.Fatalf("expected overlapping ticks to be skipped, got %d runs", got)
	}
	close(release)
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		if runs.Load() >= 2 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected scheduler to run again after release, got %d runs", runs.Load())
}

func TestScheduledJobRetriesFailures(t *testing.T) {
	t.Parallel()

	job := &scheduledJob{
		fn: func(ctx context.Context) error {
			_ = ctx
			return errors.New("boom")
		},
		retryDelay:    time.Millisecond,
		maxRetryCount: 2,
	}
	var attempts atomic.Int32
	job.fn = func(ctx context.Context) error {
		_ = ctx
		attempts.Add(1)
		return errors.New("boom")
	}
	job.run(context.Background())
	if got := attempts.Load(); got != 3 {
		t.Fatalf("expected 3 attempts, got %d", got)
	}
	if job.lastError == "" {
		t.Fatal("expected last error to be recorded")
	}
}
