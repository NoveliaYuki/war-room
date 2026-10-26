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
	repo         *repository.Repository
	cfg          *config.Config
	mirrorLock   sync.Mutex
	driveTrigger func(userID string)
}

// SetDriveTrigger registers the backup hook for job creation and status changes.
func (s *JobService) SetDriveTrigger(trigger func(userID string)) { s.driveTrigger = trigger }

func (s *JobService) notifyDriveBackup(userID string) {
	if s.driveTrigger != nil {
		s.driveTrigger(userID)
	}
}

// ErrInvalidAttachmentOwner indicates that an attachment references a missing or unrelated job or stage.
var ErrInvalidAttachmentOwner = errors.New("invalid attachment owner")

// ErrInvalidMeetingDate indicates a meeting date is not a real YYYY-MM-DD date.
var ErrInvalidMeetingDate = errors.New("meeting_date must be a valid YYYY-MM-DD date")

// ErrInvalidField indicates a request contains a value outside the supported model.
var ErrInvalidField = errors.New("request contains an invalid field value")

// ErrPositionTitleRequired indicates an attempted update with a blank title.
var ErrPositionTitleRequired = errors.New("position_title is required")

// ErrQuestionTextRequired indicates an attempted update with blank question text.
var ErrQuestionTextRequired = errors.New("question text is required")

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

// GetAllJobs returns userID's jobs matching the optional status and search filters.
func (s *JobService) GetAllJobs(userID, status, search string) ([]models.Job, error) {
	return s.repo.GetAllJobs(userID, status, search)
}

// GetJobCounts returns the number of userID's jobs in each status.
func (s *JobService) GetJobCounts(userID string) (*models.JobCounts, error) {
	return s.repo.GetJobCounts(userID)
}

// GetFullJobDetails returns a job with its stages, questions, and attachments, if owned by userID.
func (s *JobService) GetFullJobDetails(userID, id string) (*models.Job, error) {
	return getFullJobDetails(s.repo, userID, id)
}

func getFullJobDetails(repo *repository.Repository, userID, id string) (*models.Job, error) {
	job, err := repo.GetJobByID(userID, id)
	if err != nil {
		return nil, err
	}
	if job == nil {
		return nil, sql.ErrNoRows
	}

	stages, err := repo.GetStagesByJobID(userID, job.ID)
	if err != nil {
		return nil, err
	}

	for i := range stages {
		questions, err := repo.GetQuestionsByStageID(userID, stages[i].ID)
		if err != nil {
			return nil, err
		}
		stages[i].Questions = questions
	}
	job.Stages = stages

	attachments, err := repo.GetAttachmentsByJobID(userID, job.ID)
	if err != nil {
		return nil, err
	}
	job.Attachments = attachments

	return job, nil
}

// CreateJob validates and persists a job and any explicitly requested stages for userID.
func (s *JobService) CreateJob(userID string, input models.CreateJobInput) (*models.Job, error) {
	if err := s.validateCreateTechnologies(userID, input.TechnologyIDs); err != nil {
		return nil, err
	}
	job, err := buildJob(input)
	if err != nil {
		return nil, err
	}
	job.SearchPeriodID, err = s.resolveNewJobSearchPeriod(userID, input.SearchPeriodID)
	if err != nil {
		return nil, err
	}
	job.ID = uuid.NewString()
	if err := s.assignLatestCV(userID, job); err != nil {
		return nil, err
	}
	if err := s.repo.InsertJob(userID, job); err != nil {
		return nil, err
	}
	if err := s.assignCreateTechnologies(userID, job, input.TechnologyIDs); err != nil {
		_ = s.repo.DeleteJob(userID, job.ID)
		return nil, err
	}
	if err := s.createStagesIfRequested(job, input); err != nil {
		_ = s.repo.DeleteJob(userID, job.ID)
		return nil, err
	}
	s.mirrorAfterMutation()
	s.notifyDriveBackup(userID)
	return s.GetFullJobDetails(userID, job.ID)
}

func (s *JobService) validateCreateTechnologies(userID string, ids []string) error {
	if ids == nil {
		return nil
	}
	return s.validateJobTechnologyIDs(userID, ids)
}

func (s *JobService) assignCreateTechnologies(userID string, job *models.Job, ids []string) error {
	if ids == nil {
		return nil
	}
	if err := s.repo.SetJobTechnologies(userID, job.ID, ids); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidField, err)
	}
	return nil
}

