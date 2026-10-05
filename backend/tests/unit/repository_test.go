package backend_test

import (
	"database/sql"
	"errors"
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

func TestTechnologyRepositoryCRUDAndAssignmentValidation(t *testing.T) {
	repo := setupTestRepo(t)
	item := createTechnologyRepository(t, repo)
	job := createTechnologyJob(t, repo)
	assertInvalidTechnologyAssignments(t, repo, job.ID, item.ID)
	assertTechnologyAssignments(t, repo, job.ID, item.ID)
	assertJobListIncludesTechnologies(t, repo)
	assertTechnologyUpdateRollback(t, repo, item.ID)
	assertTechnologyRepositoryRemoval(t, repo, item.ID)
}

func assertJobListIncludesTechnologies(t *testing.T, repo *repository.Repository) {
	t.Helper()
	jobs, err := repo.GetAllJobs(string(models.StatusOngoing), "")
	if err != nil || len(jobs) != 1 || len(jobs[0].Technologies) != 1 || jobs[0].Technologies[0].Name != "Kubernetes" {
		t.Fatalf("job list technologies=%+v err=%v", jobs, err)
	}
}

func TestJobTechnologyReadFailuresAreReturned(t *testing.T) {
	for _, operation := range []string{"list", "details"} {
		t.Run(operation, func(t *testing.T) {
			dir := t.TempDir()
			db, err := database.InitDB(&config.Config{DBPath: dir + "/test.db"})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })
			repo := repository.New(db)
			if err := repo.CreateTechnology(models.Technology{ID: "tech-kubernetes", Name: "Kubernetes"}); err != nil {
				t.Fatal(err)
			}
			job := createTechnologyJob(t, repo)
			// The missing relation simulates a damaged or incomplete database schema.
			if _, err := db.Exec("DROP TABLE job_technologies"); err != nil {
				t.Fatal(err)
			}
			if operation == "list" {
				if _, err := repo.GetAllJobs(string(models.StatusOngoing), ""); err == nil {
					t.Fatal("job list should report missing technology assignments table")
				}
				return
			}
			if _, err := repo.GetJobByID(job.ID); err == nil {
				t.Fatal("job details should report missing technology assignments table")
			}
		})
	}
}

func createTechnologyJob(t *testing.T, repo *repository.Repository) *models.Job {
	t.Helper()
	job := &models.Job{ID: "technology-job", CompanyName: "Example", PositionTitle: "Engineer", Status: models.StatusOngoing, SalaryType: models.SalaryUnknown, RecruiterType: models.RecruiterNone}
	if err := repo.InsertJob(job); err != nil {
		t.Fatal(err)
	}
	return job
}

func assertTechnologyAssignments(t *testing.T, repo *repository.Repository, jobID, technologyID string) {
	t.Helper()
	if err := repo.SetJobTechnologies("missing-job", []string{technologyID}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing job assignment error=%v", err)
	}
	if err := repo.SetJobTechnologies(jobID, []string{technologyID}); err != nil {
		t.Fatalf("assign technology: %v", err)
	}
	assigned, err := repo.JobTechnologies(jobID)
	if err != nil || len(assigned) != 1 || assigned[0].Name != "Kubernetes" {
		t.Fatalf("job technologies=%+v err=%v", assigned, err)
	}
	assertTechnologyUsageCatalog(t, repo)
}

func assertTechnologyUsageCatalog(t *testing.T, repo *repository.Repository) {
	t.Helper()
	catalog, err := repo.ListTechnologies()
	if err != nil || len(catalog) != 1 || catalog[0].Aliases[0] != "Kube" || len(catalog[0].Jobs) != 1 {
		t.Fatalf("technology catalog=%+v err=%v", catalog, err)
	}
}

func assertTechnologyUpdateRollback(t *testing.T, repo *repository.Repository, id string) {
	t.Helper()
	if err := repo.CreateTechnology(models.Technology{ID: "tech-second", Name: "Go"}); err != nil {
		t.Fatalf("create second technology: %v", err)
	}
	if err := repo.UpdateTechnology(models.Technology{ID: id, Name: "Kubernetes", Aliases: []string{"Kube", "Kube"}}); err == nil {
		t.Fatal("duplicate alias should fail")
	}
	catalog, err := repo.ListTechnologies()
	var aliases []string
	for _, item := range catalog {
		if item.ID == id {
			aliases = item.Aliases
		}
	}
	if err != nil || len(aliases) != 1 || aliases[0] != "Kube" {
		t.Fatalf("failed update did not roll back aliases: catalog=%+v err=%v", catalog, err)
	}
}

func assertTechnologyRepositoryRemoval(t *testing.T, repo *repository.Repository, id string) {
	t.Helper()
	if err := repo.DeleteTechnology(id); !errors.Is(err, repository.ErrTechnologyInUse) {
		t.Fatalf("delete assigned technology error=%v", err)
	}
	removed, err := repo.RemoveTechnologyAssignments(id)
	if err != nil || removed != 1 {
		t.Fatalf("removed assignments=%d err=%v", removed, err)
	}
	removed, err = repo.RemoveTechnologyAssignments(id)
	if err != nil || removed != 0 {
		t.Fatalf("second assignment removal=%d err=%v", removed, err)
	}
	if err := repo.DeleteTechnology(id); err != nil {
		t.Fatalf("delete unused technology: %v", err)
	}
	if err := repo.DeleteTechnology("missing"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing technology delete error=%v", err)
	}
}

