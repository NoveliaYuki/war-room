package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func captureDefault(t *testing.T, level, format string) *bytes.Buffer {
	t.Helper()
	previous := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previous) })
	var out bytes.Buffer
	if err := Setup(&out, level, format); err != nil {
		t.Fatal(err)
	}
	return &out
}

func TestParseLevel(t *testing.T) {
	for value, want := range map[string]slog.Level{"": slog.LevelInfo, "debug": slog.LevelDebug, " WARN ": slog.LevelWarn, "error": slog.LevelError} {
		got, err := ParseLevel(value)
		if err != nil || got != want {
			t.Fatalf("ParseLevel(%q) = %v, %v; want %v", value, got, err, want)
		}
	}
	if _, err := ParseLevel("loud"); err == nil {
		t.Fatal("expected an invalid level error")
	}
}

func TestNewRejectsUnknownFormat(t *testing.T) {
	if _, err := New(&bytes.Buffer{}, slog.LevelInfo, "xml"); err == nil {
		t.Fatal("expected an invalid format error")
	}
	if logger, err := New(&bytes.Buffer{}, slog.LevelInfo, "text"); err != nil || logger == nil {
		t.Fatalf("text format: %v", err)
	}
}

func TestSetupFallsBackAndReportsInvalidSettings(t *testing.T) {
	previous := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previous) })
	var out bytes.Buffer
	if err := Setup(&out, "loud", "xml"); err == nil {
		t.Fatal("expected invalid settings to be reported")
	}
	slog.Info("still logging")
	if !strings.Contains(out.String(), `"msg":"still logging"`) {
		t.Fatalf("expected JSON fallback output, got %q", out.String())
	}
}

func TestSetupRedactsSecretsAndRoutesStandardLog(t *testing.T) {
	out := captureDefault(t, "debug", "json")
	slog.Info("oauth", "access_token", "ya29.secret", "client_secret", "shh", "code", "4/abc", "state", "s1", "Authorization", "Bearer x", "status_code", 403)
	log.Print("legacy line")
	text := out.String()
	for _, secret := range []string{"ya29.secret", "shh", "4/abc", "\"s1\"", "Bearer x"} {
		if strings.Contains(text, secret) {
			t.Fatalf("log output leaked %q: %s", secret, text)
		}
	}
	if !strings.Contains(text, `"status_code":403`) || !strings.Contains(text, "legacy line") {
		t.Fatalf("expected non-secret fields and stdlib output, got %s", text)
	}
}

func TestIsSensitiveKey(t *testing.T) {
	for _, key := range []string{"refresh_token", "GOOGLE_CLIENT_SECRET", "cookie", "code", "picker_api_key", "session_id"} {
		if !IsSensitiveKey(key) {
			t.Fatalf("%q should be sensitive", key)
		}
	}
	for _, key := range []string{"status_code", "import_code", "user_id", "google_reason", "path", "https_only_sessions"} {
		if IsSensitiveKey(key) {
			t.Fatalf("%q should not be sensitive", key)
		}
	}
}

func TestRequestCollectsFields(t *testing.T) {
	req := NewRequest()
	if len(req.ID) != 16 {
		t.Fatalf("unexpected request ID %q", req.ID)
	}
	req.SetUserID("user-1")
	req.RecordError(errors.New("first"))
	req.RecordError(nil)
	req.RecordError(errors.New("second"))
	req.AddAttrs(slog.String("step", "upload"))
	if req.UserID() != "user-1" || req.Err().Error() != "first\nsecond" || len(req.Attrs()) != 1 {
		t.Fatalf("unexpected request state: %q %v %v", req.UserID(), req.Err(), req.Attrs())
	}
}

func TestNilRequestIsSafe(t *testing.T) {
	var req *Request
	req.SetUserID("x")
	req.RecordError(errors.New("x"))
	req.AddAttrs(slog.String("k", "v"))
	if req.UserID() != "" || req.Err() != nil || req.Attrs() != nil {
		t.Fatal("nil request should be inert")
	}
}

type carrierWriter struct {
	http.ResponseWriter
	req *Request
}

func (c carrierWriter) LogRequest() *Request { return c.req }

func TestRecordErrorUsesCarrierOrLogsImmediately(t *testing.T) {
	out := captureDefault(t, "info", "json")
	req := NewRequest()
	RecordError(carrierWriter{ResponseWriter: httptest.NewRecorder(), req: req}, errors.New("kept"))
	if req.Err() == nil || out.Len() != 0 {
		t.Fatalf("expected the error on the request only, log=%q", out.String())
	}
	RecordError(httptest.NewRecorder(), errors.New("orphan"))
	RecordError(httptest.NewRecorder(), nil)
	if !strings.Contains(out.String(), "orphan") {
		t.Fatalf("expected orphan error to be logged, got %q", out.String())
	}
}

func TestFromContextAndAudit(t *testing.T) {
	out := captureDefault(t, "info", "json")
	Audit(context.Background(), "plain")
	req := NewRequest()
	req.SetUserID("user-7")
	ctx := WithRequest(context.Background(), req)
	if RequestFrom(ctx) != req {
		t.Fatal("request not attached to context")
	}
	Audit(ctx, "auth.sign_in")
	AuditFailure(ctx, "auth.sign_in", "reason", "denied")
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected three records, got %q", out.String())
	}
	var record map[string]any
	if err := json.Unmarshal([]byte(lines[2]), &record); err != nil {
		t.Fatal(err)
	}
	if record["request_id"] != req.ID || record["user_id"] != "user-7" || record["audit"] != true ||
		record["outcome"] != "failure" || record["level"] != "WARN" {
		t.Fatalf("unexpected audit record: %v", record)
	}
}
