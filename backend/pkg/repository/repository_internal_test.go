package repository

import (
	"database/sql"
	"errors"
	"testing"

	_ "modernc.org/sqlite"
	"path/filepath"
	"war-room/backend/pkg/config"
	"war-room/backend/pkg/database"
	"war-room/backend/pkg/models"
)

type rowsResult struct {
	rows int64
	err  error
}

func TestReplaceAllJobsWithCVRestoresGappedSequenceAndSelection(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	versions := []models.CVVersion{{ID: "cv-1", Version: 1, OriginalName: "old.pdf", StoredFilename: "hash-1", FileSize: 3, SHA256: "hash-1"},
		{ID: "cv-3", Version: 3, OriginalName: "new.pdf", StoredFilename: "hash-3", FileSize: 3, SHA256: "hash-3"}}
	job := &models.Job{ID: "job-new", CompanyName: "Example", PositionTitle: "Engineer", Status: models.StatusOngoing,
		SalaryType: models.SalaryUnknown, SalaryCurrency: "EUR", RecruiterType: models.RecruiterNone,
		SelectedCVVersion: &versions[1]}
	if err := repo.ReplaceAllJobsWithCV([]models.Job{*job}, versions, true, 7); err != nil {
		t.Fatal(err)
	}
	loaded, err := repo.GetJobByID("job-new")
	if err != nil || loaded.SelectedCVVersion == nil || loaded.SelectedCVVersion.ID != "cv-3" {
		t.Fatalf("job=%+v err=%v", loaded, err)
	}
	next, err := repo.GetNextCVVersion()
	if err != nil || next != 7 {
		t.Fatalf("next version=%d err=%v", next, err)
	}
}

func TestCVRepositoryVersionLifecycle(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	insertRepositoryTestJob(t, repo, "job-cv")
	version := &models.CVVersion{ID: "cv-1", OriginalName: "resume.pdf", StoredFilename: "hash", FileSize: 5, MimeType: "application/pdf", SHA256: "hash", UploadedAt: 42}
	assertCVInsertAndRead(t, repo, version)
	assertCVAssignmentAndDelete(t, repo, version.ID)
}

func TestInsertFirstStageAndSetJobOngoing(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	insertRepositoryTestJob(t, repo, "job-first-stage")
	stage := &models.Stage{ID: "stage-first", JobID: "job-first-stage", StageType: models.StageHR, Status: models.StageStatusCurrent,
		MeetingType: "video", RecruiterType: models.RecruiterNone}
	if err := repo.InsertFirstStageAndSetJobOngoing(stage); err != nil {
		t.Fatalf("insert first stage: %v", err)
	}
	job, err := repo.GetJobByID(stage.JobID)
	if err != nil || job.Status != models.StatusOngoing {
		t.Fatalf("job status=%q err=%v", job.Status, err)
	}
	stored, err := repo.GetStageByID(stage.ID)
	if err != nil || stored.InterviewersJSON != "null" {
		t.Fatalf("stage interviewers=%q err=%v", stored.InterviewersJSON, err)
	}
}

