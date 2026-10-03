package database

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"war-room/backend/pkg/models"
)

func TestRecoverySnapshotHandlesLegacyArrayDecodeFailure(t *testing.T) {
	backup := filepath.Join(t.TempDir(), "legacy.json")
	if err := os.WriteFile(backup, []byte("[}"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadRecoverySnapshot(backup); err == nil {
		t.Fatal("invalid legacy JSON array was accepted")
	}
}

func TestRestoreJobsWithCVConvertsVersionsAndSequence(t *testing.T) {
	db, cfg := recoveryCoverageDB(t)
	if err := os.MkdirAll(cfg.CVDir, 0700); err != nil {
		t.Fatal(err)
	}
	content := []byte("versioned CV")
	digest := sha256.Sum256(content)
	filename := "restored.pdf"
	if err := os.WriteFile(filepath.Join(cfg.CVDir, filename), content, 0600); err != nil {
		t.Fatal(err)
	}
	version := models.CVVersion{ID: "cv-restored", Version: 3, OriginalName: "resume.pdf", StoredFilename: filename, FileSize: int64(len(content)),
		MimeType: "application/pdf", SHA256: hex.EncodeToString(digest[:]), UploadedAt: 10}
	if err := restoreJobsWithCV(db, nil, cfg.AttachmentsDir, []models.CVVersion{version}, cfg.CVDir); err != nil {
		t.Fatal(err)
	}
	var next int
	if err := db.QueryRow(`SELECT next_version FROM cv_version_sequence WHERE id = 1`).Scan(&next); err != nil || next != 4 {
		t.Fatalf("restored next version=%d err=%v", next, err)
	}
}

func TestRecoveryPrepareStatementsReportsMissingCVColumn(t *testing.T) {
	db, cfg := recoveryCoverageDB(t)
	if _, err := db.Exec(`ALTER TABLE cv_versions RENAME COLUMN original_name TO saved_name`); err != nil {
		t.Fatal(err)
	}
	if err := restoreRecoverySnapshot(db, models.RecoverySnapshot{Jobs: []models.Job{}}, cfg.AttachmentsDir, cfg.CVDir); err == nil {
		t.Fatal("restore should fail when CV metadata schema is incomplete")
	}
}

func TestRecoveryAttachmentInspectionErrorsRollBackJob(t *testing.T) {
	db, _ := recoveryCoverageDB(t)
	attachmentsPath := filepath.Join(t.TempDir(), "attachments")
	if err := os.WriteFile(attachmentsPath, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	job := models.Job{ID: "restore-job", CompanyName: "Example", PositionTitle: "Engineer", Status: models.StatusOngoing,
		SalaryType: models.SalaryUnknown, SalaryCurrency: "EUR", RecruiterType: models.RecruiterNone,
		Attachments: []models.Attachment{{ID: "attachment", JobID: "restore-job", OriginalName: "resume.pdf", StoredFilename: "resume.pdf", FileSize: 4}}}
	if err := restoreJobs(db, []models.Job{job}, attachmentsPath); err == nil {
		t.Fatal("invalid attachment directory should fail restore")
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM jobs`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed recovery left %d job rows, err=%v", count, err)
	}
}
