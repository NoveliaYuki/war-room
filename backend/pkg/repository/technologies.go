package repository

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"war-room/backend/pkg/models"
)

// ErrTechnologyInUse indicates a catalog entry still has job assignments.
var ErrTechnologyInUse = errors.New("technology still has job assignments")

// ListTechnologies returns userID's catalog and the jobs currently using each item.
func (r *Repository) ListTechnologies(userID string) ([]models.Technology, error) {
	technologies, byID, err := r.readTechnologyCatalog(&userID)
	if err != nil {
		return nil, err
	}
	if err := r.populateTechnologyAliases(byID, &userID); err != nil {
		return nil, err
	}
	if err := r.populateTechnologyJobs(byID, &userID); err != nil {
		return nil, err
	}
	return technologies, nil
}

// ListTechnologiesAllUsers returns every account's catalog, with ownership
// populated, for the install-wide local disaster-recovery mirror.
func (r *Repository) ListTechnologiesAllUsers() ([]models.Technology, error) {
	technologies, byID, err := r.readTechnologyCatalog(nil)
	if err != nil {
		return nil, err
	}
	if err := r.populateTechnologyAliases(byID, nil); err != nil {
		return nil, err
	}
	if err := r.populateTechnologyJobs(byID, nil); err != nil {
		return nil, err
	}
	return technologies, nil
}

func (r *Repository) readTechnologyCatalog(scopeUserID *string) ([]models.Technology, map[string]*models.Technology, error) {
	query := `SELECT id, name, user_id FROM technologies`
	var args []interface{}
	if scopeUserID != nil {
		query += " WHERE user_id = ?"
		args = append(args, *scopeUserID)
	}
	query += " ORDER BY name COLLATE NOCASE"
	rows, err := r.reader.Query(query, args...)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = rows.Close() }()
	var technologies []models.Technology
	for rows.Next() {
		var item models.Technology
		var owner sql.NullString
		if err := rows.Scan(&item.ID, &item.Name, &owner); err != nil {
			return nil, nil, err
		}
		if scopeUserID == nil {
			item.UserID = nullableStringPointer(owner)
		}
		item.Aliases = []string{}
		item.Jobs = []models.TechnologyJob{}
		technologies = append(technologies, item)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	byID := make(map[string]*models.Technology, len(technologies))
	for i := range technologies {
		byID[technologies[i].ID] = &technologies[i]
	}
	return technologies, byID, nil
}

func (r *Repository) populateTechnologyAliases(byID map[string]*models.Technology, scopeUserID *string) error {
	query := `SELECT technology_id, alias FROM technology_aliases`
	var args []interface{}
	if scopeUserID != nil {
		query += " WHERE user_id = ?"
		args = append(args, *scopeUserID)
	}
	query += " ORDER BY alias COLLATE NOCASE"
	aliasRows, err := r.reader.Query(query, args...)
	if err != nil {
		return err
	}
	for aliasRows.Next() {
		var id, alias string
		if err := aliasRows.Scan(&id, &alias); err != nil {
			_ = aliasRows.Close()
			return err
		}
		if item := byID[id]; item != nil {
			item.Aliases = append(item.Aliases, alias)
		}
	}
	if err := aliasRows.Err(); err != nil {
		_ = aliasRows.Close()
		return err
	}
	_ = aliasRows.Close()
	return nil
}

func (r *Repository) populateTechnologyJobs(byID map[string]*models.Technology, scopeUserID *string) error {
	query := `SELECT jt.technology_id, j.id, j.company_name, j.position_title FROM job_technologies jt JOIN jobs j ON j.id = jt.job_id`
	var args []interface{}
	if scopeUserID != nil {
		query += " WHERE jt.user_id = ?"
		args = append(args, *scopeUserID)
	}
	query += " ORDER BY j.company_name COLLATE NOCASE, j.position_title COLLATE NOCASE"
	jobRows, err := r.reader.Query(query, args...)
	if err != nil {
		return err
	}
	defer func() { _ = jobRows.Close() }()
	for jobRows.Next() {
		var technologyID string
		var job models.TechnologyJob
		if err := jobRows.Scan(&technologyID, &job.ID, &job.CompanyName, &job.PositionTitle); err != nil {
			return err
		}
		if item := byID[technologyID]; item != nil {
			item.Jobs = append(item.Jobs, job)
		}
	}
	return jobRows.Err()
}

