package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"war-room/backend/pkg/config"
)

// ErrGoogleNotConfigured indicates the OAuth client env vars are missing.
var ErrGoogleNotConfigured = errors.New("google drive is not configured: set GOOGLE_CLIENT_ID, GOOGLE_CLIENT_SECRET, and GOOGLE_BACKUP_ENCRYPTION_KEY")

// ErrGoogleNotConnected indicates no Google account is connected.
var ErrGoogleNotConnected = errors.New("no google account connected")

// ErrGoogleRevocationFailed indicates that local disconnect succeeded but Google revocation failed.
var ErrGoogleRevocationFailed = errors.New("google access may still be authorized; revoke War Room access in your Google Account settings")

const driveBackupFilename = "war-room-backup-v%d-%s.wrbackup"
const maxGoogleOAuthStates = 128

// DriveStatus is the public, token-free view of the Drive connection.
type DriveStatus struct {
	Configured   bool       `json:"configured"`
	PickerAPIKey string     `json:"picker_api_key,omitempty"`
	PickerAppID  string     `json:"picker_app_id,omitempty"`
	Connected    bool       `json:"connected"`
	Email        string     `json:"email,omitempty"`
	FolderID     string     `json:"folder_id,omitempty"`
	FolderName   string     `json:"folder_name,omitempty"`
	LastBackupAt *time.Time `json:"last_backup_at,omitempty"`
	LastStatus   string     `json:"last_status,omitempty"`
	LastError    string     `json:"last_error,omitempty"`
}

// DriveFolder is one selectable Drive folder.
type DriveFolder struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type driveStore struct {
	Email           string     `json:"email,omitempty"`
	RefreshToken    string     `json:"refresh_token,omitempty"`
	AccessToken     string     `json:"access_token,omitempty"`
	Expiry          time.Time  `json:"expiry,omitempty"`
	FolderID        string     `json:"folder_id,omitempty"`
	FolderName      string     `json:"folder_name,omitempty"`
	FileID          string     `json:"file_id,omitempty"`
	BackupVersion   int        `json:"backup_version,omitempty"`
	PreviousFileIDs []string   `json:"previous_file_ids,omitempty"`
	LastBackupAt    *time.Time `json:"last_backup_at,omitempty"`
	LastStatus      string     `json:"last_status,omitempty"`
	LastError       string     `json:"last_error,omitempty"`
}

// DriveBackupService uploads encrypted backups to one connected Google Drive account.
type DriveBackupService struct {
	cfg          *config.Config
	export       func(io.Writer) error
	client       *http.Client
	uploadClient *http.Client
	authBase     string
	tokenURL     string
	userURL      string
	revokeURL    string
	driveBase    string
	uploadBase   string
	storePath    string

	mu        sync.Mutex
	accountMu sync.Mutex
	running   bool
	pending   bool
	states    map[string]time.Time
}

// NewDriveBackupService creates the service. Export supplies the ZIP bytes.
func NewDriveBackupService(cfg *config.Config, export func(io.Writer) error) *DriveBackupService {
	path := cfg.GoogleDrivePath
	if path == "" {
		path = filepath.Join(cfg.DataDir, "google-drive.json")
	}
	// #nosec G101 -- endpoint URLs only; no credential values are hardcoded.
	return &DriveBackupService{
		cfg: cfg, export: export, storePath: path,
		client: &http.Client{Timeout: 60 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}},
		uploadClient: &http.Client{Timeout: 10 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}},
		authBase:   "https://accounts.google.com/o/oauth2/v2/auth",
		tokenURL:   "https://oauth2.googleapis.com/token",
		userURL:    "https://openidconnect.googleapis.com/v1/userinfo",
		revokeURL:  "https://oauth2.googleapis.com/revoke",
		driveBase:  "https://www.googleapis.com/drive/v3",
		uploadBase: "https://www.googleapis.com/upload/drive/v3",
		states:     map[string]time.Time{},
	}
}

