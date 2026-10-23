package service

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"war-room/backend/pkg/models"
)

func TestInactiveWaitingSchedulerChecksOnStartup(t *testing.T) {
	fixture := newAttachmentOwnerFixture(t)
	job := &models.Job{ID: "scheduler-startup", CompanyName: "Example", PositionTitle: "Engineer", Status: models.StatusWaiting,
		SalaryType: models.SalaryUnknown, SalaryCurrency: "EUR", RecruiterType: models.RecruiterNone}
	if err := fixture.repo.InsertJob(job); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if _, err := fixture.db.Exec(`UPDATE jobs SET last_activity_at = ? WHERE id = ?`, now.Add(-30*24*time.Hour).Unix(), job.ID); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	startInactiveWaitingScheduler(ctx, fixture.repo, func() time.Time { return now }, time.Hour)
	stored, err := fixture.repo.GetJobByID(job.ID)
	if err != nil || stored.Status != models.StatusRejected {
		t.Fatalf("startup sweep status=%q err=%v, want rejected", stored.Status, err)
	}
}

func TestInactiveWaitingSchedulerTriggersBackupAfterAutomaticRejection(t *testing.T) {
	fixture := newAttachmentOwnerFixture(t)
	job := &models.Job{ID: "scheduler-backup", CompanyName: "Example", PositionTitle: "Engineer", Status: models.StatusWaiting,
		SalaryType: models.SalaryUnknown, SalaryCurrency: "EUR", RecruiterType: models.RecruiterNone}
	if err := fixture.repo.InsertJob(job); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	if _, err := fixture.db.Exec(`UPDATE jobs SET last_activity_at = ? WHERE id = ?`, now.Add(-30*24*time.Hour).Unix(), job.ID); err != nil {
		t.Fatal(err)
	}
	calls := 0
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	StartInactiveWaitingSchedulerWithCallback(ctx, fixture.repo, time.Hour, func() { calls++ })
	if calls != 1 {
		t.Fatalf("automatic status change backup calls=%d, want 1", calls)
	}
}

func TestInactiveWaitingSchedulerPublicEntry(t *testing.T) {
	fixture := newAttachmentOwnerFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	StartInactiveWaitingScheduler(ctx, fixture.repo, time.Hour)
}

func TestInactiveWaitingSchedulerRepeatsAfterInterval(t *testing.T) {
	fixture := newAttachmentOwnerFixture(t)
	job := &models.Job{ID: "scheduler-tick", CompanyName: "Example", PositionTitle: "Engineer", Status: models.StatusWaiting,
		SalaryType: models.SalaryUnknown, SalaryCurrency: "EUR", RecruiterType: models.RecruiterNone}
	if err := fixture.repo.InsertJob(job); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	if _, err := fixture.db.Exec(`UPDATE jobs SET last_activity_at = ? WHERE id = ?`, now.Add(-30*24*time.Hour+time.Second).Unix(), job.ID); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	nowFn := func() time.Time {
		if calls.Add(1) == 1 {
			return now
		}
		return now.Add(2 * time.Second)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	startInactiveWaitingScheduler(ctx, fixture.repo, nowFn, 10*time.Millisecond)
	deadline := time.After(time.Second)
	for {
		stored, err := fixture.repo.GetJobByID(job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if stored.Status == models.StatusRejected {
			if calls.Load() < 2 {
				t.Fatal("job was rejected before the scheduled repeat")
			}
			return
		}
		select {
		case <-deadline:
			t.Fatal("scheduled sweep did not reject job")
		case <-time.After(5 * time.Millisecond):
		}
	}
}
