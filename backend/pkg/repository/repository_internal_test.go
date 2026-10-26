package repository

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	_ "modernc.org/sqlite"
	"path/filepath"
	"war-room/backend/pkg/config"
	"war-room/backend/pkg/database"
	"war-room/backend/pkg/models"
)

const testUserID = "test-user"

type rowsResult struct {
	rows int64
	err  error
}

func TestImportedJobActivityTimestampUsesAvailableTimestamp(t *testing.T) {
	if got := importedJobActivityTimestamp(models.Job{LastActivityAt: 10, UpdatedAt: 20}); got != 10 {
		t.Fatalf("stored activity timestamp=%d, want 10", got)
	}
	if got := importedJobActivityTimestamp(models.Job{UpdatedAt: 20, CreatedAt: 30, StatusChangedAt: 25}); got != 30 {
		t.Fatalf("legacy activity timestamp=%d, want 30", got)
	}
	if got := importedJobActivityTimestamp(models.Job{}); got < time.Now().Unix()-2 {
		t.Fatalf("empty activity timestamp=%d, want current time", got)
	}
}

func TestArchiveImportPreservesActivityAfterRestoringChildRecords(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	job := models.Job{ID: "activity-restore", CompanyName: "Example", PositionTitle: "Engineer", Status: models.StatusOngoing,
		SalaryType: models.SalaryUnknown, RecruiterType: models.RecruiterNone, CreatedAt: 100, UpdatedAt: 200, LastActivityAt: 300,
		Stages: []models.Stage{{ID: "activity-stage", StageType: "HR", Status: models.StageStatusCurrent,
			Questions: []models.Question{{ID: "activity-question", Question: "Example question"}}}}}
	if err := repo.ReplaceAllJobs(testUserID, []models.Job{job}); err != nil {
		t.Fatal(err)
	}
	loaded, err := repo.GetJobByID(testUserID, job.ID)
	if err != nil || loaded.LastActivityAt != job.LastActivityAt || loaded.UpdatedAt != job.UpdatedAt {
		t.Fatalf("import changed activity timestamps: err=%v", err)
	}
}

func TestActivityRestorationFailureRollsBackArchiveImport(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	insertRepositoryTestJob(t, repo, "kept-job")
	if _, err := repo.db.Exec(`CREATE TRIGGER reject_activity_restore BEFORE UPDATE OF last_activity_at ON jobs WHEN NEW.last_activity_at = 300 BEGIN SELECT RAISE(ABORT, 'rejected activity restoration'); END`); err != nil {
		t.Fatal(err)
	}
	job := models.Job{ID: "incoming-job", CompanyName: "Example", PositionTitle: "Engineer", Status: models.StatusOngoing,
		SalaryType: models.SalaryUnknown, RecruiterType: models.RecruiterNone, LastActivityAt: 300}
	if err := repo.ReplaceAllJobs(testUserID, []models.Job{job}); err == nil {
		t.Fatal("activity restoration failure must abort import")
	}
	jobs, err := repo.GetAllJobs(testUserID, "", "")
	if err != nil || len(jobs) != 1 || jobs[0].ID != "kept-job" {
		t.Fatalf("failed activity restore replaced saved records: count=%d err=%v", len(jobs), err)
	}
}

func TestReplaceAllJobsWithCVRestoresGappedSequenceAndSelection(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	versions := []models.CVVersion{{ID: "cv-1", Version: 1, OriginalName: "old.pdf", StoredFilename: "hash-1", FileSize: 3, SHA256: "hash-1"},
		{ID: "cv-3", Version: 3, OriginalName: "new.pdf", StoredFilename: "hash-3", FileSize: 3, SHA256: "hash-3"}}
	job := &models.Job{ID: "job-new", CompanyName: "Example", PositionTitle: "Engineer", Status: models.StatusOngoing,
		SalaryType: models.SalaryUnknown, SalaryCurrency: "EUR", RecruiterType: models.RecruiterNone,
		SelectedCVVersion: &versions[1]}
	if err := repo.ReplaceAllJobsWithCV(testUserID, []models.Job{*job}, versions, true, 7); err != nil {
		t.Fatal(err)
	}
	loaded, err := repo.GetJobByID(testUserID, "job-new")
	if err != nil || loaded.SelectedCVVersion == nil || loaded.SelectedCVVersion.ID != "cv-3" {
		t.Fatalf("job=%+v err=%v", loaded, err)
	}
	next, err := repo.GetNextCVVersion(testUserID)
	if err != nil || next != 7 {
		t.Fatalf("next version=%d err=%v", next, err)
	}
}

func TestReplaceAllJobsPreservesHistoryAndRejectsInvalidHistory(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	job := models.Job{ID: "job-history", CompanyName: "Example", PositionTitle: "Engineer", Status: models.StatusOngoing,
		SalaryType: models.SalaryUnknown, RecruiterType: models.RecruiterNone,
		History: []models.JobHistory{{OccurredAt: 100, Action: "created"}, {OccurredAt: 200, Action: "moved to ongoing"}}}
	assertImportedJobHistory(t, repo, job)
	assertInvalidHistoryRollback(t, repo, job)
}

