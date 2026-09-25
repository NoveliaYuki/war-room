// Package service implements application rules above the persistence layer.
package service

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"war-room/backend/pkg/config"
	"war-room/backend/pkg/models"
	"war-room/backend/pkg/repository"
)

// JobService applies business rules to jobs, stages, questions, and attachments.
type JobService struct {
	repo       *repository.Repository
	cfg        *config.Config
	mirrorLock sync.Mutex
}

// NewJobService creates a job service backed by repo and cfg.
func NewJobService(repo *repository.Repository, cfg *config.Config) *JobService {
	return &JobService{
		repo: repo,
		cfg:  cfg,
	}
}

func (s *JobService) mirrorAfterMutation() {
	if err := s.MirrorDatabaseToJSON(); err != nil {
		log.Printf("Warning: failed to update backup snapshot: %v", err)
	}
}

// GetAllJobs returns jobs matching the optional status and search filters.
func (s *JobService) GetAllJobs(status, search string) ([]models.Job, error) {
	return s.repo.GetAllJobs(status, search)
}

// GetJobCounts returns the number of jobs in each status.
func (s *JobService) GetJobCounts() (*models.JobCounts, error) {
	return s.repo.GetJobCounts()
}

// GetFullJobDetails returns a job with its stages, questions, and attachments.
func (s *JobService) GetFullJobDetails(id string) (*models.Job, error) {
	return getFullJobDetails(s.repo, id)
}

func getFullJobDetails(repo *repository.Repository, id string) (*models.Job, error) {
	job, err := repo.GetJobByID(id)
	if err != nil {
		return nil, err
	}
	if job == nil {
		return nil, nil
	}

	stages, err := repo.GetStagesByJobID(job.ID)
	if err != nil {
		return nil, err
	}

	for i := range stages {
		questions, err := repo.GetQuestionsByStageID(stages[i].ID)
		if err != nil {
			return nil, err
		}
		stages[i].Questions = questions
	}
	job.Stages = stages

	attachments, err := repo.GetAttachmentsByJobID(job.ID)
	if err != nil {
		return nil, err
	}
	job.Attachments = attachments

	return job, nil
}

// CreateJob validates and persists a job and its default stages.
func (s *JobService) CreateJob(input models.CreateJobInput) (*models.Job, error) {
	job, err := buildJob(input)
	if err != nil {
		return nil, err
	}
	job.ID = uuid.NewString()
	if err := s.repo.InsertJob(job); err != nil {
		return nil, err
	}
	if input.CreateDefaultStages == nil || *input.CreateDefaultStages {
		if err := s.createDefaultStages(job, input); err != nil {
			_ = s.repo.DeleteJob(job.ID)
			return nil, err
		}
	}
	s.mirrorAfterMutation()
	return s.GetFullJobDetails(job.ID)
}

func buildJob(input models.CreateJobInput) (*models.Job, error) {
	workArrangement, employmentType, err := normalizeJobWorkDetails(input)
	if err != nil {
		return nil, err
	}
	position := strings.TrimSpace(input.PositionTitle)
	if position == "" {
		return nil, errors.New("position_title is required")
	}
	company := strings.TrimSpace(input.CompanyName)
	if company == "" {
		company = "Unknown"
	}
	keyword := normalizeKeyword(input.KeywordNote)
	avatar := jobAvatar(company, position, input.AvatarSeed)
	salaryType, minSalary, maxSalary := normalizeSalary(input)
	status, recruiterType, currency := models.StatusOngoing, models.RecruiterNone, "EUR"
	if input.Status != nil {
		status = *input.Status
	}
	if input.RecruiterType != nil {
		recruiterType = *input.RecruiterType
	}
	if input.SalaryCurrency != nil && *input.SalaryCurrency != "" {
		currency = *input.SalaryCurrency
	}
	referral := input.IsReferral != nil && *input.IsReferral
	return &models.Job{
		CompanyName: company, PositionTitle: position, Status: status,
		SalaryType: salaryType, SalaryMin: minSalary, SalaryMax: maxSalary,
		SalaryCurrency: currency, RecruiterType: recruiterType,
		RecruiterName: input.RecruiterName, RecruiterAgency: input.RecruiterAgency,
		RecruiterContact: input.RecruiterContact, Interviewers: input.Interviewers,
		JobPostURL: input.JobPostURL, AvatarSeed: avatar, KeywordNote: keyword,
		Description: valueOrEmpty(input.Description), CompanyOverview: valueOrEmpty(input.CompanyOverview),
		CompanyDomain: input.CompanyDomain, InterviewNotes: valueOrEmpty(input.InterviewNotes),
		ReasonsToChange: valueOrEmpty(input.ReasonsToChange),
		ExperienceNotes: strings.TrimSpace(valueOrEmpty(input.ExperienceNotes)),
		ExpectedSalary:  strings.TrimSpace(valueOrEmpty(input.ExpectedSalary)),
		WorkArrangement: workArrangement,
		EmploymentType:  employmentType,
		IsReferral:      referral,
	}, nil
}

