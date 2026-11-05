package handlers

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"war-room/backend/pkg/logging"
	"war-room/backend/pkg/middleware"
	"war-room/backend/pkg/service"
)

func serveLogged(t *testing.T, write func(http.ResponseWriter)) (*httptest.ResponseRecorder, string) {
	t.Helper()
	previous := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previous) })
	var out bytes.Buffer
	if err := logging.Setup(&out, "debug", "json"); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	middleware.RequestLogger(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { write(w) })).
		ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/google/backup", nil))
	return recorder, out.String()
}

func TestGoogleErrorHidesUpstreamDetailButLogsIt(t *testing.T) {
	upstream := &service.GoogleAPIError{Operation: "drive upload initialization", Status: 403, Reason: "storageQuotaExceeded"}
	recorder, logs := serveLogged(t, func(w http.ResponseWriter) { googleError(w, fmt.Errorf("upload: %w", upstream)) })
	body := recorder.Body.String()
	if recorder.Code != http.StatusBadGateway || strings.Contains(body, "403") || strings.Contains(body, "storageQuotaExceeded") {
		t.Fatalf("upstream detail leaked to client: %d %s", recorder.Code, body)
	}
	if !strings.Contains(body, recorder.Header().Get(middleware.RequestIDHeader)) {
		t.Fatalf("expected request ID in body: %s", body)
	}
	if !strings.Contains(logs, "storageQuotaExceeded") {
		t.Fatalf("expected upstream reason in logs: %s", logs)
	}
}

func TestGoogleErrorKeepsPublicSentinels(t *testing.T) {
	for err, status := range map[error]int{
		service.ErrGoogleNotConfigured:    http.StatusServiceUnavailable,
		service.ErrGoogleNotConnected:     http.StatusConflict,
		service.ErrGoogleRevocationFailed: http.StatusBadGateway,
	} {
		recorder, _ := serveLogged(t, func(w http.ResponseWriter) { googleError(w, err) })
		if recorder.Code != status || !strings.Contains(recorder.Body.String(), err.Error()) {
			t.Fatalf("%v: got %d %s", err, recorder.Code, recorder.Body.String())
		}
	}
}

func TestServiceErrorsSeparateValidationFromInternals(t *testing.T) {
	recorder, _ := serveLogged(t, func(w http.ResponseWriter) {
		writeServiceError(w, fmt.Errorf("%w: invalid status %q", service.ErrInvalidField, "x"))
	})
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "invalid status") {
		t.Fatalf("validation message should reach the client: %d %s", recorder.Code, recorder.Body.String())
	}
	recorder, logs := serveLogged(t, func(w http.ResponseWriter) {
		writeServiceError(w, errors.New("SQL logic error: no such table: jobs"))
	})
	if recorder.Code != http.StatusInternalServerError || strings.Contains(recorder.Body.String(), "SQL") || !strings.Contains(logs, "no such table") {
		t.Fatalf("internal error handling wrong: %d %s / %s", recorder.Code, recorder.Body.String(), logs)
	}
}

func TestInvalidPayloadAndStageLookupHideDetails(t *testing.T) {
	recorder, logs := serveLogged(t, func(w http.ResponseWriter) { invalidPayload(w, errors.New("invalid character 'x' at offset 3")) })
	if strings.Contains(recorder.Body.String(), "offset") || !strings.Contains(logs, "offset 3") {
		t.Fatalf("decoder detail handling wrong: %s / %s", recorder.Body.String(), logs)
	}
	recorder, _ = serveLogged(t, func(w http.ResponseWriter) { writeStageLookupError(w, fmt.Errorf("stage: %w", sql.ErrNoRows)) })
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("missing stage should be 404, got %d", recorder.Code)
	}
	recorder, _ = serveLogged(t, func(w http.ResponseWriter) { writeStageLookupError(w, errors.New("disk I/O error")) })
	if recorder.Code != http.StatusInternalServerError || strings.Contains(recorder.Body.String(), "disk") {
		t.Fatalf("stage lookup failure leaked: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestTruncateForLog(t *testing.T) {
	if got := truncateForLog(strings.Repeat("o", 300)); len(got) != 128 {
		t.Fatalf("expected 128 characters, got %d", len(got))
	}
}
