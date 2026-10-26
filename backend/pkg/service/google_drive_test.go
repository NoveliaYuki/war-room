package service

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"war-room/backend/pkg/config"
	"war-room/backend/pkg/database"
	"war-room/backend/pkg/models"
)

const driveTestEncryptionKey = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="

func fakeDriveServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

func mustNoErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func waitForDrive(t *testing.T, drive *DriveBackupService, userID string) {
	t.Helper()
	for i := 0; i < 200; i++ {
		drive.mu.Lock()
		running := drive.running[userID]
		drive.mu.Unlock()
		if !running {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("drive backup did not finish")
}

// writeExport returns a DriveBackupService export function that writes a
// fixed payload, ignoring userID (these tests exercise Drive plumbing, not
// per-user export scoping, which belongs to the job export tests).
func writeExport(payload string) func(string, io.Writer) error {
	return func(_ string, w io.Writer) error {
		_, err := io.WriteString(w, payload)
		return err
	}
}

// newDriveTestDB opens a fresh, fully migrated application database for
// Drive service tests (google_drive_accounts has a foreign key to users).
func newDriveTestDB(t *testing.T, dir string) *sql.DB {
	t.Helper()
	cfg := &config.Config{
		DataDir: dir, DBPath: filepath.Join(dir, "drive.db"), BackupPath: filepath.Join(dir, "backup.json"),
		AttachmentsDir: filepath.Join(dir, "attachments"), CVDir: filepath.Join(dir, "cvs"), LogosDir: filepath.Join(dir, "logos"),
	}
	db, err := database.InitDB(cfg)
	if err != nil {
		t.Fatalf("init database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func seedDriveUser(t *testing.T, db *sql.DB, userID string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO users (id, google_sub, email, name, picture_url) VALUES (?, ?, ?, ?, ?)`,
		userID, "sub-"+userID, userID+"@example.com", userID, ""); err != nil {
		t.Fatalf("seed user %q: %v", userID, err)
	}
}

func driveTestConfig(dir string) *config.Config {
	return &config.Config{
		DataDir: dir, GoogleClientID: "id", GoogleClientSecret: "secret",
		GoogleBackupEncryptionKey: driveTestEncryptionKey, AuthTokenEncryptionKey: driveTestEncryptionKey,
	}
}

var fakeDriveCreatedBody string
var fakeDriveCreatedMetadata string

func fakeDriveAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodDelete {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	routes := map[string]http.HandlerFunc{
		"/token": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, `{"access_token":"at","refresh_token":"rt","expires_in":3600}`)
		},
		"/userinfo": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, `{"sub":"google-sub-1","email":"person@example.com","email_verified":true,"name":"Person"}`)
		},
		"/revoke": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		},
		"/files": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, `{"files":[{"id":"f1","name":"Backups"}]}`)
		},
		"/upload/files": func(w http.ResponseWriter, r *http.Request) {
			metadata, _ := io.ReadAll(r.Body)
			fakeDriveCreatedMetadata = string(metadata)
			w.Header().Set("Location", "http://"+r.Host+"/upload/session")
			w.WriteHeader(http.StatusOK)
		},
		"/upload/session": func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			fakeDriveCreatedBody = string(body)
			_, _ = io.WriteString(w, `{"id":"file-1"}`)
		},
	}
	if route, ok := routes[r.URL.Path]; ok {
		route(w, r)
		return
	}
	http.NotFound(w, r)
}

func fakeStaleDriveAPI(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/token":
		_, _ = io.WriteString(w, `{"access_token":"at","expires_in":3600}`)
	case r.URL.Path == "/files":
		_, _ = io.WriteString(w, `{"files":[]}`)
	case strings.HasPrefix(r.URL.Path, "/upload/files/"):
		http.Error(w, "gone", http.StatusNotFound)
	case r.URL.Path == "/upload/files" && r.Method == http.MethodPost:
		w.Header().Set("Location", "http://"+r.Host+"/upload/session")
		w.WriteHeader(http.StatusOK)
	case r.URL.Path == "/upload/session" && r.Method == http.MethodPut:
		_, _ = io.WriteString(w, `{"id":"fresh-1"}`)
	default:
		http.NotFound(w, r)
	}
}

func oauthState(t *testing.T, authURL string) string {
	t.Helper()
	state := authURL[strings.Index(authURL, "state=")+6:]
	if idx := strings.Index(state, "&"); idx >= 0 {
		state = state[:idx]
	}
	return state
}

// signIn runs the combined exchange-and-save flow a successful OAuth
// callback performs: ExchangeCode (pure HTTP) followed by SaveAccount
// (persistence), returning the identity Google reported.
func signIn(t *testing.T, drive *DriveBackupService, userID, code, state string) googleUserInfo {
	t.Helper()
	info, token, err := drive.ExchangeCode(code, state)
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if err := drive.SaveAccount(userID, token); err != nil {
		t.Fatalf("SaveAccount: %v", err)
	}
	return info
}

func TestDriveTriggerOnlyOnJobCreateAndStatusChange(t *testing.T) {
	fixture := newAttachmentOwnerFixture(t)
	calls := 0
	fixture.service.SetDriveTrigger(func(string) { calls++ })

	job, err := fixture.service.CreateJob(testUserID, models.CreateJobInput{CompanyName: "DriveCo", PositionTitle: "Engineer"})
	mustNoErr(t, err)
	if calls != 1 {
		t.Fatalf("after CreateJob calls=%d, want 1", calls)
	}
	stage, err := fixture.service.CreateStage(testUserID, models.CreateStageInput{JobID: job.ID, StageType: models.StageHR})
	mustNoErr(t, err)
	if calls != 2 {
		t.Fatalf("first stage changes job status and must trigger backup: calls=%d, want 2", calls)
	}
	if _, err := fixture.service.CreateQuestion(testUserID, models.CreateQuestionInput{StageID: stage.ID, Question: "Why us?"}); err != nil {
		t.Fatal(err)
	}
	title := "Renamed role"
	if _, err := fixture.service.UpdateJob(testUserID, job.ID, models.UpdateJobInput{PositionTitle: &title}); err != nil {
		t.Fatal(err)
	}
	status := models.StatusRejected
	if _, err := fixture.service.UpdateJob(testUserID, job.ID, models.UpdateJobInput{Status: &status}); err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatalf("status change must trigger backup: calls=%d, want 3", calls)
	}
	if _, err := fixture.service.UpdateJob(testUserID, job.ID, models.UpdateJobInput{Status: &status}); err != nil {
		t.Fatal(err)
	}
	mustNoErr(t, fixture.service.DeleteStage(testUserID, stage.ID))
	if calls != 3 {
		t.Fatalf("non-status changes and unchanged status must not trigger backup: calls=%d, want 3", calls)
	}
}

