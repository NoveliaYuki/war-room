// Package models defines API and persistence data structures.
package models

import (
	"encoding/json"
	"fmt"
)

// JobStatus describes the current outcome of a job application.
type JobStatus string

const (
	// StatusOngoing marks a job application that is still active.
	StatusOngoing JobStatus = "ongoing"
	// StatusAccepted marks a job application that resulted in an offer.
	StatusAccepted JobStatus = "accepted"
	// StatusRejected marks a job application that did not proceed.
	StatusRejected JobStatus = "rejected"
)

// SalaryType describes how a job's salary range is represented.
type SalaryType string

const (
	// SalaryLimited indicates both salary bounds are known.
	SalaryLimited SalaryType = "limited"
	// SalaryNoMin indicates only an upper salary bound is known.
	SalaryNoMin SalaryType = "no_min"
	// SalaryNoMax indicates only a lower salary bound is known.
	SalaryNoMax SalaryType = "no_max"
	// SalaryUnknown indicates no salary information is available.
	SalaryUnknown SalaryType = "unknown"
)

// WorkArrangement describes where the role is normally performed.
type WorkArrangement string

const (
	// WorkArrangementUnknown marks a role with no known work location.
	WorkArrangementUnknown WorkArrangement = "unknown"
	// WorkArrangementRemote marks a fully remote role.
	WorkArrangementRemote WorkArrangement = "remote"
	// WorkArrangementHybrid marks a hybrid role.
	WorkArrangementHybrid WorkArrangement = "hybrid"
	// WorkArrangementOnSite marks a role based on-site.
	WorkArrangementOnSite WorkArrangement = "on_site"
)

// EmploymentType describes the contractual arrangement for a role.
type EmploymentType string

const (
	// EmploymentTypeUnknown marks a role with no known contract type.
	EmploymentTypeUnknown EmploymentType = "unknown"
	// EmploymentTypePermanent marks a permanent employee role.
	EmploymentTypePermanent EmploymentType = "permanent"
	// EmploymentTypeB2B marks a business-to-business contract.
	EmploymentTypeB2B EmploymentType = "b2b"
	// EmploymentTypePermanentB2B marks a role combining permanent and B2B terms.
	EmploymentTypePermanentB2B EmploymentType = "permanent_b2b"
)

// RecruiterType describes the relationship of a recruiter to the company.
type RecruiterType string

const (
	// RecruiterInternal identifies a company employee as the recruiter.
	RecruiterInternal RecruiterType = "internal"
	// RecruiterExternal identifies an agency or independent recruiter.
	RecruiterExternal RecruiterType = "external"
	// RecruiterNone indicates that no recruiter is recorded.
	RecruiterNone RecruiterType = "none"
)

// StageType identifies a standard interview stage.
type StageType string

const (
	// StageHR identifies a human resources interview.
	StageHR StageType = "HR"
	// StageTechnical identifies a technical interview.
	StageTechnical StageType = "Technical"
	// StageCultural identifies a culture interview.
	StageCultural StageType = "Cultural"
	// StageOfferDecision identifies an offer or decision stage.
	StageOfferDecision StageType = "Offer & Decision"
)

// StageStatus describes progress through an interview stage.
type StageStatus string

const (
	// StageStatusPending indicates that the stage has not started.
	StageStatusPending StageStatus = "pending"
	// StageStatusCurrent indicates that the stage is active.
	StageStatusCurrent StageStatus = "current"
	// StageStatusCompleted indicates that the stage has finished.
	StageStatusCompleted StageStatus = "completed"
	// StageStatusSkipped indicates that the stage will not take place.
	StageStatusSkipped StageStatus = "skipped"
)

// Interviewer represents an individual on an interview panel.
type Interviewer struct {
	Name  string `json:"name"`
	Role  string `json:"role"`
	Notes string `json:"notes,omitempty"`
}

