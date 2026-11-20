package repository

import (
	"database/sql"
	"errors"
	"time"
)

// BackupScheduleRow is one account's stored automatic Drive backup schedule.
// NextRunAt is nil exactly when Frequency is "off".
type BackupScheduleRow struct {
	UserID        string
	Frequency     string
	Weekday       int
	MinuteOfDay   int
	TimeZone      string
	NextRunAt     *time.Time
	LastRunAt     *time.Time
	LastRunResult string
}

const backupScheduleColumns = `user_id, frequency, weekday, minute_of_day, time_zone, next_run_at, last_run_at, last_run_result`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanBackupSchedule(scanner rowScanner) (BackupScheduleRow, error) {
	var row BackupScheduleRow
	var nextRunAt, lastRunAt sql.NullInt64
	err := scanner.Scan(&row.UserID, &row.Frequency, &row.Weekday, &row.MinuteOfDay, &row.TimeZone, &nextRunAt, &lastRunAt, &row.LastRunResult)
	row.NextRunAt, row.LastRunAt = unixTime(nextRunAt), unixTime(lastRunAt)
	return row, err
}

func unixTime(value sql.NullInt64) *time.Time {
	if !value.Valid {
		return nil
	}
	parsed := time.Unix(value.Int64, 0).UTC()
	return &parsed
}

func unixValue(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.Unix()
}

// GetBackupSchedule returns userID's schedule; found is false when the account
// has never saved one, which means automatic backups are off.
func (r *Repository) GetBackupSchedule(userID string) (BackupScheduleRow, bool, error) {
	row, err := scanBackupSchedule(r.reader.QueryRow(`SELECT `+backupScheduleColumns+` FROM backup_schedules WHERE user_id = ?`, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return BackupScheduleRow{}, false, nil
	}
	return row, err == nil, err
}

// SaveBackupSchedule stores row's settings and next run for its user,
// keeping the result of the last automatic run.
func (r *Repository) SaveBackupSchedule(row BackupScheduleRow) error {
	_, err := r.db.Exec(`INSERT INTO backup_schedules (user_id, frequency, weekday, minute_of_day, time_zone, next_run_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, unixepoch())
		ON CONFLICT(user_id) DO UPDATE SET
			frequency = excluded.frequency, weekday = excluded.weekday, minute_of_day = excluded.minute_of_day,
			time_zone = excluded.time_zone, next_run_at = excluded.next_run_at, updated_at = unixepoch()`,
		row.UserID, row.Frequency, row.Weekday, row.MinuteOfDay, row.TimeZone, unixValue(row.NextRunAt))
	return err
}

// ListDueBackupSchedules returns up to limit schedules whose next run is at or
// before now, oldest first.
func (r *Repository) ListDueBackupSchedules(now time.Time, limit int) ([]BackupScheduleRow, error) {
	rows, err := r.reader.Query(`SELECT `+backupScheduleColumns+` FROM backup_schedules
		WHERE next_run_at IS NOT NULL AND next_run_at <= ? ORDER BY next_run_at, user_id LIMIT ?`, now.Unix(), limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	due := []BackupScheduleRow{}
	for rows.Next() {
		row, err := scanBackupSchedule(rows)
		if err != nil {
			return nil, err
		}
		due = append(due, row)
	}
	return due, rows.Err()
}

// ClaimBackupScheduleRun moves userID's next run from expected to next and
// reports whether this caller won it. The compare-and-set means a run is
// started at most once even when ticks overlap or the user edits the schedule
// at the same moment; a schedule claimed before a crash is not run twice.
func (r *Repository) ClaimBackupScheduleRun(userID string, expected, next time.Time) (bool, error) {
	result, err := r.db.Exec(`UPDATE backup_schedules SET next_run_at = ? WHERE user_id = ? AND next_run_at = ? AND frequency <> 'off'`,
		next.Unix(), userID, expected.Unix())
	if err != nil {
		return false, err
	}
	changed, err := result.RowsAffected()
	return changed == 1, err
}

// RecordBackupScheduleRun stores when an automatic run finished and its result code.
func (r *Repository) RecordBackupScheduleRun(userID string, at time.Time, result string) error {
	_, err := r.db.Exec(`UPDATE backup_schedules SET last_run_at = ?, last_run_result = ? WHERE user_id = ?`, at.Unix(), result, userID)
	return err
}

// ClearBackupScheduleResult forgets userID's last automatic run result when it
// equals result, so a failure the user has since fixed stops being shown.
func (r *Repository) ClearBackupScheduleResult(userID, result string) error {
	_, err := r.db.Exec(`UPDATE backup_schedules SET last_run_result = '' WHERE user_id = ? AND last_run_result = ?`, userID, result)
	return err
}