func TestDriveTriggerRunsWhenStatusPersistsButTechnologyUpdateFails(t *testing.T) {
	fixture := newAttachmentOwnerFixture(t)
	if err := fixture.repo.CreateTechnology(testUserID, models.Technology{ID: "drive-tech", Name: "Go"}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	fixture.service.SetDriveTrigger(func(string) { calls++ })
	job, err := fixture.service.CreateJob(testUserID, models.CreateJobInput{CompanyName: "DriveCo", PositionTitle: "Engineer"})
	mustNoErr(t, err)
	if _, err := fixture.db.Exec(`CREATE TRIGGER reject_drive_technology BEFORE INSERT ON job_technologies BEGIN SELECT RAISE(ABORT, 'assignment denied'); END`); err != nil {
		t.Fatal(err)
	}
	status := models.StatusRejected
	_, err = fixture.service.UpdateJob(testUserID, job.ID, models.UpdateJobInput{Status: &status, TechnologyIDs: []string{"drive-tech"}})
	if err == nil {
		t.Fatal("technology assignment should fail")
	}
	if calls != 2 {
		t.Fatalf("persisted status change triggers backup after later update failure: calls=%d, want 2", calls)
	}
	updated, err := fixture.repo.GetJobByID(testUserID, job.ID)
	if err != nil || updated.Status != models.StatusRejected {
		t.Fatalf("persisted status=%q err=%v", updated.Status, err)
	}
}

func TestDriveOAuthFoldersAndTrigger(t *testing.T) {
	const userID = "user-1"
	server := fakeDriveServer(t, fakeDriveAPI)
	dir := t.TempDir()
	db := newDriveTestDB(t, dir)
	seedDriveUser(t, db, userID)
	cfg := driveTestConfig(dir)
	cfg.GoogleClientID, cfg.GoogleRedirectURI = "509809460865-client.apps.googleusercontent.com", "http://localhost/cb"
	drive := NewDriveBackupService(db, cfg, writeExport("zip"))
	drive.tokenURL, drive.userURL, drive.driveBase, drive.uploadBase = server.URL+"/token", server.URL+"/userinfo", server.URL, server.URL+"/upload"

	authURL := assertValidCombinedAuthURL(t, drive)
	state := oauthState(t, authURL)
	info := signIn(t, drive, userID, "code", state)
	if info.Email != "person@example.com" {
		t.Fatalf("identity email=%q", info.Email)
	}
	if status := drive.Status(userID); !status.Connected {
		t.Fatalf("status after sign-in: %+v", status)
	}
	assertPickerAccessToken(t, drive, userID, "at")
	if _, _, err := drive.ExchangeCode("code", state); err == nil {
		t.Fatal("replayed OAuth state must be rejected")
	}
	assertFoldersMatch(t, drive, userID, "Back", "f1")
}

func assertValidCombinedAuthURL(t *testing.T, drive *DriveBackupService) string {
	t.Helper()
	authURL, err := drive.AuthURL()
	if err != nil || !strings.Contains(authURL, "access_type=offline") || !strings.Contains(authURL, "profile") {
		t.Fatalf("auth url: %v %s", err, authURL)
	}
	return authURL
}

func assertPickerAccessToken(t *testing.T, drive *DriveBackupService, userID, wantToken string) {
	t.Helper()
	token, err := drive.PickerAccessToken(userID)
	if err != nil || token != wantToken {
		t.Fatalf("picker access token=%q error=%v", token, err)
	}
}

func assertFoldersMatch(t *testing.T, drive *DriveBackupService, userID, query, wantFolderID string) {
	t.Helper()
	folders, err := drive.ListFolders(userID, query)
	if err != nil || len(folders) != 1 || folders[0].ID != wantFolderID {
		t.Fatalf("folders: %v %+v", err, folders)
	}
}

func TestGoogleProjectNumberUsesConfiguredValueOrOAuthClientPrefix(t *testing.T) {
	tests := []struct {
		name, clientID, configured, want string
	}{
		{name: "derive from web client ID", clientID: "509809460865-example.apps.googleusercontent.com", want: "509809460865"},
		{name: "prefer configured value", clientID: "509809460865-example.apps.googleusercontent.com", configured: "12345", want: "12345"},
		{name: "reject malformed client ID", clientID: "project-example.apps.googleusercontent.com", want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := googleProjectNumber(test.clientID, test.configured); got != test.want {
				t.Fatalf("googleProjectNumber()=%q, want %q", got, test.want)
			}
		})
	}
	dir := t.TempDir()
	db := newDriveTestDB(t, dir)
	cfg := &config.Config{DataDir: dir, GoogleClientID: "509809460865-client.apps.googleusercontent.com"}
	if got := NewDriveBackupService(db, cfg, nil).Status("user-1").PickerAppID; got != "509809460865" {
		t.Fatalf("status picker app ID=%q, want derived project number", got)
	}
}

func TestDriveOAuthStateCacheIsBoundedByEviction(t *testing.T) {
	dir := t.TempDir()
	db := newDriveTestDB(t, dir)
	drive := NewDriveBackupService(db, driveTestConfig(dir), nil)
	for range maxGoogleOAuthStates {
		if _, err := drive.AuthURL(); err != nil {
			t.Fatalf("AuthURL at capacity boundary: %v", err)
		}
	}
	if _, err := drive.AuthURL(); err != nil {
		t.Fatalf("AuthURL at capacity should evict the oldest state, got %v", err)
	}
	if len(drive.states) != maxGoogleOAuthStates {
		t.Fatalf("cached OAuth states=%d, want %d", len(drive.states), maxGoogleOAuthStates)
	}
}

func TestDriveOAuthRequestsDoNotFollowRedirects(t *testing.T) {
	var redirected bool
	redirectTarget := fakeDriveServer(t, func(w http.ResponseWriter, _ *http.Request) {
		redirected = true
		_, _ = io.WriteString(w, `{"access_token":"leaked"}`)
	})
	redirectSource := fakeDriveServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, redirectTarget.URL, http.StatusTemporaryRedirect)
	})
	dir := t.TempDir()
	db := newDriveTestDB(t, dir)
	cfg := driveTestConfig(dir)
	drive := NewDriveBackupService(db, cfg, nil)
	drive.tokenURL = redirectSource.URL
	_, err := drive.exchange(url.Values{"client_secret": {cfg.GoogleClientSecret}})
	if err == nil {
		t.Fatal("token exchange must reject redirects")
	}
	if redirected {
		t.Fatal("OAuth request followed a redirect to another host")
	}
}

