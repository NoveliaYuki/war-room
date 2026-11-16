package service

import (
	"bytes"
	"crypto/rand"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"war-room/backend/pkg/config"
	"war-room/backend/pkg/database"
)

func newAuthTestService(t *testing.T) (*AuthService, *sql.DB) {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{
		DataDir: dir, DBPath: filepath.Join(dir, "auth.db"), BackupPath: filepath.Join(dir, "backup.json"),
		AttachmentsDir: filepath.Join(dir, "attachments"), CVDir: filepath.Join(dir, "cvs"), LogosDir: filepath.Join(dir, "logos"),
		AvatarsDir:        filepath.Join(dir, "avatars"),
		AuthAllowedEmails: []string{"allowed@example.com"},
	}
	if err := os.MkdirAll(cfg.AvatarsDir, 0700); err != nil {
		t.Fatalf("create avatars dir: %v", err)
	}
	db, err := database.InitDB(cfg)
	if err != nil {
		t.Fatalf("init database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewAuthService(db, cfg), db
}

func TestUpsertUserFromGoogleRejectsUnlistedEmail(t *testing.T) {
	auth, db := newAuthTestService(t)
	_, err := auth.UpsertUserFromGoogle(GoogleIdentity{Sub: "sub-1", Email: "stranger@example.com", EmailVerified: true, Name: "Stranger"}, "203.0.113.5")
	if err != ErrEmailNotAllowed {
		t.Fatalf("UpsertUserFromGoogle error=%v, want ErrEmailNotAllowed", err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rejected sign-in created a user row: count=%d err=%v", count, err)
	}
}

func TestUpsertUserFromGoogleRejectsUnverifiedEmail(t *testing.T) {
	auth, _ := newAuthTestService(t)
	_, err := auth.UpsertUserFromGoogle(GoogleIdentity{Sub: "sub-1", Email: "allowed@example.com", EmailVerified: false, Name: "Allowed"}, "203.0.113.5")
	if err != ErrEmailNotAllowed {
		t.Fatalf("UpsertUserFromGoogle error=%v, want ErrEmailNotAllowed for an unverified email", err)
	}
}

func TestUpsertUserFromGoogleAcceptsAllowedVerifiedEmailAndIsIdempotent(t *testing.T) {
	auth, db := newAuthTestService(t)
	first, err := auth.UpsertUserFromGoogle(GoogleIdentity{Sub: "sub-1", Email: "Allowed@Example.com", EmailVerified: true, Name: "Allowed"}, "203.0.113.5")
	if err != nil {
		t.Fatalf("UpsertUserFromGoogle: %v", err)
	}
	if first.Email != "allowed@example.com" {
		t.Fatalf("email = %q, want lowercased", first.Email)
	}
	second, err := auth.UpsertUserFromGoogle(GoogleIdentity{Sub: "sub-1", Email: "allowed@example.com", EmailVerified: true, Name: "Allowed Renamed"}, "203.0.113.5")
	if err != nil {
		t.Fatalf("second UpsertUserFromGoogle: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("second sign-in created a new user row: first=%q second=%q", first.ID, second.ID)
	}
	if second.Name != "Allowed Renamed" {
		t.Fatalf("name = %q, want updated name from second sign-in", second.Name)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("user count = %d, want exactly 1", count)
	}
}

func TestUpsertUserFromGoogleClaimsUnownedDataOnlyOnFirstUser(t *testing.T) {
	auth, db := newAuthTestService(t)
	if _, err := db.Exec(`INSERT INTO jobs (id, position_title, status, salary_type, recruiter_type, avatar_seed) VALUES ('legacy-job', 'Engineer', 'waiting', 'unknown', 'none', 'seed')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO cv_versions (id, version, original_name, stored_filename, file_size, sha256, uploaded_at) VALUES ('legacy-cv', 3, 'resume.pdf', 'a.pdf', 1, 'digest', 1)`); err != nil {
		t.Fatal(err)
	}

	first, err := auth.UpsertUserFromGoogle(GoogleIdentity{Sub: "sub-first", Email: "allowed@example.com", EmailVerified: true, Name: "First"}, "203.0.113.5")
	if err != nil {
		t.Fatalf("UpsertUserFromGoogle: %v", err)
	}
	assertLegacyJobOwner(t, db, first.ID)
	var nextVersion int
	if err := db.QueryRow(`SELECT next_version FROM cv_version_sequence WHERE user_id = ?`, first.ID).Scan(&nextVersion); err != nil || nextVersion != 4 {
		t.Fatalf("claimed cv sequence next_version=%d err=%v, want 4", nextVersion, err)
	}

	cfg := &config.Config{AuthAllowedEmails: []string{"allowed@example.com", "second@example.com"}}
	secondAuth := NewAuthService(db, cfg)
	if _, err := secondAuth.UpsertUserFromGoogle(GoogleIdentity{Sub: "sub-second", Email: "second@example.com", EmailVerified: true, Name: "Second"}, "203.0.113.6"); err != nil {
		t.Fatalf("second user sign-in: %v", err)
	}
	assertLegacyJobOwner(t, db, first.ID)
}

// assertLegacyJobOwner confirms the pre-account "legacy-job" row is (still)
// owned by wantOwnerID.
func assertLegacyJobOwner(t *testing.T, db *sql.DB, wantOwnerID string) {
	t.Helper()
	var ownerID sql.NullString
	if err := db.QueryRow(`SELECT user_id FROM jobs WHERE id = 'legacy-job'`).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	if !ownerID.Valid || ownerID.String != wantOwnerID {
		t.Fatalf("legacy job owner = %+v, want %q", ownerID, wantOwnerID)
	}
}

func TestSessionLifecycleIssueAuthenticateSignOut(t *testing.T) {
	auth, _ := newAuthTestService(t)
	user, err := auth.UpsertUserFromGoogle(GoogleIdentity{Sub: "sub-1", Email: "allowed@example.com", EmailVerified: true, Name: "Allowed"}, "203.0.113.5")
	if err != nil {
		t.Fatal(err)
	}

	issueRecorder := httptest.NewRecorder()
	if err := auth.IssueSession(issueRecorder, user.ID); err != nil {
		t.Fatalf("IssueSession: %v", err)
	}
	cookie := issuedSessionCookie(t, issueRecorder)

	authenticated := authenticatedRequest(cookie)
	authRecorder := httptest.NewRecorder()
	gotUserID, ok := auth.Authenticate(authRecorder, authenticated)
	if !ok || gotUserID != user.ID {
		t.Fatalf("Authenticate() = (%q, %t), want (%q, true)", gotUserID, ok, user.ID)
	}

	signOutRecorder := httptest.NewRecorder()
	if err := auth.SignOut(signOutRecorder, authenticatedRequest(cookie)); err != nil {
		t.Fatalf("SignOut: %v", err)
	}
	afterSignOut := httptest.NewRecorder()
	if _, ok := auth.Authenticate(afterSignOut, authenticatedRequest(cookie)); ok {
		t.Fatal("session remained valid after SignOut")
	}
}

func TestSignOutAllSessionsRevokesEveryDevice(t *testing.T) {
	auth, _ := newAuthTestService(t)
	user, err := auth.UpsertUserFromGoogle(GoogleIdentity{Sub: "sub-1", Email: "allowed@example.com", EmailVerified: true, Name: "Allowed"}, "203.0.113.5")
	if err != nil {
		t.Fatal(err)
	}

	deviceOne, deviceTwo := httptest.NewRecorder(), httptest.NewRecorder()
	if err := auth.IssueSession(deviceOne, user.ID); err != nil {
		t.Fatal(err)
	}
	if err := auth.IssueSession(deviceTwo, user.ID); err != nil {
		t.Fatal(err)
	}
	cookieOne := issuedSessionCookie(t, deviceOne)
	cookieTwo := issuedSessionCookie(t, deviceTwo)

	if err := auth.SignOutAllSessions(user.ID); err != nil {
		t.Fatalf("SignOutAllSessions: %v", err)
	}
	for _, cookie := range []*http.Cookie{cookieOne, cookieTwo} {
		if _, ok := auth.Authenticate(httptest.NewRecorder(), authenticatedRequest(cookie)); ok {
			t.Fatalf("session for cookie %q remained valid after SignOutAllSessions", cookie.Value)
		}
	}
}

func TestAuthenticateRejectsExpiredSession(t *testing.T) {
	auth, db := newAuthTestService(t)
	user, err := auth.UpsertUserFromGoogle(GoogleIdentity{Sub: "sub-1", Email: "allowed@example.com", EmailVerified: true, Name: "Allowed"}, "203.0.113.5")
	if err != nil {
		t.Fatal(err)
	}
	issueRecorder := httptest.NewRecorder()
	if err := auth.IssueSession(issueRecorder, user.ID); err != nil {
		t.Fatal(err)
	}
	cookie := issuedSessionCookie(t, issueRecorder)
	if _, err := db.Exec(`UPDATE sessions SET expires_at = ? WHERE user_id = ?`, time.Now().Add(-time.Hour).Unix(), user.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := auth.Authenticate(httptest.NewRecorder(), authenticatedRequest(cookie)); ok {
		t.Fatal("Authenticate accepted an expired session")
	}
}

func TestAuthenticateSlidesExpiryForwardNearEndOfLife(t *testing.T) {
	auth, db := newAuthTestService(t)
	user, err := auth.UpsertUserFromGoogle(GoogleIdentity{Sub: "sub-1", Email: "allowed@example.com", EmailVerified: true, Name: "Allowed"}, "203.0.113.5")
	if err != nil {
		t.Fatal(err)
	}
	issueRecorder := httptest.NewRecorder()
	if err := auth.IssueSession(issueRecorder, user.ID); err != nil {
		t.Fatal(err)
	}
	cookie := issuedSessionCookie(t, issueRecorder)
	soonExpiry := time.Now().Add(24 * time.Hour)
	if _, err := db.Exec(`UPDATE sessions SET expires_at = ? WHERE user_id = ?`, soonExpiry.Unix(), user.ID); err != nil {
		t.Fatal(err)
	}

	renewRecorder := httptest.NewRecorder()
	if _, ok := auth.Authenticate(renewRecorder, authenticatedRequest(cookie)); !ok {
		t.Fatal("Authenticate rejected a soon-to-expire but still valid session")
	}
	var storedExpiry int64
	if err := db.QueryRow(`SELECT expires_at FROM sessions WHERE user_id = ?`, user.ID).Scan(&storedExpiry); err != nil {
		t.Fatal(err)
	}
	if storedExpiry <= soonExpiry.Unix() {
		t.Fatalf("expiry was not slid forward: before=%d after=%d", soonExpiry.Unix(), storedExpiry)
	}
	if renewed := issuedSessionCookie(t, renewRecorder); renewed.Value != cookie.Value {
		t.Fatal("renewed cookie carries a different token than the original session")
	}
}

func issuedSessionCookie(t *testing.T, recorder *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == sessionCookieName {
			return cookie
		}
	}
	t.Fatal("no session cookie was set")
	return nil
}

func authenticatedRequest(cookie *http.Cookie) *http.Request {
	request := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	request.AddCookie(cookie)
	return request
}

func TestCreateTestSessionRequiresAnEmail(t *testing.T) {
	auth, _ := newAuthTestService(t)
	if err := auth.CreateTestSession(httptest.NewRecorder(), "   "); err == nil {
		t.Fatal("CreateTestSession accepted a blank email")
	}
}

func TestUpsertUserFromGoogleRejectsMissingSubjectID(t *testing.T) {
	auth, _ := newAuthTestService(t)
	_, err := auth.UpsertUserFromGoogle(GoogleIdentity{Sub: "", Email: "allowed@example.com", EmailVerified: true, Name: "Allowed"}, "203.0.113.5")
	if err == nil {
		t.Fatal("UpsertUserFromGoogle accepted an identity with no subject id")
	}
}

func TestAuthenticateRejectsUnknownSessionToken(t *testing.T) {
	auth, _ := newAuthTestService(t)
	forged := &http.Cookie{Name: sessionCookieName, Value: "forged-token-that-was-never-issued", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode}
	if _, ok := auth.Authenticate(httptest.NewRecorder(), authenticatedRequest(forged)); ok {
		t.Fatal("Authenticate accepted a token that was never issued")
	}
}

func TestIssueSessionSurfacesRandomReadFailure(t *testing.T) {
	auth, _ := newAuthTestService(t)
	original := rand.Reader
	rand.Reader = bytes.NewReader(nil)
	defer func() { rand.Reader = original }()
	if err := auth.IssueSession(httptest.NewRecorder(), "user-1"); err == nil {
		t.Fatal("IssueSession succeeded despite a failed random read")
	}
}

func TestFirstSignInDoesNotAdoptPreAccountDataWhenSeveralAccountsAreAllowed(t *testing.T) {
	_, db := newAuthTestService(t)
	if _, err := db.Exec(`INSERT INTO jobs (id, position_title, status, salary_type, recruiter_type, avatar_seed) VALUES ('legacy-job', 'Engineer', 'waiting', 'unknown', 'none', 'seed')`); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{AuthAllowedEmails: []string{"allowed@example.com", "second@example.com"}}
	multi := NewAuthService(db, cfg)
	if _, err := multi.UpsertUserFromGoogle(GoogleIdentity{Sub: "sub-first", Email: "allowed@example.com", EmailVerified: true, Name: "First"}, "203.0.113.5"); err != nil {
		t.Fatal(err)
	}
	var ownerID sql.NullString
	if err := db.QueryRow(`SELECT user_id FROM jobs WHERE id = 'legacy-job'`).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	if ownerID.Valid {
		t.Fatalf("pre-account data was adopted by %q despite several allowed accounts", ownerID.String)
	}
}

func TestIssueSessionPurgesExpiredSessions(t *testing.T) {
	auth, db := newAuthTestService(t)
	user, err := auth.UpsertUserFromGoogle(GoogleIdentity{Sub: "sub-purge", Email: "allowed@example.com", EmailVerified: true, Name: "Purge"}, "203.0.113.5")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO sessions (id, user_id, token_hash, expires_at) VALUES ('stale', ?, 'stale-hash', 1)`, user.ID); err != nil {
		t.Fatal(err)
	}
	if err := auth.IssueSession(httptest.NewRecorder(), user.ID); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE id = 'stale'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("expired session survived: count=%d err=%v", count, err)
	}
}
