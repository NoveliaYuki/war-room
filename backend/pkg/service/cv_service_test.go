package service

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"war-room/backend/pkg/config"
	"war-room/backend/pkg/database"
	"war-room/backend/pkg/models"
	"war-room/backend/pkg/repository"
)

func TestStoreCVVersionsDeduplicatesBytesAndAssignsExactVersion(t *testing.T) {
	fixture := newAttachmentOwnerFixture(t)
	first := storeCVTestVersion(t, fixture.service, "resume.pdf", "same PDF bytes")
	second := storeCVTestVersion(t, fixture.service, "resume-final.pdf", "same PDF bytes")
	if first.Version != 1 || second.Version != 2 || first.ID == second.ID {
		t.Fatalf("versions=%+v %+v", first, second)
	}
	if first.SHA256 != second.SHA256 || first.StoredFilename != second.StoredFilename {
		t.Fatalf("duplicate content was not reused: %+v %+v", first, second)
	}
	if first.UploadedAt <= 0 || second.UploadedAt <= 0 {
		t.Fatalf("missing upload timestamps: %+v %+v", first, second)
	}
	assertCVAssignmentAndMonotonicDelete(t, fixture.service, second.ID)
}

func storeCVTestVersion(t *testing.T, service *JobService, filename, body string) *models.CVVersion {
	t.Helper()
	version, err := service.StoreCVVersion(filename, strings.NewReader(body), int64(len(body)), "application/pdf")
	if err != nil {
		t.Fatal(err)
	}
	return version
}

func assertCVAssignmentAndMonotonicDelete(t *testing.T, service *JobService, versionID string) {
	t.Helper()
	if err := service.AssignCVVersion("owner-a", &versionID); err != nil {
		t.Fatal(err)
	}
	job, err := service.GetFullJobDetails("owner-a")
	if err != nil || job.SelectedCVVersion == nil || job.SelectedCVVersion.ID != versionID {
		t.Fatalf("selected version=%+v err=%v", job.SelectedCVVersion, err)
	}
	if err := service.DeleteCVVersion(versionID); !errors.Is(err, ErrCVVersionInUse) {
		t.Fatalf("delete assigned CV error=%v", err)
	}
	if err := service.AssignCVVersion("owner-a", nil); err != nil {
		t.Fatal(err)
	}
	if err := service.DeleteCVVersion(versionID); err != nil {
		t.Fatalf("delete unassigned version: %v", err)
	}
	third := storeCVTestVersion(t, service, "resume.pdf", "new CV bytes")
	if third.Version != 3 {
		t.Fatalf("version after deleting v2=%+v", third)
	}
}

func TestMigrateLegacyIdenticalCopiesPreservesStageAttachment(t *testing.T) {
	fixture := newAttachmentOwnerFixture(t)
	content := []byte("identical legacy CV")
	legacy := []models.Attachment{
		{ID: "legacy-a", JobID: "owner-a", OriginalName: "resume.pdf", StoredFilename: "legacy-a.pdf", FileSize: int64(len(content)), MimeType: "application/pdf"},
		{ID: "legacy-b", JobID: "owner-b", OriginalName: "resume (copy).pdf", StoredFilename: "legacy-b.pdf", FileSize: int64(len(content)), MimeType: "application/pdf"},
		{ID: "stage-pdf", JobID: "owner-a", StageID: &fixture.ownedStage.ID, OriginalName: "interview.pdf", StoredFilename: "stage.pdf", FileSize: 5, MimeType: "application/pdf"},
	}
	insertLegacyMigrationFixtures(t, fixture, legacy, [][]byte{content, content, []byte("other")}, []int64{100, 200, 300})
	if err := fixture.service.MigrateLegacyCVAttachments(); err != nil {
		t.Fatal(err)
	}
	assertMigratedLegacyCV(t, fixture, content)
	if _, err := os.Stat(filepath.Join(fixture.cfg.AttachmentsDir, "stage.pdf")); err != nil {
		t.Fatalf("stage attachment removed: %v", err)
	}
}