func TestDriveFolderSearchEscapesQueryLiterals(t *testing.T) {
	const userID = "user-1"
	queries := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries <- r.URL.Query().Get("q")
		_, _ = io.WriteString(w, `{"files":[]}`)
	}))
	defer server.Close()

	dir := t.TempDir()
	db := newDriveTestDB(t, dir)
	seedDriveUser(t, db, userID)
	drive := NewDriveBackupService(db, driveTestConfig(dir), nil).WithEndpoints(DriveEndpoints{DriveBase: server.URL})
	mustNoErr(t, drive.saveAccount(userID, driveAccount{RefreshToken: "rt", AccessToken: "at", Expiry: time.Now().Add(time.Hour)}))
	if _, err := drive.ListFolders(userID, "x\\'y"); err != nil {
		t.Fatal(err)
	}
	want := "mimeType='application/vnd.google-apps.folder' and trashed=false and name contains 'x" + strings.Repeat("\\", 3) + "'y'"
	if got := <-queries; got != want {
		t.Fatalf("folder query=%q, want escaped literal %q", got, want)
	}
}

func TestDriveBackupSerializesTokenAndFolderUpdates(t *testing.T) {
	const userID = "user-1"
	tokenStarted := make(chan struct{})
	releaseToken := make(chan struct{})
	server := httptest.NewServer(blockingDriveTokenHandler(tokenStarted, releaseToken))
	defer server.Close()

	dir := t.TempDir()
	db := newDriveTestDB(t, dir)
	seedDriveUser(t, db, userID)
	drive := NewDriveBackupService(db, driveTestConfig(dir), writeExport("zip")).
		WithEndpoints(DriveEndpoints{TokenURL: server.URL + "/token", DriveBase: server.URL, UploadBase: server.URL + "/upload"})
	mustNoErr(t, drive.saveAccount(userID, driveAccount{RefreshToken: "rt"}))

	backupDone := make(chan error, 1)
	go func() { backupDone <- drive.BackupNow(userID) }()
	<-tokenStarted
	folderDone := make(chan error, 1)
	go func() { folderDone <- drive.SetFolder(userID, "folder-1", "Backups") }()
	select {
	case err := <-folderDone:
		t.Fatalf("folder update should wait for backup state update, got %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(releaseToken)
	if err := <-backupDone; err != nil {
		t.Fatalf("backup: %v", err)
	}
	if err := <-folderDone; err != nil {
		t.Fatalf("folder update: %v", err)
	}
	assertSerializedDriveState(t, drive, userID)
}

func blockingDriveTokenHandler(tokenStarted chan struct{}, releaseToken <-chan struct{}) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			close(tokenStarted)
			<-releaseToken
			_, _ = io.WriteString(w, `{"access_token":"at","expires_in":3600}`)
		case "/upload/files":
			w.Header().Set("Location", "http://"+r.Host+"/upload/session")
			w.WriteHeader(http.StatusOK)
		case "/upload/session":
			_, _ = io.WriteString(w, `{"id":"file-1"}`)
		default:
			http.NotFound(w, r)
		}
	}
}

func assertSerializedDriveState(t *testing.T, drive *DriveBackupService, userID string) {
	t.Helper()
	account, found, err := drive.loadAccount(userID)
	if err != nil || !found {
		t.Fatalf("load account: found=%t err=%v", found, err)
	}
	if account.RefreshToken != "rt" || account.AccessToken != "at" || account.FolderID != "folder-1" || account.FileID != "file-1" || account.LastStatus != "ok" || account.LastBackupAt == nil {
		t.Fatalf("concurrent updates lost state: %+v", account)
	}
}

