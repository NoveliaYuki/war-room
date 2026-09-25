package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
	"war-room/backend/pkg/config"
	"war-room/backend/pkg/models"
)

const schemaSQL = `
CREATE TABLE IF NOT EXISTS jobs (
    id TEXT PRIMARY KEY,
    company_name TEXT NOT NULL DEFAULT 'Unknown',
    position_title TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('ongoing', 'accepted', 'rejected')),
    salary_type TEXT NOT NULL CHECK (salary_type IN ('limited', 'no_min', 'no_max', 'unknown')),
    salary_min INTEGER NULL,
    salary_max INTEGER NULL,
    salary_currency TEXT NOT NULL DEFAULT 'EUR',
    recruiter_type TEXT NOT NULL DEFAULT 'none' CHECK (recruiter_type IN ('internal', 'external', 'none')),
    recruiter_name TEXT NULL,
    recruiter_agency TEXT NULL,
    recruiter_contact TEXT NULL,
    interviewers_json TEXT NOT NULL DEFAULT '[]',
    job_post_url TEXT NULL,
    avatar_seed TEXT NOT NULL,
    keyword_note TEXT NOT NULL DEFAULT '' CHECK (length(keyword_note) <= 100),
    description TEXT NOT NULL DEFAULT '',
    company_overview TEXT NOT NULL DEFAULT '',
    company_domain TEXT NULL,
    interview_notes TEXT NOT NULL DEFAULT '',
    reasons_to_change TEXT NOT NULL DEFAULT '',
    experience_notes TEXT NOT NULL DEFAULT '',
    expected_salary TEXT NOT NULL DEFAULT '',
    work_arrangement TEXT NOT NULL DEFAULT 'unknown' CHECK (work_arrangement IN ('unknown', 'remote', 'hybrid', 'on_site')),
    is_referral INTEGER NOT NULL DEFAULT 0 CHECK (is_referral IN (0, 1)),
    order_index INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL DEFAULT (unixepoch()),
    updated_at INTEGER NOT NULL DEFAULT (unixepoch()),
    CHECK (salary_min IS NULL OR salary_max IS NULL OR salary_min <= salary_max)
);

CREATE TABLE IF NOT EXISTS stages (
    id TEXT PRIMARY KEY,
    job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    order_index INTEGER NOT NULL,
    stage_type TEXT NOT NULL CHECK (stage_type IN ('HR', 'Technical', 'Cultural', 'Offer & Decision')),
    custom_title TEXT NULL,
    description TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'current', 'completed', 'skipped')),
    scheduled_at INTEGER NULL,
    meeting_date TEXT NULL,
    meeting_time TEXT NULL,
    meeting_url TEXT NULL,
    meeting_type TEXT NOT NULL DEFAULT 'video' CHECK (meeting_type IN ('video', 'phone', 'onsite')),
    notes TEXT NOT NULL DEFAULT '',
    recruiter_name TEXT NULL,
    recruiter_type TEXT NOT NULL DEFAULT 'none' CHECK (recruiter_type IN ('internal', 'external', 'none')),
    recruiter_agency TEXT NULL,
    recruiter_contact TEXT NULL,
    interviewers_json TEXT NOT NULL DEFAULT '[]',
    created_at INTEGER NOT NULL DEFAULT (unixepoch())
);

CREATE TABLE IF NOT EXISTS stage_questions (
    id TEXT PRIMARY KEY,
    stage_id TEXT NOT NULL REFERENCES stages(id) ON DELETE CASCADE,
    order_index INTEGER NOT NULL,
    question TEXT NOT NULL,
    answer_notes TEXT NOT NULL DEFAULT '',
    is_asked INTEGER NOT NULL DEFAULT 0 CHECK (is_asked IN (0, 1)),
    created_at INTEGER NOT NULL DEFAULT (unixepoch())
);

CREATE TABLE IF NOT EXISTS attachments (
    id TEXT PRIMARY KEY,
    job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    stage_id TEXT NULL REFERENCES stages(id) ON DELETE SET NULL,
    original_name TEXT NOT NULL,
    stored_filename TEXT NOT NULL UNIQUE,
    absolute_path TEXT NOT NULL,
    file_size INTEGER NOT NULL CHECK (file_size >= 0),
    mime_type TEXT NOT NULL DEFAULT 'application/octet-stream',
    created_at INTEGER NOT NULL DEFAULT (unixepoch())
);

CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at INTEGER NOT NULL DEFAULT (unixepoch())
);
`