func (s *JobService) createStagesIfRequested(job *models.Job, input models.CreateJobInput) error {
	if input.CreateDefaultStages == nil || !*input.CreateDefaultStages {
		return nil
	}
	return s.createDefaultStages(job, input)
}

func (s *JobService) assignLatestCV(userID string, job *models.Job) error {
	if job.IsReferral {
		return nil
	}
	versions, err := s.repo.ListCVVersions(userID)
	if err != nil {
		return err
	}
	if len(versions) > 0 {
		job.SelectedCVVersion = &versions[0]
	}
	return nil
}

func buildJob(input models.CreateJobInput) (*models.Job, error) {
	if err := validateCreateJobEnums(input); err != nil {
		return nil, err
	}
	workArrangement, employmentType, err := normalizeJobWorkDetails(input)
	if err != nil {
		return nil, err
	}
	position := strings.TrimSpace(input.PositionTitle)
	if position == "" {
		return nil, ErrPositionTitleRequired
	}
	company := strings.TrimSpace(input.CompanyName)
	if company == "" {
		company = "Unknown"
	}
	keyword := normalizeKeyword(input.KeywordNote)
	avatar := jobAvatar(company, position, input.AvatarSeed)
	salaryType, minSalary, maxSalary := normalizeSalary(input)
	status, recruiterType, currency := defaultJobCreationValues()
	if input.Status != nil {
		status = *input.Status
	}
	if input.RecruiterType != nil {
		recruiterType = *input.RecruiterType
	}
	currency = submittedCurrency(input.SalaryCurrency, currency)
	referral := input.IsReferral != nil && *input.IsReferral
	applicationSentDate := normalizeOptionalDate(input.ApplicationSentDate)
	if !referral && applicationSentDate == nil {
		today := time.Now().Format("2006-01-02")
		applicationSentDate = &today
	}
	return &models.Job{
		CompanyName: company, PositionTitle: position, Status: status,
		SalaryType: salaryType, SalaryMin: minSalary, SalaryMax: maxSalary,
		SalaryCurrency: currency, RecruiterType: recruiterType,
		RecruiterName: input.RecruiterName, RecruiterAgency: input.RecruiterAgency,
		RecruiterContact: input.RecruiterContact, Interviewers: input.Interviewers,
		JobPostURL: input.JobPostURL, ApplicationURL: input.ApplicationURL, AvatarSeed: avatar, KeywordNote: keyword,
		Description: valueOrEmpty(input.Description), CompanyOverview: valueOrEmpty(input.CompanyOverview),
		CompanyDomain: input.CompanyDomain, InterviewNotes: valueOrEmpty(input.InterviewNotes),
		ApplicationSentDate:       applicationSentDate,
		RecruiterFirstContactDate: normalizeOptionalDate(input.RecruiterFirstContactDate),
		ReasonsToChange:           valueOrEmpty(input.ReasonsToChange),
		ExperienceNotes:           strings.TrimSpace(valueOrEmpty(input.ExperienceNotes)),
		ExpectedSalary:            strings.TrimSpace(valueOrEmpty(input.ExpectedSalary)),
		WorkArrangement:           workArrangement,
		EmploymentType:            employmentType,
		IsReferral:                referral,
		Technologies:              []models.Technology{},
	}, nil
}

func submittedCurrency(value *string, fallback string) string {
	if value == nil || *value == "" {
		return fallback
	}
	return *value
}

func defaultJobCreationValues() (models.JobStatus, models.RecruiterType, string) {
	return models.StatusWaiting, models.RecruiterNone, "EUR"
}

func validateCreateJobEnums(input models.CreateJobInput) error {
	return firstValidationError(
		validateOptionalEnum(input.Status, validateJobStatus),
		validateOptionalEnum(input.RecruiterType, validateRecruiterType),
		validateOptionalEnum(input.SalaryType, validateSalaryType),
		validateOptionalEnum(input.WorkArrangement, validateWorkArrangement),
		validateOptionalEnum(input.EmploymentType, validateEmploymentType),
		validateOptionalDate(input.ApplicationSentDate, "application_sent_date"),
		validateOptionalDate(input.RecruiterFirstContactDate, "recruiter_first_contact_date"),
	)
}

func validateOptionalDate(value *string, field string) error {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil
	}
	date := strings.TrimSpace(*value)
	parsed, err := time.Parse("2006-01-02", date)
	if err != nil || parsed.Format("2006-01-02") != date {
		return fmt.Errorf("%w: %s must use YYYY-MM-DD", ErrInvalidField, field)
	}
	return nil
}

