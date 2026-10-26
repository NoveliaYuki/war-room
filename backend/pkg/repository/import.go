package repository

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"war-room/backend/pkg/models"
)

type importStatements struct {
	job        *sql.Stmt
	technology *sql.Stmt
	stage      *sql.Stmt
	question   *sql.Stmt
	attachment *sql.Stmt
}

// ReplaceAllJobs atomically replaces userID's local process data with an imported archive.
func (r *Repository) ReplaceAllJobs(userID string, jobs []models.Job) error {
	return r.ReplaceAllJobsWithCV(userID, jobs, nil, false, 1)
}

// ReplaceAllJobsWithCV replaces userID's jobs and, for a current-format backup, their CV library.
func (r *Repository) ReplaceAllJobsWithCV(userID string, jobs []models.Job, versions []models.CVVersion, replaceCV bool, nextCVVersion int) error {
	return r.ReplaceAllWithTechnologyCatalog(userID, jobs, nil, versions, replaceCV, nextCVVersion)
}

// ReplaceAllWithTechnologyCatalog atomically replaces userID's jobs, their technology catalog, and optional CV data.
func (r *Repository) ReplaceAllWithTechnologyCatalog(userID string, jobs []models.Job, technologies []models.Technology, versions []models.CVVersion, replaceCV bool, nextCVVersion int) error {
	return r.ReplaceAllWithSearchPeriods(userID, jobs, nil, technologies, versions, replaceCV, nextCVVersion)
}

// ReplaceAllWithSearchPeriods atomically replaces userID's jobs, periods, technologies, and optional CV data.
func (r *Repository) ReplaceAllWithSearchPeriods(userID string, jobs []models.Job, periods []models.SearchPeriod, technologies []models.Technology, versions []models.CVVersion, replaceCV bool, nextCVVersion int) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := clearImportedData(tx, userID); err != nil {
		return err
	}
	if replaceCV {
		if err := replaceImportedCVLibrary(tx, userID, versions, nextCVVersion); err != nil {
			return err
		}
	}
	if err := insertImportedTechnologies(tx, userID, technologies); err != nil {
		return err
	}
	if err := insertImportedSearchPeriods(tx, userID, periods); err != nil {
		return err
	}
	statements, err := prepareImportStatements(tx)
	if err != nil {
		return err
	}
	defer statements.close()
	if err := insertImportedJobs(tx, userID, statements, jobs); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit imported processes: %w", err)
	}
	return nil
}

// clearImportedData removes only userID's own jobs, technologies, and search
// periods, so importing one account's backup never touches another
// account's data.
func clearImportedData(tx *sql.Tx, userID string) error {
	if _, err := tx.Exec("DELETE FROM jobs WHERE user_id = ?", userID); err != nil {
		return fmt.Errorf("clear existing processes: %w", err)
	}
	if _, err := tx.Exec("DELETE FROM technologies WHERE user_id = ?", userID); err != nil {
		return fmt.Errorf("clear technology catalog: %w", err)
	}
	if _, err := tx.Exec("DELETE FROM search_periods WHERE user_id = ?", userID); err != nil {
		return fmt.Errorf("clear search periods: %w", err)
	}
	return nil
}