// CreateTechnology inserts a canonical technology and its aliases atomically for userID.
func (r *Repository) CreateTechnology(userID string, item models.Technology) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`INSERT INTO technologies (id, user_id, name, normalized_name) VALUES (?, ?, ?, ?)`, item.ID, userID, item.Name, normalizeCatalogName(item.Name)); err != nil {
		return err
	}
	for _, alias := range item.Aliases {
		if _, err := tx.Exec(`INSERT INTO technology_aliases (technology_id, user_id, alias, normalized_alias) VALUES (?, ?, ?, ?)`, item.ID, userID, alias, normalizeCatalogName(alias)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// UpdateTechnology replaces its canonical name and aliases atomically, if it belongs to userID.
func (r *Repository) UpdateTechnology(userID string, item models.Technology) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.Exec(`UPDATE technologies SET name = ?, normalized_name = ? WHERE id = ? AND user_id = ?`, item.Name, normalizeCatalogName(item.Name), item.ID, userID)
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
	if _, err := tx.Exec(`DELETE FROM technology_aliases WHERE technology_id = ? AND user_id = ?`, item.ID, userID); err != nil {
		return err
	}
	for _, alias := range item.Aliases {
		if _, err := tx.Exec(`INSERT INTO technology_aliases (technology_id, user_id, alias, normalized_alias) VALUES (?, ?, ?, ?)`, item.ID, userID, alias, normalizeCatalogName(alias)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DeleteTechnology removes an unused catalog entry owned by userID.
func (r *Repository) DeleteTechnology(userID, id string) error {
	result, err := r.db.Exec(`DELETE FROM technologies WHERE id = ? AND user_id = ? AND NOT EXISTS (SELECT 1 FROM job_technologies WHERE technology_id = ?)`, id, userID, id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		var exists int
		// Scoped by userID too: a technology ID that exists but belongs to a
		// different account must look exactly like one that does not exist.
		err := r.db.QueryRow(`SELECT 1 FROM technologies WHERE id = ? AND user_id = ?`, id, userID).Scan(&exists)
		if err != nil {
			return err
		}
		return ErrTechnologyInUse
	}
	return nil
}

// RemoveTechnologyAssignments removes an item's assignments from every job owned by userID.
func (r *Repository) RemoveTechnologyAssignments(userID, id string) (int64, error) {
	result, err := r.db.Exec(`DELETE FROM job_technologies WHERE technology_id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// SetJobTechnologies replaces all technology assignments for a job owned by userID.
func (r *Repository) SetJobTechnologies(userID, jobID string, technologyIDs []string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var exists int
	if err := tx.QueryRow(`SELECT 1 FROM jobs WHERE id = ? AND user_id = ?`, jobID, userID).Scan(&exists); err != nil {
		return err
	}
	if err := validateTechnologyAssignments(tx, userID, technologyIDs); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM job_technologies WHERE job_id = ?`, jobID); err != nil {
		return err
	}
	for _, id := range technologyIDs {
		if _, err := tx.Exec(`INSERT INTO job_technologies (job_id, technology_id, user_id) VALUES (?, ?, ?)`, jobID, id, userID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func validateTechnologyAssignments(tx *sql.Tx, userID string, technologyIDs []string) error {
	unique := make(map[string]bool, len(technologyIDs))
	for _, id := range technologyIDs {
		if strings.TrimSpace(id) == "" || unique[id] {
			return fmt.Errorf("invalid or duplicate technology id %q", id)
		}
		unique[id] = true
		var known int
		if err := tx.QueryRow(`SELECT 1 FROM technologies WHERE id = ? AND user_id = ?`, id, userID).Scan(&known); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("technology %q does not exist", id)
			}
			return err
		}
	}
	return nil
}

// JobTechnologies returns canonical catalog items assigned to a job.
func (r *Repository) JobTechnologies(jobID string) ([]models.Technology, error) {
	rows, err := r.reader.Query(`SELECT t.id, t.name, COALESCE((SELECT GROUP_CONCAT(alias, char(10)) FROM (SELECT alias FROM technology_aliases WHERE technology_id = t.id ORDER BY length(alias), alias)), '') FROM technologies t JOIN job_technologies jt ON jt.technology_id = t.id WHERE jt.job_id = ? ORDER BY t.name COLLATE NOCASE`, jobID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := []models.Technology{}
	for rows.Next() {
		var item models.Technology
		var aliases string
		if err := rows.Scan(&item.ID, &item.Name, &aliases); err != nil {
			return nil, err
		}
		item.Aliases = splitTechnologyAliases(aliases)
		items = append(items, item)
	}
	return items, rows.Err()
}

func normalizeCatalogName(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}