func TestDriveTriggerAndUnconfigured(t *testing.T) {
	const userID = "user-1"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			_, _ = io.WriteString(w, `{"access_token":"at","refresh_token":"rt","expires_in":3600}`)
		case "/upload/files":
			w.Header().Set("Location", "http://"+r.Host+"/upload/session")
			w.WriteHeader(http.StatusOK)
		case "/upload/session":
			_, _ = io.WriteString(w, `{"id":"file-9"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	dir := t.TempDir()
	db := newDriveTestDB(t, dir)
	seedDriveUser(t, db, userID)
	drive := NewDriveBackupService(db, driveTestConfig(dir), writeExport("zip"))
	drive.tokenURL, drive.driveBase, drive.uploadBase = server.URL+"/token", server.URL, server.URL+"/upload"
	mustNoErr(t, drive.saveAccount(userID, driveAccount{RefreshToken: "rt"}))
	drive.Trigger(userID)
	waitForDrive(t, drive, userID)
	if drive.Status(userID).LastStatus != "ok" {
		t.Fatalf("triggered backup did not complete: %+v", drive.Status(userID))
	}
	unconfigured := NewDriveBackupService(db, &config.Config{DataDir: dir}, nil)
	if _, err := unconfigured.AuthURL(); err == nil {
		t.Fatal("unconfigured service must reject AuthURL")
	}
	if _, err := unconfigured.ListFolders(userID, ""); err == nil {
		t.Fatal("unconfigured service must reject ListFolders")
	}
}

func TestDriveStaleFileFallbackAndEmptyFolders(t *testing.T) {
	const userID = "user-1"
	server := fakeDriveServer(t, fakeStaleDriveAPI)
	defer server.Close()
	dir := t.TempDir()
	db := newDriveTestDB(t, dir)
	seedDriveUser(t, db, userID)
	drive := NewDriveBackupService(db, driveTestConfig(dir), writeExport("zip"))
	drive.tokenURL, drive.driveBase, drive.uploadBase = server.URL+"/token", server.URL, server.URL+"/upload"
	mustNoErr(t, drive.saveAccount(userID, driveAccount{RefreshToken: "rt", FileID: "stale-1"}))
	folders, err := drive.ListFolders(userID, "")
	if err != nil || len(folders) != 0 {
		t.Fatalf("folders: %v %+v", err, folders)
	}
	mustNoErr(t, drive.BackupNow(userID))
	if status := drive.Status(userID); status.LastStatus != "ok" {
		t.Fatalf("stale file must fall back to create: %+v", status)
	}
	account, found, err := drive.loadAccount(userID)
	if err != nil || !found || account.FileID != "fresh-1" {
		t.Fatalf("stored file id: %+v found=%t err=%v", account, found, err)
	}
	// A second Trigger while one is running must coalesce, not error or hang.
	drive.Trigger(userID)
	drive.Trigger(userID)
	waitForDrive(t, drive, userID)
}

func TestDriveCallbackAndExportFailures(t *testing.T) {
	const userID = "user-1"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			_, _ = io.WriteString(w, `{"access_token":"at","expires_in":3600}`)
		case "/userinfo":
			http.Error(w, "no", http.StatusUnauthorized)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	dir := t.TempDir()
	db := newDriveTestDB(t, dir)
	seedDriveUser(t, db, userID)
	drive := NewDriveBackupService(db, driveTestConfig(dir), nil).WithEndpoints(DriveEndpoints{TokenURL: server.URL + "/token", UserURL: server.URL + "/userinfo"})
	authURL, err := drive.AuthURL()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := drive.ExchangeCode("code", oauthState(t, authURL)); err == nil {
		t.Fatal("userinfo failure must fail the exchange")
	}
	if err := drive.BackupNow(userID); err == nil {
		t.Fatal("backup with no connection must fail")
	}
	connected := NewDriveBackupService(db, driveTestConfig(dir), nil)
	connected.revokeURL = server.URL + "/token"
	mustNoErr(t, connected.saveAccount(userID, driveAccount{RefreshToken: "rt"}))
	if err := connected.BackupNow(userID); err == nil {
		t.Fatal("backup without exporter must fail")
	}
	mustNoErr(t, connected.Revoke(userID))
	// Token response without a refresh token must be rejected.
	authURL, err = drive.AuthURL()
	if err != nil {
		t.Fatal(err)
	}
	drive.userURL = server.URL + "/token"
	if _, _, err := drive.ExchangeCode("code", oauthState(t, authURL)); err == nil {
		t.Fatal("missing refresh token must fail the exchange")
	}
}

func TestDriveRevokeRevokesGoogleTokenAndClearsLocalState(t *testing.T) {
	const userID = "user-1"
	var revoked string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/revoke" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse revoke form: %v", err)
			return
		}
		revoked = r.Form.Get("token")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	dir := t.TempDir()
	db := newDriveTestDB(t, dir)
	seedDriveUser(t, db, userID)
	drive := NewDriveBackupService(db, driveTestConfig(dir), nil)
	drive.revokeURL = server.URL + "/revoke"
	mustNoErr(t, drive.saveAccount(userID, driveAccount{RefreshToken: "refresh-token"}))
	mustNoErr(t, drive.Revoke(userID))
	if revoked != "refresh-token" {
		t.Fatalf("revoked token=%q", revoked)
	}
	if drive.Status(userID).Connected {
		t.Fatal("revoke must clear local account state")
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM google_drive_accounts WHERE user_id = ?`, userID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("drive account row still exists: count=%d err=%v", count, err)
	}
}

func TestDriveRevokeClearsLocalStateWhenRevocationFails(t *testing.T) {
	const userID = "user-1"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "revocation unavailable", http.StatusBadGateway)
	}))
	defer server.Close()
	dir := t.TempDir()
	db := newDriveTestDB(t, dir)
	seedDriveUser(t, db, userID)
	drive := NewDriveBackupService(db, driveTestConfig(dir), nil)
	drive.revokeURL = server.URL
	mustNoErr(t, drive.saveAccount(userID, driveAccount{RefreshToken: "refresh-token"}))
	if err := drive.Revoke(userID); !errors.Is(err, ErrGoogleRevocationFailed) {
		t.Fatalf("revoke error=%v", err)
	}
	if drive.Status(userID).Connected {
		t.Fatal("failed Google revocation must still clear the local token")
	}
}

func TestDriveRevokeClearsLocalStateWhenRevocationRequestCannotStart(t *testing.T) {
	for name, endpoint := range map[string]string{"invalid URL": "://invalid", "network failure": "http://127.0.0.1:1/revoke"} {
		t.Run(name, func(t *testing.T) {
			const userID = "user-1"
			dir := t.TempDir()
			db := newDriveTestDB(t, dir)
			seedDriveUser(t, db, userID)
			drive := NewDriveBackupService(db, driveTestConfig(dir), nil)
			drive.revokeURL = endpoint
			mustNoErr(t, drive.saveAccount(userID, driveAccount{RefreshToken: "refresh-token"}))
			if err := drive.Revoke(userID); !errors.Is(err, ErrGoogleRevocationFailed) {
				t.Fatalf("revoke error=%v", err)
			}
			if drive.Status(userID).Connected {
				t.Fatal("local account must be cleared when revocation cannot start")
			}
		})
	}
}

func TestDriveAccountStoreAndNetworkErrors(t *testing.T) {
	const userID = "user-1"
	dir := t.TempDir()
	db := newDriveTestDB(t, dir)
	seedDriveUser(t, db, userID)

	unknownUser := NewDriveBackupService(db, driveTestConfig(dir), nil)
	if unknownUser.Status("no-such-user").Connected {
		t.Fatal("unknown user must report disconnected")
	}
	if _, err := unknownUser.ListFolders("no-such-user", ""); err == nil {
		t.Fatal("unconnected user must fail ListFolders")
	}

	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	deadURL := dead.URL
	dead.Close()
	network := NewDriveBackupService(db, driveTestConfig(dir), writeExport("zip")).WithEndpoints(DriveEndpoints{TokenURL: deadURL + "/token", DriveBase: deadURL, UploadBase: deadURL + "/upload"})
	mustNoErr(t, network.saveAccount(userID, driveAccount{RefreshToken: "rt"}))
	if _, err := network.ListFolders(userID, ""); err == nil {
		t.Fatal("dead Google endpoints must fail ListFolders")
	}
	if err := network.BackupNow(userID); err == nil {
		t.Fatal("dead Google endpoints must fail BackupNow")
	}
}

func TestDriveMalformedGoogleResponses(t *testing.T) {
	const userID = "user-1"
	mode := "bad-files"
	server := httptest.NewServer(malformedGoogleResponseAPI(&mode))
	defer server.Close()
	dir := t.TempDir()
	db := newDriveTestDB(t, dir)
	seedDriveUser(t, db, userID)
	drive := NewDriveBackupService(db, driveTestConfig(dir), writeExport("zip")).
		WithEndpoints(DriveEndpoints{TokenURL: server.URL + "/token", DriveBase: server.URL, UploadBase: server.URL + "/upload"})
	mustNoErr(t, drive.saveAccount(userID, driveAccount{RefreshToken: "rt"}))
	if _, err := drive.ListFolders(userID, ""); err == nil {
		t.Fatal("malformed folder response must fail")
	}
	mode = "error-files"
	if _, err := drive.ListFolders(userID, ""); err == nil {
		t.Fatal("folder API error must fail")
	}
	if err := drive.BackupNow(userID); err == nil {
		t.Fatal("upload without a file id must fail")
	}
}

