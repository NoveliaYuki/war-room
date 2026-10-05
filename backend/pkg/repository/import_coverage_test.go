package repository

import (
	"testing"

	"war-room/backend/pkg/models"
)

func TestReplaceAllJobsRollsBackWhenImportedRecordCannotBeInserted(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	insertRepositoryTestJob(t, repo, "kept-job")
	duplicate := models.Job{ID: "duplicate", CompanyName: "Example", PositionTitle: "Engineer", Status: models.StatusOngoing}
	err := repo.ReplaceAllJobs([]models.Job{duplicate, duplicate})
	if err == nil {
		t.Fatal("duplicate imported job unexpectedly succeeded")
	}
	kept, readErr := repo.GetJobByID("kept-job")
	if readErr != nil || kept == nil {
		t.Fatalf("failed import did not preserve original data: job=%+v err=%v", kept, readErr)
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
			if err := repo.ReplaceAllJobs(nil); err == nil {
				t.Fatal("import unexpectedly succeeded with incomplete schema")
			}
		})
	}
}

func TestReplaceAllJobsRollsBackInvalidImportedCVLibrary(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	insertRepositoryTestJob(t, repo, "kept-job")
	version := models.CVVersion{ID: "duplicate-cv", Version: 1, OriginalName: "resume.pdf", StoredFilename: "hash", FileSize: 4, SHA256: "hash"}
	if err := repo.ReplaceAllJobsWithCV(nil, []models.CVVersion{version, version}, true, 2); err == nil {
		t.Fatal("duplicate CV library rows unexpectedly imported")
	}
	kept, err := repo.GetJobByID("kept-job")
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
	if err := repo.ReplaceAllWithTechnologyCatalog([]models.Job{job}, []models.Technology{{ID: "tech", Name: "Go"}}, nil, false, 1); err == nil {
		t.Fatal("commit should reject the missing CV version reference")
	}
	catalog, err := repo.ListTechnologies()
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
	if err := repo.ReplaceAllWithTechnologyCatalog(nil, nil, nil, false, 1); err == nil {
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
	if _, err := repo.GetAllJobs("", ""); err == nil {
		t.Fatal("invalid integer data was accepted")
	}
}

func TestGetJobByIDSurfacesSelectedVersionLookupFailure(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	insertRepositoryTestJob(t, repo, "selected-job")
	version := &models.CVVersion{ID: "selected-cv", Version: 1, OriginalName: "resume.pdf", StoredFilename: "hash", FileSize: 4, SHA256: "hash"}
	if err := repo.InsertCVVersion(version); err != nil {
		t.Fatal(err)
	}
	if err := repo.AssignCVVersion("selected-job", &version.ID); err != nil {
		t.Fatal(err)
	}
	repo.db.SetMaxOpenConns(1)
	if _, err := repo.db.Exec(`PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.Exec(`DROP TABLE cv_versions`); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetJobByID("selected-job"); err == nil {
		t.Fatal("selected CV lookup failure was hidden")
	}
}

func TestRepositoryRejectsCorruptSerializedAndNumericFields(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	insertRepositoryTestJob(t, repo, "invalid-json")
	if _, err := repo.db.Exec(`UPDATE jobs SET interviewers_json = '{' WHERE id = 'invalid-json'`); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetJobByID("invalid-json"); err == nil {
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
	if _, err := repo.GetScheduledMeetings(); err == nil {
		t.Fatal("invalid meeting order was accepted")
	}
}

func TestGetCVVersionByIDSurfacesCorruptSize(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	version := &models.CVVersion{ID: "bad-size", Version: 1, OriginalName: "resume.pdf", StoredFilename: "hash", FileSize: 4, SHA256: "hash"}
	if err := repo.InsertCVVersion(version); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.Exec(`UPDATE cv_versions SET file_size = 'not-an-integer' WHERE id = ?`, version.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetCVVersionByID(version.ID); err == nil {
		t.Fatal("invalid CV size was accepted")
	}
	if _, err := repo.ListCVVersions(); err == nil {
		t.Fatal("CV list accepted an invalid file size")
	}
}

func TestInsertCVVersionRejectsDuplicateID(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	version := &models.CVVersion{ID: "duplicate", Version: 1, OriginalName: "resume.pdf", StoredFilename: "hash", FileSize: 4, SHA256: "hash"}
	if err := repo.InsertCVVersion(version); err != nil {
		t.Fatal(err)
	}
	duplicate := *version
	duplicate.Version = 2
	if err := repo.InsertCVVersion(&duplicate); err == nil {
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
	versions, err := repo.ListCVVersions()
	if err != nil || len(versions) != 0 {
		t.Fatalf("failed legacy migration left versions=%+v err=%v", versions, err)
	}
}

func TestCVRepositoryMutationErrorsOnClosedDatabase(t *testing.T) {
	repo := newImportRepositoryFixture(t)
	if err := repo.db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteCVVersion("missing"); err == nil {
		t.Fatal("CV delete hid a database error")
	}
	var references int
	if err := repo.CountCVBlobReferences("hash", &references); err == nil {
		t.Fatal("CV blob reference count hid a database error")
	}
	if _, err := repo.ListCVVersions(); err == nil {
		t.Fatal("CV version list hid a database error")
	}
	if _, err := repo.GetCVVersionByID("missing"); err == nil {
		t.Fatal("CV version lookup hid a database error")
	}
	if _, err := repo.GetNextCVVersion(); err == nil {
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
	if err := repo.ReplaceAllJobsWithCV(nil, nil, true, 1); err == nil {
		t.Fatal("import succeeded without a CV sequence table")
	}
}
