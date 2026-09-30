package service

import (
	"database/sql"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"war-room/backend/pkg/config"
	"war-room/backend/pkg/database"
	"war-room/backend/pkg/models"
	"war-room/backend/pkg/repository"
)

func TestLogoExtensionForMimeType(t *testing.T) {
	tests := map[string]string{
		"image/png":     ".png",
		"image/jpeg":    ".jpg",
		"image/svg+xml": ".svg",
		"image/webp":    ".webp",
		"image/gif":     "",
		"text/plain":    "",
	}
	for mimeType, expected := range tests {
		if got := logoExtensionForMimeType(mimeType); got != expected {
			t.Errorf("logoExtensionForMimeType(%q)=%q, want %q", mimeType, got, expected)
		}
	}
}

func TestSetSalaryRangeNormalizesRangesAndTypes(t *testing.T) {
	minimum, maximum := int64(140), int64(90)
	requested := models.SalaryType("custom")
	tests := []struct {
		name      string
		min, max  *int64
		requested *models.SalaryType
		wantType  models.SalaryType
		wantMin   interface{}
		wantMax   interface{}
	}{
		{"reversed range", &minimum, &maximum, nil, models.SalaryLimited, int64(90), int64(140)},
		{"minimum only", &minimum, nil, nil, models.SalaryNoMax, int64(140), nil},
		{"maximum only", nil, &maximum, nil, models.SalaryNoMin, nil, int64(90)},
		{"requested type", nil, nil, &requested, models.SalaryType("custom"), nil, nil},
		{"unknown by default", nil, nil, nil, models.SalaryUnknown, nil, nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fields := make(map[string]interface{})
			setSalaryRange(fields, test.min, test.max, test.requested)
			if fields["salary_type"] != string(test.wantType) || !reflect.DeepEqual(fields["salary_min"], test.wantMin) || !reflect.DeepEqual(fields["salary_max"], test.wantMax) {
				t.Fatalf("salary fields=%#v", fields)
			}
		})
	}
}

func TestJobAvatarUsesSeedOrCreatesFallback(t *testing.T) {
	seed := "  fixed-avatar  "
	if got := jobAvatar("Example", "Engineer", &seed); got != "fixed-avatar" {
		t.Fatalf("seed avatar=%q", got)
	}
	if got := jobAvatar("Example", "Engineer", nil); !strings.HasPrefix(got, "Example-Engineer-") || len(got) <= len("Example-Engineer-") {
		t.Fatalf("generated avatar=%q", got)
	}
}

func TestValidateAttachmentOwnerChecksJobAndStageOwnership(t *testing.T) {
	fixture := newAttachmentOwnerFixture(t)
	assertAttachmentOwnership(t, fixture)
	assertAttachmentLifecycle(t, fixture)
	assertAttachmentPersistenceErrors(t, fixture)
	assertClosedAttachmentService(t, fixture)
}

type attachmentOwnerFixture struct {
	cfg          *config.Config
	db           *sql.DB
	repo         *repository.Repository
	service      *JobService
	ownedStage   *models.Stage
	foreignStage *models.Stage
}