func malformedGoogleResponseAPI(mode *string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			_, _ = io.WriteString(w, `{"access_token":"at","expires_in":3600}`)
		case "/files":
			if *mode == "bad-files" {
				_, _ = io.WriteString(w, `not-json`)
			} else {
				http.Error(w, "no", http.StatusInternalServerError)
			}
		case "/upload/files":
			w.Header().Set("Location", "http://"+r.Host+"/upload/session")
			w.WriteHeader(http.StatusOK)
		case "/upload/session":
			_, _ = io.WriteString(w, `{}`)
		default:
			http.NotFound(w, r)
		}
	}
}

func TestDriveErrorPaths(t *testing.T) {
	const userID = "user-1"
	dir := t.TempDir()
	db := newDriveTestDB(t, dir)
	seedDriveUser(t, db, userID)
	drive := NewDriveBackupService(db, driveTestConfig(dir), func(string, io.Writer) error { return io.ErrClosedPipe })
	if err := drive.BackupNow(userID); err == nil {
		t.Fatal("backup without connection must fail")
	}
	if err := drive.SetFolder(userID, "f", "F"); err == nil {
		t.Fatal("SetFolder without connection must fail")
	}
	// Revoking an account that was never connected has nothing to revoke
	// with Google and must still succeed trivially.
	mustNoErr(t, drive.Revoke(userID))
	if status := drive.Status(userID); status.Connected {
		t.Fatal("never-connected account must report disconnected")
	}
	if _, err := drive.ListFolders(userID, ""); err == nil {
		t.Fatal("unconnected account must fail ListFolders")
	}
}

func TestDriveAccountCredentialsAreEncryptedAtRest(t *testing.T) {
	const userID = "user-1"
	dir := t.TempDir()
	db := newDriveTestDB(t, dir)
	seedDriveUser(t, db, userID)
	drive := NewDriveBackupService(db, driveTestConfig(dir), nil)
	mustNoErr(t, drive.saveAccount(userID, driveAccount{RefreshToken: "private-refresh-token"}))

	var stored []byte
	mustNoErr(t, db.QueryRow(`SELECT credentials_encrypted FROM google_drive_accounts WHERE user_id = ?`, userID).Scan(&stored))
	if bytes.Contains(stored, []byte("private-refresh-token")) || !bytes.HasPrefix(stored, []byte(accountEnvelopeMagic)) {
		t.Fatal("Drive credentials must be encrypted at rest")
	}

	loaded, found, err := drive.loadAccount(userID)
	mustNoErr(t, err)
	if !found || loaded.RefreshToken != "private-refresh-token" {
		t.Fatal("encrypted drive credentials did not load")
	}
}

func TestDriveAccountRejectsTamperedEncryptedCredentials(t *testing.T) {
	const userID = "user-1"
	dir := t.TempDir()
	db := newDriveTestDB(t, dir)
	seedDriveUser(t, db, userID)
	drive := NewDriveBackupService(db, driveTestConfig(dir), nil)
	mustNoErr(t, drive.saveAccount(userID, driveAccount{RefreshToken: "private"}))

	var stored []byte
	mustNoErr(t, db.QueryRow(`SELECT credentials_encrypted FROM google_drive_accounts WHERE user_id = ?`, userID).Scan(&stored))
	stored[len(stored)-1] ^= 1
	mustNoErr(t, errFromExec(db.Exec(`UPDATE google_drive_accounts SET credentials_encrypted = ? WHERE user_id = ?`, stored, userID)))

	if _, _, err := drive.loadAccount(userID); err == nil {
		t.Fatal("tampered Drive credentials must not load")
	}
}

func errFromExec(_ sql.Result, err error) error { return err }

func TestDriveAccountRejectsCorruptDecryptedCredentials(t *testing.T) {
	const userID = "user-1"
	dir := t.TempDir()
	db := newDriveTestDB(t, dir)
	seedDriveUser(t, db, userID)
	cfg := driveTestConfig(dir)
	drive := NewDriveBackupService(db, cfg, nil)
	// Seal garbage (not valid driveCredentials JSON) so it decrypts
	// successfully but fails the json.Unmarshal step that follows.
	key, err := drive.authTokenKey()
	mustNoErr(t, err)
	sealed, err := sealDriveData(key, accountEnvelopeMagic, accountEnvelopeAAD, []byte("not-json"))
	mustNoErr(t, err)
	mustNoErr(t, errFromExec(db.Exec(`INSERT INTO google_drive_accounts (user_id, credentials_encrypted) VALUES (?, ?)`, userID, sealed)))
	if _, _, err := drive.loadAccount(userID); err == nil {
		t.Fatal("decrypted-but-malformed Drive credentials must not load")
	}
}

func TestDriveAccountRejectsCorruptPreviousFileIDs(t *testing.T) {
	const userID = "user-1"
	dir := t.TempDir()
	db := newDriveTestDB(t, dir)
	seedDriveUser(t, db, userID)
	drive := NewDriveBackupService(db, driveTestConfig(dir), nil)
	mustNoErr(t, drive.saveAccount(userID, driveAccount{RefreshToken: "rt"}))
	mustNoErr(t, errFromExec(db.Exec(`UPDATE google_drive_accounts SET previous_file_ids_json = 'not-json' WHERE user_id = ?`, userID)))
	if _, _, err := drive.loadAccount(userID); err == nil {
		t.Fatal("corrupt previous_file_ids_json must not load")
	}
}

func TestDriveBackupKeepsOldCopyUntilReplacementAndRetriesCleanup(t *testing.T) {
	const userID = "user-1"
	deleteFails := true
	uploads := 0
	var requests []string
	server := httptest.NewServer(fakeDriveReplacementAPI(&deleteFails, &uploads, &requests))
	defer server.Close()
	dir := t.TempDir()
	db := newDriveTestDB(t, dir)
	seedDriveUser(t, db, userID)
	drive := NewDriveBackupService(db, driveTestConfig(dir), writeExport("zip")).
		WithEndpoints(DriveEndpoints{TokenURL: server.URL + "/token", DriveBase: server.URL, UploadBase: server.URL + "/upload"})
	mustNoErr(t, drive.saveAccount(userID, driveAccount{RefreshToken: "rt", FileID: "old-file"}))
	mustNoErr(t, drive.BackupNow(userID))
	account, _, err := drive.loadAccount(userID)
	mustNoErr(t, err)
	assertPendingDriveCleanup(t, account, requests)
	deleteFails = false
	mustNoErr(t, drive.BackupNow(userID))
	account, _, err = drive.loadAccount(userID)
	mustNoErr(t, err)
	assertFinishedDriveCleanup(t, account)
}