// DriveEndpoints overrides Google API base URLs (used by tests with fake servers).
type DriveEndpoints struct {
	AuthBase   string
	TokenURL   string
	UserURL    string
	RevokeURL  string
	DriveBase  string
	UploadBase string
}

// WithEndpoints applies non-empty endpoint overrides and returns the service.
func (d *DriveBackupService) WithEndpoints(endpoints DriveEndpoints) *DriveBackupService {
	if endpoints.AuthBase != "" {
		d.authBase = endpoints.AuthBase
	}
	if endpoints.TokenURL != "" {
		d.tokenURL = endpoints.TokenURL
	}
	if endpoints.UserURL != "" {
		d.userURL = endpoints.UserURL
	}
	if endpoints.RevokeURL != "" {
		d.revokeURL = endpoints.RevokeURL
	}
	if endpoints.DriveBase != "" {
		d.driveBase = endpoints.DriveBase
	}
	if endpoints.UploadBase != "" {
		d.uploadBase = endpoints.UploadBase
	}
	return d
}

// Configured reports whether OAuth credentials and the local encryption key are present.
func (d *DriveBackupService) Configured() bool {
	_, keyErr := d.encryptionKey()
	return d.cfg.GoogleClientID != "" && d.cfg.GoogleClientSecret != "" && keyErr == nil
}

func (d *DriveBackupService) load() (driveStore, error) {
	data, err := os.ReadFile(d.storePath) // #nosec G304 -- internal path under configured data dir.
	if errors.Is(err, os.ErrNotExist) {
		return driveStore{}, nil
	}
	if err != nil {
		return driveStore{}, err
	}
	encrypted := strings.HasPrefix(string(data), storeEnvelopeMagic)
	if encrypted {
		key, keyErr := d.encryptionKey()
		if keyErr != nil {
			return driveStore{}, keyErr
		}
		data, err = openDriveData(key, storeEnvelopeMagic, "google-drive-store-v1", data)
		if err != nil {
			return driveStore{}, err
		}
	}
	var store driveStore
	if err := json.Unmarshal(data, &store); err != nil {
		return driveStore{}, fmt.Errorf("read google drive store: %w", err)
	}
	if !encrypted && d.Configured() {
		if err := d.save(store); err != nil {
			return driveStore{}, fmt.Errorf("encrypt google drive store: %w", err)
		}
	}
	return store, nil
}

func (d *DriveBackupService) save(store driveStore) error {
	data, err := json.Marshal(store) // #nosec G117 -- token persists only to the 0600 store file in the private data volume.
	if err != nil {
		return err
	}
	key, err := d.encryptionKey()
	if err != nil {
		return err
	}
	data, err = sealDriveData(key, storeEnvelopeMagic, "google-drive-store-v1", data)
	if err != nil {
		return err
	}
	return writePrivateAtomic(d.storePath, data)
}

func writePrivateAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	tmpFile, err := os.CreateTemp(filepath.Dir(path), ".google-drive-*.tmp")
	if err != nil {
		return err
	}
	tmp := tmpFile.Name()
	if err := tmpFile.Chmod(0600); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmp)
		return err
	}
	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// Status returns the public connection state without any tokens.
func (d *DriveBackupService) Status() DriveStatus {
	d.accountMu.Lock()
	defer d.accountMu.Unlock()
	store, err := d.load()
	connected := err == nil && store.RefreshToken != ""
	status := DriveStatus{Configured: d.Configured(), PickerAPIKey: d.cfg.GooglePickerAPIKey, PickerAppID: googleProjectNumber(d.cfg.GoogleClientID, d.cfg.GoogleCloudProjectNumber), Connected: connected}
	if connected {
		status.Email, status.FolderID, status.FolderName = store.Email, store.FolderID, store.FolderName
		status.LastBackupAt, status.LastStatus, status.LastError = store.LastBackupAt, store.LastStatus, store.LastError
	}
	return status
}

func googleProjectNumber(clientID, configuredNumber string) string {
	if configuredNumber != "" {
		return configuredNumber
	}
	projectNumber, _, ok := strings.Cut(clientID, "-")
	if !ok || projectNumber == "" {
		return ""
	}
	for _, digit := range projectNumber {
		if digit < '0' || digit > '9' {
			return ""
		}
	}
	return projectNumber
}

