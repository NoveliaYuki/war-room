package repository

import (
	"strings"
	"testing"

	"war-room/backend/pkg/models"
)

func TestImportCleanupReturnsDeleteFailures(t *testing.T) {
	for _, test := range []struct {
		name   string
		setup  string
		insert string
	}{
		{name: "technology catalog", setup: `CREATE TRIGGER reject_technology_delete BEFORE DELETE ON technologies BEGIN SELECT RAISE(ABORT, 'rejected technology delete'); END`, insert: `INSERT INTO technologies (id, user_id, name, normalized_name) VALUES ('tech', '` + testUserID + `', 'Go', 'go')`},
		{name: "search periods", setup: `CREATE TRIGGER reject_period_delete BEFORE DELETE ON search_periods BEGIN SELECT RAISE(ABORT, 'rejected period delete'); END`, insert: `INSERT INTO search_periods (id, user_id, name, start_date, end_date) VALUES ('period', '` + testUserID + `', 'March', '2026-03-01', '2026-03-31')`},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := newImportRepositoryFixture(t)
			if _, err := repo.db.Exec(test.setup); err != nil {
				t.Fatal(err)
			}
			if _, err := repo.db.Exec(test.insert); err != nil {
				t.Fatal(err)
			}
			tx, err := repo.db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback() }()
			if err := clearImportedData(tx, testUserID); err == nil {
				t.Fatal("import cleanup ignored a rejected delete")
			}
		})
	}
}

func TestReplaceImportedCVLibraryReturnsDeleteFailure(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	version := &models.CVVersion{ID: "existing-cv", OriginalName: "resume.pdf", StoredFilename: "hash", FileSize: 4, SHA256: "hash"}
	if err := repo.InsertCVVersion(testUserID, version); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.Exec(`CREATE TRIGGER reject_cv_delete BEFORE DELETE ON cv_versions BEGIN SELECT RAISE(ABORT, 'rejected CV delete'); END`); err != nil {
		t.Fatal(err)
	}
	tx, err := repo.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := replaceImportedCVLibrary(tx, testUserID, nil, 1); err == nil {
		t.Fatal("CV library replacement ignored a rejected delete")
	}
}

func TestReorderQuestionsReturnsUpdateFailure(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	insertRepositoryTestJob(t, repo, "question-reorder")
	if _, err := repo.db.Exec(`INSERT INTO stages (id, job_id, order_index, stage_type) VALUES ('stage', 'question-reorder', 0, 'HR')`); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.Exec(`INSERT INTO stage_questions (id, stage_id, order_index, question) VALUES ('question', 'stage', 0, 'Question')`); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.Exec(`CREATE TRIGGER reject_question_reorder BEFORE UPDATE OF order_index ON stage_questions BEGIN SELECT RAISE(ABORT, 'rejected reorder'); END`); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReorderQuestions(testUserID, "stage", []string{"question"}); err == nil {
		t.Fatal("question reorder ignored a rejected update")
	}
}

func TestLegacyCVMigrationRollsBackEachRejectedWrite(t *testing.T) {
	for _, test := range []struct{ name, trigger string }{
		{name: "version insert", trigger: `CREATE TRIGGER reject_cv_insert BEFORE INSERT ON cv_versions BEGIN SELECT RAISE(ABORT, 'rejected CV insert'); END`},
		{name: "job assignment", trigger: `CREATE TRIGGER reject_cv_assignment BEFORE UPDATE OF cv_version_id ON jobs BEGIN SELECT RAISE(ABORT, 'rejected CV assignment'); END`},
		{name: "attachment delete", trigger: `CREATE TRIGGER reject_legacy_attachment_delete BEFORE DELETE ON attachments BEGIN SELECT RAISE(ABORT, 'rejected attachment delete'); END`},
		{name: "sequence update", trigger: `CREATE TRIGGER reject_cv_sequence BEFORE UPDATE OF next_version ON cv_version_sequence BEGIN SELECT RAISE(ABORT, 'rejected CV sequence'); END`},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := newImportRepositoryFixture(t)
			insertRepositoryTestJob(t, repo, "legacy-cv-job")
			if test.name == "attachment delete" {
				if _, err := repo.db.Exec(`INSERT INTO attachments (id, job_id, original_name, stored_filename, absolute_path, file_size, mime_type) VALUES ('legacy-attachment', 'legacy-cv-job', 'resume.pdf', 'legacy-file', '/tmp/legacy-file', 4, 'application/pdf')`); err != nil {
					t.Fatal(err)
				}
			}
			if test.name == "sequence update" {
				// Pre-seed the sequence row so the upsert below takes its ON
				// CONFLICT DO UPDATE branch, which is what the trigger
				// (BEFORE UPDATE OF next_version) actually fires on.
				if _, err := repo.db.Exec(`INSERT INTO cv_version_sequence (user_id, next_version) VALUES (?, 1)`, testUserID); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := repo.db.Exec(test.trigger); err != nil {
				t.Fatal(err)
			}
			attachments := []models.Attachment(nil)
			if test.name == "attachment delete" {
				attachments = []models.Attachment{{ID: "legacy-attachment"}}
			}
			_, err := repo.MigrateLegacyCVAttachments(&models.CVVersion{ID: "migrated-cv", OriginalName: "resume.pdf", StoredFilename: "legacy-file", FileSize: 4, SHA256: "hash"}, attachments, []string{"legacy-cv-job"})
			if err == nil {
				t.Fatal("legacy CV migration ignored a rejected write")
			}
		})
	}
}

