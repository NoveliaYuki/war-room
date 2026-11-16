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

// ListSearchPeriods returns userID's periods with their job counts, newest periods first.
func (r *Repository) ListSearchPeriods(userID string) ([]models.SearchPeriod, error) {
	rows, err := r.reader.Query(`SELECT p.id, p.name, p.start_date, p.end_date, p.created_at, p.updated_at, COUNT(j.id)
		FROM search_periods p LEFT JOIN jobs j ON j.search_period_id = p.id AND j.user_id = p.user_id
		WHERE p.user_id = ?
		GROUP BY p.id ORDER BY p.start_date DESC, p.created_at DESC`, userID)
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

// ListSearchPeriodsAllUsers returns every account's periods, with ownership
// populated, for the install-wide local disaster-recovery mirror.
func (r *Repository) ListSearchPeriodsAllUsers() ([]models.SearchPeriod, error) {
	rows, err := r.reader.Query(`SELECT p.id, p.name, p.start_date, p.end_date, p.created_at, p.updated_at, COUNT(j.id), p.user_id
		FROM search_periods p LEFT JOIN jobs j ON j.search_period_id = p.id
		GROUP BY p.id ORDER BY p.start_date DESC, p.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	periods := []models.SearchPeriod{}
	for rows.Next() {
		var period models.SearchPeriod
		var owner sql.NullString
		if err := rows.Scan(&period.ID, &period.Name, &period.StartDate, &period.EndDate, &period.CreatedAt, &period.UpdatedAt, &period.JobCount, &owner); err != nil {
			return nil, err
		}
		period.UserID = nullableStringPointer(owner)
		periods = append(periods, period)
	}
	return periods, rows.Err()
}

// GetSearchPeriod returns the period with id owned by userID, or nil when it does not exist.
func (r *Repository) GetSearchPeriod(userID, id string) (*models.SearchPeriod, error) {
	var period models.SearchPeriod
	err := r.reader.QueryRow(`SELECT id, name, start_date, end_date, created_at, updated_at FROM search_periods WHERE id = ? AND user_id = ?`, id, userID).
		Scan(&period.ID, &period.Name, &period.StartDate, &period.EndDate, &period.CreatedAt, &period.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &period, err
}

// SearchPeriodForDate returns the one period owned by userID that contains date, if present.
func (r *Repository) SearchPeriodForDate(userID, date string) (*models.SearchPeriod, error) {
	var period models.SearchPeriod
	err := r.reader.QueryRow(`SELECT id, name, start_date, end_date, created_at, updated_at FROM search_periods WHERE user_id = ? AND start_date <= ? AND COALESCE(NULLIF(end_date, ''), '9999-12-31') >= ?`, userID, date, date).
		Scan(&period.ID, &period.Name, &period.StartDate, &period.EndDate, &period.CreatedAt, &period.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &period, err
}

// CreateSearchPeriod inserts a period for userID after checking that its range is unique among their own periods.
func (r *Repository) CreateSearchPeriod(userID string, period *models.SearchPeriod) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := ensureNoPeriodOverlap(tx, userID, period.StartDate, period.EndDate, ""); err != nil {
		return err
	}
	now := time.Now().Unix()
	period.CreatedAt, period.UpdatedAt = now, now
	if _, err := tx.Exec(`INSERT INTO search_periods (id, user_id, name, start_date, end_date, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, period.ID, userID, period.Name, period.StartDate, period.EndDate, now, now); err != nil {
		return err
	}
	if err := assignUnassignedJobsInPeriod(tx, userID, period.StartDate, period.EndDate, period.ID); err != nil {
		return err
	}
	return tx.Commit()
}

// UpdateSearchPeriod changes period details while preserving job assignments, if it belongs to userID.
func (r *Repository) UpdateSearchPeriod(userID, id string, period models.SearchPeriodInput) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := ensureNoPeriodOverlap(tx, userID, period.StartDate, period.EndDate, id); err != nil {
		return err
	}
	result, err := tx.Exec(`UPDATE search_periods SET name = ?, start_date = ?, end_date = ?, updated_at = unixepoch() WHERE id = ? AND user_id = ?`, period.Name, period.StartDate, period.EndDate, id, userID)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return sql.ErrNoRows
	}
	if err := assignUnassignedJobsInPeriod(tx, userID, period.StartDate, period.EndDate, id); err != nil {
		return err
	}
	return tx.Commit()
}

func ensureNoPeriodOverlap(tx *sql.Tx, userID, startDate, endDate, excludeID string) error {
	var overlap int
	if endDate == "" {
		endDate = "9999-12-31"
	}
	err := tx.QueryRow(`SELECT COUNT(*) FROM search_periods WHERE user_id = ? AND start_date <= ? AND COALESCE(NULLIF(end_date, ''), '9999-12-31') >= ? AND id <> ?`, userID, endDate, startDate, excludeID).Scan(&overlap)
	if err != nil {
		return fmt.Errorf("check overlapping search periods: %w", err)
	}
	if overlap > 0 {
		return ErrSearchPeriodOverlap
	}
	return nil
}

func assignUnassignedJobsInPeriod(tx *sql.Tx, userID, startDate, endDate, periodID string) error {
	start, err := time.ParseInLocation("2006-01-02", startDate, time.Local)
	if err != nil {
		return fmt.Errorf("parse search period start date: %w", err)
	}
	if endDate == "" {
		_, err = tx.Exec(`UPDATE jobs SET search_period_id = ?, updated_at = unixepoch()
			WHERE user_id = ? AND search_period_id IS NULL AND created_at >= ?`, periodID, userID, start.Unix())
		return err
	}
	end, err := time.ParseInLocation("2006-01-02", endDate, time.Local)
	if err != nil {
		return fmt.Errorf("parse search period end date: %w", err)
	}
	end = end.AddDate(0, 0, 1)
	_, err = tx.Exec(`UPDATE jobs SET search_period_id = ?, updated_at = unixepoch()
		WHERE user_id = ? AND search_period_id IS NULL AND created_at >= ? AND created_at < ?`, periodID, userID, start.Unix(), end.Unix())
	return err
}

// DeleteSearchPeriod deletes the period owned by userID; the foreign key leaves its jobs unassigned.
func (r *Repository) DeleteSearchPeriod(userID, id string) error {
	result, err := r.db.Exec(`DELETE FROM search_periods WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return sql.ErrNoRows
	}
	return nil
}
