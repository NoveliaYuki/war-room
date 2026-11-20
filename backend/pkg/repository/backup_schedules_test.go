package repository

import (
	"fmt"
	"testing"
	"time"
)

func mustNoScheduleErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func optionalTime(value *time.Time) string {
	if value == nil {
		return "none"
	}
	return value.UTC().Format(time.RFC3339)
}

// describeSchedule loads the test user's schedule as one comparable line.
func describeSchedule(t *testing.T, repo *Repository) string {
	t.Helper()
	row, found, err := repo.GetBackupSchedule(testUserID)
	mustNoScheduleErr(t, err)
	if !found {
		return "missing"
	}
	return fmt.Sprintf("%s %d %d %s next=%s last=%s result=%q", row.Frequency, row.Weekday, row.MinuteOfDay, row.TimeZone,
		optionalTime(row.NextRunAt), optionalTime(row.LastRunAt), row.LastRunResult)
}

func requireSchedule(t *testing.T, repo *Repository, want, why string) {
	t.Helper()
	if got := describeSchedule(t, repo); got != want {
		t.Fatalf("%s:\n got %s\nwant %s", why, got, want)
	}
}

func TestBackupScheduleRepositoryLifecycle(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	requireSchedule(t, repo, "missing", "a new account has no schedule")
	next := time.Date(2026, 11, 16, 8, 0, 0, 0, time.UTC)
	row := BackupScheduleRow{UserID: testUserID, Frequency: "weekly", Weekday: 1, MinuteOfDay: 540, TimeZone: "Europe/Madrid", NextRunAt: &next}
	mustNoScheduleErr(t, repo.SaveBackupSchedule(row))
	requireSchedule(t, repo, `weekly 1 540 Europe/Madrid next=2026-11-16T08:00:00Z last=none result=""`, "saved schedule")

	mustNoScheduleErr(t, repo.RecordBackupScheduleRun(testUserID, next.Add(time.Minute), "google_access_expired"))
	row.Frequency, row.NextRunAt = "off", nil
	mustNoScheduleErr(t, repo.SaveBackupSchedule(row))
	kept := `off 1 540 Europe/Madrid next=none last=2026-11-16T08:01:00Z result="google_access_expired"`
	requireSchedule(t, repo, kept, "saving settings keeps the last run result")

	mustNoScheduleErr(t, repo.ClearBackupScheduleResult(testUserID, "failed"))
	requireSchedule(t, repo, kept, "clearing a different result keeps the stored one")
	mustNoScheduleErr(t, repo.ClearBackupScheduleResult(testUserID, "google_access_expired"))
	requireSchedule(t, repo, `off 1 540 Europe/Madrid next=none last=2026-11-16T08:01:00Z result=""`, "a matching result is cleared")
}

func requireDueCount(t *testing.T, repo *Repository, now time.Time, want int) {
	t.Helper()
	rows, err := repo.ListDueBackupSchedules(now, 10)
	mustNoScheduleErr(t, err)
	if len(rows) != want {
		t.Fatalf("%d schedules due at %v, want %d", len(rows), now, want)
	}
}

func requireClaim(t *testing.T, repo *Repository, expected, next time.Time, want bool) {
	t.Helper()
	won, err := repo.ClaimBackupScheduleRun(testUserID, expected, next)
	mustNoScheduleErr(t, err)
	if won != want {
		t.Fatalf("claim won=%v, want %v", won, want)
	}
}

func TestBackupScheduleRepositoryListsAndClaimsDueRunsOnce(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	now := time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)
	due := now.Add(-time.Hour)
	later := now.Add(time.Hour)
	mustNoScheduleErr(t, repo.SaveBackupSchedule(BackupScheduleRow{UserID: testUserID, Frequency: "daily", Weekday: 1, MinuteOfDay: 480, TimeZone: "UTC", NextRunAt: &due}))
	requireDueCount(t, repo, due.Add(-time.Second), 0)
	requireDueCount(t, repo, now, 1)
	requireClaim(t, repo, due, later, true)
	requireClaim(t, repo, due, later, false)
	requireDueCount(t, repo, now, 0)
}

func TestBackupScheduleRepositoryReportsDatabaseErrors(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	if err := repo.db.Close(); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if _, _, err := repo.GetBackupSchedule(testUserID); err == nil {
		t.Error("GetBackupSchedule should report a closed database")
	}
	if err := repo.SaveBackupSchedule(BackupScheduleRow{UserID: testUserID, Frequency: "off", TimeZone: "UTC"}); err == nil {
		t.Error("SaveBackupSchedule should report a closed database")
	}
	if _, err := repo.ListDueBackupSchedules(now, 1); err == nil {
		t.Error("ListDueBackupSchedules should report a closed database")
	}
	if _, err := repo.ClaimBackupScheduleRun(testUserID, now, now); err == nil {
		t.Error("ClaimBackupScheduleRun should report a closed database")
	}
	if err := repo.RecordBackupScheduleRun(testUserID, now, "ok"); err == nil {
		t.Error("RecordBackupScheduleRun should report a closed database")
	}
	if err := repo.ClearBackupScheduleResult(testUserID, "ok"); err == nil {
		t.Error("ClearBackupScheduleResult should report a closed database")
	}
}

func TestBackupScheduleRepositoryReportsUnreadableRows(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	if _, err := repo.db.Exec(`PRAGMA ignore_check_constraints = ON;
		INSERT INTO backup_schedules (user_id, frequency, weekday, minute_of_day, time_zone, next_run_at) VALUES (?, 'daily', 'not a number', 0, 'UTC', 1)`, testUserID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ListDueBackupSchedules(time.Now(), 10); err == nil {
		t.Fatal("ListDueBackupSchedules should report a row it cannot scan")
	}
}
