package database

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
	"war-room/backend/pkg/config"
	"war-room/backend/pkg/models"
)

type migrationTestExecutor struct {
	db        *sql.DB
	failQuery string
	failExec  string
}

func (executor migrationTestExecutor) Exec(query string, args ...any) (sql.Result, error) {
	if executor.failExec != "" && strings.Contains(query, executor.failExec) {
		return nil, errors.New("injected execution failure")
	}
	return executor.db.Exec(query, args...)
}

func (executor migrationTestExecutor) Query(query string, args ...any) (*sql.Rows, error) {
	if executor.failQuery != "" && strings.Contains(query, executor.failQuery) {
		return nil, errors.New("injected query failure")
	}
	return executor.db.Query(query, args...)
}

func TestEnsureOptionalColumnsOnLegacyJobsTable(t *testing.T) {
	db := openMigrationTestDB(t)
	if _, err := db.Exec("CREATE TABLE jobs (id TEXT PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}

	for name, migrate := range map[string]func(schemaExecutor) error{
		"preparation":      ensurePreparationColumns,
		"work arrangement": ensureWorkArrangementColumns,
		"employment type":  ensureEmploymentTypeColumn,
	} {
		t.Run(name, func(t *testing.T) {
			if err := migrate(db); err != nil {
				t.Fatalf("migrate legacy jobs table: %v", err)
			}
		})
	}
	for _, column := range []string{"experience_notes", "expected_salary", "work_arrangement", "employment_type"} {
		exists, err := hasColumn(db, "jobs", column)
		if err != nil || !exists {
			t.Fatalf("column %s exists=%t err=%v", column, exists, err)
		}
	}
	// Running each migration twice also covers the already-present-column path.
	if err := ensurePreparationColumns(db); err != nil {
		t.Fatal(err)
	}
	if err := ensureWorkArrangementColumns(db); err != nil {
		t.Fatal(err)
	}
	if err := ensureEmploymentTypeColumn(db); err != nil {
		t.Fatal(err)
	}
}

func TestInitDBRejectsConnectionSchemaAndVersionErrors(t *testing.T) {
	root := t.TempDir()
	assertInitDBFailure(t, root, "database path that is a directory should fail to connect")
	assertLegacySchemaRejected(t, root)
	assertNewerSchemaRejected(t, root)
}

func assertInitDBFailure(t *testing.T, path, message string) {
	t.Helper()
	if _, err := InitDB(&config.Config{DBPath: path}); err == nil {
		t.Fatal(message)
	}
}

func assertLegacySchemaRejected(t *testing.T, root string) {
	t.Helper()
	legacyPath := filepath.Join(root, "legacy.db")
	legacyDB, err := sql.Open("sqlite", legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacyDB.Exec("CREATE TABLE jobs (id TEXT PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	if err := legacyDB.Close(); err != nil {
		t.Fatal(err)
	}
	assertInitDBFailure(t, legacyPath, "incompatible pre-existing schema should fail")
}

func assertNewerSchemaRejected(t *testing.T, root string) {
	t.Helper()
	newerPath := filepath.Join(root, "newer.db")
	newerDB, err := sql.Open("sqlite", newerPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := newerDB.Exec(schemaSQL); err != nil {
		t.Fatal(err)
	}
	if _, err := newerDB.Exec("INSERT INTO schema_migrations (version) VALUES (?)", latestSchemaVersion+1); err != nil {
		t.Fatal(err)
	}
	if err := newerDB.Close(); err != nil {
		t.Fatal(err)
	}
	assertInitDBFailure(t, newerPath, "newer database version should fail")
}

func TestInitDBRejectsSchemaExecutionAndMigrationTableErrors(t *testing.T) {
	root := t.TempDir()
	viewPath := filepath.Join(root, "view.db")
	viewDB, err := sql.Open("sqlite", viewPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := viewDB.Exec("CREATE VIEW jobs AS SELECT 'id' AS id"); err != nil {
		t.Fatal(err)
	}
	if err := viewDB.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := InitDB(&config.Config{DBPath: viewPath}); err == nil {
		t.Fatal("incompatible database object should fail schema execution")
	}

	db := openMigrationTestDB(t)
	if err := migrateSchema(db); err == nil {
		t.Fatal("missing migration table should fail schema migration")
	}
}

func TestApplySchemaMigrationReportsBeginAndRecordErrors(t *testing.T) {
	closedDB := openMigrationTestDB(t)
	if err := closedDB.Close(); err != nil {
		t.Fatal(err)
	}
	if err := applySchemaMigration(closedDB, 2); err == nil {
		t.Fatal("closed database should fail migration transaction creation")
	}

	db := openMigrationTestDB(t)
	if _, err := db.Exec(schemaSQL); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TRIGGER reject_migration_record BEFORE INSERT ON schema_migrations BEGIN SELECT RAISE(ABORT, 'record denied'); END"); err != nil {
		t.Fatal(err)
	}
	if err := applySchemaMigration(db, 2); err == nil {
		t.Fatal("migration record failure should abort the transaction")
	}
}

func TestColumnMigrationsReturnQueryAndAlterErrors(t *testing.T) {
	db := openMigrationTestDB(t)
	if _, err := db.Exec("CREATE TABLE jobs (id TEXT PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		migrate func(schemaExecutor) error
		fake    migrationTestExecutor
	}{
		{"preparation inspect", ensurePreparationColumns, migrationTestExecutor{db: db, failQuery: "table_info"}},
		{"preparation add", ensurePreparationColumns, migrationTestExecutor{db: db, failExec: "experience_notes"}},
		{"work inspect", ensureWorkArrangementColumns, migrationTestExecutor{db: db, failQuery: "table_info"}},
		{"work add", ensureWorkArrangementColumns, migrationTestExecutor{db: db, failExec: "ADD COLUMN work_arrangement"}},
		{"employment inspect", ensureEmploymentTypeColumn, migrationTestExecutor{db: db, failQuery: "table_info"}},
		{"employment add", ensureEmploymentTypeColumn, migrationTestExecutor{db: db, failExec: "ADD COLUMN employment_type"}},
		{"employment constraints", ensureEmploymentTypeColumn, migrationTestExecutor{db: db, failExec: "UPDATE jobs SET employment_type"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.migrate(test.fake); err == nil {
				t.Fatal("expected migration failure")
			}
		})
	}
}

func TestSchemaMigrationDispatchReportsFailures(t *testing.T) {
	db := openMigrationTestDB(t)
	if _, err := db.Exec("CREATE TABLE jobs (id TEXT PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	executor := migrationTestExecutor{db: db, failExec: "CREATE TRIGGER"}
	for _, version := range []int{2, 3} {
		if err := runSchemaMigration(executor, version); err == nil {
			t.Errorf("migration %d should report trigger creation failure", version)
		}
	}
	if err := runSchemaMigration(executor, 99); err == nil || !strings.Contains(err.Error(), "unknown migration") {
		t.Fatalf("unknown migration error=%v", err)
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		t.Fatal(err)
	}
	if err := applyInitialSchemaMigration(migrationTestExecutor{db: db, failExec: "CREATE INDEX"}); err == nil {
		t.Fatal("initial migration should report index creation failure")
	}
}

func TestLegacyColumnMigrationReportsMissingTable(t *testing.T) {
	db := openMigrationTestDB(t)
	if _, err := db.Exec("CREATE TABLE jobs (id TEXT PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	if err := ensureLegacyColumns(db); err == nil || !strings.Contains(err.Error(), "stages.meeting_date") {
		t.Fatalf("missing legacy table error=%v", err)
	}
	if err := applyInitialSchemaMigration(db); err == nil {
		t.Fatal("initial schema migration should report missing legacy tables")
	}
	if err := ensureLegacyColumns(migrationTestExecutor{db: db, failQuery: "table_info"}); err == nil {
		t.Fatal("legacy column inspection query should fail")
	}
}

func TestHasColumnReportsScanFailure(t *testing.T) {
	db := openMigrationTestDB(t)
	if _, err := db.Exec("CREATE TABLE jobs (id TEXT PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query("SELECT 1")
	if err != nil {
		t.Fatal(err)
	}
	_ = rows.Close()
	_, err = hasColumn(migrationTestExecutor{db: db, failQuery: "PRAGMA table_info"}, "jobs", "id")
	if err == nil {
		t.Fatal("expected query failure")
	}
	_, err = hasColumn(malformedColumnInfo{db: db}, "jobs", "id")
	if err == nil {
		t.Fatal("expected column metadata scan failure")
	}
}

func TestBackupLoadingRejectsMissingAndMalformedFiles(t *testing.T) {
	directory := t.TempDir()
	backupPath := filepath.Join(directory, "backup.json")
	if _, err := loadBackup(backupPath); err == nil {
		t.Fatal("missing backup should fail to load")
	}
	if err := os.WriteFile(backupPath, []byte("not json"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadBackup(backupPath); err == nil {
		t.Fatal("malformed backup should fail to parse")
	}
	if err := os.Truncate(backupPath, maxBackupBytes+1); err != nil {
		t.Fatal(err)
	}
	if _, err := loadBackup(backupPath); err == nil || !strings.Contains(err.Error(), "backup exceeds") {
		t.Fatalf("oversized backup error=%v", err)
	}
}

func TestBackupLoadingAcceptsEmptyJobList(t *testing.T) {
	backupPath := filepath.Join(t.TempDir(), "backup.json")
	if err := os.WriteFile(backupPath, []byte("[]"), 0600); err != nil {
		t.Fatal(err)
	}
	jobs, err := loadBackup(backupPath)
	if err != nil {
		t.Fatalf("load valid empty backup: %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("jobs=%d, want 0", len(jobs))
	}
}

func TestBackupSourceSelection(t *testing.T) {
	directory := t.TempDir()
	backupPath := filepath.Join(directory, "backup.json")
	valid, err := json.Marshal([]models.Job{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backupPath, valid, 0600); err != nil {
		t.Fatal(err)
	}
	if source := findBackupSource(backupPath, filepath.Join(directory, "seed.json")); source != backupPath {
		t.Fatalf("mutable backup source=%q", source)
	}
	if source := findBackupSource(filepath.Join(directory, "missing.json"), ""); source != "" {
		t.Fatalf("empty seed source=%q", source)
	}
	if source := findBackupSource(filepath.Join(directory, "missing.json"), backupPath); source != backupPath {
		t.Fatalf("initial seed source=%q", source)
	}
	if source := findBackupSource("invalid\x00path", backupPath); source != "" {
		t.Fatalf("invalid backup path source=%q", source)
	}
	if err := os.Mkdir(filepath.Join(directory, "not-a-file"), 0700); err != nil {
		t.Fatal(err)
	}
	directoryPath := filepath.Join(directory, "not-a-file")
	if source := findBackupSource(directoryPath, backupPath); source != directoryPath {
		t.Fatalf("existing mutable source=%q", source)
	}
}

func TestRestoreAttachmentRestoresPresentAndSkipsMissingFiles(t *testing.T) {
	directory, statements, job, attachment := newAttachmentRestoreFixture(t)
	if err := restoreAttachment(statements.attachment, job, attachment, directory); err != nil {
		t.Fatalf("restore regular file: %v", err)
	}
	if err := restoreAttachment(statements.attachment, job, attachment, directory); err == nil {
		t.Fatal("duplicate attachment row should fail")
	}
	missing := attachment
	missing.ID, missing.StoredFilename = "missing", "missing.txt"
	if err := restoreAttachment(statements.attachment, job, missing, directory); err != nil {
		t.Fatalf("missing file should be skipped: %v", err)
	}
}

func TestRestoreRowsReportInsertFailures(t *testing.T) {
	directory := t.TempDir()
	db, err := InitDB(&config.Config{DBPath: filepath.Join(directory, "restore.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	statements, err := prepareRestoreStatements(mustBegin(t, db))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(statements.close)
	job := models.Job{ID: "job", CompanyName: "Example", PositionTitle: "Engineer", Status: models.StatusOngoing,
		Interviewers: []models.Interviewer{{Name: "Alex"}}}
	if err := restoreJobRow(statements.job, job); err != nil {
		t.Fatal(err)
	}
	if err := restoreJobRow(statements.job, job); err == nil {
		t.Fatal("duplicate job row should fail")
	}
	stage := models.Stage{ID: "stage", StageType: models.StageHR, Status: models.StageStatusCurrent, MeetingType: "video", RecruiterType: models.RecruiterNone,
		Interviewers: []models.Interviewer{{Name: "Taylor"}},
		Questions:    []models.Question{{ID: "question", Question: "First"}}}
	if err := restoreStage(statements, job.ID, stage); err != nil {
		t.Fatal(err)
	}
	duplicateQuestionStage := models.Stage{ID: "stage-2", StageType: models.StageTechnical, Status: models.StageStatusPending, MeetingType: "video", RecruiterType: models.RecruiterNone,
		Questions: []models.Question{{ID: "question", Question: "Duplicate"}}}
	if err := restoreStage(statements, job.ID, duplicateQuestionStage); err == nil {
		t.Fatal("duplicate question row should fail")
	}
}

func TestRestoreAttachmentRejectsUnsafeMetadata(t *testing.T) {
	directory, statements, job, attachment := newAttachmentRestoreFixture(t)
	wrongJob := attachment
	wrongJob.JobID = "other-job"
	wrongStage := attachment
	wrongStage.StageID = stringPointer("unknown-stage")
	negativeSize := attachment
	negativeSize.ID, negativeSize.FileSize = "negative", -1
	tests := []struct {
		name       string
		attachment models.Attachment
	}{
		{"unsafe path", withStoredFilename(attachment, "../resume.txt")},
		{"wrong job", wrongJob},
		{"wrong stage", wrongStage},
		{"negative size", negativeSize},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := restoreAttachment(statements.attachment, job, test.attachment, directory); err == nil {
				t.Fatal("invalid attachment should fail")
			}
		})
	}
}

func TestRestoreAttachmentRejectsDirectories(t *testing.T) {
	directory, statements, job, attachment := newAttachmentRestoreFixture(t)
	if err := os.Mkdir(filepath.Join(directory, "directory"), 0700); err != nil {
		t.Fatal(err)
	}
	attachment.ID, attachment.StoredFilename = "directory", "directory"
	if err := restoreAttachment(statements.attachment, job, attachment, directory); err == nil {
		t.Fatal("directory should not be restored as an attachment")
	}
}

func newAttachmentRestoreFixture(t *testing.T) (string, *restoreStatements, models.Job, models.Attachment) {
	t.Helper()
	directory := t.TempDir()
	db, err := InitDB(&config.Config{DBPath: filepath.Join(directory, "restore.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`INSERT INTO jobs (id, company_name, position_title, status, salary_type, salary_currency, recruiter_type, avatar_seed) VALUES ('job', 'Co', 'Role', 'ongoing', 'unknown', 'EUR', 'none', 'avatar')`); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "resume.txt"), []byte("resume"), 0600); err != nil {
		t.Fatal(err)
	}
	statements, err := prepareRestoreStatements(mustBegin(t, db))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(statements.close)
	job := models.Job{ID: "job", Stages: []models.Stage{{ID: "stage"}}}
	attachment := models.Attachment{ID: "attachment", JobID: "job", OriginalName: "resume.txt", StoredFilename: "resume.txt", FileSize: 6}
	return directory, statements, job, attachment
}

func TestRestoreJobReportsNestedRecordFailures(t *testing.T) {
	directory := t.TempDir()
	db, err := InitDB(&config.Config{DBPath: filepath.Join(directory, "restore.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	statements, err := prepareRestoreStatements(mustBegin(t, db))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(statements.close)
	base := models.Job{ID: "job", CompanyName: "Co", PositionTitle: "Role", Status: models.StatusOngoing, AvatarSeed: "avatar"}
	badStage := base
	badStage.Stages = []models.Stage{{ID: "bad-stage", Status: models.StageStatus("invalid")}}
	if err := restoreJob(statements, badStage, directory); err == nil {
		t.Fatal("invalid stage should fail")
	}
	base.ID = "question-job"
	base.Stages = []models.Stage{{ID: "stage", StageType: models.StageTechnical, Status: models.StageStatusPending, Questions: []models.Question{{ID: "question", Question: "Q", IsAsked: true}}}}
	if err := restoreJob(statements, base, directory); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreJobsCommitsEmptyAndPopulatedBackups(t *testing.T) {
	db, err := InitDB(&config.Config{DBPath: filepath.Join(t.TempDir(), "restore.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := restoreJobs(db, nil, t.TempDir()); err != nil {
		t.Fatalf("restore empty backup: %v", err)
	}
	job := models.Job{ID: "restored", CompanyName: "Example", PositionTitle: "Engineer", Status: models.StatusOngoing,
		Stages: []models.Stage{{ID: "restored-stage", StageType: models.StageHR, Status: models.StageStatusCurrent, MeetingType: "video", RecruiterType: models.RecruiterNone}}}
	if err := restoreJobs(db, []models.Job{job}, t.TempDir()); err != nil {
		t.Fatalf("restore job: %v", err)
	}
	assertRestoredInterviewersJSON(t, db, "jobs", job.ID)
	assertRestoredInterviewersJSON(t, db, "stages", job.Stages[0].ID)
	if err := restoreJobs(db, []models.Job{job}, t.TempDir()); err == nil {
		t.Fatal("duplicate job restore should roll back")
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM jobs").Scan(&count); err != nil || count != 1 {
		t.Fatalf("jobs after rolled back restore=%d err=%v", count, err)
	}
}

func assertRestoredInterviewersJSON(t *testing.T, db *sql.DB, table, id string) {
	t.Helper()
	var value string
	query := "SELECT interviewers_json FROM " + table + " WHERE id = ?"
	if err := db.QueryRow(query, id).Scan(&value); err != nil || value != "[]" {
		t.Fatalf("restored %s interviewers=%q err=%v", table, value, err)
	}
}

func TestRestoreJobsReturnsSetupErrors(t *testing.T) {
	db, err := InitDB(&config.Config{DBPath: filepath.Join(t.TempDir(), "restore.db")})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := restoreJobs(db, nil, t.TempDir()); err == nil {
		t.Fatal("closed database should fail restore transaction creation")
	}

	brokenDB, err := InitDB(&config.Config{DBPath: filepath.Join(t.TempDir(), "broken.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = brokenDB.Close() })
	if _, err := brokenDB.Exec("DROP TABLE attachments"); err != nil {
		t.Fatal(err)
	}
	if err := restoreJobs(brokenDB, nil, t.TempDir()); err == nil {
		t.Fatal("missing restore table should fail statement preparation")
	}
}

func TestRestoreStatementPreparationReportsMissingTables(t *testing.T) {
	for _, table := range []string{"jobs", "stages", "stage_questions", "attachments"} {
		t.Run(table, func(t *testing.T) {
			db, err := InitDB(&config.Config{DBPath: filepath.Join(t.TempDir(), "restore.db")})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })
			if _, err := db.Exec("DROP TABLE " + table); err != nil {
				t.Fatal(err)
			}
			tx := mustBegin(t, db)
			statements, err := prepareRestoreStatements(tx)
			if err == nil {
				statements.close()
				t.Fatal("expected statement preparation to fail")
			}
		})
	}
}

func TestAttachmentOwnerValidationBranches(t *testing.T) {
	job := models.Job{ID: "job", Stages: []models.Stage{{ID: "stage"}}}
	otherJob := models.Job{ID: "other-job"}
	validStage := "stage"
	unknownStage := "unknown-stage"
	tests := []struct {
		name       string
		job        models.Job
		attachment models.Attachment
		wantErr    bool
	}{
		{"job omitted", job, models.Attachment{}, false},
		{"matching job", job, models.Attachment{JobID: "job"}, false},
		{"wrong job", job, models.Attachment{JobID: "other-job"}, true},
		{"matching stage", job, models.Attachment{JobID: "job", StageID: &validStage}, false},
		{"unknown stage", job, models.Attachment{JobID: "job", StageID: &unknownStage}, true},
		{"no stages", otherJob, models.Attachment{StageID: &validStage}, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateAttachmentOwner(test.job, test.attachment)
			if (err != nil) != test.wantErr {
				t.Fatalf("validateAttachmentOwner() error=%v, want error=%t", err, test.wantErr)
			}
		})
	}
}

type malformedColumnInfo struct{ db *sql.DB }

func (executor malformedColumnInfo) Exec(query string, args ...any) (sql.Result, error) {
	return executor.db.Exec(query, args...)
}

func (executor malformedColumnInfo) Query(string, ...any) (*sql.Rows, error) {
	return executor.db.Query("SELECT 1")
}

func openMigrationTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name()))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func mustBegin(t *testing.T, db *sql.DB) *sql.Tx {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	return tx
}

func stringPointer(value string) *string { return &value }

func withStoredFilename(attachment models.Attachment, filename string) models.Attachment {
	attachment.StoredFilename = filename
	return attachment
}
