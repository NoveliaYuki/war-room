package service

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"war-room/backend/pkg/repository"
)

func mustSettings(t *testing.T, input BackupScheduleInput) scheduleSettings {
	t.Helper()
	settings, err := parseBackupScheduleInput(input)
	if err != nil {
		t.Fatalf("parse %+v: %v", input, err)
	}
	return settings
}

func mustLocation(t *testing.T, name string) *time.Location {
	t.Helper()
	location, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return location
}

func TestParseBackupScheduleInputRejectsInvalidFields(t *testing.T) {
	valid := BackupScheduleInput{Frequency: "weekly", Weekday: 1, Time: "09:00", TimeZone: "Europe/Madrid"}
	cases := map[string]struct {
		change func(*BackupScheduleInput)
		want   error
	}{
		"unknown frequency":  {func(in *BackupScheduleInput) { in.Frequency = "hourly" }, ErrBackupScheduleFrequency},
		"cron string":        {func(in *BackupScheduleInput) { in.Frequency = "0 9 * * 1" }, ErrBackupScheduleFrequency},
		"empty frequency":    {func(in *BackupScheduleInput) { in.Frequency = "" }, ErrBackupScheduleFrequency},
		"negative weekday":   {func(in *BackupScheduleInput) { in.Weekday = -1 }, ErrBackupScheduleWeekday},
		"weekday seven":      {func(in *BackupScheduleInput) { in.Weekday = 7 }, ErrBackupScheduleWeekday},
		"empty time":         {func(in *BackupScheduleInput) { in.Time = "" }, ErrBackupScheduleTime},
		"hour 24":            {func(in *BackupScheduleInput) { in.Time = "24:00" }, ErrBackupScheduleTime},
		"minute 60":          {func(in *BackupScheduleInput) { in.Time = "09:60" }, ErrBackupScheduleTime},
		"single digit hour":  {func(in *BackupScheduleInput) { in.Time = "9:00" }, ErrBackupScheduleTime},
		"seconds":            {func(in *BackupScheduleInput) { in.Time = "09:00:00" }, ErrBackupScheduleTime},
		"am pm":              {func(in *BackupScheduleInput) { in.Time = "9:00 AM" }, ErrBackupScheduleTime},
		"empty zone":         {func(in *BackupScheduleInput) { in.TimeZone = "" }, ErrBackupScheduleTimeZone},
		"server local zone":  {func(in *BackupScheduleInput) { in.TimeZone = "Local" }, ErrBackupScheduleTimeZone},
		"unknown zone":       {func(in *BackupScheduleInput) { in.TimeZone = "Mars/Olympus_Mons" }, ErrBackupScheduleTimeZone},
		"path traversal":     {func(in *BackupScheduleInput) { in.TimeZone = "../../etc/passwd" }, ErrBackupScheduleTimeZone},
		"overlong zone name": {func(in *BackupScheduleInput) { in.TimeZone = strings.Repeat("A", 65) }, ErrBackupScheduleTimeZone},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			input := valid
			tc.change(&input)
			if _, err := parseBackupScheduleInput(input); !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
	for _, zone := range []string{"UTC", "Europe/Madrid", "America/New_York", "Asia/Kolkata", "Australia/Lord_Howe"} {
		input := valid
		input.TimeZone = zone
		if _, err := parseBackupScheduleInput(input); err != nil {
			t.Errorf("zone %s should be accepted: %v", zone, err)
		}
	}
}

func TestNextRunDailyAndWeekly(t *testing.T) {
	madrid := mustLocation(t, "Europe/Madrid")
	friday := time.Date(2026, 11, 13, 8, 30, 0, 0, madrid)
	cases := []struct {
		name  string
		input BackupScheduleInput
		after time.Time
		want  time.Time
	}{
		{"daily later today", BackupScheduleInput{"daily", 0, "09:00", "Europe/Madrid"}, friday, time.Date(2026, 11, 13, 9, 0, 0, 0, madrid)},
		{"daily exactly now runs tomorrow", BackupScheduleInput{"daily", 0, "08:30", "Europe/Madrid"}, friday, time.Date(2026, 11, 14, 8, 30, 0, 0, madrid)},
		{"daily earlier today runs tomorrow", BackupScheduleInput{"daily", 0, "07:00", "Europe/Madrid"}, friday, time.Date(2026, 11, 14, 7, 0, 0, 0, madrid)},
		{"weekly next monday", BackupScheduleInput{"weekly", 1, "09:00", "Europe/Madrid"}, friday, time.Date(2026, 11, 16, 9, 0, 0, 0, madrid)},
		{"weekly today later", BackupScheduleInput{"weekly", 5, "23:59", "Europe/Madrid"}, friday, time.Date(2026, 11, 13, 23, 59, 0, 0, madrid)},
		{"weekly today passed runs next week", BackupScheduleInput{"weekly", 5, "08:00", "Europe/Madrid"}, friday, time.Date(2026, 11, 20, 8, 0, 0, 0, madrid)},
		{"weekly sunday across month", BackupScheduleInput{"weekly", 0, "00:00", "Europe/Madrid"}, time.Date(2026, 11, 30, 1, 0, 0, 0, madrid), time.Date(2026, 12, 6, 0, 0, 0, 0, madrid)},
		{"zone ahead of UTC changes the day", BackupScheduleInput{"daily", 0, "06:00", "Asia/Tokyo"}, time.Date(2026, 11, 13, 22, 0, 0, 0, time.UTC), time.Date(2026, 11, 14, 21, 0, 0, 0, time.UTC)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mustSettings(t, tc.input).nextRun(tc.after)
			if got == nil || !got.Equal(tc.want) || got.Location() != time.UTC {
				t.Fatalf("next run = %v, want %v (UTC)", got, tc.want.UTC())
			}
		})
	}
	if next := mustSettings(t, BackupScheduleInput{"off", 1, "09:00", "UTC"}).nextRun(friday); next != nil {
		t.Fatalf("an Off schedule has no next run, got %v", next)
	}
}