func normalizeJobWorkDetails(input models.CreateJobInput) (models.WorkArrangement, models.EmploymentType, error) {
	workArrangement := models.WorkArrangementUnknown
	if input.WorkArrangement != nil {
		if err := validateWorkArrangement(*input.WorkArrangement); err != nil {
			return "", "", err
		}
		workArrangement = *input.WorkArrangement
	}
	employmentType := models.EmploymentTypeUnknown
	if input.EmploymentType != nil {
		if err := validateEmploymentType(*input.EmploymentType); err != nil {
			return "", "", err
		}
		employmentType = *input.EmploymentType
	}
	return workArrangement, employmentType, nil
}

func normalizeKeyword(value *string) string {
	keyword := strings.TrimSpace(valueOrEmpty(value))
	if len(keyword) > 100 {
		return keyword[:100]
	}
	return keyword
}

func jobAvatar(company, position string, seed *string) string {
	avatar := strings.TrimSpace(valueOrEmpty(seed))
	if avatar != "" {
		return avatar
	}
	return fmt.Sprintf("%s-%s-%d", company, position, time.Now().UnixMilli())
}

func normalizeSalary(input models.CreateJobInput) (models.SalaryType, *int64, *int64) {
	minSalary := cloneInt64(input.SalaryMin)
	maxSalary := cloneInt64(input.SalaryMax)
	salaryType := models.SalaryUnknown
	switch {
	case minSalary != nil && maxSalary != nil:
		salaryType = models.SalaryLimited
		if *minSalary > *maxSalary {
			*minSalary, *maxSalary = *maxSalary, *minSalary
		}
	case minSalary != nil:
		salaryType = models.SalaryNoMax
	case maxSalary != nil:
		salaryType = models.SalaryNoMin
	case input.SalaryType != nil:
		salaryType = *input.SalaryType
	}
	return salaryType, minSalary, maxSalary
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func (s *JobService) createDefaultStages(job *models.Job, input models.CreateJobInput) error {
	defaultStages := []struct {
		stageType   models.StageType
		title       string
		description string
	}{
		{models.StageHR, "Recruiter Screen", "Initial culture fit, role overview, and expectations alignment."},
		{models.StageTechnical, "Technical Evaluation", "System architecture, coding, or take-home review."},
		{models.StageCultural, "Team & Leadership Alignment", "Discussion with cross-functional peers and leadership."},
		{models.StageOfferDecision, "Offer & Decision", "Compensation proposal, equity review, and final agreement."},
	}
	for index, definition := range defaultStages {
		status := models.StageStatusPending
		if index == 0 {
			status = models.StageStatusCurrent
		}
		stage := &models.Stage{
			ID: uuid.NewString(), JobID: job.ID, OrderIndex: index,
			StageType: definition.stageType, CustomTitle: &definition.title,
			Description: definition.description, Status: status, MeetingType: "video",
			RecruiterType: models.RecruiterNone,
		}
		if index == 0 {
			stage.RecruiterName, stage.RecruiterAgency = input.RecruiterName, input.RecruiterAgency
			stage.RecruiterType, stage.RecruiterContact = job.RecruiterType, input.RecruiterContact
		}
		if err := s.repo.InsertStage(stage); err != nil {
			return fmt.Errorf("create default stage %q: %w", definition.title, err)
		}
	}
	return nil
}

// UpdateJob applies the supplied fields to an existing job.
func (s *JobService) UpdateJob(id string, input models.UpdateJobInput) (*models.Job, error) {
	if input.WorkArrangement != nil {
		if err := validateWorkArrangement(*input.WorkArrangement); err != nil {
			return nil, err
		}
	}
	if input.EmploymentType != nil {
		if err := validateEmploymentType(*input.EmploymentType); err != nil {
			return nil, err
		}
	}
	current, err := s.repo.GetJobByID(id)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, sql.ErrNoRows
	}

	fields := jobUpdateFields(current, input)

	if err := s.repo.UpdateJob(id, fields); err != nil {
		return nil, err
	}

	s.mirrorAfterMutation()
	return s.GetFullJobDetails(id)
}

