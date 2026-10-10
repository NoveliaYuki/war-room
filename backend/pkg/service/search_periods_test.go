package service

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"war-room/backend/pkg/models"
)

func TestSearchPeriodLifecycleAndJobAssignment(t *testing.T) {
	fixture := newAttachmentOwnerFixture(t)
	today := time.Now().Format("2006-01-02")
	createStages := false
	period, err := fixture.service.CreateSearchPeriod(models.SearchPeriodInput{Name: "Current search", StartDate: today})
	if err != nil {
		t.Fatalf("create search period: %v", err)
	}
	automatic, err := fixture.service.CreateJob(models.CreateJobInput{PositionTitle: "Automatic", CreateDefaultStages: &createStages})
	if err != nil || automatic.SearchPeriodID == nil || *automatic.SearchPeriodID != period.ID {
		t.Fatalf("automatically assigned job=%+v err=%v", automatic, err)
	}
	if err := fixture.service.DeleteSearchPeriod(period.ID); err != nil {
		t.Fatalf("delete search period: %v", err)
	}
	stored, err := fixture.repo.GetJobByID(automatic.ID)
	if err != nil || stored.SearchPeriodID != nil {
		t.Fatalf("deleted period assignment=%v err=%v", stored.SearchPeriodID, err)
	}
	periods, err := fixture.service.ListSearchPeriods()
	if err != nil || len(periods) != 0 {
		t.Fatalf("periods=%+v err=%v", periods, err)
	}
}

func TestSearchPeriodCreationWithoutActivePeriod(t *testing.T) {
	fixture := newAttachmentOwnerFixture(t)
	createStages := false
	job, err := fixture.service.CreateJob(models.CreateJobInput{PositionTitle: "No active period", CreateDefaultStages: &createStages})
	if err != nil || job.SearchPeriodID != nil {
		t.Fatalf("job without an active period=%+v err=%v", job, err)
	}
}

func TestNewJobWithEmptyPeriodUsesCurrentPeriodAndCanBeCleared(t *testing.T) {
	fixture := newAttachmentOwnerFixture(t)
	today := time.Now().Format("2006-01-02")
	period, err := fixture.service.CreateSearchPeriod(models.SearchPeriodInput{Name: "Current search", StartDate: today})
	if err != nil {
		t.Fatal(err)
	}
	noPeriod := ""
	createStages := false
	created, err := fixture.service.CreateJob(models.CreateJobInput{PositionTitle: "Automatically assigned", SearchPeriodID: &noPeriod, CreateDefaultStages: &createStages})
	if err != nil || created.SearchPeriodID == nil || *created.SearchPeriodID != period.ID {
		t.Fatalf("new job with empty period=%+v err=%v", created, err)
	}
	if _, err := fixture.service.UpdateJob(created.ID, models.UpdateJobInput{SearchPeriodID: &noPeriod}); err != nil {
		t.Fatalf("clear existing job assignment: %v", err)
	}
	cleared, err := fixture.repo.GetJobByID(created.ID)
	if err != nil || cleared.SearchPeriodID != nil {
		t.Fatalf("cleared existing job assignment=%v err=%v", cleared.SearchPeriodID, err)
	}
	if _, err := fixture.service.UpdateJob(created.ID, models.UpdateJobInput{SearchPeriodID: &period.ID}); err != nil {
		t.Fatalf("assign existing job: %v", err)
	}
}