func createTechnologyRepository(t *testing.T, repo *repository.Repository) models.Technology {
	t.Helper()
	item := models.Technology{ID: "tech-kubernetes", Name: "Kubernetes", Aliases: []string{"K8s"}}
	if err := repo.CreateTechnology(item); err != nil {
		t.Fatalf("create technology: %v", err)
	}
	if err := repo.CreateTechnology(models.Technology{ID: "tech-duplicate", Name: "Kubernetes"}); err == nil {
		t.Fatal("duplicate canonical technology should fail")
	}
	if err := repo.UpdateTechnology(models.Technology{ID: item.ID, Name: "Kubernetes", Aliases: []string{"Kube"}}); err != nil {
		t.Fatalf("update technology: %v", err)
	}
	if err := repo.UpdateTechnology(models.Technology{ID: "missing", Name: "Missing"}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing technology update error=%v", err)
	}
	return item
}

func TestTechnologyRepositoryListReportsMissingRelationTables(t *testing.T) {
	for _, table := range []string{"technology_aliases", "job_technologies"} {
		t.Run(table, func(t *testing.T) {
			dir := t.TempDir()
			cfg := &config.Config{DBPath: dir + "/test.db"}
			db, err := database.InitDB(cfg)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })
			if _, err := db.Exec("DROP TABLE " + table); err != nil {
				t.Fatal(err)
			}
			if _, err := repository.New(db).ListTechnologies(); err == nil {
				t.Fatalf("missing %s should fail catalog listing", table)
			}
		})
	}
}

func TestTechnologyRepositoryReportsClosedDatabaseErrors(t *testing.T) {
	dir := t.TempDir()
	db, err := database.InitDB(&config.Config{DBPath: dir + "/test.db"})
	if err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ListTechnologies(); err == nil {
		t.Error("list should report a closed database")
	}
	if err := repo.CreateTechnology(models.Technology{ID: "one", Name: "One"}); err == nil {
		t.Error("create should report a closed database")
	}
	if err := repo.UpdateTechnology(models.Technology{ID: "one", Name: "One"}); err == nil {
		t.Error("update should report a closed database")
	}
	if err := repo.DeleteTechnology("one"); err == nil {
		t.Error("delete should report a closed database")
	}
	if _, err := repo.RemoveTechnologyAssignments("one"); err == nil {
		t.Error("assignment removal should report a closed database")
	}
	if err := repo.SetJobTechnologies("job", nil); err == nil {
		t.Error("assignment update should report a closed database")
	}
	if _, err := repo.JobTechnologies("job"); err == nil {
		t.Error("job stack should report a closed database")
	}
}

func TestTechnologyCatalogImportRejectsInvalidReferences(t *testing.T) {
	repo := setupTestRepo(t)
	for _, test := range []struct {
		name         string
		technologies []models.Technology
		jobs         []models.Job
	}{
		{name: "duplicate IDs", technologies: []models.Technology{{ID: "same", Name: "One"}, {ID: "same", Name: "Two"}}},
		{name: "duplicate aliases", technologies: []models.Technology{{ID: "one", Name: "One", Aliases: []string{"same"}}, {ID: "two", Name: "Two", Aliases: []string{"same"}}}},
		{name: "unknown assignment", technologies: []models.Technology{{ID: "one", Name: "One"}}, jobs: []models.Job{{ID: "job", CompanyName: "Example", PositionTitle: "Role", Status: models.StatusOngoing, AvatarSeed: "seed", Technologies: []models.Technology{{ID: "missing"}}}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := repo.ReplaceAllWithTechnologyCatalog(test.jobs, test.technologies, nil, false, 1); err == nil {
				t.Fatal("invalid technology import should fail")
			}
		})
	}
}

func TestTechnologyCatalogImportReportsStorageFailures(t *testing.T) {
	for _, test := range []struct {
		name, table string
		replaceCV   bool
	}{
		{name: "technology table", table: "technologies"},
		{name: "assignment table", table: "job_technologies"},
		{name: "CV table", table: "cv_versions", replaceCV: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			db, err := database.InitDB(&config.Config{DBPath: dir + "/test.db"})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })
			if _, err := db.Exec("DROP TABLE " + test.table); err != nil {
				t.Fatal(err)
			}
			if err := repository.New(db).ReplaceAllWithTechnologyCatalog(nil, nil, nil, test.replaceCV, 1); err == nil {
				t.Fatal("missing storage table should fail import")
			}
		})
	}
}

func assertInvalidTechnologyAssignments(t *testing.T, repo *repository.Repository, jobID, technologyID string) {
	t.Helper()
	for _, ids := range [][]string{{"unknown"}, {technologyID, technologyID}, {" "}} {
		if err := repo.SetJobTechnologies(jobID, ids); err == nil {
			t.Errorf("invalid technology assignment %v should fail", ids)
		}
	}
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
