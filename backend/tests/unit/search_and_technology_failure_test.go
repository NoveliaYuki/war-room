package backend_test

import (
	"testing"

	"war-room/backend/pkg/models"
)

// TestSearchPeriodDatabaseFailuresPropagate exercises the repository's
// hard-to-trigger error-return branches via the existing dropFailureTable
// fault-injection pattern, matching TestServicesPropagateDatabaseFailures'
// coverage of jobs/stages/questions/attachments.
func TestSearchPeriodDatabaseFailuresPropagate(t *testing.T) {
	_, jobs, _, _, db := newCoverageDB(t)
	period, err := jobs.CreateSearchPeriod(testUserID, models.SearchPeriodInput{Name: "Search", StartDate: "2026-01-01", EndDate: "2026-02-01"})
	if err != nil {
		t.Fatal(err)
	}
	dropFailureTable(t, db, "search_periods")
	if _, err := jobs.ListSearchPeriods(testUserID); err == nil {
		t.Fatal("expected search period list failure")
	}
	if _, err := jobs.CreateSearchPeriod(testUserID, models.SearchPeriodInput{Name: "Another", StartDate: "2026-03-01", EndDate: "2026-04-01"}); err == nil {
		t.Fatal("expected search period insert failure")
	}
	if err := jobs.UpdateSearchPeriod(testUserID, period.ID, models.SearchPeriodInput{Name: "Changed", StartDate: "2026-01-01", EndDate: "2026-02-01"}); err == nil {
		t.Fatal("expected search period update failure")
	}
	if err := jobs.DeleteSearchPeriod(testUserID, period.ID); err == nil {
		t.Fatal("expected search period delete failure")
	}
}

// TestTechnologyDatabaseFailuresPropagate mirrors
// TestSearchPeriodDatabaseFailuresPropagate for the technology catalog.
func TestTechnologyDatabaseFailuresPropagate(t *testing.T) {
	_, jobs, _, _, db := newCoverageDB(t)
	tech, err := jobs.CreateTechnology(testUserID, "Go", nil)
	if err != nil {
		t.Fatal(err)
	}
	dropFailureTable(t, db, "technologies")
	if _, err := jobs.ListTechnologies(testUserID); err == nil {
		t.Fatal("expected technology list failure")
	}
	if _, err := jobs.CreateTechnology(testUserID, "Rust", nil); err == nil {
		t.Fatal("expected technology insert failure")
	}
	if _, err := jobs.UpdateTechnology(testUserID, tech.ID, "Golang", nil); err == nil {
		t.Fatal("expected technology update failure")
	}
	if err := jobs.DeleteTechnology(testUserID, tech.ID); err == nil {
		t.Fatal("expected technology delete failure")
	}
}

func TestCreateJobSurfacesCVLookupFailure(t *testing.T) {
	_, jobs, _, _, db := newCoverageDB(t)
	dropFailureTable(t, db, "cv_versions")
	if _, err := jobs.CreateJob(testUserID, models.CreateJobInput{CompanyName: "Example", PositionTitle: "Engineer"}); err == nil {
		t.Fatal("expected CreateJob to surface a CV lookup failure")
	}
}

func TestCreateJobSurfacesInsertFailure(t *testing.T) {
	_, jobs, _, _, db := newCoverageDB(t)
	dropFailureTable(t, db, "jobs")
	if _, err := jobs.CreateJob(testUserID, models.CreateJobInput{CompanyName: "Example", PositionTitle: "Engineer"}); err == nil {
		t.Fatal("expected CreateJob to surface an insert failure")
	}
}