func jobUpdateFields(current *models.Job, input models.UpdateJobInput) map[string]interface{} {
	fields := make(map[string]interface{})
	addJobIdentityFields(fields, input)
	addJobSalaryFields(fields, current, input)
	addJobRecruiterFields(fields, input)
	addJobNotesFields(fields, input)
	return fields
}

func addJobIdentityFields(fields map[string]interface{}, input models.UpdateJobInput) {
	if input.CompanyName != nil {
		company := strings.TrimSpace(*input.CompanyName)
		if company == "" {
			company = "Unknown"
		}
		fields["company_name"] = company
	}
	if input.PositionTitle != nil {
		fields["position_title"] = strings.TrimSpace(*input.PositionTitle)
	}
	if input.Status != nil {
		fields["status"] = *input.Status
	}
	if input.SalaryCurrency != nil {
		fields["salary_currency"] = *input.SalaryCurrency
	}
	if input.WorkArrangement != nil {
		fields["work_arrangement"] = string(*input.WorkArrangement)
	}
	if input.EmploymentType != nil {
		fields["employment_type"] = string(*input.EmploymentType)
	}
}

func validateEmploymentType(employmentType models.EmploymentType) error {
	switch employmentType {
	case models.EmploymentTypeUnknown, models.EmploymentTypePermanent, models.EmploymentTypeB2B, models.EmploymentTypePermanentB2B:
		return nil
	default:
		return fmt.Errorf("invalid employment_type %q", employmentType)
	}
}

func validateWorkArrangement(arrangement models.WorkArrangement) error {
	switch arrangement {
	case models.WorkArrangementUnknown, models.WorkArrangementRemote, models.WorkArrangementHybrid, models.WorkArrangementOnSite:
		return nil
	default:
		return fmt.Errorf("invalid work_arrangement %q", arrangement)
	}
}

func addJobSalaryFields(fields map[string]interface{}, current *models.Job, input models.UpdateJobInput) {
	if input.SalaryMin == nil && input.SalaryMax == nil && input.SalaryType == nil {
		return
	}
	minSalary, maxSalary := current.SalaryMin, current.SalaryMax
	if input.SalaryMin != nil {
		minSalary = input.SalaryMin
	}
	if input.SalaryMax != nil {
		maxSalary = input.SalaryMax
	}
	if input.SalaryMin != nil || input.SalaryMax != nil {
		setSalaryRange(fields, minSalary, maxSalary, input.SalaryType)
		return
	}
	fields["salary_type"] = string(*input.SalaryType)
	if *input.SalaryType == models.SalaryUnknown {
		fields["salary_min"], fields["salary_max"] = nil, nil
	}
}

func setSalaryRange(fields map[string]interface{}, minSalary, maxSalary *int64, requestedType *models.SalaryType) {
	switch {
	case minSalary != nil && maxSalary != nil:
		low, high := *minSalary, *maxSalary
		if low > high {
			low, high = high, low
		}
		fields["salary_type"], fields["salary_min"], fields["salary_max"] = string(models.SalaryLimited), low, high
	case minSalary != nil:
		fields["salary_type"], fields["salary_min"], fields["salary_max"] = string(models.SalaryNoMax), *minSalary, nil
	case maxSalary != nil:
		fields["salary_type"], fields["salary_min"], fields["salary_max"] = string(models.SalaryNoMin), nil, *maxSalary
	default:
		salaryType := models.SalaryUnknown
		if requestedType != nil {
			salaryType = *requestedType
		}
		fields["salary_type"], fields["salary_min"], fields["salary_max"] = string(salaryType), nil, nil
	}
}