func TestAttachmentReadRejectsMalformedStoredSize(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	insertRepositoryTestJob(t, repo, "malformed-attachment-job")
	if _, err := repo.db.Exec(`INSERT INTO attachments (id, job_id, original_name, stored_filename, absolute_path, file_size, mime_type) VALUES ('bad-size', 'malformed-attachment-job', 'resume.pdf', 'bad-file', '/tmp/bad-file', 'invalid', 'application/pdf')`); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetAllAttachments(); err == nil {
		t.Fatal("attachment list accepted a malformed file size")
	}
}

func TestReorderJobsReturnsUpdateFailure(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	insertRepositoryTestJob(t, repo, "reorder-failure")
	if _, err := repo.db.Exec(`CREATE TRIGGER reject_job_reorder BEFORE UPDATE OF order_index ON jobs BEGIN SELECT RAISE(ABORT, 'rejected reorder'); END`); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReorderJobs(testUserID, []string{"reorder-failure"}); err == nil {
		t.Fatal("job reorder ignored a rejected update")
	}
}

func TestReorderOperationsRejectMissingRows(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	if err := repo.ReorderJobs(testUserID, []string{"missing-job"}); err == nil {
		t.Fatal("job reorder accepted a missing job")
	}
	if err := repo.ReorderStages(testUserID, "missing-job", []string{"missing-stage"}); err == nil {
		t.Fatal("stage reorder accepted a missing stage")
	}
	if err := repo.ReorderQuestions(testUserID, "missing-stage", []string{"missing-question"}); err == nil {
		t.Fatal("question reorder accepted a missing question")
	}
}

func TestReorderStagesReturnsUpdateFailure(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	insertRepositoryTestJob(t, repo, "reorder-stage-failure-job")
	stage := &models.Stage{ID: "reorder-stage-failure-stage", JobID: "reorder-stage-failure-job", StageType: models.StageHR, Status: models.StageStatusPending, MeetingType: "video", RecruiterType: models.RecruiterNone}
	if err := repo.InsertStage(stage); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.Exec(`CREATE TRIGGER reject_stage_reorder BEFORE UPDATE OF order_index ON stages BEGIN SELECT RAISE(ABORT, 'rejected stage reorder'); END`); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReorderStages(testUserID, "reorder-stage-failure-job", []string{stage.ID}); err == nil {
		t.Fatal("stage reorder ignored a rejected update")
	}
}

func TestReorderStagesRejectsDuplicateStageID(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	insertRepositoryTestJob(t, repo, "duplicate-reorder-job")
	stage := &models.Stage{ID: "duplicate-reorder-stage", JobID: "duplicate-reorder-job", StageType: models.StageHR, Status: models.StageStatusPending, MeetingType: "video", RecruiterType: models.RecruiterNone}
	if err := repo.InsertStage(stage); err != nil {
		t.Fatal(err)
	}
	err := repo.ReorderStages(testUserID, "duplicate-reorder-job", []string{stage.ID, stage.ID})
	if err == nil || !strings.Contains(err.Error(), "duplicate stage id") {
		t.Fatalf("ReorderStages error=%v, want a duplicate stage id error", err)
	}
}

