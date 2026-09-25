package backend_test

import (
	"database/sql"
	"testing"

	"war-room/backend/pkg/models"
	"war-room/backend/pkg/repository"
)

// TestReorderRejectsInvalidIDsWithoutPartialUpdates checks scoped and atomic ordering.
func TestReorderRejectsInvalidIDsWithoutPartialUpdates(t *testing.T) {
	_, _, repo, _, db := newCoverageDB(t)
	insertReorderJob(t, repo, "job-a")
	insertReorderJob(t, repo, "job-b")
	insertReorderStage(t, repo, "stage-a", "job-a")
	insertReorderStage(t, repo, "stage-b", "job-b")
	insertReorderQuestion(t, repo, "question-a", "stage-a")
	insertReorderQuestion(t, repo, "question-b", "stage-b")

	if err := repo.ReorderJobs([]string{"job-b", "missing-job"}); err == nil {
		t.Fatal("expected unknown job to reject the reorder")
	}
	if err := repo.ReorderJobs([]string{"job-b", "job-b"}); err == nil {
		t.Fatal("expected duplicate job to reject the reorder")
	}
	if err := repo.ReorderStages("job-a", []string{"stage-a", "stage-b"}); err == nil {
		t.Fatal("expected out-of-scope stage to reject the reorder")
	}
	if err := repo.ReorderQuestions("stage-a", []string{"question-a", "question-b"}); err == nil {
		t.Fatal("expected out-of-scope question to reject the reorder")
	}
	assertStoredOrder(t, db, "jobs", "id", "job-b", 1)
	assertStoredOrder(t, db, "stages", "id", "stage-b", 0)
	assertStoredOrder(t, db, "stage_questions", "id", "question-b", 0)
}

func insertReorderJob(t *testing.T, repo *repository.Repository, id string) {
	t.Helper()
	job := &models.Job{ID: id, CompanyName: "Example", PositionTitle: id, Status: models.StatusOngoing,
		SalaryType: models.SalaryUnknown, SalaryCurrency: "EUR", RecruiterType: models.RecruiterNone, AvatarSeed: id}
	if err := repo.InsertJob(job); err != nil {
		t.Fatalf("insert job %q: %v", id, err)
	}
}

func insertReorderStage(t *testing.T, repo *repository.Repository, id, jobID string) {
	t.Helper()
	stage := &models.Stage{ID: id, JobID: jobID, StageType: models.StageHR, Status: models.StageStatusPending,
		MeetingType: "video", RecruiterType: models.RecruiterNone}
	if err := repo.InsertStage(stage); err != nil {
		t.Fatalf("insert stage %q: %v", id, err)
	}
}

func insertReorderQuestion(t *testing.T, repo *repository.Repository, id, stageID string) {
	t.Helper()
	question := &models.Question{ID: id, StageID: stageID, Question: "Question"}
	if err := repo.InsertQuestion(question); err != nil {
		t.Fatalf("insert question %q: %v", id, err)
	}
}

func assertStoredOrder(t *testing.T, db *sql.DB, table, key, id string, expected int) {
	t.Helper()
	var order int
	query := reorderOrderQuery(t, table, key)
	if err := db.QueryRow(query, id).Scan(&order); err != nil {
		t.Fatalf("read %s order for %q: %v", table, id, err)
	}
	if order != expected {
		t.Fatalf("%s order for %q = %d, want %d", table, id, order, expected)
	}
}

func reorderOrderQuery(t *testing.T, table, key string) string {
	t.Helper()
	switch {
	case table == "jobs" && key == "id":
		return "SELECT order_index FROM jobs WHERE id = ?"
	case table == "stages" && key == "id":
		return "SELECT order_index FROM stages WHERE id = ?"
	case table == "stage_questions" && key == "id":
		return "SELECT order_index FROM stage_questions WHERE id = ?"
	default:
		t.Fatalf("unsupported reorder assertion table/key %q/%q", table, key)
		return ""
	}
}
