package backend_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"war-room/backend/pkg/config"
	"war-room/backend/pkg/database"
	"war-room/backend/pkg/handlers"
	"war-room/backend/pkg/models"
	"war-room/backend/pkg/repository"
	"war-room/backend/pkg/service"
)

func closeTestResource(t *testing.T, resource interface{ Close() error }) {
	t.Helper()
	if err := resource.Close(); err != nil {
		t.Errorf("close test resource: %v", err)
	}
}

func newCoverageApp(t *testing.T) (*http.ServeMux, *service.JobService, *repository.Repository, *config.Config) {
	mux, jobs, repo, cfg, _ := newCoverageDB(t)
	return mux, jobs, repo, cfg
}

func newCoverageDB(t *testing.T) (*http.ServeMux, *service.JobService, *repository.Repository, *config.Config, *sql.DB) {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{
		DataDir: dir, DBPath: filepath.Join(dir, "coverage.db"), BackupPath: filepath.Join(dir, "backup.json"),
		AttachmentsDir: filepath.Join(dir, "attachments"), LogosDir: filepath.Join(dir, "logos"),
	}
	for _, path := range []string{cfg.AttachmentsDir, cfg.LogosDir} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	db, err := database.InitDB(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeTestResource(t, db) })
	repo := repository.New(db)
	jobs := service.NewJobService(repo, cfg)
	logos := service.NewLogoService(cfg)
	mux := http.NewServeMux()
	handlers.NewHandler(jobs, logos, cfg).RegisterRoutes(mux)
	return mux, jobs, repo, cfg, db
}

func ptr[T any](v T) *T { return &v }

func TestRepositoryCompleteLifecycle(t *testing.T) {
	_, _, repo, _ := newCoverageApp(t)
	job := repositoryJobFixture(t, repo)
	secondJob := repositorySecondJobFixture(t, repo)
	repositoryJobUpdates(t, repo, job)
	repositoryJobQueries(t, repo)
	stage1, stage2 := repositoryStageFixture(t, repo, job.ID)
	repositoryStageUpdates(t, repo, job.ID, stage1.ID, stage2.ID)
	question := repositoryQuestionFixture(t, repo, stage2.ID)
	repositoryQuestionUpdates(t, repo, question, stage2.ID)
	attachment := repositoryAttachmentFixture(t, repo, job.ID, stage2.ID)
	repositoryCleanup(t, repo, job.ID, stage1.ID, question.ID, attachment.ID, secondJob.ID)
}

func repositoryJobFixture(t *testing.T, repo *repository.Repository) *models.Job {
	t.Helper()
	job := &models.Job{ID: "repo-job", CompanyName: "Acme", PositionTitle: "Engineer", Status: models.StatusOngoing,
		SalaryType: models.SalaryUnknown, SalaryCurrency: "EUR", RecruiterType: models.RecruiterNone,
		AvatarSeed: "acme", Interviewers: []models.Interviewer{{Name: "Ivy", Role: "Lead"}}}
	if err := repo.InsertJob(job); err != nil {
		t.Fatal(err)
	}
	if job.OrderIndex != 0 || job.InterviewersJSON == "" {
		t.Fatalf("insert did not populate derived fields: %+v", job)
	}
	if got, err := repo.GetJobByID(job.ID); err != nil || got == nil || len(got.Interviewers) != 1 {
		t.Fatalf("GetJobByID = %#v, %v", got, err)
	}
	assertJobPreparationFieldsPersist(t, repo, job.ID)
	if got, err := repo.GetJobByID("missing"); err != nil || got != nil {
		t.Fatalf("missing job = %#v, %v", got, err)
	}
	return job
}

func assertJobPreparationFieldsPersist(t *testing.T, repo *repository.Repository, jobID string) {
	t.Helper()
	if err := repo.UpdateJob(jobID, map[string]interface{}{"experience_notes": "platform migrations", "expected_salary": "flexible by scope"}); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetJobByID(jobID)
	if err != nil || got.ExperienceNotes != "platform migrations" || got.ExpectedSalary != "flexible by scope" {
		t.Fatalf("new preparation fields = %#v, %v", got, err)
	}
}