const schemaIndexesSQL = `
DROP INDEX IF EXISTS idx_stages_job_id;
DROP INDEX IF EXISTS idx_stage_questions_stage_id;
DROP INDEX IF EXISTS idx_attachments_job_id;
DROP INDEX IF EXISTS idx_jobs_status;
DROP INDEX IF EXISTS idx_jobs_order_index;
CREATE TRIGGER IF NOT EXISTS validate_jobs_insert
BEFORE INSERT ON jobs
WHEN (NEW.is_referral NOT IN (0, 1))
  OR (NEW.salary_min IS NOT NULL AND NEW.salary_max IS NOT NULL AND NEW.salary_min > NEW.salary_max)
BEGIN
    SELECT RAISE(ABORT, 'invalid job boolean or salary range');
END;
CREATE TRIGGER IF NOT EXISTS validate_jobs_update
BEFORE UPDATE OF is_referral, salary_min, salary_max ON jobs
WHEN (NEW.is_referral NOT IN (0, 1))
  OR (NEW.salary_min IS NOT NULL AND NEW.salary_max IS NOT NULL AND NEW.salary_min > NEW.salary_max)
BEGIN
    SELECT RAISE(ABORT, 'invalid job boolean or salary range');
END;
CREATE TRIGGER IF NOT EXISTS validate_stages_insert
BEFORE INSERT ON stages
WHEN NEW.recruiter_type NOT IN ('internal', 'external', 'none')
BEGIN
    SELECT RAISE(ABORT, 'invalid stage recruiter type');
END;
CREATE TRIGGER IF NOT EXISTS validate_stages_update
BEFORE UPDATE OF recruiter_type ON stages
WHEN NEW.recruiter_type NOT IN ('internal', 'external', 'none')
BEGIN
    SELECT RAISE(ABORT, 'invalid stage recruiter type');
END;
CREATE TRIGGER IF NOT EXISTS validate_questions_insert
BEFORE INSERT ON stage_questions
WHEN NEW.is_asked NOT IN (0, 1)
BEGIN
    SELECT RAISE(ABORT, 'is_asked must be 0 or 1');
END;
CREATE TRIGGER IF NOT EXISTS validate_questions_update
BEFORE UPDATE OF is_asked ON stage_questions
WHEN NEW.is_asked NOT IN (0, 1)
BEGIN
    SELECT RAISE(ABORT, 'is_asked must be 0 or 1');
END;
CREATE TRIGGER IF NOT EXISTS validate_attachments_insert
BEFORE INSERT ON attachments
WHEN NEW.file_size < 0
BEGIN
    SELECT RAISE(ABORT, 'file_size cannot be negative');
END;
CREATE TRIGGER IF NOT EXISTS validate_attachments_update
BEFORE UPDATE OF file_size ON attachments
WHEN NEW.file_size < 0
BEGIN
    SELECT RAISE(ABORT, 'file_size cannot be negative');
END;
CREATE INDEX IF NOT EXISTS idx_stages_job_order ON stages(job_id, order_index);
CREATE INDEX IF NOT EXISTS idx_stages_job_status_order ON stages(job_id, status, order_index);
CREATE INDEX IF NOT EXISTS idx_stage_questions_stage_order ON stage_questions(stage_id, order_index);
CREATE INDEX IF NOT EXISTS idx_attachments_job_created ON attachments(job_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_jobs_status_order ON jobs(status, order_index, updated_at DESC, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_jobs_order_updated ON jobs(order_index, updated_at DESC, created_at DESC);
`

// InitDB opens SQLite, applies versioned schema migrations, and restores seed data.
func InitDB(cfg *config.Config) (*sql.DB, error) {
	db, err := sql.Open("sqlite", sqliteDataSource(cfg.DBPath))
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connect to sqlite database: %w", err)
	}

	if _, err := db.Exec(schemaSQL); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to execute schema: %w", err)
	}
	if err := migrateSchema(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate sqlite schema: %w", err)
	}

	restoreFromBackupIfEmpty(db, cfg.BackupPath, cfg.InitialBackupPath, cfg.AttachmentsDir)

	return db, nil
}

