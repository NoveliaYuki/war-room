package service

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestNewGoogleAPIErrorParsesDriveErrors(t *testing.T) {
	body := `{"error":{"code":403,"message":"The user's Drive storage quota has been exceeded.","status":"PERMISSION_DENIED","errors":[{"reason":"storageQuotaExceeded","message":"x"}]}}`
	err := newGoogleAPIError("drive upload initialization", 403, []byte(body))
	if err.Reason != "storageQuotaExceeded" || !strings.Contains(err.Message, "quota") {
		t.Fatalf("unexpected parse: %+v", err)
	}
	want := "google drive upload initialization failed (403 storageQuotaExceeded): The user's Drive storage quota has been exceeded."
	if err.Error() != want {
		t.Fatalf("Error() = %q, want %q", err.Error(), want)
	}
}

func TestNewGoogleAPIErrorFallsBackToStatus(t *testing.T) {
	err := newGoogleAPIError("drive request", 404, []byte(`{"error":{"message":"File not found","status":"NOT_FOUND"}}`))
	if err.Reason != "NOT_FOUND" {
		t.Fatalf("expected status reason, got %+v", err)
	}
}

func TestNewGoogleAPIErrorParsesOAuthErrors(t *testing.T) {
	err := newGoogleAPIError("token request", 400, []byte(`{"error":"invalid_grant","error_description":"Token has been\n expired or revoked."}`))
	if err.Reason != "invalid_grant" || err.Message != "Token has been expired or revoked." {
		t.Fatalf("unexpected parse: %+v", err)
	}
}

func TestNewGoogleAPIErrorToleratesUnexpectedBodies(t *testing.T) {
	for _, body := range []string{"", "<html>bad gateway</html>", `{"other":1}`, `{"error":42}`} {
		err := newGoogleAPIError("drive delete", 502, []byte(body))
		if err.Reason != "" || err.Message != "" || err.Error() != "google drive delete failed (502)" {
			t.Fatalf("body %q: unexpected %+v / %q", body, err, err.Error())
		}
	}
}

func TestBoundedGoogleMessageTruncates(t *testing.T) {
	if got := boundedGoogleMessage(strings.Repeat("a", maxGoogleErrorMessage+50)); len(got) != maxGoogleErrorMessage {
		t.Fatalf("expected truncation, got %d characters", len(got))
	}
}

func captureServiceLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	previous := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previous) })
	var out bytes.Buffer
	slog.SetDefault(slog.New(slog.NewJSONHandler(&out, &slog.HandlerOptions{Level: slog.LevelDebug})))
	return &out
}

func TestLogBackupResultExplainsGoogleFailures(t *testing.T) {
	out := captureServiceLogs(t)
	apiErr := &GoogleAPIError{Operation: "drive upload initialization", Status: 403, Reason: "storageQuotaExceeded"}
	logBackupResult("user-1", 0, time.Millisecond, fmt.Errorf("upload backup version 2: %w", apiErr))
	logBackupResult("user-1", 10, time.Millisecond, nil)
	logBackupResult("user-1", 0, 0, ErrGoogleNotConnected)
	logBackupResult("user-1", 0, 0, errors.New("disk full"))
	text := out.String()
	for _, want := range []string{
		`"google_reason":"storageQuotaExceeded"`, `"google_status":403`, `"hint":"the Google account's Drive storage is full"`,
		`"msg":"google drive backup completed"`, `"msg":"google drive backup skipped"`, `"error":"disk full"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %s in %s", want, text)
		}
	}
}

func TestDoRequestStripsQueryFromTransportErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			_ = conn.Close()
		}
	}))
	defer server.Close()
	req, err := http.NewRequest(http.MethodPut, server.URL+"/upload/drive/v3/files?upload_id=SECRET-SESSION", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = doRequest(server.Client(), req)
	if err == nil {
		t.Fatal("expected a transport error")
	}
	if strings.Contains(err.Error(), "SECRET-SESSION") || !strings.Contains(err.Error(), "/upload/drive/v3/files") {
		t.Fatalf("unexpected transport error text: %v", err)
	}
	if transportError(nil) != nil || transportError(errors.New("plain")).Error() != "plain" {
		t.Fatal("non-URL errors must pass through unchanged")
	}
	malformed := transportError(&url.Error{Op: "Get", URL: "http://[::1", Err: errors.New("boom")})
	if !strings.Contains(malformed.Error(), "<unparseable url>") {
		t.Fatalf("unexpected malformed URL handling: %v", malformed)
	}
}
