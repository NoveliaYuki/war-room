package repository

import (
	"database/sql"
	"errors"
	"testing"

	_ "modernc.org/sqlite"
	"war-room/backend/pkg/models"
)

type rowsResult struct {
	rows int64
	err  error
}

func TestMarshalInterviewersPreservesEmptyArrays(t *testing.T) {
	for _, interviewers := range [][]models.Interviewer{nil, {}} {
		encoded, err := marshalInterviewers(interviewers)
		if err != nil || encoded != "[]" {
			t.Fatalf("marshal empty interviewers=%q err=%v", encoded, err)
		}
	}
	encoded, err := marshalInterviewers([]models.Interviewer{{Name: "Alex", Role: "Recruiter"}})
	if err != nil || encoded != `[{"name":"Alex","role":"Recruiter"}]` {
		t.Fatalf("marshal interviewer=%q err=%v", encoded, err)
	}
}

func (result rowsResult) LastInsertId() (int64, error) { return 0, nil }
func (result rowsResult) RowsAffected() (int64, error) { return result.rows, result.err }

func TestRequireOneUpdatedRow(t *testing.T) {
	if err := requireOneUpdatedRow(rowsResult{rows: 1}, "job", "one"); err != nil {
		t.Fatalf("one affected row: %v", err)
	}
	if err := requireOneUpdatedRow(rowsResult{}, "job", "missing"); err == nil {
		t.Fatal("zero affected rows should fail")
	}
	if err := requireOneUpdatedRow(rowsResult{rows: 2}, "job", "many"); err == nil {
		t.Fatal("multiple affected rows should fail")
	}
	rowsError := errors.New("rows unavailable")
	if err := requireOneUpdatedRow(rowsResult{err: rowsError}, "job", "error"); !errors.Is(err, rowsError) {
		t.Fatalf("rows affected error=%v", err)
	}
}

func TestNormalizeJobUpdateValue(t *testing.T) {
	longText := make([]byte, 101)
	for index := range longText {
		longText[index] = 'x'
	}
	if got := normalizeJobUpdateValue("keyword_note", string(longText)); got != string(longText[:100]) {
		t.Fatalf("long keyword length=%d", len(got.(string)))
	}
	for _, test := range []struct {
		key   string
		value interface{}
	}{
		{key: "keyword_note", value: "short"},
		{key: "keyword_note_other", value: string(longText)},
		{key: "keyword_note", value: 123},
	} {
		if got := normalizeJobUpdateValue(test.key, test.value); got != test.value {
			t.Errorf("normalize %q=%#v, want %#v", test.key, got, test.value)
		}
	}
}

func TestWithReadSnapshotHandlesCallbackAndCommit(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	repo := New(db)
	if err := repo.WithReadSnapshot(func(snapshot *Repository) error {
		if snapshot == repo || snapshot.reader == nil {
			t.Fatal("callback should receive a transaction-backed snapshot")
		}
		return nil
	}); err != nil {
		t.Fatalf("commit read snapshot: %v", err)
	}
	callbackErr := errors.New("read failed")
	if err := repo.WithReadSnapshot(func(*Repository) error { return callbackErr }); !errors.Is(err, callbackErr) {
		t.Fatalf("callback error=%v", err)
	}
}

func TestRepositoryMethodsReportClosedDatabaseErrors(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	repo := New(db)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	stageID, jobID := "stage", "job"
	checks := []struct {
		name string
		call func() error
	}{
		{"with snapshot", func() error { return repo.WithReadSnapshot(func(*Repository) error { return nil }) }},
		{"all jobs", func() error { _, err := repo.GetAllJobs("", ""); return err }},
		{"job", func() error { _, err := repo.GetJobByID(jobID); return err }},
		{"job counts", func() error { _, err := repo.GetJobCounts(); return err }},
		{"insert job", func() error { return repo.InsertJob(&models.Job{ID: jobID}) }},
		{"update job", func() error { return repo.UpdateJob(jobID, map[string]interface{}{"company_name": "Example"}) }},
		{"delete job", func() error { return repo.DeleteJob(jobID) }},
		{"reorder jobs", func() error { return repo.ReorderJobs([]string{jobID}) }},
		{"job stages", func() error { _, err := repo.GetStagesByJobID(jobID); return err }},
		{"all stages", func() error { _, err := repo.GetAllStages(); return err }},
		{"stage", func() error { _, err := repo.GetStageByID(stageID); return err }},
		{"insert stage", func() error { return repo.InsertStage(&models.Stage{ID: stageID, JobID: jobID}) }},
		{"update stage", func() error { return repo.UpdateStage(stageID, map[string]interface{}{"notes": "note"}) }},
		{"delete stage", func() error { return repo.DeleteStage(stageID) }},
		{"reorder stages", func() error { return repo.ReorderStages(jobID, []string{stageID}) }},
		{"set current stage", func() error { _, err := repo.SetCurrentStage(stageID); return err }},
		{"stage questions", func() error { _, err := repo.GetQuestionsByStageID(stageID); return err }},
		{"all questions", func() error { _, err := repo.GetAllQuestions(); return err }},
		{"question", func() error { _, err := repo.GetQuestionByID("question"); return err }},
		{"insert question", func() error { return repo.InsertQuestion(&models.Question{ID: "question", StageID: stageID}) }},
		{"update question", func() error { return repo.UpdateQuestion("question", map[string]interface{}{"question": "Q"}) }},
		{"delete question", func() error { return repo.DeleteQuestion("question") }},
		{"reorder questions", func() error { return repo.ReorderQuestions(stageID, []string{"question"}) }},
		{"job attachments", func() error { _, err := repo.GetAttachmentsByJobID(jobID); return err }},
		{"all attachments", func() error { _, err := repo.GetAllAttachments(); return err }},
		{"attachment", func() error { _, err := repo.GetAttachmentByID("attachment"); return err }},
		{"insert attachment", func() error { return repo.InsertAttachment(&models.Attachment{ID: "attachment", JobID: jobID}) }},
		{"delete attachment", func() error { return repo.DeleteAttachment("attachment") }},
		{"scheduled meetings", func() error { _, err := repo.GetScheduledMeetings(); return err }},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if err := check.call(); err == nil {
				t.Fatal("closed database should return an error")
			}
		})
	}
}
