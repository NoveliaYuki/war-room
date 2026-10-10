package repository

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"war-room/backend/pkg/models"
)

func TestSearchPeriodRepositoryLifecycle(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	period := &models.SearchPeriod{ID: "period-march", Name: "March", StartDate: "2026-03-01", EndDate: "2026-03-31"}
	requireSearchPeriodRepoSuccess(t, repo.CreateSearchPeriod(period))
	requirePeriodTimestamps(t, period)
	requireSearchPeriodRepoError(t, repo.CreateSearchPeriod(&models.SearchPeriod{ID: "period-overlap", Name: "Overlap", StartDate: "2026-03-31", EndDate: "2026-04-30"}), ErrSearchPeriodOverlap)
	if err := repo.UpdateSearchPeriod(period.ID, models.SearchPeriodInput{Name: "March updated", StartDate: "2026-03-01", EndDate: "2026-03-31"}); err != nil {
		t.Fatal(err)
	}
	requireSearchPeriodRepoSuccess(t, repo.CreateSearchPeriod(&models.SearchPeriod{ID: "period-april", Name: "April", StartDate: "2026-04-01", EndDate: "2026-04-30"}))
	loaded, err := repo.GetSearchPeriod(period.ID)
	requireLoadedPeriod(t, loaded, err, "March updated")
	requireSearchPeriodRepoError(t, repo.UpdateSearchPeriod(period.ID, models.SearchPeriodInput{Name: "Overlap", StartDate: "2026-03-01", EndDate: "2026-04-01"}), ErrSearchPeriodOverlap)
	requireSearchPeriodRepoError(t, repo.UpdateSearchPeriod("missing", models.SearchPeriodInput{Name: "Missing", StartDate: "2026-05-01", EndDate: "2026-05-31"}), sql.ErrNoRows)
	periods, err := repo.ListSearchPeriods()
	requirePeriodList(t, periods, err, 2)
	missing, err := repo.GetSearchPeriod("missing")
	requireLoadedPeriod(t, missing, err, "")
	requireSearchPeriodRepoSuccess(t, repo.DeleteSearchPeriod(period.ID))
	requireSearchPeriodRepoError(t, repo.DeleteSearchPeriod(period.ID), sql.ErrNoRows)
}

func TestUpdatingSearchPeriodAssignsMatchingUnassignedJobs(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	createPeriodTestJob(t, repo, "matching", "2026-03-15", nil)
	period := &models.SearchPeriod{ID: "march", Name: "March", StartDate: "2026-03-01", EndDate: "2026-03-10"}
	requireSearchPeriodRepoSuccess(t, repo.CreateSearchPeriod(period))
	if err := repo.UpdateSearchPeriod(period.ID, models.SearchPeriodInput{Name: period.Name, StartDate: "2026-03-01", EndDate: "2026-03-31"}); err != nil {
		t.Fatal(err)
	}
	var assignedID string
	if err := repo.db.QueryRow("SELECT search_period_id FROM jobs WHERE id = 'matching'").Scan(&assignedID); err != nil || assignedID != period.ID {
		t.Fatalf("assignment after extending period=%q err=%v", assignedID, err)
	}
}

func TestUpdatingSearchPeriodRollsBackWhenAssignmentFails(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	createPeriodTestJob(t, repo, "matching", "2026-03-15", nil)
	period := &models.SearchPeriod{ID: "march", Name: "March", StartDate: "2026-03-01", EndDate: "2026-03-10"}
	requireSearchPeriodRepoSuccess(t, repo.CreateSearchPeriod(period))
	if _, err := repo.db.Exec(`CREATE TRIGGER reject_period_assignment BEFORE UPDATE OF search_period_id ON jobs BEGIN SELECT RAISE(ABORT, 'no assignment'); END`); err != nil {
		t.Fatal(err)
	}
	err := repo.UpdateSearchPeriod(period.ID, models.SearchPeriodInput{Name: period.Name, StartDate: "2026-03-01", EndDate: "2026-03-31"})
	if err == nil {
		t.Fatal("assignment trigger should roll back period update")
	}
	loaded, err := repo.GetSearchPeriod(period.ID)
	if err != nil || loaded == nil || loaded.EndDate != "2026-03-10" {
		t.Fatalf("period after rollback=%+v err=%v", loaded, err)
	}
}