func normalizeOptionalDate(value *string) *string {
	if value == nil {
		return nil
	}
	date := strings.TrimSpace(*value)
	if date == "" {
		return nil
	}
	return &date
}

func validateOptionalEnum[T ~string](value *T, validate func(T) error) error {
	if value == nil {
		return nil
	}
	return validate(*value)
}

func firstValidationError(validationErrors ...error) error {
	for _, err := range validationErrors {
		if err != nil {
			return err
		}
	}
	return nil
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

// UpdateJob applies the supplied fields to an existing job owned by userID.
func (s *JobService) UpdateJob(userID, id string, input models.UpdateJobInput) (*models.Job, error) {
	if err := s.validateJobUpdate(userID, input); err != nil {
		return nil, err
	}
	current, err := s.repo.GetJobByID(userID, id)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, sql.ErrNoRows
	}
	fields := jobUpdateFields(current, input)

	if err := s.repo.UpdateJob(userID, id, fields); err != nil {
		return nil, err
	}
	statusChanged := input.Status != nil && *input.Status != current.Status
	if err := s.updateJobTechnologies(userID, id, input.TechnologyIDs, statusChanged); err != nil {
		return nil, err
	}

	s.mirrorAfterMutation()
	if statusChanged {
		s.notifyDriveBackup(userID)
	}
	return s.GetFullJobDetails(userID, id)
}

func (s *JobService) updateJobTechnologies(userID, id string, technologyIDs []string, statusChanged bool) error {
	if technologyIDs == nil {
		return nil
	}
	if err := s.repo.SetJobTechnologies(userID, id, technologyIDs); err != nil {
		if statusChanged {
			s.mirrorAfterMutation()
			s.notifyDriveBackup(userID)
		}
		return fmt.Errorf("%w: %v", ErrInvalidField, err)
	}
	return nil
}

func (s *JobService) validateJobUpdate(userID string, input models.UpdateJobInput) error {
	if err := s.validateTechnologyUpdate(userID, input.TechnologyIDs); err != nil {
		return err
	}
	if input.PositionTitle != nil && strings.TrimSpace(*input.PositionTitle) == "" {
		return ErrPositionTitleRequired
	}
	if err := validateUpdateJobEnums(input); err != nil {
		return err
	}
	return s.validateJobSearchPeriod(userID, input.SearchPeriodID)
}

func (s *JobService) validateTechnologyUpdate(userID string, ids []string) error {
	if ids == nil {
		return nil
	}
	return s.validateJobTechnologyIDs(userID, ids)
}

func (s *JobService) validateJobTechnologyIDs(userID string, ids []string) error {
	catalog, err := s.repo.ListTechnologies(userID)
	if err != nil {
		return err
	}
	known := make(map[string]bool, len(catalog))
	for _, item := range catalog {
		known[item.ID] = true
	}
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if strings.TrimSpace(id) == "" || seen[id] || !known[id] {
			return fmt.Errorf("%w: invalid or duplicate technology ID", ErrInvalidField)
		}
		seen[id] = true
	}
	return nil
}

// ListTechnologies returns userID's catalog entries and their assigned processes.
func (s *JobService) ListTechnologies(userID string) ([]models.Technology, error) {
	return s.repo.ListTechnologies(userID)
}

// CreateTechnology adds a canonical catalog entry and aliases for userID.
func (s *JobService) CreateTechnology(userID, name string, aliases []string) (models.Technology, error) {
	item := models.Technology{ID: uuid.NewString(), Name: strings.TrimSpace(name), Aliases: aliases}
	if err := normalizeTechnologyInput(&item); err != nil {
		return models.Technology{}, err
	}
	if err := s.validateTechnologyTerms(userID, item, ""); err != nil {
		return models.Technology{}, err
	}
	if err := s.repo.CreateTechnology(userID, item); err != nil {
		return models.Technology{}, fmt.Errorf("%w: %v", ErrInvalidField, err)
	}
	s.mirrorAfterMutation()
	return item, nil
}

// UpdateTechnology replaces a canonical name and its aliases for userID.
func (s *JobService) UpdateTechnology(userID, id, name string, aliases []string) (models.Technology, error) {
	item := models.Technology{ID: id, Name: strings.TrimSpace(name), Aliases: aliases}
	if err := normalizeTechnologyInput(&item); err != nil {
		return models.Technology{}, err
	}
	if err := s.validateTechnologyTerms(userID, item, id); err != nil {
		return models.Technology{}, err
	}
	if err := s.repo.UpdateTechnology(userID, item); err != nil {
		return models.Technology{}, err
	}
	s.mirrorAfterMutation()
	return item, nil
}