func insertImportedSearchPeriods(tx *sql.Tx, userID string, periods []models.SearchPeriod) error {
	for _, period := range periods {
		if _, err := tx.Exec(`INSERT INTO search_periods (id, user_id, name, start_date, end_date, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			period.ID, userID, period.Name, period.StartDate, period.EndDate, period.CreatedAt, period.UpdatedAt); err != nil {
			return fmt.Errorf("import search period %q: %w", period.ID, err)
		}
	}
	return nil
}

func insertImportedTechnologies(tx *sql.Tx, userID string, technologies []models.Technology) error {
	for _, item := range technologies {
		if _, err := tx.Exec(`INSERT INTO technologies (id, user_id, name, normalized_name) VALUES (?, ?, ?, ?)`, item.ID, userID, item.Name, normalizeCatalogName(item.Name)); err != nil {
			return fmt.Errorf("import technology %q: %w", item.Name, err)
		}
		if err := insertImportedTechnologyAliases(tx, userID, item); err != nil {
			return err
		}
	}
	return nil
}

func insertImportedTechnologyAliases(tx *sql.Tx, userID string, item models.Technology) error {
	for _, alias := range item.Aliases {
		if _, err := tx.Exec(`INSERT INTO technology_aliases (technology_id, user_id, alias, normalized_alias) VALUES (?, ?, ?, ?)`, item.ID, userID, alias, normalizeCatalogName(alias)); err != nil {
			return fmt.Errorf("import technology alias %q: %w", alias, err)
		}
	}
	return nil
}

// replaceImportedCVLibrary replaces userID's CV versions and advances their
// sequence counter to at least nextVersion (and past every imported
// version), mirroring the upsert pattern user_claim.go's
// claimCVVersionSequence uses: the counter is never lowered, only raised, so
// the next upload after an import cannot collide with a version the import
// just restored.
func replaceImportedCVLibrary(tx *sql.Tx, userID string, versions []models.CVVersion, nextVersion int) error {
	if _, err := tx.Exec("DELETE FROM cv_versions WHERE user_id = ?", userID); err != nil {
		return fmt.Errorf("clear existing CV versions: %w", err)
	}
	for _, version := range versions {
		if err := insertImportedCVVersion(tx, userID, version); err != nil {
			return err
		}
	}
	maxNext := maxCVVersion(versions, nextVersion)
	if _, err := tx.Exec(`
		INSERT INTO cv_version_sequence (user_id, next_version) VALUES (?, ?)
		ON CONFLICT(user_id) DO UPDATE SET next_version = MAX(next_version, excluded.next_version)
	`, userID, maxNext); err != nil {
		return fmt.Errorf("reset CV version sequence: %w", err)
	}
	return nil
}

func insertImportedCVVersion(tx *sql.Tx, userID string, version models.CVVersion) error {
	_, err := tx.Exec(`INSERT INTO cv_versions (id, user_id, version, original_name, stored_filename, file_size, mime_type, sha256, uploaded_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		version.ID, userID, version.Version, version.OriginalName, version.StoredFilename, version.FileSize, defaultValue(version.MimeType, "application/octet-stream"), version.SHA256, version.UploadedAt)
	if err != nil {
		return fmt.Errorf("import CV version %d: %w", version.Version, err)
	}
	return nil
}

func maxCVVersion(versions []models.CVVersion, nextVersion int) int {
	maxNext := 1
	for _, version := range versions {
		if version.Version >= maxNext {
			maxNext = version.Version + 1
		}
	}
	if nextVersion > maxNext {
		maxNext = nextVersion
	}
	return maxNext
}

func insertImportedJobs(tx *sql.Tx, userID string, statements *importStatements, jobs []models.Job) error {
	for _, job := range jobs {
		if err := insertImportedJob(statements, userID, job); err != nil {
			return fmt.Errorf("import process %q: %w", job.ID, err)
		}
		if len(job.History) > 0 {
			if _, err := tx.Exec(`DELETE FROM job_history WHERE job_id = ?`, job.ID); err != nil {
				return fmt.Errorf("replace history for process %q: %w", job.ID, err)
			}
			for _, item := range job.History {
				if item.OccurredAt <= 0 || item.Action == "" || len(item.Action) > 160 {
					return fmt.Errorf("import process %q: invalid history entry", job.ID)
				}
				if _, err := tx.Exec(`INSERT INTO job_history (job_id, occurred_at, action) VALUES (?, ?, ?)`, job.ID, item.OccurredAt, item.Action); err != nil {
					return fmt.Errorf("import history for process %q: %w", job.ID, err)
				}
			}
		}
	}
	return restoreImportedActivity(tx, jobs)
}

func restoreImportedActivity(tx *sql.Tx, jobs []models.Job) error {
	// Child-row insert triggers count as activity; restore the snapshot timestamp last.
	for _, job := range jobs {
		if _, err := tx.Exec(`UPDATE jobs SET last_activity_at = ? WHERE id = ?`, importedJobActivityTimestamp(job), job.ID); err != nil {
			return fmt.Errorf("restore process activity %q: %w", job.ID, err)
		}
	}
	return nil
}

func prepareImportStatements(tx *sql.Tx) (*importStatements, error) {
	job, err := tx.Prepare(`INSERT INTO jobs (
		id, user_id, company_name, position_title, status, salary_type, salary_min, salary_max,
		salary_currency, recruiter_type, recruiter_name, recruiter_agency, recruiter_contact,
		interviewers_json, job_post_url, application_url, avatar_seed, keyword_note, description, company_overview,
		company_domain, interview_notes, reasons_to_change, experience_notes, expected_salary,
		work_arrangement, employment_type, is_referral, order_index, status_changed_at,
		application_sent_date, recruiter_first_contact_date, created_at, updated_at, last_activity_at, cv_version_id, search_period_id
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return nil, err
	}
	statements := &importStatements{job: job}
	if statements.technology, err = tx.Prepare(`INSERT INTO job_technologies (job_id, technology_id, user_id) VALUES (?, ?, ?)`); err != nil {
		statements.close()
		return nil, err
	}
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
	if s.technology != nil {
		_ = s.technology.Close()
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

func insertImportedJob(statements *importStatements, userID string, job models.Job) error {
	interviewers := marshalInterviewers(job.Interviewers)
	statusChangedAt := job.StatusChangedAt
	if statusChangedAt == 0 {
		statusChangedAt = job.CreatedAt
	}
	lastActivityAt := importedJobActivityTimestamp(job)
	var cvVersionID any
	if job.SelectedCVVersion != nil {
		cvVersionID = job.SelectedCVVersion.ID
	}
	if _, err := statements.job.Exec(job.ID, userID, job.CompanyName, job.PositionTitle, job.Status, defaultValue(string(job.SalaryType), "unknown"), job.SalaryMin, job.SalaryMax,
		defaultValue(job.SalaryCurrency, "EUR"), defaultValue(string(job.RecruiterType), "none"), job.RecruiterName, job.RecruiterAgency, job.RecruiterContact,
		interviewers, job.JobPostURL, job.ApplicationURL, job.AvatarSeed, job.KeywordNote, job.Description, job.CompanyOverview, job.CompanyDomain,
		job.InterviewNotes, job.ReasonsToChange, job.ExperienceNotes, job.ExpectedSalary, defaultValue(string(job.WorkArrangement), "unknown"),
		defaultValue(string(job.EmploymentType), "unknown"), job.IsReferral, job.OrderIndex, statusChangedAt,
		job.ApplicationSentDate, job.RecruiterFirstContactDate, job.CreatedAt, job.UpdatedAt, lastActivityAt, cvVersionID, job.SearchPeriodID); err != nil {
		return err
	}
	for _, technology := range job.Technologies {
		if _, err := statements.technology.Exec(job.ID, technology.ID, userID); err != nil {
			return fmt.Errorf("import technology assignment %q: %w", technology.ID, err)
		}
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

func importedJobActivityTimestamp(job models.Job) int64 {
	if job.LastActivityAt != 0 {
		return job.LastActivityAt
	}
	lastActivityAt := max(job.UpdatedAt, job.CreatedAt, job.StatusChangedAt)
	if lastActivityAt != 0 {
		return lastActivityAt
	}
	return time.Now().Unix()
}

func insertImportedStage(statements *importStatements, stage models.Stage, jobID string) error {
	interviewers := marshalInterviewers(stage.Interviewers)
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

func marshalInterviewers(interviewers []models.Interviewer) string {
	if interviewers == nil {
		return "[]"
	}
	data, _ := json.Marshal(interviewers)
	return string(data)
}