func TestOpenEndedPeriodAssignsMatchingUnassignedJobs(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	createPeriodTestJob(t, repo, "inside", "2026-09-20", nil)
	createPeriodTestJob(t, repo, "outside", "2025-08-31", nil)
	assignedPeriod := &models.SearchPeriod{ID: "existing-period", Name: "Earlier", StartDate: "2026-01-01", EndDate: "2026-08-31"}
	requireSearchPeriodRepoSuccess(t, repo.CreateSearchPeriod(assignedPeriod))
	createPeriodTestJob(t, repo, "already-assigned", "2026-09-21", &assignedPeriod.ID)
	ongoing := &models.SearchPeriod{ID: "ongoing-period", Name: "September 2026", StartDate: "2026-09-01"}
	requireSearchPeriodRepoSuccess(t, repo.CreateSearchPeriod(ongoing))
	for _, test := range []struct {
		jobID string
		want  string
	}{
		{jobID: "inside", want: ongoing.ID},
		{jobID: "outside", want: ""},
		{jobID: "already-assigned", want: assignedPeriod.ID},
	} {
		var actual sql.NullString
		if err := repo.db.QueryRow("SELECT search_period_id FROM jobs WHERE id = ?", test.jobID).Scan(&actual); err != nil {
			t.Fatal(err)
		}
		if actual.String != test.want {
			t.Errorf("job %q assignment=%q, want %q", test.jobID, actual.String, test.want)
		}
	}
	period, err := repo.SearchPeriodForDate("2026-11-01")
	if err != nil || period == nil || period.ID != ongoing.ID {
		t.Fatalf("ongoing period for date=%+v err=%v", period, err)
	}
	if err := repo.CreateSearchPeriod(&models.SearchPeriod{ID: "overlap", Name: "Overlap", StartDate: "2026-12-01", EndDate: "2027-01-01"}); !errors.Is(err, ErrSearchPeriodOverlap) {
		t.Fatalf("period after ongoing period error=%v, want overlap", err)
	}
}

func TestSearchPeriodCreationRollsBackWhenJobAssignmentFails(t *testing.T) {
	for _, endDate := range []string{"", "2026-09-30"} {
		t.Run(endDate, func(t *testing.T) {
			repo := newImportRepositoryFixture(t)
			createPeriodTestJob(t, repo, "inside", "2026-09-20", nil)
			if _, err := repo.db.Exec(`CREATE TRIGGER reject_period_assignment BEFORE UPDATE OF search_period_id ON jobs BEGIN SELECT RAISE(ABORT, 'no assignment'); END`); err != nil {
				t.Fatal(err)
			}
			period := &models.SearchPeriod{ID: "period", Name: "September", StartDate: "2026-09-01", EndDate: endDate}
			if err := repo.CreateSearchPeriod(period); err == nil {
				t.Fatal("assignment trigger should roll back period creation")
			}
			if loaded, err := repo.GetSearchPeriod(period.ID); err != nil || loaded != nil {
				t.Fatalf("period after rollback=%+v err=%v", loaded, err)
			}
		})
	}
}

func createPeriodTestJob(t *testing.T, repo *Repository, id, createdDate string, periodID *string) {
	t.Helper()
	job := &models.Job{ID: id, PositionTitle: "Engineer", Status: models.StatusWaiting, SalaryType: models.SalaryUnknown,
		SalaryCurrency: "EUR", RecruiterType: models.RecruiterNone, AvatarSeed: "seed", SearchPeriodID: periodID}
	if err := repo.InsertJob(job); err != nil {
		t.Fatal(err)
	}
	created, err := time.ParseInLocation("2006-01-02", createdDate, time.Local)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.Exec("UPDATE jobs SET created_at = ? WHERE id = ?", created.Unix(), id); err != nil {
		t.Fatal(err)
	}
}

func requirePeriodTimestamps(t *testing.T, period *models.SearchPeriod) {
	t.Helper()
	if period.CreatedAt == 0 || period.UpdatedAt == 0 {
		t.Fatalf("create timestamps missing: %+v", period)
	}
}

func requireLoadedPeriod(t *testing.T, period *models.SearchPeriod, err error, name string) {
	t.Helper()
	if err != nil || (name == "" && period != nil) || (name != "" && (period == nil || period.Name != name)) {
		t.Fatalf("loaded period=%+v err=%v, want name %q", period, err, name)
	}
}

func requirePeriodList(t *testing.T, periods []models.SearchPeriod, err error, count int) {
	t.Helper()
	if err != nil || len(periods) != count || periods[0].JobCount != 0 {
		t.Fatalf("period list=%+v err=%v", periods, err)
	}
}

func requireSearchPeriodRepoError(t *testing.T, err, expected error) {
	t.Helper()
	if !errors.Is(err, expected) {
		t.Fatalf("error=%v, want %v", err, expected)
	}
}

func requireSearchPeriodRepoSuccess(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