func normalizeTechnologyInput(item *models.Technology) error {
	if item.Name == "" || len(item.Name) > 80 || len(item.Aliases) > 20 {
		return fmt.Errorf("%w: technology name is required and limited to 80 characters; at most 20 aliases are allowed", ErrInvalidField)
	}
	aliases := make([]string, 0, len(item.Aliases))
	seen := map[string]bool{}
	for _, raw := range item.Aliases {
		alias := strings.TrimSpace(raw)
		key := technologyTermKey(alias)
		if alias == "" {
			continue
		}
		if len(alias) > 80 || key == technologyTermKey(item.Name) || seen[key] {
			return fmt.Errorf("%w: technology aliases must be unique and different from the canonical name", ErrInvalidField)
		}
		seen[key] = true
		aliases = append(aliases, alias)
	}
	item.Aliases = aliases
	return nil
}

func (s *JobService) validateTechnologyTerms(userID string, item models.Technology, excludeID string) error {
	existing, err := s.repo.ListTechnologies(userID)
	if err != nil {
		return err
	}
	terms := map[string]bool{technologyTermKey(item.Name): true}
	for _, alias := range item.Aliases {
		key := technologyTermKey(alias)
		if terms[key] {
			return fmt.Errorf("%w: duplicate technology alias", ErrInvalidField)
		}
		terms[key] = true
	}
	for _, current := range existing {
		if current.ID == excludeID {
			continue
		}
		if terms[technologyTermKey(current.Name)] {
			return fmt.Errorf("%w: technology name or alias already exists", ErrInvalidField)
		}
		for _, alias := range current.Aliases {
			if terms[technologyTermKey(alias)] {
				return fmt.Errorf("%w: technology name or alias already exists", ErrInvalidField)
			}
		}
	}
	return nil
}

func technologyTermKey(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}

// DeleteTechnology removes an unused catalog entry owned by userID.
func (s *JobService) DeleteTechnology(userID, id string) error {
	if err := s.repo.DeleteTechnology(userID, id); err != nil {
		return err
	}
	s.mirrorAfterMutation()
	return nil
}

// RemoveTechnologyAssignments unassigns a technology from every process owned by userID.
func (s *JobService) RemoveTechnologyAssignments(userID, id string) (int64, error) {
	count, err := s.repo.RemoveTechnologyAssignments(userID, id)
	if err == nil {
		s.mirrorAfterMutation()
	}
	return count, err
}

func validateUpdateJobEnums(input models.UpdateJobInput) error {
	return firstValidationError(
		validateOptionalEnum(input.Status, validateJobStatus),
		validateOptionalEnum(input.RecruiterType, validateRecruiterType),
		validateOptionalEnum(input.SalaryType, validateSalaryType),
		validateOptionalEnum(input.WorkArrangement, validateWorkArrangement),
		validateOptionalEnum(input.EmploymentType, validateEmploymentType),
		validateOptionalDate(input.ApplicationSentDate, "application_sent_date"),
		validateOptionalDate(input.RecruiterFirstContactDate, "recruiter_first_contact_date"),
	)
}

func jobUpdateFields(current *models.Job, input models.UpdateJobInput) map[string]interface{} {
	fields := make(map[string]interface{})
	addJobIdentityFields(fields, input)
	addJobSalaryFields(fields, current, input)
	addJobRecruiterFields(fields, input)
	addJobNotesFields(fields, input)
	if input.SearchPeriodID != nil {
		var periodID any
		if strings.TrimSpace(*input.SearchPeriodID) != "" {
			periodID = strings.TrimSpace(*input.SearchPeriodID)
		}
		fields["search_period_id"] = periodID
	}
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
		return fmt.Errorf("%w: invalid employment_type %q", ErrInvalidField, employmentType)
	}
}

func validateWorkArrangement(arrangement models.WorkArrangement) error {
	switch arrangement {
	case models.WorkArrangementUnknown, models.WorkArrangementRemote, models.WorkArrangementHybrid, models.WorkArrangementOnSite:
		return nil
	default:
		return fmt.Errorf("%w: invalid work_arrangement %q", ErrInvalidField, arrangement)
	}
}