func TestStageReadRejectsMalformedStoredTimestamp(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	insertRepositoryTestJob(t, repo, "malformed-stage-job")
	if _, err := repo.db.Exec(`INSERT INTO stages (id, job_id, order_index, stage_type, created_at) VALUES ('bad-stage', 'malformed-stage-job', 0, 'HR', 'invalid')`); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetStagesByJobID(testUserID, "malformed-stage-job"); err == nil {
		t.Fatal("stage list accepted a malformed timestamp")
	}
}

func TestSetCurrentStageReturnsUpdateFailure(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	insertRepositoryTestJob(t, repo, "current-stage-failure")
	if _, err := repo.db.Exec(`INSERT INTO stages (id, job_id, order_index, stage_type) VALUES ('current-stage', 'current-stage-failure', 0, 'HR')`); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.Exec(`CREATE TRIGGER reject_current_stage BEFORE UPDATE ON stages BEGIN SELECT RAISE(ABORT, 'rejected current stage'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SetCurrentStage(testUserID, "current-stage"); err == nil {
		t.Fatal("current stage update ignored a rejected write")
	}
}

func TestNormalizeJobUpdateValueClearsBlankOptionalDates(t *testing.T) {
	blank := "  \t"
	if got := normalizeJobUpdateValue("application_sent_date", &blank); got != nil {
		t.Fatalf("blank optional date was not cleared: %#v", got)
	}
}

func TestReplaceAllJobsRollsBackWhenImportedRecordCannotBeInserted(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	insertRepositoryTestJob(t, repo, "kept-job")
	duplicate := models.Job{ID: "duplicate", CompanyName: "Example", PositionTitle: "Engineer", Status: models.StatusOngoing}
	err := repo.ReplaceAllJobs(testUserID, []models.Job{duplicate, duplicate})
	if err == nil {
		t.Fatal("duplicate imported job unexpectedly succeeded")
	}
	kept, readErr := repo.GetJobByID(testUserID, "kept-job")
	if readErr != nil || kept == nil {
		t.Fatalf("failed import did not preserve original data: job=%+v err=%v", kept, readErr)
	}
}

func TestReplaceAllSearchPeriodsRollsBackDuplicatePeriodImport(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	insertRepositoryTestJob(t, repo, "kept-job")
	period := models.SearchPeriod{ID: "duplicate-period", Name: "March", StartDate: "2026-03-01", EndDate: "2026-03-31"}
	if err := repo.ReplaceAllWithSearchPeriods(testUserID, nil, []models.SearchPeriod{period, period}, nil, nil, false, 1); err == nil {
		t.Fatal("duplicate search periods unexpectedly imported")
	}
	kept, err := repo.GetJobByID(testUserID, "kept-job")
	if err != nil || kept == nil {
		t.Fatalf("failed import did not preserve previous jobs: job=%+v err=%v", kept, err)
	}
}

func TestReplaceAllJobsClosesPreparedStatementsWhenSchemaIsIncomplete(t *testing.T) {
	for _, test := range []struct{ name, renameColumn string }{
		{name: "job insert", renameColumn: "ALTER TABLE jobs RENAME COLUMN company_name TO saved_company_name"},
		{name: "stage insert", renameColumn: "ALTER TABLE stages RENAME COLUMN notes TO saved_notes"},
		{name: "question insert", renameColumn: "ALTER TABLE stage_questions RENAME COLUMN question TO saved_question"},
		{name: "attachment insert", renameColumn: "ALTER TABLE attachments RENAME COLUMN original_name TO saved_original_name"},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := newImportRepositoryFixture(t)
			if _, err := repo.db.Exec(test.renameColumn); err != nil {
				t.Fatal(err)
			}
			if err := repo.ReplaceAllJobs(testUserID, nil); err == nil {
				t.Fatal("import unexpectedly succeeded with incomplete schema")
			}
		})
	}
}

func TestReplaceAllJobsReportsHistoryWriteFailures(t *testing.T) {
	for _, test := range []struct{ name, trigger string }{
		{name: "delete", trigger: `CREATE TRIGGER reject_history_delete BEFORE DELETE ON job_history BEGIN SELECT RAISE(ABORT, 'rejected history delete'); END`},
		{name: "insert", trigger: `CREATE TRIGGER reject_history_insert BEFORE INSERT ON job_history WHEN NEW.action = 'moved to ongoing' BEGIN SELECT RAISE(ABORT, 'rejected history insert'); END`},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := newImportRepositoryFixture(t)
			if _, err := repo.db.Exec(test.trigger); err != nil {
				t.Fatal(err)
			}
			job := models.Job{ID: "history-error", CompanyName: "Example", PositionTitle: "Engineer", Status: models.StatusOngoing,
				History: []models.JobHistory{{OccurredAt: 100, Action: "moved to ongoing"}}}
			if err := repo.ReplaceAllJobs(testUserID, []models.Job{job}); err == nil {
				t.Fatal("import unexpectedly ignored history write failure")
			}
		})
	}
}

func TestReplaceAllJobsRollsBackInvalidImportedCVLibrary(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	insertRepositoryTestJob(t, repo, "kept-job")
	version := models.CVVersion{ID: "duplicate-cv", Version: 1, OriginalName: "resume.pdf", StoredFilename: "hash", FileSize: 4, SHA256: "hash"}
	if err := repo.ReplaceAllJobsWithCV(testUserID, nil, []models.CVVersion{version, version}, true, 2); err == nil {
		t.Fatal("duplicate CV library rows unexpectedly imported")
	}
	kept, err := repo.GetJobByID(testUserID, "kept-job")
	if err != nil || kept == nil {
		t.Fatalf("failed CV import did not preserve previous jobs: job=%+v err=%v", kept, err)
	}
}

func TestReplaceAllWithTechnologyCatalogReturnsCommitConstraintFailure(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	repo.db.SetMaxOpenConns(1)
	if _, err := repo.db.Exec(`PRAGMA defer_foreign_keys = ON`); err != nil {
		t.Fatal(err)
	}
	job := models.Job{ID: "invalid-cv-reference", CompanyName: "Example", PositionTitle: "Engineer", Status: models.StatusOngoing,
		SelectedCVVersion: &models.CVVersion{ID: "missing-cv"}}
	if err := repo.ReplaceAllWithTechnologyCatalog(testUserID, []models.Job{job}, []models.Technology{{ID: "tech", Name: "Go"}}, nil, false, 1); err == nil {
		t.Fatal("commit should reject the missing CV version reference")
	}
	catalog, err := repo.ListTechnologies(testUserID)
	if err != nil || len(catalog) != 0 {
		t.Fatalf("failed import left technology catalog=%+v err=%v", catalog, err)
	}
}

func TestReplaceAllWithTechnologyCatalogReportsCatalogDeleteFailure(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	insertRepositoryTestJob(t, repo, "preserved")
	if _, err := repo.db.Exec(`DROP TABLE technologies`); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceAllWithTechnologyCatalog(testUserID, nil, nil, nil, false, 1); err == nil {
		t.Fatal("import should report a missing technology table")
	}
	var count int
	if err := repo.db.QueryRow(`SELECT COUNT(*) FROM jobs WHERE id='preserved'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("failed import did not preserve existing job: count=%d err=%v", count, err)
	}
}