func repositoryJobUpdates(t *testing.T, repo *repository.Repository, job *models.Job) {
	t.Helper()
	if err := repo.UpdateJob(job.ID, map[string]interface{}{"company_name": "Updated", "keyword_note": strings.Repeat("x", 120), "is_referral": true, "not_allowed": "ignored"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateJob("missing", map[string]interface{}{"company_name": "Nope"}); err == nil {
		t.Fatalf("expected missing update error, got %v", err)
	}
	updated, err := repo.GetJobByID(job.ID)
	if err != nil || updated.CompanyName != "Updated" || len(updated.KeywordNote) != 100 || !updated.IsReferral {
		t.Fatalf("updated job = %#v, %v", updated, err)
	}
	if err := repo.UpdateJob("missing", map[string]interface{}{"unknown": "ignored"}); err != nil {
		t.Fatalf("empty update: %v", err)
	}
}

func repositorySecondJobFixture(t *testing.T, repo *repository.Repository) *models.Job {
	t.Helper()
	job := &models.Job{ID: "repo-job-2", CompanyName: "Beta", PositionTitle: "Designer", Status: models.StatusAccepted,
		SalaryType: models.SalaryUnknown, SalaryCurrency: "EUR", RecruiterType: models.RecruiterNone, AvatarSeed: "beta"}
	if err := repo.InsertJob(job); err != nil {
		t.Fatal(err)
	}
	return job
}

func repositoryJobQueries(t *testing.T, repo *repository.Repository) {
	t.Helper()
	for _, tc := range []struct {
		status, search string
		want           int
	}{{"", "", 2}, {"accepted", "", 1}, {"all", "", 2}, {"", "beta", 1}} {
		jobs, err := repo.GetAllJobs(tc.status, tc.search)
		if err != nil || len(jobs) != tc.want {
			t.Fatalf("GetAllJobs(%q,%q) count=%d err=%v", tc.status, tc.search, len(jobs), err)
		}
	}
	counts, err := repo.GetJobCounts()
	if err != nil || counts.All != 2 || counts.Ongoing != 1 || counts.Accepted != 1 {
		t.Fatalf("counts=%+v err=%v", counts, err)
	}
}

func repositoryStageFixture(t *testing.T, repo *repository.Repository, jobID string) (*models.Stage, *models.Stage) {
	t.Helper()
	stage1 := &models.Stage{ID: "stage-1", JobID: jobID, StageType: models.StageHR, Status: models.StageStatusCurrent, MeetingType: "video", RecruiterType: "none", Interviewers: []models.Interviewer{{Name: "Kim"}}}
	stage2 := &models.Stage{ID: "stage-2", JobID: jobID, StageType: models.StageTechnical, Status: models.StageStatusPending, MeetingType: "video", RecruiterType: "none"}
	if err := repo.InsertStage(stage1); err != nil {
		t.Fatal(err)
	}
	if err := repo.InsertStage(stage2); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.GetStageByID(stage1.ID); err != nil || got == nil || len(got.Interviewers) != 1 {
		t.Fatalf("GetStageByID=%#v err=%v", got, err)
	}
	if got, err := repo.GetStageByID("missing"); err != nil || got != nil {
		t.Fatalf("missing stage=%#v err=%v", got, err)
	}
	if stages, err := repo.GetStagesByJobID(jobID); err != nil || len(stages) != 2 {
		t.Fatalf("stages=%d err=%v", len(stages), err)
	}
	return stage1, stage2
}

func repositoryStageUpdates(t *testing.T, repo *repository.Repository, jobID, firstID, secondID string) {
	t.Helper()
	if err := repo.UpdateStage(secondID, map[string]interface{}{"status": "current", "meeting_date": "2030-01-02", "meeting_time": "09:30", "unsupported": "ignored"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateStage("missing", map[string]interface{}{"status": "pending"}); err == nil {
		t.Fatal("expected missing stage update error")
	}
	if err := repo.UpdateStage("missing", map[string]interface{}{"unknown": "ignored"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReorderStages(jobID, []string{secondID, firstID}); err != nil {
		t.Fatal(err)
	}
	if stages, err := repo.SetCurrentStage(secondID); err != nil || len(stages) != 2 || stages[0].Status != models.StageStatusCurrent {
		t.Fatalf("SetCurrentStage=%+v err=%v", stages, err)
	}
	if _, err := repo.SetCurrentStage("missing"); err == nil {
		t.Fatal("expected missing current stage error")
	}
	repositoryMeetings(t, repo)
}

func repositoryMeetings(t *testing.T, repo *repository.Repository) {
	t.Helper()
	meetings, err := repo.GetScheduledMeetings()
	if err != nil || len(meetings) != 1 || meetings[0].MeetingDate == nil {
		t.Fatalf("meetings=%+v err=%v", meetings, err)
	}
}

func repositoryQuestionFixture(t *testing.T, repo *repository.Repository, stageID string) *models.Question {
	t.Helper()
	q := &models.Question{ID: "question-1", StageID: stageID, Question: "How?", AnswerNotes: "Answer"}
	if err := repo.InsertQuestion(q); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.GetQuestionByID(q.ID); err != nil || got == nil || got.Question != q.Question {
		t.Fatalf("GetQuestionByID=%#v err=%v", got, err)
	}
	if got, err := repo.GetQuestionByID("missing"); err != nil || got != nil {
		t.Fatalf("missing question=%#v err=%v", got, err)
	}
	if qs, err := repo.GetQuestionsByStageID(stageID); err != nil || len(qs) != 1 {
		t.Fatalf("questions=%d err=%v", len(qs), err)
	}
	return q
}

func repositoryQuestionUpdates(t *testing.T, repo *repository.Repository, q *models.Question, stageID string) {
	t.Helper()
	if err := repo.UpdateQuestion(q.ID, map[string]interface{}{"question": "Updated?", "is_asked": true, "invalid": "ignored"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateQuestion("missing", map[string]interface{}{"question": "No"}); err == nil {
		t.Fatal("expected missing question update error")
	}
	if err := repo.UpdateQuestion("missing", map[string]interface{}{"invalid": "ignored"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReorderQuestions(stageID, []string{q.ID}); err != nil {
		t.Fatal(err)
	}
}

func repositoryAttachmentFixture(t *testing.T, repo *repository.Repository, jobID, stageID string) *models.Attachment {
	t.Helper()
	att := &models.Attachment{ID: "attachment-1", JobID: jobID, StageID: ptr(stageID), OriginalName: "cv.txt", StoredFilename: "stored.txt", FileSize: 4, MimeType: "text/plain"}
	if err := repo.InsertAttachment(att); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.GetAttachmentByID(att.ID); err != nil || got == nil || got.OriginalName != att.OriginalName {
		t.Fatalf("GetAttachmentByID=%#v err=%v", got, err)
	}
	if got, err := repo.GetAttachmentByID("missing"); err != nil || got != nil {
		t.Fatalf("missing attachment=%#v err=%v", got, err)
	}
	if atts, err := repo.GetAttachmentsByJobID(jobID); err != nil || len(atts) != 1 {
		t.Fatalf("attachments=%d err=%v", len(atts), err)
	}
	return att
}

func repositoryCleanup(t *testing.T, repo *repository.Repository, jobID, stageID, questionID, attachmentID, secondJobID string) {
	t.Helper()
	if err := repo.DeleteAttachment(attachmentID); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteAttachment(attachmentID); err == nil {
		t.Fatal("expected missing attachment delete error")
	}
	if err := repo.DeleteQuestion(questionID); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteQuestion(questionID); err == nil {
		t.Fatal("expected missing question delete error")
	}
	if err := repo.DeleteStage(stageID); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteStage(stageID); err == nil {
		t.Fatal("expected missing stage delete error")
	}
	if err := repo.DeleteJob(jobID); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteJob(jobID); err == nil {
		t.Fatal("expected missing job delete error")
	}
	if err := repo.ReorderJobs([]string{secondJobID}); err != nil {
		t.Fatal(err)
	}
}

// TestJobServiceCompleteLifecycle covers job, stage, question, and attachment operations.
func TestJobServiceCompleteLifecycle(t *testing.T) {
	_, jobs, _, cfg := newCoverageApp(t)
	created := createLifecycleJobForService(t, jobs)
	stage, second := createLifecycleStagesForService(t, jobs, created.ID)
	question := exerciseLifecycleQuestions(t, jobs, second.ID)
	exerciseLifecycleMeetingsAndBackup(t, jobs, cfg, created.ID, stage.ID, second.ID)
	exerciseLifecycleJobUpdates(t, jobs, created.ID)
	exerciseLifecycleFailures(t, jobs, stage.ID, question.ID)
	exerciseLifecycleAttachment(t, jobs, cfg, created.ID, second.ID)
	if err := jobs.ReorderJobs([]string{created.ID}); err != nil {
		t.Fatal(err)
	}
	if err := jobs.DeleteJob("missing"); err == nil {
		t.Fatal("expected missing job delete error")
	}
	if err := jobs.DeleteJob(created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := jobs.GetAttachmentByID("missing"); err != nil {
		t.Fatal(err)
	}
}

func createLifecycleJobForService(t *testing.T, jobs *service.JobService) *models.Job {
	t.Helper()
	if _, err := jobs.CreateJob(models.CreateJobInput{PositionTitle: "  	"}); err == nil {
		t.Fatal("expected required title error")
	}
	min, max := int64(150), int64(100)
	noDefaults := false
	workArrangement := models.WorkArrangementRemote
	created, err := jobs.CreateJob(models.CreateJobInput{CompanyName: "  Acme ", PositionTitle: " Engineer ", SalaryMin: &min, SalaryMax: &max,
		KeywordNote: ptr(strings.Repeat("k", 105)), ExperienceNotes: ptr("  platform migrations  "), ExpectedSalary: ptr("  flexible by scope  "),
		Interviewers: []models.Interviewer{{Name: "Pat"}}, IsReferral: ptr(true), WorkArrangement: &workArrangement, CreateDefaultStages: &noDefaults})
	if err != nil {
		t.Fatal(err)
	}
	assertCreatedJobFields(t, created)
	assertJobQueryResults(t, jobs)
	return created
}

func TestWorkArrangementValuesAndDefault(t *testing.T) {
	_, jobs, _, _ := newCoverageApp(t)
	invalidArrangement := models.WorkArrangement("office-ish")
	if _, err := jobs.CreateJob(models.CreateJobInput{PositionTitle: "Invalid mode", WorkArrangement: &invalidArrangement}); err == nil {
		t.Fatal("expected invalid work arrangement error when creating a job")
	}
	arrangements := []models.WorkArrangement{
		models.WorkArrangementUnknown,
		models.WorkArrangementRemote,
		models.WorkArrangementHybrid,
		models.WorkArrangementOnSite,
	}
	for index, arrangement := range arrangements {
		created, err := jobs.CreateJob(models.CreateJobInput{
			CompanyName: "Synthetic", PositionTitle: fmt.Sprintf("Role %d", index), WorkArrangement: &arrangement,
		})
		if err != nil || created.WorkArrangement != arrangement {
			t.Fatalf("arrangement %q: created=%+v err=%v", arrangement, created, err)
		}
	}
	created, err := jobs.CreateJob(models.CreateJobInput{PositionTitle: "Default work mode"})
	if err != nil || created.WorkArrangement != models.WorkArrangementUnknown {
		t.Fatalf("default arrangement: created=%+v err=%v", created, err)
	}
}

func assertCreatedJobFields(t *testing.T, created *models.Job) {
	t.Helper()
	if created.CompanyName != "Acme" || created.PositionTitle != "Engineer" || *created.SalaryMin != 100 || len(created.KeywordNote) != 100 || !created.IsReferral || created.ExperienceNotes != "platform migrations" || created.ExpectedSalary != "flexible by scope" || created.WorkArrangement != models.WorkArrangementRemote {
		t.Fatalf("created=%+v", created)
	}
}

func assertJobQueryResults(t *testing.T, jobs *service.JobService) {
	t.Helper()
	counts, err := jobs.GetJobCounts()
	if err != nil || counts.All != 1 {
		t.Fatalf("counts=%+v err=%v", counts, err)
	}
	results, err := jobs.GetAllJobs("", "acme")
	if err != nil || len(results) != 1 {
		t.Fatalf("jobs=%+v err=%v", results, err)
	}
	missing, err := jobs.GetFullJobDetails("missing")
	if err != nil || missing != nil {
		t.Fatalf("missing job=%v err=%v", missing, err)
	}
}

func createLifecycleStagesForService(t *testing.T, jobs *service.JobService, jobID string) (*models.Stage, *models.Stage) {
	t.Helper()
	recruiterType := models.RecruiterInternal
	stage, err := jobs.CreateStage(models.CreateStageInput{JobID: jobID, StageType: models.StageHR, CustomTitle: ptr("Screen"), Description: ptr("desc"), Notes: ptr("notes"), RecruiterName: ptr("Recruiter"), RecruiterType: &recruiterType, RecruiterAgency: ptr("Agency"), RecruiterContact: ptr("mail"), Interviewers: []models.Interviewer{{Name: "Alex"}}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := jobs.CreateStage(models.CreateStageInput{JobID: jobID, StageType: models.StageTechnical})
	if err != nil {
		t.Fatal(err)
	}
	status := models.StageStatusCompleted
	recruiterType = models.RecruiterExternal
	if _, err := jobs.UpdateStage(second.ID, models.UpdateStageInput{CustomTitle: ptr("Tech"), Description: ptr("description"), Status: &status, Notes: ptr("notes"), MeetingDate: ptr("2031-04-05"), MeetingTime: ptr("10:15"), MeetingURL: ptr("https://meet.invalid"), MeetingType: ptr("video"), RecruiterName: ptr("N"), RecruiterType: &recruiterType, RecruiterAgency: ptr("A"), RecruiterContact: ptr("C"), Interviewers: []models.Interviewer{{Name: "Lee"}}}); err != nil {
		t.Fatal(err)
	}
	return stage, second
}

func exerciseLifecycleMeetingsAndBackup(t *testing.T, jobs *service.JobService, cfg *config.Config, jobID, stageID, secondID string) {
	t.Helper()
	if err := jobs.ScheduleMeeting(secondID, models.ScheduleMeetingInput{MeetingDate: "2031-04-06", MeetingTime: "11:00", MeetingURL: ptr("https://meet.invalid/2"), MeetingType: ptr("phone")}); err != nil {
		t.Fatal(err)
	}
	if stages, err := jobs.SetCurrentStage(secondID); err != nil || len(stages) != 2 {
		t.Fatalf("stages=%+v err=%v", stages, err)
	}
	if meetings, err := jobs.GetScheduledMeetings(); err != nil || len(meetings) != 1 {
		t.Fatalf("meetings=%+v err=%v", meetings, err)
	}
	if err := jobs.ReorderStages(jobID, []string{secondID, stageID}); err != nil {
		t.Fatal(err)
	}
	if err := jobs.MirrorDatabaseToJSON(); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(cfg.BackupPath); err != nil || !json.Valid(data) {
		t.Fatalf("backup valid=%t err=%v", json.Valid(data), err)
	}
}

func exerciseLifecycleQuestions(t *testing.T, jobs *service.JobService, stageID string) *models.Question {
	t.Helper()
	question, err := jobs.CreateQuestion(models.CreateQuestionInput{StageID: stageID, Question: "  Why? ", AnswerNotes: " notes "})
	if err != nil || question.Question != "Why?" {
		t.Fatalf("question=%+v err=%v", question, err)
	}
	if _, err := jobs.CreateQuestion(models.CreateQuestionInput{StageID: stageID, Question: "  "}); err == nil {
		t.Fatal("expected blank question error")
	}
	asked := true
	if _, err := jobs.UpdateQuestion(question.ID, models.UpdateQuestionInput{Question: ptr(" New? "), AnswerNotes: ptr(" revised "), IsAsked: &asked}); err != nil {
		t.Fatal(err)
	}
	if err := jobs.ReorderQuestions(stageID, []string{question.ID}); err != nil {
		t.Fatal(err)
	}
	return question
}

func exerciseLifecycleJobUpdates(t *testing.T, jobs *service.JobService, jobID string) {
	t.Helper()
	assertEmploymentTypeCreate(t, jobs)
	minOnly := int64(90)
	updated, err := jobs.UpdateJob(jobID, models.UpdateJobInput{CompanyName: ptr(" "), PositionTitle: ptr(" Updated "), SalaryMin: &minOnly,
		SalaryCurrency: ptr("USD"), RecruiterType: ptr(models.RecruiterExternal), RecruiterName: ptr("R"), RecruiterAgency: ptr("Firm"), RecruiterContact: ptr("r@x"),
		Interviewers: []models.Interviewer{}, JobPostURL: ptr("https://job.invalid"), AvatarSeed: ptr("seed"), KeywordNote: ptr(" trimmed "), Description: ptr(" desc "),
		CompanyOverview: ptr(" overview "), CompanyDomain: ptr("acme.test"), InterviewNotes: ptr(" interview "), ReasonsToChange: ptr(" reason "),
		ExperienceNotes: ptr(" experience "), ExpectedSalary: ptr(" offer salary "), WorkArrangement: ptr(models.WorkArrangementHybrid),
		EmploymentType: ptr(models.EmploymentTypePermanentB2B), IsReferral: ptr(false), OrderIndex: ptr(4)})
	if err != nil {
		t.Fatal(err)
	}
	if updated.CompanyName != "Unknown" || updated.PositionTitle != "Updated" || updated.SalaryType != models.SalaryLimited || updated.SalaryCurrency != "USD" || updated.IsReferral || updated.ExperienceNotes != "experience" || updated.ExpectedSalary != "offer salary" {
		t.Fatalf("updated=%+v", updated)
	}
	assertWorkArrangementUpdate(t, jobs, jobID, updated.WorkArrangement)
	assertEmploymentTypeUpdate(t, jobs, jobID, updated.EmploymentType)
	unknown := models.SalaryUnknown
	if _, err := jobs.UpdateJob(jobID, models.UpdateJobInput{SalaryType: &unknown}); err != nil {
		t.Fatal(err)
	}
}

func assertEmploymentTypeCreate(t *testing.T, jobs *service.JobService) {
	t.Helper()
	employmentType := models.EmploymentTypePermanent
	created, err := jobs.CreateJob(models.CreateJobInput{
		CompanyName:    "Example Company",
		PositionTitle:  "Software Engineer",
		EmploymentType: &employmentType,
	})
	if err != nil {
		t.Fatalf("create job with employment type: %v", err)
	}
	if created.EmploymentType != employmentType {
		t.Fatalf("created employment type = %q, want %q", created.EmploymentType, employmentType)
	}
	if err := jobs.DeleteJob(created.ID); err != nil {
		t.Fatalf("delete synthetic job: %v", err)
	}
	invalid := models.EmploymentType("invalid")
	if _, err := jobs.CreateJob(models.CreateJobInput{
		CompanyName:    "Example Company",
		PositionTitle:  "Software Engineer",
		EmploymentType: &invalid,
	}); err == nil {
		t.Fatal("expected invalid employment type to be rejected on create")
	}
}

func assertEmploymentTypeUpdate(t *testing.T, jobs *service.JobService, jobID string, employmentType models.EmploymentType) {
	t.Helper()
	if employmentType != models.EmploymentTypePermanentB2B {
		t.Fatalf("employment type = %q, want permanent and B2B", employmentType)
	}
	invalidEmploymentType := models.EmploymentType("invalid")
	if _, err := jobs.UpdateJob(jobID, models.UpdateJobInput{EmploymentType: &invalidEmploymentType}); err == nil {
		t.Fatal("expected an invalid employment type to be rejected")
	}
}

func assertWorkArrangementUpdate(t *testing.T, jobs *service.JobService, jobID string, arrangement models.WorkArrangement) {
	t.Helper()
	if arrangement != models.WorkArrangementHybrid {
		t.Fatalf("updated work arrangement = %q, want hybrid", arrangement)
	}
	invalidArrangement := models.WorkArrangement("office-ish")
	if _, err := jobs.UpdateJob(jobID, models.UpdateJobInput{WorkArrangement: &invalidArrangement}); err == nil {
		t.Fatal("expected invalid work arrangement error")
	}
}

func exerciseLifecycleFailures(t *testing.T, jobs *service.JobService, stageID, questionID string) {
	t.Helper()
	if _, err := jobs.UpdateJob("missing", models.UpdateJobInput{}); err == nil {
		t.Fatal("expected missing job error")
	}
	if _, err := jobs.UpdateStage("missing", models.UpdateStageInput{Notes: ptr("missing")}); err == nil {
		t.Fatal("expected missing stage error")
	}
	if err := jobs.ScheduleMeeting("missing", models.ScheduleMeetingInput{}); err == nil {
		t.Fatal("expected missing schedule error")
	}
	if _, err := jobs.UpdateQuestion("missing", models.UpdateQuestionInput{Question: ptr("missing")}); err == nil {
		t.Fatal("expected missing question error")
	}
	if err := jobs.DeleteQuestion(questionID); err != nil {
		t.Fatal(err)
	}
	if err := jobs.DeleteStage(stageID); err != nil {
		t.Fatal(err)
	}
}

func exerciseLifecycleAttachment(t *testing.T, jobs *service.JobService, cfg *config.Config, jobID, stageID string) {
	t.Helper()
	attachment, err := jobs.StoreAttachment(jobID, &stageID, "resume.txt", strings.NewReader("resume"), 6, "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	if attachment.FileSize != 6 {
		t.Fatalf("attachment size=%d", attachment.FileSize)
	}
	if got, err := jobs.GetAttachmentByID(attachment.ID); err != nil || got == nil {
		t.Fatalf("attachment=%+v err=%v", got, err)
	}
	if err := jobs.DeleteAttachment(attachment.ID); err != nil {
		t.Fatal(err)
	}
	cfg.AttachmentsDir = filepath.Join(cfg.DataDir, "missing-directory")
	if _, err := jobs.StoreAttachment(jobID, nil, "bad.txt", strings.NewReader("x"), 1, "text/plain"); err == nil {
		t.Fatal("expected file creation failure")
	}
}

func requestJSON(mux *http.ServeMux, method, target, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestHandlersAllRoutes(t *testing.T) {
	mux, _, _, cfg := newCoverageApp(t)
	expectRouteStatus(t, mux, http.MethodGet, "/api/jobs/counts", "", http.StatusOK)
	expectRouteStatus(t, mux, http.MethodGet, "/api/meetings", "", http.StatusOK)
	expectRouteStatus(t, mux, http.MethodPost, "/api/jobs", "{}", http.StatusBadRequest)
	job := createHandlerJob(t, mux)
	checkJobRoutes(t, mux, job.ID)
	stageID := createHandlerStage(t, mux, job.ID)
	checkStageRoutes(t, mux, job.ID, stageID)
	checkQuestionRoutes(t, mux, stageID)
	checkAttachmentRoutes(t, mux, job.ID)
	checkLogoRoutes(t, mux, cfg)
	checkDeleteRoutes(t, mux, job.ID, stageID)
	expectRouteStatus(t, mux, http.MethodPost, "/api/jobs", strings.Repeat("x", 1<<20+1), http.StatusBadRequest)
}

func expectRouteStatus(t *testing.T, mux *http.ServeMux, method, path, body string, status int) *httptest.ResponseRecorder {
	t.Helper()
	recorder := requestJSON(mux, method, path, body)
	if recorder.Code != status {
		t.Fatalf("%s %s status=%d, want=%d body=%s", method, path, recorder.Code, status, recorder.Body.String())
	}
	return recorder
}

func createHandlerJob(t *testing.T, mux *http.ServeMux) models.Job {
	t.Helper()
	recorder := expectRouteStatus(t, mux, http.MethodPost, "/api/jobs", `{"company_name":"Acme","position_title":"Dev","create_default_stages":false}`, http.StatusCreated)
	var job models.Job
	if err := json.Unmarshal(recorder.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}
	return job
}

func checkJobRoutes(t *testing.T, mux *http.ServeMux, jobID string) {
	t.Helper()
	expectRouteStatus(t, mux, http.MethodGet, "/api/jobs?status=all&search=Acme", "", http.StatusOK)
	expectRouteStatus(t, mux, http.MethodGet, "/api/jobs/missing", "", http.StatusNotFound)
	expectRouteStatus(t, mux, http.MethodPut, "/api/jobs/"+jobID, `{"position_title":"Developer"}`, http.StatusOK)
	expectRouteStatus(t, mux, http.MethodPut, "/api/jobs/missing", `{}`, http.StatusNotFound)
	expectRouteStatus(t, mux, http.MethodPut, "/api/jobs/reorder", `{"job_ids":["`+jobID+`"]}`, http.StatusOK)
	expectRouteStatus(t, mux, http.MethodPut, "/api/jobs/reorder", `{}`, http.StatusBadRequest)
}

func createHandlerStage(t *testing.T, mux *http.ServeMux, jobID string) string {
	t.Helper()
	body := `{"job_id":"` + jobID + `","stage_type":"HR"}`
	recorder := expectRouteStatus(t, mux, http.MethodPost, "/api/stages", body, http.StatusCreated)
	var stage struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &stage); err != nil {
		t.Fatal(err)
	}
	return stage.ID
}

func checkStageRoutes(t *testing.T, mux *http.ServeMux, jobID, stageID string) {
	t.Helper()
	reorder := `{"job_id":"` + jobID + `","stage_ids":["` + stageID + `"]}`
	expectRouteStatus(t, mux, http.MethodPut, "/api/stages/reorder", reorder, http.StatusOK)
	expectRouteStatus(t, mux, http.MethodPut, "/api/stages/"+stageID+"/schedule", `{"meeting_date":"2032-01-01","meeting_time":"12:00"}`, http.StatusOK)
	expectRouteStatus(t, mux, http.MethodPut, "/api/stages/"+stageID+"/set-current", "", http.StatusOK)
	expectRouteStatus(t, mux, http.MethodPut, "/api/stages/"+stageID, `{"notes":"updated"}`, http.StatusOK)
	expectRouteStatus(t, mux, http.MethodPut, "/api/stages/missing", `{"notes":"missing"}`, http.StatusNotFound)
	expectRouteStatus(t, mux, http.MethodPut, "/api/stages/missing/schedule", `{}`, http.StatusNotFound)
}

func checkQuestionRoutes(t *testing.T, mux *http.ServeMux, stageID string) {
	t.Helper()
	body := `{"stage_id":"` + stageID + `","question":"Why?"}`
	recorder := expectRouteStatus(t, mux, http.MethodPost, "/api/questions", body, http.StatusCreated)
	var question struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &question); err != nil {
		t.Fatal(err)
	}
	expectRouteStatus(t, mux, http.MethodPut, "/api/questions/reorder", `{"stage_id":"`+stageID+`","question_ids":["`+question.ID+`"]}`, http.StatusOK)
	expectRouteStatus(t, mux, http.MethodPut, "/api/questions/"+question.ID, `{"answer_notes":"answer","is_asked":true}`, http.StatusOK)
	expectRouteStatus(t, mux, http.MethodDelete, "/api/questions/"+question.ID, "", http.StatusOK)
	expectRouteStatus(t, mux, http.MethodDelete, "/api/questions/missing", "", http.StatusNotFound)
}

func checkAttachmentRoutes(t *testing.T, mux *http.ServeMux, jobID string) {
	t.Helper()
	var form bytes.Buffer
	writer := multipart.NewWriter(&form)
	_ = writer.WriteField("job_id", jobID)
	part, err := writer.CreateFormFile("file", "resume.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(part, strings.NewReader("resume"))
	_ = writer.Close()
	request := httptest.NewRequest(http.MethodPost, "/api/attachments", &form)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("upload status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var attachment models.Attachment
	if err := json.Unmarshal(recorder.Body.Bytes(), &attachment); err != nil {
		t.Fatal(err)
	}
	download := expectRouteStatus(t, mux, http.MethodGet, "/api/attachments/"+attachment.ID+"/download", "", http.StatusOK)
	if download.Body.String() != "resume" {
		t.Fatalf("download body=%q", download.Body.String())
	}
	expectRouteStatus(t, mux, http.MethodDelete, "/api/attachments/"+attachment.ID, "", http.StatusOK)
	expectRouteStatus(t, mux, http.MethodGet, "/api/attachments/missing/download", "", http.StatusNotFound)
	expectRouteStatus(t, mux, http.MethodPost, "/api/attachments", "bad", http.StatusBadRequest)
}

func checkLogoRoutes(t *testing.T, mux *http.ServeMux, cfg *config.Config) {
	t.Helper()
	logoPath := filepath.Join(cfg.LogosDir, "acme.test.png")
	if err := os.WriteFile(logoPath, []byte("png-data"), 0600); err != nil {
		t.Fatal(err)
	}
	expectRouteStatus(t, mux, http.MethodGet, "/api/company-logo?company=Unknown", "", http.StatusNotFound)
	response := expectRouteStatus(t, mux, http.MethodGet, "/api/company-logo?company=Acme&domain=acme.test", "", http.StatusOK)
	if response.Body.String() != "png-data" {
		t.Fatalf("logo body=%q", response.Body.String())
	}
}

func checkDeleteRoutes(t *testing.T, mux *http.ServeMux, jobID, stageID string) {
	t.Helper()
	expectRouteStatus(t, mux, http.MethodDelete, "/api/stages/"+stageID, "", http.StatusOK)
	expectRouteStatus(t, mux, http.MethodDelete, "/api/stages/missing", "", http.StatusNotFound)
	expectRouteStatus(t, mux, http.MethodDelete, "/api/jobs/"+jobID, "", http.StatusOK)
	expectRouteStatus(t, mux, http.MethodDelete, "/api/jobs/missing", "", http.StatusNotFound)
}

func TestStaticAssetFallback(t *testing.T) {
	_, jobs, _, cfg, db := newCoverageDB(t)
	t.Cleanup(func() { closeTestResource(t, db) })
	staticDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("frontend"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg.StaticDir = staticDir
	mux := http.NewServeMux()
	logos := service.NewLogoService(cfg)
	handlers.NewHandler(jobs, logos, cfg).RegisterRoutes(mux)
	rootReq := httptest.NewRequest(http.MethodGet, "/", nil)
	rootRec := httptest.NewRecorder()
	mux.ServeHTTP(rootRec, rootReq)
	if rootRec.Code != http.StatusOK || rootRec.Body.String() != "frontend" {
		t.Fatalf("static root response=%d body=%q", rootRec.Code, rootRec.Body.String())
	}
	apiReq := httptest.NewRequest(http.MethodGet, "/api/not-registered", nil)
	apiRec := httptest.NewRecorder()
	mux.ServeHTTP(apiRec, apiReq)
	if apiRec.Code != http.StatusNotFound {
		t.Fatalf("unknown API response=%d", apiRec.Code)
	}
}

func TestDatabaseRestoreFromBackup(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{DBPath: filepath.Join(dir, "restore.db"), BackupPath: filepath.Join(dir, "backup.json")}
	backup := []models.Job{{ID: "restore-job", CompanyName: "Restore Co", PositionTitle: "Restored", Status: models.StatusOngoing, SalaryType: models.SalaryUnknown,
		SalaryCurrency: "EUR", RecruiterType: models.RecruiterNone, AvatarSeed: "restore", Stages: []models.Stage{{ID: "restore-stage", JobID: "restore-job", OrderIndex: 0,
			StageType: models.StageHR, Status: models.StageStatusCurrent, MeetingType: "video", RecruiterType: "none", Questions: []models.Question{{ID: "restore-question", StageID: "restore-stage", Question: "Q?"}}}}}}
	data, err := json.Marshal(backup)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.BackupPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	db, err := database.InitDB(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeTestResource(t, db) })
	repo := repository.New(db)
	job, err := repo.GetJobByID("restore-job")
	if err != nil || job == nil {
		t.Fatalf("restored job=%#v err=%v", job, err)
	}
	stages, err := repo.GetStagesByJobID("restore-job")
	if err != nil || len(stages) != 1 {
		t.Fatalf("restored stages=%+v err=%v", stages, err)
	}
	questions, err := repo.GetQuestionsByStageID("restore-stage")
	if err != nil || len(questions) != 1 {
		t.Fatalf("restored questions=%+v err=%v", questions, err)
	}
}

func TestJSONHelpersAndInputLimit(t *testing.T) {
	rec := httptest.NewRecorder()
	handlers.JSON(rec, http.StatusAccepted, map[string]string{"ok": "yes"})
	if rec.Code != http.StatusAccepted || rec.Header().Get("Content-Type") != "application/json; charset=utf-8" || !json.Valid(rec.Body.Bytes()) {
		t.Fatalf("JSON response code=%d headers=%v body=%q", rec.Code, rec.Header(), rec.Body.String())
	}
	app, _, _, _ := newCoverageApp(t)
	tooLarge := fmt.Sprintf(`{"company_name":"%s","position_title":"Dev"}`, strings.Repeat("a", 1<<20))
	if got := requestJSON(app, http.MethodPost, "/api/jobs", tooLarge); got.Code != http.StatusBadRequest {
		t.Fatalf("oversized request status=%d", got.Code)
	}
}

func TestServiceDefaultsAndFailureBranches(t *testing.T) {
	_, jobs, _, cfg := newCoverageApp(t)
	defaultJob := assertDefaultJobAndCreateSecond(t, jobs)
	exerciseSalaryTypeUpdates(t, jobs)
	if err := jobs.DeleteJob(defaultJob.ID); err != nil {
		t.Fatal(err)
	}
	exerciseBackupRenameFailure(t, jobs, cfg)
}

func assertDefaultJobAndCreateSecond(t *testing.T, jobs *service.JobService) *models.Job {
	t.Helper()
	max := int64(120)
	defaultJob, err := jobs.CreateJob(models.CreateJobInput{PositionTitle: "Default role", SalaryMax: &max})
	if err != nil {
		t.Fatal(err)
	}
	if defaultJob.CompanyName != "Unknown" || defaultJob.SalaryType != models.SalaryNoMin || len(defaultJob.Stages) != 4 || defaultJob.Stages[0].Status != models.StageStatusCurrent {
		t.Fatalf("default job=%+v", defaultJob)
	}
	if defaultJob.Stages[0].RecruiterType != models.RecruiterNone {
		t.Fatalf("default first stage recruiter type=%q", defaultJob.Stages[0].RecruiterType)
	}

	return defaultJob
}

func exerciseSalaryTypeUpdates(t *testing.T, jobs *service.JobService) {
	t.Helper()
	limited := models.SalaryLimited
	second, err := jobs.CreateJob(models.CreateJobInput{CompanyName: "Second", PositionTitle: "Role", SalaryType: &limited})
	if err != nil {
		t.Fatal(err)
	}
	if second.SalaryType != models.SalaryLimited {
		t.Fatalf("explicit salary type=%q", second.SalaryType)
	}
	maxOnly := int64(80)
	updated, err := jobs.UpdateJob(second.ID, models.UpdateJobInput{SalaryMax: &maxOnly})
	if err != nil || updated.SalaryType != models.SalaryNoMin || updated.SalaryMin != nil || updated.SalaryMax == nil {
		t.Fatalf("max-only update=%+v err=%v", updated, err)
	}
	noRange := models.SalaryUnknown
	updated, err = jobs.UpdateJob(second.ID, models.UpdateJobInput{SalaryType: &noRange})
	if err != nil || updated.SalaryMin != nil || updated.SalaryMax != nil {
		t.Fatalf("unknown salary update=%+v err=%v", updated, err)
	}
}

func exerciseBackupRenameFailure(t *testing.T, jobs *service.JobService, cfg *config.Config) {
	t.Helper()
	if err := os.Mkdir(cfg.BackupPath+".directory", 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(cfg.BackupPath); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	cfg.BackupPath = cfg.BackupPath + ".directory"
	if err := jobs.MirrorDatabaseToJSON(); err == nil {
		t.Fatal("expected backup rename failure")
	}
}

func TestHandlerValidationAndNotFoundBranches(t *testing.T) {
	mux, _, _, _ := newCoverageApp(t)
	checkInvalidPayloadRoutes(t, mux)
	checkValidReadRoutes(t, mux)
	checkInvalidLogoAndUpload(t, mux)
	checkMissingMultipartFields(t, mux)
}

func checkInvalidPayloadRoutes(t *testing.T, mux *http.ServeMux) {
	t.Helper()
	badPayloads := []struct{ method, path, body string }{
		{http.MethodPost, "/api/stages", "{"},
		{http.MethodPut, "/api/stages/reorder", `{}`},
		{http.MethodPut, "/api/stages/a/schedule", "{"},
		{http.MethodPut, "/api/questions/reorder", `{}`},
		{http.MethodPost, "/api/questions", `{"stage_id":"x","question":"  "}`},
		{http.MethodPut, "/api/questions/a", "{"},
		{http.MethodPut, "/api/jobs/a", "{"},
		{http.MethodPut, "/api/jobs/reorder", "{"},
	}
	for _, tc := range badPayloads {
		expectRouteStatus(t, mux, tc.method, tc.path, tc.body, http.StatusBadRequest)
	}
}

func checkValidReadRoutes(t *testing.T, mux *http.ServeMux) {
	t.Helper()
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/jobs/counts"},
		{http.MethodGet, "/api/meetings"},
		{http.MethodGet, "/api/jobs"},
	} {
		expectRouteStatus(t, mux, tc.method, tc.path, "", http.StatusOK)
	}
}

func checkInvalidLogoAndUpload(t *testing.T, mux *http.ServeMux) {
	t.Helper()
	expectRouteStatus(t, mux, http.MethodGet, "/api/company-logo?company=not-a-domain", "", http.StatusNotFound)
	expectRouteStatus(t, mux, http.MethodPost, "/api/attachments", "", http.StatusBadRequest)
}

func checkMissingMultipartFields(t *testing.T, mux *http.ServeMux) {
	t.Helper()
	for _, field := range []string{"job_id", "file"} {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		if field != "job_id" {
			_ = writer.WriteField("job_id", "some-job")
		}
		if field != "file" {
			part, err := writer.CreateFormFile("file", "empty.txt")
			if err != nil {
				t.Fatal(err)
			}
			_, _ = io.Copy(part, strings.NewReader("x"))
		}
		_ = writer.Close()
		req := httptest.NewRequest(http.MethodPost, "/api/attachments", &body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		expectResponseStatus(t, rec, http.StatusBadRequest)
	}
}

func expectResponseStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status=%d, want=%d body=%s", rec.Code, want, rec.Body.String())
	}
}

func TestRepositoryEmptyAndConstraintPaths(t *testing.T) {
	_, _, repo, _ := newCoverageApp(t)
	assertRepositoryReturnsEmptyRows(t, repo)
	assertRepositoryConstraints(t, repo)
}

func assertRepositoryReturnsEmptyRows(t *testing.T, repo *repository.Repository) {
	t.Helper()
	assertEmptyJobRows(t, repo)
	assertEmptyRelatedRows(t, repo)
}

func assertEmptyJobRows(t *testing.T, repo *repository.Repository) {
	t.Helper()
	if jobs, err := repo.GetAllJobs("", ""); err != nil || len(jobs) != 0 {
		t.Fatalf("empty jobs=%v err=%v", jobs, err)
	}
	if counts, err := repo.GetJobCounts(); err != nil || counts.All != 0 {
		t.Fatalf("empty counts=%+v err=%v", counts, err)
	}
}

func assertEmptyRelatedRows(t *testing.T, repo *repository.Repository) {
	t.Helper()
	if stages, err := repo.GetStagesByJobID("missing"); err != nil || len(stages) != 0 {
		t.Fatalf("empty stages=%v err=%v", stages, err)
	}
	if qs, err := repo.GetQuestionsByStageID("missing"); err != nil || len(qs) != 0 {
		t.Fatalf("empty questions=%v err=%v", qs, err)
	}
	if atts, err := repo.GetAttachmentsByJobID("missing"); err != nil || len(atts) != 0 {
		t.Fatalf("empty attachments=%v err=%v", atts, err)
	}
	if meetings, err := repo.GetScheduledMeetings(); err != nil || len(meetings) != 0 {
		t.Fatalf("empty meetings=%v err=%v", meetings, err)
	}
}

func assertRepositoryConstraints(t *testing.T, repo *repository.Repository) {
	t.Helper()
	invalid := &models.Job{ID: "invalid", CompanyName: "Invalid", PositionTitle: "Role", Status: "invalid", SalaryType: models.SalaryUnknown,
		SalaryCurrency: "EUR", RecruiterType: models.RecruiterNone, AvatarSeed: "invalid"}
	if err := repo.InsertJob(invalid); err == nil {
		t.Fatal("expected invalid status constraint")
	}
}

func TestRepositoryRejectsCorruptSerializedInterviewers(t *testing.T) {
	// The repository intentionally exposes malformed persisted JSON as an error.
	dir := t.TempDir()
	localCfg := &config.Config{DBPath: filepath.Join(dir, "corrupt.db")}
	db, err := database.InitDB(localCfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeTestResource(t, db) })
	localRepo := repository.New(db)
	if err := localRepo.InsertJob(&models.Job{ID: "bad-json", CompanyName: "Bad", PositionTitle: "Role", Status: models.StatusOngoing,
		SalaryType: models.SalaryUnknown, SalaryCurrency: "EUR", RecruiterType: models.RecruiterNone, AvatarSeed: "bad"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE jobs SET interviewers_json = 'not-json' WHERE id = 'bad-json'`); err != nil {
		t.Fatal(err)
	}
	if _, err := localRepo.GetJobByID("bad-json"); err == nil {
		t.Fatal("expected corrupt job JSON error")
	}
	if _, err := localRepo.GetAllJobs("", ""); err == nil {
		t.Fatal("expected corrupt job list JSON error")
	}
	stage := &models.Stage{ID: "bad-stage", JobID: "bad-json", StageType: models.StageHR, Status: models.StageStatusCurrent, MeetingType: "video", RecruiterType: "none"}
	if err := localRepo.InsertStage(stage); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE stages SET interviewers_json = 'not-json' WHERE id = 'bad-stage'`); err != nil {
		t.Fatal(err)
	}
	if _, err := localRepo.GetStageByID(stage.ID); err == nil {
		t.Fatal("expected corrupt stage JSON error")
	}
	if _, err := localRepo.GetStagesByJobID(stage.JobID); err == nil {
		t.Fatal("expected corrupt stages list JSON error")
	}
}

func TestServicesPropagateDatabaseFailures(t *testing.T) {
	fixture := newDatabaseFailureFixture(t)
	exerciseAttachmentDatabaseFailures(t, fixture)
	exerciseQuestionDatabaseFailures(t, fixture)
	exerciseStageDatabaseFailures(t, fixture)
	exerciseJobDatabaseFailures(t, fixture)
}

type databaseFailureFixture struct {
	mux        *http.ServeMux
	jobs       *service.JobService
	db         *sql.DB
	job        *models.Job
	stage      *models.Stage
	question   *models.Question
	attachment *models.Attachment
}

func newDatabaseFailureFixture(t *testing.T) *databaseFailureFixture {
	t.Helper()
	mux, jobs, _, _, db := newCoverageDB(t)
	job, err := jobs.CreateJob(models.CreateJobInput{CompanyName: "Failures", PositionTitle: "Role", CreateDefaultStages: ptr(false)})
	if err != nil {
		t.Fatal(err)
	}
	stage, err := jobs.CreateStage(models.CreateStageInput{JobID: job.ID, StageType: models.StageHR})
	if err != nil {
		t.Fatal(err)
	}
	question, err := jobs.CreateQuestion(models.CreateQuestionInput{StageID: stage.ID, Question: "Question"})
	if err != nil {
		t.Fatal(err)
	}
	attachment, err := jobs.StoreAttachment(job.ID, nil, "existing.txt", strings.NewReader("file"), 4, "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	return &databaseFailureFixture{mux: mux, jobs: jobs, db: db, job: job, stage: stage, question: question, attachment: attachment}
}

func dropFailureTable(t *testing.T, db *sql.DB, name string) {
	t.Helper()
	if _, err := db.Exec("DROP TABLE " + name); err != nil {
		t.Fatalf("drop %s: %v", name, err)
	}
}

func exerciseAttachmentDatabaseFailures(t *testing.T, fixture *databaseFailureFixture) {
	t.Helper()
	dropFailureTable(t, fixture.db, "attachments")
	if _, err := fixture.jobs.GetFullJobDetails(fixture.job.ID); err == nil {
		t.Fatal("expected attachment query failure")
	}
	if err := fixture.jobs.DeleteAttachment(fixture.attachment.ID); err == nil {
		t.Fatal("expected attachment lookup failure")
	}
	if _, err := fixture.jobs.StoreAttachment(fixture.job.ID, nil, "failed.txt", strings.NewReader("file"), 4, "text/plain"); err == nil {
		t.Fatal("expected attachment insert failure")
	}
	assertAttachmentErrorRoutes(t, fixture)
	assertAttachmentUploadFailure(t, fixture)
}

func assertAttachmentErrorRoutes(t *testing.T, fixture *databaseFailureFixture) {
	t.Helper()
	expectRouteStatus(t, fixture.mux, http.MethodGet, "/api/attachments/"+fixture.attachment.ID+"/download", "", http.StatusNotFound)
	expectRouteStatus(t, fixture.mux, http.MethodDelete, "/api/attachments/"+fixture.attachment.ID, "", http.StatusNotFound)
}

func assertAttachmentUploadFailure(t *testing.T, fixture *databaseFailureFixture) {
	t.Helper()
	var upload bytes.Buffer
	writer := multipart.NewWriter(&upload)
	_ = writer.WriteField("job_id", fixture.job.ID)
	part, err := writer.CreateFormFile("file", "new.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(part, "new")
	_ = writer.Close()
	uploadReq := httptest.NewRequest(http.MethodPost, "/api/attachments", &upload)
	uploadReq.Header.Set("Content-Type", writer.FormDataContentType())
	uploadRec := httptest.NewRecorder()
	fixture.mux.ServeHTTP(uploadRec, uploadReq)
	if uploadRec.Code != http.StatusInternalServerError {
		t.Fatalf("attachment insert error response=%d", uploadRec.Code)
	}

}

func exerciseQuestionDatabaseFailures(t *testing.T, fixture *databaseFailureFixture) {
	t.Helper()
	dropFailureTable(t, fixture.db, "stage_questions")
	if _, err := fixture.jobs.GetFullJobDetails(fixture.job.ID); err == nil {
		t.Fatal("expected question query failure")
	}
	if _, err := fixture.jobs.CreateQuestion(models.CreateQuestionInput{StageID: fixture.stage.ID, Question: "New"}); err == nil {
		t.Fatal("expected question insert failure")
	}
	if _, err := fixture.jobs.UpdateQuestion(fixture.question.ID, models.UpdateQuestionInput{Question: ptr("changed")}); err == nil {
		t.Fatal("expected question update failure")
	}
	if err := fixture.jobs.DeleteQuestion(fixture.question.ID); err == nil {
		t.Fatal("expected question delete failure")
	}
	if err := fixture.jobs.ReorderQuestions(fixture.stage.ID, []string{fixture.question.ID}); err == nil {
		t.Fatal("expected question reorder failure")
	}
	assertQuestionErrorRoutes(t, fixture)
}

func assertQuestionErrorRoutes(t *testing.T, fixture *databaseFailureFixture) {
	t.Helper()
	expectRouteStatus(t, fixture.mux, http.MethodPost, "/api/questions", `{"stage_id":"`+fixture.stage.ID+`","question":"New"}`, http.StatusBadRequest)
	expectRouteStatus(t, fixture.mux, http.MethodPut, "/api/questions/"+fixture.question.ID, `{"question":"changed"}`, http.StatusInternalServerError)
	expectRouteStatus(t, fixture.mux, http.MethodDelete, "/api/questions/"+fixture.question.ID, "", http.StatusInternalServerError)
	body := `{"stage_id":"` + fixture.stage.ID + `","question_ids":["` + fixture.question.ID + `"]}`
	expectRouteStatus(t, fixture.mux, http.MethodPut, "/api/questions/reorder", body, http.StatusInternalServerError)
	expectRouteStatus(t, fixture.mux, http.MethodGet, "/api/jobs/"+fixture.job.ID, "", http.StatusInternalServerError)
}

func exerciseStageDatabaseFailures(t *testing.T, fixture *databaseFailureFixture) {
	t.Helper()
	dropFailureTable(t, fixture.db, "stages")
	if _, err := fixture.jobs.GetFullJobDetails(fixture.job.ID); err == nil {
		t.Fatal("expected stage query failure")
	}
	if _, err := fixture.jobs.CreateStage(models.CreateStageInput{JobID: fixture.job.ID, StageType: models.StageHR}); err == nil || !strings.Contains(err.Error(), "load existing stages for job") {
		t.Fatalf("CreateStage error = %v, want existing-stage lookup error", err)
	}
	if _, err := fixture.jobs.UpdateStage(fixture.stage.ID, models.UpdateStageInput{Notes: ptr("changed")}); err == nil {
		t.Fatal("expected stage update failure")
	}
	if err := fixture.jobs.ScheduleMeeting(fixture.stage.ID, models.ScheduleMeetingInput{MeetingDate: "2032-01-01"}); err == nil {
		t.Fatal("expected schedule failure")
	}
	if err := fixture.jobs.DeleteStage(fixture.stage.ID); err == nil {
		t.Fatal("expected stage delete failure")
	}
	if err := fixture.jobs.ReorderStages(fixture.job.ID, []string{fixture.stage.ID}); err == nil {
		t.Fatal("expected stage reorder failure")
	}
	if _, err := fixture.jobs.SetCurrentStage(fixture.stage.ID); err == nil {
		t.Fatal("expected set-current failure")
	}
	assertStageErrorRoutes(t, fixture)
}

func assertStageErrorRoutes(t *testing.T, fixture *databaseFailureFixture) {
	jobID, stageID := fixture.job.ID, fixture.stage.ID
	expectRouteStatus(t, fixture.mux, http.MethodPost, "/api/stages", `{"job_id":"`+jobID+`","stage_type":"HR"}`, http.StatusBadRequest)
	expectRouteStatus(t, fixture.mux, http.MethodPut, "/api/stages/reorder", `{"job_id":"`+jobID+`","stage_ids":["`+stageID+`"]}`, http.StatusInternalServerError)
	expectRouteStatus(t, fixture.mux, http.MethodPut, "/api/stages/"+stageID+"/set-current", "", http.StatusNotFound)
	expectRouteStatus(t, fixture.mux, http.MethodDelete, "/api/stages/"+stageID, "", http.StatusInternalServerError)
	expectRouteStatus(t, fixture.mux, http.MethodPut, "/api/stages/"+stageID, `{"notes":"changed"}`, http.StatusInternalServerError)
	expectRouteStatus(t, fixture.mux, http.MethodPut, "/api/stages/"+stageID+"/schedule", `{"meeting_date":"2032-01-01"}`, http.StatusInternalServerError)
}

func exerciseJobDatabaseFailures(t *testing.T, fixture *databaseFailureFixture) {
	t.Helper()
	dropFailureTable(t, fixture.db, "jobs")
	if _, err := fixture.jobs.GetFullJobDetails(fixture.job.ID); err == nil {
		t.Fatal("expected job query failure")
	}
	if _, err := fixture.jobs.UpdateJob(fixture.job.ID, models.UpdateJobInput{CompanyName: ptr("changed")}); err == nil {
		t.Fatal("expected update lookup failure")
	}
	if _, err := fixture.jobs.CreateJob(models.CreateJobInput{PositionTitle: "Another"}); err == nil {
		t.Fatal("expected job insert failure")
	}
	if err := fixture.jobs.DeleteJob(fixture.job.ID); err == nil {
		t.Fatal("expected job delete failure")
	}
	if err := fixture.jobs.ReorderJobs([]string{fixture.job.ID}); err == nil {
		t.Fatal("expected job reorder failure")
	}
	assertJobErrorRoutes(t, fixture)
}

func assertJobErrorRoutes(t *testing.T, fixture *databaseFailureFixture) {
	jobID := fixture.job.ID
	expectRouteStatus(t, fixture.mux, http.MethodPut, "/api/jobs/"+jobID, `{"company_name":"changed"}`, http.StatusInternalServerError)
	expectRouteStatus(t, fixture.mux, http.MethodDelete, "/api/jobs/"+jobID, "", http.StatusInternalServerError)
	expectRouteStatus(t, fixture.mux, http.MethodPut, "/api/jobs/reorder", `{"job_ids":["`+jobID+`"]}`, http.StatusInternalServerError)
	expectRouteStatus(t, fixture.mux, http.MethodGet, "/api/jobs/counts", "", http.StatusInternalServerError)
	expectRouteStatus(t, fixture.mux, http.MethodGet, "/api/jobs", "", http.StatusInternalServerError)
}

func TestLogoCacheVariantsAndUnknownDomains(t *testing.T) {
	dir := t.TempDir()
	svc := service.NewLogoService(&config.Config{LogosDir: dir})
	for _, tc := range []struct{ ext, mime string }{
		{".png", "image/png"}, {".svg", "image/svg+xml"}, {".jpg", "image/jpeg"}, {".webp", "image/webp"},
	} {
		want := []byte("logo" + tc.ext)
		if err := os.WriteFile(filepath.Join(dir, "acme.test"+tc.ext), want, 0600); err != nil {
			t.Fatal(err)
		}
		got, mime, err := svc.GetLogo("Acme", "acme.test")
		if err != nil || mime != tc.mime || !bytes.Equal(got, want) {
			t.Fatalf("cache %s: mime=%q data=%q err=%v", tc.ext, mime, got, err)
		}
		if err := os.Remove(filepath.Join(dir, "acme.test"+tc.ext)); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := svc.GetLogo("X", ""); err == nil {
		t.Fatal("expected short company name to have no deduced domain")
	}
}

func TestLogoRemoteFetchBranches(t *testing.T) {
	t.Run("disabled by default", testLogoRemoteLookupDisabled)
	t.Run("success and cache", testLogoRemoteFetchSuccessAndCache)
	t.Run("non-200", testLogoRemoteFetchNon200)
	t.Run("transport failure", testLogoRemoteFetchTransportFailure)
	t.Run("empty body", testLogoRemoteFetchEmptyBody)
	t.Run("read failure", testLogoRemoteFetchReadFailure)
	t.Run("cache write failure", testLogoRemoteFetchCacheWriteFailure)
}

func testLogoRemoteLookupDisabled(t *testing.T) {
	requests := 0
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests++
		return nil, errors.New("unexpected remote request")
	})}
	svc := service.NewLogoServiceWithClient(&config.Config{LogosDir: t.TempDir()}, client, func(string) string {
		return "https://logos.invalid"
	})
	if _, _, err := svc.GetLogo("Acme", "acme.test"); err == nil || err.Error() != "remote logo lookup is disabled" {
		t.Fatalf("disabled lookup error=%v", err)
	}
	if requests != 0 {
		t.Fatalf("remote requests=%d, want none", requests)
	}
}

const testPNGSignature = "\x89PNG\r\n\x1a\n"

func testLogoRemoteFetchSuccessAndCache(t *testing.T) {
	for _, testCase := range []struct {
		name, signature, mimeType, extension string
	}{
		{name: "png", signature: testPNGSignature, mimeType: "image/png", extension: ".png"},
		{name: "jpeg", signature: "\xff\xd8\xff", mimeType: "image/jpeg", extension: ".jpg"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			dir := t.TempDir()
			requests := 0
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				requests++
				if r.URL.Path != "/favicon" || r.URL.Query().Get("domain") != "acme.test" {
					t.Errorf("unexpected fetch URL: %s", r.URL)
				}
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(testCase.signature)), Request: r}, nil
			})}
			svc := service.NewLogoServiceWithClient(&config.Config{LogosDir: dir, LogoLookupEnabled: true}, client, func(domain string) string {
				return "https://logos.invalid/favicon?domain=" + url.QueryEscape(domain)
			})
			for i := 0; i < 2; i++ {
				data, mime, err := svc.GetLogo("Acme", "acme.test")
				if err != nil || mime != testCase.mimeType || string(data) != testCase.signature {
					t.Fatalf("fetch=%q mime=%q err=%v", data, mime, err)
				}
			}
			if requests != 1 {
				t.Fatalf("remote requests=%d, want cached second read", requests)
			}
			if _, err := os.Stat(filepath.Join(dir, "acme.test"+testCase.extension)); err != nil {
				t.Fatalf("logo cache extension %q not written: %v", testCase.extension, err)
			}
		})
	}
}

func testLogoRemoteFetchNon200(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("unavailable")), Request: r}, nil
	})}
	svc := service.NewLogoServiceWithClient(&config.Config{LogosDir: t.TempDir(), LogoLookupEnabled: true}, client, func(string) string { return "https://logos.invalid" })
	if _, _, err := svc.GetLogo("Acme", "acme.test"); err == nil || !strings.Contains(err.Error(), "remote logo fetch failed") {
		t.Fatalf("non-200 error=%v", err)
	}
}

func testLogoRemoteFetchTransportFailure(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, fmt.Errorf("offline") })}
	svc := service.NewLogoServiceWithClient(&config.Config{LogosDir: t.TempDir(), LogoLookupEnabled: true}, client, func(string) string { return "https://logos.invalid" })
	if _, _, err := svc.GetLogo("Acme", "acme.test"); err == nil {
		t.Fatal("expected transport failure")
	}
}

