package handlers

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"war-room/backend/pkg/config"
	"war-room/backend/pkg/service"
)

func mustNoErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func googleHandler(t *testing.T, configured bool) (*Handler, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{DataDir: dir, GoogleDrivePath: filepath.Join(dir, "google-drive.json"), GoogleFrontendURL: "http://localhost:3000"}
	if configured {
		cfg.GoogleClientID, cfg.GoogleClientSecret = "id", "secret"
		cfg.GoogleBackupEncryptionKey = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="
	}
	handler := NewHandler(nil, nil, cfg)
	handler.SetDriveService(service.NewDriveBackupService(cfg, func(w io.Writer) error { return nil }))
	return handler, cfg.GoogleDrivePath
}

func TestRegisterRoutesExposesGoogleStatusWhenUnconfigured(t *testing.T) {
	handler, _ := googleHandler(t, false)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/google/status", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"configured":false`) {
		t.Fatalf("unconfigured Google status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestGoogleHandlersWithoutDriveService(t *testing.T) {
	handler := NewHandler(nil, nil, &config.Config{})
	rec := httptest.NewRecorder()
	handler.handleGoogleStatus(rec, httptest.NewRequest(http.MethodGet, "/api/google/status", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"configured":false`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	handler.handleGoogleDisconnect(rec, httptest.NewRequest(http.MethodPost, "/api/google/disconnect", nil))
	pickerRequest := httptest.NewRequest(http.MethodGet, "/api/google/picker-token", nil)
	rec = httptest.NewRecorder()
	handler.handleGooglePickerToken(rec, pickerRequest)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("picker token without Drive service=%d", rec.Code)
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("disconnect without service=%d", rec.Code)
	}
	for _, call := range []func(http.ResponseWriter, *http.Request){
		handler.handleGoogleConnect, handler.handleGoogleFolders, handler.handleGoogleBackupNow,
	} {
		rec = httptest.NewRecorder()
		call(rec, httptest.NewRequest(http.MethodGet, "/api/google/x", nil))
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("nil drive handler=%d", rec.Code)
		}
	}
	rec = httptest.NewRecorder()
	handler.handleGoogleSetFolder(rec, httptest.NewRequest(http.MethodPut, "/api/google/folder", strings.NewReader(`{}`)))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("set folder without service=%d", rec.Code)
	}
	// Disconnect failure (non-empty directory at the store path) maps to 500.
	dir := t.TempDir()
	storePath := filepath.Join(dir, "store")
	mustNoErr(t, os.Mkdir(storePath, 0700))
	mustNoErr(t, os.WriteFile(filepath.Join(storePath, "keep"), []byte("x"), 0600))
	cfg := &config.Config{DataDir: dir, GoogleDrivePath: storePath}
	broken := NewHandler(nil, nil, cfg)
	broken.SetDriveService(service.NewDriveBackupService(cfg, func(w io.Writer) error { return nil }))
	rec = httptest.NewRecorder()
	broken.handleGoogleDisconnect(rec, httptest.NewRequest(http.MethodPost, "/api/google/disconnect", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("disconnect failure=%d", rec.Code)
	}
}

func TestGoogleHandlersConnectionLifecycle(t *testing.T) {
	handler, _ := googleHandler(t, true)

	rec := httptest.NewRecorder()
	handler.handleGoogleConnect(rec, httptest.NewRequest(http.MethodGet, "/api/google/connect", nil))
	if rec.Code != http.StatusFound || !strings.Contains(rec.Header().Get("Location"), "accounts.google.com") {
		t.Fatalf("connect=%d location=%s", rec.Code, rec.Header().Get("Location"))
	}

	rec = httptest.NewRecorder()
	handler.handleGoogleCallback(rec, httptest.NewRequest(http.MethodGet, "/api/google/callback?code=x&state=bad", nil))
	if rec.Code != http.StatusFound || !strings.Contains(rec.Header().Get("Location"), "google=error") {
		t.Fatalf("callback=%d location=%s", rec.Code, rec.Header().Get("Location"))
	}

	rec = httptest.NewRecorder()
	handler.handleGoogleFolders(rec, httptest.NewRequest(http.MethodGet, "/api/google/folders", nil))
	if rec.Code != http.StatusConflict {
		t.Fatalf("folders while disconnected=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	handler.handleGoogleBackupNow(rec, httptest.NewRequest(http.MethodPost, "/api/google/backup", nil))
	if rec.Code != http.StatusConflict {
		t.Fatalf("backup while disconnected=%d", rec.Code)
	}

}

func TestGoogleConnectRejectsCrossSiteNavigation(t *testing.T) {
	handler, _ := googleHandler(t, true)
	request := httptest.NewRequest(http.MethodGet, "/api/google/connect", nil)
	request.Header.Set("Sec-Fetch-Site", "cross-site")
	response := httptest.NewRecorder()
	handler.handleGoogleConnect(response, request)
	if response.Code != http.StatusForbidden || response.Header().Get("Set-Cookie") != "" || response.Header().Get("Location") != "" {
		t.Fatalf("cross-site connect status=%d headers=%v", response.Code, response.Header())
	}
}

func TestGoogleHandlersSavedAccount(t *testing.T) {
	handler, storePath := googleHandler(t, true)
	revoker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	t.Cleanup(revoker.Close)
	handler.drive.WithEndpoints(service.DriveEndpoints{RevokeURL: revoker.URL})
	mustNoErr(t, os.WriteFile(storePath, []byte(`{"email":"a@example.com","refresh_token":"rt","folder_id":"old"}`), 0600))
	rec := httptest.NewRecorder()
	handler.handleGoogleStatus(rec, httptest.NewRequest(http.MethodGet, "/api/google/status", nil))
	if !strings.Contains(rec.Body.String(), "a@example.com") || strings.Contains(rec.Body.String(), "refresh_token") {
		t.Fatalf("status must show email without tokens: %s", rec.Body.String())
	}
	rec = httptest.NewRecorder()
	handler.handleGoogleSetFolder(rec, httptest.NewRequest(http.MethodPut, "/api/google/folder", strings.NewReader(`{"folder_id":"f1","folder_name":"Backups"}`)))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"folder_id":"f1"`) {
		t.Fatalf("set folder=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	handler.handleGoogleSetFolder(rec, httptest.NewRequest(http.MethodPut, "/api/google/folder", strings.NewReader(`{`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid folder payload=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	handler.handleGoogleDisconnect(rec, httptest.NewRequest(http.MethodPost, "/api/google/disconnect", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("disconnect=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	handler.handleGoogleStatus(rec, httptest.NewRequest(http.MethodGet, "/api/google/status", nil))
	if strings.Contains(rec.Body.String(), "a@example.com") {
		t.Fatalf("status after disconnect: %s", rec.Body.String())
	}
}

func TestGoogleDisconnectReportsRevocationFailureAfterClearingLocalAccount(t *testing.T) {
	handler, drive := fakeGoogleHandler(t)
	connectFakeGoogle(t, handler, drive)
	revoker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusBadGateway)
	}))
	t.Cleanup(revoker.Close)
	drive.WithEndpoints(service.DriveEndpoints{RevokeURL: revoker.URL})
	request := httptest.NewRequest(http.MethodPost, "/api/google/disconnect", nil)
	request.Header.Set("Origin", "http://localhost:3000")
	response := httptest.NewRecorder()
	handler.handleGoogleDisconnect(response, request)
	if response.Code != http.StatusBadGateway || !strings.Contains(response.Body.String(), "revoke War Room access") {
		t.Fatalf("disconnect revocation failure=%d body=%s", response.Code, response.Body.String())
	}
	if drive.Status().Connected {
		t.Fatal("local account must be cleared even when revocation fails")
	}
}

