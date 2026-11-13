package service

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"time"

	// Embeds the IANA time zone database so schedules in any zone work even
	// where the host or test image has no zoneinfo files.
	_ "time/tzdata"

	"war-room/backend/pkg/repository"
)

// Backup schedule frequencies. Off is the default for every account.
const (
	BackupFrequencyOff    = "off"
	BackupFrequencyDaily  = "daily"
	BackupFrequencyWeekly = "weekly"
)

// Results recorded for the last automatic backup. They are codes, not
// sentences: the interface shows each one in the selected language.
const (
	BackupResultOK                  = "ok"
	BackupResultFailed              = "failed"
	BackupResultGoogleAccessExpired = "google_access_expired"
	BackupResultDriveNotConnected   = "drive_not_connected"
	BackupResultDriveNotConfigured  = "drive_not_configured"
)

const maxTimeZoneLength = 64

// Validation errors for a backup schedule sent by a client.
var (
	ErrBackupScheduleFrequency = errors.New("backup schedule frequency must be off, daily, or weekly")
	ErrBackupScheduleWeekday   = errors.New("backup schedule weekday must be 0 (Sunday) through 6 (Saturday)")
	ErrBackupScheduleTime      = errors.New("backup schedule time must be HH:MM in 24-hour format")
	ErrBackupScheduleTimeZone  = errors.New("backup schedule time zone must be an IANA time zone name")
)

var timeOfDayPattern = regexp.MustCompile(`^([01][0-9]|2[0-3]):([0-5][0-9])$`)

// BackupScheduleInput is the schedule a user saves. Time is the local time of
// day as HH:MM and TimeZone is the browser's IANA zone, such as Europe/Madrid.
type BackupScheduleInput struct {
	Frequency string `json:"frequency"`
	Weekday   int    `json:"weekday"`
	Time      string `json:"time"`
	TimeZone  string `json:"time_zone"`
}

// BackupSchedule is one account's automatic backup schedule and the outcome
// of its last automatic run. Weekday follows Go and JavaScript: 0 is Sunday.
type BackupSchedule struct {
	Frequency     string     `json:"frequency"`
	Weekday       int        `json:"weekday"`
	Time          string     `json:"time"`
	TimeZone      string     `json:"time_zone"`
	NextRunAt     *time.Time `json:"next_run_at"`
	LastRunAt     *time.Time `json:"last_run_at"`
	LastRunResult string     `json:"last_run_result"`
}

// defaultBackupSchedule is what an account without a saved schedule sees.
func defaultBackupSchedule() BackupSchedule {
	return BackupSchedule{Frequency: BackupFrequencyOff, Weekday: int(time.Monday), Time: "09:00", TimeZone: "UTC"}
}

// scheduleSettings is a validated schedule ready for next-run arithmetic.
type scheduleSettings struct {
	frequency   string
	weekday     time.Weekday
	minuteOfDay int
	location    *time.Location
}

// parseBackupScheduleInput validates every field of untrusted input.
func parseBackupScheduleInput(input BackupScheduleInput) (scheduleSettings, error) {
	if input.Frequency != BackupFrequencyOff && input.Frequency != BackupFrequencyDaily && input.Frequency != BackupFrequencyWeekly {
		return scheduleSettings{}, ErrBackupScheduleFrequency
	}
	if input.Weekday < 0 || input.Weekday > 6 {
		return scheduleSettings{}, ErrBackupScheduleWeekday
	}
	minuteOfDay, ok := parseTimeOfDay(input.Time)
	if !ok {
		return scheduleSettings{}, ErrBackupScheduleTime
	}
	location, err := loadScheduleLocation(input.TimeZone)
	if err != nil {
		return scheduleSettings{}, err
	}
	return scheduleSettings{frequency: input.Frequency, weekday: time.Weekday(input.Weekday), minuteOfDay: minuteOfDay, location: location}, nil
}

func parseTimeOfDay(value string) (int, bool) {
	match := timeOfDayPattern.FindStringSubmatch(value)
	if match == nil {
		return 0, false
	}
	hours, _ := strconv.Atoi(match[1])
	minutes, _ := strconv.Atoi(match[2])
	return hours*60 + minutes, true
}

