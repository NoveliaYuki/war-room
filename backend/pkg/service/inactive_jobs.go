package service

import (
	"context"
	"log"
	"time"

	"war-room/backend/pkg/repository"
)

// StartInactiveWaitingScheduler checks inactive jobs at startup and on each interval.
func StartInactiveWaitingScheduler(ctx context.Context, repo *repository.Repository, interval time.Duration) {
	startInactiveWaitingScheduler(ctx, repo, time.Now, interval)
}

func startInactiveWaitingScheduler(ctx context.Context, repo *repository.Repository, now func() time.Time, interval time.Duration) {
	check := func() {
		count, err := repo.RejectInactiveWaitingJobs(now())
		if err != nil {
			log.Printf("Failed to reject inactive waiting jobs: %v", err)
			return
		}
		if count > 0 {
			log.Printf("Automatically rejected %d inactive waiting job(s)", count)
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