func TestRepositorySurfacesCorruptDatabaseValues(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	insertRepositoryTestJob(t, repo, "corrupt-job")
	if _, err := repo.db.Exec(`UPDATE jobs SET order_index = 'not-an-integer' WHERE id = 'corrupt-job'`); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetAllJobs(testUserID, "", ""); err == nil {
		t.Fatal("invalid integer data was accepted")
	}
}

func TestGetJobByIDSurfacesSelectedVersionLookupFailure(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	insertRepositoryTestJob(t, repo, "selected-job")
	version := &models.CVVersion{ID: "selected-cv", Version: 1, OriginalName: "resume.pdf", StoredFilename: "hash", FileSize: 4, SHA256: "hash"}
	if err := repo.InsertCVVersion(testUserID, version); err != nil {
		t.Fatal(err)
	}
	if err := repo.AssignCVVersion(testUserID, "selected-job", &version.ID); err != nil {
		t.Fatal(err)
	}
	repo.db.SetMaxOpenConns(1)
	if _, err := repo.db.Exec(`PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.Exec(`DROP TABLE cv_versions`); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetJobByID(testUserID, "selected-job"); err == nil {
		t.Fatal("selected CV lookup failure was hidden")
	}
}

func TestRepositoryRejectsCorruptSerializedAndNumericFields(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	insertRepositoryTestJob(t, repo, "invalid-json")
	if _, err := repo.db.Exec(`UPDATE jobs SET interviewers_json = '{' WHERE id = 'invalid-json'`); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetJobByID(testUserID, "invalid-json"); err == nil {
		t.Fatal("invalid interviewer JSON was accepted")
	}

	meetingDate := "2030-06-15"
	stage := models.Stage{ID: "invalid-meeting", JobID: "invalid-json", StageType: models.StageHR, Status: models.StageStatusPending,
		MeetingDate: &meetingDate, MeetingType: "video", RecruiterType: models.RecruiterNone}
	if err := repo.InsertStage(&stage); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.Exec(`UPDATE stages SET order_index = 'not-an-integer' WHERE id = 'invalid-meeting'`); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetScheduledMeetings(testUserID); err == nil {
		t.Fatal("invalid meeting order was accepted")
	}
}

func TestGetCVVersionByIDSurfacesCorruptSize(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	version := &models.CVVersion{ID: "bad-size", Version: 1, OriginalName: "resume.pdf", StoredFilename: "hash", FileSize: 4, SHA256: "hash"}
	if err := repo.InsertCVVersion(testUserID, version); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.Exec(`UPDATE cv_versions SET file_size = 'not-an-integer' WHERE id = ?`, version.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetCVVersionByID(testUserID, version.ID); err == nil {
		t.Fatal("invalid CV size was accepted")
	}
	if _, err := repo.ListCVVersions(testUserID); err == nil {
		t.Fatal("CV list accepted an invalid file size")
	}
}

func TestInsertCVVersionRejectsDuplicateID(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	version := &models.CVVersion{ID: "duplicate", Version: 1, OriginalName: "resume.pdf", StoredFilename: "hash", FileSize: 4, SHA256: "hash"}
	if err := repo.InsertCVVersion(testUserID, version); err != nil {
		t.Fatal(err)
	}
	duplicate := *version
	duplicate.Version = 2
	if err := repo.InsertCVVersion(testUserID, &duplicate); err == nil {
		t.Fatal("duplicate CV version ID was accepted")
	}
}

func TestLegacyCVMigrationRollsBackOnAssignmentFailure(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	insertRepositoryTestJob(t, repo, "legacy-job")
	if _, err := repo.db.Exec(`CREATE TRIGGER fail_cv_assignment BEFORE UPDATE OF cv_version_id ON jobs BEGIN SELECT RAISE(ABORT, 'assignment denied'); END`); err != nil {
		t.Fatal(err)
	}
	version := &models.CVVersion{ID: "legacy-cv", OriginalName: "resume.pdf", StoredFilename: "hash", FileSize: 4, SHA256: "hash"}
	if created, err := repo.MigrateLegacyCVAttachments(version, nil, []string{"legacy-job"}); err == nil || created {
		t.Fatalf("failed legacy assignment created=%t err=%v", created, err)
	}
	versions, err := repo.ListCVVersions(testUserID)
	if err != nil || len(versions) != 0 {
		t.Fatalf("failed legacy migration left versions=%+v err=%v", versions, err)
	}
}

func TestCVRepositoryMutationErrorsOnClosedDatabase(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	if err := repo.db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteCVVersion(testUserID, "missing"); err == nil {
		t.Fatal("CV delete hid a database error")
	}
	var references int
	if err := repo.CountCVBlobReferences("hash", &references); err == nil {
		t.Fatal("CV blob reference count hid a database error")
	}
	if _, err := repo.ListCVVersions(testUserID); err == nil {
		t.Fatal("CV version list hid a database error")
	}
	if _, err := repo.GetCVVersionByID(testUserID, "missing"); err == nil {
		t.Fatal("CV version lookup hid a database error")
	}
	if _, err := repo.GetNextCVVersion(testUserID); err == nil {
		t.Fatal("CV sequence lookup hid a database error")
	}
	version := &models.CVVersion{ID: "legacy-cv", OriginalName: "resume.pdf", StoredFilename: "hash", FileSize: 4, SHA256: "hash"}
	if _, err := repo.MigrateLegacyCVAttachments(version, nil, nil); err == nil {
		t.Fatal("legacy migration hid a transaction error")
	}
}

func TestReplaceAllJobsReportsMissingCVSequenceTable(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	if _, err := repo.db.Exec(`DROP TABLE cv_version_sequence`); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceAllJobsWithCV(testUserID, nil, nil, true, 1); err == nil {
		t.Fatal("import succeeded without a CV sequence table")
	}
}