func TestHasCVFilenameMarker(t *testing.T) {
	cases := map[string]bool{
		"CV.pdf":                 true,
		"resume-final.pdf":       true,
		"curriculum-vitae.pdf":   true,
		"job-description-cv.pdf": false,
		"job-specification.pdf":  false,
		"service-overview.pdf":   false,
	}
	for filename, expected := range cases {
		if actual := hasCVFilenameMarker(filename); actual != expected {
			t.Errorf("hasCVFilenameMarker(%q)=%t; want %t", filename, actual, expected)
		}
	}
}

func TestMigrateLegacyCVMarkersAllowMetadataHashDifferencesAndPreserveJobSpec(t *testing.T) {
	fixture := newAttachmentOwnerFixture(t)
	attachments := []models.Attachment{
		{ID: "resume", JobID: "owner-a", OriginalName: "resume.pdf", StoredFilename: "resume.pdf", FileSize: 6, MimeType: "application/pdf"},
		{ID: "resume-copy", JobID: "owner-b", OriginalName: "CV version 2.pdf", StoredFilename: "resume-copy.pdf", FileSize: 6, MimeType: "application/pdf"},
		{ID: "job-spec", JobID: "owner-a", OriginalName: "spec.pdf", StoredFilename: "spec.pdf", FileSize: 5, MimeType: "application/pdf"},
	}
	insertLegacyMigrationFixtures(t, fixture, attachments, [][]byte{[]byte("resume"), []byte("resumE"), []byte("spec!")}, []int64{100, 150, 200})
	if err := fixture.service.MigrateLegacyCVAttachments(); err != nil {
		t.Fatal(err)
	}
	versions, err := fixture.service.ListCVVersions()
	remaining, attachmentErr := fixture.repo.GetAllAttachments()
	if err != nil || attachmentErr != nil || len(versions) != 1 || len(remaining) != 1 || remaining[0].ID != "job-spec" {
		t.Fatalf("versions=%+v remaining=%+v errors=%v/%v", versions, remaining, err, attachmentErr)
	}
	assertMigratedVersion(t, fixture, versions[0], []byte("resume"))
	if versions[0].UploadedAt != 100 {
		t.Fatalf("version timestamp should come from earliest marked CV copy: %+v", versions[0])
	}
	assertLegacyCVFilesCleaned(t, fixture)
}

func assertLegacyCVFilesCleaned(t *testing.T, fixture attachmentOwnerFixture) {
	t.Helper()
	for _, filename := range []string{"resume.pdf", "resume-copy.pdf"} {
		if _, err := os.Stat(filepath.Join(fixture.cfg.AttachmentsDir, filename)); !os.IsNotExist(err) {
			t.Fatalf("migrated CV copy %q remains: %v", filename, err)
		}
	}
	if _, err := os.Stat(filepath.Join(fixture.cfg.AttachmentsDir, "spec.pdf")); err != nil {
		t.Fatalf("unmarked job-spec PDF was removed: %v", err)
	}
}

func insertLegacyMigrationFixtures(t *testing.T, fixture attachmentOwnerFixture, attachments []models.Attachment, contents [][]byte, createdAt []int64) {
	t.Helper()
	for index, attachment := range attachments {
		attachment.FileSize = int64(len(contents[index]))
		if err := os.WriteFile(filepath.Join(fixture.cfg.AttachmentsDir, attachment.StoredFilename), contents[index], 0600); err != nil {
			t.Fatal(err)
		}
		if err := fixture.repo.InsertAttachment(&attachment); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.db.Exec(`UPDATE attachments SET created_at = ? WHERE id = ?`, createdAt[index], attachment.ID); err != nil {
			t.Fatal(err)
		}
	}
}