func addJobRecruiterFields(fields map[string]interface{}, input models.UpdateJobInput) {
	if input.RecruiterType != nil {
		fields["recruiter_type"] = string(*input.RecruiterType)
	}
	if input.RecruiterName != nil {
		fields["recruiter_name"] = *input.RecruiterName
	}
	if input.RecruiterAgency != nil {
		fields["recruiter_agency"] = *input.RecruiterAgency
	}
	if input.RecruiterContact != nil {
		fields["recruiter_contact"] = *input.RecruiterContact
	}
	if input.Interviewers != nil {
		encoded, _ := json.Marshal(input.Interviewers)
		fields["interviewers_json"] = string(encoded)
	}
	if input.JobPostURL != nil {
		fields["job_post_url"] = *input.JobPostURL
	}
	if input.AvatarSeed != nil {
		fields["avatar_seed"] = *input.AvatarSeed
	}
}

func addJobNotesFields(fields map[string]interface{}, input models.UpdateJobInput) {
	if input.KeywordNote != nil {
		fields["keyword_note"] = normalizeKeyword(input.KeywordNote)
	}
	if input.Description != nil {
		fields["description"] = strings.TrimSpace(*input.Description)
	}
	if input.CompanyOverview != nil {
		fields["company_overview"] = strings.TrimSpace(*input.CompanyOverview)
	}
	if input.CompanyDomain != nil {
		fields["company_domain"] = *input.CompanyDomain
	}
	if input.InterviewNotes != nil {
		fields["interview_notes"] = strings.TrimSpace(*input.InterviewNotes)
	}
	if input.ReasonsToChange != nil {
		fields["reasons_to_change"] = strings.TrimSpace(*input.ReasonsToChange)
	}
	addPreparationNoteFields(fields, input)
	if input.IsReferral != nil {
		fields["is_referral"] = *input.IsReferral
	}
	if input.OrderIndex != nil {
		fields["order_index"] = *input.OrderIndex
	}
}

func addPreparationNoteFields(fields map[string]interface{}, input models.UpdateJobInput) {
	if input.ExperienceNotes != nil {
		fields["experience_notes"] = strings.TrimSpace(*input.ExperienceNotes)
	}
	if input.ExpectedSalary != nil {
		fields["expected_salary"] = strings.TrimSpace(*input.ExpectedSalary)
	}
}

// DeleteJob removes the job identified by id.
func (s *JobService) DeleteJob(id string) error {
	atts, err := s.repo.GetAttachmentsByJobID(id)
	if err != nil {
		return fmt.Errorf("load job attachments before deletion: %w", err)
	}
	if err := s.repo.DeleteJob(id); err != nil {
		return err
	}
	for _, attachment := range atts {
		removeStoredAttachment(s.cfg.AttachmentsDir, attachment.StoredFilename)
	}
	s.mirrorAfterMutation()
	return nil
}

// ReorderJobs stores the supplied job ordering.
func (s *JobService) ReorderJobs(jobIDs []string) error {
	if err := s.repo.ReorderJobs(jobIDs); err != nil {
		return err
	}
	s.mirrorAfterMutation()
	return nil
}

// Stage methods

// CreateStage validates and persists a stage.
func (s *JobService) CreateStage(input models.CreateStageInput) (*models.Stage, error) {
	existing, err := s.repo.GetStagesByJobID(input.JobID)
	if err != nil {
		return nil, fmt.Errorf("load existing stages for job %q: %w", input.JobID, err)
	}
	orderIdx := len(existing)

	stg := &models.Stage{
		ID:               uuid.NewString(),
		JobID:            input.JobID,
		OrderIndex:       orderIdx,
		StageType:        input.StageType,
		CustomTitle:      input.CustomTitle,
		Description:      valueOrEmpty(input.Description),
		Status:           models.StageStatusPending,
		Notes:            valueOrEmpty(input.Notes),
		RecruiterName:    input.RecruiterName,
		RecruiterType:    valueOrDefault(input.RecruiterType, models.RecruiterNone),
		RecruiterAgency:  input.RecruiterAgency,
		RecruiterContact: input.RecruiterContact,
		Interviewers:     input.Interviewers,
		MeetingType:      "video",
	}
	if orderIdx == 0 {
		stg.Status = models.StageStatusCurrent
	}

	if err := s.repo.InsertStage(stg); err != nil {
		return nil, err
	}
	s.mirrorAfterMutation()
	return s.repo.GetStageByID(stg.ID)
}