func TestDriveBackupRemovesNewUploadWhenLocalStateCannotCommit(t *testing.T) {
	const userID = "user-1"
	dir := t.TempDir()
	db := newDriveTestDB(t, dir)
	seedDriveUser(t, db, userID)
	var requests []string
	server := httptest.NewServer(fakeDriveStoreFailureAPI(db, userID, &requests))
	defer server.Close()
	drive := NewDriveBackupService(db, driveTestConfig(dir), writeExport("zip")).
		WithEndpoints(DriveEndpoints{TokenURL: server.URL + "/token", DriveBase: server.URL, UploadBase: server.URL + "/upload"})
	mustNoErr(t, drive.saveAccount(userID, driveAccount{RefreshToken: "rt", AccessToken: "at", Expiry: time.Now().Add(time.Hour), FileID: "old-file"}))
	if err := drive.BackupNow(userID); err == nil {
		t.Fatal("backup must report a failed local state commit")
	}
	if len(requests) != 3 || requests[0] != "POST /upload/files" || requests[1] != "PUT /upload/session" || requests[2] != "DELETE /files/new-file" {
		t.Fatalf("new upload cleanup order=%v", requests)
	}
}

func fakeDriveStoreFailureAPI(db *sql.DB, userID string, requests *[]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		*requests = append(*requests, r.Method+" "+r.URL.Path)
		switch {
		case r.URL.Path == "/upload/files" && r.Method == http.MethodPost:
			w.Header().Set("Location", "http://"+r.Host+"/upload/session")
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/upload/session" && r.Method == http.MethodPut:
			respondToFailedStoreUpload(w, db, userID)
		case r.URL.Path == "/files/new-file" && r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/token":
			_, _ = io.WriteString(w, `{"access_token":"at","expires_in":3600}`)
		default:
			http.NotFound(w, r)
		}
	}
}

// respondToFailedStoreUpload drops the google_drive_accounts row entirely so
// the subsequent commit-time saveAccount UPSERT still succeeds (there's no
// row-level way to make a single UPDATE fail in SQLite the way a corrupt
// JSON file could before); instead this test's real signal is the delete
// race asserted via requests, which is unaffected by this helper no-op.
func respondToFailedStoreUpload(w http.ResponseWriter, db *sql.DB, userID string) {
	if _, err := db.Exec(`DROP TABLE google_drive_accounts`); err != nil {
		http.Error(w, "could not break test store", http.StatusInternalServerError)
		return
	}
	_, _ = io.WriteString(w, `{"id":"new-file"}`)
}

func fakeDriveReplacementAPI(deleteFails *bool, uploads *int, requests *[]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		*requests = append(*requests, r.Method+" "+r.URL.Path)
		switch {
		case r.URL.Path == "/token":
			_, _ = io.WriteString(w, `{"access_token":"at","expires_in":3600}`)
		case r.URL.Path == "/upload/files" && r.Method == http.MethodPost:
			w.Header().Set("Location", "http://"+r.Host+"/upload/session")
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/upload/session" && r.Method == http.MethodPut:
			*uploads++
			_, _ = fmt.Fprintf(w, `{"id":"new-%d"}`, *uploads)
		case strings.HasPrefix(r.URL.Path, "/files/") && r.Method == http.MethodDelete:
			if *deleteFails {
				http.Error(w, "temporary", http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}
}

func assertPendingDriveCleanup(t *testing.T, account driveAccount, requests []string) {
	t.Helper()
	if account.FileID != "new-1" || account.LastStatus != "cleanup_pending" || len(account.PreviousFileIDs) != 1 || len(requests) < 4 || requests[len(requests)-3] != "POST /upload/files" || requests[len(requests)-2] != "PUT /upload/session" || requests[len(requests)-1] != "DELETE /files/old-file" {
		t.Fatalf("replacement and pending cleanup state=%+v requests=%v", account, requests)
	}
}

func assertFinishedDriveCleanup(t *testing.T, account driveAccount) {
	t.Helper()
	if account.LastStatus != "ok" || len(account.PreviousFileIDs) != 0 {
		t.Fatalf("cleanup retry state=%+v", account)
	}
}

func TestDriveFailureIsRecorded(t *testing.T) {
	const userID = "user-1"
	dir := t.TempDir()
	db := newDriveTestDB(t, dir)
	seedDriveUser(t, db, userID)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusBadGateway)
	}))
	defer server.Close()
	drive2 := NewDriveBackupService(db, driveTestConfig(dir), writeExport("zip"))
	drive2.tokenURL, drive2.uploadBase = server.URL+"/token", server.URL+"/upload"
	mustNoErr(t, drive2.saveAccount(userID, driveAccount{RefreshToken: "rt"}))
	if err := drive2.BackupNow(userID); err == nil {
		t.Fatal("failing Google endpoints must fail the backup")
	}
	if status := drive2.Status(userID); status.LastStatus != "error" || status.LastError == "" {
		t.Fatalf("failure must be recorded: %+v", status)
	}
}

func TestDriveBackupNowRecordsExportFailuresAndCleansUp(t *testing.T) {
	const userID = "user-1"
	server := fakeDriveServer(t, fakeDriveAPI)
	for name, export := range map[string]func(string, io.Writer) error{
		"exporter error": func(string, io.Writer) error { return errors.New("export exploded") },
		"unconfigured":   nil,
	} {
		dir := t.TempDir()
		db := newDriveTestDB(t, dir)
		seedDriveUser(t, db, userID)
		drive := NewDriveBackupService(db, driveTestConfig(dir), export)
		drive.tokenURL, drive.driveBase, drive.uploadBase = server.URL+"/token", server.URL, server.URL+"/upload"
		mustNoErr(t, drive.saveAccount(userID, driveAccount{RefreshToken: "rt"}))
		if err := drive.BackupNow(userID); err == nil {
			t.Fatalf("%s: backup must fail", name)
		}
		if status := drive.Status(userID); status.LastStatus != "error" || status.LastError == "" {
			t.Fatalf("%s: failure not recorded: %+v", name, status)
		}
		if entries, _ := os.ReadDir(dir); hasBackupTempFile(entries) {
			t.Fatalf("%s: temp file left behind", name)
		}
	}
}

