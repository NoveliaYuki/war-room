package backend_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"war-room/backend/pkg/models"
)

// TestStoreAttachmentRejectsStageFromAnotherJob protects attachment ownership integrity.
func TestStoreAttachmentRejectsStageFromAnotherJob(t *testing.T) {
	_, jobs, _, cfg, db := newCoverageDB(t)
	firstJob, err := jobs.CreateJob(models.CreateJobInput{
		CompanyName: "First Company", PositionTitle: "Engineer", CreateDefaultStages: ptr(false),
	})
	if err != nil {
		t.Fatal(err)
	}
	secondJob, err := jobs.CreateJob(models.CreateJobInput{
		CompanyName: "Second Company", PositionTitle: "Designer", CreateDefaultStages: ptr(false),
	})
	if err != nil {
		t.Fatal(err)
	}
	stage, err := jobs.CreateStage(models.CreateStageInput{JobID: secondJob.ID, StageType: models.StageHR})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := jobs.StoreAttachment(firstJob.ID, &stage.ID, "resume.txt", strings.NewReader("resume"), 6, "text/plain"); err == nil {
		t.Fatal("expected attachment stage ownership error")
	}

	files, err := os.ReadDir(cfg.AttachmentsDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("cross-job attachment wrote %d files before validation", len(files))
	}
	if _, err := db.Exec(`
		INSERT INTO attachments (id, job_id, stage_id, original_name, stored_filename, absolute_path, file_size, mime_type)
		VALUES ('invalid-owner', ?, ?, 'resume.txt', 'invalid-owner.txt', 'invalid-owner.txt', 6, 'text/plain')
	`, firstJob.ID, stage.ID); err == nil {
		t.Fatal("database accepted an attachment stage owned by another job")
	}
	attachments, err := jobs.GetFullJobDetails(firstJob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(attachments.Attachments) != 0 {
		t.Fatalf("cross-job attachment persisted: %+v", attachments.Attachments)
	}
}

// TestDeletingStageDetachesButPreservesItsAttachment covers the stage foreign-key action.
func TestDeletingStageDetachesButPreservesItsAttachment(t *testing.T) {
	_, jobs, _, cfg := newCoverageApp(t)
	job, err := jobs.CreateJob(models.CreateJobInput{
		CompanyName: "Acme", PositionTitle: "Engineer", CreateDefaultStages: ptr(false),
	})
	if err != nil {
		t.Fatal(err)
	}
	stage, err := jobs.CreateStage(models.CreateStageInput{JobID: job.ID, StageType: models.StageHR})
	if err != nil {
		t.Fatal(err)
	}
	attachment, err := jobs.StoreAttachment(job.ID, &stage.ID, "resume.txt", strings.NewReader("resume"), 6, "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	if err := jobs.DeleteStage(stage.ID); err != nil {
		t.Fatal(err)
	}

	updated, err := jobs.GetAttachmentByID(attachment.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated == nil || updated.StageID != nil {
		t.Fatalf("attachment after stage deletion = %+v, want attachment retained with no stage", updated)
	}
	if _, err := os.Stat(filepath.Join(cfg.AttachmentsDir, attachment.StoredFilename)); err != nil {
		t.Fatalf("attachment file after stage deletion: %v", err)
	}
}
