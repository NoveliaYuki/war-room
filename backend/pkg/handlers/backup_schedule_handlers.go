package handlers

import (
	"errors"
	"fmt"
	"net/http"

	"war-room/backend/pkg/logging"
	"war-room/backend/pkg/service"
)

// SetBackupScheduleService enables the automatic Drive backup schedule endpoints.
func (h *Handler) SetBackupScheduleService(schedules *service.BackupScheduleService) {
	h.schedules = schedules
}

// backupScheduleMessages is the public message for each invalid schedule field.
var backupScheduleMessages = []struct {
	Err     error
	Message string
}{
	{Err: service.ErrBackupScheduleFrequency, Message: "Choose Off, Every day, or Every week"},
	{Err: service.ErrBackupScheduleWeekday, Message: "Choose a day of the week"},
	{Err: service.ErrBackupScheduleTime, Message: "Enter the backup time as hours and minutes"},
	{Err: service.ErrBackupScheduleTimeZone, Message: "Your browser's time zone was not recognized"},
}

func (h *Handler) scheduleUser(w http.ResponseWriter, r *http.Request) (string, bool) {
	userID, ok := h.authUserID(r)
	if !ok {
		ErrorJSON(w, http.StatusUnauthorized, "sign in required")
		return "", false
	}
	if h.schedules == nil {
		ErrorJSON(w, http.StatusServiceUnavailable, "Scheduled backups are unavailable")
		return "", false
	}
	return userID, true
}

// handleGetBackupSchedule returns the signed-in user's automatic backup
// schedule, its next run, and the result of its last automatic run.
func (h *Handler) handleGetBackupSchedule(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.scheduleUser(w, r)
	if !ok {
		return
	}
	schedule, err := h.schedules.Get(userID)
	if err != nil {
		internalError(w, fmt.Errorf("load backup schedule: %w", err))
		return
	}
	JSON(w, http.StatusOK, schedule)
}

// handleSaveBackupSchedule stores the signed-in user's automatic backup
// schedule. Saving works whatever the Drive connection state: it is a
// preference, and the scheduler records why a due run could not upload.
func (h *Handler) handleSaveBackupSchedule(w http.ResponseWriter, r *http.Request) {
	if !h.validGoogleRequestOrigin(r) {
		rejectGoogleOrigin(w, r)
		return
	}
	userID, ok := h.scheduleUser(w, r)
	if !ok {
		return
	}
	var input service.BackupScheduleInput
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := decodeJSONBody(r.Body, &input); err != nil {
		invalidPayload(w, err)
		return
	}
	schedule, err := h.schedules.Save(userID, input)
	if err != nil {
		writeBackupScheduleError(w, err)
		return
	}
	logging.Audit(r.Context(), "backup.schedule_changed", "frequency", schedule.Frequency)
	JSON(w, http.StatusOK, schedule)
}

func writeBackupScheduleError(w http.ResponseWriter, err error) {
	for _, validation := range backupScheduleMessages {
		if errors.Is(err, validation.Err) {
			publicError(w, http.StatusBadRequest, validation.Message, err)
			return
		}
	}
	internalError(w, fmt.Errorf("save backup schedule: %w", err))
}