func sqliteDataSource(databasePath string) string {
	absolutePath, err := filepath.Abs(databasePath)
	if err == nil {
		databasePath = absolutePath
	}
	location := &url.URL{Scheme: "file", Path: filepath.ToSlash(databasePath)}
	query := url.Values{}
	query.Set("_foreign_keys", "on")
	query.Set("_busy_timeout", "5000")
	query.Set("_journal_mode", "WAL")
	query.Set("_synchronous", "NORMAL")
	location.RawQuery = query.Encode()
	return location.String()
}

type columnMigration struct {
	table     string
	column    string
	statement string
}

type schemaExecutor interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
}

const latestSchemaVersion = 6

func migrateSchema(db *sql.DB) error {
	var currentVersion int
	if err := db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&currentVersion); err != nil {
		return fmt.Errorf("read current schema version: %w", err)
	}
	if currentVersion > latestSchemaVersion {
		return fmt.Errorf("database schema version %d is newer than supported version %d", currentVersion, latestSchemaVersion)
	}
	for version := currentVersion + 1; version <= latestSchemaVersion; version++ {
		if err := applySchemaMigration(db, version); err != nil {
			return fmt.Errorf("apply schema migration %d: %w", version, err)
		}
	}
	return nil
}

func applySchemaMigration(db *sql.DB, version int) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := runSchemaMigration(tx, version); err != nil {
		return err
	}
	if _, err := tx.Exec("INSERT INTO schema_migrations (version) VALUES (?)", version); err != nil {
		return fmt.Errorf("record migration: %w", err)
	}
	return tx.Commit()
}

func runSchemaMigration(executor schemaExecutor, version int) error {
	switch version {
	case 1:
		return applyInitialSchemaMigration(executor)
	case 2:
		if _, err := executor.Exec(attachmentOwnershipTriggersSQL); err != nil {
			return fmt.Errorf("create attachment ownership triggers: %w", err)
		}
	case 3:
		if _, err := executor.Exec(meetingTypeConstraintsSQL); err != nil {
			return fmt.Errorf("constrain meeting types: %w", err)
		}
	case 4:
		return ensurePreparationColumns(executor)
	case 5:
		return ensureWorkArrangementColumns(executor)
	case 6:
		return ensureEmploymentTypeColumn(executor)
	default:
		return fmt.Errorf("unknown migration version %d", version)
	}
	return nil
}

func ensureEmploymentTypeColumn(executor schemaExecutor) error {
	exists, err := hasColumn(executor, "jobs", "employment_type")
	if err != nil {
		return fmt.Errorf("inspect jobs.employment_type: %w", err)
	}
	if !exists {
		if _, err := executor.Exec("ALTER TABLE jobs ADD COLUMN employment_type TEXT NOT NULL DEFAULT 'unknown'"); err != nil {
			return fmt.Errorf("add jobs.employment_type: %w", err)
		}
	}
	if _, err := executor.Exec(employmentTypeConstraintsSQL); err != nil {
		return fmt.Errorf("constrain employment types: %w", err)
	}
	return nil
}

const employmentTypeConstraintsSQL = `
UPDATE jobs SET employment_type = 'unknown'
WHERE employment_type IS NULL OR employment_type NOT IN ('unknown', 'permanent', 'b2b', 'permanent_b2b');
CREATE TRIGGER IF NOT EXISTS validate_employment_type_insert
BEFORE INSERT ON jobs
WHEN NEW.employment_type IS NULL OR NEW.employment_type NOT IN ('unknown', 'permanent', 'b2b', 'permanent_b2b')
BEGIN
    SELECT RAISE(ABORT, 'invalid job employment type');
END;
CREATE TRIGGER IF NOT EXISTS validate_employment_type_update
BEFORE UPDATE OF employment_type ON jobs
WHEN NEW.employment_type IS NULL OR NEW.employment_type NOT IN ('unknown', 'permanent', 'b2b', 'permanent_b2b')
BEGIN
    SELECT RAISE(ABORT, 'invalid job employment type');
END;
`

func applyInitialSchemaMigration(executor schemaExecutor) error {
	if err := ensureLegacyColumns(executor); err != nil {
		return err
	}
	if _, err := executor.Exec(schemaIndexesSQL); err != nil {
		return fmt.Errorf("create indexes: %w", err)
	}
	return nil
}