func testLogoRemoteFetchEmptyBody(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})}
	svc := service.NewLogoServiceWithClient(&config.Config{LogosDir: t.TempDir(), LogoLookupEnabled: true}, client, func(string) string { return "https://logos.invalid" })
	if _, _, err := svc.GetLogo("Acme", "acme.test"); err == nil || err.Error() != "empty logo body" {
		t.Fatalf("empty body error=%v", err)
	}
}

func testLogoRemoteFetchReadFailure(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(failingReader{}), Request: r}, nil
	})}
	svc := service.NewLogoServiceWithClient(&config.Config{LogosDir: t.TempDir(), LogoLookupEnabled: true}, client, func(string) string { return "https://logos.invalid" })
	if _, _, err := svc.GetLogo("Acme", "acme.test"); err == nil || err.Error() != "empty logo body" {
		t.Fatalf("truncated body error=%v", err)
	}
}

func testLogoRemoteFetchCacheWriteFailure(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(testPNGSignature)), Request: r}, nil
	})}
	missingCacheDir := filepath.Join(t.TempDir(), "missing")
	svc := service.NewLogoServiceWithClient(&config.Config{LogosDir: missingCacheDir, LogoLookupEnabled: true}, client, func(string) string { return "https://logos.invalid" })
	if data, _, err := svc.GetLogo("Acme", "acme.test"); err != nil || string(data) != testPNGSignature {
		t.Fatalf("cache write error should be ignored: data=%q err=%v", data, err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, fmt.Errorf("read failed") }

func TestDefaultLogoEndpointUsesHTTPDefaultTransport(t *testing.T) {
	const pngSignature = "\x89PNG\r\n\x1a\n"
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	called := false
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		called = true
		if req.URL.Host != "www.google.com" || req.URL.Path != "/s2/favicons" || req.URL.Query().Get("domain") != "acme.com" || req.URL.Query().Get("sz") != "128" {
			return nil, fmt.Errorf("unexpected default endpoint URL: %s", req.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(pngSignature)), Header: make(http.Header), Request: req}, nil
	})
	svc := service.NewLogoService(&config.Config{LogosDir: t.TempDir(), LogoLookupEnabled: true})
	if data, mime, err := svc.GetLogo("Acme", ""); err != nil || string(data) != pngSignature || mime != "image/png" {
		t.Fatalf("default fetch=%q mime=%q err=%v", data, mime, err)
	}
	if !called {
		t.Fatal("default transport was not called")
	}
}

