package database

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
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

func TestAddCVVersionJobReferenceLeavesExistingColumnUntouched(t *testing.T) {
	db := openMigrationTestDB(t)
	if _, err := db.Exec(schemaSQL); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`ALTER TABLE jobs ADD COLUMN cv_version_id TEXT NULL`); err != nil {
		t.Fatal(err)
	}
	if err := addCVVersionJobReference(db); err != nil {
		t.Fatalf("existing jobs.cv_version_id should be accepted: %v", err)
	}
}

func TestSearchPeriodMigrationPreservesExistingJobsAsUnassigned(t *testing.T) {
	db := openMigrationTestDB(t)
	if _, err := db.Exec(schemaSQL); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO jobs (id, position_title, status, salary_type, recruiter_type, avatar_seed) VALUES ('legacy-job', 'Engineer', 'waiting', 'unknown', 'none', 'seed')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO schema_migrations (version) VALUES (1),(2),(3),(4),(5),(6),(7),(8),(9),(10),(11)"); err != nil {
		t.Fatal(err)
	}
	if err := migrateSchema(db); err != nil {
		t.Fatalf("apply search period migration: %v", err)
	}
	var periodID sql.NullString
	if err := db.QueryRow("SELECT search_period_id FROM jobs WHERE id = 'legacy-job'").Scan(&periodID); err != nil {
		t.Fatal(err)
	}
	if periodID.Valid {
		t.Fatalf("legacy job should remain unassigned, got %q", periodID.String)
	}
}

func TestInitDBReturnsDatabaseOpenErrors(t *testing.T) {
	openError := errors.New("driver unavailable")
	_, err := initDBWithOpener(&config.Config{DBPath: filepath.Join(t.TempDir(), "db.sqlite")}, func(string, string) (*sql.DB, error) {
		return nil, openError
	})
	if !errors.Is(err, openError) {
		t.Fatalf("database open error=%v, want wrapped opener error", err)
	}
}

func TestInitDBReturnsSchemaExecutionErrors(t *testing.T) {
	cfg := &config.Config{DBPath: filepath.Join(t.TempDir(), "readonly.db")}
	setup, err := sql.Open("sqlite", sqliteDataSource(cfg.DBPath))
	if err != nil {
		t.Fatal(err)
	}
	if err := setup.Ping(); err != nil {
		t.Fatal(err)
	}
	if err := setup.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = initDBWithOpener(cfg, func(_, source string) (*sql.DB, error) {
		return sql.Open("sqlite", source+"&mode=ro")
	})
	if err == nil || !strings.Contains(err.Error(), "failed to execute schema") {
		t.Fatalf("schema execution error=%v", err)
	}
}

func TestRestoreRecoveryCVSnapshotValidatesBlobAndSequence(t *testing.T) {
	for _, test := range []struct {
		name     string
		filename string
		content  string
		wantErr  bool
	}{
		{name: "unsafe filename", filename: "../escape", content: "cv", wantErr: true},
		{name: "missing blob", filename: strings.Repeat("a", 64), content: "", wantErr: true},
		{name: "checksum mismatch", filename: strings.Repeat("a", 64), content: "wrong", wantErr: true},
		{name: "valid", filename: strings.Repeat("a", 64), content: "cv"},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, cvDir := recoveryDatabase(t)
			content := []byte(test.content)
			if test.name != "missing blob" && test.filename != "../escape" {
				if err := os.WriteFile(filepath.Join(cvDir, test.filename), content, 0600); err != nil {
					t.Fatal(err)
				}
			}
			digest := hex.EncodeToString(sha256.New().Sum(nil))
			if test.name == "valid" {
				digestBytes := sha256.Sum256(content)
				digest = hex.EncodeToString(digestBytes[:])
			}
			snapshot := models.RecoverySnapshot{NextCVVersion: 8, CVVersions: []models.CVVersionSnapshot{{ID: "cv-1", Version: 3, OriginalName: "resume.pdf", StoredFilename: test.filename, FileSize: int64(len(content)), SHA256: digest, UploadedAt: 42}}}
			err := restoreRecoverySnapshot(db, snapshot, t.TempDir(), cvDir)
			if (err != nil) != test.wantErr {
				t.Fatalf("restore error=%v, want error=%t", err, test.wantErr)
			}
			if !test.wantErr {
				var next int
				if err := db.QueryRow(`SELECT next_version FROM cv_version_sequence WHERE id = 1`).Scan(&next); err != nil || next != 8 {
					t.Fatalf("next version=%d err=%v", next, err)
				}
			}
		})
	}
}