func TestHistoryReadFailuresAreReturnedForListsAndDetails(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	insertRepositoryTestJob(t, repo, "history-read-error")
	if _, err := repo.db.Exec(`DROP TABLE job_history`); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetAllJobs(testUserID, "", ""); err == nil {
		t.Fatal("job list ignored unavailable history")
	}
	if _, err := repo.GetJobByID(testUserID, "history-read-error"); err == nil {
		t.Fatal("job details ignored unavailable history")
	}
}

func TestJobHistoryReadRejectsMalformedTimestamp(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	insertRepositoryTestJob(t, repo, "malformed-history-job")
	if _, err := repo.db.Exec(`INSERT INTO job_history (job_id, occurred_at, action) VALUES ('malformed-history-job', 'invalid', 'created')`); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetJobByID(testUserID, "malformed-history-job"); err == nil {
		t.Fatal("job detail read accepted a malformed history timestamp")
	}
}

func assertImportedJobHistory(t *testing.T, repo *Repository, job models.Job) {
	t.Helper()
	if err := repo.ReplaceAllJobs(testUserID, []models.Job{job}); err != nil {
		t.Fatalf("import history: %v", err)
	}
	loaded, err := repo.GetJobByID(testUserID, job.ID)
	if err != nil || len(loaded.History) != 2 || loaded.History[0] != job.History[0] || loaded.History[1] != job.History[1] {
		t.Fatalf("imported history=%+v err=%v", loaded, err)
	}
	listed, err := repo.GetAllJobs(testUserID, "", "")
	if err != nil || len(listed) != 1 || len(listed[0].History) != 2 {
		t.Fatalf("listed history=%+v err=%v", listed, err)
	}
}

func assertInvalidHistoryRollback(t *testing.T, repo *Repository, job models.Job) {
	t.Helper()
	for _, history := range []models.JobHistory{{OccurredAt: 0, Action: "created"}, {OccurredAt: 100, Action: ""}} {
		job.History = []models.JobHistory{history}
		if err := repo.ReplaceAllJobs(testUserID, []models.Job{job}); err == nil {
			t.Fatalf("invalid history %+v was imported", history)
		}
		loaded, err := repo.GetJobByID(testUserID, job.ID)
		if err != nil || len(loaded.History) != 2 {
			t.Fatalf("invalid import did not preserve original history: %+v err=%v", loaded, err)
		}
	}
}

func TestCVRepositoryVersionLifecycle(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	insertRepositoryTestJob(t, repo, "job-cv")
	version := &models.CVVersion{ID: "cv-1", OriginalName: "resume.pdf", StoredFilename: "hash", FileSize: 5, MimeType: "application/pdf", SHA256: "hash", UploadedAt: 42}
	assertCVInsertAndRead(t, repo, version)
	assertCVAssignmentAndDelete(t, repo, version.ID)
}

func TestRejectInactiveWaitingJobsUsesActivityTimestamp(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	old := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	now := old.Add(31 * 24 * time.Hour)
	insertWaitingActivityJob(t, repo, "stale")
	insertWaitingActivityJob(t, repo, "recent")
	insertWaitingActivityJob(t, repo, "ongoing")
	setJobActivity(t, repo, "stale", old.Unix())
	setJobActivity(t, repo, "recent", now.Add(-29*24*time.Hour).Unix())
	mustUpdateJobStatus(t, repo, "ongoing", models.StatusOngoing)
	count, err := repo.RejectInactiveWaitingJobs(testUserID, now)
	if err != nil || count != 1 {
		t.Fatalf("rejected=%d err=%v, want one stale waiting job", count, err)
	}
	assertJobStatus(t, repo, "stale", models.StatusRejected)
	assertJobActivityEquals(t, repo, "stale", old.Unix())
	assertJobStatus(t, repo, "recent", models.StatusWaiting)
	assertJobStatus(t, repo, "ongoing", models.StatusOngoing)
	mustUpdateJobStatus(t, repo, "recent", models.StatusRejected)
	assertJobActivity(t, repo, "recent", now.Unix()-1)
}

func TestRejectInactiveWaitingJobsReturnsDatabaseError(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	if err := repo.db.Close(); err != nil {
		t.Fatal(err)
	}
	if count, err := repo.RejectInactiveWaitingJobs(testUserID, time.Now()); err == nil || count != 0 {
		t.Fatalf("rejected=%d err=%v, want database error", count, err)
	}
}

func insertWaitingActivityJob(t *testing.T, repo *Repository, id string) {
	t.Helper()
	job := &models.Job{ID: id, CompanyName: "Example", PositionTitle: "Engineer", Status: models.StatusWaiting,
		SalaryType: models.SalaryUnknown, SalaryCurrency: "EUR", RecruiterType: models.RecruiterNone}
	if err := repo.InsertJob(testUserID, job); err != nil {
		t.Fatal(err)
	}
}