// Job represents a single selection process.
type Job struct {
	ID               string          `json:"id"`
	CompanyName      string          `json:"company_name"`
	PositionTitle    string          `json:"position_title"`
	Status           JobStatus       `json:"status"`
	SalaryType       SalaryType      `json:"salary_type"`
	SalaryMin        *int64          `json:"salary_min"`
	SalaryMax        *int64          `json:"salary_max"`
	SalaryCurrency   string          `json:"salary_currency"`
	RecruiterType    RecruiterType   `json:"recruiter_type"`
	RecruiterName    *string         `json:"recruiter_name"`
	RecruiterAgency  *string         `json:"recruiter_agency"`
	RecruiterContact *string         `json:"recruiter_contact"`
	InterviewersJSON string          `json:"-"`
	JobPostURL       *string         `json:"job_post_url"`
	AvatarSeed       string          `json:"avatar_seed"`
	KeywordNote      string          `json:"keyword_note"`
	Description      string          `json:"description"`
	CompanyOverview  string          `json:"company_overview"`
	CompanyDomain    *string         `json:"company_domain"`
	InterviewNotes   string          `json:"interview_notes"`
	ReasonsToChange  string          `json:"reasons_to_change"`
	ExperienceNotes  string          `json:"experience_notes"`
	ExpectedSalary   string          `json:"expected_salary"`
	WorkArrangement  WorkArrangement `json:"work_arrangement"`
	EmploymentType   EmploymentType  `json:"employment_type"`
	IsReferral       bool            `json:"is_referral"`
	OrderIndex       int             `json:"order_index"`
	CreatedAt        int64           `json:"created_at"`
	UpdatedAt        int64           `json:"updated_at"`

	// Derived / populated fields
	CurrentStageTitle *string       `json:"current_stage_title,omitempty"`
	CurrentStageIndex *int          `json:"current_stage_index,omitempty"`
	TotalStagesCount  int           `json:"total_stages_count,omitempty"`
	Interviewers      []Interviewer `json:"interviewers,omitempty"`
	Stages            []Stage       `json:"stages,omitempty"`
	Attachments       []Attachment  `json:"attachments,omitempty"`
}

// UnmarshalJSON reads boolean flags written by both the current API and legacy SQLite snapshots.
func (job *Job) UnmarshalJSON(data []byte) error {
	type jobAlias Job
	var decoded struct {
		*jobAlias
		IsReferral json.RawMessage `json:"is_referral"`
	}
	decoded.jobAlias = (*jobAlias)(job)
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	value, err := decodeStoredBoolean(decoded.IsReferral)
	if err != nil {
		return fmt.Errorf("decode is_referral: %w", err)
	}
	job.IsReferral = value
	return nil
}

// Stage represents an individual interview round.
type Stage struct {
	ID               string        `json:"id"`
	JobID            string        `json:"job_id"`
	OrderIndex       int           `json:"order_index"`
	StageType        StageType     `json:"stage_type"`
	CustomTitle      *string       `json:"custom_title"`
	Description      string        `json:"description"`
	Status           StageStatus   `json:"status"`
	ScheduledAt      *int64        `json:"scheduled_at"`
	MeetingDate      *string       `json:"meeting_date"`
	MeetingTime      *string       `json:"meeting_time"`
	MeetingURL       *string       `json:"meeting_url"`
	MeetingType      string        `json:"meeting_type"`
	Notes            string        `json:"notes"`
	RecruiterName    *string       `json:"recruiter_name"`
	RecruiterType    RecruiterType `json:"recruiter_type"`
	RecruiterAgency  *string       `json:"recruiter_agency"`
	RecruiterContact *string       `json:"recruiter_contact"`
	InterviewersJSON string        `json:"-"`
	CreatedAt        int64         `json:"created_at"`

	// Derived
	Interviewers []Interviewer `json:"interviewers"`
	Questions    []Question    `json:"questions"`
}

// Question represents a candidate's prepared question for an interview round.
type Question struct {
	ID          string `json:"id"`
	StageID     string `json:"stage_id"`
	OrderIndex  int    `json:"order_index"`
	Question    string `json:"question"`
	AnswerNotes string `json:"answer_notes"`
	IsAsked     bool   `json:"is_asked"`
	CreatedAt   int64  `json:"created_at"`
}

// UnmarshalJSON reads question flags written as JSON booleans or legacy SQLite integers.
func (question *Question) UnmarshalJSON(data []byte) error {
	type questionAlias Question
	var decoded struct {
		*questionAlias
		IsAsked json.RawMessage `json:"is_asked"`
	}
	decoded.questionAlias = (*questionAlias)(question)
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	value, err := decodeStoredBoolean(decoded.IsAsked)
	if err != nil {
		return fmt.Errorf("decode is_asked: %w", err)
	}
	question.IsAsked = value
	return nil
}