func TestDatabaseBackupIgnoredWhenInvalidOrDatabaseHasRows(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{DBPath: filepath.Join(dir, "backup.db"), BackupPath: filepath.Join(dir, "backup.json")}
	if err := os.WriteFile(cfg.BackupPath, []byte("not-json"), 0600); err != nil {
		t.Fatal(err)
	}
	db, err := database.InitDB(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.BackupPath, []byte("[]"), 0600); err != nil {
		t.Fatal(err)
	}
	db, err = database.InitDB(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeTestResource(t, db) })
	if _, err := db.Exec(`INSERT INTO jobs (id, company_name, position_title, status, salary_type, salary_currency, recruiter_type, avatar_seed) VALUES ('present', 'Present', 'Role', 'ongoing', 'unknown', 'EUR', 'none', 'present')`); err != nil {
		t.Fatal(err)
	}
	populatedDB, err := database.InitDB(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := populatedDB.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestDatabaseRestoreAppliesLegacyDefaults restores legacy records with omitted optional fields.
func TestDatabaseRestoreAppliesLegacyDefaults(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{DBPath: filepath.Join(dir, "defaults.db"), BackupPath: filepath.Join(dir, "backup.json")}
	jobs := []models.Job{{
		ID: "legacy", CompanyName: "Legacy", PositionTitle: "Role", Status: models.StatusOngoing, AvatarSeed: "legacy",
		Stages: []models.Stage{{ID: "legacy-stage", StageType: models.StageTechnical, Status: models.StageStatusPending,
			Questions: []models.Question{{ID: "legacy-question", Question: "First"}},
		}},
	}}
	data, err := json.Marshal(jobs)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.BackupPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	db, err := database.InitDB(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeTestResource(t, db) })
	repo := repository.New(db)
	assertRestoredLegacyJob(t, repo)
	assertRestoredLegacyStageData(t, repo)
}