func setJobActivity(t *testing.T, repo *Repository, id string, timestamp int64) {
	t.Helper()
	if _, err := repo.db.Exec(`UPDATE jobs SET last_activity_at = ? WHERE id = ?`, timestamp, id); err != nil {
		t.Fatal(err)
	}
}

func mustUpdateJobStatus(t *testing.T, repo *Repository, id string, status models.JobStatus) {
	t.Helper()
	if err := repo.UpdateJob(testUserID, id, map[string]interface{}{"status": string(status)}); err != nil {
		t.Fatal(err)
	}
}

func assertJobStatus(t *testing.T, repo *Repository, id string, status models.JobStatus) {
	t.Helper()
	job, err := repo.GetJobByID(testUserID, id)
	if err != nil || job.Status != status {
		t.Fatalf("job %s status=%q err=%v, want %q", id, job.Status, err, status)
	}
}

func assertJobActivity(t *testing.T, repo *Repository, id string, minimum int64) {
	t.Helper()
	job, err := repo.GetJobByID(testUserID, id)
	if err != nil || job.LastActivityAt <= minimum {
		t.Fatalf("job %s activity=%d err=%v, want greater than %d", id, job.LastActivityAt, err, minimum)
	}
}

func assertJobActivityEquals(t *testing.T, repo *Repository, id string, want int64) {
	t.Helper()
	job, err := repo.GetJobByID(testUserID, id)
	if err != nil || job.LastActivityAt != want {
		t.Fatalf("job %s activity=%d err=%v, want %d", id, job.LastActivityAt, err, want)
	}
}

func TestRelatedSavedChangesResetJobActivity(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	job := &models.Job{ID: "activity-job", CompanyName: "Example", PositionTitle: "Engineer", Status: models.StatusWaiting,
		SalaryType: models.SalaryUnknown, SalaryCurrency: "EUR", RecruiterType: models.RecruiterNone}
	if err := repo.InsertJob(testUserID, job); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-31 * 24 * time.Hour).Unix()
	setJobActivity(t, repo, job.ID, old)
	stage := &models.Stage{ID: "activity-stage", JobID: job.ID, StageType: models.StageHR, Status: models.StageStatusPending,
		MeetingType: "video", RecruiterType: models.RecruiterNone}
	if err := repo.InsertStage(stage); err != nil {
		t.Fatal(err)
	}
	stored, err := repo.GetJobByID(testUserID, job.ID)
	if err != nil || stored.LastActivityAt <= old {
		t.Fatalf("stage insert did not reset activity: timestamp=%d err=%v", stored.LastActivityAt, err)
	}
	if err := repo.UpdateStage(testUserID, stage.ID, map[string]interface{}{"order_index": 1}); err != nil {
		t.Fatal(err)
	}
	setJobActivity(t, repo, job.ID, old)
	if err := repo.ReorderStages(testUserID, job.ID, []string{stage.ID}); err != nil {
		t.Fatal(err)
	}
	stored, err = repo.GetJobByID(testUserID, job.ID)
	if err != nil || stored.LastActivityAt <= old {
		t.Fatalf("stage reorder did not reset activity: timestamp=%d err=%v", stored.LastActivityAt, err)
	}
}

