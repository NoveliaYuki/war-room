package service

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"war-room/backend/pkg/repository"
)

// Scheduler limits: at most this many backups upload at once, and each check
// claims at most dueBatchSize schedules (the rest are picked up next tick).
const (
	maxConcurrentScheduledBackups = 2
	dueBackupBatchSize            = 50
	// retryInvalidScheduleAfter keeps a schedule whose stored settings no
	// longer parse (for example a zone removed from tzdata) from being
	// retried on every tick.
	retryInvalidScheduleAfter = 24 * time.Hour
)

// BackupScheduler runs each account's automatic Drive backups when they are
// due. Time comes from an injected clock and checks from an injected tick
// channel, so tests control both and never wait on real time.
type BackupScheduler struct {
	repo   *repository.Repository
	backup func(userID string) error
	now    func() time.Time
	slots  chan struct{}
	runs   sync.WaitGroup

	cancel   context.CancelFunc
	loopDone chan struct{}
}

// NewBackupScheduler creates a scheduler. backup uploads one account's backup
// (DriveBackupService.BackupNow in production; a fake in tests).
func NewBackupScheduler(repo *repository.Repository, backup func(userID string) error, now func() time.Time) *BackupScheduler {
	if now == nil {
		now = time.Now
	}
	return &BackupScheduler{repo: repo, backup: backup, now: now, slots: make(chan struct{}, maxConcurrentScheduledBackups)}
}

// Start checks for due backups immediately, then on every value from ticks,
// until ctx ends or Stop is called. A schedule missed while the server was
// down runs once at startup, then resumes its normal times.
func (s *BackupScheduler) Start(ctx context.Context, ticks <-chan time.Time) {
	ctx, s.cancel = context.WithCancel(ctx)
	s.loopDone = make(chan struct{})
	go func() {
		defer close(s.loopDone)
		s.RunDue(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticks:
				s.RunDue(ctx)
			}
		}
	}()
}

// Stop ends the check loop and waits up to timeout for running backups to
// finish. It reports whether everything finished in time.
func (s *BackupScheduler) Stop(timeout time.Duration) bool {
	if s.cancel == nil {
		return true
	}
	s.cancel()
	finished := make(chan struct{})
	go func() {
		<-s.loopDone
		s.runs.Wait()
		close(finished)
	}()
	select {
	case <-finished:
		return true
	case <-time.After(timeout):
		return false
	}
}

// RunDue claims and starts every backup due now, waiting for a free upload
// slot before each one. It returns how many backups it started; Wait blocks
// until they finish.
func (s *BackupScheduler) RunDue(ctx context.Context) int {
	due, err := s.repo.ListDueBackupSchedules(s.now(), dueBackupBatchSize)
	if err != nil {
		slog.Error("scheduled backup check could not list due schedules", "error", err.Error())
		return 0
	}
	started := 0
	for _, schedule := range due {
		select {
		case <-ctx.Done():
			return started
		case s.slots <- struct{}{}:
		}
		if !s.claim(schedule) {
			<-s.slots
			continue
		}
		started++
		s.runs.Add(1)
		go s.run(schedule.UserID)
	}
	return started
}

// Wait blocks until every backup started by RunDue has finished.
func (s *BackupScheduler) Wait() {
	s.runs.Wait()
}

// claim advances schedule to its next run before the backup starts, so the
// same run is never started twice, even across a restart.
func (s *BackupScheduler) claim(schedule repository.BackupScheduleRow) bool {
	now := s.now()
	next := now.Add(retryInvalidScheduleAfter)
	settings, err := settingsFromRow(schedule)
	if err != nil {
		slog.Error("stored backup schedule is invalid; retrying later", "user_id", schedule.UserID, "error", err.Error())
	} else if computed := settings.nextRun(now); computed != nil {
		next = *computed
	}
	won, err := s.repo.ClaimBackupScheduleRun(schedule.UserID, *schedule.NextRunAt, next)
	if err != nil {
		slog.Error("could not claim scheduled backup", "user_id", schedule.UserID, "error", err.Error())
		return false
	}
	return won && settings.location != nil
}

func (s *BackupScheduler) run(userID string) {
	defer s.runs.Done()
	defer func() { <-s.slots }()
	result := BackupResult(s.backup(userID))
	if err := s.repo.RecordBackupScheduleRun(userID, s.now(), result); err != nil {
		slog.Error("could not record scheduled backup result", "user_id", userID, "result", result, "error", err.Error())
		return
	}
	slog.Info("scheduled google drive backup finished", "user_id", userID, "result", result)
}

// BackupResult turns a backup error into the result code shown to the user.
func BackupResult(err error) string {
	switch {
	case err == nil:
		return BackupResultOK
	case errors.Is(err, ErrGoogleNotConfigured):
		return BackupResultDriveNotConfigured
	case errors.Is(err, ErrGoogleNotConnected):
		return BackupResultDriveNotConnected
	case IsGoogleAccessExpired(err):
		return BackupResultGoogleAccessExpired
	default:
		return BackupResultFailed
	}
}