func fakeGoogleHandler(t *testing.T) (*Handler, *service.DriveBackupService) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			_, _ = io.WriteString(w, `{"access_token":"at","refresh_token":"rt","expires_in":3600}`)
		case "/userinfo":
			_, _ = io.WriteString(w, `{"email":"person@example.com"}`)
		case "/revoke":
			w.WriteHeader(http.StatusOK)
		case "/files":
			_, _ = io.WriteString(w, `{"files":[{"id":"f1","name":"Backups"}]}`)
		case "/upload/files":
			w.Header().Set("Location", "http://"+r.Host+"/upload/session")
			w.WriteHeader(http.StatusOK)
		case "/upload/session":
			_, _ = io.WriteString(w, `{"id":"file-1"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	dir := t.TempDir()
	cfg := &config.Config{DataDir: dir, GoogleClientID: "id", GoogleClientSecret: "secret", GoogleBackupEncryptionKey: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=", GoogleFrontendURL: "http://localhost:3000", GoogleDrivePath: filepath.Join(dir, "g.json")}
	drive := service.NewDriveBackupService(cfg, func(w io.Writer) error { _, err := io.WriteString(w, "zip"); return err }).
		WithEndpoints(service.DriveEndpoints{TokenURL: server.URL + "/token", UserURL: server.URL + "/userinfo", RevokeURL: server.URL + "/revoke", DriveBase: server.URL, UploadBase: server.URL + "/upload"})
	handler := NewHandler(nil, nil, cfg)
	handler.SetDriveService(drive)
	return handler, drive
}

func connectFakeGoogle(t *testing.T, handler *Handler, drive *service.DriveBackupService) {
	t.Helper()
	connect := httptest.NewRecorder()
	handler.handleGoogleConnect(connect, httptest.NewRequest(http.MethodGet, "/api/google/connect", nil))
	authURL := connect.Header().Get("Location")
	state := authURL[strings.Index(authURL, "state=")+6:]
	if idx := strings.Index(state, "&"); idx >= 0 {
		state = state[:idx]
	}
	rec := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/google/callback?code=ok&state="+state, nil)
	for _, cookie := range connect.Result().Cookies() {
		request.AddCookie(cookie)
	}
	handler.handleGoogleCallback(rec, request)
	if rec.Code != http.StatusFound || !strings.Contains(rec.Header().Get("Location"), "google=connected") {
		t.Fatalf("callback=%d location=%s", rec.Code, rec.Header().Get("Location"))
	}
	if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("callback security headers: Cache-Control=%q Referrer-Policy=%q", rec.Header().Get("Cache-Control"), rec.Header().Get("Referrer-Policy"))
	}
}

func TestGoogleCallbackRequiresBrowserBoundState(t *testing.T) {
	handler, drive := fakeGoogleHandler(t)
	connect, _ := drive.AuthURL()
	parsed, err := url.Parse(connect)
	if err != nil {
		t.Fatal(err)
	}
	state := parsed.Query().Get("state")
	request := httptest.NewRequest(http.MethodGet, "/api/google/callback?code=ok&state="+state, nil)
	rec := httptest.NewRecorder()
	handler.handleGoogleCallback(rec, request)
	if !strings.Contains(rec.Header().Get("Location"), "google=error") || drive.Status().Connected {
		t.Fatalf("callback without browser cookie must fail: location=%s status=%+v", rec.Header().Get("Location"), drive.Status())
	}
}

func TestGoogleMutationsRejectCrossSiteRequests(t *testing.T) {
	handler, _ := googleHandler(t, true)
	for _, call := range []func(http.ResponseWriter, *http.Request){
		handler.handleGoogleDisconnect, handler.handleGoogleBackupNow,
		func(w http.ResponseWriter, r *http.Request) { handler.handleGoogleSetFolder(w, r) },
	} {
		request := httptest.NewRequest(http.MethodPost, "/api/google/action", nil)
		request.Header.Set("Origin", "https://attacker.example")
		rec := httptest.NewRecorder()
		call(rec, request)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("cross-site mutation status=%d body=%s", rec.Code, rec.Body.String())
		}
	}
}

func TestGoogleMutationAcceptsConfiguredOrigin(t *testing.T) {
	handler, _ := googleHandler(t, true)
	request := httptest.NewRequest(http.MethodPost, "/api/google/disconnect", nil)
	request.Header.Set("Origin", "http://localhost:3000")
	rec := httptest.NewRecorder()
	handler.handleGoogleDisconnect(rec, request)
	if rec.Code != http.StatusOK {
		t.Fatalf("same-origin disconnect status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGoogleConnectRejectsMalformedAuthorizationURL(t *testing.T) {
	handler, _ := googleHandler(t, true)
	handler.drive.WithEndpoints(service.DriveEndpoints{AuthBase: "%"})
	rec := httptest.NewRecorder()
	handler.handleGoogleConnect(rec, httptest.NewRequest(http.MethodGet, "/api/google/connect", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("malformed OAuth URL status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGoogleConnectRejectsStatelessAuthorizationURL(t *testing.T) {
	handler, _ := googleHandler(t, true)
	handler.drive.WithEndpoints(service.DriveEndpoints{AuthBase: "https://example.com/auth#nosuchquery"})
	rec := httptest.NewRecorder()
	handler.handleGoogleConnect(rec, httptest.NewRequest(http.MethodGet, "/api/google/connect", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("stateless OAuth URL status=%d", rec.Code)
	}
}

func TestGoogleMutationRejectsInvalidConfiguredOrigin(t *testing.T) {
	handler, _ := googleHandler(t, true)
	handler.cfg.GoogleFrontendURL = "://invalid"
	request := httptest.NewRequest(http.MethodPost, "/api/google/disconnect", nil)
	request.Header.Set("Origin", "http://localhost:3000")
	rec := httptest.NewRecorder()
	handler.handleGoogleDisconnect(rec, request)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("invalid configured origin status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGoogleHandlersAgainstFakeGoogle(t *testing.T) {
	handler, drive := fakeGoogleHandler(t)
	connectFakeGoogle(t, handler, drive)
	tokenRequest := httptest.NewRequest(http.MethodGet, "/api/google/picker-token", nil)
	tokenRequest.Header.Set("Origin", "http://localhost:3000")
	tokenRec := httptest.NewRecorder()
	handler.handleGooglePickerToken(tokenRec, tokenRequest)
	if tokenRec.Code != http.StatusOK || !strings.Contains(tokenRec.Body.String(), `"access_token":"at"`) {
		t.Fatalf("picker token=%d body=%s", tokenRec.Code, tokenRec.Body.String())
	}
	if !strings.Contains(tokenRec.Header().Get("Cache-Control"), "no-store") {
		t.Fatal("Picker access tokens must not be cached")
	}
	statusRec := httptest.NewRecorder()
	handler.handleGoogleStatus(statusRec, httptest.NewRequest(http.MethodGet, "/api/google/status", nil))
	if strings.Contains(statusRec.Body.String(), "refresh_token") || strings.Contains(statusRec.Body.String(), "access_token") {
		t.Fatalf("Google status must not expose OAuth tokens: %s", statusRec.Body.String())
	}
	rec := httptest.NewRecorder()
	handler.handleGoogleFolders(rec, httptest.NewRequest(http.MethodGet, "/api/google/folders?q=Back", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Backups") {
		t.Fatalf("folders=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGooglePickerTokenRejectsCrossSiteRequest(t *testing.T) {
	handler, _ := fakeGoogleHandler(t)
	request := httptest.NewRequest(http.MethodGet, "/api/google/picker-token", nil)
	request.Header.Set("Origin", "https://attacker.example")
	rec := httptest.NewRecorder()
	handler.handleGooglePickerToken(rec, request)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-site picker token status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGoogleBackupNowAgainstFakeGoogle(t *testing.T) {
	handler, drive := fakeGoogleHandler(t)
	connectFakeGoogle(t, handler, drive)
	rec := httptest.NewRecorder()
	handler.handleGoogleBackupNow(rec, httptest.NewRequest(http.MethodPost, "/api/google/backup", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"last_status":"ok"`) {
		t.Fatalf("backup=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGoogleHandlerErrorMapping(t *testing.T) {
	handler, _ := googleHandler(t, true)
	rec := httptest.NewRecorder()
	handler.handleGoogleSetFolder(rec, httptest.NewRequest(http.MethodPut, "/api/google/folder", strings.NewReader(`{"folder_id":"f1"}`)))
	if rec.Code != http.StatusConflict {
		t.Fatalf("set folder while disconnected=%d", rec.Code)
	}
	// A connected account whose Google endpoints fail maps to 502.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusBadGateway)
	}))
	defer server.Close()
	dir := t.TempDir()
	cfg := &config.Config{DataDir: dir, GoogleClientID: "id", GoogleClientSecret: "secret", GoogleBackupEncryptionKey: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=", GoogleDrivePath: filepath.Join(dir, "g.json")}
	drive := service.NewDriveBackupService(cfg, func(w io.Writer) error { return nil }).
		WithEndpoints(service.DriveEndpoints{TokenURL: server.URL + "/token", DriveBase: server.URL, UploadBase: server.URL + "/upload"})
	mustNoErr(t, os.WriteFile(cfg.GoogleDrivePath, []byte(`{"email":"a@example.com","refresh_token":"rt"}`), 0600))
	failing := NewHandler(nil, nil, cfg)
	failing.SetDriveService(drive)
	rec = httptest.NewRecorder()
	failing.handleGoogleFolders(rec, httptest.NewRequest(http.MethodGet, "/api/google/folders", nil))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("folders with failing Google=%d body=%s", rec.Code, rec.Body.String())
	}
	pickerRequest := httptest.NewRequest(http.MethodGet, "/api/google/picker-token", nil)
	rec = httptest.NewRecorder()
	failing.handleGooglePickerToken(rec, pickerRequest)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("picker token with failing Google=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	failing.handleGoogleBackupNow(rec, httptest.NewRequest(http.MethodPost, "/api/google/backup", nil))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("backup with failing Google=%d", rec.Code)
	}
}

func TestGoogleHandlersNotConfigured(t *testing.T) {
	handler, _ := googleHandler(t, false)
	rec := httptest.NewRecorder()
	handler.handleGoogleConnect(rec, httptest.NewRequest(http.MethodGet, "/api/google/connect", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("connect unconfigured=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	handler.handleGoogleFolders(rec, httptest.NewRequest(http.MethodGet, "/api/google/folders", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("folders unconfigured=%d", rec.Code)
	}
}
