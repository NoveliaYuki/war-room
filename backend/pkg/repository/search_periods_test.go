package repository

import (
	"database/sql"
	"errors"
	"testing"

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
	periods, err := repo.ListSearchPeriods()
	requirePeriodList(t, periods, err, 2)
	missing, err := repo.GetSearchPeriod("missing")
	requireLoadedPeriod(t, missing, err, "")
	requireSearchPeriodRepoSuccess(t, repo.DeleteSearchPeriod(period.ID))
	requireSearchPeriodRepoError(t, repo.DeleteSearchPeriod(period.ID), sql.ErrNoRows)
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
