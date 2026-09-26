// Package repository persists application models in SQLite.
package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"war-room/backend/pkg/models"
)

type readQueryer interface {
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

// Repository provides database operations for jobs and related records.
type Repository struct {
	db     *sql.DB
	reader readQueryer
}

// New creates a repository backed by db.
func New(db *sql.DB) *Repository {
	return &Repository{db: db, reader: db}
}

// WithReadSnapshot runs read operations against one consistent database snapshot.
func (r *Repository) WithReadSnapshot(read func(*Repository) error) error {
	tx, err := r.db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	snapshot := &Repository{db: r.db, reader: tx}
	if err := read(snapshot); err != nil {
		return err
	}
	return tx.Commit()
}

// GetAllJobs returns jobs matching the optional status and search filters.
func (r *Repository) GetAllJobs(status, search string) ([]models.Job, error) {
	query := `
		SELECT
			jobs.id, jobs.company_name, jobs.position_title, jobs.status, jobs.salary_type,
			jobs.salary_min, jobs.salary_max, jobs.salary_currency, jobs.recruiter_type,
			jobs.recruiter_name, jobs.recruiter_agency, jobs.recruiter_contact,
			jobs.interviewers_json, jobs.job_post_url, jobs.avatar_seed, jobs.keyword_note,
			jobs.description, jobs.company_overview, jobs.company_domain, jobs.interview_notes,
			jobs.reasons_to_change, jobs.experience_notes, jobs.expected_salary, jobs.work_arrangement,
			jobs.employment_type, jobs.is_referral, jobs.order_index, jobs.created_at, jobs.updated_at,
			(SELECT COALESCE(custom_title, stage_type) FROM stages WHERE job_id = jobs.id AND status = 'current' ORDER BY order_index, id LIMIT 1) as current_stage_title,
			(SELECT order_index + 1 FROM stages WHERE job_id = jobs.id AND status = 'current' ORDER BY order_index, id LIMIT 1) as current_stage_index,
			(SELECT COUNT(*) FROM stages WHERE job_id = jobs.id) as total_stages_count
		FROM jobs
		WHERE 1=1
	`
	var args []interface{}

	if status != "" && status != "all" {
		query += " AND status = ?"
		args = append(args, status)
	}

	if searchTrim := strings.TrimSpace(search); searchTrim != "" {
		wildcard := "%" + searchTrim + "%"
		query += " AND (company_name LIKE ? OR position_title LIKE ? OR keyword_note LIKE ? OR recruiter_name LIKE ? OR (is_referral = 1 AND 'referral' LIKE ?))"
		args = append(args, wildcard, wildcard, wildcard, wildcard, wildcard)
	}

	query += " ORDER BY order_index ASC, updated_at DESC, created_at DESC"

	rows, err := r.reader.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var jobs []models.Job
	for rows.Next() {
		var j models.Job
		var ijJSON sql.NullString
		err := rows.Scan(
			&j.ID, &j.CompanyName, &j.PositionTitle, &j.Status, &j.SalaryType,
			&j.SalaryMin, &j.SalaryMax, &j.SalaryCurrency, &j.RecruiterType,
			&j.RecruiterName, &j.RecruiterAgency, &j.RecruiterContact,
			&ijJSON, &j.JobPostURL, &j.AvatarSeed, &j.KeywordNote,
			&j.Description, &j.CompanyOverview, &j.CompanyDomain, &j.InterviewNotes,
			&j.ReasonsToChange, &j.ExperienceNotes, &j.ExpectedSalary, &j.WorkArrangement,
			&j.EmploymentType, &j.IsReferral, &j.OrderIndex, &j.CreatedAt, &j.UpdatedAt,
			&j.CurrentStageTitle, &j.CurrentStageIndex, &j.TotalStagesCount,
		)
		if err != nil {
			return nil, err
		}
		if ijJSON.Valid {
			j.InterviewersJSON = ijJSON.String
			if err := json.Unmarshal([]byte(ijJSON.String), &j.Interviewers); err != nil {
				return nil, fmt.Errorf("decode job interviewers: %w", err)
			}
		}
		jobs = append(jobs, j)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return jobs, nil
}

// GetJobByID returns the job identified by id.
func (r *Repository) GetJobByID(id string) (*models.Job, error) {
	row := r.reader.QueryRow(`
		SELECT
			id, company_name, position_title, status, salary_type, salary_min, salary_max,
			salary_currency, recruiter_type, recruiter_name, recruiter_agency, recruiter_contact,
			interviewers_json, job_post_url, avatar_seed, keyword_note, description, company_overview,
			company_domain, interview_notes, reasons_to_change, experience_notes, expected_salary, work_arrangement,
			employment_type, is_referral, order_index, created_at, updated_at
		FROM jobs WHERE id = ?
	`, id)

	var j models.Job
	var ijJSON sql.NullString
	err := row.Scan(
		&j.ID, &j.CompanyName, &j.PositionTitle, &j.Status, &j.SalaryType,
		&j.SalaryMin, &j.SalaryMax, &j.SalaryCurrency, &j.RecruiterType,
		&j.RecruiterName, &j.RecruiterAgency, &j.RecruiterContact,
		&ijJSON, &j.JobPostURL, &j.AvatarSeed, &j.KeywordNote,
		&j.Description, &j.CompanyOverview, &j.CompanyDomain, &j.InterviewNotes,
		&j.ReasonsToChange, &j.ExperienceNotes, &j.ExpectedSalary, &j.WorkArrangement,
		&j.EmploymentType, &j.IsReferral, &j.OrderIndex, &j.CreatedAt, &j.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if ijJSON.Valid {
		j.InterviewersJSON = ijJSON.String
		if err := json.Unmarshal([]byte(ijJSON.String), &j.Interviewers); err != nil {
			return nil, fmt.Errorf("decode job interviewers: %w", err)
		}
	}
	return &j, nil
}

// GetJobCounts returns the number of jobs in each status.
func (r *Repository) GetJobCounts() (*models.JobCounts, error) {
	row := r.reader.QueryRow(`
		SELECT
			COUNT(*) as total,
			COALESCE(SUM(CASE WHEN status = 'ongoing' THEN 1 ELSE 0 END), 0) as ongoing,
			COALESCE(SUM(CASE WHEN status = 'accepted' THEN 1 ELSE 0 END), 0) as accepted,
			COALESCE(SUM(CASE WHEN status = 'rejected' THEN 1 ELSE 0 END), 0) as rejected
		FROM jobs
	`)
	var counts models.JobCounts
	if err := row.Scan(&counts.All, &counts.Ongoing, &counts.Accepted, &counts.Rejected); err != nil {
		return nil, err
	}
	return &counts, nil
}

// InsertJob persists a new job and allocates an order when needed.
func (r *Repository) InsertJob(job *models.Job) error {
	if job.WorkArrangement == "" {
		job.WorkArrangement = models.WorkArrangementUnknown
	}
	if job.EmploymentType == "" {
		job.EmploymentType = models.EmploymentTypeUnknown
	}
	if job.OrderIndex == 0 {
		var nextOrder int
		if err := r.db.QueryRow("SELECT COALESCE(MAX(order_index), -1) + 1 FROM jobs").Scan(&nextOrder); err != nil {
			return fmt.Errorf("allocate job order: %w", err)
		}
		job.OrderIndex = nextOrder
	}
	now := time.Now().Unix()
	job.CreatedAt = now
	job.UpdatedAt = now

	ijBytes, _ := json.Marshal(job.Interviewers)
	job.InterviewersJSON = string(ijBytes)

	_, err := r.db.Exec(`
		INSERT INTO jobs (
			id, company_name, position_title, status, salary_type, salary_min, salary_max,
			salary_currency, recruiter_type, recruiter_name, recruiter_agency, recruiter_contact,
			interviewers_json, job_post_url, avatar_seed, keyword_note, description, company_overview,
			company_domain, interview_notes, reasons_to_change, experience_notes, expected_salary, work_arrangement,
			employment_type, is_referral, order_index, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		job.ID, job.CompanyName, job.PositionTitle, job.Status, job.SalaryType,
		job.SalaryMin, job.SalaryMax, job.SalaryCurrency, job.RecruiterType,
		job.RecruiterName, job.RecruiterAgency, job.RecruiterContact,
		job.InterviewersJSON, job.JobPostURL, job.AvatarSeed, job.KeywordNote,
		job.Description, job.CompanyOverview, job.CompanyDomain, job.InterviewNotes,
		job.ReasonsToChange, job.ExperienceNotes, job.ExpectedSalary,
		job.WorkArrangement, job.EmploymentType, job.IsReferral, job.OrderIndex, job.CreatedAt, job.UpdatedAt,
	)
	return err
}

// UpdateJob updates the fields selected by the service layer.
func (r *Repository) UpdateJob(id string, fields map[string]interface{}) error {
	allowed := map[string]bool{
		"company_name": true, "position_title": true, "status": true, "salary_type": true,
		"salary_min": true, "salary_max": true, "salary_currency": true, "recruiter_type": true,
		"recruiter_name": true, "recruiter_agency": true, "recruiter_contact": true,
		"interviewers_json": true, "job_post_url": true, "avatar_seed": true, "keyword_note": true,
		"description": true, "company_overview": true, "company_domain": true,
		"interview_notes": true, "reasons_to_change": true, "experience_notes": true,
		"expected_salary": true, "work_arrangement": true, "employment_type": true,
		"is_referral": true, "order_index": true,
	}

	var clauses []string
	var args []interface{}

	for k, v := range fields {
		if !allowed[k] {
			continue
		}
		clauses = append(clauses, fmt.Sprintf("%s = ?", k))
		args = append(args, normalizeJobUpdateValue(k, v))
	}

	if len(clauses) == 0 {
		return nil
	}

	clauses = append(clauses, "updated_at = unixepoch()")
	// Column names in clauses come only from the allowlist above; values remain parameterized.
	query := fmt.Sprintf("UPDATE jobs SET %s WHERE id = ?", strings.Join(clauses, ", ")) // #nosec G201
	args = append(args, id)

	res, err := r.db.Exec(query, args...)
	if err != nil {
		return err
	}
	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func normalizeJobUpdateValue(key string, value interface{}) interface{} {
	switch key {
	case "keyword_note":
		if text, ok := value.(string); ok && len(text) > 100 {
			return text[:100]
		}
	}
	return value
}

// DeleteJob removes the job identified by id.
func (r *Repository) DeleteJob(id string) error {
	res, err := r.db.Exec("DELETE FROM jobs WHERE id = ?", id)
	if err != nil {
		return err
	}
	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ReorderJobs stores the supplied job ordering.
func (r *Repository) ReorderJobs(jobIDs []string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.Prepare("UPDATE jobs SET order_index = ?, updated_at = unixepoch() WHERE id = ?")
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()

	seenIDs := make(map[string]struct{}, len(jobIDs))
	for idx, id := range jobIDs {
		if _, exists := seenIDs[id]; exists {
			return fmt.Errorf("duplicate job id %q", id)
		}
		seenIDs[id] = struct{}{}
		result, err := stmt.Exec(idx, id)
		if err != nil {
			return err
		}
		if err := requireOneUpdatedRow(result, "job", id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func requireOneUpdatedRow(result sql.Result, kind, id string) error {
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return fmt.Errorf("%s %q was not found in reorder scope", kind, id)
	}
	return nil
}

// Stages

// GetStagesByJobID returns the stages belonging to a job.
func (r *Repository) GetStagesByJobID(jobID string) ([]models.Stage, error) {
	return r.getStages(&jobID)
}

// GetAllStages returns every stage in stable job and stage order.
func (r *Repository) GetAllStages() ([]models.Stage, error) {
	return r.getStages(nil)
}

func (r *Repository) getStages(jobID *string) ([]models.Stage, error) {
	query := `
		SELECT
			id, job_id, order_index, stage_type, custom_title, description, status,
			scheduled_at, meeting_date, meeting_time, meeting_url, meeting_type, notes,
			recruiter_name, recruiter_type, recruiter_agency, recruiter_contact,
			interviewers_json, created_at
		FROM stages`
	var args []interface{}
	if jobID != nil {
		query += " WHERE job_id = ?"
		args = append(args, *jobID)
	}
	query += " ORDER BY job_id, order_index, id"
	rows, err := r.reader.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var stages []models.Stage
	for rows.Next() {
		var s models.Stage
		var ijJSON sql.NullString
		err := rows.Scan(
			&s.ID, &s.JobID, &s.OrderIndex, &s.StageType, &s.CustomTitle, &s.Description,
			&s.Status, &s.ScheduledAt, &s.MeetingDate, &s.MeetingTime, &s.MeetingURL,
			&s.MeetingType, &s.Notes, &s.RecruiterName, &s.RecruiterType, &s.RecruiterAgency,
			&s.RecruiterContact, &ijJSON, &s.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		if ijJSON.Valid {
			s.InterviewersJSON = ijJSON.String
			if err := json.Unmarshal([]byte(ijJSON.String), &s.Interviewers); err != nil {
				return nil, fmt.Errorf("decode stage interviewers: %w", err)
			}
		}
		stages = append(stages, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return stages, nil
}

// GetStageByID returns the stage identified by id.
func (r *Repository) GetStageByID(id string) (*models.Stage, error) {
	row := r.reader.QueryRow(`
		SELECT
			id, job_id, order_index, stage_type, custom_title, description, status,
			scheduled_at, meeting_date, meeting_time, meeting_url, meeting_type, notes,
			recruiter_name, recruiter_type, recruiter_agency, recruiter_contact,
			interviewers_json, created_at
		FROM stages WHERE id = ?
	`, id)

	var s models.Stage
	var ijJSON sql.NullString
	err := row.Scan(
		&s.ID, &s.JobID, &s.OrderIndex, &s.StageType, &s.CustomTitle, &s.Description,
		&s.Status, &s.ScheduledAt, &s.MeetingDate, &s.MeetingTime, &s.MeetingURL,
		&s.MeetingType, &s.Notes, &s.RecruiterName, &s.RecruiterType, &s.RecruiterAgency,
		&s.RecruiterContact, &ijJSON, &s.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if ijJSON.Valid {
		s.InterviewersJSON = ijJSON.String
		if err := json.Unmarshal([]byte(ijJSON.String), &s.Interviewers); err != nil {
			return nil, fmt.Errorf("decode stage interviewers: %w", err)
		}
	}
	return &s, nil
}

// InsertStage persists a new stage.
func (r *Repository) InsertStage(stage *models.Stage) error {
	now := time.Now().Unix()
	stage.CreatedAt = now
	ijBytes, _ := json.Marshal(stage.Interviewers)
	stage.InterviewersJSON = string(ijBytes)

	_, err := r.db.Exec(`
		INSERT INTO stages (
			id, job_id, order_index, stage_type, custom_title, description, status,
			scheduled_at, meeting_date, meeting_time, meeting_url, meeting_type, notes,
			recruiter_name, recruiter_type, recruiter_agency, recruiter_contact,
			interviewers_json, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		stage.ID, stage.JobID, stage.OrderIndex, stage.StageType, stage.CustomTitle,
		stage.Description, stage.Status, stage.ScheduledAt, stage.MeetingDate,
		stage.MeetingTime, stage.MeetingURL, stage.MeetingType, stage.Notes,
		stage.RecruiterName, stage.RecruiterType, stage.RecruiterAgency,
		stage.RecruiterContact, stage.InterviewersJSON, stage.CreatedAt,
	)
	return err
}

// UpdateStage updates the fields selected by the service layer.
func (r *Repository) UpdateStage(id string, fields map[string]interface{}) error {
	allowed := map[string]bool{
		"order_index": true, "stage_type": true, "custom_title": true, "description": true,
		"status": true, "scheduled_at": true, "meeting_date": true, "meeting_time": true,
		"meeting_url": true, "meeting_type": true, "notes": true, "recruiter_name": true,
		"recruiter_type": true, "recruiter_agency": true, "recruiter_contact": true,
		"interviewers_json": true,
	}

	var clauses []string
	var args []interface{}

	for k, v := range fields {
		if !allowed[k] {
			continue
		}
		clauses = append(clauses, fmt.Sprintf("%s = ?", k))
		args = append(args, v)
	}

	if len(clauses) == 0 {
		return nil
	}

	// Column names in clauses come only from the allowlist above; values remain parameterized.
	query := fmt.Sprintf("UPDATE stages SET %s WHERE id = ?", strings.Join(clauses, ", ")) // #nosec G201
	args = append(args, id)

	res, err := r.db.Exec(query, args...)
	if err != nil {
		return err
	}
	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteStage removes the stage identified by id.
func (r *Repository) DeleteStage(id string) error {
	res, err := r.db.Exec("DELETE FROM stages WHERE id = ?", id)
	if err != nil {
		return err
	}
	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ReorderStages stores the supplied ordering for a job's stages.
func (r *Repository) ReorderStages(jobID string, stageIDs []string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.Prepare("UPDATE stages SET order_index = ? WHERE id = ? AND job_id = ?")
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()

	seenIDs := make(map[string]struct{}, len(stageIDs))
	for idx, id := range stageIDs {
		if _, exists := seenIDs[id]; exists {
			return fmt.Errorf("duplicate stage id %q", id)
		}
		seenIDs[id] = struct{}{}
		result, err := stmt.Exec(idx, id, jobID)
		if err != nil {
			return err
		}
		if err := requireOneUpdatedRow(result, "stage", id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SetCurrentStage updates stage statuses relative to the selected stage.
func (r *Repository) SetCurrentStage(stageID string) ([]models.Stage, error) {
	stage, err := r.GetStageByID(stageID)
	if err != nil || stage == nil {
		return nil, fmt.Errorf("stage not found: %s", stageID)
	}

	tx, err := r.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.Exec(`
		UPDATE stages
		SET status = CASE
			WHEN order_index < ? THEN 'completed'
			WHEN id = ? THEN 'current'
			ELSE 'pending'
		END
		WHERE job_id = ?
	`, stage.OrderIndex, stage.ID, stage.JobID)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return r.GetStagesByJobID(stage.JobID)
}

// Questions

// GetQuestionsByStageID returns the questions belonging to a stage.
func (r *Repository) GetQuestionsByStageID(stageID string) ([]models.Question, error) {
	return r.getQuestions(&stageID)
}

// GetAllQuestions returns all questions in stable stage and question order.
func (r *Repository) GetAllQuestions() ([]models.Question, error) {
	return r.getQuestions(nil)
}

func (r *Repository) getQuestions(stageID *string) ([]models.Question, error) {
	query := `
		SELECT id, stage_id, order_index, question, answer_notes, is_asked, created_at
		FROM stage_questions`
	var args []interface{}
	if stageID != nil {
		query += " WHERE stage_id = ?"
		args = append(args, *stageID)
	}
	query += " ORDER BY stage_id, order_index, id"
	rows, err := r.reader.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var questions []models.Question
	for rows.Next() {
		var q models.Question
		if err := rows.Scan(&q.ID, &q.StageID, &q.OrderIndex, &q.Question, &q.AnswerNotes, &q.IsAsked, &q.CreatedAt); err != nil {
			return nil, err
		}
		questions = append(questions, q)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return questions, nil
}

// GetQuestionByID returns the question identified by id.
func (r *Repository) GetQuestionByID(id string) (*models.Question, error) {
	row := r.db.QueryRow(`
		SELECT id, stage_id, order_index, question, answer_notes, is_asked, created_at
		FROM stage_questions WHERE id = ?
	`, id)
	var q models.Question
	if err := row.Scan(&q.ID, &q.StageID, &q.OrderIndex, &q.Question, &q.AnswerNotes, &q.IsAsked, &q.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &q, nil
}

// InsertQuestion persists a new question and allocates an order when needed.
func (r *Repository) InsertQuestion(q *models.Question) error {
	if q.OrderIndex == 0 {
		var nextOrder int
		if err := r.db.QueryRow("SELECT COALESCE(MAX(order_index), -1) + 1 FROM stage_questions WHERE stage_id = ?", q.StageID).Scan(&nextOrder); err != nil {
			return fmt.Errorf("allocate question order: %w", err)
		}
		q.OrderIndex = nextOrder
	}
	q.CreatedAt = time.Now().Unix()

	_, err := r.db.Exec(`
		INSERT INTO stage_questions (id, stage_id, order_index, question, answer_notes, is_asked, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, q.ID, q.StageID, q.OrderIndex, q.Question, q.AnswerNotes, q.IsAsked, q.CreatedAt)
	return err
}

// UpdateQuestion updates the fields selected by the service layer.
func (r *Repository) UpdateQuestion(id string, fields map[string]interface{}) error {
	allowed := map[string]bool{"question": true, "answer_notes": true, "is_asked": true, "order_index": true}
	var clauses []string
	var args []interface{}

	for k, v := range fields {
		if !allowed[k] {
			continue
		}
		clauses = append(clauses, fmt.Sprintf("%s = ?", k))
		args = append(args, v)
	}

	if len(clauses) == 0 {
		return nil
	}
	// Column names in clauses come only from the allowlist above; values remain parameterized.
	query := fmt.Sprintf("UPDATE stage_questions SET %s WHERE id = ?", strings.Join(clauses, ", ")) // #nosec G201
	args = append(args, id)

	res, err := r.db.Exec(query, args...)
	if err != nil {
		return err
	}
	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteQuestion removes the question identified by id.
func (r *Repository) DeleteQuestion(id string) error {
	res, err := r.db.Exec("DELETE FROM stage_questions WHERE id = ?", id)
	if err != nil {
		return err
	}
	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ReorderQuestions stores the supplied ordering for a stage's questions.
func (r *Repository) ReorderQuestions(stageID string, questionIDs []string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.Prepare("UPDATE stage_questions SET order_index = ? WHERE id = ? AND stage_id = ?")
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()

	seenIDs := make(map[string]struct{}, len(questionIDs))
	for idx, id := range questionIDs {
		if _, exists := seenIDs[id]; exists {
			return fmt.Errorf("duplicate question id %q", id)
		}
		seenIDs[id] = struct{}{}
		result, err := stmt.Exec(idx, id, stageID)
		if err != nil {
			return err
		}
		if err := requireOneUpdatedRow(result, "question", id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Attachments

// GetAttachmentsByJobID returns the attachments belonging to a job.
func (r *Repository) GetAttachmentsByJobID(jobID string) ([]models.Attachment, error) {
	return r.getAttachments(&jobID)
}

// GetAllAttachments returns every attachment in stable job and creation order.
func (r *Repository) GetAllAttachments() ([]models.Attachment, error) {
	return r.getAttachments(nil)
}

func (r *Repository) getAttachments(jobID *string) ([]models.Attachment, error) {
	query := `
		SELECT id, job_id, stage_id, original_name, stored_filename, file_size, mime_type, created_at
		FROM attachments`
	var args []interface{}
	if jobID != nil {
		query += " WHERE job_id = ?"
		args = append(args, *jobID)
	}
	query += " ORDER BY job_id, created_at DESC, id"
	rows, err := r.reader.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var attachments []models.Attachment
	for rows.Next() {
		var a models.Attachment
		if err := rows.Scan(&a.ID, &a.JobID, &a.StageID, &a.OriginalName, &a.StoredFilename, &a.FileSize, &a.MimeType, &a.CreatedAt); err != nil {
			return nil, err
		}
		attachments = append(attachments, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return attachments, nil
}

// GetAttachmentByID returns the attachment identified by id.
func (r *Repository) GetAttachmentByID(id string) (*models.Attachment, error) {
	row := r.reader.QueryRow(`
		SELECT id, job_id, stage_id, original_name, stored_filename, file_size, mime_type, created_at
		FROM attachments WHERE id = ?
	`, id)

	var a models.Attachment
	if err := row.Scan(&a.ID, &a.JobID, &a.StageID, &a.OriginalName, &a.StoredFilename, &a.FileSize, &a.MimeType, &a.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &a, nil
}

// InsertAttachment persists attachment metadata.
func (r *Repository) InsertAttachment(a *models.Attachment) error {
	a.CreatedAt = time.Now().Unix()
	// Keep the legacy column populated without persisting an environment-specific path.
	legacyPathValue := a.StoredFilename
	_, err := r.db.Exec(`
		INSERT INTO attachments (id, job_id, stage_id, original_name, stored_filename, absolute_path, file_size, mime_type, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, a.ID, a.JobID, a.StageID, a.OriginalName, a.StoredFilename, legacyPathValue, a.FileSize, a.MimeType, a.CreatedAt)
	return err
}

// DeleteAttachment removes the attachment identified by id.
func (r *Repository) DeleteAttachment(id string) error {
	res, err := r.db.Exec("DELETE FROM attachments WHERE id = ?", id)
	if err != nil {
		return err
	}
	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// GetScheduledMeetings returns the stages with scheduled interview details.
func (r *Repository) GetScheduledMeetings() ([]models.ScheduledMeeting, error) {
	rows, err := r.reader.Query(`
		SELECT
			stages.id as stage_id,
			stages.job_id,
			stages.order_index,
			stages.stage_type,
			stages.custom_title,
			stages.description as stage_description,
			stages.status as stage_status,
			stages.meeting_date,
			stages.meeting_time,
			stages.meeting_url,
			stages.meeting_type,
			stages.notes as stage_notes,
			jobs.company_name,
			jobs.position_title,
			jobs.status as job_status,
			COALESCE(stages.recruiter_name, jobs.recruiter_name) as recruiter_name,
			COALESCE(stages.recruiter_contact, jobs.recruiter_contact) as recruiter_contact,
			COALESCE(stages.recruiter_agency, jobs.recruiter_agency) as recruiter_agency,
			jobs.job_post_url,
			jobs.avatar_seed,
			COALESCE(jobs.company_domain, '')
		FROM stages
		INNER JOIN jobs ON jobs.id = stages.job_id
		WHERE stages.meeting_date IS NOT NULL AND stages.meeting_date != ''
		ORDER BY stages.meeting_date ASC, stages.meeting_time ASC
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var meetings []models.ScheduledMeeting
	for rows.Next() {
		var m models.ScheduledMeeting
		err := rows.Scan(
			&m.StageID, &m.JobID, &m.OrderIndex, &m.StageType, &m.CustomTitle,
			&m.StageDescription, &m.StageStatus, &m.MeetingDate, &m.MeetingTime,
			&m.MeetingURL, &m.MeetingType, &m.StageNotes, &m.CompanyName,
			&m.PositionTitle, &m.JobStatus, &m.RecruiterName, &m.RecruiterContact,
			&m.RecruiterAgency, &m.JobPostURL, &m.AvatarSeed, &m.CompanyDomain,
		)
		if err != nil {
			return nil, err
		}
		meetings = append(meetings, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return meetings, nil
}
