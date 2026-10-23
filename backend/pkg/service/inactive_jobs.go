package service

import (
	"context"
	"log"
	"time"

	"war-room/backend/pkg/repository"
)

// StartInactiveWaitingScheduler checks inactive jobs at startup and on each interval.
func StartInactiveWaitingScheduler(ctx context.Context, repo *repository.Repository, interval time.Duration) {
	StartInactiveWaitingSchedulerWithCallback(ctx, repo, interval, nil)
}

// StartInactiveWaitingSchedulerWithCallback checks inactive jobs and calls onRejected when jobs change status.
func StartInactiveWaitingSchedulerWithCallback(ctx context.Context, repo *repository.Repository, interval time.Duration, onRejected func()) {
	startInactiveWaitingSchedulerWithCallback(ctx, repo, time.Now, interval, onRejected)
}

func startInactiveWaitingScheduler(ctx context.Context, repo *repository.Repository, now func() time.Time, interval time.Duration) {
	startInactiveWaitingSchedulerWithCallback(ctx, repo, now, interval, nil)
}

func startInactiveWaitingSchedulerWithCallback(ctx context.Context, repo *repository.Repository, now func() time.Time, interval time.Duration, onRejected func()) {
	check := func() {
		count, err := repo.RejectInactiveWaitingJobs(now())
		if err != nil {
			log.Printf("Failed to reject inactive waiting jobs: %v", err)
			return
		}
		if count > 0 {
			log.Printf("Automatically rejected %d inactive waiting job(s)", count)
			if onRejected != nil {
				onRejected()
			}
		}
	}
	check()

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				check()
			}
		}
	}()
}
