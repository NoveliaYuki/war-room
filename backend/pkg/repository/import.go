package repository

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"war-room/backend/pkg/models"
)

type importStatements struct {
	job        *sql.Stmt
	stage      *sql.Stmt
	question   *sql.Stmt
	attachment *sql.Stmt
}

// ReplaceAllJobs atomically replaces local process data with an imported archive.
func (r *Repository) ReplaceAllJobs(jobs []models.Job) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec("DELETE FROM jobs"); err != nil {
		return fmt.Errorf("clear existing processes: %w", err)
	}
	statements, err := prepareImportStatements(tx)
	if err != nil {
		return err
	}
	defer statements.close()
	for _, job := range jobs {
		if err := insertImportedJob(statements, job); err != nil {
			return fmt.Errorf("import process %q: %w", job.ID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit imported processes: %w", err)
	}
	return nil
}

func prepareImportStatements(tx *sql.Tx) (*importStatements, error) {
	job, err := tx.Prepare(`INSERT INTO jobs (
		id, company_name, position_title, status, salary_type, salary_min, salary_max,
		salary_currency, recruiter_type, recruiter_name, recruiter_agency, recruiter_contact,
		interviewers_json, job_post_url, avatar_seed, keyword_note, description, company_overview,
		company_domain, interview_notes, reasons_to_change, experience_notes, expected_salary,
		work_arrangement, employment_type, is_referral, order_index, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return nil, err
	}
	statements := &importStatements{job: job}
	if statements.stage, err = tx.Prepare(`INSERT INTO stages (
		id, job_id, order_index, stage_type, custom_title, description, status,
		scheduled_at, meeting_date, meeting_time, meeting_url, meeting_type, notes,
		recruiter_name, recruiter_type, recruiter_agency, recruiter_contact, interviewers_json, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`); err != nil {
		statements.close()
		return nil, err
	}
	if statements.question, err = tx.Prepare(`INSERT INTO stage_questions (
		id, stage_id, order_index, question, answer_notes, is_asked, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?)`); err != nil {
		statements.close()
		return nil, err
	}
	if statements.attachment, err = tx.Prepare(`INSERT INTO attachments (
		id, job_id, stage_id, original_name, stored_filename, absolute_path, file_size, mime_type, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`); err != nil {
		statements.close()
		return nil, err
	}
	return statements, nil
}

func (s *importStatements) close() {
	if s.job != nil {
		_ = s.job.Close()
	}
	if s.stage != nil {
		_ = s.stage.Close()
	}
	if s.question != nil {
		_ = s.question.Close()
	}
	if s.attachment != nil {
		_ = s.attachment.Close()
	}
}

func insertImportedJob(statements *importStatements, job models.Job) error {
	interviewers, err := marshalInterviewers(job.Interviewers)
	if err != nil {
		return err
	}
	if _, err := statements.job.Exec(job.ID, job.CompanyName, job.PositionTitle, job.Status, defaultValue(string(job.SalaryType), "unknown"), job.SalaryMin, job.SalaryMax,
		defaultValue(job.SalaryCurrency, "EUR"), defaultValue(string(job.RecruiterType), "none"), job.RecruiterName, job.RecruiterAgency, job.RecruiterContact,
		interviewers, job.JobPostURL, job.AvatarSeed, job.KeywordNote, job.Description, job.CompanyOverview, job.CompanyDomain,
		job.InterviewNotes, job.ReasonsToChange, job.ExperienceNotes, job.ExpectedSalary, defaultValue(string(job.WorkArrangement), "unknown"),
		defaultValue(string(job.EmploymentType), "unknown"), job.IsReferral, job.OrderIndex, job.CreatedAt, job.UpdatedAt); err != nil {
		return err
	}
	for _, stage := range job.Stages {
		if err := insertImportedStage(statements, stage, job.ID); err != nil {
			return err
		}
	}
	for _, attachment := range job.Attachments {
		if _, err := statements.attachment.Exec(attachment.ID, job.ID, attachment.StageID, attachment.OriginalName, attachment.StoredFilename,
			attachment.StoredFilename, attachment.FileSize, defaultValue(attachment.MimeType, "application/octet-stream"), attachment.CreatedAt); err != nil {
			return fmt.Errorf("import attachment %q: %w", attachment.ID, err)
		}
	}
	return nil
}

func insertImportedStage(statements *importStatements, stage models.Stage, jobID string) error {
	interviewers, err := marshalInterviewers(stage.Interviewers)
	if err != nil {
		return err
	}
	if _, err := statements.stage.Exec(stage.ID, jobID, stage.OrderIndex, stage.StageType, stage.CustomTitle, stage.Description, stage.Status,
		stage.ScheduledAt, stage.MeetingDate, stage.MeetingTime, stage.MeetingURL, defaultValue(stage.MeetingType, "video"), stage.Notes,
		stage.RecruiterName, defaultValue(string(stage.RecruiterType), "none"), stage.RecruiterAgency, stage.RecruiterContact, interviewers, stage.CreatedAt); err != nil {
		return fmt.Errorf("import interview stage %q: %w", stage.ID, err)
	}
	for _, question := range stage.Questions {
		if _, err := statements.question.Exec(question.ID, stage.ID, question.OrderIndex, question.Question, question.AnswerNotes, question.IsAsked, question.CreatedAt); err != nil {
			return fmt.Errorf("import interview question %q: %w", question.ID, err)
		}
	}
	return nil
}

func marshalInterviewers(interviewers []models.Interviewer) (string, error) {
	if interviewers == nil {
		return "[]", nil
	}
	data, err := json.Marshal(interviewers)
	if err != nil {
		return "", err
	}
	if len(data) == 0 {
		return "[]", nil
	}
	return string(data), nil
}