func TestNextRunKeepsWallClockAcrossDaylightSaving(t *testing.T) {
	madrid := mustLocation(t, "Europe/Madrid")
	daily := mustSettings(t, BackupScheduleInput{"daily", 0, "09:00", "Europe/Madrid"})

	// Spring forward: 2026-03-29 02:00 becomes 03:00 in Madrid (UTC+1 -> UTC+2).
	beforeSpring := time.Date(2026, 3, 28, 10, 0, 0, 0, madrid)
	if got := daily.nextRun(beforeSpring); !got.Equal(time.Date(2026, 3, 29, 7, 0, 0, 0, time.UTC)) {
		t.Fatalf("09:00 after spring forward should be 07:00 UTC, got %v", got)
	}
	// Fall back: 2026-10-25 03:00 becomes 02:00 (UTC+2 -> UTC+1).
	beforeFall := time.Date(2026, 10, 24, 10, 0, 0, 0, madrid)
	if got := daily.nextRun(beforeFall); !got.Equal(time.Date(2026, 10, 25, 8, 0, 0, 0, time.UTC)) {
		t.Fatalf("09:00 after fall back should be 08:00 UTC, got %v", got)
	}

	// 02:30 does not exist on spring-forward day; it must still run that day, once.
	gap := mustSettings(t, BackupScheduleInput{"daily", 0, "02:30", "Europe/Madrid"})
	first := gap.nextRun(time.Date(2026, 3, 29, 0, 0, 0, 0, madrid))
	if first == nil || first.In(madrid).Day() != 29 {
		t.Fatalf("a time in the spring-forward gap should still run that day, got %v", first)
	}
	second := gap.nextRun(*first)
	if second == nil || !second.Equal(time.Date(2026, 3, 30, 2, 30, 0, 0, madrid)) {
		t.Fatalf("the following run should be back at 02:30, got %v", second)
	}
	// 02:30 happens twice on fall-back day; it must run once, then next day.
	repeat := mustSettings(t, BackupScheduleInput{"daily", 0, "02:30", "Europe/Madrid"})
	once := repeat.nextRun(time.Date(2026, 10, 25, 0, 0, 0, 0, madrid))
	again := repeat.nextRun(*once)
	if again == nil || again.In(madrid).Day() != 26 {
		t.Fatalf("an ambiguous fall-back time must run once per day, runs %v then %v", once, again)
	}
}

func newScheduleTestRepo(t *testing.T, userIDs ...string) *repository.Repository {
	t.Helper()
	db := newDriveTestDB(t, t.TempDir())
	for _, userID := range userIDs {
		seedDriveUser(t, db, userID)
	}
	return repository.New(db)
}

func optionalScheduleTime(value *time.Time) string {
	if value == nil {
		return "none"
	}
	return value.UTC().Format(time.RFC3339)
}

// describe renders a schedule as one comparable line.
func describe(schedule BackupSchedule) string {
	return fmt.Sprintf("%s %d %s %s next=%s last=%s result=%q", schedule.Frequency, schedule.Weekday, schedule.Time, schedule.TimeZone,
		optionalScheduleTime(schedule.NextRunAt), optionalScheduleTime(schedule.LastRunAt), schedule.LastRunResult)
}

func requireDescribed(t *testing.T, schedule BackupSchedule, err error, want string) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	if got := describe(schedule); got != want {
		t.Fatalf("schedule:\n got %s\nwant %s", got, want)
	}
}