func ensureWorkArrangementColumns(executor schemaExecutor) error {
	exists, err := hasColumn(executor, "jobs", "work_arrangement")
	if err != nil {
		return fmt.Errorf("inspect jobs.work_arrangement: %w", err)
	}
	if !exists {
		if _, err := executor.Exec("ALTER TABLE jobs ADD COLUMN work_arrangement TEXT NOT NULL DEFAULT 'unknown'"); err != nil {
			return fmt.Errorf("add jobs.work_arrangement: %w", err)
		}
	}
	if _, err := executor.Exec(workArrangementConstraintsSQL); err != nil {
		return fmt.Errorf("constrain work arrangements: %w", err)
	}
	return nil
}

const workArrangementConstraintsSQL = `
UPDATE jobs SET work_arrangement = 'unknown'
WHERE work_arrangement IS NULL OR work_arrangement NOT IN ('unknown', 'remote', 'hybrid', 'on_site');
CREATE TRIGGER IF NOT EXISTS validate_work_arrangement_insert
BEFORE INSERT ON jobs
WHEN NEW.work_arrangement IS NULL OR NEW.work_arrangement NOT IN ('unknown', 'remote', 'hybrid', 'on_site')
BEGIN
    SELECT RAISE(ABORT, 'invalid job work arrangement');
END;
CREATE TRIGGER IF NOT EXISTS validate_work_arrangement_update
BEFORE UPDATE OF work_arrangement ON jobs
WHEN NEW.work_arrangement IS NULL OR NEW.work_arrangement NOT IN ('unknown', 'remote', 'hybrid', 'on_site')
BEGIN
    SELECT RAISE(ABORT, 'invalid job work arrangement');
END;
`

func ensurePreparationColumns(executor schemaExecutor) error {
	for _, migration := range []columnMigration{
		{"jobs", "experience_notes", "ALTER TABLE jobs ADD COLUMN experience_notes TEXT NOT NULL DEFAULT ''"},
		{"jobs", "expected_salary", "ALTER TABLE jobs ADD COLUMN expected_salary TEXT NOT NULL DEFAULT ''"},
	} {
		exists, err := hasColumn(executor, migration.table, migration.column)
		if err != nil {
			return fmt.Errorf("inspect %s.%s: %w", migration.table, migration.column, err)
		}
		if !exists {
			if _, err := executor.Exec(migration.statement); err != nil {
				return fmt.Errorf("add %s.%s: %w", migration.table, migration.column, err)
			}
		}
	}
	return nil
}

const attachmentOwnershipTriggersSQL = `
-- Preserve the job-level attachment while detaching any invalid legacy stage link.
UPDATE attachments
SET stage_id = NULL
WHERE stage_id IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM stages WHERE id = attachments.stage_id AND job_id = attachments.job_id);
CREATE TRIGGER IF NOT EXISTS validate_attachment_stage_owner_insert
BEFORE INSERT ON attachments
WHEN NEW.stage_id IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM stages WHERE id = NEW.stage_id AND job_id = NEW.job_id)
BEGIN
    SELECT RAISE(ABORT, 'attachment stage must belong to its job');
END;
CREATE TRIGGER IF NOT EXISTS validate_attachment_stage_owner_update
BEFORE UPDATE OF job_id, stage_id ON attachments
WHEN NEW.stage_id IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM stages WHERE id = NEW.stage_id AND job_id = NEW.job_id)
BEGIN
    SELECT RAISE(ABORT, 'attachment stage must belong to its job');
END;
`

const meetingTypeConstraintsSQL = `
UPDATE stages
SET meeting_type = 'video'
WHERE meeting_type IS NULL OR meeting_type NOT IN ('video', 'phone', 'onsite');
CREATE TRIGGER IF NOT EXISTS validate_stage_meeting_type_insert
BEFORE INSERT ON stages
WHEN NEW.meeting_type IS NULL OR NEW.meeting_type NOT IN ('video', 'phone', 'onsite')
BEGIN
    SELECT RAISE(ABORT, 'invalid stage meeting type');
END;
CREATE TRIGGER IF NOT EXISTS validate_stage_meeting_type_update
BEFORE UPDATE OF meeting_type ON stages
WHEN NEW.meeting_type IS NULL OR NEW.meeting_type NOT IN ('video', 'phone', 'onsite')
BEGIN
    SELECT RAISE(ABORT, 'invalid stage meeting type');
END;
`

