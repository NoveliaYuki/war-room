package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"war-room/backend/pkg/config"
	"war-room/backend/pkg/repository"
	"war-room/backend/pkg/service"
)

func scheduleHandler(t *testing.T) (*Handler, *http.ServeMux, string) {
	t.Helper()
	handler, db, userID := googleHandler(t, false)
	now := time.Date(2026, 11, 13, 8, 0, 0, 0, time.UTC)
	handler.SetBackupScheduleService(service.NewBackupScheduleService(repository.New(db), func() time.Time { return now }))
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	return handler, mux, userID
}

func scheduleRequest(t *testing.T, mux *http.ServeMux, userID, method, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := withUserID(httptest.NewRequest(method, "/api/google/backup-schedule", strings.NewReader(body)), userID)
	request.Header.Set("Origin", "http://localhost:3000")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, request)
	return rec
}

func requireScheduleResponse(t *testing.T, rec *httptest.ResponseRecorder, wantCode int, wantBody string) {
	t.Helper()
	if got := strings.TrimSpace(rec.Body.String()); rec.Code != wantCode || got != wantBody {
		t.Fatalf("response %d %s\nwant     %d %s", rec.Code, got, wantCode, wantBody)
	}
}

func TestBackupScheduleEndpointsSaveWithoutDriveConnected(t *testing.T) {
	_, mux, userID := scheduleHandler(t)
	requireScheduleResponse(t, scheduleRequest(t, mux, userID, http.MethodGet, ""), http.StatusOK,
		`{"frequency":"off","weekday":1,"time":"09:00","time_zone":"UTC","next_run_at":null,"last_run_at":null,"last_run_result":""}`)
	saved := `{"frequency":"weekly","weekday":1,"time":"09:00","time_zone":"Europe/Madrid","next_run_at":"2026-11-16T08:00:00Z","last_run_at":null,"last_run_result":""}`
	requireScheduleResponse(t, scheduleRequest(t, mux, userID, http.MethodPut, `{"frequency":"weekly","weekday":1,"time":"09:00","time_zone":"Europe/Madrid"}`), http.StatusOK, saved)
	requireScheduleResponse(t, scheduleRequest(t, mux, userID, http.MethodGet, ""), http.StatusOK, saved)
}

func TestBackupScheduleEndpointRejectsInvalidInput(t *testing.T) {
	_, mux, userID := scheduleHandler(t)
	cases := map[string]string{
		`{"frequency":"* * * * *","weekday":1,"time":"09:00","time_zone":"UTC"}`:    "Choose Off, Every day, or Every week",
		`{"frequency":"weekly","weekday":8,"time":"09:00","time_zone":"UTC"}`:       "Choose a day of the week",
		`{"frequency":"daily","weekday":1,"time":"25:00","time_zone":"UTC"}`:        "Enter the backup time as hours and minutes",
		`{"frequency":"daily","weekday":1,"time":"09:00","time_zone":"Nowhere/X"}`:  "Your browser's time zone was not recognized",
		`{"frequency":"daily","weekday":1,"time":"09:00","time_zone":"UTC"} {}`:     "Invalid JSON payload",
		`{"frequency":"daily","weekday":"monday","time":"09:00","time_zone":"UTC"}`: "Invalid JSON payload",
		`{"frequency":"daily","padding":"` + strings.Repeat("x", 5000) + `"}`:       "Invalid JSON payload",
	}
	for body, message := range cases {
		rec := scheduleRequest(t, mux, userID, http.MethodPut, body)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), message) {
			t.Errorf("body %.60s: status=%d response=%s, want %q", body, rec.Code, rec.Body.String(), message)
		}
	}
	if rec := scheduleRequest(t, mux, userID, http.MethodGet, ""); !strings.Contains(rec.Body.String(), `"frequency":"off"`) {
		t.Fatalf("rejected input must not be stored: %s", rec.Body.String())
	}
}

func TestBackupScheduleEndpointRequiresSessionOriginAndService(t *testing.T) {
	handler, mux, userID := scheduleHandler(t)

	request := withUserID(httptest.NewRequest(http.MethodPut, "/api/google/backup-schedule", strings.NewReader(`{}`)), userID)
	request.Header.Set("Origin", "https://attacker.example")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, request)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-site schedule change status=%d", rec.Code)
	}

	for _, method := range []string{http.MethodGet, http.MethodPut} {
		rec = httptest.NewRecorder()
		handler.handleGetBackupSchedule(rec, httptest.NewRequest(method, "/api/google/backup-schedule", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s without session status=%d", method, rec.Code)
		}
	}

	bare := NewHandler(nil, nil, &config.Config{GoogleFrontendURL: "http://localhost:3000"})
	for _, call := range []func(http.ResponseWriter, *http.Request){bare.handleGetBackupSchedule, bare.handleSaveBackupSchedule} {
		rec = httptest.NewRecorder()
		call(rec, withUserID(httptest.NewRequest(http.MethodPut, "/api/google/backup-schedule", strings.NewReader(`{}`)), "user-1"))
		if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "Scheduled backups are unavailable") {
			t.Fatalf("missing schedule service status=%d body=%s", rec.Code, rec.Body.String())
		}
	}
}

func TestBackupScheduleEndpointsHideStorageErrors(t *testing.T) {
	handler, db, userID := googleHandler(t, false)
	handler.SetBackupScheduleService(service.NewBackupScheduleService(repository.New(db), nil))
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	if _, err := db.Exec(`DROP TABLE backup_schedules`); err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		rec := scheduleRequest(t, mux, userID, method, `{"frequency":"daily","weekday":1,"time":"09:00","time_zone":"UTC"}`)
		if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "backup_schedules") {
			t.Fatalf("%s storage error status=%d body=%s", method, rec.Code, rec.Body.String())
		}
	}
}

func TestGoogleErrorExplainsExpiredAccess(t *testing.T) {
	rec := httptest.NewRecorder()
	expired := &service.GoogleAPIError{Operation: "token request", Status: 400, Reason: "invalid_grant"}
	googleError(rec, fmt.Errorf("refresh access token: %w", expired))
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "Google access expired or was revoked") {
		t.Fatalf("expired access status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	googleError(rec, errors.New("other"))
	if !strings.Contains(rec.Body.String(), "Google Drive request failed") {
		t.Fatalf("other failures keep the generic message: %s", rec.Body.String())
	}
}

func TestSigningInAgainClearsExpiredAccessResult(t *testing.T) {
	handler, drive, _ := fakeGoogleHandler(t)
	db := googleHandlerDB(t, handler)
	schedules := service.NewBackupScheduleService(repository.New(db), nil)
	handler.SetBackupScheduleService(schedules)
	userID := connectFakeGoogle(t, handler, drive)
	if _, err := schedules.Save(userID, service.BackupScheduleInput{Frequency: "daily", Time: "09:00", TimeZone: "UTC"}); err != nil {
		t.Fatal(err)
	}
	if err := repository.New(db).RecordBackupScheduleRun(userID, time.Now(), service.BackupResultGoogleAccessExpired); err != nil {
		t.Fatal(err)
	}
	connectFakeGoogle(t, handler, drive)
	schedule, err := schedules.Get(userID)
	if err != nil || schedule.LastRunResult != "" {
		t.Fatalf("signing in again should clear the expired-access result, got %q err=%v", schedule.LastRunResult, err)
	}

	if _, err := db.Exec(`DROP TABLE backup_schedules`); err != nil {
		t.Fatal(err)
	}
	connectFakeGoogle(t, handler, drive) // a failed clear is logged and never blocks sign-in
}
