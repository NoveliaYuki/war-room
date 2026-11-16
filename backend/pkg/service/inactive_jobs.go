package service

import (
	"context"
	"log/slog"
	"time"

	"war-room/backend/pkg/logging"
	"war-room/backend/pkg/repository"
)

// StartInactiveWaitingScheduler checks inactive jobs at startup and on each interval.
func StartInactiveWaitingScheduler(ctx context.Context, repo *repository.Repository, interval time.Duration) {
	StartInactiveWaitingSchedulerWithCallback(ctx, repo, interval, nil)
}

// StartInactiveWaitingSchedulerWithCallback checks every account's inactive
// jobs and calls onRejected with a user's ID when that user's jobs changed status.
func StartInactiveWaitingSchedulerWithCallback(ctx context.Context, repo *repository.Repository, interval time.Duration, onRejected func(userID string)) {
	startInactiveWaitingSchedulerWithCallback(ctx, repo, time.Now, interval, onRejected)
}

func startInactiveWaitingScheduler(ctx context.Context, repo *repository.Repository, now func() time.Time, interval time.Duration) {
	startInactiveWaitingSchedulerWithCallback(ctx, repo, now, interval, nil)
}

func startInactiveWaitingSchedulerWithCallback(ctx context.Context, repo *repository.Repository, now func() time.Time, interval time.Duration, onRejected func(userID string)) {
	check := func() {
		checkAllUsersInactiveWaitingJobs(repo, now(), onRejected)
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

// checkAllUsersInactiveWaitingJobs rejects each account's own inactive
// waiting jobs independently, so the 30-day inactivity rule never leaks
// activity or outcomes across accounts.
func checkAllUsersInactiveWaitingJobs(repo *repository.Repository, now time.Time, onRejected func(userID string)) {
	userIDs, err := repo.ListUserIDs()
	if err != nil {
		slog.Error("inactive waiting job sweep could not list users", "error", err.Error())
		return
	}
	for _, userID := range userIDs {
		count, err := repo.RejectInactiveWaitingJobs(userID, now)
		if err != nil {
			slog.Error("inactive waiting job sweep failed for user", "user_id", userID, "error", err.Error())
			continue
		}
		if count > 0 {
			logging.Audit(context.Background(), "jobs.auto_rejected_inactive", "user_id", userID, "count", count)
			if onRejected != nil {
				onRejected(userID)
			}
		}
	}
}