// AuthURL starts a single-account OAuth flow with a short-lived state.
func (d *DriveBackupService) AuthURL() (string, error) {
	if !d.Configured() {
		return "", ErrGoogleNotConfigured
	}
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	state := hex.EncodeToString(raw)
	d.mu.Lock()
	now := time.Now()
	for existing, expiry := range d.states {
		if !now.Before(expiry) {
			delete(d.states, existing)
		}
	}
	if len(d.states) >= maxGoogleOAuthStates {
		d.mu.Unlock()
		return "", errors.New("too many active google sign-in attempts; try again shortly")
	}
	d.states[state] = now.Add(10 * time.Minute)
	d.mu.Unlock()
	q := url.Values{
		"client_id": {d.cfg.GoogleClientID}, "redirect_uri": {d.cfg.GoogleRedirectURI},
		"response_type": {"code"}, "state": {state}, "access_type": {"offline"}, "prompt": {"consent"},
		"scope": {"openid email https://www.googleapis.com/auth/drive.file"},
	}
	return d.authBase + "?" + q.Encode(), nil
}

func (d *DriveBackupService) validState(state string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	expiry, ok := d.states[state]
	delete(d.states, state)
	return ok && time.Now().Before(expiry)
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

// HandleCallback exchanges the OAuth code and stores the single account.
func (d *DriveBackupService) HandleCallback(code, state string) error {
	d.accountMu.Lock()
	defer d.accountMu.Unlock()
	if !d.validState(state) || code == "" {
		return errors.New("invalid or expired google sign-in state")
	}
	token, err := d.exchange(url.Values{
		"code": {code}, "client_id": {d.cfg.GoogleClientID}, "client_secret": {d.cfg.GoogleClientSecret},
		"redirect_uri": {d.cfg.GoogleRedirectURI}, "grant_type": {"authorization_code"},
	})
	if err != nil {
		return err
	}
	email, err := d.fetchEmail(token.AccessToken)
	if err != nil {
		return err
	}
	refresh := token.RefreshToken
	if refresh == "" {
		return errors.New("google did not return a refresh token; revoke access and connect again")
	}
	return d.save(driveStore{Email: email, RefreshToken: refresh, AccessToken: token.AccessToken,
		Expiry: time.Now().Add(time.Duration(token.ExpiresIn) * time.Second), LastStatus: "connected"})
}

func (d *DriveBackupService) exchange(form url.Values) (tokenResponse, error) {
	req, err := http.NewRequest(http.MethodPost, d.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return tokenResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return d.doToken(req)
}

func (d *DriveBackupService) doToken(req *http.Request) (tokenResponse, error) {
	res, err := d.client.Do(req)
	if err != nil {
		return tokenResponse{}, err
	}
	defer func() { _ = res.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode != http.StatusOK {
		return tokenResponse{}, fmt.Errorf("google token request failed (%d)", res.StatusCode)
	}
	var token tokenResponse
	return token, json.Unmarshal(body, &token)
}

func (d *DriveBackupService) fetchEmail(accessToken string) (string, error) {
	req, _ := http.NewRequest(http.MethodGet, d.userURL, nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	res, err := d.client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("google userinfo request failed (%d)", res.StatusCode)
	}
	var info struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&info); err != nil {
		return "", err
	}
	if strings.TrimSpace(info.Email) == "" {
		return "", errors.New("google userinfo response did not include an email")
	}
	return info.Email, nil
}

func (d *DriveBackupService) accessToken(store *driveStore) (string, error) {
	if store.AccessToken != "" && time.Until(store.Expiry) > time.Minute {
		return store.AccessToken, nil
	}
	token, err := d.exchange(url.Values{
		"client_id": {d.cfg.GoogleClientID}, "client_secret": {d.cfg.GoogleClientSecret},
		"refresh_token": {store.RefreshToken}, "grant_type": {"refresh_token"},
	})
	if err != nil {
		return "", err
	}
	store.AccessToken = token.AccessToken
	store.Expiry = time.Now().Add(time.Duration(token.ExpiresIn) * time.Second)
	_ = d.save(*store)
	return token.AccessToken, nil
}

// Disconnect revokes Google access and removes the locally stored account.
func (d *DriveBackupService) Disconnect() error {
	d.accountMu.Lock()
	defer d.accountMu.Unlock()
	store, loadErr := d.load()
	var revokeErr error
	if loadErr == nil && store.RefreshToken != "" {
		revokeErr = d.revokeToken(store.RefreshToken)
	}
	if err := os.Remove(d.storePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if revokeErr != nil {
		return ErrGoogleRevocationFailed
	}
	return nil
}

func (d *DriveBackupService) revokeToken(token string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	form := url.Values{"token": {token}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.revokeURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<20))
	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("google token revocation failed (%d)", res.StatusCode)
	}
	return nil
}

// SetFolder saves the user-chosen backup folder (empty ID means Drive root).
// The existing backup file moves to the new folder so older folders are not
// left with orphaned copies; when Drive cannot move it, the next backup
// creates a fresh file in the chosen folder instead.
func (d *DriveBackupService) SetFolder(id, name string) error {
	d.accountMu.Lock()
	defer d.accountMu.Unlock()
	store, err := d.load()
	if err != nil {
		return err
	}
	if store.RefreshToken == "" {
		return ErrGoogleNotConnected
	}
	id, name = strings.TrimSpace(id), strings.TrimSpace(name)
	if store.FileID != "" && id != "" {
		if id == store.FolderID {
			store.FolderName = name
			return d.save(store)
		}
		if token, err := d.accessToken(&store); err == nil {
			if d.moveFile(store.FileID, id, token) == nil {
				store.FolderID, store.FolderName = id, name
				return d.save(store)
			}
		}
	}
	// Keep the old ID so the next successful upload can clean it up safely.
	store.FolderID, store.FolderName = id, name
	return d.save(store)
}

// moveFile relocates a Drive file into folderID, keeping its version history.
func (d *DriveBackupService) moveFile(id, folderID, token string) error {
	body, err := d.driveGET("/files/"+url.PathEscape(id)+"?fields=parents", token)
	if err != nil {
		return err
	}
	var current struct {
		Parents []string `json:"parents"`
	}
	if err := json.Unmarshal(body, &current); err != nil {
		return err
	}
	kept := current.Parents[:0]
	for _, parent := range current.Parents {
		if parent != folderID {
			kept = append(kept, parent)
		}
	}
	params := url.Values{"addParents": {folderID}, "fields": {"id"}}
	if len(kept) > 0 {
		params.Set("removeParents", strings.Join(kept, ","))
	}
	meta, _ := json.Marshal(map[string]string{})
	req, err := http.NewRequest(http.MethodPatch, d.driveBase+"/files/"+url.PathEscape(id)+"?"+params.Encode(), bytes.NewReader(meta))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	res, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("google drive move failed (%d)", res.StatusCode)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<20))
	return nil
}