func decodeStoredBoolean(value json.RawMessage) (bool, error) {
	if len(value) == 0 {
		return false, nil
	}
	var boolean bool
	if err := json.Unmarshal(value, &boolean); err == nil {
		return boolean, nil
	}
	var integer int
	if err := json.Unmarshal(value, &integer); err != nil || (integer != 0 && integer != 1) {
		return false, fmt.Errorf("expected true, false, 0, or 1; got %s", value)
	}
	return integer == 1, nil
}

// Attachment represents a local uploaded file.
type Attachment struct {
	ID             string  `json:"id"`
	JobID          string  `json:"job_id"`
	StageID        *string `json:"stage_id"`
	OriginalName   string  `json:"original_name"`
	StoredFilename string  `json:"stored_filename"`
	FileSize       int64   `json:"file_size"`
	MimeType       string  `json:"mime_type"`
	CreatedAt      int64   `json:"created_at"`
}

// JobCounts aggregates selection processes by status.
type JobCounts struct {
	All      int `json:"all"`
	Ongoing  int `json:"ongoing"`
	Accepted int `json:"accepted"`
	Rejected int `json:"rejected"`
}

// ScheduledMeeting represents an item on the candidate interview calendar.
type ScheduledMeeting struct {
	StageID          string  `json:"stage_id"`
	JobID            string  `json:"job_id"`
	OrderIndex       int     `json:"order_index"`
	StageType        string  `json:"stage_type"`
	CustomTitle      *string `json:"custom_title"`
	StageDescription string  `json:"stage_description"`
	StageStatus      string  `json:"stage_status"`
	MeetingDate      *string `json:"meeting_date"`
	MeetingTime      *string `json:"meeting_time"`
	MeetingURL       *string `json:"meeting_url"`
	MeetingType      string  `json:"meeting_type"`
	StageNotes       string  `json:"stage_notes"`
	CompanyName      string  `json:"company_name"`
	PositionTitle    string  `json:"position_title"`
	JobStatus        string  `json:"job_status"`
	RecruiterName    *string `json:"recruiter_name"`
	RecruiterContact *string `json:"recruiter_contact"`
	RecruiterAgency  *string `json:"recruiter_agency"`
	JobPostURL       *string `json:"job_post_url"`
	AvatarSeed       string  `json:"avatar_seed"`
}

// CreateJobInput contains fields accepted when creating a job.
type CreateJobInput struct {
	CompanyName         string           `json:"company_name"`
	PositionTitle       string           `json:"position_title"`
	Status              *JobStatus       `json:"status"`
	SalaryType          *SalaryType      `json:"salary_type"`
	SalaryMin           *int64           `json:"salary_min"`
	SalaryMax           *int64           `json:"salary_max"`
	SalaryCurrency      *string          `json:"salary_currency"`
	RecruiterType       *RecruiterType   `json:"recruiter_type"`
	RecruiterName       *string          `json:"recruiter_name"`
	RecruiterAgency     *string          `json:"recruiter_agency"`
	RecruiterContact    *string          `json:"recruiter_contact"`
	Interviewers        []Interviewer    `json:"interviewers"`
	JobPostURL          *string          `json:"job_post_url"`
	AvatarSeed          *string          `json:"avatar_seed"`
	KeywordNote         *string          `json:"keyword_note"`
	Description         *string          `json:"description"`
	CompanyOverview     *string          `json:"company_overview"`
	CompanyDomain       *string          `json:"company_domain"`
	InterviewNotes      *string          `json:"interview_notes"`
	ReasonsToChange     *string          `json:"reasons_to_change"`
	ExperienceNotes     *string          `json:"experience_notes"`
	ExpectedSalary      *string          `json:"expected_salary"`
	WorkArrangement     *WorkArrangement `json:"work_arrangement"`
	EmploymentType      *EmploymentType  `json:"employment_type"`
	IsReferral          *bool            `json:"is_referral"`
	CreateDefaultStages *bool            `json:"create_default_stages"`
}

