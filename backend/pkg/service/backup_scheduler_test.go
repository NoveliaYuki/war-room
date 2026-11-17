package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"war-room/backend/pkg/repository"
)

// fakeClock is a settable clock shared by the scheduler and the test.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Set(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = now
}

// fakeBackups records scheduled backup calls instead of calling Google Drive.
type fakeBackups struct {
	mu      sync.Mutex
	calls   map[string]int
	errs    map[string]error
	release chan struct{}
	active  atomic.Int32
	peak    atomic.Int32
}

func newFakeBackups() *fakeBackups {
	return &fakeBackups{calls: map[string]int{}, errs: map[string]error{}}
}

func (f *fakeBackups) BackupNow(userID string) error {
	current := f.active.Add(1)
	defer f.active.Add(-1)
	for {
		peak := f.peak.Load()
		if current <= peak || f.peak.CompareAndSwap(peak, current) {
			break
		}
	}
	if f.release != nil {
		<-f.release
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[userID]++
	return f.errs[userID]
}

func (f *fakeBackups) count(userID string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[userID]
}

func saveTestSchedule(t *testing.T, schedules *BackupScheduleService, userID string, input BackupScheduleInput) {
	t.Helper()
	if _, err := schedules.Save(userID, input); err != nil {
		t.Fatal(err)
	}
}

func TestSchedulerRunsDueBackupsOnceAndRecordsResults(t *testing.T) {
	repo := newScheduleTestRepo(t, "ok-user", "expired-user", "off-user", "later-user")
	clock := &fakeClock{now: time.Date(2026, 11, 16, 7, 0, 0, 0, time.UTC)}
	schedules := NewBackupScheduleService(repo, clock.Now)
	saveTestSchedule(t, schedules, "ok-user", BackupScheduleInput{Frequency: "weekly", Weekday: 1, Time: "09:00", TimeZone: "Europe/Madrid"})
	saveTestSchedule(t, schedules, "expired-user", BackupScheduleInput{Frequency: "daily", Time: "08:30", TimeZone: "Europe/Madrid"})
	saveTestSchedule(t, schedules, "off-user", BackupScheduleInput{Frequency: "off", Time: "08:00", TimeZone: "Europe/Madrid"})
	saveTestSchedule(t, schedules, "later-user", BackupScheduleInput{Frequency: "daily", Time: "18:00", TimeZone: "Europe/Madrid"})

	backups := newFakeBackups()
	backups.errs["expired-user"] = newGoogleAPIError("token request", 400, []byte(`{"error":"invalid_grant"}`))
	scheduler := NewBackupScheduler(repo, backups.BackupNow, clock.Now)

	requireStarted(t, scheduler, 0, "nothing is due at 08:00 Madrid")
	clock.Set(time.Date(2026, 11, 16, 8, 0, 30, 0, time.UTC)) // Monday 09:00:30 in Madrid
	requireStarted(t, scheduler, 2, "the two due backups")
	requireStarted(t, scheduler, 0, "a run must not repeat within the same minute")
	calls := fmt.Sprint(backups.count("ok-user"), backups.count("expired-user"), backups.count("off-user"), backups.count("later-user"))
	if calls != "1 1 0 0" {
		t.Fatalf("backup calls (ok, expired, off, later) = %s", calls)
	}
	ok, err := schedules.Get("ok-user")
	requireDescribed(t, ok, err, `weekly 1 09:00 Europe/Madrid next=2026-11-23T08:00:00Z last=2026-11-16T08:00:30Z result="ok"`)
	expired, err := schedules.Get("expired-user")
	requireDescribed(t, expired, err, `daily 0 08:30 Europe/Madrid next=2026-11-17T07:30:00Z last=2026-11-16T08:00:30Z result="google_access_expired"`)
}

// requireStarted runs one check, waits for its backups, and checks how many started.
func requireStarted(t *testing.T, scheduler *BackupScheduler, want int, why string) {
	t.Helper()
	started := scheduler.RunDue(context.Background())
	scheduler.Wait()
	if started != want {
		t.Fatalf("%s: started %d, want %d", why, started, want)
	}
}

func TestSchedulerCatchesUpOnceAfterDowntime(t *testing.T) {
	repo := newScheduleTestRepo(t, "user-1")
	clock := &fakeClock{now: time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)}
	schedules := NewBackupScheduleService(repo, clock.Now)
	saveTestSchedule(t, schedules, "user-1", BackupScheduleInput{Frequency: "daily", Time: "09:00", TimeZone: "UTC"})

	clock.Set(time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)) // three missed runs
	backups := newFakeBackups()
	restarted := NewBackupScheduler(repo, backups.BackupNow, clock.Now)
	if started := restarted.RunDue(context.Background()); started != 1 {
		t.Fatalf("missed runs should produce one catch-up run, started %d", started)
	}
	restarted.Wait()
	schedule, _ := schedules.Get("user-1")
	if !schedule.NextRunAt.Equal(time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("after catching up, the next run is the next normal time, got %v", schedule.NextRunAt)
	}
	if again := NewBackupScheduler(repo, backups.BackupNow, clock.Now).RunDue(context.Background()); again != 0 || backups.count("user-1") != 1 {
		t.Fatalf("a second restart must not run the claimed backup again, started %d, calls %d", again, backups.count("user-1"))
	}
}