// UpdateStage applies the supplied fields to an existing stage.
func (s *JobService) UpdateStage(id string, input models.UpdateStageInput) (*models.Stage, error) {
	if input.MeetingType != nil {
		if err := validateMeetingType(*input.MeetingType); err != nil {
			return nil, err
		}
	}
	fields := make(map[string]interface{})
	addStageCoreFields(fields, input)
	addStageMeetingFields(fields, input)
	addStageRecruiterFields(fields, input)
	if err := s.repo.UpdateStage(id, fields); err != nil {
		return nil, err
	}
	s.mirrorAfterMutation()
	return s.repo.GetStageByID(id)
}

func addStageCoreFields(fields map[string]interface{}, input models.UpdateStageInput) {
	if input.CustomTitle != nil {
		fields["custom_title"] = *input.CustomTitle
	}
	if input.Description != nil {
		fields["description"] = *input.Description
	}
	if input.Status != nil {
		fields["status"] = string(*input.Status)
	}
	if input.Notes != nil {
		fields["notes"] = *input.Notes
	}
	if input.Interviewers != nil {
		encoded, _ := json.Marshal(input.Interviewers)
		fields["interviewers_json"] = string(encoded)
	}
}

func addStageMeetingFields(fields map[string]interface{}, input models.UpdateStageInput) {
	if input.MeetingDate != nil {
		fields["meeting_date"] = *input.MeetingDate
	}
	if input.MeetingTime != nil {
		fields["meeting_time"] = *input.MeetingTime
	}
	if input.MeetingURL != nil {
		fields["meeting_url"] = *input.MeetingURL
	}
	if input.MeetingType != nil {
		fields["meeting_type"] = *input.MeetingType
	}
}

func addStageRecruiterFields(fields map[string]interface{}, input models.UpdateStageInput) {
	if input.RecruiterName != nil {
		fields["recruiter_name"] = *input.RecruiterName
	}
	if input.RecruiterType != nil {
		fields["recruiter_type"] = string(*input.RecruiterType)
	}
	if input.RecruiterAgency != nil {
		fields["recruiter_agency"] = *input.RecruiterAgency
	}
	if input.RecruiterContact != nil {
		fields["recruiter_contact"] = *input.RecruiterContact
	}
}

// DeleteStage removes the stage identified by id.
func (s *JobService) DeleteStage(id string) error {
	if err := s.repo.DeleteStage(id); err != nil {
		return err
	}
	s.mirrorAfterMutation()
	return nil
}

// ReorderStages stores the supplied ordering for a job's stages.
func (s *JobService) ReorderStages(jobID string, stageIDs []string) error {
	if err := s.repo.ReorderStages(jobID, stageIDs); err != nil {
		return err
	}
	s.mirrorAfterMutation()
	return nil
}

// SetCurrentStage marks the selected stage as current and updates sibling statuses.
func (s *JobService) SetCurrentStage(stageID string) ([]models.Stage, error) {
	stages, err := s.repo.SetCurrentStage(stageID)
	if err != nil {
		return nil, err
	}
	s.mirrorAfterMutation()
	return stages, nil
}

// ScheduleMeeting assigns meeting details to a stage.
func (s *JobService) ScheduleMeeting(stageID string, input models.ScheduleMeetingInput) error {
	if input.MeetingType != nil {
		if err := validateMeetingType(*input.MeetingType); err != nil {
			return err
		}
	}
	fields := map[string]interface{}{
		"meeting_date": input.MeetingDate,
		"meeting_time": input.MeetingTime,
	}
	if input.MeetingURL != nil {
		fields["meeting_url"] = *input.MeetingURL
	}
	if input.MeetingType != nil {
		fields["meeting_type"] = *input.MeetingType
	}
	if err := s.repo.UpdateStage(stageID, fields); err != nil {
		return err
	}
	s.mirrorAfterMutation()
	return nil
}

func validateMeetingType(meetingType string) error {
	switch meetingType {
	case "video", "phone", "onsite":
		return nil
	default:
		return fmt.Errorf("invalid meeting_type %q: expected video, phone, or onsite", meetingType)
	}
}

// Question methods

// CreateQuestion validates and persists a question for a stage.
func (s *JobService) CreateQuestion(input models.CreateQuestionInput) (*models.Question, error) {
	if strings.TrimSpace(input.Question) == "" {
		return nil, errors.New("question text is required")
	}
	q := &models.Question{
		ID:          uuid.NewString(),
		StageID:     input.StageID,
		Question:    strings.TrimSpace(input.Question),
		AnswerNotes: input.AnswerNotes,
		IsAsked:     false,
	}
	if err := s.repo.InsertQuestion(q); err != nil {
		return nil, err
	}
	s.mirrorAfterMutation()
	return q, nil
}