func TestJobActivityTriggersCoverSavedCardAndRelatedChanges(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	job := &models.Job{ID: "activity-all", CompanyName: "Example", PositionTitle: "Engineer", Status: models.StatusWaiting,
		SalaryType: models.SalaryUnknown, SalaryCurrency: "EUR", RecruiterType: models.RecruiterNone}
	other := &models.Job{ID: "activity-other", CompanyName: "Example", PositionTitle: "Designer", Status: models.StatusWaiting,
		SalaryType: models.SalaryUnknown, SalaryCurrency: "EUR", RecruiterType: models.RecruiterNone}
	insertActivityJob(t, repo, job)
	insertActivityJob(t, repo, other)
	stageA := &models.Stage{ID: "activity-stage-a", JobID: job.ID, OrderIndex: 0, StageType: models.StageHR,
		Status: models.StageStatusPending, MeetingType: "video", RecruiterType: models.RecruiterNone}
	stageB := &models.Stage{ID: "activity-stage-b", JobID: job.ID, OrderIndex: 1, StageType: models.StageTechnical,
		Status: models.StageStatusPending, MeetingType: "video", RecruiterType: models.RecruiterNone}
	insertActivityStage(t, repo, stageA)
	insertActivityStage(t, repo, stageB)
	questionA := &models.Question{ID: "activity-question-a", StageID: stageA.ID, Question: "Question A"}
	questionB := &models.Question{ID: "activity-question-b", StageID: stageA.ID, Question: "Question B"}
	insertActivityQuestion(t, repo, questionA)
	insertActivityQuestion(t, repo, questionB)
	createActivityTechnology(t, repo)

	actions := []struct {
		name string
		run  func() error
	}{
		{name: "job edit", run: func() error {
			return repo.UpdateJob(testUserID, job.ID, map[string]interface{}{"description": "Updated role"})
		}},
		{name: "job status", run: func() error {
			return repo.UpdateJob(testUserID, job.ID, map[string]interface{}{"status": string(models.StatusOngoing)})
		}},
		{name: "job reorder", run: func() error { return repo.ReorderJobs(testUserID, []string{other.ID, job.ID}) }},
		{name: "stage reorder", run: func() error { return repo.ReorderStages(testUserID, job.ID, []string{stageB.ID, stageA.ID}) }},
		{name: "stage edit", run: func() error {
			return repo.UpdateStage(testUserID, stageA.ID, map[string]interface{}{"description": "Updated stage"})
		}},
		{name: "question reorder", run: func() error {
			return repo.ReorderQuestions(testUserID, stageA.ID, []string{questionB.ID, questionA.ID})
		}},
		{name: "question insert", run: func() error {
			return repo.InsertQuestion(&models.Question{ID: "activity-question-c", StageID: stageA.ID, Question: "Question C"})
		}},
		{name: "question edit", run: func() error {
			return repo.UpdateQuestion(testUserID, questionA.ID, map[string]interface{}{"answer_notes": "Prepared"})
		}},
		{name: "technology assignment", run: func() error { return repo.SetJobTechnologies(testUserID, job.ID, []string{"activity-tech"}) }},
		{name: "attachment insert", run: func() error {
			return repo.InsertAttachment(&models.Attachment{ID: "activity-attachment", JobID: job.ID,
				OriginalName: "notes.txt", StoredFilename: "activity-attachment.txt", FileSize: 1, MimeType: "text/plain"})
		}},
		{name: "attachment edit", run: func() error {
			_, err := repo.db.Exec(`UPDATE attachments SET original_name = 'updated.txt' WHERE id = 'activity-attachment'`)
			return err
		}},
		{name: "attachment delete", run: func() error { return repo.DeleteAttachment(testUserID, "activity-attachment") }},
		{name: "question delete", run: func() error { return repo.DeleteQuestion(testUserID, questionA.ID) }},
		{name: "stage delete", run: func() error { return repo.DeleteStage(testUserID, stageB.ID) }},
	}
	for _, action := range actions {
		t.Run(action.name, func(t *testing.T) {
			assertActivityAction(t, repo, job.ID, action.run)
		})
	}
}

func insertActivityJob(t *testing.T, repo *Repository, job *models.Job) {
	t.Helper()
	if err := repo.InsertJob(testUserID, job); err != nil {
		t.Fatal(err)
	}
}

func insertActivityStage(t *testing.T, repo *Repository, stage *models.Stage) {
	t.Helper()
	if err := repo.InsertStage(stage); err != nil {
		t.Fatal(err)
	}
}

func insertActivityQuestion(t *testing.T, repo *Repository, question *models.Question) {
	t.Helper()
	if err := repo.InsertQuestion(question); err != nil {
		t.Fatal(err)
	}
}

func createActivityTechnology(t *testing.T, repo *Repository) {
	t.Helper()
	if err := repo.CreateTechnology(testUserID, models.Technology{ID: "activity-tech", Name: "Go"}); err != nil {
		t.Fatal(err)
	}
}

func assertActivityAction(t *testing.T, repo *Repository, jobID string, run func() error) {
	t.Helper()
	setJobActivity(t, repo, jobID, 1)
	if err := run(); err != nil {
		t.Fatal(err)
	}
	assertJobActivity(t, repo, jobID, 1)
}

func TestInsertFirstStageAndSetJobOngoing(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	job := &models.Job{ID: "job-first-stage", CompanyName: "Example", PositionTitle: "Engineer", Status: models.StatusWaiting,
		SalaryType: models.SalaryUnknown, SalaryCurrency: "EUR", RecruiterType: models.RecruiterNone, AvatarSeed: "seed"}
	if err := repo.InsertJob(testUserID, job); err != nil {
		t.Fatal(err)
	}
	stage := &models.Stage{ID: "stage-first", JobID: "job-first-stage", StageType: models.StageHR, Status: models.StageStatusCurrent,
		MeetingType: "video", RecruiterType: models.RecruiterNone}
	if err := repo.InsertFirstStageAndSetJobOngoing(stage); err != nil {
		t.Fatalf("insert first stage: %v", err)
	}
	job, err := repo.GetJobByID(testUserID, stage.JobID)
	if err != nil || job.Status != models.StatusOngoing {
		t.Fatalf("job status=%q err=%v", job.Status, err)
	}
	stored, err := repo.GetStageByID(testUserID, stage.ID)
	if err != nil || stored.InterviewersJSON != "null" {
		t.Fatalf("stage interviewers=%q err=%v", stored.InterviewersJSON, err)
	}
}

