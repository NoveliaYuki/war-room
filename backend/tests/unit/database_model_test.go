package backend_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"war-room/backend/pkg/config"
	"war-room/backend/pkg/database"
	"war-room/backend/pkg/models"
)

// TestForeignKeysApplyToEveryPooledConnection verifies per-connection SQLite integrity.
func TestForeignKeysApplyToEveryPooledConnection(t *testing.T) {
	cfg := &config.Config{DBPath: filepath.Join(t.TempDir(), "integrity.db")}
	db, err := database.InitDB(cfg)
	if err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	t.Cleanup(func() { closeTestResource(t, db) })
	db.SetMaxOpenConns(4)
	connections := make([]*sql.Conn, 4)
	for index := range connections {
		connections[index], err = db.Conn(context.Background())
		if err != nil {
			t.Fatalf("acquire connection %d: %v", index, err)
		}
		t.Cleanup(func() { closeTestResource(t, connections[index]) })
	}
	for index, connection := range connections {
		var enabled int
		if err := connection.QueryRowContext(context.Background(), "PRAGMA foreign_keys").Scan(&enabled); err != nil {
			t.Fatalf("read foreign-key setting on connection %d: %v", index, err)
		}
		if enabled != 1 {
			t.Errorf("foreign keys disabled on pooled connection %d", index)
		}
	}
}