func assertMigratedLegacyCV(t *testing.T, fixture attachmentOwnerFixture, expectedBytes []byte) {
	t.Helper()
	versions, err := fixture.service.ListCVVersions()
	if err != nil || len(versions) != 1 {
		t.Fatalf("versions=%+v err=%v", versions, err)
	}
	assertMigratedVersion(t, fixture, versions[0], expectedBytes)
	assertJobsUseMigratedVersion(t, fixture, versions[0].ID)
	assertMigratedAttachments(t, fixture, versions[0].StoredFilename)
}

func assertMigratedVersion(t *testing.T, fixture attachmentOwnerFixture, version models.CVVersion, expectedBytes []byte) {
	t.Helper()
	if version.Version != 1 || version.UploadedAt != 100 || version.OriginalName != "resume.pdf" || version.FileSize != int64(len(expectedBytes)) {
		t.Fatalf("migrated version=%+v", version)
	}
	migratedBytes, err := os.ReadFile(filepath.Join(fixture.cfg.CVDir, version.StoredFilename))
	if err != nil || string(migratedBytes) != string(expectedBytes) {
		t.Fatalf("migrated bytes=%q err=%v", migratedBytes, err)
	}
}

func assertJobsUseMigratedVersion(t *testing.T, fixture attachmentOwnerFixture, versionID string) {
	t.Helper()
	for _, jobID := range []string{"owner-a", "owner-b"} {
		job, err := fixture.service.GetFullJobDetails(jobID)
		if err != nil || job.SelectedCVVersion == nil || job.SelectedCVVersion.ID != versionID {
			t.Fatalf("job %q selection=%+v err=%v", jobID, job, err)
		}
	}
}

