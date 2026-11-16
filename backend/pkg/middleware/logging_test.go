package middleware

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"war-room/backend/pkg/logging"
)

func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	previous := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previous) })
	var out bytes.Buffer
	if err := logging.Setup(&out, "debug", "json"); err != nil {
		t.Fatal(err)
	}
	return &out
}

func lastRecord(t *testing.T, out *bytes.Buffer) map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	var record map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &record); err != nil {
		t.Fatalf("decode %q: %v", lines[len(lines)-1], err)
	}
	return record
}

func TestRequestLoggerWritesOneDetailedRecord(t *testing.T) {
	out := captureLogs(t)
	handler := RequestLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logging.RequestFrom(r.Context()).SetUserID("user-1")
		logging.RecordError(w, errors.New("google drive upload initialization failed (403 storageQuotaExceeded)"))
		logging.RequestFrom(r.Context()).AddAttrs(slog.String("step", "upload"))
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":"generic"}`))
	}))
	request := httptest.NewRequest(http.MethodPost, "/api/google/backup?code=secret-code&state=secret-state", nil)
	request.Header.Set("User-Agent", strings.Repeat("u", 400))
	request.Header.Set("X-Forwarded-For", "203.0.113.9")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	record := lastRecord(t, out)
	requireFields(t, record, map[string]any{
		"request_id": recorder.Header().Get(RequestIDHeader), "level": "ERROR", "status": float64(502),
		"user_id": "user-1", "step": "upload", "bytes_out": float64(19), "forwarded_for": "203.0.113.9",
		"user_agent": strings.Repeat("u", maxLoggedHeaderLength),
		"error":      "google drive upload initialization failed (403 storageQuotaExceeded)",
	})
	if strings.Contains(out.String(), "secret-code") || strings.Contains(out.String(), "secret-state") {
		t.Fatalf("query string leaked into logs: %s", out.String())
	}
}

func requireFields(t *testing.T, record, want map[string]any) {
	t.Helper()
	for key, value := range want {
		if record[key] != value {
			t.Fatalf("%s = %v, want %v (record %v)", key, record[key], value, record)
		}
	}
}

func TestRequestLogLevel(t *testing.T) {
	cases := []struct {
		path   string
		status int
		want   slog.Level
	}{
		{"/api/health", 200, slog.LevelDebug},
		{"/api/jobs/counts", 200, slog.LevelDebug},
		{"/api/jobs/counts", 500, slog.LevelError},
		{"/api/auth/me", 401, slog.LevelInfo},
		{"/api/jobs", 404, slog.LevelWarn},
		{"/api/jobs", 200, slog.LevelInfo},
	}
	for _, tc := range cases {
		if got := requestLogLevel(tc.path, tc.status); got != tc.want {
			t.Fatalf("%s %d: got %v want %v", tc.path, tc.status, got, tc.want)
		}
	}
}

func TestRecovererLogsPanicAndHidesIt(t *testing.T) {
	out := captureLogs(t)
	handler := RequestLogger(Recoverer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("database exploded at /data/jobs.db")
	})))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/jobs", nil))
	if recorder.Code != http.StatusInternalServerError || strings.Contains(recorder.Body.String(), "exploded") {
		t.Fatalf("panic leaked or wrong status: %d %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), recorder.Header().Get(RequestIDHeader)) {
		t.Fatalf("expected request ID in body: %s", recorder.Body.String())
	}
	if !strings.Contains(out.String(), `"msg":"handler panic"`) || !strings.Contains(out.String(), "goroutine") {
		t.Fatalf("expected panic record with stack: %s", out.String())
	}
}

func TestRecovererRethrowsAbortHandler(t *testing.T) {
	defer func() {
		if recover() != http.ErrAbortHandler {
			t.Fatal("expected ErrAbortHandler to propagate")
		}
	}()
	Recoverer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) })).
		ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}

func TestResponseWriterKeepsFirstStatusAndUnwraps(t *testing.T) {
	recorder := httptest.NewRecorder()
	rw := &responseWriter{ResponseWriter: recorder, status: http.StatusOK}
	_, _ = rw.Write([]byte("x"))
	rw.WriteHeader(http.StatusTeapot)
	if rw.status != http.StatusOK || rw.Unwrap() != recorder {
		t.Fatalf("unexpected writer state: %d", rw.status)
	}
}

func TestSessionAndCORSRejectionsAreRecorded(t *testing.T) {
	out := captureLogs(t)
	handler := RequestLogger(CORS("https://app.example")(RequireSession(denyAll{})(http.NotFoundHandler())))
	cors := httptest.NewRequest(http.MethodGet, "/api/jobs", nil)
	cors.Header.Set("Origin", "https://evil.example")
	handler.ServeHTTP(httptest.NewRecorder(), cors)
	if record := lastRecord(t, out); !strings.Contains(record["error"].(string), "evil.example") {
		t.Fatalf("expected CORS reason: %v", record)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/jobs", nil))
	if record := lastRecord(t, out); record["status"] != float64(401) || !strings.Contains(record["error"].(string), "session") {
		t.Fatalf("expected session reason: %v", record)
	}
	if !strings.Contains(recorder.Body.String(), `"request_id"`) {
		t.Fatalf("expected request ID in 401 body: %s", recorder.Body.String())
	}
}

type denyAll struct{}

func (denyAll) Authenticate(http.ResponseWriter, *http.Request) (string, bool) { return "", false }