// loadScheduleLocation accepts only named IANA zones. "Local" is refused
// because it would mean the server's zone, not the user's.
func loadScheduleLocation(name string) (*time.Location, error) {
	if name == "" || name == "Local" || len(name) > maxTimeZoneLength {
		return nil, ErrBackupScheduleTimeZone
	}
	location, err := time.LoadLocation(name)
	if err != nil {
		return nil, ErrBackupScheduleTimeZone
	}
	return location, nil
}

// nextRun returns the first scheduled moment strictly after after, or nil when
// the schedule is off. It steps through calendar days in the user's zone so
// the wall-clock time stays fixed across daylight-saving changes; a time that
// does not exist on a spring-forward day runs at the shifted time Go picks.
func (s scheduleSettings) nextRun(after time.Time) *time.Time {
	if s.frequency == BackupFrequencyOff {
		return nil
	}
	local := after.In(s.location)
	for day := 0; day <= 8; day++ {
		candidate := time.Date(local.Year(), local.Month(), local.Day()+day, s.minuteOfDay/60, s.minuteOfDay%60, 0, 0, s.location)
		if s.frequency == BackupFrequencyWeekly && candidate.Weekday() != s.weekday {
			continue
		}
		if candidate.After(after) {
			next := candidate.UTC()
			return &next
		}
	}
	return nil
}

func (s scheduleSettings) row(userID string, next *time.Time) repository.BackupScheduleRow {
	return repository.BackupScheduleRow{
		UserID: userID, Frequency: s.frequency, Weekday: int(s.weekday), MinuteOfDay: s.minuteOfDay,
		TimeZone: s.location.String(), NextRunAt: next,
	}
}

func settingsFromRow(row repository.BackupScheduleRow) (scheduleSettings, error) {
	return parseBackupScheduleInput(BackupScheduleInput{
		Frequency: row.Frequency, Weekday: row.Weekday, Time: formatTimeOfDay(row.MinuteOfDay), TimeZone: row.TimeZone,
	})
}

func formatTimeOfDay(minuteOfDay int) string {
	return fmt.Sprintf("%02d:%02d", minuteOfDay/60, minuteOfDay%60)
}

func scheduleFromRow(row repository.BackupScheduleRow) BackupSchedule {
	return BackupSchedule{
		Frequency: row.Frequency, Weekday: row.Weekday, Time: formatTimeOfDay(row.MinuteOfDay), TimeZone: row.TimeZone,
		NextRunAt: row.NextRunAt, LastRunAt: row.LastRunAt, LastRunResult: row.LastRunResult,
	}
}

// BackupScheduleService stores each account's automatic backup schedule.
type BackupScheduleService struct {
	repo *repository.Repository
	now  func() time.Time
}

// NewBackupScheduleService creates the service; now supplies the current time.
func NewBackupScheduleService(repo *repository.Repository, now func() time.Time) *BackupScheduleService {
	if now == nil {
		now = time.Now
	}
	return &BackupScheduleService{repo: repo, now: now}
}

// Get returns userID's schedule, or the Off default when none was saved.
func (s *BackupScheduleService) Get(userID string) (BackupSchedule, error) {
	row, found, err := s.repo.GetBackupSchedule(userID)
	if err != nil {
		return BackupSchedule{}, err
	}
	if !found {
		return defaultBackupSchedule(), nil
	}
	return scheduleFromRow(row), nil
}

// Save validates input, stores it for userID, and computes the next run from now.
func (s *BackupScheduleService) Save(userID string, input BackupScheduleInput) (BackupSchedule, error) {
	settings, err := parseBackupScheduleInput(input)
	if err != nil {
		return BackupSchedule{}, err
	}
	if err := s.repo.SaveBackupSchedule(settings.row(userID, settings.nextRun(s.now()))); err != nil {
		return BackupSchedule{}, err
	}
	return s.Get(userID)
}

// ClearGoogleAccessFailure forgets a recorded expired-access failure once the
// user has signed in again and granted fresh Google access.
func (s *BackupScheduleService) ClearGoogleAccessFailure(userID string) error {
	return s.repo.ClearBackupScheduleResult(userID, BackupResultGoogleAccessExpired)
}
