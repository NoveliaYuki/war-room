package repository

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"war-room/backend/pkg/models"
)

// ErrSearchPeriodOverlap indicates that a period conflicts with another period's dates.
var ErrSearchPeriodOverlap = errors.New("search period dates overlap another period")

// ListSearchPeriods returns periods with their job counts, newest periods first.
func (r *Repository) ListSearchPeriods() ([]models.SearchPeriod, error) {
	rows, err := r.reader.Query(`SELECT p.id, p.name, p.start_date, p.end_date, p.created_at, p.updated_at, COUNT(j.id)
		FROM search_periods p LEFT JOIN jobs j ON j.search_period_id = p.id
		GROUP BY p.id ORDER BY p.start_date DESC, p.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	periods := []models.SearchPeriod{}
	for rows.Next() {
		var period models.SearchPeriod
		if err := rows.Scan(&period.ID, &period.Name, &period.StartDate, &period.EndDate, &period.CreatedAt, &period.UpdatedAt, &period.JobCount); err != nil {
			return nil, err
		}
		periods = append(periods, period)
	}
	return periods, rows.Err()
}

// GetSearchPeriod returns the period with id, or nil when it does not exist.
func (r *Repository) GetSearchPeriod(id string) (*models.SearchPeriod, error) {
	var period models.SearchPeriod
	err := r.reader.QueryRow(`SELECT id, name, start_date, end_date, created_at, updated_at FROM search_periods WHERE id = ?`, id).
		Scan(&period.ID, &period.Name, &period.StartDate, &period.EndDate, &period.CreatedAt, &period.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &period, err
}

// SearchPeriodForDate returns the one period containing date, if present.
func (r *Repository) SearchPeriodForDate(date string) (*models.SearchPeriod, error) {
	var period models.SearchPeriod
	err := r.reader.QueryRow(`SELECT id, name, start_date, end_date, created_at, updated_at FROM search_periods WHERE start_date <= ? AND end_date >= ?`, date, date).
		Scan(&period.ID, &period.Name, &period.StartDate, &period.EndDate, &period.CreatedAt, &period.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &period, err
}

// CreateSearchPeriod inserts a period after checking that its range is unique.
func (r *Repository) CreateSearchPeriod(period *models.SearchPeriod) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := ensureNoPeriodOverlap(tx, period.StartDate, period.EndDate, ""); err != nil {
		return err
	}
	now := time.Now().Unix()
	period.CreatedAt, period.UpdatedAt = now, now
	if _, err := tx.Exec(`INSERT INTO search_periods (id, name, start_date, end_date, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`, period.ID, period.Name, period.StartDate, period.EndDate, now, now); err != nil {
		return err
	}
	return tx.Commit()
}

// UpdateSearchPeriod changes period details while preserving job assignments.
func (r *Repository) UpdateSearchPeriod(id string, period models.SearchPeriodInput) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := ensureNoPeriodOverlap(tx, period.StartDate, period.EndDate, id); err != nil {
		return err
	}
	result, err := tx.Exec(`UPDATE search_periods SET name = ?, start_date = ?, end_date = ?, updated_at = unixepoch() WHERE id = ?`, period.Name, period.StartDate, period.EndDate, id)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}

func ensureNoPeriodOverlap(tx *sql.Tx, startDate, endDate, excludeID string) error {
	var overlap int
	err := tx.QueryRow(`SELECT COUNT(*) FROM search_periods WHERE start_date <= ? AND end_date >= ? AND id <> ?`, endDate, startDate, excludeID).Scan(&overlap)
	if err != nil {
		return fmt.Errorf("check overlapping search periods: %w", err)
	}
	if overlap > 0 {
		return ErrSearchPeriodOverlap
	}
	return nil
}

// DeleteSearchPeriod deletes the period; the foreign key leaves its jobs unassigned.
func (r *Repository) DeleteSearchPeriod(id string) error {
	result, err := r.db.Exec(`DELETE FROM search_periods WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return sql.ErrNoRows
	}
	return nil
}