func TestRestoreRecoveryCVSnapshotRollsBackFailedWrites(t *testing.T) {
	db, directory := recoveryDatabase(t)
	content := []byte("cv")
	digestBytes := sha256.Sum256(content)
	digest := hex.EncodeToString(digestBytes[:])
	if err := os.WriteFile(filepath.Join(directory, digest), content, 0600); err != nil {
		t.Fatal(err)
	}
	snapshot := models.RecoverySnapshot{Jobs: []models.Job{}, CVVersions: []models.CVVersionSnapshot{{ID: "cv", Version: 1, OriginalName: "resume.pdf", StoredFilename: digest, FileSize: 2, SHA256: digest}}}
	if err := restoreRecoverySnapshot(db, snapshot, t.TempDir(), directory); err != nil {
		t.Fatal(err)
	}
	if err := restoreRecoverySnapshot(db, snapshot, t.TempDir(), directory); err == nil {
		t.Fatal("duplicate CV metadata should fail restore")
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM cv_versions`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("versions after rollback=%d err=%v", count, err)
	}
	sequenceDB, _ := recoveryDatabase(t)
	if _, err := sequenceDB.Exec(`DROP TABLE cv_version_sequence`); err != nil {
		t.Fatal(err)
	}
	if err := restoreRecoverySnapshot(sequenceDB, models.RecoverySnapshot{Jobs: []models.Job{}}, t.TempDir(), directory); err == nil {
		t.Fatal("missing sequence table should fail restore")
	}
}

func recoveryDatabase(t *testing.T) (*sql.DB, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{DataDir: dir, DBPath: filepath.Join(dir, "recovery.sqlite"), BackupPath: filepath.Join(dir, "backup.json"),
		AttachmentsDir: filepath.Join(dir, "attachments"), CVDir: filepath.Join(dir, "cvs"), LogosDir: filepath.Join(dir, "logos")}
	db, err := InitDB(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cfg.CVDir, 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, cfg.CVDir
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

func TestRunCVSchemaMigrationReportsStepFailures(t *testing.T) {
	for _, test := range []struct {
		name, failExec, failQuery string
	}{
		{name: "CV table", failExec: "CREATE TABLE IF NOT EXISTS cv_versions"},
		{name: "sequence table", failExec: "CREATE TABLE IF NOT EXISTS cv_version_sequence"},
		{name: "inspect job column", failQuery: "pragma_table_info"},
		{name: "job reference", failExec: "ALTER TABLE jobs ADD COLUMN cv_version_id"},
		{name: "version index", failExec: "idx_cv_versions_version"},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := openMigrationTestDB(t)
			if _, err := db.Exec("CREATE TABLE jobs (id TEXT PRIMARY KEY)"); err != nil {
				t.Fatal(err)
			}
			executor := migrationTestExecutor{db: db, failExec: test.failExec, failQuery: test.failQuery}
			if err := runSchemaMigration(executor, 7); err == nil {
				t.Fatal("migration step failure should be returned")
			}
		})
	}
}

func TestTechnologyCatalogMigrationPreservesExistingJobs(t *testing.T) {
	db := openMigrationTestDB(t)
	if _, err := db.Exec(`CREATE TABLE jobs (id TEXT PRIMARY KEY); INSERT INTO jobs (id) VALUES ('existing-job')`); err != nil {
		t.Fatal(err)
	}
	if err := migrateTechnologyCatalog(db); err != nil {
		t.Fatalf("migrate technology catalog: %v", err)
	}
	var jobCount, tableCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM jobs`).Scan(&jobCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name IN ('technologies', 'technology_aliases', 'job_technologies')`).Scan(&tableCount); err != nil {
		t.Fatal(err)
	}
	if jobCount != 1 || tableCount != 3 {
		t.Fatalf("preserved jobs=%d new tables=%d", jobCount, tableCount)
	}
}

func TestTechnologyCatalogMigrationReportsSQLFailure(t *testing.T) {
	db := openMigrationTestDB(t)
	if _, err := db.Exec(`CREATE TABLE jobs (id TEXT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	executor := migrationTestExecutor{db: db, failExec: "technology_aliases"}
	if err := runSchemaMigration(executor, 8); err == nil {
		t.Fatal("catalog migration SQL failure should be returned")
	}
}

func TestWaitingStatusMigrationPreservesRelatedRecords(t *testing.T) {
	db := prepareWaitingStatusMigrationDB(t)
	insertWaitingMigrationRecords(t, db)
	if err := applyWaitingStatusMigration(db, 9); err != nil {
		t.Fatalf("apply waiting status migration: %v", err)
	}
	assertWaitingMigrationRecords(t, db)
}

func TestWaitingStatusMigrationReportsConnectionAndSchemaErrors(t *testing.T) {
	db := openMigrationTestDB(t)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := applyWaitingStatusMigration(db, 9); err == nil {
		t.Fatal("closed database should fail to acquire a migration connection")
	}

	db = openMigrationTestDB(t)
	if err := applyWaitingStatusMigration(db, 9); err == nil {
		t.Fatal("missing jobs table should fail the waiting status migration")
	}
}

func TestWaitingStatusMigrationAddsStatusWhenColumnIsMissing(t *testing.T) {
	db := openMigrationTestDB(t)
	if _, err := db.Exec(`CREATE TABLE jobs (id TEXT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if err := migrateJobWaitingStatus(db); err != nil {
		t.Fatalf("add waiting job status: %v", err)
	}
	if hasStatus, err := hasColumn(db, "jobs", "status"); err != nil || !hasStatus {
		t.Fatalf("jobs.status exists=%t err=%v", hasStatus, err)
	}
}

func TestHasColumnTreatsIdentifiersAsValues(t *testing.T) {
	db := openMigrationTestDB(t)
	if _, err := db.Exec("CREATE TABLE jobs (id TEXT PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}

	for _, input := range []struct{ table, column string }{
		{table: "jobs); SELECT 1; --", column: "id"},
		{table: "jobs", column: "id' OR 1=1 --"},
	} {
		exists, err := hasColumn(db, input.table, input.column)
		if err != nil || exists {
			t.Fatalf("hasColumn(%q, %q) = %t, %v; want false, nil", input.table, input.column, exists, err)
		}
	}
}

func TestWaitingStatusMigrationReportsMigrationRecordFailure(t *testing.T) {
	db := prepareWaitingStatusMigrationDB(t)
	if _, err := db.Exec(`CREATE TRIGGER fail_waiting_migration_record BEFORE INSERT ON schema_migrations WHEN NEW.version = 9 BEGIN SELECT RAISE(ABORT, 'injected migration record failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := applyWaitingStatusMigration(db, 9); err == nil {
		t.Fatal("migration record failure should be returned")
	}
}

func TestWaitingStatusMigrationReturnsInspectionAndSQLFailures(t *testing.T) {
	db := openMigrationTestDB(t)
	if err := runSchemaMigration(migrationTestExecutor{db: db, failQuery: "pragma_table_info"}, 9); err == nil {
		t.Fatal("schema inspection failure should be returned")
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		t.Fatal(err)
	}
	executor := migrationTestExecutor{db: db, failExec: "DROP TRIGGER IF EXISTS validate_employment_type_insert"}
	if err := runSchemaMigration(executor, 9); err == nil {
		t.Fatal("waiting status migration SQL failure should be returned")
	}
}

func prepareWaitingStatusMigrationDB(t *testing.T) *sql.DB {
	t.Helper()
	databasePath := filepath.Join(t.TempDir(), "waiting-migration.db")
	db, err := sql.Open("sqlite", sqliteDataSource(databasePath))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	legacySchema := strings.Replace(schemaSQL, "('waiting', 'ongoing', 'accepted', 'rejected')", "('ongoing', 'accepted', 'rejected')", 1)
	if _, err := db.Exec(legacySchema); err != nil {
		t.Fatalf("create legacy schema: %v", err)
	}
	for version := 1; version <= 8; version++ {
		if err := applySchemaMigration(db, version); err != nil {
			t.Fatalf("apply legacy schema migration %d: %v", version, err)
		}
	}
	return db
}

func insertWaitingMigrationRecords(t *testing.T, db *sql.DB) {
	t.Helper()
	statements := []string{
		`INSERT INTO cv_versions (id, version, original_name, stored_filename, file_size, mime_type, sha256, uploaded_at) VALUES ('cv-1', 1, 'resume.pdf', 'resume.pdf', 1, 'application/pdf', 'digest', 1)`,
		`INSERT INTO jobs (id, company_name, position_title, status, salary_type, salary_currency, recruiter_type, avatar_seed, cv_version_id) VALUES ('job-1', 'Example', 'Engineer', 'ongoing', 'unknown', 'EUR', 'none', 'avatar', 'cv-1')`,
		`INSERT INTO stages (id, job_id, order_index, stage_type, status, meeting_type) VALUES ('stage-1', 'job-1', 0, 'HR', 'current', 'video')`,
		`INSERT INTO stage_questions (id, stage_id, order_index, question) VALUES ('question-1', 'stage-1', 0, 'Question?')`,
		`INSERT INTO attachments (id, job_id, stage_id, original_name, stored_filename, absolute_path, file_size, mime_type) VALUES ('attachment-1', 'job-1', 'stage-1', 'note.txt', 'note.txt', '/data/attachments/note.txt', 1, 'text/plain')`,
		`INSERT INTO technologies (id, name, normalized_name) VALUES ('tech-1', 'Go', 'go')`,
		`INSERT INTO job_technologies (job_id, technology_id) VALUES ('job-1', 'tech-1')`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("insert related migration fixture: %v", err)
		}
	}
}

func assertWaitingMigrationRecords(t *testing.T, db *sql.DB) {
	t.Helper()
	for table, want := range map[string]int{"stages": 1, "stage_questions": 1, "attachments": 1, "job_technologies": 1} {
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != want {
			t.Fatalf("%s count=%d err=%v, want %d", table, count, err, want)
		}
	}
	var cvVersionID string
	if err := db.QueryRow(`SELECT cv_version_id FROM jobs WHERE id = 'job-1'`).Scan(&cvVersionID); err != nil || cvVersionID != "cv-1" {
		t.Fatalf("job CV version=%q err=%v", cvVersionID, err)
	}
	var violations int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&violations); err != nil || violations != 0 {
		t.Fatalf("foreign-key violations=%d err=%v", violations, err)
	}
}

func TestRecoverySnapshotRestoresTechnologyCatalogAndAssignments(t *testing.T) {
	cfg := &config.Config{DBPath: filepath.Join(t.TempDir(), "technology-recovery.db")}
	db, err := InitDB(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	technology := models.Technology{ID: "tech-kubernetes", Name: "Kubernetes", Aliases: []string{"K8s"}}
	job := models.Job{ID: "job-1", CompanyName: "Example", PositionTitle: "Engineer", Status: models.StatusOngoing,
		SalaryType: models.SalaryUnknown, SalaryCurrency: "EUR", RecruiterType: models.RecruiterNone, AvatarSeed: "seed",
		WorkArrangement: models.WorkArrangementUnknown, EmploymentType: models.EmploymentTypeUnknown,
		Technologies: []models.Technology{technology}}
	if err := restoreRecoverySnapshot(db, models.RecoverySnapshot{Jobs: []models.Job{job}, Technologies: []models.Technology{technology}}, "", ""); err != nil {
		t.Fatalf("restore recovery snapshot: %v", err)
	}
	var alias, canonical string
	if err := db.QueryRow(`SELECT t.name, a.alias FROM technologies t JOIN technology_aliases a ON a.technology_id = t.id JOIN job_technologies jt ON jt.technology_id = t.id WHERE jt.job_id = ?`, job.ID).Scan(&canonical, &alias); err != nil {
		t.Fatal(err)
	}
	if canonical != "Kubernetes" || alias != "K8s" {
		t.Fatalf("restored canonical=%q alias=%q", canonical, alias)
	}
}

func TestRecoverySnapshotRestoresSearchPeriodsAndAssignments(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{DataDir: dir, DBPath: filepath.Join(dir, "recovery-search-period.sqlite"),
		BackupPath: filepath.Join(dir, "backup.json"), AttachmentsDir: filepath.Join(dir, "attachments"),
		CVDir: filepath.Join(dir, "cvs"), LogosDir: filepath.Join(dir, "logos")}
	db, err := InitDB(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	periodID := "period-march"
	job := models.Job{ID: "job-march", CompanyName: "Example", PositionTitle: "Engineer", Status: models.StatusWaiting,
		SalaryType: models.SalaryUnknown, SalaryCurrency: "EUR", RecruiterType: models.RecruiterNone,
		SearchPeriodID: &periodID}
	period := models.SearchPeriod{ID: periodID, Name: "March 2026", StartDate: "2026-03-01", EndDate: "2026-03-31"}
	if err := restoreRecoverySnapshot(db, models.RecoverySnapshot{Jobs: []models.Job{job}, SearchPeriods: []models.SearchPeriod{period}}, cfg.AttachmentsDir, cfg.CVDir); err != nil {
		t.Fatalf("restore search period snapshot: %v", err)
	}
	var got string
	if err := db.QueryRow("SELECT search_period_id FROM jobs WHERE id = ?", job.ID).Scan(&got); err != nil || got != periodID {
		t.Fatalf("restored period assignment=%q err=%v", got, err)
	}
}

func TestRecoverySnapshotRollsBackInvalidTechnologyReferences(t *testing.T) {
	cfg := &config.Config{DBPath: filepath.Join(t.TempDir(), "invalid-technology-recovery.db")}
	db, err := InitDB(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	technology := models.Technology{ID: "tech-one", Name: "One", Aliases: []string{"Same"}}
	duplicateAlias := models.Technology{ID: "tech-two", Name: "Two", Aliases: []string{"Same"}}
	err = restoreRecoverySnapshot(db, models.RecoverySnapshot{Technologies: []models.Technology{technology, duplicateAlias}}, "", "")
	if err == nil {
		t.Fatal("duplicate alias should fail recovery")
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM technologies`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rolled back technology count=%d err=%v", count, err)
	}
}

func TestRecoverySnapshotRejectsDuplicateCatalogIDs(t *testing.T) {
	db, err := InitDB(&config.Config{DBPath: filepath.Join(t.TempDir(), "duplicate-technology.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	technologies := []models.Technology{{ID: "same", Name: "One"}, {ID: "same", Name: "Two"}}
	if err := restoreRecoverySnapshot(db, models.RecoverySnapshot{Technologies: technologies}, "", ""); err == nil {
		t.Fatal("duplicate catalog IDs should fail recovery")
	}
}

func TestRecoverySnapshotRejectsUnknownJobTechnology(t *testing.T) {
	db, err := InitDB(&config.Config{DBPath: filepath.Join(t.TempDir(), "unknown-job-technology.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	job := models.Job{ID: "job-1", CompanyName: "Example", PositionTitle: "Engineer", Status: models.StatusOngoing,
		SalaryType: models.SalaryUnknown, SalaryCurrency: "EUR", RecruiterType: models.RecruiterNone, AvatarSeed: "seed",
		WorkArrangement: models.WorkArrangementUnknown, EmploymentType: models.EmploymentTypeUnknown,
		Technologies: []models.Technology{{ID: "missing-tech", Name: "Missing"}}}
	if err := restoreRecoverySnapshot(db, models.RecoverySnapshot{Jobs: []models.Job{job}}, "", ""); err == nil {
		t.Fatal("unknown technology assignment should fail recovery")
	}
}

func TestPrepareRestoreStatementsReportsMissingTechnologyRelation(t *testing.T) {
	db, err := InitDB(&config.Config{DBPath: filepath.Join(t.TempDir(), "prepare-technology-statements.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`DROP TABLE job_technologies`); err != nil {
		t.Fatal(err)
	}
	tx := mustBegin(t, db)
	defer func() { _ = tx.Rollback() }()
	if _, err := prepareRestoreStatements(tx); err == nil {
		t.Fatal("missing job technology relation should fail restore preparation")
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
	if err := applyWaitingStatusMigration(closedDB, 9); err == nil {
		t.Fatal("closed database should fail acquiring the waiting migration connection")
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
	executor := migrationTestExecutor{db: db, failExec: "CREATE TRIGGER"}
	for _, version := range []int{2, 3} {
		if err := runSchemaMigration(executor, version); err == nil {
			t.Errorf("migration %d should report trigger creation failure", version)
		}
	}
	if err := runSchemaMigration(executor, 99); err == nil || !strings.Contains(err.Error(), "unknown migration") {
		t.Fatalf("unknown migration error=%v", err)
	}
	if err := applyInitialSchemaMigration(migrationTestExecutor{db: db, failExec: "CREATE INDEX"}); err == nil {
		t.Fatal("initial migration should report index creation failure")
	}
	dispatchDB := openMigrationTestDB(t)
	if _, err := dispatchDB.Exec(schemaSQL); err != nil {
		t.Fatal(err)
	}
	for _, version := range []int{4, 5, 6, 7, 8, 9, 10, 11, 12} {
		if err := runSchemaMigration(dispatchDB, version); err != nil {
			t.Errorf("migration %d dispatch failed: %v", version, err)
		}
	}
	if err := applyWaitingStatusMigration(dispatchDB, 9); err != nil {
		t.Fatalf("waiting status migration transaction: %v", err)
	}
}

func TestStatusTimestampMigrationPreservesLegacyUpdateTime(t *testing.T) {
	db := openMigrationTestDB(t)
	if _, err := db.Exec("CREATE TABLE jobs (id TEXT PRIMARY KEY, status TEXT, updated_at INTEGER)"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO jobs (id, status, updated_at) VALUES ('legacy', 'waiting', 123)"); err != nil {
		t.Fatal(err)
	}
	if err := migrateJobStatusChangedAt(db); err != nil {
		t.Fatalf("add status timestamp: %v", err)
	}
	var changedAt int64
	if err := db.QueryRow("SELECT status_changed_at FROM jobs WHERE id = 'legacy'").Scan(&changedAt); err != nil || changedAt != 123 {
		t.Fatalf("status timestamp=%d err=%v, want legacy update time 123", changedAt, err)
	}
}

func TestStatusTimestampMigrationSupportsJobsWithoutUpdateTime(t *testing.T) {
	db := openMigrationTestDB(t)
	if _, err := db.Exec("CREATE TABLE jobs (id TEXT PRIMARY KEY, status TEXT)"); err != nil {
		t.Fatal(err)
	}
	if err := migrateJobStatusChangedAt(db); err != nil {
		t.Fatalf("add status timestamp without legacy updated_at: %v", err)
	}
	if exists, err := hasColumn(db, "jobs", "status_changed_at"); err != nil || !exists {
		t.Fatalf("status_changed_at exists=%t err=%v", exists, err)
	}
}

func TestProcessDateMigrationAddsNullableColumnsIdempotently(t *testing.T) {
	db := openMigrationTestDB(t)
	if _, err := db.Exec("CREATE TABLE jobs (id TEXT PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	if err := migrateJobProcessDates(db); err != nil {
		t.Fatalf("add process dates: %v", err)
	}
	if err := migrateJobProcessDates(db); err != nil {
		t.Fatalf("repeat process date migration: %v", err)
	}
	for _, column := range []string{"application_sent_date", "recruiter_first_contact_date"} {
		exists, err := hasColumn(db, "jobs", column)
		if err != nil || !exists {
			t.Fatalf("column %s exists=%t err=%v", column, exists, err)
		}
	}
}

func TestProcessDateMigrationsReturnInspectionAndAlterErrors(t *testing.T) {
	db := openMigrationTestDB(t)
	if _, err := db.Exec("CREATE TABLE jobs (id TEXT PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	if err := migrateJobStatusChangedAt(migrationTestExecutor{db: db, failQuery: "pragma_table_info"}); err == nil {
		t.Fatal("status timestamp inspection failure should be returned")
	}
	if err := migrateJobStatusChangedAt(migrationTestExecutor{db: db, failExec: "ADD COLUMN status_changed_at"}); err == nil {
		t.Fatal("status timestamp alter failure should be returned")
	}
	if err := migrateJobProcessDates(migrationTestExecutor{db: db, failQuery: "pragma_table_info"}); err == nil {
		t.Fatal("process date inspection failure should be returned")
	}
	if err := migrateJobProcessDates(migrationTestExecutor{db: db, failExec: "application_sent_date"}); err == nil {
		t.Fatal("process date alter failure should be returned")
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

func TestHasColumnReportsQueryFailure(t *testing.T) {
	db := openMigrationTestDB(t)
	if _, err := db.Exec("CREATE TABLE jobs (id TEXT PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	_, err := hasColumn(migrationTestExecutor{db: db, failQuery: "pragma_table_info"}, "jobs", "id")
	if err == nil {
		t.Fatal("expected query failure")
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

func TestRestoreJobsDefaultsMissingStatusTimestampToCreationTime(t *testing.T) {
	db, err := InitDB(&config.Config{DBPath: filepath.Join(t.TempDir(), "restore-time.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	job := models.Job{ID: "legacy-restored", CompanyName: "Example", PositionTitle: "Engineer", Status: models.StatusWaiting, CreatedAt: 456}
	if err := restoreJobs(db, []models.Job{job}, t.TempDir()); err != nil {
		t.Fatalf("restore legacy job: %v", err)
	}
	var statusChangedAt int64
	if err := db.QueryRow("SELECT status_changed_at FROM jobs WHERE id = ?", job.ID).Scan(&statusChangedAt); err != nil || statusChangedAt != job.CreatedAt {
		t.Fatalf("status_changed_at=%d err=%v, want created_at %d", statusChangedAt, err, job.CreatedAt)
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