func hasBackupTempFile(entries []os.DirEntry) bool {
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".war-room-") {
			return true
		}
	}
	return false
}

func TestDriveBackupNowRemovesTempFileAfterUploadFailure(t *testing.T) {
	const userID = "user-1"
	dir := t.TempDir()
	db := newDriveTestDB(t, dir)
	seedDriveUser(t, db, userID)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			_, _ = io.WriteString(w, `{"access_token":"at","expires_in":3600}`)
			return
		}
		http.Error(w, "nope", http.StatusBadGateway)
	}))
	defer server.Close()
	drive := NewDriveBackupService(db, driveTestConfig(dir), writeExport("zip"))
	drive.tokenURL, drive.uploadBase = server.URL+"/token", server.URL+"/upload"
	mustNoErr(t, drive.saveAccount(userID, driveAccount{RefreshToken: "rt"}))
	if err := drive.BackupNow(userID); err == nil {
		t.Fatal("upload failure must fail the backup")
	}
	if entries, _ := os.ReadDir(dir); hasBackupTempFile(entries) {
		t.Fatal("temp file left behind after a failed upload")
	}
}

func TestDriveBackupNowUploadsToChosenFolder(t *testing.T) {
	const userID = "user-1"
	fakeDriveCreatedBody, fakeDriveCreatedMetadata = "", ""
	server := fakeDriveServer(t, fakeDriveAPI)

	dir := t.TempDir()
	db := newDriveTestDB(t, dir)
	seedDriveUser(t, db, userID)
	drive := NewDriveBackupService(db, driveTestConfig(dir), writeExport("zip-bytes"))
	drive.tokenURL, drive.driveBase, drive.uploadBase = server.URL+"/token", server.URL, server.URL+"/upload"
	mustNoErr(t, drive.saveAccount(userID, driveAccount{RefreshToken: "rt", FolderID: "folder-9", FolderName: "Backups"}))
	mustNoErr(t, drive.BackupNow(userID))
	assertEncryptedUploadBody(t, drive, fakeDriveCreatedMetadata, fakeDriveCreatedBody)
	status := drive.Status(userID)
	if !status.Connected || status.LastStatus != "ok" || status.FolderID != "folder-9" {
		t.Fatalf("status=%+v", status)
	}
	mustNoErr(t, drive.BackupNow(userID))
	account, _, err := drive.loadAccount(userID)
	mustNoErr(t, err)
	if account.BackupVersion != 2 || account.FileID != "file-1" {
		t.Fatalf("second backup should create version 2: %+v", account)
	}
	assertFolderResetAndRevoke(t, drive, userID)
}

func TestDriveResumableUploadHandlesArchivesOverFiveMiB(t *testing.T) {
	const userID = "user-1"
	const archiveSize = (5 << 20) + 1
	archive := bytes.Repeat([]byte("x"), archiveSize)
	var uploadedBytes int
	server := httptest.NewServer(largeResumableUploadAPI(t, &uploadedBytes))
	defer server.Close()
	dir := t.TempDir()
	db := newDriveTestDB(t, dir)
	seedDriveUser(t, db, userID)
	drive := NewDriveBackupService(db, driveTestConfig(dir), func(_ string, w io.Writer) error { _, err := w.Write(archive); return err }).
		WithEndpoints(DriveEndpoints{TokenURL: server.URL + "/token", UploadBase: server.URL + "/upload"})
	mustNoErr(t, drive.saveAccount(userID, driveAccount{RefreshToken: "rt", AccessToken: "at", Expiry: time.Now().Add(time.Hour)}))
	if err := drive.BackupNow(userID); err != nil {
		t.Fatalf("resumable backup: %v", err)
	}
	if uploadedBytes <= 5<<20 {
		t.Fatalf("uploaded bytes=%d, want more than 5 MiB", uploadedBytes)
	}
}

func largeResumableUploadAPI(t *testing.T, uploadedBytes *int) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/upload/files":
			if r.Method != http.MethodPost || r.Header.Get("X-Upload-Content-Type") != "application/octet-stream" || r.Header.Get("X-Upload-Content-Length") == "" {
				t.Errorf("invalid resumable initiation: method=%s headers=%v", r.Method, r.Header)
			}
			w.Header().Set("Location", "http://"+r.Host+"/upload/session")
			w.WriteHeader(http.StatusOK)
		case "/upload/session":
			if r.Method != http.MethodPut {
				t.Errorf("resumable media method=%s, want PUT", r.Method)
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read resumable media: %v", err)
			}
			*uploadedBytes = len(body)
			_, _ = io.WriteString(w, `{"id":"large-file"}`)
		case "/token":
			_, _ = io.WriteString(w, `{"access_token":"at","expires_in":3600}`)
		default:
			http.NotFound(w, r)
		}
	}
}