func validateJobStatus(status models.JobStatus) error {
	switch status {
	case models.StatusWaiting, models.StatusOngoing, models.StatusAccepted, models.StatusRejected:
		return nil
	}
	return fmt.Errorf("%w: invalid status %q", ErrInvalidField, status)
}

func validateRecruiterType(value models.RecruiterType) error {
	switch value {
	case models.RecruiterInternal, models.RecruiterExternal, models.RecruiterNone:
		return nil
	}
	return fmt.Errorf("%w: invalid recruiter_type %q", ErrInvalidField, value)
}

func validateSalaryType(value models.SalaryType) error {
	switch value {
	case models.SalaryLimited, models.SalaryNoMin, models.SalaryNoMax, models.SalaryUnknown:
		return nil
	}
	return fmt.Errorf("%w: invalid salary_type %q", ErrInvalidField, value)
}

func validateStageType(value models.StageType) error {
	switch value {
	case models.StageHR, models.StageTechnical, models.StageCultural, models.StageOfferDecision:
		return nil
	}
	return fmt.Errorf("%w: invalid stage_type %q", ErrInvalidField, value)
}

func validateStageStatus(value models.StageStatus) error {
	switch value {
	case models.StageStatusPending, models.StageStatusCurrent, models.StageStatusCompleted, models.StageStatusSkipped:
		return nil
	}
	return fmt.Errorf("%w: invalid stage status %q", ErrInvalidField, value)
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
	if input.SalaryType != nil {
		minSalary, maxSalary = boundsForSalaryType(*input.SalaryType, minSalary, maxSalary)
	}
	if input.SalaryMin != nil || input.SalaryMax != nil {
		setSalaryRange(fields, minSalary, maxSalary, input.SalaryType)
		return
	}
	fields["salary_type"] = string(*input.SalaryType)
	fields["salary_min"], fields["salary_max"] = salaryBound(minSalary), salaryBound(maxSalary)
}

func salaryBound(value *int64) interface{} {
	if value == nil {
		return nil
	}
	return *value
}

// boundsForSalaryType drops the bounds an explicit salary type says do not
// exist, because a JSON null cannot tell "clear this bound" from "unchanged".
func boundsForSalaryType(salaryType models.SalaryType, minSalary, maxSalary *int64) (*int64, *int64) {
	switch salaryType {
	case models.SalaryNoMin:
		return nil, maxSalary
	case models.SalaryNoMax:
		return minSalary, nil
	case models.SalaryUnknown:
		return nil, nil
	}
	return minSalary, maxSalary
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
	if input.ApplicationURL != nil {
		fields["application_url"] = *input.ApplicationURL
	}
	if input.AvatarSeed != nil {
		fields["avatar_seed"] = *input.AvatarSeed
	}
}

func addJobNotesFields(fields map[string]interface{}, input models.UpdateJobInput) {
	addJobLongTextFields(fields, input)
	addJobProcessDateFields(fields, input)
	addPreparationNoteFields(fields, input)
	addJobFlags(fields, input)
}

func addJobLongTextFields(fields map[string]interface{}, input models.UpdateJobInput) {
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
}

func addJobProcessDateFields(fields map[string]interface{}, input models.UpdateJobInput) {
	if input.ApplicationSentDate != nil {
		fields["application_sent_date"] = normalizeOptionalDate(input.ApplicationSentDate)
	}
	if input.RecruiterFirstContactDate != nil {
		fields["recruiter_first_contact_date"] = normalizeOptionalDate(input.RecruiterFirstContactDate)
	}
}

