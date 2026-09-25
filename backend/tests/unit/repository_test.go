package backend_test

import (
	"testing"
	"war-room/backend/pkg/config"
	"war-room/backend/pkg/database"
	"war-room/backend/pkg/models"
	"war-room/backend/pkg/repository"
)

func setupTestRepo(t *testing.T) *repository.Repository {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{DBPath: dir + "/test.db"}
	db, err := database.InitDB(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return repository.New(db)
}

func TestRepositoryJobs(t *testing.T) {
	repo := setupTestRepo(t)

	job := &models.Job{
		ID:            "test-id",
		CompanyName:   "Test Co",
		PositionTitle: "Tester",
		Status:        "ongoing",
		SalaryType:    "unknown",
		RecruiterType: "none",
	}

	err := repo.InsertJob(job)
	if err != nil {
		t.Fatalf("InsertJob failed: %v", err)
	}

	jobs, err := repo.GetAllJobs("", "")
	if err != nil || len(jobs) != 1 {
		t.Errorf("Expected 1 job, got %d", len(jobs))
	}

	err = repo.DeleteJob(job.ID)
	if err != nil {
		t.Errorf("DeleteJob failed: %v", err)
	}
}