// UpdateQuestion applies the supplied fields to an existing question.
func (s *JobService) UpdateQuestion(id string, input models.UpdateQuestionInput) (*models.Question, error) {
	fields := make(map[string]interface{})
	if input.Question != nil {
		fields["question"] = strings.TrimSpace(*input.Question)
	}
	if input.AnswerNotes != nil {
		fields["answer_notes"] = strings.TrimSpace(*input.AnswerNotes)
	}
	if input.IsAsked != nil {
		fields["is_asked"] = *input.IsAsked
	}

	if err := s.repo.UpdateQuestion(id, fields); err != nil {
		return nil, err
	}
	s.mirrorAfterMutation()
	return s.repo.GetQuestionByID(id)
}

// DeleteQuestion removes the question identified by id.
func (s *JobService) DeleteQuestion(id string) error {
	if err := s.repo.DeleteQuestion(id); err != nil {
		return err
	}
	s.mirrorAfterMutation()
	return nil
}

// ReorderQuestions stores the supplied ordering for a stage's questions.
func (s *JobService) ReorderQuestions(stageID string, questionIDs []string) error {
	if err := s.repo.ReorderQuestions(stageID, questionIDs); err != nil {
		return err
	}
	s.mirrorAfterMutation()
	return nil
}

// Attachments

// StoreAttachment persists an uploaded file and its metadata.
func (s *JobService) StoreAttachment(jobID string, stageID *string, filename string, r io.Reader, size int64, mimeType string) (*models.Attachment, error) {
	if err := s.validateAttachmentOwner(jobID, stageID); err != nil {
		return nil, err
	}

	attID := uuid.NewString()
	ext := safeAttachmentExtension(filename)
	storedFilename := fmt.Sprintf("%s-%d%s", attID, time.Now().Unix(), ext)

	// Guard against path traversal
	cleanStored := filepath.Base(storedFilename)
	absPath := filepath.Join(s.cfg.AttachmentsDir, cleanStored)

	// absPath is built from the configured attachment directory and a generated UUID basename.
	out, err := os.OpenFile(absPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600) // #nosec G304
	if err != nil {
		return nil, fmt.Errorf("failed to create attachment file: %w", err)
	}
	written, err := io.Copy(out, r)
	closeErr := out.Close()
	if err != nil {
		_ = os.Remove(absPath) // best-effort deletion
		return nil, fmt.Errorf("failed to write attachment data: %w", err)
	}
	if closeErr != nil {
		_ = os.Remove(absPath) // best-effort deletion
		return nil, fmt.Errorf("failed to close attachment file: %w", closeErr)
	}

	att := &models.Attachment{
		ID:             attID,
		JobID:          jobID,
		StageID:        stageID,
		OriginalName:   filename,
		StoredFilename: cleanStored,
		FileSize:       written,
		MimeType:       mimeType,
	}

	if err := s.repo.InsertAttachment(att); err != nil {
		_ = os.Remove(absPath) // best-effort deletion
		return nil, err
	}

	s.mirrorAfterMutation()
	return att, nil
}

func (s *JobService) validateAttachmentOwner(jobID string, stageID *string) error {
	job, err := s.repo.GetJobByID(jobID)
	if err != nil {
		return fmt.Errorf("load attachment job %q: %w", jobID, err)
	}
	if job == nil {
		return sql.ErrNoRows
	}
	if stageID == nil {
		return nil
	}
	stage, err := s.repo.GetStageByID(*stageID)
	if err != nil {
		return fmt.Errorf("load attachment stage %q: %w", *stageID, err)
	}
	if stage == nil {
		return sql.ErrNoRows
	}
	if stage.JobID != jobID {
		return fmt.Errorf("stage %q does not belong to job %q", *stageID, jobID)
	}
	return nil
}

func safeAttachmentExtension(filename string) string {
	extension := filepath.Ext(filepath.Base(filename))
	if len(extension) < 2 || len(extension) > 13 {
		return ""
	}
	for _, char := range extension[1:] {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') {
			return ""
		}
	}
	return extension
}

