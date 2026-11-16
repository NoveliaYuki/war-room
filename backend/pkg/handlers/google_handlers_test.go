package handlers

import (
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"war-room/backend/pkg/config"
	"war-room/backend/pkg/database"
	"war-room/backend/pkg/middleware"
	"war-room/backend/pkg/service"
)

const googleTestEncryptionKey = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="

func mustNoErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// withUserID simulates what RequireSession does for handlers invoked
// directly in these tests (bypassing the middleware chain).
func withUserID(r *http.Request, userID string) *http.Request {
	return r.WithContext(middleware.ContextWithUserID(r.Context(), userID))
}

func googleTestDB(t *testing.T, dir string) *sql.DB {
	t.Helper()
	cfg := &config.Config{
		DataDir: dir, DBPath: filepath.Join(dir, "g.db"), BackupPath: filepath.Join(dir, "backup.json"),
		AttachmentsDir: filepath.Join(dir, "attachments"), CVDir: filepath.Join(dir, "cvs"), LogosDir: filepath.Join(dir, "logos"),
		AvatarsDir: filepath.Join(dir, "avatars"),
	}
	if err := os.MkdirAll(cfg.AvatarsDir, 0700); err != nil {
		t.Fatal(err)
	}
	db, err := database.InitDB(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func seedGoogleTestUser(t *testing.T, db *sql.DB, userID, email string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO users (id, google_sub, email, name, picture_url) VALUES (?, ?, ?, ?, ?)`,
		userID, "sub-"+userID, email, email, ""); err != nil {
		t.Fatal(err)
	}
}

// googleHandler returns a Handler wired with a real database, Drive
// service, and AuthService, plus a userID already seeded as a user row
// (simulating someone who has already signed in).
func googleHandler(t *testing.T, configured bool) (*Handler, *sql.DB, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{DataDir: dir, GoogleFrontendURL: "http://localhost:3000", AvatarsDir: filepath.Join(dir, "avatars")}
	if configured {
		cfg.GoogleClientID, cfg.GoogleClientSecret = "id", "secret"
		cfg.GoogleBackupEncryptionKey = googleTestEncryptionKey
		cfg.AuthTokenEncryptionKey = googleTestEncryptionKey
	}
	if err := os.MkdirAll(cfg.AvatarsDir, 0700); err != nil {
		t.Fatal(err)
	}
	db := googleTestDB(t, dir)
	const userID = "user-1"
	seedGoogleTestUser(t, db, userID, "person@example.com")
	handler := NewHandler(nil, nil, cfg)
	handler.SetDriveService(service.NewDriveBackupService(db, cfg, func(string, io.Writer) error { return nil }))
	handler.SetAuthService(service.NewAuthService(db, cfg))
	return handler, db, userID
}

func TestRegisterRoutesExposesGoogleStatusWhenUnconfigured(t *testing.T) {
	handler, _, userID := googleHandler(t, false)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	response := httptest.NewRecorder()
	mux.ServeHTTP(response, withUserID(httptest.NewRequest(http.MethodGet, "/api/google/status", nil), userID))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"configured":false`) {
		t.Fatalf("unconfigured Google status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestGoogleHandlersWithoutDriveService(t *testing.T) {
	handler := NewHandler(nil, nil, &config.Config{})
	rec := httptest.NewRecorder()
	handler.handleGoogleStatus(rec, withUserID(httptest.NewRequest(http.MethodGet, "/api/google/status", nil), "user-1"))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"configured":false`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	handler.handleGoogleRevoke(rec, httptest.NewRequest(http.MethodPost, "/api/google/revoke", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("revoke without service=%d", rec.Code)
	}
	pickerRequest := withUserID(httptest.NewRequest(http.MethodGet, "/api/google/picker-token", nil), "user-1")
	rec = httptest.NewRecorder()
	handler.handleGooglePickerToken(rec, pickerRequest)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("picker token without Drive service=%d", rec.Code)
	}
	for _, call := range []func(http.ResponseWriter, *http.Request){
		handler.handleGoogleConnect, handler.handleGoogleFolders, handler.handleGoogleBackupNow,
	} {
		rec = httptest.NewRecorder()
		call(rec, withUserID(httptest.NewRequest(http.MethodGet, "/api/google/x", nil), "user-1"))
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("nil drive handler=%d", rec.Code)
		}
	}
	rec = httptest.NewRecorder()
	handler.handleGoogleSetFolder(rec, withUserID(httptest.NewRequest(http.MethodPut, "/api/google/folder", strings.NewReader(`{}`)), "user-1"))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("set folder without service=%d", rec.Code)
	}
}

func TestGoogleHandlersConnectionLifecycle(t *testing.T) {
	handler, _, userID := googleHandler(t, true)

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
	handler.handleGoogleFolders(rec, withUserID(httptest.NewRequest(http.MethodGet, "/api/google/folders", nil), userID))
	if rec.Code != http.StatusConflict {
		t.Fatalf("folders while disconnected=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	handler.handleGoogleBackupNow(rec, withUserID(httptest.NewRequest(http.MethodPost, "/api/google/backup", nil), userID))
	if rec.Code != http.StatusConflict {
		t.Fatalf("backup while disconnected=%d", rec.Code)
	}
}

func TestGoogleConnectRejectsCrossSiteNavigation(t *testing.T) {
	handler, _, _ := googleHandler(t, true)
	request := httptest.NewRequest(http.MethodGet, "/api/google/connect", nil)
	request.Header.Set("Sec-Fetch-Site", "cross-site")
	response := httptest.NewRecorder()
	handler.handleGoogleConnect(response, request)
	if response.Code != http.StatusForbidden || response.Header().Get("Set-Cookie") != "" || response.Header().Get("Location") != "" {
		t.Fatalf("cross-site connect status=%d headers=%v", response.Code, response.Header())
	}
}

func TestGoogleHandlersSavedAccount(t *testing.T) {
	handler, db, userID := googleHandler(t, true)
	revoker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	t.Cleanup(revoker.Close)
	handler.drive.WithEndpoints(service.DriveEndpoints{RevokeURL: revoker.URL})
	saveFakeDriveAccount(t, db, userID, "rt", "old")

	rec := httptest.NewRecorder()
	handler.handleGoogleStatus(rec, withUserID(httptest.NewRequest(http.MethodGet, "/api/google/status", nil), userID))
	if !strings.Contains(rec.Body.String(), `"connected":true`) || strings.Contains(rec.Body.String(), "refresh_token") {
		t.Fatalf("status must show connected without tokens: %s", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	setFolderRequest := withUserID(httptest.NewRequest(http.MethodPut, "/api/google/folder", strings.NewReader(`{"folder_id":"f1","folder_name":"Backups"}`)), userID)
	handler.handleGoogleSetFolder(rec, setFolderRequest)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"folder_id":"f1"`) {
		t.Fatalf("set folder=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	handler.handleGoogleSetFolder(rec, withUserID(httptest.NewRequest(http.MethodPut, "/api/google/folder", strings.NewReader(`{`)), userID))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid folder payload=%d", rec.Code)
	}

	rec = httptest.NewRecorder()
	handler.handleGoogleRevoke(rec, withUserID(httptest.NewRequest(http.MethodPost, "/api/google/revoke", nil), userID))
	if rec.Code != http.StatusOK {
		t.Fatalf("revoke=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	handler.handleGoogleStatus(rec, withUserID(httptest.NewRequest(http.MethodGet, "/api/google/status", nil), userID))
	if strings.Contains(rec.Body.String(), `"connected":true`) {
		t.Fatalf("status after revoke: %s", rec.Body.String())
	}
}

// saveFakeDriveAccount seeds a Drive account row directly, bypassing the
// OAuth exchange, for tests that only need "already connected" state.
func saveFakeDriveAccount(t *testing.T, db *sql.DB, userID, refreshToken, folderID string) {
	t.Helper()
	drive := service.NewDriveBackupService(db, &config.Config{AuthTokenEncryptionKey: googleTestEncryptionKey}, nil)
	if err := drive.SaveAccountForTesting(userID, refreshToken, "", 0); err != nil {
		t.Fatal(err)
	}
	if folderID != "" {
		if err := drive.SetFolder(userID, folderID, "Old"); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGoogleRevokeReportsRevocationFailureAfterClearingLocalAccount(t *testing.T) {
	handler, drive, _ := fakeGoogleHandler(t)
	userID := connectFakeGoogle(t, handler, drive)
	revoker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusBadGateway)
	}))
	t.Cleanup(revoker.Close)
	drive.WithEndpoints(service.DriveEndpoints{RevokeURL: revoker.URL})
	request := withUserID(httptest.NewRequest(http.MethodPost, "/api/google/revoke", nil), userID)
	request.Header.Set("Origin", "http://localhost:3000")
	response := httptest.NewRecorder()
	handler.handleGoogleRevoke(response, request)
	if response.Code != http.StatusBadGateway || !strings.Contains(response.Body.String(), "revoke War Room access") {
		t.Fatalf("revoke failure=%d body=%s", response.Code, response.Body.String())
	}
	if drive.Status(userID).Connected {
		t.Fatal("local account must be cleared even when revocation fails")
	}
}

func fakeGoogleHandler(t *testing.T) (*Handler, *service.DriveBackupService, string) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			_, _ = io.WriteString(w, `{"access_token":"at","refresh_token":"rt","expires_in":3600}`)
		case "/userinfo":
			_, _ = io.WriteString(w, `{"sub":"google-sub-1","email":"person@example.com","email_verified":true,"name":"Person"}`)
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
	cfg := &config.Config{
		DataDir: dir, GoogleClientID: "id", GoogleClientSecret: "secret", GoogleBackupEncryptionKey: googleTestEncryptionKey,
		AuthTokenEncryptionKey: googleTestEncryptionKey, GoogleFrontendURL: "http://localhost:3000",
		AuthAllowedEmails: []string{"person@example.com"},
	}
	db := googleTestDB(t, dir)
	drive := service.NewDriveBackupService(db, cfg, func(string, io.Writer) error { _, err := io.WriteString(io.Discard, "zip"); return err }).
		WithEndpoints(service.DriveEndpoints{TokenURL: server.URL + "/token", UserURL: server.URL + "/userinfo", RevokeURL: server.URL + "/revoke", DriveBase: server.URL, UploadBase: server.URL + "/upload"})
	handler := NewHandler(nil, nil, cfg)
	handler.SetDriveService(drive)
	handler.SetAuthService(service.NewAuthService(db, cfg))
	return handler, drive, ""
}

// connectFakeGoogle runs the full connect-then-callback flow against the
// fake Google server and returns the signed-in user's ID (read back from
// the session cookie the callback issued).
func connectFakeGoogle(t *testing.T, handler *Handler, drive *service.DriveBackupService) string {
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
	sessionRequest := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	for _, cookie := range rec.Result().Cookies() {
		sessionRequest.AddCookie(cookie)
	}
	userID, ok := handler.auth.Authenticate(httptest.NewRecorder(), sessionRequest)
	if !ok {
		t.Fatal("callback did not issue a usable session")
	}
	return userID
}

func TestGoogleCallbackRequiresBrowserBoundState(t *testing.T) {
	handler, drive, _ := fakeGoogleHandler(t)
	connect, _ := drive.AuthURL()
	parsed, err := url.Parse(connect)
	if err != nil {
		t.Fatal(err)
	}
	state := parsed.Query().Get("state")
	request := httptest.NewRequest(http.MethodGet, "/api/google/callback?code=ok&state="+state, nil)
	rec := httptest.NewRecorder()
	handler.handleGoogleCallback(rec, request)
	if !strings.Contains(rec.Header().Get("Location"), "google=error") {
		t.Fatalf("callback without browser cookie must fail: location=%s", rec.Header().Get("Location"))
	}
}

func TestGoogleMutationsRejectCrossSiteRequests(t *testing.T) {
	handler, _, userID := googleHandler(t, true)
	for _, call := range []func(http.ResponseWriter, *http.Request){
		handler.handleGoogleRevoke, handler.handleGoogleBackupNow,
		func(w http.ResponseWriter, r *http.Request) { handler.handleGoogleSetFolder(w, r) },
	} {
		request := withUserID(httptest.NewRequest(http.MethodPost, "/api/google/action", nil), userID)
		request.Header.Set("Origin", "https://attacker.example")
		rec := httptest.NewRecorder()
		call(rec, request)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("cross-site mutation status=%d body=%s", rec.Code, rec.Body.String())
		}
	}
}