// UpdateJobInput contains optional fields accepted when updating a job.
type UpdateJobInput struct {
	CompanyName      *string          `json:"company_name"`
	PositionTitle    *string          `json:"position_title"`
	Status           *JobStatus       `json:"status"`
	SalaryType       *SalaryType      `json:"salary_type"`
	SalaryMin        *int64           `json:"salary_min"`
	SalaryMax        *int64           `json:"salary_max"`
	SalaryCurrency   *string          `json:"salary_currency"`
	RecruiterType    *RecruiterType   `json:"recruiter_type"`
	RecruiterName    *string          `json:"recruiter_name"`
	RecruiterAgency  *string          `json:"recruiter_agency"`
	RecruiterContact *string          `json:"recruiter_contact"`
	Interviewers     []Interviewer    `json:"interviewers"`
	JobPostURL       *string          `json:"job_post_url"`
	AvatarSeed       *string          `json:"avatar_seed"`
	KeywordNote      *string          `json:"keyword_note"`
	Description      *string          `json:"description"`
	CompanyOverview  *string          `json:"company_overview"`
	CompanyDomain    *string          `json:"company_domain"`
	InterviewNotes   *string          `json:"interview_notes"`
	ReasonsToChange  *string          `json:"reasons_to_change"`
	ExperienceNotes  *string          `json:"experience_notes"`
	ExpectedSalary   *string          `json:"expected_salary"`
	WorkArrangement  *WorkArrangement `json:"work_arrangement"`
	EmploymentType   *EmploymentType  `json:"employment_type"`
	IsReferral       *bool            `json:"is_referral"`
	OrderIndex       *int             `json:"order_index"`
}

// CreateStageInput contains fields accepted when creating a stage.
type CreateStageInput struct {
	JobID            string         `json:"job_id"`
	StageType        StageType      `json:"stage_type"`
	CustomTitle      *string        `json:"custom_title"`
	Description      *string        `json:"description"`
	Notes            *string        `json:"notes"`
	RecruiterName    *string        `json:"recruiter_name"`
	RecruiterType    *RecruiterType `json:"recruiter_type"`
	RecruiterAgency  *string        `json:"recruiter_agency"`
	RecruiterContact *string        `json:"recruiter_contact"`
	Interviewers     []Interviewer  `json:"interviewers"`
}

// UpdateStageInput contains optional fields accepted when updating a stage.
type UpdateStageInput struct {
	CustomTitle      *string        `json:"custom_title"`
	Description      *string        `json:"description"`
	Status           *StageStatus   `json:"status"`
	Notes            *string        `json:"notes"`
	MeetingDate      *string        `json:"meeting_date"`
	MeetingTime      *string        `json:"meeting_time"`
	MeetingURL       *string        `json:"meeting_url"`
	MeetingType      *string        `json:"meeting_type"`
	RecruiterName    *string        `json:"recruiter_name"`
	RecruiterType    *RecruiterType `json:"recruiter_type"`
	RecruiterAgency  *string        `json:"recruiter_agency"`
	RecruiterContact *string        `json:"recruiter_contact"`
	Interviewers     []Interviewer  `json:"interviewers"`
}

// ScheduleMeetingInput contains the meeting details assigned to a stage.
type ScheduleMeetingInput struct {
	MeetingDate string  `json:"meeting_date"`
	MeetingTime string  `json:"meeting_time"`
	MeetingURL  *string `json:"meeting_url"`
	MeetingType *string `json:"meeting_type"`
}

// ReorderPayload contains ordered IDs for reorder operations.
type ReorderPayload struct {
	JobID       *string  `json:"job_id,omitempty"`
	StageIDs    []string `json:"stage_ids,omitempty"`
	JobIDs      []string `json:"job_ids,omitempty"`
	StageID     *string  `json:"stage_id,omitempty"`
	QuestionIDs []string `json:"question_ids,omitempty"`
}

// CreateQuestionInput contains fields accepted when creating a question.
type CreateQuestionInput struct {
	StageID     string `json:"stage_id"`
	Question    string `json:"question"`
	AnswerNotes string `json:"answer_notes,omitempty"`
}

// UpdateQuestionInput contains optional fields accepted when updating a question.
type UpdateQuestionInput struct {
	Question    *string `json:"question"`
	AnswerNotes *string `json:"answer_notes"`
	IsAsked     *bool   `json:"is_asked"`
}