func assertRestoredLegacyJob(t *testing.T, repo *repository.Repository) {
	t.Helper()
	job, err := repo.GetJobByID("legacy")
	if err != nil || job == nil || job.SalaryType != models.SalaryUnknown || job.SalaryCurrency != "EUR" || job.RecruiterType != models.RecruiterNone {
		t.Fatalf("legacy job=%+v err=%v", job, err)
	}
}

func assertRestoredLegacyStageData(t *testing.T, repo *repository.Repository) {
	t.Helper()
	stages, err := repo.GetStagesByJobID("legacy")
	if err != nil || len(stages) != 1 || stages[0].MeetingType != "video" || stages[0].RecruiterType != "none" {
		t.Fatalf("legacy stages=%+v err=%v", stages, err)
	}
	questions, err := repo.GetQuestionsByStageID("legacy-stage")
	if err != nil || len(questions) != 1 || questions[0].ID != "legacy-question" {
		t.Fatalf("restored questions=%+v err=%v", questions, err)
	}
}

func TestDatabaseInitializationFailures(t *testing.T) {
	t.Run("schema failure", func(t *testing.T) {
		cfg := &config.Config{DBPath: filepath.Join(t.TempDir(), "missing", "db.sqlite")}
		if db, err := database.InitDB(cfg); err == nil {
			if db != nil {
				_ = db.Close()
			}
			t.Fatal("expected schema initialization failure")
		}
	})
	t.Run("backup read failure", func(t *testing.T) {
		dir := t.TempDir()
		backupDir := filepath.Join(dir, "backup-dir")
		if err := os.Mkdir(backupDir, 0700); err != nil {
			t.Fatal(err)
		}
		cfg := &config.Config{DBPath: filepath.Join(dir, "db.sqlite"), BackupPath: backupDir}
		db, err := database.InitDB(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	})
}

func TestConfigFallbacksAndDirectoryErrors(t *testing.T) {
	workDir := t.TempDir()
	t.Chdir(workDir)
	legacyDir := t.TempDir()
	checkConfigEnvironmentFallback(t, legacyDir)
	checkConfigStaticFallbacks(t)
	checkConfigDefaultDataDirectory(t, workDir)
	checkConfigDirectoryCreationFailures(t)
}

func checkConfigEnvironmentFallback(t *testing.T, legacyDir string) {
	t.Helper()
	t.Setenv("PORT", "0")
	t.Setenv("HOST", "")
	t.Setenv("WARROOM_DATA_DIR", "")
	t.Setenv("ONGOING_DATA_DIR", legacyDir)
	t.Setenv("STATIC_DIR", "")
	t.Setenv("CORS_ALLOWED_ORIGINS", "")
	cfg := config.Load()
	if cfg.Port != 4040 || cfg.DataDir != legacyDir || cfg.CORSAllowed == "" || cfg.StaticDir == "" {
		t.Fatalf("fallback config=%+v", cfg)
	}
}

func checkConfigStaticFallbacks(t *testing.T) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join("frontend", "public"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ONGOING_DATA_DIR", t.TempDir())
	if got := config.Load().StaticDir; got != filepath.Join("frontend", "public") {
		t.Fatalf("frontend static dir=%q", got)
	}
	if err := os.RemoveAll("frontend"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir("public", 0700); err != nil {
		t.Fatal(err)
	}
	if got := config.Load().StaticDir; got != "public" {
		t.Fatalf("public static dir=%q", got)
	}
	if err := os.RemoveAll("public"); err != nil {
		t.Fatal(err)
	}
}

func checkConfigDefaultDataDirectory(t *testing.T, workDir string) {
	t.Helper()
	t.Setenv("ONGOING_DATA_DIR", "")
	defaultCfg := config.Load()
	if defaultCfg.DataDir != filepath.Join(workDir, "data") {
		t.Fatalf("default data dir=%q", defaultCfg.DataDir)
	}
}

func checkConfigDirectoryCreationFailures(t *testing.T) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(file, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	assertConfigLoadPanics(t, "data", file, "")
	attachRoot := t.TempDir()
	assertConfigLoadPanics(t, "attachments", attachRoot, filepath.Join(attachRoot, "attachments"))
	logoRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(logoRoot, "attachments"), 0700); err != nil {
		t.Fatal(err)
	}
	assertConfigLoadPanics(t, "logos", logoRoot, filepath.Join(logoRoot, "logos"))
}

func assertConfigLoadPanics(t *testing.T, label, dataDir, obstacle string) {
	t.Helper()
	if obstacle != "" {
		if err := os.MkdirAll(filepath.Dir(obstacle), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(obstacle, []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("WARROOM_DATA_DIR", dataDir)
	t.Setenv("ONGOING_DATA_DIR", "")
	defer func() {
		if recover() == nil {
			t.Errorf("%s: expected directory creation panic", label)
		}
	}()
	_ = config.Load()
}
