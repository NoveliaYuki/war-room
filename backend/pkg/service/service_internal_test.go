package service

import (
	"database/sql"
	"errors"
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

func TestJobStageAndQuestionServiceCRUD(t *testing.T) {
	fixture := newAttachmentOwnerFixture(t)
	assertJobQueries(t, fixture.service)
	assertJobUpdates(t, fixture.service)
	assertStageQuestionLifecycle(t, fixture.service)
}

func assertJobQueries(t *testing.T, service *JobService) {
	t.Helper()
	jobs, err := service.GetAllJobs("ongoing", "Example")
	if err != nil || len(jobs) != 2 {
		t.Fatalf("filtered jobs=%d err=%v", len(jobs), err)
	}
	counts, err := service.GetJobCounts()
	if err != nil || counts.All != 2 || counts.Ongoing != 2 {
		t.Fatalf("counts=%+v err=%v", counts, err)
	}
	if err := service.ReorderJobs([]string{"owner-b", "owner-a"}); err != nil {
		t.Fatalf("reorder jobs: %v", err)
	}
	if err := service.ReorderJobs([]string{"owner-a", "owner-a"}); err == nil {
		t.Fatal("duplicate job order should fail")
	}
	if err := service.DeleteJob("owner-b"); err != nil {
		t.Fatalf("delete job: %v", err)
	}
}

func assertJobUpdates(t *testing.T, service *JobService) {
	t.Helper()
	blank := "  "
	if _, err := service.UpdateJob("owner-a", models.UpdateJobInput{PositionTitle: &blank}); !errors.Is(err, ErrPositionTitleRequired) {
		t.Fatalf("blank title error=%v", err)
	}
	title := "Staff Engineer"
	updated, err := service.UpdateJob("owner-a", models.UpdateJobInput{PositionTitle: &title})
	if err != nil || updated.PositionTitle != title {
		t.Fatalf("updated job=%+v err=%v", updated, err)
	}
	assertAllJobFieldsUpdate(t, service)
	if _, err := service.UpdateJob("missing", models.UpdateJobInput{}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing job update error=%v", err)
	}
}

func assertAllJobFieldsUpdate(t *testing.T, service *JobService) {
	t.Helper()
	company, title, status := "Updated Co", "Principal Engineer", models.StatusAccepted
	salaryType, currency := models.SalaryLimited, "USD"
	minSalary, maxSalary := int64(150), int64(200)
	recruiterType, recruiterName := models.RecruiterExternal, "Alex Recruiter"
	recruiterAgency, recruiterContact := "Agency", "alex@example.test"
	url, avatar, keyword := "https://example.test/job", "seed", "distributed systems"
	description, overview, domain := "Role description", "Company profile", "example.test"
	interviewNotes, reasons, experience, expected := "notes", "growth", "platform work", "170k USD"
	work, employment := models.WorkArrangementRemote, models.EmploymentTypePermanent
	referral, order := true, 1
	updated, err := service.UpdateJob("owner-a", models.UpdateJobInput{
		CompanyName: &company, PositionTitle: &title, Status: &status, SalaryType: &salaryType, SalaryMin: &minSalary,
		SalaryMax: &maxSalary, SalaryCurrency: &currency, RecruiterType: &recruiterType, RecruiterName: &recruiterName,
		RecruiterAgency: &recruiterAgency, RecruiterContact: &recruiterContact, Interviewers: []models.Interviewer{{Name: "Jamie", Role: "Hiring manager"}},
		JobPostURL: &url, AvatarSeed: &avatar, KeywordNote: &keyword, Description: &description, CompanyOverview: &overview,
		CompanyDomain: &domain, InterviewNotes: &interviewNotes, ReasonsToChange: &reasons, ExperienceNotes: &experience,
		ExpectedSalary: &expected, WorkArrangement: &work, EmploymentType: &employment, IsReferral: &referral, OrderIndex: &order,
	})
	if err != nil || updated.CompanyName != company || updated.Status != status || updated.SalaryMin == nil || *updated.SalaryMin != minSalary || updated.RecruiterName == nil || *updated.RecruiterName != recruiterName || !updated.IsReferral {
		t.Fatalf("updated fields=%+v err=%v", updated, err)
	}
}

func assertStageQuestionLifecycle(t *testing.T, service *JobService) {
	t.Helper()
	stage := createAndUpdateTestStage(t, service)
	assertTestQuestionLifecycle(t, service, stage.ID)
	if err := service.DeleteStage(stage.ID); err != nil {
		t.Fatal(err)
	}
}

func createAndUpdateTestStage(t *testing.T, service *JobService) *models.Stage {
	t.Helper()
	stage, err := service.CreateStage(models.CreateStageInput{JobID: "owner-a", StageType: models.StageTechnical})
	if err != nil {
		t.Fatal(err)
	}
	description, date, meetingType := "System design", "2030-06-15", "onsite"
	updated, err := service.UpdateStage(stage.ID, models.UpdateStageInput{Description: &description, MeetingDate: &date, MeetingType: &meetingType})
	if err != nil || updated.Description != description || updated.MeetingDate == nil || *updated.MeetingDate != date {
		t.Fatalf("updated stage=%+v err=%v", updated, err)
	}
	assertStageMeetingOperations(t, service, stage.ID, date)
	assertStageOrdering(t, service, stage.ID)
	return stage
}

func assertStageMeetingOperations(t *testing.T, service *JobService, stageID, date string) {
	t.Helper()
	invalidDate := "invalid"
	if _, err := service.UpdateStage(stageID, models.UpdateStageInput{MeetingDate: &invalidDate}); !errors.Is(err, ErrInvalidMeetingDate) {
		t.Fatalf("invalid meeting date error=%v", err)
	}
	meeting := "video"
	if err := service.ScheduleMeeting(stageID, models.ScheduleMeetingInput{MeetingDate: date, MeetingTime: "10:00", MeetingType: &meeting}); err != nil {
		t.Fatalf("schedule meeting: %v", err)
	}
	if _, err := service.GetScheduledMeetings(); err != nil {
		t.Fatalf("scheduled meetings: %v", err)
	}
}

func assertStageOrdering(t *testing.T, service *JobService, stageID string) {
	t.Helper()
	if _, err := service.SetCurrentStage(stageID); err != nil {
		t.Fatalf("set current stage: %v", err)
	}
	if err := service.ReorderStages("owner-a", []string{stageID, "stage-a"}); err != nil {
		t.Fatalf("reorder stages: %v", err)
	}
}

func assertTestQuestionLifecycle(t *testing.T, service *JobService, stageID string) {
	t.Helper()
	question, err := service.CreateQuestion(models.CreateQuestionInput{StageID: stageID, Question: "  Design a cache  "})
	if err != nil || question.Question != "Design a cache" {
		t.Fatalf("question=%+v err=%v", question, err)
	}
	updatedText, notes := "Design a distributed cache", "Discuss eviction"
	asked := true
	changed, err := service.UpdateQuestion(question.ID, models.UpdateQuestionInput{Question: &updatedText, AnswerNotes: &notes, IsAsked: &asked})
	if err != nil || changed.Question != updatedText || !changed.IsAsked {
		t.Fatalf("updated question=%+v err=%v", changed, err)
	}
	if err := service.DeleteQuestion(question.ID); err != nil {
		t.Fatal(err)
	}
	assertQuestionReordering(t, service, stageID)
}

func assertQuestionReordering(t *testing.T, service *JobService, stageID string) {
	t.Helper()
	other, err := service.CreateQuestion(models.CreateQuestionInput{StageID: stageID, Question: "Follow-up"})
	if err != nil {
		t.Fatal(err)
	}
	another, err := service.CreateQuestion(models.CreateQuestionInput{StageID: stageID, Question: "Edge cases"})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.ReorderQuestions(stageID, []string{other.ID, other.ID}); err == nil {
		t.Fatal("duplicate question order should fail")
	}
	if err := service.ReorderQuestions(stageID, []string{another.ID, other.ID}); err != nil {
		t.Fatalf("reorder questions: %v", err)
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

func TestValidateModelEnums(t *testing.T) {
	tests := []struct {
		name    string
		valid   func() error
		invalid func() error
	}{
		{"job status", func() error { return validateJobStatus(models.StatusOngoing) }, func() error { return validateJobStatus("unknown") }},
		{"recruiter type", func() error { return validateRecruiterType(models.RecruiterExternal) }, func() error { return validateRecruiterType("unknown") }},
		{"salary type", func() error { return validateSalaryType(models.SalaryUnknown) }, func() error { return validateSalaryType("bogus") }},
		{"stage type", func() error { return validateStageType(models.StageTechnical) }, func() error { return validateStageType("unknown") }},
		{"stage status", func() error { return validateStageStatus(models.StageStatusSkipped) }, func() error { return validateStageStatus("unknown") }},
		{"meeting type", func() error { return validateMeetingType("onsite") }, func() error { return validateMeetingType("unknown") }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.valid(); err != nil {
				t.Fatalf("valid enum rejected: %v", err)
			}
			if err := test.invalid(); !errors.Is(err, ErrInvalidField) {
				t.Fatalf("invalid enum error=%v, want ErrInvalidField", err)
			}
		})
	}
}

func TestValidateJobInputEnums(t *testing.T) {
	if err := validateCreateJobEnums(models.CreateJobInput{}); err != nil {
		t.Fatalf("empty create input rejected: %v", err)
	}
	badStatus := models.JobStatus("invalid")
	if err := validateCreateJobEnums(models.CreateJobInput{Status: &badStatus}); !errors.Is(err, ErrInvalidField) {
		t.Fatalf("invalid create status error=%v", err)
	}
	badRecruiter := models.RecruiterType("invalid")
	badSalary := models.SalaryType("invalid")
	badArrangement := models.WorkArrangement("invalid")
	badEmployment := models.EmploymentType("invalid")
	for _, input := range []models.CreateJobInput{
		{RecruiterType: &badRecruiter}, {SalaryType: &badSalary},
		{WorkArrangement: &badArrangement}, {EmploymentType: &badEmployment},
	} {
		if err := validateCreateJobEnums(input); !errors.Is(err, ErrInvalidField) {
			t.Errorf("invalid create enum accepted: %+v, error=%v", input, err)
		}
	}
	if err := validateUpdateJobEnums(models.UpdateJobInput{}); err != nil {
		t.Fatalf("empty update input rejected: %v", err)
	}
	for _, input := range []models.UpdateJobInput{
		{Status: &badStatus}, {RecruiterType: &badRecruiter}, {SalaryType: &badSalary},
		{WorkArrangement: &badArrangement}, {EmploymentType: &badEmployment},
	} {
		if err := validateUpdateJobEnums(input); !errors.Is(err, ErrInvalidField) {
			t.Errorf("invalid update enum accepted: %+v, error=%v", input, err)
		}
	}
}

func TestJobServiceRejectsInvalidEnumsBeforePersistence(t *testing.T) {
	fixture := newAttachmentOwnerFixture(t)
	badStatus := models.JobStatus("invalid")
	if _, err := fixture.service.CreateJob(models.CreateJobInput{PositionTitle: "Engineer", Status: &badStatus}); !errors.Is(err, ErrInvalidField) {
		t.Fatalf("invalid job status error=%v", err)
	}
	if _, err := fixture.service.CreateStage(models.CreateStageInput{JobID: "owner-a", StageType: "invalid"}); !errors.Is(err, ErrInvalidField) {
		t.Fatalf("invalid stage type error=%v", err)
	}
	badStageStatus := models.StageStatus("invalid")
	if _, err := fixture.service.UpdateStage(fixture.ownedStage.ID, models.UpdateStageInput{Status: &badStageStatus}); !errors.Is(err, ErrInvalidField) {
		t.Fatalf("invalid stage status error=%v", err)
	}
	badMeetingType := "invalid"
	if err := fixture.service.ScheduleMeeting(fixture.ownedStage.ID, models.ScheduleMeetingInput{MeetingType: &badMeetingType}); !errors.Is(err, ErrInvalidField) {
		t.Fatalf("invalid meeting type error=%v", err)
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
		AttachmentsDir: filepath.Join(directory, "attachments"), CVDir: filepath.Join(directory, "cvs"), LogosDir: filepath.Join(directory, "logos"),
	}
	db, err := database.InitDB(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	repo := repository.New(db)
	service := NewJobService(repo, cfg)
	for _, dir := range []string{cfg.AttachmentsDir, cfg.CVDir, cfg.LogosDir} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
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
	if err := fixture.service.validateAttachmentOwner("missing", nil); !errors.Is(err, ErrInvalidAttachmentOwner) {
		t.Fatalf("missing job error=%v", err)
	}
	missingStage := "missing-stage"
	if err := fixture.service.validateAttachmentOwner("owner-a", &missingStage); !errors.Is(err, ErrInvalidAttachmentOwner) {
		t.Fatalf("missing stage error=%v", err)
	}
	if err := fixture.service.validateAttachmentOwner("owner-a", &fixture.foreignStage.ID); !errors.Is(err, ErrInvalidAttachmentOwner) {
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

func TestNormalizeJobWorkDetailsOptions(t *testing.T) {
	defaultWork, defaultEmployment, err := normalizeJobWorkDetails(models.CreateJobInput{})
	if err != nil || defaultWork != models.WorkArrangementUnknown || defaultEmployment != models.EmploymentTypeUnknown {
		t.Fatalf("default work details=%q/%q err=%v", defaultWork, defaultEmployment, err)
	}
	remote := models.WorkArrangementRemote
	permanent := models.EmploymentTypePermanent
	work, employment, err := normalizeJobWorkDetails(models.CreateJobInput{WorkArrangement: &remote, EmploymentType: &permanent})
	if err != nil || work != remote || employment != permanent {
		t.Fatalf("valid work details=%q/%q err=%v", work, employment, err)
	}
	invalidWork := models.WorkArrangement("invalid")
	if _, _, err := normalizeJobWorkDetails(models.CreateJobInput{WorkArrangement: &invalidWork}); err == nil {
		t.Fatal("invalid work arrangement accepted")
	}
	invalidEmployment := models.EmploymentType("invalid")
	if _, _, err := normalizeJobWorkDetails(models.CreateJobInput{EmploymentType: &invalidEmployment}); err == nil {
		t.Fatal("invalid employment type accepted")
	}

}

func TestNormalizeSalaryOptions(t *testing.T) {
	min, max := int64(120), int64(80)
	cases := []struct {
		name             string
		input            models.CreateJobInput
		kind             models.SalaryType
		wantMin, wantMax *int64
	}{
		{name: "both reordered", input: models.CreateJobInput{SalaryMin: &min, SalaryMax: &max}, kind: models.SalaryLimited, wantMin: &max, wantMax: &min},
		{name: "minimum only", input: models.CreateJobInput{SalaryMin: &min}, kind: models.SalaryNoMax, wantMin: &min},
		{name: "maximum only", input: models.CreateJobInput{SalaryMax: &max}, kind: models.SalaryNoMin, wantMax: &max},
		{name: "explicit without bounds", input: models.CreateJobInput{SalaryType: ptrSalaryType(models.SalaryLimited)}, kind: models.SalaryLimited},
		{name: "unknown", input: models.CreateJobInput{}, kind: models.SalaryUnknown},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			kind, gotMin, gotMax := normalizeSalary(test.input)
			if kind != test.kind || !equalInt64(gotMin, test.wantMin) || !equalInt64(gotMax, test.wantMax) {
				t.Fatalf("normalized salary=(%q,%v,%v), want (%q,%v,%v)", kind, gotMin, gotMax, test.kind, test.wantMin, test.wantMax)
			}
		})
	}
}

func ptrSalaryType(value models.SalaryType) *models.SalaryType { return &value }

func equalInt64(left, right *int64) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func TestUpdateStageFieldBuildersIncludeAllOptionalFields(t *testing.T) {
	text := func(value string) *string { return &value }
	status := models.StageStatusCompleted
	recruiterType := models.RecruiterExternal
	input := models.UpdateStageInput{
		CustomTitle: text("Final interview"), Description: text("Panel round"), Status: &status, Notes: text("Bring examples"),
		MeetingDate: text("2030-06-15"), MeetingTime: text("10:00"), MeetingURL: text("https://meet.example"), MeetingType: text("video"),
		RecruiterName: text("Alex"), RecruiterType: &recruiterType, RecruiterAgency: text("Agency"), RecruiterContact: text("alex@example.test"),
		Interviewers: []models.Interviewer{{Name: "Pat", Role: "Engineer"}},
	}
	fields := make(map[string]interface{})
	addStageCoreFields(fields, input)
	addStageMeetingFields(fields, input)
	addStageRecruiterFields(fields, input)
	for _, key := range []string{"custom_title", "description", "status", "notes", "interviewers_json", "meeting_date", "meeting_time", "meeting_url", "meeting_type", "recruiter_name", "recruiter_type", "recruiter_agency", "recruiter_contact"} {
		if _, ok := fields[key]; !ok {
			t.Errorf("update field %q missing from built map", key)
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