func TestInsertFirstStageRollsBackWhenJobUpdateFails(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	job := &models.Job{ID: "job-stage-rollback", CompanyName: "Example", PositionTitle: "Engineer", Status: models.StatusWaiting,
		SalaryType: models.SalaryUnknown, SalaryCurrency: "EUR", RecruiterType: models.RecruiterNone, AvatarSeed: "seed"}
	if err := repo.InsertJob(testUserID, job); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.Exec(`CREATE TRIGGER fail_job_start BEFORE UPDATE OF status ON jobs WHEN NEW.status = 'ongoing' BEGIN SELECT RAISE(ABORT, 'injected job update failure'); END`); err != nil {
		t.Fatal(err)
	}
	stage := &models.Stage{ID: "stage-rollback", JobID: job.ID, StageType: models.StageHR, Status: models.StageStatusCurrent,
		MeetingType: "video", RecruiterType: models.RecruiterNone}
	if err := repo.InsertFirstStageAndSetJobOngoing(stage); err == nil {
		t.Fatal("job update failure should be returned")
	}
	if stored, err := repo.GetStageByID(testUserID, stage.ID); err != nil || stored != nil {
		t.Fatalf("rolled back stage=%+v err=%v", stored, err)
	}
	storedJob, err := repo.GetJobByID(testUserID, job.ID)
	if err != nil || storedJob.Status != models.StatusWaiting {
		t.Fatalf("job status=%q err=%v", storedJob.Status, err)
	}
}

func TestInsertFirstStageRollsBackWhenInsertFails(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	job := &models.Job{ID: "job-insert-rollback", CompanyName: "Example", PositionTitle: "Engineer", Status: models.StatusWaiting,
		SalaryType: models.SalaryUnknown, SalaryCurrency: "EUR", RecruiterType: models.RecruiterNone, AvatarSeed: "seed"}
	if err := repo.InsertJob(testUserID, job); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.Exec(`CREATE TRIGGER fail_stage_insert BEFORE INSERT ON stages BEGIN SELECT RAISE(ABORT, 'injected stage insert failure'); END`); err != nil {
		t.Fatal(err)
	}
	stage := &models.Stage{ID: "stage-insert-rollback", JobID: job.ID, StageType: models.StageHR, Status: models.StageStatusCurrent,
		MeetingType: "video", RecruiterType: models.RecruiterNone}
	if err := repo.InsertFirstStageAndSetJobOngoing(stage); err == nil {
		t.Fatal("stage insert failure should be returned")
	}
	if stored, err := repo.GetStageByID(testUserID, stage.ID); err != nil || stored != nil {
		t.Fatalf("rolled back stage=%+v err=%v", stored, err)
	}
}

func TestInsertFirstStageRollsBackWhenJobUpdateAffectsNoRows(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	job := &models.Job{ID: "job-ignore-rollback", CompanyName: "Example", PositionTitle: "Engineer", Status: models.StatusWaiting,
		SalaryType: models.SalaryUnknown, SalaryCurrency: "EUR", RecruiterType: models.RecruiterNone, AvatarSeed: "seed"}
	if err := repo.InsertJob(testUserID, job); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.Exec(`CREATE TRIGGER ignore_job_start BEFORE UPDATE OF status ON jobs WHEN NEW.status = 'ongoing' BEGIN SELECT RAISE(IGNORE); END`); err != nil {
		t.Fatal(err)
	}
	stage := &models.Stage{ID: "stage-ignore-rollback", JobID: job.ID, StageType: models.StageHR, Status: models.StageStatusCurrent,
		MeetingType: "video", RecruiterType: models.RecruiterNone}
	if err := repo.InsertFirstStageAndSetJobOngoing(stage); err == nil {
		t.Fatal("zero-row job update should be rejected")
	}
	if stored, err := repo.GetStageByID(testUserID, stage.ID); err != nil || stored != nil {
		t.Fatalf("rolled back stage=%+v err=%v", stored, err)
	}
}