var legacyColumns = []columnMigration{
	{"stages", "meeting_date", "ALTER TABLE stages ADD COLUMN meeting_date TEXT NULL"},
	{"stages", "meeting_time", "ALTER TABLE stages ADD COLUMN meeting_time TEXT NULL"},
	{"stages", "meeting_url", "ALTER TABLE stages ADD COLUMN meeting_url TEXT NULL"},
	{"stages", "meeting_type", "ALTER TABLE stages ADD COLUMN meeting_type TEXT NULL DEFAULT 'video'"},
	{"jobs", "order_index", "ALTER TABLE jobs ADD COLUMN order_index INTEGER NOT NULL DEFAULT 0"},
	{"jobs", "company_overview", "ALTER TABLE jobs ADD COLUMN company_overview TEXT NOT NULL DEFAULT ''"},
	{"jobs", "company_domain", "ALTER TABLE jobs ADD COLUMN company_domain TEXT NULL"},
	{"jobs", "interview_notes", "ALTER TABLE jobs ADD COLUMN interview_notes TEXT NOT NULL DEFAULT ''"},
	{"jobs", "reasons_to_change", "ALTER TABLE jobs ADD COLUMN reasons_to_change TEXT NOT NULL DEFAULT ''"},
	{"jobs", "experience_notes", "ALTER TABLE jobs ADD COLUMN experience_notes TEXT NOT NULL DEFAULT ''"},
	{"jobs", "expected_salary", "ALTER TABLE jobs ADD COLUMN expected_salary TEXT NOT NULL DEFAULT ''"},
	{"jobs", "work_arrangement", "ALTER TABLE jobs ADD COLUMN work_arrangement TEXT NOT NULL DEFAULT 'unknown'"},
	{"jobs", "is_referral", "ALTER TABLE jobs ADD COLUMN is_referral INTEGER NOT NULL DEFAULT 0"},
	{"stages", "recruiter_name", "ALTER TABLE stages ADD COLUMN recruiter_name TEXT NULL"},
	{"stages", "recruiter_type", "ALTER TABLE stages ADD COLUMN recruiter_type TEXT NOT NULL DEFAULT 'none'"},
	{"stages", "recruiter_agency", "ALTER TABLE stages ADD COLUMN recruiter_agency TEXT NULL"},
	{"stages", "recruiter_contact", "ALTER TABLE stages ADD COLUMN recruiter_contact TEXT NULL"},
	{"stages", "interviewers_json", "ALTER TABLE stages ADD COLUMN interviewers_json TEXT NOT NULL DEFAULT '[]'"},
}

func ensureLegacyColumns(db schemaExecutor) error {
	for _, migration := range legacyColumns {
		exists, err := hasColumn(db, migration.table, migration.column)
		if err != nil {
			return fmt.Errorf("inspect %s.%s: %w", migration.table, migration.column, err)
		}
		if exists {
			continue
		}
		if _, err := db.Exec(migration.statement); err != nil {
			return fmt.Errorf("add %s.%s: %w", migration.table, migration.column, err)
		}
	}
	return nil
}