func TestSchedulerLimitsConcurrentUploads(t *testing.T) {
	users := []string{"user-1", "user-2", "user-3", "user-4", "user-5"}
	repo := newScheduleTestRepo(t, users...)
	clock := &fakeClock{now: time.Date(2026, 11, 13, 8, 0, 0, 0, time.UTC)}
	schedules := NewBackupScheduleService(repo, clock.Now)
	for _, userID := range users {
		saveTestSchedule(t, schedules, userID, BackupScheduleInput{Frequency: "daily", Time: "09:00", TimeZone: "UTC"})
	}
	clock.Set(time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC))
	backups := newFakeBackups()
	backups.release = make(chan struct{})
	scheduler := NewBackupScheduler(repo, backups.BackupNow, clock.Now)

	started := make(chan int)
	go func() { started <- scheduler.RunDue(context.Background()) }()
	for i := 0; i < len(users); i++ {
		backups.release <- struct{}{}
	}
	if got := <-started; got != len(users) {
		t.Fatalf("started %d backups, want %d", got, len(users))
	}
	scheduler.Wait()
	if peak := backups.peak.Load(); peak > maxConcurrentScheduledBackups {
		t.Fatalf("%d backups ran at once, limit is %d", peak, maxConcurrentScheduledBackups)
	}
}

func TestSchedulerStopsWaitingForASlotWhenCancelled(t *testing.T) {
	users := []string{"user-1", "user-2", "user-3"}
	repo := newScheduleTestRepo(t, users...)
	clock := &fakeClock{now: time.Date(2026, 11, 13, 8, 0, 0, 0, time.UTC)}
	schedules := NewBackupScheduleService(repo, clock.Now)
	for _, userID := range users {
		saveTestSchedule(t, schedules, userID, BackupScheduleInput{Frequency: "daily", Time: "09:00", TimeZone: "UTC"})
	}
	clock.Set(time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC))
	backups := newFakeBackups()
	backups.release = make(chan struct{})
	scheduler := NewBackupScheduler(repo, backups.BackupNow, clock.Now)
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan int)
	go func() { started <- scheduler.RunDue(ctx) }()
	for backups.active.Load() < maxConcurrentScheduledBackups {
		time.Sleep(time.Millisecond)
	}
	cancel()
	if got := <-started; got != maxConcurrentScheduledBackups {
		t.Fatalf("started %d backups before cancel, want %d", got, maxConcurrentScheduledBackups)
	}
	close(backups.release)
	scheduler.Wait()
	third, _ := schedules.Get("user-3")
	if !third.NextRunAt.Equal(time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("an unstarted backup must stay due for the next start, next=%v", third.NextRunAt)
	}
}

func TestSchedulerLoopRunsOnTicksAndStopsCleanly(t *testing.T) {
	repo := newScheduleTestRepo(t, "user-1")
	clock := &fakeClock{now: time.Date(2026, 11, 13, 8, 0, 0, 0, time.UTC)}
	schedules := NewBackupScheduleService(repo, clock.Now)
	saveTestSchedule(t, schedules, "user-1", BackupScheduleInput{Frequency: "daily", Time: "09:00", TimeZone: "UTC"})
	backups := newFakeBackups()
	scheduler := NewBackupScheduler(repo, backups.BackupNow, clock.Now)
	ticks := make(chan time.Time)
	scheduler.Start(context.Background(), ticks)

	clock.Set(time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC))
	ticks <- clock.Now()
	ticks <- clock.Now() // the loop has finished the first check once this send returns
	if !scheduler.Stop(5 * time.Second) {
		t.Fatal("scheduler did not stop")
	}
	if backups.count("user-1") != 1 {
		t.Fatalf("one tick past the due time should run one backup, got %d", backups.count("user-1"))
	}
	if !NewBackupScheduler(repo, backups.BackupNow, clock.Now).Stop(time.Millisecond) {
		t.Fatal("stopping a scheduler that never started should succeed")
	}
}