func addJobFlags(fields map[string]interface{}, input models.UpdateJobInput) {
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

// DeleteJob removes the job identified by id if owned by userID.
func (s *JobService) DeleteJob(userID, id string) error {
	atts, err := s.repo.GetAttachmentsByJobID(userID, id)
	if err != nil {
		return fmt.Errorf("load job attachments before deletion: %w", err)
	}
	if err := s.repo.DeleteJob(userID, id); err != nil {
		return err
	}
	for _, attachment := range atts {
		removeStoredAttachment(s.cfg.AttachmentsDir, attachment.StoredFilename)
	}
	s.mirrorAfterMutation()
	return nil
}

// ReorderJobs stores the supplied job ordering for userID's own jobs.
func (s *JobService) ReorderJobs(userID string, jobIDs []string) error {
	if err := s.repo.ReorderJobs(userID, jobIDs); err != nil {
		return err
	}
	s.mirrorAfterMutation()
	return nil
}

// Stage methods

// CreateStage validates and persists a stage for a job owned by userID.
func (s *JobService) CreateStage(userID string, input models.CreateStageInput) (*models.Stage, error) {
	if err := validateCreateStageInput(input); err != nil {
		return nil, err
	}
	orderIdx, err := s.ownedStageOrderIndex(userID, input.JobID)
	if err != nil {
		return nil, err
	}
	stg := buildNewStage(input, orderIdx)

	if orderIdx == 0 {
		err = s.repo.InsertFirstStageAndSetJobOngoing(stg)
	} else {
		err = s.repo.InsertStage(stg)
	}
	if err != nil {
		return nil, err
	}
	s.mirrorAfterMutation()
	if orderIdx == 0 {
		s.notifyDriveBackup(userID)
	}
	return s.repo.GetStageByID(userID, stg.ID)
}

func validateCreateStageInput(input models.CreateStageInput) error {
	if err := validateStageType(input.StageType); err != nil {
		return err
	}
	if input.RecruiterType != nil {
		return validateRecruiterType(*input.RecruiterType)
	}
	return nil
}

// ownedStageOrderIndex verifies jobID belongs to userID and returns the
// order index a newly created stage should take (the count of its existing
// siblings).
func (s *JobService) ownedStageOrderIndex(userID, jobID string) (int, error) {
	job, err := s.repo.GetJobByID(userID, jobID)
	if err != nil {
		return 0, fmt.Errorf("load job %q: %w", jobID, err)
	}
	if job == nil {
		return 0, sql.ErrNoRows
	}
	existing, err := s.repo.GetStagesByJobID(userID, jobID)
	if err != nil {
		return 0, fmt.Errorf("load existing stages for job %q: %w", jobID, err)
	}
	return len(existing), nil
}

func buildNewStage(input models.CreateStageInput, orderIdx int) *models.Stage {
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
	return stg
}

// UpdateStage applies the supplied fields to an existing stage owned by userID.
func (s *JobService) UpdateStage(userID, id string, input models.UpdateStageInput) (*models.Stage, error) {
	if input.Status != nil {
		if err := validateStageStatus(*input.Status); err != nil {
			return nil, err
		}
	}
	if input.RecruiterType != nil {
		if err := validateRecruiterType(*input.RecruiterType); err != nil {
			return nil, err
		}
	}
	if err := validateOptionalMeetingDate(input.MeetingDate); err != nil {
		return nil, err
	}
	if input.MeetingType != nil {
		if err := validateMeetingType(*input.MeetingType); err != nil {
			return nil, err
		}
	}
	fields := make(map[string]interface{})
	addStageCoreFields(fields, input)
	addStageMeetingFields(fields, input)
	addStageRecruiterFields(fields, input)
	if err := s.repo.UpdateStage(userID, id, fields); err != nil {
		return nil, err
	}
	s.mirrorAfterMutation()
	return s.repo.GetStageByID(userID, id)
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

// DeleteStage removes the stage identified by id if owned by userID.
func (s *JobService) DeleteStage(userID, id string) error {
	if err := s.repo.DeleteStage(userID, id); err != nil {
		return err
	}
	s.mirrorAfterMutation()
	return nil
}

// ReorderStages stores the supplied ordering for a job's stages if owned by userID.
func (s *JobService) ReorderStages(userID, jobID string, stageIDs []string) error {
	if err := s.repo.ReorderStages(userID, jobID, stageIDs); err != nil {
		return err
	}
	s.mirrorAfterMutation()
	return nil
}

// SetCurrentStage marks the selected stage as current and updates sibling statuses, if owned by userID.
func (s *JobService) SetCurrentStage(userID, stageID string) ([]models.Stage, error) {
	stages, err := s.repo.SetCurrentStage(userID, stageID)
	if err != nil {
		return nil, err
	}
	s.mirrorAfterMutation()
	return stages, nil
}

// ScheduleMeeting assigns meeting details to a stage owned by userID.
func (s *JobService) ScheduleMeeting(userID, stageID string, input models.ScheduleMeetingInput) error {
	if err := validateOptionalMeetingDate(&input.MeetingDate); err != nil {
		return err
	}
	if input.MeetingType != nil {
		if err := validateMeetingType(*input.MeetingType); err != nil {
			return err
		}
	}
	fields := scheduleMeetingFields(input)
	if err := s.repo.UpdateStage(userID, stageID, fields); err != nil {
		return err
	}
	s.mirrorAfterMutation()
	return nil
}

// scheduleMeetingFields maps a schedule request to stage columns. A meeting
// that is no longer a video call, or is cleared, drops its stale link.
func scheduleMeetingFields(input models.ScheduleMeetingInput) map[string]interface{} {
	fields := map[string]interface{}{
		"meeting_date": input.MeetingDate,
		"meeting_time": input.MeetingTime,
	}
	notVideo := input.MeetingType != nil && *input.MeetingType != "video"
	switch {
	case input.MeetingURL != nil:
		fields["meeting_url"] = *input.MeetingURL
	case notVideo, input.MeetingDate == "" && input.MeetingTime == "":
		fields["meeting_url"] = ""
	}
	if input.MeetingType != nil {
		fields["meeting_type"] = *input.MeetingType
	}
	if input.Notes != nil {
		fields["notes"] = *input.Notes
	}
	return fields
}

func validateMeetingDate(value string) error {
	if _, err := time.Parse(time.DateOnly, value); err != nil {
		return ErrInvalidMeetingDate
	}
	return nil
}

func validateOptionalMeetingDate(value *string) error {
	if value == nil || *value == "" {
		return nil
	}
	return validateMeetingDate(*value)
}

func validateMeetingType(meetingType string) error {
	switch meetingType {
	case "video", "phone", "onsite":
		return nil
	default:
		return fmt.Errorf("%w: invalid meeting_type %q: expected video, phone, or onsite", ErrInvalidField, meetingType)
	}
}

// Question methods

// CreateQuestion validates and persists a question for a stage owned by userID.
func (s *JobService) CreateQuestion(userID string, input models.CreateQuestionInput) (*models.Question, error) {
	if strings.TrimSpace(input.Question) == "" {
		return nil, ErrQuestionTextRequired
	}
	stage, err := s.repo.GetStageByID(userID, input.StageID)
	if err != nil {
		return nil, fmt.Errorf("load stage %q: %w", input.StageID, err)
	}
	if stage == nil {
		return nil, sql.ErrNoRows
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

// UpdateQuestion applies the supplied fields to an existing question owned by userID.
func (s *JobService) UpdateQuestion(userID, id string, input models.UpdateQuestionInput) (*models.Question, error) {
	if input.Question != nil && strings.TrimSpace(*input.Question) == "" {
		return nil, ErrQuestionTextRequired
	}
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

	if err := s.repo.UpdateQuestion(userID, id, fields); err != nil {
		return nil, err
	}
	s.mirrorAfterMutation()
	return s.repo.GetQuestionByID(userID, id)
}

// DeleteQuestion removes the question identified by id if owned by userID.
func (s *JobService) DeleteQuestion(userID, id string) error {
	if err := s.repo.DeleteQuestion(userID, id); err != nil {
		return err
	}
	s.mirrorAfterMutation()
	return nil
}

// ReorderQuestions stores the supplied ordering for a stage's questions if owned by userID.
func (s *JobService) ReorderQuestions(userID, stageID string, questionIDs []string) error {
	if err := s.repo.ReorderQuestions(userID, stageID, questionIDs); err != nil {
		return err
	}
	s.mirrorAfterMutation()
	return nil
}

// Attachments

// StoreAttachment persists an uploaded file and its metadata for userID.
func (s *JobService) StoreAttachment(userID, jobID string, stageID *string, filename string, r io.Reader, size int64, mimeType string) (*models.Attachment, error) {
	if err := s.validateAttachmentOwner(userID, jobID, stageID); err != nil {
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

func (s *JobService) validateAttachmentOwner(userID, jobID string, stageID *string) error {
	job, err := s.repo.GetJobByID(userID, jobID)
	if err != nil {
		return fmt.Errorf("load attachment job %q: %w", jobID, err)
	}
	if job == nil {
		return fmt.Errorf("%w: job %q was not found", ErrInvalidAttachmentOwner, jobID)
	}
	if stageID == nil {
		return nil
	}
	stage, err := s.repo.GetStageByID(userID, *stageID)
	if err != nil {
		return fmt.Errorf("load attachment stage %q: %w", *stageID, err)
	}
	if stage == nil {
		return fmt.Errorf("%w: stage %q was not found", ErrInvalidAttachmentOwner, *stageID)
	}
	if stage.JobID != jobID {
		return fmt.Errorf("%w: stage %q does not belong to job %q", ErrInvalidAttachmentOwner, *stageID, jobID)
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

// DeleteAttachment removes the attachment record and its stored file, if owned by userID.
func (s *JobService) DeleteAttachment(userID, id string) error {
	att, err := s.repo.GetAttachmentByID(userID, id)
	if err != nil || att == nil {
		return sql.ErrNoRows
	}
	if err := s.repo.DeleteAttachment(userID, id); err != nil {
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

// GetAttachmentByID returns the attachment identified by id if owned by
// userID, or sql.ErrNoRows when it does not exist or belongs to someone else.
func (s *JobService) GetAttachmentByID(userID, id string) (*models.Attachment, error) {
	att, err := s.repo.GetAttachmentByID(userID, id)
	if err != nil {
		return nil, err
	}
	if att == nil {
		return nil, sql.ErrNoRows
	}
	return att, nil
}

// GetScheduledMeetings returns userID's stages with scheduled interview details.
func (s *JobService) GetScheduledMeetings(userID string) ([]models.ScheduledMeeting, error) {
	return s.repo.GetScheduledMeetings(userID)
}

// MirrorDatabaseToJSON atomically mirrors every account's jobs to backup.json.
// This local disaster-recovery snapshot deliberately stays install-wide
// (unlike every other read in this file): it exists to repopulate an empty,
// freshly provisioned database, before any account has signed in, so it
// cannot be scoped to one acting user. See models.Job.UserID.
func (s *JobService) MirrorDatabaseToJSON() error {
	s.mirrorLock.Lock()
	defer s.mirrorLock.Unlock()

	var fullJobs []models.Job
	var technologies []models.Technology
	var searchPeriods []models.SearchPeriod
	var cvVersions []models.CVVersion
	var nextCVVersion int
	err := s.repo.WithReadSnapshot(func(snapshot *repository.Repository) error {
		var snapshotErr error
		fullJobs, snapshotErr = loadFullJobSnapshotAllUsers(snapshot)
		if snapshotErr == nil {
			technologies, snapshotErr = snapshot.ListTechnologiesAllUsers()
		}
		if snapshotErr == nil {
			searchPeriods, snapshotErr = snapshot.ListSearchPeriodsAllUsers()
		}
		if snapshotErr == nil {
			cvVersions, snapshotErr = snapshot.ListCVVersionsAllUsers()
		}
		if snapshotErr == nil {
			nextCVVersion, snapshotErr = snapshot.GetNextCVVersionAllUsers()
		}
		return snapshotErr
	})
	if err != nil {
		return fmt.Errorf("load consistent database snapshot: %w", err)
	}

	snapshotVersions := make([]models.CVVersionSnapshot, 0, len(cvVersions))
	for _, version := range cvVersions {
		snapshotVersions = append(snapshotVersions, version.Snapshot())
	}
	data, err := json.MarshalIndent(models.RecoverySnapshot{Jobs: fullJobs, SearchPeriods: searchPeriods, Technologies: technologies, CVVersions: snapshotVersions, NextCVVersion: nextCVVersion}, "", "  ")
	if err != nil {
		return err
	}

	if err := writeBackupAtomically(s.cfg.BackupPath, data); err != nil {
		log.Printf("Warning: Failed to commit backup.json: %v", err)
		return err
	}

	return nil
}

// loadFullJobSnapshot loads userID's own jobs with their stages, questions,
// and attachments, for the exported backup archive.
func loadFullJobSnapshot(repo *repository.Repository, userID string) ([]models.Job, error) {
	jobs, err := repo.GetAllJobs(userID, "", "")
	if err != nil {
		return nil, err
	}
	for index := range jobs {
		stages, err := repo.GetStagesByJobID(userID, jobs[index].ID)
		if err != nil {
			return nil, err
		}
		for stageIndex := range stages {
			questions, err := repo.GetQuestionsByStageID(userID, stages[stageIndex].ID)
			if err != nil {
				return nil, err
			}
			stages[stageIndex].Questions = questions
		}
		jobs[index].Stages = stages
		attachments, err := repo.GetAttachmentsByJobID(userID, jobs[index].ID)
		if err != nil {
			return nil, err
		}
		jobs[index].Attachments = attachments
	}
	return jobs, nil
}

// loadFullJobSnapshotAllUsers loads every account's jobs (with their
// stages, questions, and attachments) for the install-wide recovery mirror.
func loadFullJobSnapshotAllUsers(repo *repository.Repository) ([]models.Job, error) {
	jobs, err := repo.GetAllJobsAllUsers()
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