func hasColumn(db schemaExecutor, table, column string) (bool, error) {
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return false, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var position, notNull, primaryKey int
		var name, dataType string
		var defaultValue sql.NullString
		if err := rows.Scan(&position, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

func restoreFromBackupIfEmpty(db *sql.DB, backupPath, initialBackupPath, attachmentsDir string) {
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM jobs").Scan(&count); err != nil || count > 0 {
		return
	}

	backupSource := findBackupSource(backupPath, initialBackupPath)
	if backupSource == "" {
		return
	}

	jobs, err := loadBackup(backupSource)
	if err != nil {
		log.Printf("Warning: Failed to load backup.json: %v", err)
		return
	}
	if len(jobs) == 0 {
		return
	}
	if err := restoreJobs(db, jobs, attachmentsDir); err != nil {
		log.Printf("Warning: Failed to restore backup.json atomically: %v", err)
		return
	}
	log.Printf("Restored %d jobs from backup.json into empty database.", len(jobs))
}

const maxBackupBytes = 64 << 20

func loadBackup(backupPath string) ([]models.Job, error) {
	// backupPath comes from application configuration, not request input.
	file, err := os.Open(backupPath) // #nosec G304
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, maxBackupBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxBackupBytes {
		return nil, fmt.Errorf("backup exceeds %d-byte limit", maxBackupBytes)
	}
	var jobs []models.Job
	if err := json.Unmarshal(data, &jobs); err != nil {
		return nil, fmt.Errorf("parse backup: %w", err)
	}
	return jobs, nil
}

func restoreJobs(db *sql.DB, jobs []models.Job, attachmentsDir string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	statements, err := prepareRestoreStatements(tx)
	if err != nil {
		return err
	}
	defer statements.close()
	for _, job := range jobs {
		if err := restoreJob(statements, job, attachmentsDir); err != nil {
			return err
		}
	}
	return tx.Commit()
}

type restoreStatements struct {
	job        *sql.Stmt
	stage      *sql.Stmt
	question   *sql.Stmt
	attachment *sql.Stmt
}

func prepareRestoreStatements(tx *sql.Tx) (*restoreStatements, error) {
	job, err := tx.Prepare(`
		INSERT INTO jobs (
			id, company_name, position_title, status, salary_type, salary_min, salary_max,
			salary_currency, recruiter_type, recruiter_name, recruiter_agency, recruiter_contact,
			interviewers_json, job_post_url, avatar_seed, keyword_note, description, company_overview,
			company_domain, interview_notes, reasons_to_change, experience_notes, expected_salary,
			is_referral, order_index, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return nil, err
	}

	stageStmt, err := tx.Prepare(`
		INSERT INTO stages (
			id, job_id, order_index, stage_type, custom_title, description, status,
			scheduled_at, meeting_date, meeting_time, meeting_url, meeting_type, notes,
			recruiter_name, recruiter_type, recruiter_agency, recruiter_contact, interviewers_json, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		_ = job.Close()
		return nil, err
	}

	qStmt, err := tx.Prepare(`
		INSERT INTO stage_questions (
			id, stage_id, order_index, question, answer_notes, is_asked, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		_ = job.Close()
		_ = stageStmt.Close()
		return nil, err
	}
	attachmentStmt, err := tx.Prepare(`
		INSERT INTO attachments (
			id, job_id, stage_id, original_name, stored_filename, absolute_path,
			file_size, mime_type, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		_ = job.Close()
		_ = stageStmt.Close()
		_ = qStmt.Close()
		return nil, err
	}
	return &restoreStatements{job: job, stage: stageStmt, question: qStmt, attachment: attachmentStmt}, nil
}

func (statements *restoreStatements) close() {
	_ = statements.job.Close()
	_ = statements.stage.Close()
	_ = statements.question.Close()
	_ = statements.attachment.Close()
}

func restoreJob(statements *restoreStatements, job models.Job, attachmentsDir string) error {
	if err := restoreJobRow(statements.job, job); err != nil {
		return fmt.Errorf("restore job %q: %w", job.ID, err)
	}
	for _, stage := range job.Stages {
		if err := restoreStage(statements, job.ID, stage); err != nil {
			return fmt.Errorf("restore stage %q: %w", stage.ID, err)
		}
	}
	for _, attachment := range job.Attachments {
		if err := restoreAttachment(statements.attachment, job, attachment, attachmentsDir); err != nil {
			return fmt.Errorf("restore attachment %q: %w", attachment.ID, err)
		}
	}
	return nil
}

func restoreAttachment(statement *sql.Stmt, job models.Job, attachment models.Attachment, directory string) error {
	filename, err := safeStoredFilename(attachment.StoredFilename)
	if err != nil {
		return err
	}
	if err := validateAttachmentOwner(job, attachment); err != nil {
		return err
	}
	fileInfo, err := os.Lstat(filepath.Join(directory, filename))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !fileInfo.Mode().IsRegular() || attachment.FileSize < 0 {
		return fmt.Errorf("attachment %q is not a regular file or has invalid size", attachment.ID)
	}
	return insertRestoredAttachment(statement, job.ID, attachment, filename)
}

func safeStoredFilename(storedFilename string) (string, error) {
	filename := filepath.Base(storedFilename)
	if filename != storedFilename || filename == "." || filename == string(filepath.Separator) {
		return "", fmt.Errorf("unsafe stored filename %q", storedFilename)
	}
	return filename, nil
}

func validateAttachmentOwner(job models.Job, attachment models.Attachment) error {
	if attachment.JobID != "" && attachment.JobID != job.ID {
		return fmt.Errorf("attachment belongs to job %q instead of %q", attachment.JobID, job.ID)
	}
	if attachment.StageID != nil && !jobContainsStage(job.Stages, *attachment.StageID) {
		return fmt.Errorf("attachment references unknown stage %q", *attachment.StageID)
	}
	return nil
}

func insertRestoredAttachment(statement *sql.Stmt, jobID string, attachment models.Attachment, filename string) error {
	mimeType := defaultString(attachment.MimeType, "application/octet-stream")
	_, err := statement.Exec(attachment.ID, jobID, attachment.StageID, attachment.OriginalName,
		filename, filename, attachment.FileSize, mimeType, attachment.CreatedAt)
	return err
}

func jobContainsStage(stages []models.Stage, stageID string) bool {
	for _, stage := range stages {
		if stage.ID == stageID {
			return true
		}
	}
	return false
}

func restoreJobRow(statement *sql.Stmt, job models.Job) error {
	recruiterType := defaultString(string(job.RecruiterType), string(models.RecruiterNone))
	salaryType := defaultString(string(job.SalaryType), string(models.SalaryUnknown))
	currency := defaultString(job.SalaryCurrency, "EUR")
	interviewers, err := json.Marshal(job.Interviewers)
	if err != nil {
		return err
	}
	if len(interviewers) == 0 {
		interviewers = []byte("[]")
	}
	_, err = statement.Exec(job.ID, job.CompanyName, job.PositionTitle, job.Status, salaryType,
		job.SalaryMin, job.SalaryMax, currency, recruiterType, job.RecruiterName,
		job.RecruiterAgency, job.RecruiterContact, string(interviewers), job.JobPostURL,
		job.AvatarSeed, job.KeywordNote, job.Description, job.CompanyOverview, job.CompanyDomain,
		job.InterviewNotes, job.ReasonsToChange, job.ExperienceNotes, job.ExpectedSalary,
		job.IsReferral, job.OrderIndex, job.CreatedAt, job.UpdatedAt)
	return err
}

func restoreStage(statements *restoreStatements, jobID string, stage models.Stage) error {
	if err := restoreStageRow(statements.stage, jobID, stage); err != nil {
		return err
	}
	for _, question := range stage.Questions {
		if _, err := statements.question.Exec(question.ID, stage.ID, question.OrderIndex,
			question.Question, question.AnswerNotes, question.IsAsked, question.CreatedAt); err != nil {
			return fmt.Errorf("restore question %q: %w", question.ID, err)
		}
	}
	return nil
}

func restoreStageRow(statement *sql.Stmt, jobID string, stage models.Stage) error {
	recruiterType := defaultString(string(stage.RecruiterType), "none")
	meetingType := defaultString(stage.MeetingType, "video")
	interviewers, err := json.Marshal(stage.Interviewers)
	if err != nil {
		return err
	}
	if len(interviewers) == 0 {
		interviewers = []byte("[]")
	}
	_, err = statement.Exec(stage.ID, jobID, stage.OrderIndex, stage.StageType, stage.CustomTitle,
		stage.Description, stage.Status, stage.ScheduledAt, stage.MeetingDate, stage.MeetingTime,
		stage.MeetingURL, meetingType, stage.Notes, stage.RecruiterName, recruiterType,
		stage.RecruiterAgency, stage.RecruiterContact, string(interviewers), stage.CreatedAt)
	return err
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// findBackupSource prefers the mutable data backup, then falls back to an immutable seed.
func findBackupSource(backupPath, initialBackupPath string) string {
	if _, err := os.Stat(backupPath); err == nil {
		return backupPath
	} else if !os.IsNotExist(err) {
		return ""
	}
	if initialBackupPath == "" {
		return ""
	}
	if _, err := os.Stat(initialBackupPath); err != nil {
		return ""
	}
	return initialBackupPath
}