func TestSchedulerStopTimesOutOnSlowBackup(t *testing.T) {
	repo := newScheduleTestRepo(t, "user-1")
	clock := &fakeClock{now: time.Date(2026, 11, 13, 8, 0, 0, 0, time.UTC)}
	saveTestSchedule(t, NewBackupScheduleService(repo, clock.Now), "user-1", BackupScheduleInput{Frequency: "daily", Time: "09:00", TimeZone: "UTC"})
	clock.Set(time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC))
	backups := newFakeBackups()
	backups.release = make(chan struct{})
	scheduler := NewBackupScheduler(repo, backups.BackupNow, clock.Now)
	scheduler.Start(context.Background(), nil)
	for backups.active.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	if scheduler.Stop(10 * time.Millisecond) {
		t.Fatal("Stop should report a backup still running")
	}
	close(backups.release)
	scheduler.Wait()
}

func mustExec(t *testing.T, db *sql.DB, statement string, args ...any) {
	t.Helper()
	if _, err := db.Exec(statement, args...); err != nil {
		t.Fatal(err)
	}
}

func TestSchedulerPostponesAnUnreadableStoredSchedule(t *testing.T) {
	db := newDriveTestDB(t, t.TempDir())
	seedDriveUser(t, db, "user-1")
	now := time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)
	mustExec(t, db, `INSERT INTO backup_schedules (user_id, frequency, weekday, minute_of_day, time_zone, next_run_at) VALUES ('user-1', 'daily', 1, 540, 'Gone/Zone', ?)`, now.Unix())
	backups := newFakeBackups()
	scheduler := NewBackupScheduler(repository.New(db), backups.BackupNow, func() time.Time { return now })
	requireStarted(t, scheduler, 0, "an unreadable schedule must not run")
	var next int64
	if err := db.QueryRow(`SELECT next_run_at FROM backup_schedules WHERE user_id = 'user-1'`).Scan(&next); err != nil {
		t.Fatal(err)
	}
	if next != now.Add(retryInvalidScheduleAfter).Unix() {
		t.Fatalf("an unreadable schedule should be retried a day later, next=%d", next)
	}
}

func TestSchedulerSurvivesStorageErrors(t *testing.T) {
	db := newDriveTestDB(t, t.TempDir())
	seedDriveUser(t, db, "user-1")
	now := time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)
	mustExec(t, db, `INSERT INTO backup_schedules (user_id, frequency, weekday, minute_of_day, time_zone, next_run_at) VALUES ('user-1', 'daily', 1, 540, 'UTC', ?)`, now.Unix())
	backups := newFakeBackups()
	scheduler := NewBackupScheduler(repository.New(db), backups.BackupNow, func() time.Time { return now })

	mustExec(t, db, `CREATE TRIGGER block_claim BEFORE UPDATE OF next_run_at ON backup_schedules BEGIN SELECT RAISE(ABORT, 'blocked'); END`)
	requireStarted(t, scheduler, 0, "a failed claim must not run the backup")
	mustExec(t, db, `DROP TRIGGER block_claim; CREATE TRIGGER block_record BEFORE UPDATE OF last_run_at ON backup_schedules BEGIN SELECT RAISE(ABORT, 'blocked'); END`)
	requireStarted(t, scheduler, 1, "a result that cannot be recorded still runs the backup")

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	requireStarted(t, scheduler, 0, "a database error must start nothing")
}

func TestNewBackupSchedulerDefaultsToRealClock(t *testing.T) {
	scheduler := NewBackupScheduler(nil, func(string) error { return errors.New("unused") }, nil)
	if time.Since(scheduler.now()) > time.Minute {
		t.Fatal("a scheduler without a clock should use the real time")
	}
}
