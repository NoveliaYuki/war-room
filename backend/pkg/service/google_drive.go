package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"war-room/backend/pkg/config"
)

// ErrGoogleNotConfigured indicates the OAuth client env vars or either
// encryption key are missing. Its message is deliberately generic: it is
// returned verbatim to unauthenticated HTTP clients (handleGoogleConnect
// requires no session), so it must never name the specific environment
// variables an operator needs to set. See ConfigurationIssues for the
// detailed, server-log-only diagnostic.
var ErrGoogleNotConfigured = errors.New("google sign-in is not configured")

// ErrGoogleNotConnected indicates the user has no stored Google Drive credentials.
var ErrGoogleNotConnected = errors.New("no google drive account connected")

// ErrGoogleRevocationFailed indicates that local cleanup succeeded but Google revocation failed.
var ErrGoogleRevocationFailed = errors.New("google access may still be authorized; revoke War Room access in your Google Account settings")

const driveBackupFilename = "war-room-backup-v%d-%s.wrbackup"
const maxGoogleOAuthStates = 128

// DriveStatus is the public, token-free view of one user's Drive connection.
// Identity (email, name, picture) lives in GET /api/auth/me, not here: this
// endpoint only ever reports Drive/backup state.
type DriveStatus struct {
	Configured   bool       `json:"configured"`
	PickerAPIKey string     `json:"picker_api_key,omitempty"`
	PickerAppID  string     `json:"picker_app_id,omitempty"`
	Connected    bool       `json:"connected"`
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

// googleUserInfo is the identity claims read from Google's userinfo endpoint.
type googleUserInfo struct {
	Sub           string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
}

// driveAccount is one user's stored Drive credentials and backup bookkeeping.
type driveAccount struct {
	RefreshToken    string
	AccessToken     string
	Expiry          time.Time
	FolderID        string
	FolderName      string
	FileID          string
	BackupVersion   int
	PreviousFileIDs []string
	LastBackupAt    *time.Time
	LastStatus      string
	LastError       string
}

// driveCredentials is the sealed portion of a driveAccount: only the
// material an attacker could use against the user's Google account is
// encrypted; folder names and backup status are not secrets.
type driveCredentials struct {
	RefreshToken string    `json:"refresh_token"`
	AccessToken  string    `json:"access_token"`
	Expiry       time.Time `json:"expiry"`
}

// DriveBackupService uploads encrypted backups to each signed-in user's
// Google Drive, using credentials stored per-user in google_drive_accounts.
type DriveBackupService struct {
	db           *sql.DB
	cfg          *config.Config
	export       func(userID string, output io.Writer) error
	client       *http.Client
	uploadClient *http.Client
	authBase     string
	tokenURL     string
	userURL      string
	revokeURL    string
	driveBase    string
	uploadBase   string

	mu         sync.Mutex
	running    map[string]bool
	pending    map[string]bool
	states     map[string]time.Time
	accountMus map[string]*sync.Mutex
}

// lockAccount serializes read-modify-write sequences against one user's
// Drive account (e.g. a folder change racing an in-flight backup upload),
// without blocking unrelated users on each other.
func (d *DriveBackupService) lockAccount(userID string) func() {
	d.mu.Lock()
	lock, ok := d.accountMus[userID]
	if !ok {
		lock = &sync.Mutex{}
		d.accountMus[userID] = lock
	}
	d.mu.Unlock()
	lock.Lock()
	return lock.Unlock
}

// NewDriveBackupService creates the service. export supplies one user's
// backup archive bytes.
func NewDriveBackupService(db *sql.DB, cfg *config.Config, export func(userID string, output io.Writer) error) *DriveBackupService {
	// #nosec G101 -- endpoint URLs only; no credential values are hardcoded.
	return &DriveBackupService{
		db: db, cfg: cfg, export: export,
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
		running:    map[string]bool{},
		pending:    map[string]bool{},
		accountMus: map[string]*sync.Mutex{},
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

// Configured reports whether OAuth credentials and both local encryption
// keys are present.
func (d *DriveBackupService) Configured() bool {
	return len(d.ConfigurationIssues()) == 0
}

// ConfigurationIssues names exactly which required environment variables are
// missing or invalid. It exists only for a server-side startup log: unlike
// ErrGoogleNotConfigured (returned to anonymous HTTP clients), this detail
// must never reach an HTTP response.
func (d *DriveBackupService) ConfigurationIssues() []string {
	var issues []string
	if d.cfg.GoogleClientID == "" {
		issues = append(issues, "GOOGLE_CLIENT_ID is not set")
	}
	if d.cfg.GoogleClientSecret == "" {
		issues = append(issues, "GOOGLE_CLIENT_SECRET is not set")
	}
	if _, err := d.encryptionKey(); err != nil {
		issues = append(issues, "GOOGLE_BACKUP_ENCRYPTION_KEY is missing or invalid")
	}
	if _, err := d.authTokenKey(); err != nil {
		issues = append(issues, "AUTH_TOKEN_ENCRYPTION_KEY is missing or invalid")
	}
	return issues
}

func (d *DriveBackupService) loadAccount(userID string) (driveAccount, bool, error) {
	var account driveAccount
	var credentialsEncrypted []byte
	var lastBackupAt sql.NullInt64
	var previousFileIDsJSON string
	row := d.db.QueryRow(`SELECT credentials_encrypted, folder_id, folder_name, file_id, backup_version, previous_file_ids_json, last_backup_at, last_status, last_error
		FROM google_drive_accounts WHERE user_id = ?`, userID)
	err := row.Scan(&credentialsEncrypted, &account.FolderID, &account.FolderName, &account.FileID, &account.BackupVersion,
		&previousFileIDsJSON, &lastBackupAt, &account.LastStatus, &account.LastError)
	if errors.Is(err, sql.ErrNoRows) {
		return driveAccount{}, false, nil
	}
	if err != nil {
		return driveAccount{}, false, err
	}
	if lastBackupAt.Valid {
		backupTime := time.Unix(lastBackupAt.Int64, 0).UTC()
		account.LastBackupAt = &backupTime
	}
	if previousFileIDsJSON != "" {
		if err := json.Unmarshal([]byte(previousFileIDsJSON), &account.PreviousFileIDs); err != nil {
			return driveAccount{}, false, fmt.Errorf("read previous drive file ids: %w", err)
		}
	}
	key, err := d.authTokenKey()
	if err != nil {
		return driveAccount{}, false, err
	}
	plaintext, err := openDriveData(key, accountEnvelopeMagic, accountEnvelopeAAD, credentialsEncrypted)
	if err != nil {
		return driveAccount{}, false, err
	}
	var credentials driveCredentials
	if err := json.Unmarshal(plaintext, &credentials); err != nil {
		return driveAccount{}, false, fmt.Errorf("read drive credentials: %w", err)
	}
	account.RefreshToken, account.AccessToken, account.Expiry = credentials.RefreshToken, credentials.AccessToken, credentials.Expiry
	return account, true, nil
}

func (d *DriveBackupService) saveAccount(userID string, account driveAccount) error {
	key, err := d.authTokenKey()
	if err != nil {
		return err
	}
	credentialsJSON, err := json.Marshal(driveCredentials{RefreshToken: account.RefreshToken, AccessToken: account.AccessToken, Expiry: account.Expiry}) // #nosec G117 -- this plaintext JSON is sealed by sealDriveData immediately below and never persisted or logged unencrypted.
	if err != nil {
		return err
	}
	encrypted, err := sealDriveData(key, accountEnvelopeMagic, accountEnvelopeAAD, credentialsJSON)
	if err != nil {
		return err
	}
	if account.PreviousFileIDs == nil {
		account.PreviousFileIDs = []string{}
	}
	previousFileIDsJSON, err := json.Marshal(account.PreviousFileIDs)
	if err != nil {
		return err
	}
	var lastBackupAt any
	if account.LastBackupAt != nil {
		lastBackupAt = account.LastBackupAt.Unix()
	}
	_, err = d.db.Exec(`
		INSERT INTO google_drive_accounts (user_id, credentials_encrypted, folder_id, folder_name, file_id, backup_version, previous_file_ids_json, last_backup_at, last_status, last_error, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, unixepoch())
		ON CONFLICT(user_id) DO UPDATE SET
			credentials_encrypted = excluded.credentials_encrypted, folder_id = excluded.folder_id, folder_name = excluded.folder_name,
			file_id = excluded.file_id, backup_version = excluded.backup_version, previous_file_ids_json = excluded.previous_file_ids_json,
			last_backup_at = excluded.last_backup_at, last_status = excluded.last_status, last_error = excluded.last_error, updated_at = unixepoch()
	`, userID, encrypted, account.FolderID, account.FolderName, account.FileID, account.BackupVersion,
		string(previousFileIDsJSON), lastBackupAt, account.LastStatus, account.LastError)
	return err
}

// Status returns userID's public connection state without any tokens.
func (d *DriveBackupService) Status(userID string) DriveStatus {
	defer d.lockAccount(userID)()
	status := DriveStatus{Configured: d.Configured(), PickerAPIKey: d.cfg.GooglePickerAPIKey, PickerAppID: googleProjectNumber(d.cfg.GoogleClientID, d.cfg.GoogleCloudProjectNumber)}
	account, found, err := d.loadAccount(userID)
	if err != nil || !found || account.RefreshToken == "" {
		return status
	}
	status.Connected = true
	status.FolderID, status.FolderName = account.FolderID, account.FolderName
	status.LastBackupAt, status.LastStatus, status.LastError = account.LastBackupAt, account.LastStatus, account.LastError
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

// evictOldestOAuthState drops the state closest to expiry so anonymous
// callers cannot lock everyone out of signing in by filling the table.
func evictOldestOAuthState(states map[string]time.Time) {
	var oldest string
	var oldestExpiry time.Time
	for state, expiry := range states {
		if oldest == "" || expiry.Before(oldestExpiry) {
			oldest, oldestExpiry = state, expiry
		}
	}
	delete(states, oldest)
}

// AuthURL starts the combined sign-in-and-Drive-grant OAuth flow with a
// short-lived state. Signing in and authorizing Drive are the same consent:
// the scope list carries both identity (openid email profile) and Drive
// (drive.file) scopes in one request.
func (d *DriveBackupService) AuthURL() (string, error) {
	if !d.Configured() {
		return "", ErrGoogleNotConfigured
	}
	raw := make([]byte, 16)
	// io.ReadFull(rand.Reader, ...) rather than the rand.Read convenience
	// function, which terminates the process on a read failure instead of
	// returning an error.
	if _, err := io.ReadFull(rand.Reader, raw); err != nil {
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
		evictOldestOAuthState(d.states)
	}
	d.states[state] = now.Add(10 * time.Minute)
	d.mu.Unlock()
	q := url.Values{
		"client_id": {d.cfg.GoogleClientID}, "redirect_uri": {d.cfg.GoogleRedirectURI},
		"response_type": {"code"}, "state": {state}, "access_type": {"offline"}, "prompt": {"consent"},
		"scope": {"openid email profile https://www.googleapis.com/auth/drive.file"},
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

// ExchangeCode exchanges an OAuth authorization code for Google tokens and
// identity claims. It performs no persistence: the caller (the callback
// handler) decides whether the identity is allowed to sign in, and only
// then calls SaveAccount to store the Drive credentials alongside the new
// session.
func (d *DriveBackupService) ExchangeCode(code, state string) (googleUserInfo, tokenResponse, error) {
	if !d.validState(state) || code == "" {
		return googleUserInfo{}, tokenResponse{}, errors.New("invalid or expired google sign-in state")
	}
	token, err := d.exchange(url.Values{
		"code": {code}, "client_id": {d.cfg.GoogleClientID}, "client_secret": {d.cfg.GoogleClientSecret},
		"redirect_uri": {d.cfg.GoogleRedirectURI}, "grant_type": {"authorization_code"},
	})
	if err != nil {
		return googleUserInfo{}, tokenResponse{}, err
	}
	info, err := d.fetchUserInfo(token.AccessToken)
	if err != nil {
		return googleUserInfo{}, tokenResponse{}, err
	}
	if token.RefreshToken == "" {
		return googleUserInfo{}, tokenResponse{}, errors.New("google did not return a refresh token; sign out and sign in again")
	}
	return info, token, nil
}

// SaveAccount stores or refreshes userID's Drive credentials, preserving
// any previously chosen backup folder and backup bookkeeping.
func (d *DriveBackupService) SaveAccount(userID string, token tokenResponse) error {
	defer d.lockAccount(userID)()
	account, _, err := d.loadAccount(userID)
	if err != nil {
		return err
	}
	account.RefreshToken = token.RefreshToken
	account.AccessToken = token.AccessToken
	account.Expiry = time.Now().Add(time.Duration(token.ExpiresIn) * time.Second)
	account.LastStatus = "connected"
	return d.saveAccount(userID, account)
}

// SaveAccountForTesting seeds userID's Drive credentials directly, bypassing
// the OAuth exchange. Exported so other packages' tests can simulate an
// already-connected account without a fake Google server.
func (d *DriveBackupService) SaveAccountForTesting(userID, refreshToken, accessToken string, expiresInSeconds int) error {
	return d.SaveAccount(userID, tokenResponse{RefreshToken: refreshToken, AccessToken: accessToken, ExpiresIn: expiresInSeconds})
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

func (d *DriveBackupService) fetchUserInfo(accessToken string) (googleUserInfo, error) {
	req, _ := http.NewRequest(http.MethodGet, d.userURL, nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	res, err := d.client.Do(req)
	if err != nil {
		return googleUserInfo{}, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return googleUserInfo{}, fmt.Errorf("google userinfo request failed (%d)", res.StatusCode)
	}
	var info googleUserInfo
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&info); err != nil {
		return googleUserInfo{}, err
	}
	if strings.TrimSpace(info.Email) == "" || strings.TrimSpace(info.Sub) == "" {
		return googleUserInfo{}, errors.New("google userinfo response did not include an email or subject id")
	}
	return info, nil
}

func (d *DriveBackupService) accessToken(userID string, account *driveAccount) (string, error) {
	if account.AccessToken != "" && time.Until(account.Expiry) > time.Minute {
		return account.AccessToken, nil
	}
	token, err := d.exchange(url.Values{
		"client_id": {d.cfg.GoogleClientID}, "client_secret": {d.cfg.GoogleClientSecret},
		"refresh_token": {account.RefreshToken}, "grant_type": {"refresh_token"},
	})
	if err != nil {
		return "", err
	}
	account.AccessToken = token.AccessToken
	account.Expiry = time.Now().Add(time.Duration(token.ExpiresIn) * time.Second)
	_ = d.saveAccount(userID, *account)
	return token.AccessToken, nil
}

// Revoke revokes userID's Google Drive grant via Google's revoke endpoint
// and deletes their stored credentials. The caller is responsible for also
// ending every session for userID: identity and Drive are now the same
// OAuth consent, so revoking one must revoke both.
func (d *DriveBackupService) Revoke(userID string) error {
	defer d.lockAccount(userID)()
	account, found, loadErr := d.loadAccount(userID)
	var revokeErr error
	if loadErr == nil && found && account.RefreshToken != "" {
		revokeErr = d.revokeToken(account.RefreshToken)
	}
	if _, err := d.db.Exec(`DELETE FROM google_drive_accounts WHERE user_id = ?`, userID); err != nil {
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

// SetFolder saves userID's chosen backup folder (empty ID means Drive root).
// The existing backup file moves to the new folder so older folders are not
// left with orphaned copies; when Drive cannot move it, the next backup
// creates a fresh file in the chosen folder instead.
func (d *DriveBackupService) SetFolder(userID, id, name string) error {
	defer d.lockAccount(userID)()
	account, found, err := d.loadAccount(userID)
	if err != nil {
		return err
	}
	if !found || account.RefreshToken == "" {
		return ErrGoogleNotConnected
	}
	id, name = strings.TrimSpace(id), strings.TrimSpace(name)
	if account.FileID != "" && id != "" {
		if id == account.FolderID {
			account.FolderName = name
			return d.saveAccount(userID, account)
		}
		if token, err := d.accessToken(userID, &account); err == nil {
			if d.moveFile(account.FileID, id, token) == nil {
				account.FolderID, account.FolderName = id, name
				return d.saveAccount(userID, account)
			}
		}
	}
	// Keep the old ID so the next successful upload can clean it up safely.
	account.FolderID, account.FolderName = id, name
	return d.saveAccount(userID, account)
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

// ListFolders returns folders userID can pick as the backup destination.
func (d *DriveBackupService) ListFolders(userID, query string) ([]DriveFolder, error) {
	defer d.lockAccount(userID)()
	account, err := d.connectedAccount(userID)
	if err != nil {
		return nil, err
	}
	token, err := d.accessToken(userID, &account)
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
func (d *DriveBackupService) PickerAccessToken(userID string) (string, error) {
	defer d.lockAccount(userID)()
	account, err := d.connectedAccount(userID)
	if err != nil {
		return "", err
	}
	return d.accessToken(userID, &account)
}

func (d *DriveBackupService) connectedAccount(userID string) (driveAccount, error) {
	if !d.Configured() {
		return driveAccount{}, ErrGoogleNotConfigured
	}
	account, found, err := d.loadAccount(userID)
	if err != nil {
		return account, err
	}
	if !found || account.RefreshToken == "" {
		return account, ErrGoogleNotConnected
	}
	return account, nil
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

// Trigger schedules a coalesced background backup for userID; rapid creates
// upload once per user.
func (d *DriveBackupService) Trigger(userID string) {
	d.mu.Lock()
	if d.running[userID] {
		d.pending[userID] = true
		d.mu.Unlock()
		return
	}
	d.running[userID] = true
	d.mu.Unlock()
	go d.runLoop(userID)
}

func (d *DriveBackupService) runLoop(userID string) {
	for {
		_ = d.BackupNow(userID)
		d.mu.Lock()
		if d.pending[userID] {
			d.pending[userID] = false
			d.mu.Unlock()
			continue
		}
		d.running[userID] = false
		d.mu.Unlock()
		return
	}
}

// BackupNow exports and uploads one backup for userID synchronously.
func (d *DriveBackupService) BackupNow(userID string) error {
	defer d.lockAccount(userID)()
	account, err := d.connectedAccount(userID)
	if err != nil {
		return err
	}
	token, err := d.accessToken(userID, &account)
	if err != nil {
		return d.recordFailure(userID, account, err)
	}
	encrypted, size, err := d.encryptedExport(userID)
	if err != nil {
		return d.recordFailure(userID, account, err)
	}
	defer DiscardTempFile(encrypted)
	return d.replaceDriveBackup(userID, &account, token, encrypted, size)
}

// encryptedExport streams the user's archive through the encryptor into a
// private temp file, so neither the archive nor its ciphertext sits in memory.
// The caller owns the returned file and must pass it to DiscardTempFile.
func (d *DriveBackupService) encryptedExport(userID string) (*os.File, int64, error) {
	if d.export == nil {
		return nil, 0, errors.New("backup exporter is not configured")
	}
	file, size, err := d.SealArchiveFile(d.cfg.DataDir, maxImportArchiveBytes, func(out io.Writer) error {
		return d.export(userID, out)
	})
	if err != nil {
		return nil, 0, fmt.Errorf("create backup archive: %w", err)
	}
	return file, size, nil
}

func (d *DriveBackupService) replaceDriveBackup(userID string, account *driveAccount, token string, encrypted io.Reader, size int64) error {
	oldIDs := append([]string(nil), account.PreviousFileIDs...)
	if account.FileID != "" {
		oldIDs = append(oldIDs, account.FileID)
	}
	version := account.BackupVersion + 1
	fileID, err := d.createFile(account, token, encrypted, size, version)
	if err != nil {
		return d.recordFailure(userID, *account, err)
	}
	return d.commitDriveBackup(userID, account, token, fileID, version, oldIDs)
}

func (d *DriveBackupService) commitDriveBackup(userID string, account *driveAccount, token, fileID string, version int, oldIDs []string) error {
	now := time.Now().UTC()
	account.FileID, account.BackupVersion, account.LastBackupAt, account.LastStatus, account.LastError = fileID, version, &now, "ok", ""
	account.PreviousFileIDs = oldIDs
	if err := d.saveAccount(userID, *account); err != nil {
		_ = d.deleteFile(fileID, token)
		return err
	}
	remaining := make([]string, 0, len(oldIDs))
	for _, oldID := range oldIDs {
		if oldID != fileID && d.deleteFile(oldID, token) != nil {
			remaining = append(remaining, oldID)
		}
	}
	account.PreviousFileIDs = remaining
	if len(remaining) > 0 {
		account.LastStatus = "cleanup_pending"
	}
	return d.saveAccount(userID, *account)
}

func (d *DriveBackupService) recordFailure(userID string, account driveAccount, err error) error {
	account.LastStatus, account.LastError = "error", err.Error()
	_ = d.saveAccount(userID, account)
	return err
}

func (d *DriveBackupService) createFile(account *driveAccount, token string, body io.Reader, size int64, version int) (string, error) {
	var randomSuffix [8]byte
	if _, err := io.ReadFull(rand.Reader, randomSuffix[:]); err != nil {
		return "", err
	}
	meta := map[string]interface{}{"name": fmt.Sprintf(driveBackupFilename, version, hex.EncodeToString(randomSuffix[:])), "mimeType": "application/octet-stream"}
	if account.FolderID != "" {
		meta["parents"] = []string{account.FolderID}
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
	req.Header.Set("X-Upload-Content-Length", strconv.FormatInt(size, 10))
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
	uploadRequest, err := http.NewRequest(http.MethodPut, sessionURL, body)
	if err != nil {
		return "", err
	}
	uploadRequest.ContentLength = size
	uploadRequest.Header.Set("Authorization", "Bearer "+token)
	uploadRequest.Header.Set("Content-Type", "application/octet-stream")
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