// ListFolders returns folders the user can pick as the backup destination.
func (d *DriveBackupService) ListFolders(query string) ([]DriveFolder, error) {
	d.accountMu.Lock()
	defer d.accountMu.Unlock()
	store, err := d.connectedStore()
	if err != nil {
		return nil, err
	}
	token, err := d.accessToken(&store)
	if err != nil {
		return nil, err
	}
	q := "mimeType='application/vnd.google-apps.folder' and trashed=false"
	if query = strings.TrimSpace(query); query != "" {
		literal := strings.NewReplacer(`\`, `\\`, "'", `\'`).Replace(query)
		q += " and name contains '" + literal + "'"
	}
	params := url.Values{"q": {q}, "fields": {"files(id,name)"}, "orderBy": {"name"}, "pageSize": {"100"}}
	body, err := d.driveGET("/files?"+params.Encode(), token)
	if err != nil {
		return nil, err
	}
	var out struct {
		Files []DriveFolder `json:"files"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	if out.Files == nil {
		out.Files = []DriveFolder{}
	}
	return out.Files, nil
}

// PickerAccessToken returns the short-lived Drive token required by Google Picker.
func (d *DriveBackupService) PickerAccessToken() (string, error) {
	d.accountMu.Lock()
	defer d.accountMu.Unlock()
	store, err := d.connectedStore()
	if err != nil {
		return "", err
	}
	return d.accessToken(&store)
}

func (d *DriveBackupService) connectedStore() (driveStore, error) {
	if !d.Configured() {
		return driveStore{}, ErrGoogleNotConfigured
	}
	store, err := d.load()
	if err != nil {
		return store, err
	}
	if store.RefreshToken == "" {
		return store, ErrGoogleNotConnected
	}
	return store, nil
}

func (d *DriveBackupService) driveGET(path, token string) ([]byte, error) {
	req, _ := http.NewRequest(http.MethodGet, d.driveBase+path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("google drive request failed (%d)", res.StatusCode)
	}
	return body, nil
}

// Trigger schedules a coalesced background backup; rapid creates upload once.
func (d *DriveBackupService) Trigger() {
	d.mu.Lock()
	if d.running {
		d.pending = true
		d.mu.Unlock()
		return
	}
	d.running = true
	d.mu.Unlock()
	go d.runLoop()
}

func (d *DriveBackupService) runLoop() {
	for {
		_ = d.BackupNow()
		d.mu.Lock()
		if d.pending {
			d.pending = false
			d.mu.Unlock()
			continue
		}
		d.running = false
		d.mu.Unlock()
		return
	}
}

// BackupNow exports and uploads one backup synchronously.
func (d *DriveBackupService) BackupNow() error {
	d.accountMu.Lock()
	defer d.accountMu.Unlock()
	store, err := d.connectedStore()
	if err != nil {
		return err
	}
	token, err := d.accessToken(&store)
	if err != nil {
		return d.recordFailure(store, err)
	}
	encrypted, err := d.encryptedExport()
	if err != nil {
		return d.recordFailure(store, err)
	}
	return d.replaceDriveBackup(&store, token, encrypted)
}

func (d *DriveBackupService) encryptedExport() ([]byte, error) {
	zipBytes, err := d.exportBytes()
	if err != nil {
		return nil, err
	}
	return d.EncryptBackup(zipBytes)
}

func (d *DriveBackupService) replaceDriveBackup(store *driveStore, token string, encrypted []byte) error {
	oldIDs := append([]string(nil), store.PreviousFileIDs...)
	if store.FileID != "" {
		oldIDs = append(oldIDs, store.FileID)
	}
	version := store.BackupVersion + 1
	fileID, err := d.createFile(store, token, encrypted, version)
	if err != nil {
		return d.recordFailure(*store, err)
	}
	if err := d.commitDriveBackup(store, token, fileID, version, oldIDs); err != nil {
		return err
	}
	return nil
}

func (d *DriveBackupService) commitDriveBackup(store *driveStore, token, fileID string, version int, oldIDs []string) error {
	now := time.Now().UTC()
	store.FileID, store.BackupVersion, store.LastBackupAt, store.LastStatus, store.LastError = fileID, version, &now, "ok", ""
	store.PreviousFileIDs = oldIDs
	if err := d.save(*store); err != nil {
		_ = d.deleteFile(fileID, token)
		return err
	}
	remaining := make([]string, 0, len(oldIDs))
	for _, oldID := range oldIDs {
		if oldID != fileID && d.deleteFile(oldID, token) != nil {
			remaining = append(remaining, oldID)
		}
	}
	store.PreviousFileIDs = remaining
	if len(remaining) > 0 {
		store.LastStatus = "cleanup_pending"
	}
	return d.save(*store)
}

func (d *DriveBackupService) recordFailure(store driveStore, err error) error {
	store.LastStatus, store.LastError = "error", err.Error()
	_ = d.save(store)
	return err
}

func (d *DriveBackupService) exportBytes() ([]byte, error) {
	if d.export == nil {
		return nil, errors.New("backup exporter is not configured")
	}
	buffer := &boundedBackupBuffer{limit: maxImportArchiveBytes}
	if err := d.export(buffer); err != nil {
		return nil, fmt.Errorf("create backup archive: %w", err)
	}
	return buffer.Bytes(), nil
}

type boundedBackupBuffer struct {
	bytes.Buffer
	limit int64
}

func (buffer *boundedBackupBuffer) Write(data []byte) (int, error) {
	remaining := buffer.limit - int64(buffer.Len())
	if int64(len(data)) > remaining {
		if remaining <= 0 {
			return 0, errors.New("backup ZIP exceeds the 500 MiB limit")
		}
		length := int(remaining)
		written, err := buffer.Buffer.Write(data[:length])
		if err != nil {
			return written, err
		}
		return written, errors.New("backup ZIP exceeds the 500 MiB limit")
	}
	return buffer.Buffer.Write(data)
}

func (d *DriveBackupService) createFile(store *driveStore, token string, data []byte, version int) (string, error) {
	var randomSuffix [8]byte
	if _, err := rand.Read(randomSuffix[:]); err != nil {
		return "", err
	}
	meta := map[string]interface{}{"name": fmt.Sprintf(driveBackupFilename, version, hex.EncodeToString(randomSuffix[:])), "mimeType": "application/octet-stream"}
	if store.FolderID != "" {
		meta["parents"] = []string{store.FolderID}
	}
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return "", err
	}
	params := url.Values{"uploadType": {"resumable"}, "fields": {"id"}}
	req, err := http.NewRequest(http.MethodPost, d.uploadBase+"/files?"+params.Encode(), bytes.NewReader(metaJSON))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	req.Header.Set("X-Upload-Content-Type", "application/octet-stream")
	req.Header.Set("X-Upload-Content-Length", strconv.Itoa(len(data)))
	response, err := d.doUploadInitialization(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = response.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("google drive upload initialization failed (%d)", response.StatusCode)
	}
	sessionURL, err := d.validateUploadSessionURL(response.Header.Get("Location"))
	if err != nil {
		return "", err
	}
	uploadRequest, err := http.NewRequest(http.MethodPut, sessionURL, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	uploadRequest.Header.Set("Authorization", "Bearer "+token)
	uploadRequest.Header.Set("Content-Type", "application/octet-stream")
	uploadRequest.Header.Set("Content-Length", strconv.Itoa(len(data)))
	return d.doUpload(uploadRequest)
}

func (d *DriveBackupService) doUploadInitialization(req *http.Request) (*http.Response, error) {
	client := *d.client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return client.Do(req)
}

func (d *DriveBackupService) validateUploadSessionURL(raw string) (string, error) {
	base, baseErr := url.Parse(d.uploadBase)
	session, sessionErr := url.Parse(raw)
	if baseErr != nil || sessionErr != nil || base.Scheme == "" || base.Host == "" || session.Scheme != base.Scheme || !strings.EqualFold(session.Host, base.Host) || session.User != nil || session.Fragment != "" || !strings.HasPrefix(session.EscapedPath(), strings.TrimSuffix(base.EscapedPath(), "/")+"/") {
		return "", errors.New("google drive returned an invalid resumable upload URL")
	}
	return session.String(), nil
}

func (d *DriveBackupService) deleteFile(id, token string) error {
	req, err := http.NewRequest(http.MethodDelete, d.driveBase+"/files/"+url.PathEscape(id), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusNoContent && res.StatusCode != http.StatusOK && res.StatusCode != http.StatusNotFound {
		return fmt.Errorf("google drive delete failed (%d)", res.StatusCode)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<20))
	return nil
}

func (d *DriveBackupService) doUpload(req *http.Request) (string, error) {
	res, err := d.uploadClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = res.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", fmt.Errorf("google drive upload failed (%d)", res.StatusCode)
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &out); err != nil || out.ID == "" {
		return "", errors.New("google drive upload returned no file id")
	}
	return out.ID, nil
}