// TestVersionedMigrationRejectsNewerSchemas prevents running older code on newer data.
func TestVersionedMigrationRejectsNewerSchemas(t *testing.T) {
	cfg := &config.Config{DBPath: filepath.Join(t.TempDir(), "future.db")}
	db, err := database.InitDB(cfg)
	if err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	if _, err := db.Exec("INSERT INTO schema_migrations(version) VALUES (7)"); err != nil {
		t.Fatalf("insert future migration: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}
	if _, err := database.InitDB(cfg); err == nil || !strings.Contains(err.Error(), "newer than supported") {
		t.Fatalf("InitDB error = %v, want unsupported schema version", err)
	}
}

// TestPreparationFieldsMigrationPreservesExistingJobs adds notes without replacing legacy rows.
func TestPreparationFieldsMigrationPreservesExistingJobs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}
	_, err = legacy.Exec(`
		CREATE TABLE jobs (id TEXT PRIMARY KEY, company_name TEXT NOT NULL, position_title TEXT NOT NULL);
		CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL DEFAULT (unixepoch()));
		INSERT INTO jobs (id, company_name, position_title) VALUES ('legacy-job', 'Synthetic Co', 'Engineer');
		INSERT INTO schema_migrations (version) VALUES (3);
	`)
	if err != nil {
		t.Fatalf("prepare legacy database: %v", err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatalf("close legacy database: %v", err)
	}

	db, err := database.InitDB(&config.Config{DBPath: path})
	if err != nil {
		t.Fatalf("migrate legacy database: %v", err)
	}
	t.Cleanup(func() { closeTestResource(t, db) })
	var company, position, experience, expectedSalary, workArrangement, employmentType string
	err = db.QueryRow(`SELECT company_name, position_title, experience_notes, expected_salary, work_arrangement, employment_type FROM jobs WHERE id = ?`, "legacy-job").Scan(&company, &position, &experience, &expectedSalary, &workArrangement, &employmentType)
	if err != nil {
		t.Fatalf("read migrated job: %v", err)
	}
	if company != "Synthetic Co" || position != "Engineer" {
		t.Fatalf("migration changed existing job: company=%q position=%q", company, position)
	}
	if experience != "" || expectedSalary != "" {
		t.Fatalf("new notes should default to empty, got experience=%q expected_salary=%q", experience, expectedSalary)
	}
	assertLegacyWorkArrangementMigration(t, db, workArrangement)
	assertLegacyEmploymentTypeMigration(t, db, employmentType)
}

func assertLegacyWorkArrangementMigration(t *testing.T, db *sql.DB, workArrangement string) {
	t.Helper()
	if workArrangement != string(models.WorkArrangementUnknown) {
		t.Fatalf("work arrangement = %q, want unknown", workArrangement)
	}
	if _, err := db.Exec(`UPDATE jobs SET work_arrangement = 'invalid' WHERE id = ?`, "legacy-job"); err == nil {
		t.Fatal("work-arrangement migration did not constrain updates")
	}
}

func assertLegacyEmploymentTypeMigration(t *testing.T, db *sql.DB, employmentType string) {
	t.Helper()
	if employmentType != string(models.EmploymentTypeUnknown) {
		t.Fatalf("employment type = %q, want unknown", employmentType)
	}
	if _, err := db.Exec(`UPDATE jobs SET employment_type = 'invalid' WHERE id = ?`, "legacy-job"); err == nil {
		t.Fatal("employment-type migration did not constrain updates")
	}
}

func TestWorkArrangementMigrationReturnsConstraintFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "blocked-migration.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = legacy.Exec(`
		CREATE TABLE jobs (id TEXT PRIMARY KEY, work_arrangement TEXT);
		CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL DEFAULT (unixepoch()));
		INSERT INTO jobs (id, work_arrangement) VALUES ('legacy-job', 'invalid');
		INSERT INTO schema_migrations (version) VALUES (4);
		CREATE TRIGGER block_work_arrangement_migration
		BEFORE UPDATE OF work_arrangement ON jobs
		BEGIN SELECT RAISE(ABORT, 'blocked'); END;
	`)
	if err != nil {
		t.Fatalf("prepare legacy database: %v", err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := database.InitDB(&config.Config{DBPath: path}); err == nil {
		t.Fatal("expected migration to report the blocked normalization update")
	}
}

// TestBackupRestoreKeepsOnlyExistingAttachmentFiles validates complete relational restore.
func TestBackupRestoreKeepsOnlyExistingAttachmentFiles(t *testing.T) {
	directory := t.TempDir()
	attachmentsDirectory := filepath.Join(directory, "attachments")
	if err := os.MkdirAll(attachmentsDirectory, 0700); err != nil {
		t.Fatalf("create attachment directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(attachmentsDirectory, "resume.txt"), []byte("resume"), 0600); err != nil {
		t.Fatalf("create attachment fixture: %v", err)
	}
	stageID := "restore-stage"
	job := models.Job{
		ID: "restore-job", CompanyName: "Restore Co", PositionTitle: "Engineer",
		Status: models.StatusOngoing, SalaryType: models.SalaryUnknown,
		SalaryCurrency: "EUR", RecruiterType: models.RecruiterNone, AvatarSeed: "restore",
		Stages: []models.Stage{{
			ID: stageID, StageType: models.StageHR, Status: models.StageStatusCurrent,
			RecruiterType: models.RecruiterNone, Interviewers: []models.Interviewer{},
			Questions: []models.Question{{ID: "question-1", Question: "Ready?", IsAsked: true}},
		}},
		Attachments: []models.Attachment{
			{ID: "attachment-present", StageID: &stageID, OriginalName: "resume.txt", StoredFilename: "resume.txt", FileSize: 6, MimeType: "text/plain"},
			{ID: "attachment-missing", OriginalName: "missing.txt", StoredFilename: "missing.txt", FileSize: 4, MimeType: "text/plain"},
		},
	}
	backup, err := json.Marshal([]models.Job{job})
	if err != nil {
		t.Fatalf("encode backup fixture: %v", err)
	}
	backupPath := filepath.Join(directory, "backup.json")
	if err := os.WriteFile(backupPath, backup, 0600); err != nil {
		t.Fatalf("write backup fixture: %v", err)
	}
	cfg := &config.Config{
		DBPath: filepath.Join(directory, "restored.db"), BackupPath: backupPath,
		AttachmentsDir: attachmentsDirectory,
	}
	db, err := database.InitDB(cfg)
	if err != nil {
		t.Fatalf("initialize restored database: %v", err)
	}
	t.Cleanup(func() { closeTestResource(t, db) })
	assertRowCount(t, db, "jobs", 1)
	assertRowCount(t, db, "stages", 1)
	assertRowCount(t, db, "stage_questions", 1)
	assertRowCount(t, db, "attachments", 1)
	var storedPath string
	if err := db.QueryRow("SELECT absolute_path FROM attachments WHERE id = ?", "attachment-present").Scan(&storedPath); err != nil {
		t.Fatalf("read legacy attachment path: %v", err)
	}
	if storedPath != "resume.txt" {
		t.Fatalf("stored path = %q, want a volume-relative filename", storedPath)
	}
}

// TestBackupRestoreRollsBackInvalidRows preserves all-or-nothing recovery semantics.
func TestBackupRestoreRollsBackInvalidRows(t *testing.T) {
	directory := t.TempDir()
	jobs := []models.Job{
		{ID: "valid-first", CompanyName: "Valid", PositionTitle: "Engineer", Status: models.StatusOngoing, AvatarSeed: "valid"},
		{ID: "invalid-second", CompanyName: "Invalid", PositionTitle: "Engineer", Status: "unknown", AvatarSeed: "invalid"},
	}
	backup, err := json.Marshal(jobs)
	if err != nil {
		t.Fatalf("encode backup fixture: %v", err)
	}
	backupPath := filepath.Join(directory, "backup.json")
	if err := os.WriteFile(backupPath, backup, 0600); err != nil {
		t.Fatalf("write backup fixture: %v", err)
	}
	db, err := database.InitDB(&config.Config{
		DBPath: filepath.Join(directory, "restore.db"), BackupPath: backupPath,
	})
	if err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	t.Cleanup(func() { closeTestResource(t, db) })
	assertRowCount(t, db, "jobs", 0)
}

func assertRowCount(t *testing.T, db *sql.DB, table string, expected int) {
	t.Helper()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if count != expected {
		t.Fatalf("%s count = %d, want %d", table, count, expected)
	}
}