func TestBackupScheduleServiceDefaultsToOffAndSaves(t *testing.T) {
	repo := newScheduleTestRepo(t, "user-1")
	now := time.Date(2026, 11, 13, 8, 0, 0, 0, time.UTC)
	schedules := NewBackupScheduleService(repo, func() time.Time { return now })

	schedule, err := schedules.Get("user-1")
	requireDescribed(t, schedule, err, `off 1 09:00 UTC next=none last=none result=""`)
	schedule, err = schedules.Save("user-1", BackupScheduleInput{Frequency: "weekly", Weekday: 1, Time: "09:15", TimeZone: "Europe/Madrid"})
	requireDescribed(t, schedule, err, `weekly 1 09:15 Europe/Madrid next=2026-11-16T08:15:00Z last=none result=""`)
	if _, err := schedules.Save("user-1", BackupScheduleInput{Frequency: "weekly", Weekday: 9, Time: "09:15", TimeZone: "UTC"}); !errors.Is(err, ErrBackupScheduleWeekday) {
		t.Fatalf("invalid weekday error = %v", err)
	}
	schedule, err = schedules.Save("user-1", BackupScheduleInput{Frequency: "off", Weekday: 1, Time: "09:15", TimeZone: "Europe/Madrid"})
	requireDescribed(t, schedule, err, `off 1 09:15 Europe/Madrid next=none last=none result=""`)
}

func TestBackupScheduleServiceClearsOnlyExpiredAccessFailure(t *testing.T) {
	repo := newScheduleTestRepo(t, "user-1")
	schedules := NewBackupScheduleService(repo, nil)
	if _, err := schedules.Save("user-1", BackupScheduleInput{Frequency: "daily", Time: "09:00", TimeZone: "UTC"}); err != nil {
		t.Fatal(err)
	}
	for _, result := range []string{BackupResultFailed, BackupResultGoogleAccessExpired} {
		if err := repo.RecordBackupScheduleRun("user-1", time.Now(), result); err != nil {
			t.Fatal(err)
		}
		if err := schedules.ClearGoogleAccessFailure("user-1"); err != nil {
			t.Fatal(err)
		}
		schedule, _ := schedules.Get("user-1")
		want := result
		if result == BackupResultGoogleAccessExpired {
			want = ""
		}
		if schedule.LastRunResult != want {
			t.Fatalf("after clearing %q the result is %q, want %q", result, schedule.LastRunResult, want)
		}
	}
}

func TestBackupScheduleServiceReportsStorageErrors(t *testing.T) {
	db := newDriveTestDB(t, t.TempDir())
	schedules := NewBackupScheduleService(repository.New(db), nil)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := schedules.Get("user-1"); err == nil {
		t.Error("Get should report a closed database")
	}
	if _, err := schedules.Save("user-1", BackupScheduleInput{Frequency: "daily", Time: "09:00", TimeZone: "UTC"}); err == nil {
		t.Error("Save should report a closed database")
	}
}

func TestBackupResultClassifiesFailures(t *testing.T) {
	expired := fmt.Errorf("refresh access token: %w", newGoogleAPIError("token request", 400, []byte(`{"error":"invalid_grant"}`)))
	wrongClient := newGoogleAPIError("token request", 401, []byte(`{"error":"unauthorized_client"}`))
	quota := newGoogleAPIError("drive upload", 403, []byte(`{"error":{"errors":[{"reason":"storageQuotaExceeded"}]}}`))
	for err, want := range map[error]string{
		ErrGoogleNotConfigured:                           BackupResultDriveNotConfigured,
		fmt.Errorf("wrapped: %w", ErrGoogleNotConnected): BackupResultDriveNotConnected,
		expired:                 BackupResultGoogleAccessExpired,
		wrongClient:             BackupResultGoogleAccessExpired,
		quota:                   BackupResultFailed,
		errors.New("disk full"): BackupResultFailed,
	} {
		if got := BackupResult(err); got != want {
			t.Errorf("BackupResult(%v) = %q, want %q", err, got, want)
		}
	}
	if got := BackupResult(nil); got != BackupResultOK {
		t.Errorf("BackupResult(nil) = %q", got)
	}
}

func TestDriveRecordsExpiredGoogleAccess(t *testing.T) {
	const userID = "user-1"
	dir := t.TempDir()
	db := newDriveTestDB(t, dir)
	seedDriveUser(t, db, userID)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"Token has been expired or revoked."}`))
	}))
	defer server.Close()
	drive := NewDriveBackupService(db, driveTestConfig(dir), writeExport("zip"))
	drive.tokenURL = server.URL + "/token"
	mustNoErr(t, drive.saveAccount(userID, driveAccount{RefreshToken: "rt"}))
	err := drive.BackupNow(userID)
	if BackupResult(err) != BackupResultGoogleAccessExpired {
		t.Fatalf("a revoked refresh token must be reported as expired access, got %v", err)
	}
	if status := drive.Status(userID); status.LastStatus != "reauth_required" {
		t.Fatalf("expired access must be recorded for the account menu: %+v", status)
	}
}