func TestCreateTechnologyAliasFailureRollsBack(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	if _, err := repo.db.Exec(`CREATE TRIGGER fail_alias BEFORE INSERT ON technology_aliases BEGIN SELECT RAISE(ABORT, 'injected failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateTechnology(testUserID, models.Technology{ID: "failed-create", Name: "Failed", Aliases: []string{"alias"}}); err == nil {
		t.Fatal("create should surface alias storage failure")
	}
	assertTechnologyNotStored(t, repo, "failed-create")
}

func TestUpdateTechnologyAliasFailureRollsBack(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	if err := repo.CreateTechnology(testUserID, models.Technology{ID: "existing", Name: "Original", Aliases: []string{"old"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.Exec(`CREATE TRIGGER fail_alias BEFORE INSERT ON technology_aliases BEGIN SELECT RAISE(ABORT, 'injected failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateTechnology(testUserID, models.Technology{ID: "existing", Name: "Changed", Aliases: []string{"new"}}); err == nil {
		t.Fatal("update should surface alias storage failure")
	}
	var name string
	var alias string
	if err := repo.db.QueryRow(`SELECT name FROM technologies WHERE id='existing'`).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if err := repo.db.QueryRow(`SELECT alias FROM technology_aliases WHERE technology_id='existing'`).Scan(&alias); err != nil {
		t.Fatal(err)
	}
	if name != "Original" || alias != "old" {
		t.Fatalf("failed update left name=%q alias=%q", name, alias)
	}
}

func TestUpdateTechnologyAliasDeletionFailureRollsBack(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	if err := repo.CreateTechnology(testUserID, models.Technology{ID: "existing", Name: "Original", Aliases: []string{"old"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.Exec(`CREATE TRIGGER fail_alias_delete BEFORE DELETE ON technology_aliases BEGIN SELECT RAISE(ABORT, 'injected failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateTechnology(testUserID, models.Technology{ID: "existing", Name: "Changed", Aliases: []string{"new"}}); err == nil {
		t.Fatal("update should surface alias deletion failure")
	}
	var name, alias string
	if err := repo.db.QueryRow(`SELECT name FROM technologies WHERE id='existing'`).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if err := repo.db.QueryRow(`SELECT alias FROM technology_aliases WHERE technology_id='existing'`).Scan(&alias); err != nil {
		t.Fatal(err)
	}
	if name != "Original" || alias != "old" {
		t.Fatalf("failed update left name=%q alias=%q", name, alias)
	}
}

func assertTechnologyNotStored(t *testing.T, repo *Repository, id string) {
	t.Helper()
	var count int
	if err := repo.db.QueryRow(`SELECT COUNT(*) FROM technologies WHERE id=?`, id).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed create left %d catalog entries: %v", count, err)
	}
}

func TestTechnologyMutationsReportMissingStorage(t *testing.T) {
	for _, operation := range []string{"create", "update", "delete", "remove assignments"} {
		t.Run(operation, func(t *testing.T) {
			repo := newImportRepositoryFixture(t)
			if _, err := repo.db.Exec(`DROP TABLE technologies`); err != nil {
				t.Fatal(err)
			}
			var err error
			switch operation {
			case "create":
				err = repo.CreateTechnology(testUserID, models.Technology{ID: "one", Name: "One"})
			case "update":
				err = repo.UpdateTechnology(testUserID, models.Technology{ID: "one", Name: "One"})
			case "delete":
				err = repo.DeleteTechnology(testUserID, "one")
			default:
				_, err = repo.RemoveTechnologyAssignments(testUserID, "one")
			}
			if err == nil {
				t.Fatal("missing catalog storage should return an error")
			}
		})
	}
}

func TestTechnologyAssignmentMutationFailuresPreserveExistingStack(t *testing.T) {
	for _, test := range []struct {
		name, trigger string
	}{
		{name: "delete", trigger: `CREATE TRIGGER fail_assignment_delete BEFORE DELETE ON job_technologies BEGIN SELECT RAISE(ABORT, 'injected failure'); END`},
		{name: "insert", trigger: `CREATE TRIGGER fail_assignment_insert BEFORE INSERT ON job_technologies BEGIN SELECT RAISE(ABORT, 'injected failure'); END`},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := newImportRepositoryFixture(t)
			insertRepositoryTestJob(t, repo, "stack-job")
			if err := repo.CreateTechnology(testUserID, models.Technology{ID: "old-tech", Name: "Old"}); err != nil {
				t.Fatal(err)
			}
			if err := repo.CreateTechnology(testUserID, models.Technology{ID: "new-tech", Name: "New"}); err != nil {
				t.Fatal(err)
			}
			if err := repo.SetJobTechnologies(testUserID, "stack-job", []string{"old-tech"}); err != nil {
				t.Fatal(err)
			}
			if _, err := repo.db.Exec(test.trigger); err != nil {
				t.Fatal(err)
			}
			if err := repo.SetJobTechnologies(testUserID, "stack-job", []string{"new-tech"}); err == nil {
				t.Fatal("injected assignment failure should be returned")
			}
			assigned, err := repo.JobTechnologies("stack-job")
			if err != nil || len(assigned) != 1 || assigned[0].ID != "old-tech" {
				t.Fatalf("failed assignment changed stack=%+v err=%v", assigned, err)
			}
		})
	}
}

func TestTechnologyCatalogSurfacesMalformedStoredRows(t *testing.T) {
	for _, test := range []struct {
		name, setup string
	}{
		{name: "catalog name", setup: `PRAGMA foreign_keys=OFF; DROP TABLE technologies; CREATE TABLE technologies (id TEXT, user_id TEXT, name TEXT, normalized_name TEXT); INSERT INTO technologies VALUES ('one', '` + testUserID + `', NULL, NULL);`},
		{name: "alias", setup: `PRAGMA foreign_keys=OFF; DROP TABLE technology_aliases; CREATE TABLE technology_aliases (technology_id TEXT, user_id TEXT, alias TEXT, normalized_alias TEXT); INSERT INTO technology_aliases VALUES ('one', '` + testUserID + `', NULL, NULL);`},
		{name: "assigned job", setup: `PRAGMA foreign_keys=OFF; DROP TABLE jobs; CREATE TABLE jobs (id TEXT, company_name TEXT, position_title TEXT, last_activity_at INTEGER); INSERT INTO jobs VALUES ('job', NULL, 'Engineer', 0); INSERT INTO job_technologies VALUES ('job', 'one', '` + testUserID + `');`},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := newImportRepositoryFixture(t)
			repo.db.SetMaxOpenConns(1)
			if _, err := repo.db.Exec(test.setup); err != nil {
				t.Fatal(err)
			}
			if _, err := repo.ListTechnologies(testUserID); err == nil {
				t.Fatal("malformed catalog row should fail listing")
			}
		})
	}
}

func TestJobTechnologiesRejectsMalformedStoredName(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	repo.db.SetMaxOpenConns(1)
	insertRepositoryTestJob(t, repo, "stack-job")
	if err := repo.CreateTechnology(testUserID, models.Technology{ID: "one", Name: "One"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetJobTechnologies(testUserID, "stack-job", []string{"one"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.Exec(`PRAGMA foreign_keys=OFF; DROP TABLE technologies; CREATE TABLE technologies (id TEXT, user_id TEXT, name TEXT, normalized_name TEXT); INSERT INTO technologies VALUES ('one', '` + testUserID + `', NULL, NULL);`); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.JobTechnologies("stack-job"); err == nil {
		t.Fatal("NULL technology name should fail job stack loading")
	}
}

func TestTechnologyAssignmentValidationReportsStorageFailure(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	insertRepositoryTestJob(t, repo, "stack-job")
	if _, err := repo.db.Exec(`DROP TABLE technologies`); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetJobTechnologies(testUserID, "stack-job", []string{"tech"}); err == nil {
		t.Fatal("missing catalog table should fail technology validation")
	}
}

func assertCVInsertAndRead(t *testing.T, repo *Repository, version *models.CVVersion) {
	t.Helper()
	if err := repo.InsertCVVersion(testUserID, version); err != nil || version.Version != 1 {
		t.Fatalf("insert version=%+v err=%v", version, err)
	}
	loaded, err := repo.GetCVVersionByID(testUserID, version.ID)
	if err != nil || loaded == nil || loaded.Version != 1 {
		t.Fatalf("loaded version=%+v err=%v", loaded, err)
	}
	if missing, err := repo.GetCVVersionByID(testUserID, "missing"); err != nil || missing != nil {
		t.Fatalf("missing version=%+v err=%v", missing, err)
	}
	versions, err := repo.ListCVVersions(testUserID)
	if err != nil || len(versions) != 1 {
		t.Fatalf("versions=%+v err=%v", versions, err)
	}
}

func assertCVAssignmentAndDelete(t *testing.T, repo *Repository, versionID string) {
	t.Helper()
	if err := repo.AssignCVVersion(testUserID, "job-cv", &versionID); err != nil {
		t.Fatal(err)
	}
	var refs int
	if err := repo.CountCVBlobReferences("hash", &refs); err != nil || refs != 1 {
		t.Fatalf("references=%d err=%v", refs, err)
	}
	if err := repo.DeleteCVVersion(testUserID, versionID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("assigned delete err=%v", err)
	}
	if err := repo.AssignCVVersion(testUserID, "job-cv", nil); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteCVVersion(testUserID, versionID); err != nil {
		t.Fatal(err)
	}
	if err := repo.AssignCVVersion(testUserID, "missing", nil); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing job assignment err=%v", err)
	}
}

func TestMigrateLegacyCVAttachmentsIsIdempotent(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	insertRepositoryTestJob(t, repo, "legacy-job")
	version := &models.CVVersion{ID: "legacy-cv", OriginalName: "resume.pdf", StoredFilename: "hash", FileSize: 4, MimeType: "application/pdf", SHA256: "hash", UploadedAt: 15}
	created, err := repo.MigrateLegacyCVAttachments(version, nil, []string{"legacy-job"})
	if err != nil || !created {
		t.Fatalf("migration created=%t err=%v", created, err)
	}
	created, err = repo.MigrateLegacyCVAttachments(version, nil, []string{"legacy-job"})
	if err != nil || created {
		t.Fatalf("repeat migration created=%t err=%v", created, err)
	}
	job, err := repo.GetJobByID(testUserID, "legacy-job")
	if err != nil || job.SelectedCVVersion == nil || job.SelectedCVVersion.ID != version.ID {
		t.Fatalf("migrated job=%+v err=%v", job, err)
	}
}

func insertRepositoryTestJob(t *testing.T, repo *Repository, id string) {
	t.Helper()
	if err := repo.InsertJob(testUserID, &models.Job{ID: id, CompanyName: "Example", PositionTitle: "Engineer", Status: models.StatusOngoing,
		SalaryType: models.SalaryUnknown, SalaryCurrency: "EUR", RecruiterType: models.RecruiterNone}); err != nil {
		t.Fatal(err)
	}
}

func newImportRepositoryFixture(t *testing.T) *Repository {
	t.Helper()
	dir := t.TempDir()
	db, err := database.InitDB(&config.Config{DataDir: dir, DBPath: filepath.Join(dir, "import.sqlite"), BackupPath: filepath.Join(dir, "backup.json"),
		AttachmentsDir: filepath.Join(dir, "attachments"), CVDir: filepath.Join(dir, "cvs"), LogosDir: filepath.Join(dir, "logos")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`INSERT INTO users (id, google_sub, email, name, picture_url) VALUES (?, ?, ?, ?, ?)`,
		testUserID, "sub-"+testUserID, testUserID+"@example.com", testUserID, ""); err != nil {
		t.Fatal(err)
	}
	return New(db)
}

func TestMarshalInterviewersPreservesEmptyArrays(t *testing.T) {
	for _, interviewers := range [][]models.Interviewer{nil, {}} {
		encoded := marshalInterviewers(interviewers)
		if encoded != "[]" {
			t.Fatalf("marshal empty interviewers=%q", encoded)
		}
	}
	encoded := marshalInterviewers([]models.Interviewer{{Name: "Alex", Role: "Recruiter"}})
	if encoded != `[{"name":"Alex","role":"Recruiter"}]` {
		t.Fatalf("marshal interviewer=%q", encoded)
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
		{"all jobs", func() error { _, err := repo.GetAllJobs(testUserID, "", ""); return err }},
		{"job", func() error { _, err := repo.GetJobByID(testUserID, jobID); return err }},
		{"job counts", func() error { _, err := repo.GetJobCounts(testUserID); return err }},
		{"insert job", func() error { return repo.InsertJob(testUserID, &models.Job{ID: jobID}) }},
		{"update job", func() error {
			return repo.UpdateJob(testUserID, jobID, map[string]interface{}{"company_name": "Example"})
		}},
		{"delete job", func() error { return repo.DeleteJob(testUserID, jobID) }},
		{"reorder jobs", func() error { return repo.ReorderJobs(testUserID, []string{jobID}) }},
		{"job stages", func() error { _, err := repo.GetStagesByJobID(testUserID, jobID); return err }},
		{"all stages", func() error { _, err := repo.GetAllStages(); return err }},
		{"stage", func() error { _, err := repo.GetStageByID(testUserID, stageID); return err }},
		{"insert stage", func() error { return repo.InsertStage(&models.Stage{ID: stageID, JobID: jobID}) }},
		{"update stage", func() error { return repo.UpdateStage(testUserID, stageID, map[string]interface{}{"notes": "note"}) }},
		{"delete stage", func() error { return repo.DeleteStage(testUserID, stageID) }},
		{"reorder stages", func() error { return repo.ReorderStages(testUserID, jobID, []string{stageID}) }},
		{"set current stage", func() error { _, err := repo.SetCurrentStage(testUserID, stageID); return err }},
		{"stage questions", func() error { _, err := repo.GetQuestionsByStageID(testUserID, stageID); return err }},
		{"all questions", func() error { _, err := repo.GetAllQuestions(); return err }},
		{"question", func() error { _, err := repo.GetQuestionByID(testUserID, "question"); return err }},
		{"insert question", func() error { return repo.InsertQuestion(&models.Question{ID: "question", StageID: stageID}) }},
		{"update question", func() error {
			return repo.UpdateQuestion(testUserID, "question", map[string]interface{}{"question": "Q"})
		}},
		{"delete question", func() error { return repo.DeleteQuestion(testUserID, "question") }},
		{"reorder questions", func() error { return repo.ReorderQuestions(testUserID, stageID, []string{"question"}) }},
		{"job attachments", func() error { _, err := repo.GetAttachmentsByJobID(testUserID, jobID); return err }},
		{"all attachments", func() error { _, err := repo.GetAllAttachments(); return err }},
		{"attachment", func() error { _, err := repo.GetAttachmentByID(testUserID, "attachment"); return err }},
		{"insert attachment", func() error { return repo.InsertAttachment(&models.Attachment{ID: "attachment", JobID: jobID}) }},
		{"delete attachment", func() error { return repo.DeleteAttachment(testUserID, "attachment") }},
		{"scheduled meetings", func() error { _, err := repo.GetScheduledMeetings(testUserID); return err }},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if err := check.call(); err == nil {
				t.Fatal("closed database should return an error")
			}
		})
	}
}