func TestSearchPeriodRejectsInvalidDatesAndOverlaps(t *testing.T) {
	fixture := newAttachmentOwnerFixture(t)
	first, err := fixture.service.CreateSearchPeriod(models.SearchPeriodInput{Name: "First", StartDate: "2026-03-01", EndDate: "2026-03-31"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.CreateSearchPeriod(models.SearchPeriodInput{Name: "Second", StartDate: "2026-04-01", EndDate: "2026-04-30"}); err != nil {
		t.Fatal(err)
	}
	ongoing, err := fixture.service.CreateSearchPeriod(models.SearchPeriodInput{Name: "Ongoing", StartDate: "2026-05-01"})
	if err != nil || ongoing.EndDate != "" {
		t.Fatalf("open-ended period=%+v err=%v", ongoing, err)
	}
	for _, input := range []models.SearchPeriodInput{
		{Name: "Invalid calendar", StartDate: "2026-02-30", EndDate: "2026-03-01"},
		{Name: "Reversed", StartDate: "2026-04-01", EndDate: "2026-03-31"},
		{Name: "Overlap", StartDate: "2026-03-31", EndDate: "2026-04-10"},
		{Name: "After ongoing period", StartDate: "2026-06-01", EndDate: "2026-06-30"},
	} {
		if _, err := fixture.service.CreateSearchPeriod(input); !errors.Is(err, ErrInvalidField) {
			t.Errorf("create period %+v error=%v, want ErrInvalidField", input, err)
		}
	}
	if err := fixture.service.UpdateSearchPeriod(first.ID, models.SearchPeriodInput{Name: "First", StartDate: "2026-03-01", EndDate: "2026-04-01"}); !errors.Is(err, ErrInvalidField) {
		t.Fatalf("overlap update error=%v, want ErrInvalidField", err)
	}
	if err := fixture.service.UpdateSearchPeriod("missing", models.SearchPeriodInput{Name: "Missing", StartDate: "2026-05-01", EndDate: "2026-05-31"}); err == nil {
		t.Fatal("updating missing period should fail")
	}
}

func TestSearchPeriodArchiveRoundTrip(t *testing.T) {
	source := newAttachmentOwnerFixture(t)
	period, job := createSearchPeriodArchiveRecords(t, source)
	var archive bytes.Buffer
	if err := source.service.ExportArchive(&archive); err != nil {
		t.Fatalf("export backup: %v", err)
	}
	target := newAttachmentOwnerFixture(t)
	if err := target.service.ImportArchive(bytes.NewReader(archive.Bytes()), int64(archive.Len()), true); err != nil {
		t.Fatalf("import backup: %v", err)
	}
	periods, err := target.service.ListSearchPeriods()
	if err != nil || len(periods) != 1 || periods[0].Name != "March 2026" {
		t.Fatalf("imported periods=%+v err=%v", periods, err)
	}
	imported, err := target.repo.GetJobByID(job.ID)
	if err != nil || imported.SearchPeriodID == nil || *imported.SearchPeriodID != period.ID {
		t.Fatalf("imported job=%+v err=%v", imported, err)
	}
}

func createSearchPeriodArchiveRecords(t *testing.T, fixture attachmentOwnerFixture) (*models.SearchPeriod, *models.Job) {
	t.Helper()
	period, err := fixture.service.CreateSearchPeriod(models.SearchPeriodInput{Name: "March 2026", StartDate: "2026-03-01", EndDate: "2026-03-31"})
	if err != nil {
		t.Fatal(err)
	}
	createStages := false
	job, err := fixture.service.CreateJob(models.CreateJobInput{PositionTitle: "Archived job", SearchPeriodID: &period.ID, CreateDefaultStages: &createStages})
	if err != nil {
		t.Fatal(err)
	}
	return period, job
}

func TestSearchPeriodDateLookupAndAssignmentErrors(t *testing.T) {
	fixture := newAttachmentOwnerFixture(t)
	period, err := fixture.service.CreateSearchPeriod(models.SearchPeriodInput{Name: "March", StartDate: "2026-03-01", EndDate: "2026-03-31"})
	if err != nil {
		t.Fatal(err)
	}
	found, err := fixture.repo.SearchPeriodForDate("2026-03-10")
	if err != nil || found == nil || found.ID != period.ID {
		t.Fatalf("period lookup=%+v err=%v", found, err)
	}
	missing, err := fixture.repo.SearchPeriodForDate("2026-04-01")
	if err != nil || missing != nil {
		t.Fatalf("outside date lookup=%+v err=%v", missing, err)
	}
	invalid := "missing-period"
	createStages := false
	if _, err := fixture.service.CreateJob(models.CreateJobInput{PositionTitle: "Invalid assignment", SearchPeriodID: &invalid, CreateDefaultStages: &createStages}); !errors.Is(err, ErrInvalidField) {
		t.Fatalf("invalid period assignment error=%v", err)
	}
	if err := fixture.service.DeleteSearchPeriod("missing-period"); err == nil {
		t.Fatal("deleting a missing period should fail")
	}
}
