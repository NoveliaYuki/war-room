// Package repository persists application models in SQLite.
package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"war-room/backend/pkg/models"
)

func splitTechnologyAliases(value string) []string {
	if value == "" {
		return []string{}
	}
	return strings.Split(value, "\n")
}

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

// ListUserIDs returns every signed-in account's ID.
func (r *Repository) ListUserIDs() ([]string, error) {
	rows, err := r.reader.Query(`SELECT id FROM users`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// GetAllJobs returns the calling user's jobs matching the optional status and search filters.
func (r *Repository) GetAllJobs(userID, status, search string) ([]models.Job, error) {
	query, args := buildJobsQuery(&userID, status, search)
	rows, err := r.reader.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	jobs, err := scanJobs(rows, r, &userID)
	if err != nil {
		return nil, err
	}
	if err := populateJobTechnologies(r.reader, jobs); err != nil {
		return nil, err
	}
	if err := populateJobHistory(r.reader, jobs); err != nil {
		return nil, err
	}
	return jobs, rows.Err()
}

// GetAllJobsAllUsers returns every job across every account, with ownership
// populated, for the install-wide local disaster-recovery mirror.
func (r *Repository) GetAllJobsAllUsers() ([]models.Job, error) {
	query, args := buildJobsQuery(nil, "", "")
	rows, err := r.reader.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	jobs, err := scanJobs(rows, r, nil)
	if err != nil {
		return nil, err
	}
	if err := populateJobTechnologies(r.reader, jobs); err != nil {
		return nil, err
	}
	if err := populateJobHistory(r.reader, jobs); err != nil {
		return nil, err
	}
	return jobs, rows.Err()
}

func populateJobHistory(reader readQueryer, jobs []models.Job) error {
	if len(jobs) == 0 {
		return nil
	}
	byID := make(map[string]int, len(jobs))
	for index := range jobs {
		jobs[index].History = []models.JobHistory{}
		byID[jobs[index].ID] = index
	}
	rows, err := reader.Query(`SELECT job_id, occurred_at, action FROM job_history ORDER BY occurred_at, id`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var jobID string
		var item models.JobHistory
		if err := rows.Scan(&jobID, &item.OccurredAt, &item.Action); err != nil {
			return err
		}
		if index, exists := byID[jobID]; exists {
			jobs[index].History = append(jobs[index].History, item)
		}
	}
	return rows.Err()
}

func populateJobTechnologies(reader readQueryer, jobs []models.Job) error {
	if len(jobs) == 0 {
		return nil
	}
	byID := make(map[string]int, len(jobs))
	for index := range jobs {
		jobs[index].Technologies = []models.Technology{}
		byID[jobs[index].ID] = index
	}
	rows, err := reader.Query(`SELECT jt.job_id, t.id, t.name, COALESCE((SELECT GROUP_CONCAT(alias, char(10)) FROM (SELECT alias FROM technology_aliases WHERE technology_id = t.id ORDER BY length(alias), alias)), '') FROM job_technologies jt JOIN technologies t ON t.id = jt.technology_id ORDER BY t.name COLLATE NOCASE`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var jobID string
		var item models.Technology
		var aliases string
		if err := rows.Scan(&jobID, &item.ID, &item.Name, &aliases); err != nil {
			return err
		}
		item.Aliases = splitTechnologyAliases(aliases)
		if index, exists := byID[jobID]; exists {
			jobs[index].Technologies = append(jobs[index].Technologies, item)
		}
	}
	return rows.Err()
}

// buildJobsQuery assembles the jobs listing query. userID of nil scopes
// nothing (every account); a non-nil userID restricts the result to that
// account's own jobs. This is a private helper: every exported entry point
// (GetAllJobs vs GetAllJobsAllUsers) decides explicitly which behavior it
// wants, so no caller can leak data across accounts by a missed argument.
func buildJobsQuery(userID *string, status, search string) (string, []interface{}) {
	query := `
		SELECT
			jobs.id, jobs.company_name, jobs.position_title, jobs.status, jobs.salary_type,
			jobs.salary_min, jobs.salary_max, jobs.salary_currency, jobs.recruiter_type,
			jobs.recruiter_name, jobs.recruiter_agency, jobs.recruiter_contact,
			jobs.interviewers_json, jobs.job_post_url, jobs.application_url, jobs.avatar_seed, jobs.keyword_note,
			jobs.description, jobs.company_overview, jobs.company_domain, jobs.interview_notes,
			jobs.reasons_to_change, jobs.experience_notes, jobs.expected_salary, jobs.work_arrangement,
			jobs.employment_type, jobs.is_referral, jobs.order_index, jobs.status_changed_at,
			jobs.application_sent_date, jobs.recruiter_first_contact_date, jobs.created_at, jobs.updated_at, jobs.last_activity_at, jobs.cv_version_id, jobs.search_period_id, jobs.user_id,
			(SELECT COALESCE(custom_title, stage_type) FROM stages WHERE job_id = jobs.id AND status = 'current' ORDER BY order_index, id LIMIT 1) as current_stage_title,
			(SELECT order_index + 1 FROM stages WHERE job_id = jobs.id AND status = 'current' ORDER BY order_index, id LIMIT 1) as current_stage_index,
			(SELECT COUNT(*) FROM stages WHERE job_id = jobs.id) as total_stages_count
		FROM jobs
		WHERE 1=1
	`
	var args []interface{}

	if userID != nil {
		query += " AND jobs.user_id = ?"
		args = append(args, *userID)
	}

	if status != "" && status != "all" {
		query += " AND status = ?"
		args = append(args, status)
	}

	if searchTrim := strings.TrimSpace(search); searchTrim != "" {
		wildcard := "%" + escapeLikeLiteral(searchTrim) + "%"
		query += " AND (company_name LIKE ? ESCAPE '\\' OR position_title LIKE ? ESCAPE '\\' OR keyword_note LIKE ? ESCAPE '\\' OR recruiter_name LIKE ? ESCAPE '\\' OR (is_referral = 1 AND 'referral' LIKE ? ESCAPE '\\'))"
		args = append(args, wildcard, wildcard, wildcard, wildcard, wildcard)
	}

	query += " ORDER BY jobs.status_changed_at DESC, jobs.created_at DESC, jobs.order_index DESC"

	return query, args
}

// escapeLikeLiteral neutralizes LIKE wildcards in user search text.
func escapeLikeLiteral(value string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(value)
}

// scanJobs scans rows produced by buildJobsQuery. scopeUserID mirrors the
// same argument passed to buildJobsQuery: nil when scanning an install-wide
// (all users) result, so the selected CV version lookup for each row is
// resolved with the same scope as the row itself.
func scanJobs(rows *sql.Rows, repo *Repository, scopeUserID *string) ([]models.Job, error) {
	var jobs []models.Job
	for rows.Next() {
		var j models.Job
		var ijJSON sql.NullString
		var cvVersionID, searchPeriodID, rowUserID sql.NullString
		var applicationSentDate, recruiterFirstContactDate sql.NullString
		err := rows.Scan(
			&j.ID, &j.CompanyName, &j.PositionTitle, &j.Status, &j.SalaryType,
			&j.SalaryMin, &j.SalaryMax, &j.SalaryCurrency, &j.RecruiterType,
			&j.RecruiterName, &j.RecruiterAgency, &j.RecruiterContact,
			&ijJSON, &j.JobPostURL, &j.ApplicationURL, &j.AvatarSeed, &j.KeywordNote,
			&j.Description, &j.CompanyOverview, &j.CompanyDomain, &j.InterviewNotes,
			&j.ReasonsToChange, &j.ExperienceNotes, &j.ExpectedSalary, &j.WorkArrangement,
			&j.EmploymentType, &j.IsReferral, &j.OrderIndex, &j.StatusChangedAt, &applicationSentDate, &recruiterFirstContactDate, &j.CreatedAt, &j.UpdatedAt, &j.LastActivityAt, &cvVersionID, &searchPeriodID, &rowUserID,
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
		if cvVersionID.Valid {
			j.SelectedCVVersion, err = repo.getCVVersionByIDScoped(scopeUserID, cvVersionID.String)
			if err != nil {
				return nil, fmt.Errorf("load selected CV version: %w", err)
			}
		}
		j.ApplicationSentDate = nullableStringPointer(applicationSentDate)
		j.RecruiterFirstContactDate = nullableStringPointer(recruiterFirstContactDate)
		j.SearchPeriodID = nullableStringPointer(searchPeriodID)
		if scopeUserID == nil {
			j.UserID = nullableStringPointer(rowUserID)
		}
		jobs = append(jobs, j)
	}
	return jobs, nil
}

func nullableStringPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

// GetJobByID returns the job identified by id if it belongs to userID.
func (r *Repository) GetJobByID(userID, id string) (*models.Job, error) {
	row := r.reader.QueryRow(`
		SELECT
			id, company_name, position_title, status, salary_type, salary_min, salary_max,
			salary_currency, recruiter_type, recruiter_name, recruiter_agency, recruiter_contact,
			interviewers_json, job_post_url, application_url, avatar_seed, keyword_note, description, company_overview,
			company_domain, interview_notes, reasons_to_change, experience_notes, expected_salary, work_arrangement,
			employment_type, is_referral, order_index, status_changed_at, application_sent_date,
			recruiter_first_contact_date, created_at, updated_at, last_activity_at, cv_version_id, search_period_id
		FROM jobs WHERE id = ? AND user_id = ?
	`, id, userID)

	var j models.Job
	var ijJSON sql.NullString
	var cvVersionID, searchPeriodID sql.NullString
	var applicationSentDate, recruiterFirstContactDate sql.NullString
	err := row.Scan(
		&j.ID, &j.CompanyName, &j.PositionTitle, &j.Status, &j.SalaryType,
		&j.SalaryMin, &j.SalaryMax, &j.SalaryCurrency, &j.RecruiterType,
		&j.RecruiterName, &j.RecruiterAgency, &j.RecruiterContact,
		&ijJSON, &j.JobPostURL, &j.ApplicationURL, &j.AvatarSeed, &j.KeywordNote,
		&j.Description, &j.CompanyOverview, &j.CompanyDomain, &j.InterviewNotes,
		&j.ReasonsToChange, &j.ExperienceNotes, &j.ExpectedSalary, &j.WorkArrangement,
		&j.EmploymentType, &j.IsReferral, &j.OrderIndex, &j.StatusChangedAt, &applicationSentDate, &recruiterFirstContactDate, &j.CreatedAt, &j.UpdatedAt, &j.LastActivityAt, &cvVersionID, &searchPeriodID,
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
	if cvVersionID.Valid {
		j.SelectedCVVersion, err = r.GetCVVersionByID(userID, cvVersionID.String)
		if err != nil {
			return nil, fmt.Errorf("load selected CV version: %w", err)
		}
	}
	j.ApplicationSentDate = nullableStringPointer(applicationSentDate)
	j.RecruiterFirstContactDate = nullableStringPointer(recruiterFirstContactDate)
	j.SearchPeriodID = nullableStringPointer(searchPeriodID)
	jobTechnologies, err := r.JobTechnologies(j.ID)
	if err != nil {
		return nil, err
	}
	j.Technologies = jobTechnologies
	if err := r.loadJobHistory(&j); err != nil {
		return nil, err
	}
	return &j, nil
}

func (r *Repository) loadJobHistory(job *models.Job) error {
	rows, err := r.reader.Query(`SELECT occurred_at, action FROM job_history WHERE job_id = ? ORDER BY occurred_at DESC, id DESC`, job.ID)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	job.History = []models.JobHistory{}
	for rows.Next() {
		var item models.JobHistory
		if err := rows.Scan(&item.OccurredAt, &item.Action); err != nil {
			return err
		}
		job.History = append(job.History, item)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for left, right := 0, len(job.History)-1; left < right; left, right = left+1, right-1 {
		job.History[left], job.History[right] = job.History[right], job.History[left]
	}
	return nil
}

// ListCVVersions returns the calling user's CV uploads in descending version order.
func (r *Repository) ListCVVersions(userID string) ([]models.CVVersion, error) {
	rows, err := r.reader.Query(`SELECT id, version, original_name, stored_filename, file_size, mime_type, sha256, uploaded_at FROM cv_versions WHERE user_id = ? ORDER BY version DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	versions := []models.CVVersion{}
	for rows.Next() {
		var version models.CVVersion
		if err := rows.Scan(&version.ID, &version.Version, &version.OriginalName, &version.StoredFilename, &version.FileSize, &version.MimeType, &version.SHA256, &version.UploadedAt); err != nil {
			return nil, err
		}
		versions = append(versions, version)
	}
	return versions, rows.Err()
}

// ListCVVersionsAllUsers returns every account's CV uploads, with ownership
// populated, for the install-wide local disaster-recovery mirror.
func (r *Repository) ListCVVersionsAllUsers() ([]models.CVVersion, error) {
	rows, err := r.reader.Query(`SELECT id, version, original_name, stored_filename, file_size, mime_type, sha256, uploaded_at, user_id FROM cv_versions ORDER BY version DESC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	versions := []models.CVVersion{}
	for rows.Next() {
		var version models.CVVersion
		var userID sql.NullString
		if err := rows.Scan(&version.ID, &version.Version, &version.OriginalName, &version.StoredFilename, &version.FileSize, &version.MimeType, &version.SHA256, &version.UploadedAt, &userID); err != nil {
			return nil, err
		}
		version.UserID = nullableStringPointer(userID)
		versions = append(versions, version)
	}
	return versions, rows.Err()
}

// GetCVVersionByID returns the version identified by id if it belongs to userID.
func (r *Repository) GetCVVersionByID(userID, id string) (*models.CVVersion, error) {
	return r.getCVVersionByIDScoped(&userID, id)
}

// getCVVersionByIDScoped is the shared implementation behind GetCVVersionByID
// and the internal lookups scanJobs performs for each row it reads. A nil
// scopeUserID is only ever passed from the AllUsers scanning path.
func (r *Repository) getCVVersionByIDScoped(scopeUserID *string, id string) (*models.CVVersion, error) {
	query := `SELECT id, version, original_name, stored_filename, file_size, mime_type, sha256, uploaded_at FROM cv_versions WHERE id = ?`
	args := []interface{}{id}
	if scopeUserID != nil {
		query += " AND user_id = ?"
		args = append(args, *scopeUserID)
	}
	var version models.CVVersion
	err := r.reader.QueryRow(query, args...).
		Scan(&version.ID, &version.Version, &version.OriginalName, &version.StoredFilename, &version.FileSize, &version.MimeType, &version.SHA256, &version.UploadedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &version, nil
}

// InsertCVVersion stores metadata for a newly uploaded CV version, assigning
// it the next version number in userID's own library.
func (r *Repository) InsertCVVersion(userID string, version *models.CVVersion) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`INSERT INTO cv_version_sequence (user_id, next_version) VALUES (?, 1) ON CONFLICT(user_id) DO NOTHING`, userID); err != nil {
		return err
	}
	if err := tx.QueryRow(`SELECT next_version FROM cv_version_sequence WHERE user_id = ?`, userID).Scan(&version.Version); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO cv_versions (id, user_id, version, original_name, stored_filename, file_size, mime_type, sha256, uploaded_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		version.ID, userID, version.Version, version.OriginalName, version.StoredFilename, version.FileSize, version.MimeType, version.SHA256, version.UploadedAt); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE cv_version_sequence SET next_version = ? WHERE user_id = ?`, version.Version+1, userID); err != nil {
		return err
	}
	return tx.Commit()
}

// AssignCVVersion selects a CV version for a job owned by userID, or clears
// it when versionID is nil.
func (r *Repository) AssignCVVersion(userID, jobID string, versionID *string) error {
	result, err := r.db.Exec(`UPDATE jobs SET cv_version_id = ?, updated_at = unixepoch() WHERE id = ? AND user_id = ?`, versionID, jobID, userID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteCVVersion removes an unassigned version owned by userID.
func (r *Repository) DeleteCVVersion(userID, id string) error {
	result, err := r.db.Exec(`DELETE FROM cv_versions WHERE id = ? AND user_id = ? AND NOT EXISTS (SELECT 1 FROM jobs WHERE cv_version_id = ?)`, id, userID, id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// CountCVBlobReferences reports how many versions across every account still
// point at one stored blob. CV blobs are content-addressed and stored in a
// single shared directory, so this stays install-wide: it protects a blob
// another user's version still relies on.
func (r *Repository) CountCVBlobReferences(filename string, count *int) error {
	return r.db.QueryRow(`SELECT COUNT(*) FROM cv_versions WHERE stored_filename = ?`, filename).Scan(count)
}

// GetNextCVVersion returns userID's persistent next version number, lazily
// creating their sequence row (starting at 1) on first use.
func (r *Repository) GetNextCVVersion(userID string) (int, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`INSERT INTO cv_version_sequence (user_id, next_version) VALUES (?, 1) ON CONFLICT(user_id) DO NOTHING`, userID); err != nil {
		return 0, err
	}
	var next int
	if err := tx.QueryRow(`SELECT next_version FROM cv_version_sequence WHERE user_id = ?`, userID).Scan(&next); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return next, nil
}

// GetNextCVVersionAllUsers reports the highest next-version counter across
// every account, for the install-wide recovery mirror's informational field.
func (r *Repository) GetNextCVVersionAllUsers() (int, error) {
	var next int
	err := r.reader.QueryRow(`SELECT COALESCE(MAX(next_version), 1) FROM cv_version_sequence`).Scan(&next)
	return next, err
}

// MigrateLegacyCVAttachments atomically creates one initial CV version and
// replaces legacy attachment references across the supplied jobs. This
// one-time sweep predates per-user data: it runs at server startup, before
// any account may have signed in, and also right after importing a
// pre-multi-user backup. In both cases the affected jobs already share a
// single owner (nil before any account exists; a concrete user right after
// that user's own import), so the migrated version's ownership is derived
// from the jobs themselves rather than taking a userID parameter that no
// caller could supply consistently.
func (r *Repository) MigrateLegacyCVAttachments(version *models.CVVersion, attachments []models.Attachment, jobIDs []string) (bool, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	created, err := migrateLegacyCVRows(tx, version, attachments, jobIDs)
	if err != nil || !created {
		return created, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func migrateLegacyCVRows(tx *sql.Tx, version *models.CVVersion, attachments []models.Attachment, jobIDs []string) (bool, error) {
	var count int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM cv_versions`).Scan(&count); err != nil {
		return false, err
	}
	if count > 0 {
		return false, nil
	}
	ownerID, err := commonJobOwner(tx, jobIDs)
	if err != nil {
		return false, err
	}
	if _, err := tx.Exec(`INSERT INTO cv_versions (id, user_id, version, original_name, stored_filename, file_size, mime_type, sha256, uploaded_at) VALUES (?, ?, 1, ?, ?, ?, ?, ?, ?)`, version.ID, ownerID, version.OriginalName, version.StoredFilename, version.FileSize, version.MimeType, version.SHA256, version.UploadedAt); err != nil {
		return false, err
	}
	if err := assignLegacyCVJobs(tx, version.ID, jobIDs); err != nil {
		return false, err
	}
	if err := deleteLegacyCVAttachments(tx, attachments); err != nil {
		return false, err
	}
	if ownerID != nil {
		// A concrete owner (the post-import case) needs its sequence
		// advanced past version 1. When ownerID is nil (the pre-account
		// case) there is no per-user sequence to seed yet; ClaimUnownedData
		// recomputes it correctly from cv_versions once a real account
		// claims this row.
		if _, err := tx.Exec(`INSERT INTO cv_version_sequence (user_id, next_version) VALUES (?, 2) ON CONFLICT(user_id) DO UPDATE SET next_version = MAX(next_version, 2)`, *ownerID); err != nil {
			return false, err
		}
	}
	return true, nil
}

// commonJobOwner returns the single user_id shared by every job in jobIDs
// (nil if they are all unowned), or an error if they do not all agree.
func commonJobOwner(tx *sql.Tx, jobIDs []string) (*string, error) {
	if len(jobIDs) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(jobIDs))
	args := make([]interface{}, len(jobIDs))
	for i, id := range jobIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	rows, err := tx.Query(`SELECT DISTINCT user_id FROM jobs WHERE id IN (`+strings.Join(placeholders, ",")+`)`, args...) // #nosec G202 -- placeholders are always literal "?" marks; job IDs are bound as args, never concatenated.
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var owners []sql.NullString
	for rows.Next() {
		var owner sql.NullString
		if err := rows.Scan(&owner); err != nil {
			return nil, err
		}
		owners = append(owners, owner)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(owners) != 1 {
		return nil, ErrLegacyCVAcrossAccounts
	}
	return nullableStringPointer(owners[0]), nil
}

// ErrInvalidReorder reports a reorder request naming unknown or duplicate IDs.
var ErrInvalidReorder = errors.New("invalid reorder request")

// ErrLegacyCVAcrossAccounts reports legacy CV files owned by more than one account.
var ErrLegacyCVAcrossAccounts = errors.New("legacy CV attachments span multiple accounts")

func assignLegacyCVJobs(tx *sql.Tx, versionID string, jobIDs []string) error {
	for _, jobID := range jobIDs {
		if _, err := tx.Exec(`UPDATE jobs SET cv_version_id = ? WHERE id = ?`, versionID, jobID); err != nil {
			return err
		}
	}
	return nil
}

func deleteLegacyCVAttachments(tx *sql.Tx, attachments []models.Attachment) error {
	for _, attachment := range attachments {
		if _, err := tx.Exec(`DELETE FROM attachments WHERE id = ?`, attachment.ID); err != nil {
			return err
		}
	}
	return nil
}

// GetJobCounts returns the number of jobs in each status for userID.
func (r *Repository) GetJobCounts(userID string) (*models.JobCounts, error) {
	row := r.reader.QueryRow(`
		SELECT
			COUNT(*) as total,
			COALESCE(SUM(CASE WHEN status = 'waiting' THEN 1 ELSE 0 END), 0) as waiting,
			COALESCE(SUM(CASE WHEN status = 'ongoing' THEN 1 ELSE 0 END), 0) as ongoing,
			COALESCE(SUM(CASE WHEN status = 'accepted' THEN 1 ELSE 0 END), 0) as accepted,
			COALESCE(SUM(CASE WHEN status = 'rejected' THEN 1 ELSE 0 END), 0) as rejected
		FROM jobs WHERE user_id = ?
	`, userID)
	var counts models.JobCounts
	if err := row.Scan(&counts.All, &counts.Waiting, &counts.Ongoing, &counts.Accepted, &counts.Rejected); err != nil {
		return nil, err
	}
	return &counts, nil
}

// InsertJob persists a new job owned by userID and allocates an order when needed.
func (r *Repository) InsertJob(userID string, job *models.Job) error {
	if job.WorkArrangement == "" {
		job.WorkArrangement = models.WorkArrangementUnknown
	}
	if job.EmploymentType == "" {
		job.EmploymentType = models.EmploymentTypeUnknown
	}
	if job.OrderIndex == 0 {
		var nextOrder int
		if err := r.db.QueryRow("SELECT COALESCE(MAX(order_index), -1) + 1 FROM jobs WHERE user_id = ?", userID).Scan(&nextOrder); err != nil {
			return fmt.Errorf("allocate job order: %w", err)
		}
		job.OrderIndex = nextOrder
	}
	now := time.Now().Unix()
	job.CreatedAt = now
	job.StatusChangedAt = now
	job.UpdatedAt = now
	job.LastActivityAt = now

	ijBytes, _ := json.Marshal(job.Interviewers)
	job.InterviewersJSON = string(ijBytes)

	_, err := r.db.Exec(`
		INSERT INTO jobs (
			id, user_id, company_name, position_title, status, salary_type, salary_min, salary_max,
			salary_currency, recruiter_type, recruiter_name, recruiter_agency, recruiter_contact,
			interviewers_json, job_post_url, application_url, avatar_seed, keyword_note, description, company_overview,
			company_domain, interview_notes, reasons_to_change, experience_notes, expected_salary, work_arrangement,
		employment_type, is_referral, order_index, status_changed_at, application_sent_date,
			recruiter_first_contact_date, created_at, updated_at, last_activity_at, cv_version_id, search_period_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		job.ID, userID, job.CompanyName, job.PositionTitle, job.Status, job.SalaryType,
		job.SalaryMin, job.SalaryMax, job.SalaryCurrency, job.RecruiterType,
		job.RecruiterName, job.RecruiterAgency, job.RecruiterContact,
		job.InterviewersJSON, job.JobPostURL, job.ApplicationURL, job.AvatarSeed, job.KeywordNote,
		job.Description, job.CompanyOverview, job.CompanyDomain, job.InterviewNotes,
		job.ReasonsToChange, job.ExperienceNotes, job.ExpectedSalary,
		job.WorkArrangement, job.EmploymentType, job.IsReferral, job.OrderIndex, job.StatusChangedAt,
		job.ApplicationSentDate, job.RecruiterFirstContactDate, job.CreatedAt, job.UpdatedAt, job.LastActivityAt, selectedCVVersionID(job.SelectedCVVersion), job.SearchPeriodID,
	)
	return err
}

// RejectInactiveWaitingJobs moves userID's waiting jobs with no saved activity for 30 days to rejected.
func (r *Repository) RejectInactiveWaitingJobs(userID string, now time.Time) (int64, error) {
	cutoff := now.Add(-30 * 24 * time.Hour).Unix()
	result, err := r.db.Exec(`UPDATE jobs SET status = 'rejected', updated_at = ?
		WHERE user_id = ? AND status = 'waiting' AND last_activity_at <= ?`, now.Unix(), userID, cutoff)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func selectedCVVersionID(version *models.CVVersion) interface{} {
	if version == nil {
		return nil
	}
	return version.ID
}

// UpdateJob updates the fields selected by the service layer on a job owned by userID.
func (r *Repository) UpdateJob(userID, id string, fields map[string]interface{}) error {
	allowed := map[string]bool{
		"company_name": true, "position_title": true, "status": true, "salary_type": true,
		"salary_min": true, "salary_max": true, "salary_currency": true, "recruiter_type": true,
		"recruiter_name": true, "recruiter_agency": true, "recruiter_contact": true,
		"interviewers_json": true, "job_post_url": true, "application_url": true, "avatar_seed": true, "keyword_note": true,
		"description": true, "company_overview": true, "company_domain": true,
		"interview_notes": true, "reasons_to_change": true, "experience_notes": true,
		"expected_salary": true, "work_arrangement": true, "employment_type": true,
		"is_referral": true, "order_index": true, "application_sent_date": true,
		"recruiter_first_contact_date": true, "search_period_id": true,
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

	clauses = append(clauses, "updated_at = unixepoch()", "last_activity_at = unixepoch()")
	// Column names are allowlisted above; values remain parameterized.
	query := fmt.Sprintf("UPDATE jobs SET %s WHERE id = ? AND user_id = ?", strings.Join(clauses, ", ")) // #nosec G201 // nosemgrep: go.lang.security.audit.database.string-formatted-query.string-formatted-query
	args = append(args, id, userID)

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
	case "application_sent_date", "recruiter_first_contact_date":
		if text, ok := value.(*string); ok && text != nil && strings.TrimSpace(*text) == "" {
			return nil
		}
	}
	return value
}

// DeleteJob removes the job identified by id if it belongs to userID.
func (r *Repository) DeleteJob(userID, id string) error {
	res, err := r.db.Exec("DELETE FROM jobs WHERE id = ? AND user_id = ?", id, userID)
	if err != nil {
		return err
	}
	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ReorderJobs stores the supplied job ordering for userID's own jobs.
func (r *Repository) ReorderJobs(userID string, jobIDs []string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.Prepare("UPDATE jobs SET order_index = ?, updated_at = unixepoch() WHERE id = ? AND user_id = ?")
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()

	seenIDs := make(map[string]struct{}, len(jobIDs))
	for idx, id := range jobIDs {
		if _, exists := seenIDs[id]; exists {
			return fmt.Errorf("%w: duplicate job id %q", ErrInvalidReorder, id)
		}
		seenIDs[id] = struct{}{}
		result, err := stmt.Exec(idx, id, userID)
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
		return fmt.Errorf("%w: %s %q was not found in reorder scope", ErrInvalidReorder, kind, id)
	}
	return nil
}

// Stages
//
// Stages, questions, and attachments carry no user_id column of their own:
// they are pure children of jobs, so ownership is always derived by joining
// to jobs.user_id. Every lookup or mutation below that accepts a raw stage,
// question, or attachment ID (rather than one already verified through a
// scoped job fetch) takes userID and enforces that join.

const stageOwnedByUserClause = `job_id IN (SELECT id FROM jobs WHERE user_id = ?)`

// GetStagesByJobID returns the stages belonging to a job owned by userID.
func (r *Repository) GetStagesByJobID(userID, jobID string) ([]models.Stage, error) {
	return r.getStages(&jobID, userID)
}

// GetAllStages returns every stage across every account, in stable job and
// stage order, for the install-wide local disaster-recovery mirror.
func (r *Repository) GetAllStages() ([]models.Stage, error) {
	return r.getStages(nil, "")
}

func (r *Repository) getStages(jobID *string, userID string) ([]models.Stage, error) {
	query := `
		SELECT
			id, job_id, order_index, stage_type, custom_title, description, status,
			scheduled_at, meeting_date, meeting_time, meeting_url, meeting_type, notes,
			recruiter_name, recruiter_type, recruiter_agency, recruiter_contact,
			interviewers_json, created_at
		FROM stages`
	var args []interface{}
	if jobID != nil {
		query += " WHERE job_id = ? AND " + stageOwnedByUserClause
		args = append(args, *jobID, userID)
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

// GetStageByID returns the stage identified by id if its job belongs to userID.
func (r *Repository) GetStageByID(userID, id string) (*models.Stage, error) {
	row := r.reader.QueryRow(`
		SELECT
			id, job_id, order_index, stage_type, custom_title, description, status,
			scheduled_at, meeting_date, meeting_time, meeting_url, meeting_type, notes,
			recruiter_name, recruiter_type, recruiter_agency, recruiter_contact,
			interviewers_json, created_at
		FROM stages WHERE id = ? AND `+stageOwnedByUserClause, id, userID)

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

// InsertStage persists a new stage. Callers must already have verified that
// stage.JobID belongs to the acting user (typically via a scoped GetJobByID).
func (r *Repository) InsertStage(stage *models.Stage) error {
	return insertStage(r.db, stage)
}

// InsertFirstStageAndSetJobOngoing atomically starts a process when its first
// stage is added. Callers must already have verified job ownership.
func (r *Repository) InsertFirstStageAndSetJobOngoing(stage *models.Stage) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := insertStage(tx, stage); err != nil {
		return err
	}
	result, err := tx.Exec(`UPDATE jobs SET status = 'ongoing', updated_at = unixepoch() WHERE id = ?`, stage.JobID)
	if err != nil {
		return err
	}
	if err := requireOneUpdatedRow(result, "job", stage.JobID); err != nil {
		return err
	}
	return tx.Commit()
}

type stageExecer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

func insertStage(execer stageExecer, stage *models.Stage) error {
	if stage.CreatedAt == 0 {
		stage.CreatedAt = time.Now().Unix()
	}
	ijBytes, err := json.Marshal(stage.Interviewers)
	if err != nil {
		return err
	}
	stage.InterviewersJSON = string(ijBytes)
	_, err = execer.Exec(`
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

// UpdateStage updates the fields selected by the service layer on a stage
// whose job belongs to userID.
func (r *Repository) UpdateStage(userID, id string, fields map[string]interface{}) error {
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

	// Column names are allowlisted above; values remain parameterized.
	query := fmt.Sprintf("UPDATE stages SET %s WHERE id = ? AND %s", strings.Join(clauses, ", "), stageOwnedByUserClause) // #nosec G201 // nosemgrep: go.lang.security.audit.database.string-formatted-query.string-formatted-query
	args = append(args, id, userID)

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

// DeleteStage removes the stage identified by id if its job belongs to userID.
func (r *Repository) DeleteStage(userID, id string) error {
	res, err := r.db.Exec("DELETE FROM stages WHERE id = ? AND "+stageOwnedByUserClause, id, userID)
	if err != nil {
		return err
	}
	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ReorderStages stores the supplied ordering for a job's stages if jobID belongs to userID.
func (r *Repository) ReorderStages(userID, jobID string, stageIDs []string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if err := requireJobOwnedByUser(tx, userID, jobID); err != nil {
		return err
	}

	stmt, err := tx.Prepare("UPDATE stages SET order_index = ? WHERE id = ? AND job_id = ?")
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()

	seenIDs := make(map[string]struct{}, len(stageIDs))
	for idx, id := range stageIDs {
		if _, exists := seenIDs[id]; exists {
			return fmt.Errorf("%w: duplicate stage id %q", ErrInvalidReorder, id)
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

func requireJobOwnedByUser(tx *sql.Tx, userID, jobID string) error {
	var exists int
	err := tx.QueryRow(`SELECT 1 FROM jobs WHERE id = ? AND user_id = ?`, jobID, userID).Scan(&exists)
	if err == sql.ErrNoRows {
		return sql.ErrNoRows
	}
	return err
}

// SetCurrentStage updates stage statuses relative to the selected stage, if
// it belongs to userID.
func (r *Repository) SetCurrentStage(userID, stageID string) ([]models.Stage, error) {
	stage, err := r.GetStageByID(userID, stageID)
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

	return r.GetStagesByJobID(userID, stage.JobID)
}

// Questions

const questionOwnedByUserClause = `stage_id IN (SELECT s.id FROM stages s JOIN jobs j ON j.id = s.job_id WHERE j.user_id = ?)`

// GetQuestionsByStageID returns the questions belonging to a stage whose job belongs to userID.
func (r *Repository) GetQuestionsByStageID(userID, stageID string) ([]models.Question, error) {
	return r.getQuestions(&stageID, userID)
}

// GetAllQuestions returns every question across every account, in stable
// stage and question order, for the install-wide local disaster-recovery mirror.
func (r *Repository) GetAllQuestions() ([]models.Question, error) {
	return r.getQuestions(nil, "")
}

func (r *Repository) getQuestions(stageID *string, userID string) ([]models.Question, error) {
	query := `
		SELECT id, stage_id, order_index, question, answer_notes, is_asked, created_at
		FROM stage_questions`
	var args []interface{}
	if stageID != nil {
		query += " WHERE stage_id = ? AND " + questionOwnedByUserClause
		args = append(args, *stageID, userID)
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

// GetQuestionByID returns the question identified by id if its job belongs to userID.
func (r *Repository) GetQuestionByID(userID, id string) (*models.Question, error) {
	row := r.db.QueryRow(`
		SELECT id, stage_id, order_index, question, answer_notes, is_asked, created_at
		FROM stage_questions WHERE id = ? AND `+questionOwnedByUserClause, id, userID)
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
// Callers must already have verified that q.StageID belongs to the acting
// user (typically via a scoped GetStageByID).
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

// UpdateQuestion updates the fields selected by the service layer on a
// question whose job belongs to userID.
func (r *Repository) UpdateQuestion(userID, id string, fields map[string]interface{}) error {
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
	// Column names are allowlisted above; values remain parameterized.
	query := fmt.Sprintf("UPDATE stage_questions SET %s WHERE id = ? AND %s", strings.Join(clauses, ", "), questionOwnedByUserClause) // #nosec G201 // nosemgrep: go.lang.security.audit.database.string-formatted-query.string-formatted-query
	args = append(args, id, userID)

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

// DeleteQuestion removes the question identified by id if its job belongs to userID.
func (r *Repository) DeleteQuestion(userID, id string) error {
	res, err := r.db.Exec("DELETE FROM stage_questions WHERE id = ? AND "+questionOwnedByUserClause, id, userID)
	if err != nil {
		return err
	}
	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ReorderQuestions stores the supplied ordering for a stage's questions if it belongs to userID.
func (r *Repository) ReorderQuestions(userID, stageID string, questionIDs []string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if err := requireStageOwnedByUser(tx, userID, stageID); err != nil {
		return err
	}

	stmt, err := tx.Prepare("UPDATE stage_questions SET order_index = ? WHERE id = ? AND stage_id = ?")
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()

	seenIDs := make(map[string]struct{}, len(questionIDs))
	for idx, id := range questionIDs {
		if _, exists := seenIDs[id]; exists {
			return fmt.Errorf("%w: duplicate question id %q", ErrInvalidReorder, id)
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

func requireStageOwnedByUser(tx *sql.Tx, userID, stageID string) error {
	var exists int
	err := tx.QueryRow(`SELECT 1 FROM stages s JOIN jobs j ON j.id = s.job_id WHERE s.id = ? AND j.user_id = ?`, stageID, userID).Scan(&exists)
	if err == sql.ErrNoRows {
		return sql.ErrNoRows
	}
	return err
}

// Attachments

const attachmentOwnedByUserClause = `job_id IN (SELECT id FROM jobs WHERE user_id = ?)`

// GetAttachmentsByJobID returns the attachments belonging to a job owned by userID.
func (r *Repository) GetAttachmentsByJobID(userID, jobID string) ([]models.Attachment, error) {
	return r.getAttachments(&jobID, userID)
}

// GetAllAttachments returns every attachment across every account, in
// stable job and creation order, for the install-wide local
// disaster-recovery mirror.
func (r *Repository) GetAllAttachments() ([]models.Attachment, error) {
	return r.getAttachments(nil, "")
}

func (r *Repository) getAttachments(jobID *string, userID string) ([]models.Attachment, error) {
	query := `
		SELECT id, job_id, stage_id, original_name, stored_filename, file_size, mime_type, created_at
		FROM attachments`
	var args []interface{}
	if jobID != nil {
		query += " WHERE job_id = ? AND " + attachmentOwnedByUserClause
		args = append(args, *jobID, userID)
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

// GetAttachmentByID returns the attachment identified by id if its job belongs to userID.
func (r *Repository) GetAttachmentByID(userID, id string) (*models.Attachment, error) {
	row := r.reader.QueryRow(`
		SELECT id, job_id, stage_id, original_name, stored_filename, file_size, mime_type, created_at
		FROM attachments WHERE id = ? AND `+attachmentOwnedByUserClause, id, userID)

	var a models.Attachment
	if err := row.Scan(&a.ID, &a.JobID, &a.StageID, &a.OriginalName, &a.StoredFilename, &a.FileSize, &a.MimeType, &a.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &a, nil
}

// InsertAttachment persists attachment metadata. Callers must already have
// verified that a.JobID (and a.StageID, when set) belong to the acting user.
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

func defaultValue(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// DeleteAttachment removes the attachment identified by id if its job belongs to userID.
func (r *Repository) DeleteAttachment(userID, id string) error {
	res, err := r.db.Exec("DELETE FROM attachments WHERE id = ? AND "+attachmentOwnedByUserClause, id, userID)
	if err != nil {
		return err
	}
	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// GetScheduledMeetings returns userID's stages with scheduled interview details.
func (r *Repository) GetScheduledMeetings(userID string) ([]models.ScheduledMeeting, error) {
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
		WHERE jobs.user_id = ? AND stages.meeting_date IS NOT NULL AND stages.meeting_date != ''
		ORDER BY stages.meeting_date ASC, stages.meeting_time ASC
	`, userID)
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
