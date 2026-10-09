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

// ListTechnologies returns the catalog and the jobs currently using each item.
func (r *Repository) ListTechnologies() ([]models.Technology, error) {
	technologies, byID, err := r.readTechnologyCatalog()
	if err != nil {
		return nil, err
	}
	if err := r.populateTechnologyAliases(byID); err != nil {
		return nil, err
	}
	if err := r.populateTechnologyJobs(byID); err != nil {
		return nil, err
	}
	return technologies, nil
}

func (r *Repository) readTechnologyCatalog() ([]models.Technology, map[string]*models.Technology, error) {
	rows, err := r.reader.Query(`SELECT id, name FROM technologies ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = rows.Close() }()
	var technologies []models.Technology
	for rows.Next() {
		var item models.Technology
		if err := rows.Scan(&item.ID, &item.Name); err != nil {
			return nil, nil, err
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

func (r *Repository) populateTechnologyAliases(byID map[string]*models.Technology) error {
	aliasRows, err := r.reader.Query(`SELECT technology_id, alias FROM technology_aliases ORDER BY alias COLLATE NOCASE`)
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

func (r *Repository) populateTechnologyJobs(byID map[string]*models.Technology) error {
	jobRows, err := r.reader.Query(`SELECT jt.technology_id, j.id, j.company_name, j.position_title FROM job_technologies jt JOIN jobs j ON j.id = jt.job_id ORDER BY j.company_name COLLATE NOCASE, j.position_title COLLATE NOCASE`)
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

// CreateTechnology inserts a canonical technology and its aliases atomically.
func (r *Repository) CreateTechnology(item models.Technology) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`INSERT INTO technologies (id, name, normalized_name) VALUES (?, ?, ?)`, item.ID, item.Name, normalizeCatalogName(item.Name)); err != nil {
		return err
	}
	for _, alias := range item.Aliases {
		if _, err := tx.Exec(`INSERT INTO technology_aliases (technology_id, alias, normalized_alias) VALUES (?, ?, ?)`, item.ID, alias, normalizeCatalogName(alias)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// UpdateTechnology replaces its canonical name and aliases atomically.
func (r *Repository) UpdateTechnology(item models.Technology) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.Exec(`UPDATE technologies SET name = ?, normalized_name = ? WHERE id = ?`, item.Name, normalizeCatalogName(item.Name), item.ID)
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
	if _, err := tx.Exec(`DELETE FROM technology_aliases WHERE technology_id = ?`, item.ID); err != nil {
		return err
	}
	for _, alias := range item.Aliases {
		if _, err := tx.Exec(`INSERT INTO technology_aliases (technology_id, alias, normalized_alias) VALUES (?, ?, ?)`, item.ID, alias, normalizeCatalogName(alias)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DeleteTechnology removes an unused catalog entry.
func (r *Repository) DeleteTechnology(id string) error {
	result, err := r.db.Exec(`DELETE FROM technologies WHERE id = ? AND NOT EXISTS (SELECT 1 FROM job_technologies WHERE technology_id = ?)`, id, id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		var exists int
		if err := r.db.QueryRow(`SELECT 1 FROM technologies WHERE id = ?`, id).Scan(&exists); err != nil {
			return err
		}
		return ErrTechnologyInUse
	}
	return nil
}

// RemoveTechnologyAssignments removes an item's assignments from every job.
func (r *Repository) RemoveTechnologyAssignments(id string) (int64, error) {
	result, err := r.db.Exec(`DELETE FROM job_technologies WHERE technology_id = ?`, id)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// SetJobTechnologies replaces all technology assignments for a job.
func (r *Repository) SetJobTechnologies(jobID string, technologyIDs []string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var exists int
	if err := tx.QueryRow(`SELECT 1 FROM jobs WHERE id = ?`, jobID).Scan(&exists); err != nil {
		return err
	}
	if err := validateTechnologyAssignments(tx, technologyIDs); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM job_technologies WHERE job_id = ?`, jobID); err != nil {
		return err
	}
	for _, id := range technologyIDs {
		if _, err := tx.Exec(`INSERT INTO job_technologies (job_id, technology_id) VALUES (?, ?)`, jobID, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func validateTechnologyAssignments(tx *sql.Tx, technologyIDs []string) error {
	unique := make(map[string]bool, len(technologyIDs))
	for _, id := range technologyIDs {
		if strings.TrimSpace(id) == "" || unique[id] {
			return fmt.Errorf("invalid or duplicate technology id %q", id)
		}
		unique[id] = true
		var known int
		if err := tx.QueryRow(`SELECT 1 FROM technologies WHERE id = ?`, id).Scan(&known); err != nil {
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