func TestCreateTechnologyAliasFailureRollsBack(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	if _, err := repo.db.Exec(`CREATE TRIGGER fail_alias BEFORE INSERT ON technology_aliases BEGIN SELECT RAISE(ABORT, 'injected failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateTechnology(models.Technology{ID: "failed-create", Name: "Failed", Aliases: []string{"alias"}}); err == nil {
		t.Fatal("create should surface alias storage failure")
	}
	assertTechnologyNotStored(t, repo, "failed-create")
}

func TestUpdateTechnologyAliasFailureRollsBack(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	if err := repo.CreateTechnology(models.Technology{ID: "existing", Name: "Original", Aliases: []string{"old"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.Exec(`CREATE TRIGGER fail_alias BEFORE INSERT ON technology_aliases BEGIN SELECT RAISE(ABORT, 'injected failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateTechnology(models.Technology{ID: "existing", Name: "Changed", Aliases: []string{"new"}}); err == nil {
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
	if err := repo.CreateTechnology(models.Technology{ID: "existing", Name: "Original", Aliases: []string{"old"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.Exec(`CREATE TRIGGER fail_alias_delete BEFORE DELETE ON technology_aliases BEGIN SELECT RAISE(ABORT, 'injected failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateTechnology(models.Technology{ID: "existing", Name: "Changed", Aliases: []string{"new"}}); err == nil {
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
				err = repo.CreateTechnology(models.Technology{ID: "one", Name: "One"})
			case "update":
				err = repo.UpdateTechnology(models.Technology{ID: "one", Name: "One"})
			case "delete":
				err = repo.DeleteTechnology("one")
			default:
				_, err = repo.RemoveTechnologyAssignments("one")
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
			if err := repo.CreateTechnology(models.Technology{ID: "old-tech", Name: "Old"}); err != nil {
				t.Fatal(err)
			}
			if err := repo.CreateTechnology(models.Technology{ID: "new-tech", Name: "New"}); err != nil {
				t.Fatal(err)
			}
			if err := repo.SetJobTechnologies("stack-job", []string{"old-tech"}); err != nil {
				t.Fatal(err)
			}
			if _, err := repo.db.Exec(test.trigger); err != nil {
				t.Fatal(err)
			}
			if err := repo.SetJobTechnologies("stack-job", []string{"new-tech"}); err == nil {
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
		{name: "catalog name", setup: `PRAGMA foreign_keys=OFF; DROP TABLE technologies; CREATE TABLE technologies (id TEXT, name TEXT, normalized_name TEXT); INSERT INTO technologies VALUES ('one', NULL, NULL);`},
		{name: "alias", setup: `PRAGMA foreign_keys=OFF; DROP TABLE technology_aliases; CREATE TABLE technology_aliases (technology_id TEXT, alias TEXT, normalized_alias TEXT); INSERT INTO technology_aliases VALUES ('one', NULL, NULL);`},
		{name: "assigned job", setup: `PRAGMA foreign_keys=OFF; DROP TABLE jobs; CREATE TABLE jobs (id TEXT, company_name TEXT, position_title TEXT); INSERT INTO jobs VALUES ('job', NULL, 'Engineer'); INSERT INTO job_technologies VALUES ('job', 'one');`},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := newImportRepositoryFixture(t)
			repo.db.SetMaxOpenConns(1)
			if _, err := repo.db.Exec(test.setup); err != nil {
				t.Fatal(err)
			}
			if _, err := repo.ListTechnologies(); err == nil {
				t.Fatal("malformed catalog row should fail listing")
			}
		})
	}
}

func TestJobTechnologiesRejectsMalformedStoredName(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	repo.db.SetMaxOpenConns(1)
	insertRepositoryTestJob(t, repo, "stack-job")
	if err := repo.CreateTechnology(models.Technology{ID: "one", Name: "One"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetJobTechnologies("stack-job", []string{"one"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.Exec(`PRAGMA foreign_keys=OFF; DROP TABLE technologies; CREATE TABLE technologies (id TEXT, name TEXT, normalized_name TEXT); INSERT INTO technologies VALUES ('one', NULL, NULL);`); err != nil {
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
	if err := repo.SetJobTechnologies("stack-job", []string{"tech"}); err == nil {
		t.Fatal("missing catalog table should fail technology validation")
	}
}

func assertCVInsertAndRead(t *testing.T, repo *Repository, version *models.CVVersion) {
	t.Helper()
	if err := repo.InsertCVVersion(version); err != nil || version.Version != 1 {
		t.Fatalf("insert version=%+v err=%v", version, err)
	}
	loaded, err := repo.GetCVVersionByID(version.ID)
	if err != nil || loaded == nil || loaded.Version != 1 {
		t.Fatalf("loaded version=%+v err=%v", loaded, err)
	}
	if missing, err := repo.GetCVVersionByID("missing"); err != nil || missing != nil {
		t.Fatalf("missing version=%+v err=%v", missing, err)
	}
	versions, err := repo.ListCVVersions()
	if err != nil || len(versions) != 1 {
		t.Fatalf("versions=%+v err=%v", versions, err)
	}
}

func assertCVAssignmentAndDelete(t *testing.T, repo *Repository, versionID string) {
	t.Helper()
	if err := repo.AssignCVVersion("job-cv", &versionID); err != nil {
		t.Fatal(err)
	}
	var refs int
	if err := repo.CountCVBlobReferences("hash", &refs); err != nil || refs != 1 {
		t.Fatalf("references=%d err=%v", refs, err)
	}
	if err := repo.DeleteCVVersion(versionID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("assigned delete err=%v", err)
	}
	if err := repo.AssignCVVersion("job-cv", nil); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteCVVersion(versionID); err != nil {
		t.Fatal(err)
	}
	if err := repo.AssignCVVersion("missing", nil); !errors.Is(err, sql.ErrNoRows) {
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
	job, err := repo.GetJobByID("legacy-job")
	if err != nil || job.SelectedCVVersion == nil || job.SelectedCVVersion.ID != version.ID {
		t.Fatalf("migrated job=%+v err=%v", job, err)
	}
}

func insertRepositoryTestJob(t *testing.T, repo *Repository, id string) {
	t.Helper()
	if err := repo.InsertJob(&models.Job{ID: id, CompanyName: "Example", PositionTitle: "Engineer", Status: models.StatusOngoing,
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