func newAttachmentOwnerFixture(t *testing.T) attachmentOwnerFixture {
	t.Helper()
	directory := t.TempDir()
	cfg := &config.Config{
		DataDir: directory, DBPath: filepath.Join(directory, "service.db"),
		BackupPath:     filepath.Join(directory, "backup.json"),
		AttachmentsDir: filepath.Join(directory, "attachments"), LogosDir: filepath.Join(directory, "logos"),
	}
	db, err := database.InitDB(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	repo := repository.New(db)
	service := NewJobService(repo, cfg)
	if err := os.MkdirAll(cfg.AttachmentsDir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"owner-a", "owner-b"} {
		job := &models.Job{ID: id, CompanyName: "Example", PositionTitle: "Engineer", Status: models.StatusOngoing,
			SalaryType: models.SalaryUnknown, SalaryCurrency: "EUR", RecruiterType: models.RecruiterNone}
		if err := repo.InsertJob(job); err != nil {
			t.Fatal(err)
		}
	}
	stage := &models.Stage{ID: "stage-b", JobID: "owner-b", StageType: models.StageHR, Status: models.StageStatusPending,
		MeetingType: "video", RecruiterType: models.RecruiterNone}
	if err := repo.InsertStage(stage); err != nil {
		t.Fatal(err)
	}
	ownedStage := &models.Stage{ID: "stage-a", JobID: "owner-a", StageType: models.StageHR, Status: models.StageStatusPending,
		MeetingType: "video", RecruiterType: models.RecruiterNone}
	if err := repo.InsertStage(ownedStage); err != nil {
		t.Fatal(err)
	}
	return attachmentOwnerFixture{cfg: cfg, db: db, repo: repo, service: service, ownedStage: ownedStage, foreignStage: stage}
}

func assertAttachmentOwnership(t *testing.T, fixture attachmentOwnerFixture) {
	t.Helper()
	if err := fixture.service.validateAttachmentOwner("owner-a", nil); err != nil {
		t.Fatalf("job attachment owner: %v", err)
	}
	if err := fixture.service.validateAttachmentOwner("owner-a", &fixture.ownedStage.ID); err != nil {
		t.Fatalf("stage attachment owner: %v", err)
	}
	if err := fixture.service.validateAttachmentOwner("missing", nil); err != sql.ErrNoRows {
		t.Fatalf("missing job error=%v", err)
	}
	missingStage := "missing-stage"
	if err := fixture.service.validateAttachmentOwner("owner-a", &missingStage); err != sql.ErrNoRows {
		t.Fatalf("missing stage error=%v", err)
	}
	if err := fixture.service.validateAttachmentOwner("owner-a", &fixture.foreignStage.ID); err == nil {
		t.Fatal("stage owned by another job should fail")
	}
}

func assertAttachmentLifecycle(t *testing.T, fixture attachmentOwnerFixture) {
	t.Helper()
	attachment, err := fixture.service.StoreAttachment("owner-a", nil, "resume.txt", strings.NewReader("resume"), 6, "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.db.Exec(`CREATE TRIGGER reject_attachment_delete BEFORE DELETE ON attachments BEGIN SELECT RAISE(ABORT, 'attachment delete unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.DeleteAttachment(attachment.ID); err == nil {
		t.Fatal("attachment database delete failure should be returned")
	}
	if _, err := os.Stat(filepath.Join(fixture.cfg.AttachmentsDir, attachment.StoredFilename)); err != nil {
		t.Fatalf("file removed despite database delete failure: %v", err)
	}
}

func assertAttachmentPersistenceErrors(t *testing.T, fixture attachmentOwnerFixture) {
	t.Helper()
	if err := fixture.service.MirrorDatabaseToJSON(); err != nil {
		t.Fatalf("mirror valid database: %v", err)
	}
	fixture.cfg.BackupPath = filepath.Join(filepath.Dir(fixture.cfg.DBPath), "missing", "backup.json")
	if err := fixture.service.MirrorDatabaseToJSON(); err == nil {
		t.Fatal("backup write error should be returned")
	}
	if _, err := fixture.db.Exec(`CREATE TRIGGER reject_default_stage BEFORE INSERT ON stages BEGIN SELECT RAISE(ABORT, 'stage unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.CreateJob(models.CreateJobInput{CompanyName: "Rollback", PositionTitle: "Engineer"}); err == nil {
		t.Fatal("default stage insertion error should fail job creation")
	}
	jobs, err := fixture.repo.GetAllJobs("", "")
	if err != nil || len(jobs) != 2 {
		t.Fatalf("jobs after rollback=%d err=%v", len(jobs), err)
	}
}

func assertClosedAttachmentService(t *testing.T, fixture attachmentOwnerFixture) {
	t.Helper()
	if err := fixture.db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.validateAttachmentOwner("owner-a", nil); err == nil || !strings.Contains(err.Error(), "load attachment job") {
		t.Fatalf("closed database error=%v", err)
	}
	if err := fixture.service.MirrorDatabaseToJSON(); err == nil {
		t.Fatal("database snapshot error should be returned")
	}
	if _, err := fixture.service.CreateJob(models.CreateJobInput{CompanyName: "Unavailable", PositionTitle: "Engineer"}); err == nil {
		t.Fatal("closed database insert error should fail job creation")
	}
}

func TestSafeAttachmentExtension(t *testing.T) {
	tests := map[string]string{
		"resume.pdf":                ".pdf",
		"folder/resume.PDF":         ".PDF",
		"resume":                    "",
		"resume.":                   "",
		"resume.extensionistoolong": "",
		"resume.pd/f":               "",
	}
	for filename, expected := range tests {
		if got := safeAttachmentExtension(filename); got != expected {
			t.Errorf("safeAttachmentExtension(%q)=%q, want %q", filename, got, expected)
		}
	}
}

func TestRemoveStoredAttachmentRejectsUnsafeNames(t *testing.T) {
	directory := t.TempDir()
	validPath := filepath.Join(directory, "resume.pdf")
	unsafePath := filepath.Join(filepath.Dir(directory), "outside.pdf")
	if err := os.WriteFile(validPath, []byte("resume"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unsafePath, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	removeStoredAttachment(directory, "../outside.pdf")
	removeStoredAttachment(directory, "resume.pdf")
	removeStoredAttachment(directory, ".")
	if _, err := os.Stat(validPath); !os.IsNotExist(err) {
		t.Fatalf("stored file remains after cleanup: %v", err)
	}
	if data, err := readTestFile(t, unsafePath); err != nil || string(data) != "keep" {
		t.Fatalf("unsafe path was modified: data=%q err=%v", data, err)
	}
}

func TestWriteBackupAtomicallySuccessAndFilesystemErrors(t *testing.T) {
	directory := t.TempDir()
	backupPath := filepath.Join(directory, "backup.json")
	if err := writeBackupAtomically(backupPath, []byte("first")); err != nil {
		t.Fatalf("write backup: %v", err)
	}
	if err := writeBackupAtomically(backupPath, []byte("second")); err != nil {
		t.Fatalf("replace backup: %v", err)
	}
	if data, err := readTestFile(t, backupPath); err != nil || string(data) != "second" {
		t.Fatalf("backup data=%q err=%v", data, err)
	}
	if err := writeBackupAtomically(filepath.Join(directory, "missing", "backup.json"), []byte("data")); err == nil {
		t.Fatal("missing parent should fail to create the temporary backup")
	}
	if err := writeBackupAtomically(directory, []byte("data")); err == nil {
		t.Fatal("directory destination should fail atomic replacement")
	}
}

func TestInstallImportedLogosAndRollback(t *testing.T) {
	stagingDirectory := t.TempDir()
	logosDirectory := t.TempDir()
	logo := validTestLogo("example.test", ".png")
	stagedPath := filepath.Join(stagingDirectory, "example.test.png")
	destination := filepath.Join(logosDirectory, "example.test.png")
	if err := os.WriteFile(stagedPath, []byte("new logo"), 0600); err != nil {
		t.Fatal(err)
	}
	installed, err := installImportedLogos([]backupLogo{logo}, stagingDirectory, logosDirectory)
	if err != nil {
		t.Fatalf("install new logo: %v", err)
	}
	assertFileContents(t, destination, "new logo")
	rollbackImportedLogos(installed)
	assertPathMissing(t, destination)

	if err := os.WriteFile(destination, []byte("old logo"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stagedPath, []byte("replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	installed, err = installImportedLogos([]backupLogo{logo}, stagingDirectory, logosDirectory)
	if err != nil {
		t.Fatalf("replace existing logo: %v", err)
	}
	assertFileContents(t, destination, "replacement")
	rollbackImportedLogos(installed)
	assertFileContents(t, destination, "old logo")
}

func TestInstallImportedLogosRollsBackEarlierFilesOnFailure(t *testing.T) {
	stagingDirectory := t.TempDir()
	logosDirectory := t.TempDir()
	first := validTestLogo("first.test", ".png")
	second := validTestLogo("second.test", ".png")
	if err := os.WriteFile(filepath.Join(stagingDirectory, "first.test.png"), []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := installImportedLogos([]backupLogo{first, second}, stagingDirectory, logosDirectory); err == nil {
		t.Fatal("missing staged logo should fail import")
	}
	assertPathMissing(t, filepath.Join(logosDirectory, "first.test.png"))
}

func TestInstallImportedLogoReportsPreservationFailure(t *testing.T) {
	logosDirectory := t.TempDir()
	if err := os.WriteFile(filepath.Join(logosDirectory, "example.test.png"), []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := installImportedLogos([]backupLogo{validTestLogo("example.test", ".png")}, filepath.Join(t.TempDir(), "missing-stage"), logosDirectory); err == nil {
		t.Fatal("missing staging directory should fail to preserve an existing logo")
	}
	assertFileContents(t, filepath.Join(logosDirectory, "example.test.png"), "old")
}

func TestInstallImportedLogoReportsDestinationInspectionFailure(t *testing.T) {
	stagingDirectory := t.TempDir()
	logosParent := t.TempDir()
	logosDirectory := filepath.Join(logosParent, "not-a-directory")
	if err := os.WriteFile(logosDirectory, []byte("blocker"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stagingDirectory, "example.test.png"), []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := installImportedLogos([]backupLogo{validTestLogo("example.test", ".png")}, stagingDirectory, logosDirectory); err == nil {
		t.Fatal("non-directory logo root should fail inspection")
	}
}

func TestNormalizeLogoHostRejectsInvalidURLAndDNSLabels(t *testing.T) {
	tests := map[string]string{
		" HTTPS://Example.Test./path ": "example.test",
		"example.test":                 "example.test",
		"http://example.test?x=1":      "",
		"https://user@example.test":    "",
		"ftp://example.test":           "",
		"https://127.0.0.1":            "",
		"-invalid.test":                "",
		"invalid-.test":                "",
		"a..b":                         "",
	}
	for input, expected := range tests {
		if got := normalizeLogoHost(input); got != expected {
			t.Errorf("normalizeLogoHost(%q)=%q, want %q", input, got, expected)
		}
	}
}

func validTestLogo(domain, extension string) backupLogo {
	mimeType := "image/png"
	if extension == ".svg" {
		mimeType = "image/svg+xml"
	}
	return backupLogo{Domain: domain, Path: "logos/" + domain + extension, MimeType: mimeType, Size: 8, SHA256: "unused-in-installer"}
}

func assertFileContents(t *testing.T, path, expected string) {
	t.Helper()
	data, err := readTestFile(t, path)
	if err != nil || string(data) != expected {
		t.Fatalf("file %q contents=%q err=%v, want %q", path, data, err, expected)
	}
}

func readTestFile(t *testing.T, path string) ([]byte, error) {
	t.Helper()
	// #nosec G304 -- callers pass paths created within the test's temporary directory.
	return os.ReadFile(path)
}

func assertPathMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("path %q should not exist, got %v", path, err)
	}
}
