package backend_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"war-room/backend/pkg/repository"
	"war-room/backend/pkg/service"
)

// TestAPI_BackupScheduleRunReportsResult saves a schedule through the
// production middleware chain, runs the scheduler against the same database
// with a fake clock and a fake Drive upload, and reads the result back.
func TestAPI_BackupScheduleRunReportsResult(t *testing.T) {
	stack := newIntegrationStack(t)
	defer stack.cleanup()
	client := stack.server.Client()
	url := stack.server.URL + "/api/google/backup-schedule"

	response := mustRequest(t, client, http.MethodGet, url, "")
	requireStatus(t, response, http.StatusOK)
	var schedule service.BackupSchedule
	mustDecode(t, response, &schedule)
	_ = response.Body.Close()
	if schedule.Frequency != service.BackupFrequencyOff || schedule.NextRunAt != nil {
		t.Fatalf("a new account must start with backups off, got %+v", schedule)
	}

	response = mustRequest(t, client, http.MethodPut, url, `{"frequency":"daily","weekday":1,"time":"09:00","time_zone":"Europe/Madrid"}`)
	requireStatus(t, response, http.StatusOK)
	mustDecode(t, response, &schedule)
	_ = response.Body.Close()
	if schedule.NextRunAt == nil {
		t.Fatalf("saved daily schedule has no next run: %+v", schedule)
	}

	response = mustRequest(t, client, http.MethodPut, url, `{"frequency":"0 9 * * *","weekday":1,"time":"09:00","time_zone":"Europe/Madrid"}`)
	requireStatus(t, response, http.StatusBadRequest)
	_ = response.Body.Close()

	runAt := schedule.NextRunAt.Add(30 * time.Second)
	uploads := 0
	scheduler := service.NewBackupScheduler(repository.New(stack.db), func(string) error {
		uploads++
		return &service.GoogleAPIError{Operation: "token request", Status: 400, Reason: "invalid_grant"}
	}, func() time.Time { return runAt })
	if started := scheduler.RunDue(context.Background()); started != 1 {
		t.Fatalf("started %d scheduled backups, want 1", started)
	}
	scheduler.Wait()
	if uploads != 1 {
		t.Fatalf("scheduled backup ran %d times", uploads)
	}

	response = mustRequest(t, client, http.MethodGet, url, "")
	requireStatus(t, response, http.StatusOK)
	mustDecode(t, response, &schedule)
	_ = response.Body.Close()
	if schedule.LastRunResult != service.BackupResultGoogleAccessExpired || schedule.LastRunAt == nil || !schedule.NextRunAt.After(runAt) {
		t.Fatalf("the failed scheduled run must be reported, got %+v", schedule)
	}
}

// TestAPI_BackupScheduleSkipsWithoutGoogle runs a due schedule through the
// production DriveBackupService on an installation without Google
// configured: it records the skip and never reaches Google.
func TestAPI_BackupScheduleSkipsWithoutGoogle(t *testing.T) {
	stack := newIntegrationStack(t)
	defer stack.cleanup()
	client := stack.server.Client()
	url := stack.server.URL + "/api/google/backup-schedule"
	response := mustRequest(t, client, http.MethodPut, url, `{"frequency":"weekly","weekday":3,"time":"18:45","time_zone":"America/New_York"}`)
	requireStatus(t, response, http.StatusOK)
	var schedule service.BackupSchedule
	mustDecode(t, response, &schedule)
	_ = response.Body.Close()

	drive := service.NewDriveBackupService(stack.db, stack.cfg, nil)
	runAt := schedule.NextRunAt.Add(time.Minute)
	scheduler := service.NewBackupScheduler(repository.New(stack.db), drive.BackupNow, func() time.Time { return runAt })
	if started := scheduler.RunDue(context.Background()); started != 1 {
		t.Fatalf("started %d, want 1", started)
	}
	scheduler.Wait()
	response = mustRequest(t, client, http.MethodGet, url, "")
	mustDecode(t, response, &schedule)
	_ = response.Body.Close()
	if schedule.LastRunResult != service.BackupResultDriveNotConfigured {
		t.Fatalf("an installation without Google must record a skipped run, got %q", schedule.LastRunResult)
	}
	newYork, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	if weekday := schedule.NextRunAt.In(newYork).Weekday(); weekday != time.Wednesday {
		t.Fatalf("next weekly run falls on %v, want Wednesday", weekday)
	}
}