func assertMigratedAttachments(t *testing.T, fixture attachmentOwnerFixture, cvFilename string) {
	t.Helper()
	attachments, err := fixture.repo.GetAllAttachments()
	if err != nil || len(attachments) != 1 || attachments[0].ID != "stage-pdf" {
		t.Fatalf("remaining attachments=%+v err=%v", attachments, err)
	}
	if _, err := os.Stat(filepath.Join(fixture.cfg.CVDir, cvFilename)); err != nil {
		t.Fatalf("migrated blob missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(fixture.cfg.AttachmentsDir, "legacy-a.pdf")); !os.IsNotExist(err) {
		t.Fatalf("old CV copy remains: %v", err)
	}
	if _, err := os.Stat(filepath.Join(fixture.cfg.AttachmentsDir, "legacy-b.pdf")); !os.IsNotExist(err) {
		t.Fatalf("old CV copy remains: %v", err)
	}
	if _, err := os.Stat(filepath.Join(fixture.cfg.AttachmentsDir, "stage.pdf")); err != nil {
		t.Fatalf("stage attachment removed: %v", err)
	}
}

func TestMigrateLegacyLeavesNonPDFAttachmentsUntouched(t *testing.T) {
	fixture := newAttachmentOwnerFixture(t)
	attachment := models.Attachment{ID: "notes", JobID: "owner-a", OriginalName: "notes.txt", StoredFilename: "notes.txt", FileSize: 5, MimeType: "text/plain"}
	singlePDF := models.Attachment{ID: "single-pdf", JobID: "owner-a", OriginalName: "resume.pdf", StoredFilename: "resume.pdf", FileSize: 3, MimeType: "application/pdf"}
	jobSpec := models.Attachment{ID: "job-spec", JobID: "owner-a", OriginalName: "job-description.pdf", StoredFilename: "job-description.pdf", FileSize: 3, MimeType: "application/pdf"}
	insertLegacyMigrationFixtures(t, fixture, []models.Attachment{attachment, singlePDF, jobSpec}, [][]byte{[]byte("notes"), []byte("pdf"), []byte("spec")}, []int64{100, 200, 300})
	if err := fixture.service.MigrateLegacyCVAttachments(); err != nil {
		t.Fatal(err)
	}
	versions, err := fixture.service.ListCVVersions()
	attachments, attachmentsErr := fixture.repo.GetAllAttachments()
	if err != nil || attachmentsErr != nil || len(versions) != 0 || len(attachments) != 3 {
		t.Fatalf("versions=%+v attachments=%+v errors=%v/%v", versions, attachments, err, attachmentsErr)
	}
	for _, filename := range []string{singlePDF.StoredFilename, jobSpec.StoredFilename} {
		if _, err := os.Stat(filepath.Join(fixture.cfg.AttachmentsDir, filename)); err != nil {
			t.Fatalf("unpaired PDF %q was removed: %v", filename, err)
		}
	}
}

func TestStoreCVVersionRejectsInvalidUploadAndCorruptExistingBlob(t *testing.T) {
	fixture := newAttachmentOwnerFixture(t)
	invalidUploads := []struct {
		name, filename, body string
		size                 int64
	}{
		{name: "invalid name", filename: "resume\n.pdf", body: "cv", size: 2},
		{name: "empty file", filename: "resume.pdf", body: "", size: 0},
		{name: "size mismatch", filename: "resume.pdf", body: "cv", size: 3},
		{name: "too large", filename: "resume.pdf", body: "", size: 50<<20 + 1},
	}
	for _, test := range invalidUploads {
		t.Run(test.name, func(t *testing.T) {
			if _, err := fixture.service.StoreCVVersion(test.filename, strings.NewReader(test.body), test.size, "application/pdf"); err == nil {
				t.Fatal("invalid upload should fail")
			}
		})
	}
	content := "known CV"
	digest := sha256.Sum256([]byte(content))
	filename := hex.EncodeToString(digest[:])
	if err := os.WriteFile(filepath.Join(fixture.cfg.CVDir, filename), []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.StoreCVVersion("resume.pdf", strings.NewReader(content), int64(len(content)), "application/pdf"); err == nil {
		t.Fatal("corrupt content-addressed file must not be reused")
	}
}

func TestCVServiceLookupAndBlobSafetyHelpers(t *testing.T) {
	fixture := newAttachmentOwnerFixture(t)
	version := storeCVTestVersion(t, fixture.service, "resume.pdf", "safe CV")
	assertCVLookup(t, fixture.service, version.ID)
	assertCVBlobSafety(t, fixture, version)
}

func assertCVLookup(t *testing.T, service *JobService, versionID string) {
	t.Helper()
	loaded, err := service.GetCVVersion(versionID)
	if err != nil || loaded == nil || loaded.ID != versionID {
		t.Fatalf("loaded CV=%+v err=%v", loaded, err)
	}
	if missing, err := service.GetCVVersion("missing"); err != nil || missing != nil {
		t.Fatalf("missing CV=%+v err=%v", missing, err)
	}
}

func assertCVBlobSafety(t *testing.T, fixture attachmentOwnerFixture, version *models.CVVersion) {
	t.Helper()
	blob := filepath.Join(fixture.cfg.CVDir, version.StoredFilename)
	if err := verifyCVFile(blob, version.SHA256, version.FileSize); err != nil {
		t.Fatalf("verify valid blob: %v", err)
	}
	if err := verifyCVFile(blob, "wrong", version.FileSize); err == nil {
		t.Fatal("wrong digest should fail")
	}
	if err := verifyCVFile(filepath.Join(fixture.cfg.CVDir, "missing"), version.SHA256, version.FileSize); err == nil {
		t.Fatal("missing blob should fail")
	}
	fixture.service.removeUnreferencedCVBlob(version.StoredFilename)
	if _, err := os.Stat(blob); err != nil {
		t.Fatalf("referenced blob removed: %v", err)
	}
	if err := fixture.repo.DeleteCVVersion(version.ID); err != nil {
		t.Fatal(err)
	}
	fixture.service.removeUnreferencedCVBlob(version.StoredFilename)
	if _, err := os.Stat(blob); !os.IsNotExist(err) {
		t.Fatalf("unreferenced blob remains: %v", err)
	}
}

func TestStoreCVVersionCleansBlobAfterDatabaseInsertFailure(t *testing.T) {
	fixture := newAttachmentOwnerFixture(t)
	if _, err := fixture.db.Exec(`DROP TABLE cv_version_sequence`); err != nil {
		t.Fatal(err)
	}
	content := "metadata insertion failure"
	digest := sha256.Sum256([]byte(content))
	filename := hex.EncodeToString(digest[:])
	if _, err := fixture.service.StoreCVVersion("resume.pdf", strings.NewReader(content), int64(len(content)), "application/pdf"); err == nil {
		t.Fatal("missing sequence table should fail metadata insertion")
	}
	if _, err := os.Stat(filepath.Join(fixture.cfg.CVDir, filename)); !os.IsNotExist(err) {
		t.Fatalf("unreferenced blob was not cleaned: %v", err)
	}
}

func TestMigrationRetainsLegacyFilesWhenRecoverySnapshotCannotBeWritten(t *testing.T) {
	fixture := newAttachmentOwnerFixture(t)
	body := []byte("legacy CV")
	for index, jobID := range []string{"owner-a", "owner-b"} {
		name := []string{"resume-a.pdf", "resume-b.pdf"}[index]
		if err := os.WriteFile(filepath.Join(fixture.cfg.AttachmentsDir, name), body, 0600); err != nil {
			t.Fatal(err)
		}
		attachment := models.Attachment{ID: "migration-" + jobID, JobID: jobID, OriginalName: name, StoredFilename: name, FileSize: int64(len(body)), MimeType: "application/pdf"}
		if err := fixture.repo.InsertAttachment(&attachment); err != nil {
			t.Fatal(err)
		}
	}
	fixture.cfg.BackupPath = filepath.Join(fixture.cfg.DataDir, "missing", "backup.json")
	if err := fixture.service.MigrateLegacyCVAttachments(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"resume-a.pdf", "resume-b.pdf"} {
		if _, err := os.Stat(filepath.Join(fixture.cfg.AttachmentsDir, name)); err != nil {
			t.Fatalf("legacy recovery source %q was removed: %v", name, err)
		}
	}
}

func TestLegacyCVMigrationRejectsUnsafeMissingAndCorruptFiles(t *testing.T) {
	tests := []struct {
		name, stored, content string
		writeFile             bool
		declaredSize          int64
	}{
		{name: "unsafe path", stored: "../resume.pdf", content: "cv", writeFile: true, declaredSize: 2},
		{name: "missing file", stored: "missing.pdf", content: "cv", declaredSize: 2},
		{name: "size mismatch", stored: "wrong-size.pdf", content: "cv", writeFile: true, declaredSize: 10},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newAttachmentOwnerFixture(t)
			insertLegacyAttachmentForError(t, fixture, test.stored, test.content, test.writeFile, test.declaredSize)
			if err := fixture.service.MigrateLegacyCVAttachments(); err == nil {
				t.Fatal("invalid legacy attachment should fail migration")
			}
		})
	}
	fixture := newAttachmentOwnerFixture(t)
	content := "legacy cv"
	insertLegacyAttachmentForError(t, fixture, "legacy.pdf", content, true, int64(len(content)))
	duplicate := models.Attachment{ID: "legacy-error-copy", JobID: "owner-b", OriginalName: "resume-copy.pdf", StoredFilename: "legacy-copy.pdf", FileSize: int64(len(content)), MimeType: "application/pdf"}
	if err := os.WriteFile(filepath.Join(fixture.cfg.AttachmentsDir, duplicate.StoredFilename), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	if err := fixture.repo.InsertAttachment(&duplicate); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(content))
	if err := os.WriteFile(filepath.Join(fixture.cfg.CVDir, hex.EncodeToString(digest[:])), []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.MigrateLegacyCVAttachments(); err == nil {
		t.Fatal("corrupt existing CV blob should fail migration")
	}
}

func insertLegacyAttachmentForError(t *testing.T, fixture attachmentOwnerFixture, filename, content string, writeFile bool, declaredSize int64) {
	t.Helper()
	attachment := models.Attachment{ID: "legacy-error", JobID: "owner-a", OriginalName: "resume.pdf", StoredFilename: filename, FileSize: declaredSize, MimeType: "application/pdf"}
	if writeFile {
		path := filepath.Join(fixture.cfg.AttachmentsDir, filepath.Base(filename))
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := fixture.repo.InsertAttachment(&attachment); err != nil {
		t.Fatal(err)
	}
}

func TestCVBackupRoundTripPreservesVersionsAndSelection(t *testing.T) {
	source := newAttachmentOwnerFixture(t)
	version := seedBackupCVVersions(t, source)
	archive := exportCVBackup(t, source.service)
	target := newAttachmentOwnerFixture(t)
	importCVBackup(t, target.service, archive)
	assertCVBackupRestored(t, target, version)
}

func seedBackupCVVersions(t *testing.T, fixture attachmentOwnerFixture) *models.CVVersion {
	t.Helper()
	version := storeCVTestVersion(t, fixture.service, "resume.pdf", "CV backup bytes")
	if err := fixture.service.AssignCVVersion("owner-a", &version.ID); err != nil {
		t.Fatal(err)
	}
	storeCVTestVersion(t, fixture.service, "resume.pdf", "version two")
	deleted := storeCVTestVersion(t, fixture.service, "resume.pdf", "deleted version three")
	if err := fixture.service.DeleteCVVersion(deleted.ID); err != nil {
		t.Fatal(err)
	}
	return version
}

func exportCVBackup(t *testing.T, service *JobService) []byte {
	t.Helper()
	var archive bytes.Buffer
	if err := service.ExportArchive(&archive); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes()
}

func importCVBackup(t *testing.T, service *JobService, archive []byte) {
	t.Helper()
	if err := service.ImportArchive(bytes.NewReader(archive), int64(len(archive)), false); err != nil {
		t.Fatal(err)
	}
}

func assertCVBackupRestored(t *testing.T, target attachmentOwnerFixture, version *models.CVVersion) {
	t.Helper()
	versions, err := target.service.ListCVVersions()
	job, jobErr := target.service.GetFullJobDetails("owner-a")
	if err != nil || jobErr != nil || len(versions) != 2 || job.SelectedCVVersion == nil || job.SelectedCVVersion.ID != version.ID {
		t.Fatalf("versions=%+v job=%+v errors=%v/%v", versions, job, err, jobErr)
	}
	data, err := os.ReadFile(filepath.Join(target.cfg.CVDir, version.StoredFilename))
	if err != nil || string(data) != "CV backup bytes" {
		t.Fatalf("restored CV=%q err=%v", data, err)
	}
	next, err := target.service.StoreCVVersion("resume.pdf", strings.NewReader("version four"), int64(len("version four")), "application/pdf")
	if err != nil || next.Version != 4 {
		t.Fatalf("next version after ZIP import=%+v err=%v", next, err)
	}
}

func TestRecoverySnapshotPreservesCVMetadataAndSelection(t *testing.T) {
	fixture := newAttachmentOwnerFixture(t)
	version := seedRecoveryCVVersions(t, fixture)
	recoveryDB := openRecoveryTestDatabase(t, fixture)
	recovered := repository.New(recoveryDB)
	assertRecoveryVersions(t, recovered, fixture, version)
}

func seedRecoveryCVVersions(t *testing.T, fixture attachmentOwnerFixture) *models.CVVersion {
	t.Helper()
	version := storeCVTestVersion(t, fixture.service, "resume.pdf", "recovery CV bytes")
	if err := fixture.service.AssignCVVersion("owner-b", &version.ID); err != nil {
		t.Fatal(err)
	}
	storeCVTestVersion(t, fixture.service, "resume.pdf", "recovery two")
	deleted := storeCVTestVersion(t, fixture.service, "resume.pdf", "recovery three")
	if err := fixture.service.DeleteCVVersion(deleted.ID); err != nil {
		t.Fatal(err)
	}
	return version
}

func openRecoveryTestDatabase(t *testing.T, fixture attachmentOwnerFixture) *sql.DB {
	t.Helper()
	db, err := database.InitDB(&config.Config{
		DBPath: filepath.Join(fixture.cfg.DataDir, "recovered.db"), BackupPath: fixture.cfg.BackupPath,
		AttachmentsDir: fixture.cfg.AttachmentsDir,
		CVDir:          fixture.cfg.CVDir, LogosDir: fixture.cfg.LogosDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func assertRecoveryVersions(t *testing.T, recovered *repository.Repository, fixture attachmentOwnerFixture, version *models.CVVersion) {
	t.Helper()
	versions, err := recovered.ListCVVersions()
	job, jobErr := recovered.GetJobByID("owner-b")
	if err != nil || jobErr != nil || len(versions) != 2 || job.SelectedCVVersion == nil || job.SelectedCVVersion.ID != version.ID {
		t.Fatalf("recovered versions=%+v job=%+v errors=%v/%v", versions, job, err, jobErr)
	}
	service := NewJobService(recovered, &config.Config{BackupPath: fixture.cfg.BackupPath, AttachmentsDir: fixture.cfg.AttachmentsDir, CVDir: fixture.cfg.CVDir, LogosDir: fixture.cfg.LogosDir})
	next := storeCVTestVersion(t, service, "resume.pdf", "recovery four")
	if next.Version != 4 {
		t.Fatalf("next version after recovery restore=%+v", next)
	}
}

func TestLegacyV1BackupConvertsIdenticalPDFAttachments(t *testing.T) {
	fixture := newAttachmentOwnerFixture(t)
	addLegacyCVAttachments(t, fixture.service)
	current := exportCVBackup(t, fixture.service)
	legacy := convertCVArchiveToV1(t, current)
	importCVBackup(t, fixture.service, legacy)
	assertLegacyCVImport(t, fixture)
}

func addLegacyCVAttachments(t *testing.T, service *JobService) {
	t.Helper()
	for _, jobID := range []string{"owner-a", "owner-b"} {
		if _, err := service.StoreAttachment(jobID, nil, "resume.pdf", strings.NewReader("legacy CV bytes"), int64(len("legacy CV bytes")), "application/pdf"); err != nil {
			t.Fatal(err)
		}
	}
}

func convertCVArchiveToV1(t *testing.T, archive []byte) []byte {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}
	var legacy bytes.Buffer
	writer := zip.NewWriter(&legacy)
	for _, entry := range reader.File {
		content, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(content)
		_ = content.Close()
		if err != nil {
			t.Fatal(err)
		}
		if entry.Name == "manifest.json" {
			data = legacyManifestData(t, data)
		}
		output, err := writer.Create(entry.Name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := output.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return legacy.Bytes()
}

func legacyManifestData(t *testing.T, data []byte) []byte {
	t.Helper()
	var manifest backupManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Version = 1
	manifest.CVVersions = nil
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func assertLegacyCVImport(t *testing.T, fixture attachmentOwnerFixture) {
	t.Helper()
	versions, err := fixture.service.ListCVVersions()
	attachments, attachmentErr := fixture.repo.GetAllAttachments()
	if err != nil || attachmentErr != nil || len(versions) != 1 || versions[0].Version != 1 || len(attachments) != 0 {
		t.Fatalf("versions=%+v attachments=%+v errors=%v/%v", versions, attachments, err, attachmentErr)
	}
}
