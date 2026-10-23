package service

import (
	"bytes"
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
	"war-room/backend/pkg/models"
)

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

func mustErr(t *testing.T, err error, message string) {
	t.Helper()
	if err == nil {
		t.Fatal(message)
	}
}

func waitForDrive(t *testing.T, drive *DriveBackupService) {
	t.Helper()
	for i := 0; i < 200; i++ {
		drive.mu.Lock()
		running := drive.running
		drive.mu.Unlock()
		if !running {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("drive backup did not finish")
}

var fakeDriveCreatedBody string
var fakeDriveCreatedMetadata string
var fakeDriveUpdated bool

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
			_, _ = io.WriteString(w, `{"email":"person@example.com"}`)
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

func TestDriveTriggerOnlyOnJobCreateAndStatusChange(t *testing.T) {
	fixture := newAttachmentOwnerFixture(t)
	calls := 0
	fixture.service.SetDriveTrigger(func() { calls++ })

	job, err := fixture.service.CreateJob(models.CreateJobInput{CompanyName: "DriveCo", PositionTitle: "Engineer"})
	mustNoErr(t, err)
	if calls != 1 {
		t.Fatalf("after CreateJob calls=%d, want 1", calls)
	}
	stage, err := fixture.service.CreateStage(models.CreateStageInput{JobID: job.ID, StageType: models.StageHR})
	mustNoErr(t, err)
	if calls != 2 {
		t.Fatalf("first stage changes job status and must trigger backup: calls=%d, want 2", calls)
	}
	if _, err := fixture.service.CreateQuestion(models.CreateQuestionInput{StageID: stage.ID, Question: "Why us?"}); err != nil {
		t.Fatal(err)
	}
	title := "Renamed role"
	if _, err := fixture.service.UpdateJob(job.ID, models.UpdateJobInput{PositionTitle: &title}); err != nil {
		t.Fatal(err)
	}
	status := models.StatusRejected
	if _, err := fixture.service.UpdateJob(job.ID, models.UpdateJobInput{Status: &status}); err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatalf("status change must trigger backup: calls=%d, want 3", calls)
	}
	if _, err := fixture.service.UpdateJob(job.ID, models.UpdateJobInput{Status: &status}); err != nil {
		t.Fatal(err)
	}
	mustNoErr(t, fixture.service.DeleteStage(stage.ID))
	if calls != 3 {
		t.Fatalf("non-status changes and unchanged status must not trigger backup: calls=%d, want 3", calls)
	}
}

func TestBoundedBackupBufferStopsAtUploadLimit(t *testing.T) {
	buffer := &boundedBackupBuffer{limit: 3}
	written, err := buffer.Write([]byte("four"))
	if err == nil || written != 3 || buffer.String() != "fou" {
		t.Fatalf("bounded write returned (%d, %v) with %q", written, err, buffer.String())
	}
	written, err = buffer.Write([]byte("more"))
	if err == nil || written != 0 || buffer.String() != "fou" {
		t.Fatalf("write after limit returned (%d, %v) with %q", written, err, buffer.String())
	}
}

func TestDriveTriggerRunsWhenStatusPersistsButTechnologyUpdateFails(t *testing.T) {
	fixture := newAttachmentOwnerFixture(t)
	if err := fixture.repo.CreateTechnology(models.Technology{ID: "drive-tech", Name: "Go"}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	fixture.service.SetDriveTrigger(func() { calls++ })
	job, err := fixture.service.CreateJob(models.CreateJobInput{CompanyName: "DriveCo", PositionTitle: "Engineer"})
	mustNoErr(t, err)
	if _, err := fixture.db.Exec(`CREATE TRIGGER reject_drive_technology BEFORE INSERT ON job_technologies BEGIN SELECT RAISE(ABORT, 'assignment denied'); END`); err != nil {
		t.Fatal(err)
	}
	status := models.StatusRejected
	_, err = fixture.service.UpdateJob(job.ID, models.UpdateJobInput{Status: &status, TechnologyIDs: []string{"drive-tech"}})
	if err == nil {
		t.Fatal("technology assignment should fail")
	}
	if calls != 2 {
		t.Fatalf("persisted status change triggers backup after later update failure: calls=%d, want 2", calls)
	}
	updated, err := fixture.repo.GetJobByID(job.ID)
	if err != nil || updated.Status != models.StatusRejected {
		t.Fatalf("persisted status=%q err=%v", updated.Status, err)
	}
}

func TestDriveOAuthFoldersAndTrigger(t *testing.T) {
	server := fakeDriveServer(t, fakeDriveAPI)
	dir := t.TempDir()
	cfg := &config.Config{DataDir: dir, GoogleClientID: "509809460865-client.apps.googleusercontent.com", GoogleClientSecret: "secret", GoogleBackupEncryptionKey: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=", GoogleRedirectURI: "http://localhost/cb", GoogleDrivePath: filepath.Join(dir, "g.json")}
	drive := NewDriveBackupService(cfg, func(w io.Writer) error { _, err := io.WriteString(w, "zip"); return err })
	drive.tokenURL, drive.userURL, drive.driveBase, drive.uploadBase = server.URL+"/token", server.URL+"/userinfo", server.URL, server.URL+"/upload"

	authURL, err := drive.AuthURL()
	if err != nil || !strings.Contains(authURL, "access_type=offline") {
		t.Fatalf("auth url: %v %s", err, authURL)
	}
	state := oauthState(t, authURL)
	mustNoErr(t, drive.HandleCallback("code", state))
	if status := drive.Status(); !status.Connected || status.Email != "person@example.com" {
		t.Fatalf("status after callback: %+v", status)
	}
	if token, err := drive.PickerAccessToken(); err != nil || token != "at" {
		t.Fatalf("picker access token=%q error=%v", token, err)
	}
	mustErr(t, drive.HandleCallback("code", state), "replayed OAuth state must be rejected")
	folders, err := drive.ListFolders("Back")
	if err != nil || len(folders) != 1 || folders[0].ID != "f1" {
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
	cfg := &config.Config{DataDir: t.TempDir(), GoogleClientID: "509809460865-client.apps.googleusercontent.com"}
	if got := NewDriveBackupService(cfg, nil).Status().PickerAppID; got != "509809460865" {
		t.Fatalf("status picker app ID=%q, want derived project number", got)
	}
}

func TestDriveOAuthStateCacheIsBounded(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{DataDir: dir, GoogleClientID: "id", GoogleClientSecret: "secret", GoogleBackupEncryptionKey: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=", GoogleDrivePath: filepath.Join(dir, "g.json")}
	drive := NewDriveBackupService(cfg, nil)
	for range maxGoogleOAuthStates {
		if _, err := drive.AuthURL(); err != nil {
			t.Fatalf("AuthURL at capacity boundary: %v", err)
		}
	}
	if _, err := drive.AuthURL(); err == nil {
		t.Fatal("AuthURL should reject more than the bounded number of active states")
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
	cfg := &config.Config{DataDir: dir, GoogleClientID: "id", GoogleClientSecret: "secret", GoogleBackupEncryptionKey: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=", GoogleDrivePath: filepath.Join(dir, "google-drive.json")}
	drive := NewDriveBackupService(cfg, nil)
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
	queries := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries <- r.URL.Query().Get("q")
		_, _ = io.WriteString(w, `{"files":[]}`)
	}))
	defer server.Close()

	dir := t.TempDir()
	cfg := &config.Config{DataDir: dir, GoogleClientID: "id", GoogleClientSecret: "secret", GoogleBackupEncryptionKey: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=", GoogleDrivePath: filepath.Join(dir, "g.json")}
	drive := NewDriveBackupService(cfg, nil).WithEndpoints(DriveEndpoints{DriveBase: server.URL})
	mustNoErr(t, drive.save(driveStore{RefreshToken: "rt", AccessToken: "at", Expiry: time.Now().Add(time.Hour)}))
	if _, err := drive.ListFolders("x\\'y"); err != nil {
		t.Fatal(err)
	}
	want := "mimeType='application/vnd.google-apps.folder' and trashed=false and name contains 'x" + strings.Repeat("\\", 3) + "'y'"
	if got := <-queries; got != want {
		t.Fatalf("folder query=%q, want escaped literal %q", got, want)
	}
}

func TestDriveBackupSerializesTokenAndFolderUpdates(t *testing.T) {
	tokenStarted := make(chan struct{})
	releaseToken := make(chan struct{})
	server := httptest.NewServer(blockingDriveTokenHandler(tokenStarted, releaseToken))
	defer server.Close()

	dir := t.TempDir()
	cfg := &config.Config{DataDir: dir, GoogleClientID: "id", GoogleClientSecret: "secret", GoogleBackupEncryptionKey: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=", GoogleDrivePath: filepath.Join(dir, "g.json")}
	drive := NewDriveBackupService(cfg, func(w io.Writer) error { _, err := io.WriteString(w, "zip"); return err }).
		WithEndpoints(DriveEndpoints{TokenURL: server.URL + "/token", DriveBase: server.URL, UploadBase: server.URL + "/upload"})
	mustNoErr(t, drive.save(driveStore{Email: "person@example.com", RefreshToken: "rt"}))

	backupDone := make(chan error, 1)
	go func() { backupDone <- drive.BackupNow() }()
	<-tokenStarted
	folderDone := make(chan error, 1)
	go func() { folderDone <- drive.SetFolder("folder-1", "Backups") }()
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
	assertSerializedDriveState(t, drive)
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

func assertSerializedDriveState(t *testing.T, drive *DriveBackupService) {
	t.Helper()
	store, err := drive.load()
	if err != nil {
		t.Fatal(err)
	}
	if store.RefreshToken != "rt" || store.AccessToken != "at" || store.FolderID != "folder-1" || store.FileID != "file-1" || store.LastStatus != "ok" || store.LastBackupAt == nil {
		t.Fatalf("concurrent updates lost state: %+v", store)
	}
}

func TestDriveTriggerAndUnconfigured(t *testing.T) {
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
	cfg := &config.Config{DataDir: dir, GoogleClientID: "id", GoogleClientSecret: "secret", GoogleBackupEncryptionKey: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=", GoogleDrivePath: filepath.Join(dir, "g.json")}
	drive := NewDriveBackupService(cfg, func(w io.Writer) error { _, err := io.WriteString(w, "zip"); return err })
	drive.tokenURL, drive.driveBase, drive.uploadBase = server.URL+"/token", server.URL, server.URL+"/upload"
	mustNoErr(t, drive.save(driveStore{Email: "a@example.com", RefreshToken: "rt"}))
	drive.Trigger()
	waitForDrive(t, drive)
	if drive.Status().LastStatus != "ok" {
		t.Fatalf("triggered backup did not complete: %+v", drive.Status())
	}
	unconfigured := NewDriveBackupService(&config.Config{DataDir: dir}, nil)
	if _, err := unconfigured.AuthURL(); err == nil {
		t.Fatal("unconfigured service must reject AuthURL")
	}
	if _, err := unconfigured.ListFolders(""); err == nil {
		t.Fatal("unconfigured service must reject ListFolders")
	}
}

func TestDriveStaleFileFallbackAndEmptyFolders(t *testing.T) {
	server := fakeDriveServer(t, fakeStaleDriveAPI)
	defer server.Close()
	dir := t.TempDir()
	cfg := &config.Config{DataDir: dir, GoogleClientID: "id", GoogleClientSecret: "secret", GoogleBackupEncryptionKey: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=", GoogleDrivePath: filepath.Join(dir, "g.json")}
	drive := NewDriveBackupService(cfg, func(w io.Writer) error { _, err := io.WriteString(w, "zip"); return err })
	drive.tokenURL, drive.driveBase, drive.uploadBase = server.URL+"/token", server.URL, server.URL+"/upload"
	mustNoErr(t, drive.save(driveStore{Email: "a@example.com", RefreshToken: "rt", FileID: "stale-1"}))
	folders, err := drive.ListFolders("")
	if err != nil || len(folders) != 0 {
		t.Fatalf("folders: %v %+v", err, folders)
	}
	mustNoErr(t, drive.BackupNow())
	if status := drive.Status(); status.LastStatus != "ok" {
		t.Fatalf("stale file must fall back to create: %+v", status)
	}
	store, err := drive.load()
	if err != nil || store.FileID != "fresh-1" {
		t.Fatalf("stored file id: %+v %v", store, err)
	}
	// A second Trigger while one is running must coalesce, not error or hang.
	drive.Trigger()
	drive.Trigger()
	waitForDrive(t, drive)
}

func TestDriveCallbackAndExportFailures(t *testing.T) {
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
	cfg := &config.Config{DataDir: dir, GoogleClientID: "id", GoogleClientSecret: "secret", GoogleBackupEncryptionKey: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=", GoogleDrivePath: filepath.Join(dir, "missing", "g.json")}
	drive := NewDriveBackupService(cfg, nil).WithEndpoints(DriveEndpoints{TokenURL: server.URL + "/token", UserURL: server.URL + "/userinfo"})
	authURL, err := drive.AuthURL()
	if err != nil {
		t.Fatal(err)
	}
	if err := drive.HandleCallback("code", oauthState(t, authURL)); err == nil {
		t.Fatal("userinfo failure must fail the callback")
	}
	if err := drive.BackupNow(); err == nil {
		t.Fatal("backup with no exporter and no connection must fail")
	}
	connected := NewDriveBackupService(&config.Config{DataDir: dir, GoogleClientID: "id", GoogleClientSecret: "secret", GoogleBackupEncryptionKey: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=", GoogleDrivePath: filepath.Join(dir, "g.json")}, nil)
	connected.revokeURL = server.URL + "/token"
	mustNoErr(t, connected.save(driveStore{Email: "a@example.com", RefreshToken: "rt"}))
	if err := connected.BackupNow(); err == nil {
		t.Fatal("backup without exporter must fail")
	}
	mustNoErr(t, connected.Disconnect())
	// Token response without a refresh token must be rejected on reconnect.
	authURL, err = drive.AuthURL()
	if err != nil {
		t.Fatal(err)
	}
	drive.userURL = server.URL + "/token"
	if err := drive.HandleCallback("code", oauthState(t, authURL)); err == nil {
		t.Fatal("missing refresh token must fail the callback")
	}
}

func TestDriveDisconnectRevokesGoogleTokenAndClearsLocalState(t *testing.T) {
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
	storePath := filepath.Join(dir, "google-drive.json")
	drive := NewDriveBackupService(&config.Config{DataDir: dir, GoogleClientID: "id", GoogleClientSecret: "secret", GoogleBackupEncryptionKey: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=", GoogleDrivePath: storePath}, nil)
	drive.revokeURL = server.URL + "/revoke"
	mustNoErr(t, drive.save(driveStore{Email: "person@example.com", RefreshToken: "refresh-token"}))
	mustNoErr(t, drive.Disconnect())
	if revoked != "refresh-token" {
		t.Fatalf("revoked token=%q", revoked)
	}
	if drive.Status().Connected {
		t.Fatal("disconnect must clear local account state")
	}
	if _, err := os.Stat(storePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("local token store still exists: %v", err)
	}
}

func TestDriveDisconnectClearsLocalStateWhenRevocationFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "revocation unavailable", http.StatusBadGateway)
	}))
	defer server.Close()
	dir := t.TempDir()
	storePath := filepath.Join(dir, "google-drive.json")
	drive := NewDriveBackupService(&config.Config{DataDir: dir, GoogleClientID: "id", GoogleClientSecret: "secret", GoogleBackupEncryptionKey: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=", GoogleDrivePath: storePath}, nil)
	drive.revokeURL = server.URL
	mustNoErr(t, drive.save(driveStore{Email: "person@example.com", RefreshToken: "refresh-token"}))
	if err := drive.Disconnect(); !errors.Is(err, ErrGoogleRevocationFailed) {
		t.Fatalf("disconnect error=%v", err)
	}
	if drive.Status().Connected {
		t.Fatal("failed Google revocation must still clear the local token")
	}
}

func TestDriveDisconnectClearsLocalStateWhenRevocationRequestCannotStart(t *testing.T) {
	for name, endpoint := range map[string]string{"invalid URL": "://invalid", "network failure": "http://127.0.0.1:1/revoke"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			drive := NewDriveBackupService(&config.Config{DataDir: dir, GoogleClientID: "id", GoogleClientSecret: "secret", GoogleBackupEncryptionKey: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=", GoogleDrivePath: filepath.Join(dir, "google-drive.json")}, nil)
			drive.revokeURL = endpoint
			mustNoErr(t, drive.save(driveStore{Email: "person@example.com", RefreshToken: "refresh-token"}))
			if err := drive.Disconnect(); !errors.Is(err, ErrGoogleRevocationFailed) {
				t.Fatalf("disconnect error=%v", err)
			}
			if drive.Status().Connected {
				t.Fatal("local account must be cleared when revocation cannot start")
			}
		})
	}
}

func TestDriveStoreAndNetworkErrors(t *testing.T) {
	dir := t.TempDir()
	blocked := filepath.Join(dir, "blocked")
	mustNoErr(t, os.WriteFile(blocked, []byte("x"), 0600))
	cfg := &config.Config{DataDir: dir, GoogleClientID: "id", GoogleClientSecret: "secret", GoogleBackupEncryptionKey: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=", GoogleDrivePath: filepath.Join(blocked, "g.json")}
	drive := NewDriveBackupService(cfg, nil)
	if err := drive.save(driveStore{Email: "a@example.com"}); err == nil {
		t.Fatal("save beneath a file must fail")
	}
	occupiedDirectory := filepath.Join(dir, "occupied")
	mustNoErr(t, os.Mkdir(occupiedDirectory, 0700))
	if err := NewDriveBackupService(&config.Config{GoogleBackupEncryptionKey: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=", GoogleDrivePath: occupiedDirectory}, nil).save(driveStore{Email: "a@example.com"}); err == nil {
		t.Fatal("atomic save must fail when its destination is a directory")
	}
	dirStore := NewDriveBackupService(&config.Config{DataDir: dir, GoogleClientID: "id", GoogleClientSecret: "secret", GoogleBackupEncryptionKey: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=", GoogleDrivePath: dir}, nil)
	if dirStore.Status().Connected {
		t.Fatal("directory store path must report disconnected")
	}
	if _, err := dirStore.ListFolders(""); err == nil {
		t.Fatal("directory store path must fail ListFolders")
	}
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	deadURL := dead.URL
	dead.Close()
	cfg2 := &config.Config{DataDir: dir, GoogleClientID: "id", GoogleClientSecret: "secret", GoogleBackupEncryptionKey: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=", GoogleDrivePath: filepath.Join(dir, "g2.json")}
	network := NewDriveBackupService(cfg2, func(w io.Writer) error { _, err := io.WriteString(w, "zip"); return err }).WithEndpoints(DriveEndpoints{TokenURL: deadURL + "/token", DriveBase: deadURL, UploadBase: deadURL + "/upload"})
	mustNoErr(t, network.save(driveStore{Email: "a@example.com", RefreshToken: "rt"}))
	if _, err := network.ListFolders(""); err == nil {
		t.Fatal("dead Google endpoints must fail ListFolders")
	}
	if err := network.BackupNow(); err == nil {
		t.Fatal("dead Google endpoints must fail BackupNow")
	}
}

func TestDriveMalformedGoogleResponses(t *testing.T) {
	mode := "bad-files"
	server := httptest.NewServer(malformedGoogleResponseAPI(&mode))
	defer server.Close()
	dir := t.TempDir()
	cfg := &config.Config{DataDir: dir, GoogleClientID: "id", GoogleClientSecret: "secret", GoogleBackupEncryptionKey: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="}
	drive := NewDriveBackupService(cfg, func(w io.Writer) error { _, err := io.WriteString(w, "zip"); return err }).
		WithEndpoints(DriveEndpoints{TokenURL: server.URL + "/token", DriveBase: server.URL, UploadBase: server.URL + "/upload"})
	if drive.storePath != filepath.Join(dir, "google-drive.json") {
		t.Fatalf("default store path: %s", drive.storePath)
	}
	mustNoErr(t, drive.save(driveStore{Email: "a@example.com", RefreshToken: "rt"}))
	if _, err := drive.ListFolders(""); err == nil {
		t.Fatal("malformed folder response must fail")
	}
	mode = "error-files"
	if _, err := drive.ListFolders(""); err == nil {
		t.Fatal("folder API error must fail")
	}
	if err := drive.BackupNow(); err == nil {
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
	dir := t.TempDir()
	storePath := filepath.Join(dir, "g.json")
	cfg := &config.Config{DataDir: dir, GoogleClientID: "id", GoogleClientSecret: "secret", GoogleBackupEncryptionKey: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=", GoogleDrivePath: storePath}
	drive := NewDriveBackupService(cfg, func(w io.Writer) error { return io.ErrClosedPipe })
	if err := drive.BackupNow(); err == nil {
		t.Fatal("backup without connection must fail")
	}
	if err := drive.SetFolder("f", "F"); err == nil {
		t.Fatal("SetFolder without connection must fail")
	}
	mustNoErr(t, drive.Disconnect())
	mustNoErr(t, os.WriteFile(storePath, []byte("{"), 0600))
	if status := drive.Status(); status.Connected {
		t.Fatal("corrupt store must report disconnected")
	}
	if _, err := drive.ListFolders(""); err == nil {
		t.Fatal("corrupt store must fail ListFolders")
	}
}

func TestDriveStoreEncryptsTokensAndMigratesPlaintext(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "google-drive.json")
	cfg := &config.Config{DataDir: dir, GoogleClientID: "id", GoogleClientSecret: "secret", GoogleBackupEncryptionKey: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=", GoogleDrivePath: path}
	drive := NewDriveBackupService(cfg, nil)
	mustNoErr(t, drive.save(driveStore{Email: "person@example.com", RefreshToken: "private-refresh-token"}))
	stored, err := os.ReadFile(path) // #nosec G304 -- test path is inside t.TempDir.
	mustNoErr(t, err)
	if strings.Contains(string(stored), "private-refresh-token") || !strings.HasPrefix(string(stored), storeEnvelopeMagic) {
		t.Fatal("Drive token store must be encrypted at rest")
	}
	info, err := os.Stat(path)
	mustNoErr(t, err)
	if info.Mode().Perm() != 0600 {
		t.Fatalf("token store permissions=%o", info.Mode().Perm())
	}
	loaded, err := drive.load()
	mustNoErr(t, err)
	if loaded.RefreshToken != "private-refresh-token" {
		t.Fatal("encrypted token store did not load")
	}
	legacy := []byte(`{"email":"legacy@example.com","refresh_token":"legacy-refresh-token"}`)
	mustNoErr(t, os.WriteFile(path, legacy, 0600))
	loaded, err = drive.load()
	mustNoErr(t, err)
	if loaded.RefreshToken != "legacy-refresh-token" {
		t.Fatal("legacy token store did not load before migration")
	}
	stored, err = os.ReadFile(path) // #nosec G304 -- test path is inside t.TempDir.
	mustNoErr(t, err)
	if !strings.HasPrefix(string(stored), storeEnvelopeMagic) {
		t.Fatal("legacy token store was not migrated to encrypted storage")
	}
}

func TestDriveStoreRejectsTamperedEncryptedTokenFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "google-drive.json")
	cfg := &config.Config{DataDir: dir, GoogleClientID: "id", GoogleClientSecret: "secret", GoogleBackupEncryptionKey: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=", GoogleDrivePath: path}
	drive := NewDriveBackupService(cfg, nil)
	mustNoErr(t, drive.save(driveStore{RefreshToken: "private"}))
	data, err := os.ReadFile(path) // #nosec G304 -- test path is inside t.TempDir.
	mustNoErr(t, err)
	data[len(data)-1] ^= 1
	mustNoErr(t, os.WriteFile(path, data, 0600)) // #nosec G703 -- test path is inside t.TempDir.
	if _, err := drive.load(); err == nil {
		t.Fatal("tampered OAuth token store must not load")
	}
}

func TestDriveBackupKeepsOldCopyUntilReplacementAndRetriesCleanup(t *testing.T) {
	deleteFails := true
	uploads := 0
	var requests []string
	server := httptest.NewServer(fakeDriveReplacementAPI(&deleteFails, &uploads, &requests))
	defer server.Close()
	dir := t.TempDir()
	cfg := &config.Config{DataDir: dir, GoogleClientID: "id", GoogleClientSecret: "secret", GoogleBackupEncryptionKey: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=", GoogleDrivePath: filepath.Join(dir, "store")}
	drive := NewDriveBackupService(cfg, func(w io.Writer) error { _, err := io.WriteString(w, "zip"); return err }).
		WithEndpoints(DriveEndpoints{TokenURL: server.URL + "/token", DriveBase: server.URL, UploadBase: server.URL + "/upload"})
	mustNoErr(t, drive.save(driveStore{RefreshToken: "rt", FileID: "old-file"}))
	mustNoErr(t, drive.BackupNow())
	store, err := drive.load()
	mustNoErr(t, err)
	assertPendingDriveCleanup(t, store, requests)
	deleteFails = false
	mustNoErr(t, drive.BackupNow())
	store, err = drive.load()
	mustNoErr(t, err)
	assertFinishedDriveCleanup(t, store)
}

func TestDriveBackupRemovesNewUploadWhenLocalStateCannotCommit(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "store")
	var requests []string
	server := httptest.NewServer(fakeDriveStoreFailureAPI(storePath, &requests))
	defer server.Close()
	cfg := &config.Config{DataDir: dir, GoogleClientID: "id", GoogleClientSecret: "secret", GoogleBackupEncryptionKey: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=", GoogleDrivePath: storePath}
	drive := NewDriveBackupService(cfg, func(w io.Writer) error { _, err := io.WriteString(w, "zip"); return err }).
		WithEndpoints(DriveEndpoints{TokenURL: server.URL + "/token", DriveBase: server.URL, UploadBase: server.URL + "/upload"})
	mustNoErr(t, drive.save(driveStore{RefreshToken: "rt", AccessToken: "at", Expiry: time.Now().Add(time.Hour), FileID: "old-file"}))
	if err := drive.BackupNow(); err == nil {
		t.Fatal("backup must report a failed local state commit")
	}
	if len(requests) != 3 || requests[0] != "POST /upload/files" || requests[1] != "PUT /upload/session" || requests[2] != "DELETE /files/new-file" {
		t.Fatalf("new upload cleanup order=%v", requests)
	}
}

func fakeDriveStoreFailureAPI(storePath string, requests *[]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		*requests = append(*requests, r.Method+" "+r.URL.Path)
		switch {
		case r.URL.Path == "/upload/files" && r.Method == http.MethodPost:
			w.Header().Set("Location", "http://"+r.Host+"/upload/session")
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/upload/session" && r.Method == http.MethodPut:
			respondToFailedStoreUpload(w, storePath)
		case r.URL.Path == "/files/new-file" && r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/token":
			_, _ = io.WriteString(w, `{"access_token":"at","expires_in":3600}`)
		default:
			http.NotFound(w, r)
		}
	}
}

func respondToFailedStoreUpload(w http.ResponseWriter, storePath string) {
	if err := os.Remove(storePath); err != nil {
		http.Error(w, "could not replace test store", http.StatusInternalServerError)
		return
	}
	if err := os.Mkdir(storePath, 0700); err != nil {
		http.Error(w, "could not replace test store", http.StatusInternalServerError)
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

func assertPendingDriveCleanup(t *testing.T, store driveStore, requests []string) {
	t.Helper()
	if store.FileID != "new-1" || store.LastStatus != "cleanup_pending" || len(store.PreviousFileIDs) != 1 || len(requests) < 4 || requests[len(requests)-3] != "POST /upload/files" || requests[len(requests)-2] != "PUT /upload/session" || requests[len(requests)-1] != "DELETE /files/old-file" {
		t.Fatalf("replacement and pending cleanup state=%+v requests=%v", store, requests)
	}
}

func assertFinishedDriveCleanup(t *testing.T, store driveStore) {
	t.Helper()
	if store.LastStatus != "ok" || len(store.PreviousFileIDs) != 0 {
		t.Fatalf("cleanup retry state=%+v", store)
	}
}

func TestDriveFailureIsRecorded(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{DataDir: dir, GoogleClientID: "id", GoogleClientSecret: "secret", GoogleBackupEncryptionKey: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=", GoogleDrivePath: filepath.Join(dir, "g.json")}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusBadGateway)
	}))
	defer server.Close()
	drive2 := NewDriveBackupService(cfg, func(w io.Writer) error { _, err := io.WriteString(w, "zip"); return err })
	drive2.tokenURL, drive2.uploadBase = server.URL+"/token", server.URL+"/upload"
	mustNoErr(t, drive2.save(driveStore{Email: "a@example.com", RefreshToken: "rt"}))
	if err := drive2.BackupNow(); err == nil {
		t.Fatal("failing Google endpoints must fail the backup")
	}
	if status := drive2.Status(); status.LastStatus != "error" || status.LastError == "" {
		t.Fatalf("failure must be recorded: %+v", status)
	}
}

func TestDriveBackupNowUploadsToChosenFolder(t *testing.T) {
	fakeDriveCreatedBody, fakeDriveCreatedMetadata, fakeDriveUpdated = "", "", false
	server := fakeDriveServer(t, fakeDriveAPI)

	dir := t.TempDir()
	cfg := &config.Config{DataDir: dir, GoogleClientID: "id", GoogleClientSecret: "secret", GoogleBackupEncryptionKey: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=", GoogleDrivePath: filepath.Join(dir, "google-drive.json")}
	drive := NewDriveBackupService(cfg, func(w io.Writer) error { _, err := io.WriteString(w, "zip-bytes"); return err })
	drive.tokenURL, drive.driveBase, drive.uploadBase = server.URL+"/token", server.URL, server.URL+"/upload"
	mustNoErr(t, drive.save(driveStore{Email: "a@example.com", RefreshToken: "rt", FolderID: "folder-9", FolderName: "Backups"}))
	mustNoErr(t, drive.BackupNow())
	assertEncryptedUploadBody(t, fakeDriveCreatedMetadata, fakeDriveCreatedBody)
	status := drive.Status()
	if !status.Connected || status.Email != "a@example.com" || status.LastStatus != "ok" || status.FolderID != "folder-9" {
		t.Fatalf("status=%+v", status)
	}
	mustNoErr(t, drive.BackupNow())
	store, err := drive.load()
	mustNoErr(t, err)
	if store.BackupVersion != 2 || store.FileID != "file-1" {
		t.Fatalf("second backup should create version 2: %+v", store)
	}
	assertFolderResetAndDisconnect(t, drive)
}

func TestDriveResumableUploadHandlesArchivesOverFiveMiB(t *testing.T) {
	const archiveSize = (5 << 20) + 1
	archive := bytes.Repeat([]byte("x"), archiveSize)
	var uploadedBytes int
	server := httptest.NewServer(largeResumableUploadAPI(t, &uploadedBytes))
	defer server.Close()
	dir := t.TempDir()
	cfg := &config.Config{DataDir: dir, GoogleClientID: "id", GoogleClientSecret: "secret", GoogleBackupEncryptionKey: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=", GoogleDrivePath: filepath.Join(dir, "store")}
	drive := NewDriveBackupService(cfg, func(w io.Writer) error { _, err := w.Write(archive); return err }).
		WithEndpoints(DriveEndpoints{TokenURL: server.URL + "/token", UploadBase: server.URL + "/upload"})
	mustNoErr(t, drive.save(driveStore{RefreshToken: "rt", AccessToken: "at", Expiry: time.Now().Add(time.Hour)}))
	if err := drive.BackupNow(); err != nil {
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
	cfg := &config.Config{DataDir: dir, GoogleClientID: "id", GoogleClientSecret: "secret", GoogleBackupEncryptionKey: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=", GoogleDrivePath: filepath.Join(dir, "store")}
	drive := NewDriveBackupService(cfg, func(w io.Writer) error { _, err := io.WriteString(w, "zip"); return err }).
		WithEndpoints(DriveEndpoints{TokenURL: primary.URL + "/token", UploadBase: primary.URL + "/upload"})
	mustNoErr(t, drive.save(driveStore{RefreshToken: "rt", AccessToken: "at", Expiry: time.Now().Add(time.Hour)}))
	if err := drive.BackupNow(); err == nil || !strings.Contains(err.Error(), "invalid resumable upload URL") {
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
			cfg := &config.Config{DataDir: dir, GoogleClientID: "id", GoogleClientSecret: "secret", GoogleBackupEncryptionKey: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=", GoogleDrivePath: filepath.Join(dir, "store")}
			drive := NewDriveBackupService(cfg, func(w io.Writer) error { _, err := io.WriteString(w, "zip"); return err }).
				WithEndpoints(DriveEndpoints{TokenURL: server.URL + "/token", UploadBase: server.URL + "/upload"})
			mustNoErr(t, drive.save(driveStore{RefreshToken: "rt", AccessToken: "at", Expiry: time.Now().Add(time.Hour)}))
			if err := drive.BackupNow(); err == nil {
				t.Fatal("malformed or failed resumable upload must fail the backup")
			}
		})
	}
}

func assertEncryptedUploadBody(t *testing.T, metadata, body string) {
	t.Helper()
	if !strings.Contains(metadata, `"parents":["folder-9"]`) || !strings.Contains(metadata, "war-room-backup-v1-") || !strings.Contains(metadata, ".wrbackup") || strings.Contains(body, "zip-bytes") || !strings.HasPrefix(body, backupEnvelopeMagic) {
		t.Fatalf("create upload metadata or encrypted media invalid: metadata=%s body=%q", metadata, body[:min(len(body), 64)])
	}
}

func TestDriveSetFolderMovesExistingFile(t *testing.T) {
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
	cfg := &config.Config{DataDir: dir, GoogleClientID: "id", GoogleClientSecret: "secret", GoogleBackupEncryptionKey: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=", GoogleDrivePath: filepath.Join(dir, "g.json")}
	drive := NewDriveBackupService(cfg, nil).WithEndpoints(DriveEndpoints{TokenURL: server.URL + "/token", DriveBase: server.URL})
	mustNoErr(t, drive.save(driveStore{Email: "a@example.com", RefreshToken: "rt", FolderID: "old-folder", FolderName: "Old", FileID: "file-1"}))
	mustNoErr(t, drive.SetFolder("new-folder", "New"))
	store, err := drive.load()
	mustNoErr(t, err)
	if store.FileID != "file-1" || store.FolderID != "new-folder" || store.FolderName != "New" {
		t.Fatalf("moved store=%+v", store)
	}
}

func TestDriveSetFolderFallsBackWhenMoveFails(t *testing.T) {
	server := fakeDriveServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			_, _ = io.WriteString(w, `{"access_token":"at","expires_in":3600}`)
			return
		}
		http.Error(w, "gone", http.StatusNotFound)
	})
	dir := t.TempDir()
	cfg := &config.Config{DataDir: dir, GoogleClientID: "id", GoogleClientSecret: "secret", GoogleBackupEncryptionKey: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=", GoogleDrivePath: filepath.Join(dir, "g.json")}
	drive := NewDriveBackupService(cfg, nil).WithEndpoints(DriveEndpoints{TokenURL: server.URL + "/token", DriveBase: server.URL})
	mustNoErr(t, drive.save(driveStore{Email: "a@example.com", RefreshToken: "rt", FolderID: "old-folder", FileID: "file-1"}))
	mustNoErr(t, drive.SetFolder("new-folder", "New"))
	store, err := drive.load()
	mustNoErr(t, err)
	if store.FileID != "file-1" || store.FolderID != "new-folder" {
		t.Fatalf("fallback store=%+v", store)
	}
}

func TestDriveSetFolderSameFolderKeepsFile(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{DataDir: dir, GoogleClientID: "id", GoogleClientSecret: "secret", GoogleBackupEncryptionKey: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=", GoogleDrivePath: filepath.Join(dir, "g.json")}
	drive := NewDriveBackupService(cfg, nil)
	mustNoErr(t, drive.save(driveStore{Email: "a@example.com", RefreshToken: "rt", FolderID: "same-folder", FolderName: "Old name", FileID: "file-1"}))
	mustNoErr(t, drive.SetFolder("same-folder", "New name"))
	store, err := drive.load()
	mustNoErr(t, err)
	if store.FileID != "file-1" || store.FolderID != "same-folder" || store.FolderName != "New name" {
		t.Fatalf("same-folder store=%+v", store)
	}
}

func assertFolderResetAndDisconnect(t *testing.T, drive *DriveBackupService) {
	t.Helper()
	drive.revokeURL = drive.driveBase + "/revoke"
	mustNoErr(t, drive.SetFolder("", ""))
	if got := drive.Status().FolderID; got != "" {
		t.Fatalf("folder reset failed: %q", got)
	}
	mustNoErr(t, drive.Disconnect())
	if drive.Status().Connected {
		t.Fatal("disconnect must clear the saved account")
	}
}