func TestDriveRejectsUntrustedResumableUploadURL(t *testing.T) {
	const userID = "user-1"
	var attackerReceived bool
	attacker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attackerReceived = true
		if r.Header.Get("Authorization") != "" {
			t.Error("OAuth bearer token was sent to an untrusted upload host")
		}
	}))
	defer attacker.Close()
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/upload/files" {
			w.Header().Set("Location", attacker.URL+"/session")
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path == "/token" {
			_, _ = io.WriteString(w, `{"access_token":"at","expires_in":3600}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer primary.Close()
	dir := t.TempDir()
	db := newDriveTestDB(t, dir)
	seedDriveUser(t, db, userID)
	drive := NewDriveBackupService(db, driveTestConfig(dir), writeExport("zip")).
		WithEndpoints(DriveEndpoints{TokenURL: primary.URL + "/token", UploadBase: primary.URL + "/upload"})
	mustNoErr(t, drive.saveAccount(userID, driveAccount{RefreshToken: "rt", AccessToken: "at", Expiry: time.Now().Add(time.Hour)}))
	if err := drive.BackupNow(userID); err == nil || !strings.Contains(err.Error(), "invalid resumable upload URL") {
		t.Fatalf("untrusted upload URL error=%v", err)
	}
	if attackerReceived {
		t.Fatal("untrusted resumable upload URL must not be contacted")
	}
}

func TestDriveRejectsFailedOrMalformedResumableUploads(t *testing.T) {
	for _, test := range []struct {
		name       string
		initStatus int
		mediaBody  string
		mediaCode  int
	}{
		{name: "initialization status", initStatus: http.StatusBadGateway},
		{name: "media status", mediaBody: `{"id":"file"}`, mediaCode: http.StatusBadGateway},
		{name: "malformed media response", mediaBody: "not-json", mediaCode: http.StatusOK},
		{name: "missing media file id", mediaBody: `{}`, mediaCode: http.StatusOK},
	} {
		t.Run(test.name, func(t *testing.T) {
			const userID = "user-1"
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/upload/files":
					if test.initStatus != 0 {
						http.Error(w, "upload initialization failed", test.initStatus)
						return
					}
					w.Header().Set("Location", "http://"+r.Host+"/upload/session")
					w.WriteHeader(http.StatusOK)
				case "/upload/session":
					w.WriteHeader(test.mediaCode)
					_, _ = io.WriteString(w, test.mediaBody)
				case "/token":
					_, _ = io.WriteString(w, `{"access_token":"at","expires_in":3600}`)
				default:
					http.NotFound(w, r)
				}
			}))
			t.Cleanup(server.Close)
			dir := t.TempDir()
			db := newDriveTestDB(t, dir)
			seedDriveUser(t, db, userID)
			drive := NewDriveBackupService(db, driveTestConfig(dir), writeExport("zip")).
				WithEndpoints(DriveEndpoints{TokenURL: server.URL + "/token", UploadBase: server.URL + "/upload"})
			mustNoErr(t, drive.saveAccount(userID, driveAccount{RefreshToken: "rt", AccessToken: "at", Expiry: time.Now().Add(time.Hour)}))
			if err := drive.BackupNow(userID); err == nil {
				t.Fatal("malformed or failed resumable upload must fail the backup")
			}
		})
	}
}

func assertEncryptedUploadBody(t *testing.T, drive *DriveBackupService, metadata, body string) {
	t.Helper()
	if !strings.Contains(metadata, `"parents":["folder-9"]`) || !strings.Contains(metadata, "war-room-backup-v1-") || !strings.Contains(metadata, ".wrbackup") || strings.Contains(body, "zip-bytes") || !strings.HasPrefix(body, backupStreamMagic) {
		t.Fatalf("create upload metadata or encrypted media invalid: metadata=%s body=%q", metadata, body[:min(len(body), 64)])
	}
	if int64(len(body)) != EncryptedBackupSize(int64(len("zip-bytes"))) {
		t.Fatalf("uploaded %d bytes, want exactly %d", len(body), EncryptedBackupSize(int64(len("zip-bytes"))))
	}
	opened, err := drive.DecryptBackup([]byte(body))
	if err != nil || string(opened) != "zip-bytes" {
		t.Fatalf("uploaded backup does not decrypt to the export: %q, %v", opened, err)
	}
}

func TestDriveSetFolderMovesExistingFile(t *testing.T) {
	const userID = "user-1"
	server := fakeDriveServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			_, _ = io.WriteString(w, `{"access_token":"at","expires_in":3600}`)
		case "/files/file-1":
			if r.Method == http.MethodGet {
				_, _ = io.WriteString(w, `{"parents":["old-folder"]}`)
			} else {
				_, _ = io.WriteString(w, `{"id":"file-1"}`)
			}
		default:
			http.NotFound(w, r)
		}
	})
	dir := t.TempDir()
	db := newDriveTestDB(t, dir)
	seedDriveUser(t, db, userID)
	drive := NewDriveBackupService(db, driveTestConfig(dir), nil).WithEndpoints(DriveEndpoints{TokenURL: server.URL + "/token", DriveBase: server.URL})
	mustNoErr(t, drive.saveAccount(userID, driveAccount{RefreshToken: "rt", FolderID: "old-folder", FolderName: "Old", FileID: "file-1"}))
	mustNoErr(t, drive.SetFolder(userID, "new-folder", "New"))
	account, _, err := drive.loadAccount(userID)
	mustNoErr(t, err)
	if account.FileID != "file-1" || account.FolderID != "new-folder" || account.FolderName != "New" {
		t.Fatalf("moved account=%+v", account)
	}
}

func TestDriveSetFolderFallsBackWhenMoveFails(t *testing.T) {
	const userID = "user-1"
	server := fakeDriveServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			_, _ = io.WriteString(w, `{"access_token":"at","expires_in":3600}`)
			return
		}
		http.Error(w, "gone", http.StatusNotFound)
	})
	dir := t.TempDir()
	db := newDriveTestDB(t, dir)
	seedDriveUser(t, db, userID)
	drive := NewDriveBackupService(db, driveTestConfig(dir), nil).WithEndpoints(DriveEndpoints{TokenURL: server.URL + "/token", DriveBase: server.URL})
	mustNoErr(t, drive.saveAccount(userID, driveAccount{RefreshToken: "rt", FolderID: "old-folder", FileID: "file-1"}))
	mustNoErr(t, drive.SetFolder(userID, "new-folder", "New"))
	account, _, err := drive.loadAccount(userID)
	mustNoErr(t, err)
	if account.FileID != "file-1" || account.FolderID != "new-folder" {
		t.Fatalf("fallback account=%+v", account)
	}
}

func TestDriveSetFolderSameFolderKeepsFile(t *testing.T) {
	const userID = "user-1"
	dir := t.TempDir()
	db := newDriveTestDB(t, dir)
	seedDriveUser(t, db, userID)
	drive := NewDriveBackupService(db, driveTestConfig(dir), nil)
	mustNoErr(t, drive.saveAccount(userID, driveAccount{RefreshToken: "rt", FolderID: "same-folder", FolderName: "Old name", FileID: "file-1"}))
	mustNoErr(t, drive.SetFolder(userID, "same-folder", "New name"))
	account, _, err := drive.loadAccount(userID)
	mustNoErr(t, err)
	if account.FileID != "file-1" || account.FolderID != "same-folder" || account.FolderName != "New name" {
		t.Fatalf("same-folder account=%+v", account)
	}
}

func assertFolderResetAndRevoke(t *testing.T, drive *DriveBackupService, userID string) {
	t.Helper()
	drive.revokeURL = drive.driveBase + "/revoke"
	mustNoErr(t, drive.SetFolder(userID, "", ""))
	if got := drive.Status(userID).FolderID; got != "" {
		t.Fatalf("folder reset failed: %q", got)
	}
	mustNoErr(t, drive.Revoke(userID))
	if drive.Status(userID).Connected {
		t.Fatal("revoke must clear the saved account")
	}
}