// DeleteAttachment removes the attachment record and its stored file.
func (s *JobService) DeleteAttachment(id string) error {
	att, err := s.repo.GetAttachmentByID(id)
	if err != nil || att == nil {
		return sql.ErrNoRows
	}
	if err := s.repo.DeleteAttachment(id); err != nil {
		return err
	}
	// Derive the path from the stored basename instead of trusting a database path.
	removeStoredAttachment(s.cfg.AttachmentsDir, att.StoredFilename)
	s.mirrorAfterMutation()
	return nil
}

func removeStoredAttachment(directory, storedFilename string) {
	filename := filepath.Base(storedFilename)
	if filename != storedFilename || filename == "." || filename == string(filepath.Separator) {
		return
	}
	_ = os.Remove(filepath.Join(directory, filename)) // best-effort cleanup after the database mutation.
}

// GetAttachmentByID returns the attachment identified by id.
func (s *JobService) GetAttachmentByID(id string) (*models.Attachment, error) {
	return s.repo.GetAttachmentByID(id)
}

// GetScheduledMeetings returns the stages with scheduled interview details.
func (s *JobService) GetScheduledMeetings() ([]models.ScheduledMeeting, error) {
	return s.repo.GetScheduledMeetings()
}

// MirrorDatabaseToJSON atomically mirrors all jobs to backup.json
func (s *JobService) MirrorDatabaseToJSON() error {
	s.mirrorLock.Lock()
	defer s.mirrorLock.Unlock()

	var fullJobs []models.Job
	err := s.repo.WithReadSnapshot(func(snapshot *repository.Repository) error {
		var snapshotErr error
		fullJobs, snapshotErr = loadFullJobSnapshot(snapshot)
		return snapshotErr
	})
	if err != nil {
		return fmt.Errorf("load consistent database snapshot: %w", err)
	}

	data, err := json.MarshalIndent(fullJobs, "", "  ")
	if err != nil {
		return err
	}

	if err := writeBackupAtomically(s.cfg.BackupPath, data); err != nil {
		log.Printf("Warning: Failed to commit backup.json: %v", err)
		return err
	}

	return nil
}

func loadFullJobSnapshot(repo *repository.Repository) ([]models.Job, error) {
	jobs, err := repo.GetAllJobs("", "")
	if err != nil {
		return nil, err
	}
	stages, err := repo.GetAllStages()
	if err != nil {
		return nil, err
	}
	questions, err := repo.GetAllQuestions()
	if err != nil {
		return nil, err
	}
	attachments, err := repo.GetAllAttachments()
	if err != nil {
		return nil, err
	}
	return attachSnapshotRecords(jobs, stages, questions, attachments), nil
}

func attachSnapshotRecords(jobs []models.Job, stages []models.Stage, questions []models.Question, attachments []models.Attachment) []models.Job {
	jobIndexes := make(map[string]int, len(jobs))
	for index := range jobs {
		jobIndexes[jobs[index].ID] = index
	}
	stageIndexes := make(map[string]int, len(stages))
	for index := range stages {
		stageIndexes[stages[index].ID] = index
	}
	for _, question := range questions {
		if index, exists := stageIndexes[question.StageID]; exists {
			stages[index].Questions = append(stages[index].Questions, question)
		}
	}
	for _, stage := range stages {
		if index, exists := jobIndexes[stage.JobID]; exists {
			jobs[index].Stages = append(jobs[index].Stages, stage)
		}
	}
	for _, attachment := range attachments {
		if index, exists := jobIndexes[attachment.JobID]; exists {
			jobs[index].Attachments = append(jobs[index].Attachments, attachment)
		}
	}
	return jobs
}

func writeBackupAtomically(backupPath string, data []byte) error {
	temporaryPath := fmt.Sprintf("%s.tmp.%d", backupPath, time.Now().UnixNano())
	// temporaryPath is derived from the configured backup destination and an exclusive unique suffix.
	file, err := os.OpenFile(temporaryPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600) // #nosec G304
	if err != nil {
		return fmt.Errorf("create temporary backup: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return fmt.Errorf("write temporary backup: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync temporary backup: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close temporary backup: %w", err)
	}
	if err := os.Rename(temporaryPath, backupPath); err != nil {
		return fmt.Errorf("replace backup: %w", err)
	}
	committed = true
	return nil
}

func valueOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func valueOrDefault[T any](value *T, fallback T) T {
	if value == nil {
		return fallback
	}
	return *value
}