func TestGoogleMutationAcceptsConfiguredOrigin(t *testing.T) {
	handler, _, userID := googleHandler(t, true)
	request := withUserID(httptest.NewRequest(http.MethodPost, "/api/google/revoke", nil), userID)
	request.Header.Set("Origin", "http://localhost:3000")
	rec := httptest.NewRecorder()
	handler.handleGoogleRevoke(rec, request)
	if rec.Code != http.StatusOK {
		t.Fatalf("same-origin revoke status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGoogleConnectRejectsMalformedAuthorizationURL(t *testing.T) {
	handler, _, _ := googleHandler(t, true)
	handler.drive.WithEndpoints(service.DriveEndpoints{AuthBase: "%"})
	rec := httptest.NewRecorder()
	handler.handleGoogleConnect(rec, httptest.NewRequest(http.MethodGet, "/api/google/connect", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("malformed OAuth URL status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGoogleConnectRejectsStatelessAuthorizationURL(t *testing.T) {
	handler, _, _ := googleHandler(t, true)
	handler.drive.WithEndpoints(service.DriveEndpoints{AuthBase: "https://example.com/auth#nosuchquery"})
	rec := httptest.NewRecorder()
	handler.handleGoogleConnect(rec, httptest.NewRequest(http.MethodGet, "/api/google/connect", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("stateless OAuth URL status=%d", rec.Code)
	}
}

func TestGoogleMutationRejectsInvalidConfiguredOrigin(t *testing.T) {
	handler, _, userID := googleHandler(t, true)
	handler.cfg.GoogleFrontendURL = "://invalid"
	request := withUserID(httptest.NewRequest(http.MethodPost, "/api/google/revoke", nil), userID)
	request.Header.Set("Origin", "http://localhost:3000")
	rec := httptest.NewRecorder()
	handler.handleGoogleRevoke(rec, request)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("invalid configured origin status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGoogleHandlersAgainstFakeGoogle(t *testing.T) {
	handler, drive, _ := fakeGoogleHandler(t)
	userID := connectFakeGoogle(t, handler, drive)
	tokenRequest := withUserID(httptest.NewRequest(http.MethodGet, "/api/google/picker-token", nil), userID)
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
	handler.handleGoogleStatus(statusRec, withUserID(httptest.NewRequest(http.MethodGet, "/api/google/status", nil), userID))
	if strings.Contains(statusRec.Body.String(), "refresh_token") || strings.Contains(statusRec.Body.String(), "access_token") {
		t.Fatalf("Google status must not expose OAuth tokens: %s", statusRec.Body.String())
	}
	rec := httptest.NewRecorder()
	handler.handleGoogleFolders(rec, withUserID(httptest.NewRequest(http.MethodGet, "/api/google/folders?q=Back", nil), userID))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Backups") {
		t.Fatalf("folders=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGooglePickerTokenRejectsCrossSiteRequest(t *testing.T) {
	handler, _, _ := fakeGoogleHandler(t)
	request := httptest.NewRequest(http.MethodGet, "/api/google/picker-token", nil)
	request.Header.Set("Origin", "https://attacker.example")
	rec := httptest.NewRecorder()
	handler.handleGooglePickerToken(rec, request)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-site picker token status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGoogleBackupNowAgainstFakeGoogle(t *testing.T) {
	handler, drive, _ := fakeGoogleHandler(t)
	userID := connectFakeGoogle(t, handler, drive)
	rec := httptest.NewRecorder()
	handler.handleGoogleBackupNow(rec, withUserID(httptest.NewRequest(http.MethodPost, "/api/google/backup", nil), userID))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"last_status":"ok"`) {
		t.Fatalf("backup=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGoogleHandlerErrorMapping(t *testing.T) {
	handler, _, userID := googleHandler(t, true)
	rec := httptest.NewRecorder()
	handler.handleGoogleSetFolder(rec, withUserID(httptest.NewRequest(http.MethodPut, "/api/google/folder", strings.NewReader(`{"folder_id":"f1"}`)), userID))
	if rec.Code != http.StatusConflict {
		t.Fatalf("set folder while disconnected=%d", rec.Code)
	}
	// A connected account whose Google endpoints fail maps to 502.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusBadGateway)
	}))
	defer server.Close()
	dir := t.TempDir()
	cfg := &config.Config{DataDir: dir, GoogleClientID: "id", GoogleClientSecret: "secret", GoogleBackupEncryptionKey: googleTestEncryptionKey, AuthTokenEncryptionKey: googleTestEncryptionKey}
	db := googleTestDB(t, dir)
	const failingUserID = "user-failing"
	seedGoogleTestUser(t, db, failingUserID, "failing@example.com")
	drive := service.NewDriveBackupService(db, cfg, func(string, io.Writer) error { return nil }).
		WithEndpoints(service.DriveEndpoints{TokenURL: server.URL + "/token", DriveBase: server.URL, UploadBase: server.URL + "/upload"})
	mustNoErr(t, drive.SaveAccountForTesting(failingUserID, "rt", "", 0))
	failing := NewHandler(nil, nil, cfg)
	failing.SetDriveService(drive)
	rec = httptest.NewRecorder()
	failing.handleGoogleFolders(rec, withUserID(httptest.NewRequest(http.MethodGet, "/api/google/folders", nil), failingUserID))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("folders with failing Google=%d body=%s", rec.Code, rec.Body.String())
	}
	pickerRequest := withUserID(httptest.NewRequest(http.MethodGet, "/api/google/picker-token", nil), failingUserID)
	rec = httptest.NewRecorder()
	failing.handleGooglePickerToken(rec, pickerRequest)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("picker token with failing Google=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	failing.handleGoogleBackupNow(rec, withUserID(httptest.NewRequest(http.MethodPost, "/api/google/backup", nil), failingUserID))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("backup with failing Google=%d", rec.Code)
	}
}

func TestGoogleHandlersNotConfigured(t *testing.T) {
	handler, _, userID := googleHandler(t, false)
	rec := httptest.NewRecorder()
	handler.handleGoogleConnect(rec, httptest.NewRequest(http.MethodGet, "/api/google/connect", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("connect unconfigured=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	handler.handleGoogleFolders(rec, withUserID(httptest.NewRequest(http.MethodGet, "/api/google/folders", nil), userID))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("folders unconfigured=%d", rec.Code)
	}
}

// TestGoogleHandlersRejectMissingSessionContext exercises the defensive
// "no userID in context" branch each protected handler has, in case it is
// ever invoked outside the RequireSession middleware that normally
// guarantees the context carries one.
func TestGoogleHandlersRejectMissingSessionContext(t *testing.T) {
	handler, _, _ := googleHandler(t, true)
	for name, call := range map[string]func(http.ResponseWriter, *http.Request){
		"status":       handler.handleGoogleStatus,
		"folders":      handler.handleGoogleFolders,
		"picker-token": handler.handleGooglePickerToken,
		"backup":       handler.handleGoogleBackupNow,
		"set-folder":   func(w http.ResponseWriter, r *http.Request) { handler.handleGoogleSetFolder(w, r) },
		"revoke":       handler.handleGoogleRevoke,
	} {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			call(rec, httptest.NewRequest(http.MethodGet, "/api/google/x", nil))
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("%s without session context=%d", name, rec.Code)
			}
		})
	}
}

// TestGoogleCallbackRejectsDisallowedAccount exercises the handler-level
// path (not just AuthService directly) where Google's identity succeeds but
// the account is not on the allowlist: no user or session must be created.
func TestGoogleCallbackRejectsDisallowedAccount(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			_, _ = w.Write([]byte(`{"access_token":"at","refresh_token":"rt","expires_in":3600}`))
		case "/userinfo":
			_, _ = w.Write([]byte(`{"sub":"google-sub-stranger","email":"stranger@example.com","email_verified":true,"name":"Stranger"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	dir := t.TempDir()
	cfg := &config.Config{
		DataDir: dir, GoogleClientID: "id", GoogleClientSecret: "secret", GoogleBackupEncryptionKey: googleTestEncryptionKey,
		AuthTokenEncryptionKey: googleTestEncryptionKey, GoogleFrontendURL: "http://localhost:3000",
		AuthAllowedEmails: []string{"allowed@example.com"},
	}
	db := googleTestDB(t, dir)
	drive := service.NewDriveBackupService(db, cfg, nil).
		WithEndpoints(service.DriveEndpoints{TokenURL: server.URL + "/token", UserURL: server.URL + "/userinfo"})
	handler := NewHandler(nil, nil, cfg)
	handler.SetDriveService(drive)
	handler.SetAuthService(service.NewAuthService(db, cfg))

	rec := httptest.NewRecorder()
	handler.handleGoogleCallback(rec, startCallbackAndGetState(t, handler))
	assertDisallowedCallbackCreatedNothing(t, db, rec)
}

func assertDisallowedCallbackCreatedNothing(t *testing.T, db *sql.DB, rec *httptest.ResponseRecorder) {
	t.Helper()
	if !strings.Contains(rec.Header().Get("Location"), "google=error") {
		t.Fatalf("callback for disallowed account location=%s", rec.Header().Get("Location"))
	}
	var userCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&userCount); err != nil || userCount != 0 {
		t.Fatalf("disallowed sign-in created a user row: count=%d err=%v", userCount, err)
	}
	if len(rec.Result().Cookies()) != 1 || rec.Result().Cookies()[0].Name != oauthStateCookieName {
		t.Fatalf("disallowed sign-in should only clear the OAuth state cookie, got %+v", rec.Result().Cookies())
	}
}

// TestGoogleCallbackFailsWhenExchangeCodeFails exercises the handler-level
// path where the state cookie matches (so the earlier checks pass) but the
// token exchange itself fails against Google.
func TestGoogleCallbackFailsWhenExchangeCodeFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusBadGateway)
	}))
	t.Cleanup(server.Close)
	dir := t.TempDir()
	cfg := &config.Config{
		DataDir: dir, GoogleClientID: "id", GoogleClientSecret: "secret", GoogleBackupEncryptionKey: googleTestEncryptionKey,
		AuthTokenEncryptionKey: googleTestEncryptionKey, GoogleFrontendURL: "http://localhost:3000",
		AuthAllowedEmails: []string{"allowed@example.com"},
	}
	db := googleTestDB(t, dir)
	drive := service.NewDriveBackupService(db, cfg, nil).
		WithEndpoints(service.DriveEndpoints{TokenURL: server.URL})
	handler := NewHandler(nil, nil, cfg)
	handler.SetDriveService(drive)
	handler.SetAuthService(service.NewAuthService(db, cfg))

	rec := httptest.NewRecorder()
	handler.handleGoogleCallback(rec, startCallbackAndGetState(t, handler))
	if !strings.Contains(rec.Header().Get("Location"), "google=error") {
		t.Fatalf("callback with failing token exchange location=%s", rec.Header().Get("Location"))
	}
}

func startCallbackAndGetState(t *testing.T, handler *Handler) *http.Request {
	t.Helper()
	connect := httptest.NewRecorder()
	handler.handleGoogleConnect(connect, httptest.NewRequest(http.MethodGet, "/api/google/connect", nil))
	location := connect.Header().Get("Location")
	state := location[strings.Index(location, "state=")+6:]
	if idx := strings.Index(state, "&"); idx >= 0 {
		state = state[:idx]
	}
	request := httptest.NewRequest(http.MethodGet, "/api/google/callback?code=ok&state="+state, nil)
	for _, cookie := range connect.Result().Cookies() {
		request.AddCookie(cookie)
	}
	return request
}

func TestGoogleCallbackFailsCleanlyWhenSaveAccountCannotPersist(t *testing.T) {
	handler, _, _ := fakeGoogleHandler(t)
	db := googleHandlerDB(t, handler)
	if _, err := db.Exec(`DROP TABLE google_drive_accounts`); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	handler.handleGoogleCallback(rec, startCallbackAndGetState(t, handler))
	if !strings.Contains(rec.Header().Get("Location"), "google=error") {
		t.Fatalf("callback with no account storage location=%s", rec.Header().Get("Location"))
	}
}

func TestGoogleCallbackFailsCleanlyWhenIssueSessionCannotPersist(t *testing.T) {
	handler, _, _ := fakeGoogleHandler(t)
	db := googleHandlerDB(t, handler)
	if _, err := db.Exec(`DROP TABLE sessions`); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	handler.handleGoogleCallback(rec, startCallbackAndGetState(t, handler))
	if !strings.Contains(rec.Header().Get("Location"), "google=error") {
		t.Fatalf("callback with no session storage location=%s", rec.Header().Get("Location"))
	}
}

// googleHandlerDB recovers the *sql.DB a Handler's AuthService was built
// with, for tests that need to break a specific table underneath it.
func googleHandlerDB(t *testing.T, handler *Handler) *sql.DB {
	t.Helper()
	return handler.auth.DatabaseForTesting()
}

func TestGoogleRevokeReturns500OnNonRevocationFailureError(t *testing.T) {
	handler, _, userID := googleHandler(t, true)
	db := googleHandlerDB(t, handler)
	if _, err := db.Exec(`DROP TABLE google_drive_accounts`); err != nil {
		t.Fatal(err)
	}
	request := withUserID(httptest.NewRequest(http.MethodPost, "/api/google/revoke", nil), userID)
	request.Header.Set("Origin", "http://localhost:3000")
	rec := httptest.NewRecorder()
	handler.handleGoogleRevoke(rec, request)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("revoke with broken storage status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGoogleRevokeReturns500WhenSignOutAllSessionsFails(t *testing.T) {
	handler, _, userID := googleHandler(t, true)
	db := googleHandlerDB(t, handler)
	if _, err := db.Exec(`DROP TABLE sessions`); err != nil {
		t.Fatal(err)
	}
	// No Drive account was ever connected, so Revoke itself succeeds
	// trivially; SignOutAllSessions is what fails here.
	request := withUserID(httptest.NewRequest(http.MethodPost, "/api/google/revoke", nil), userID)
	request.Header.Set("Origin", "http://localhost:3000")
	rec := httptest.NewRecorder()
	handler.handleGoogleRevoke(rec, request)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("revoke with broken session storage status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleTestSessionFailsWithNoResolvableEmail(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{DataDir: dir, TestMode: true}
	db := googleTestDB(t, dir)
	handler := NewHandler(nil, nil, cfg)
	handler.SetAuthService(service.NewAuthService(db, cfg))
	rec := httptest.NewRecorder()
	handler.handleTestSession(rec, httptest.NewRequest(http.MethodPost, "/api/test/session", strings.NewReader(`{}`)))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("test session with no resolvable email status=%d body=%s", rec.Code, rec.Body.String())
	}
}
