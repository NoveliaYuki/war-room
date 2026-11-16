package service

import (
	"bytes"
	"crypto/rand"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"war-room/backend/pkg/logging"
)

func newAPITokenTestUser(t *testing.T) (*AuthService, string) {
	t.Helper()
	auth, _ := newAuthTestService(t)
	user, err := auth.UpsertUserFromGoogle(GoogleIdentity{Sub: "sub-1", Email: "allowed@example.com", EmailVerified: true, Name: "Allowed"}, "203.0.113.5")
	if err != nil {
		t.Fatal(err)
	}
	return auth, user.ID
}

func bearerRequest(header string) *http.Request {
	request := httptest.NewRequest(http.MethodGet, "/api/jobs", nil)
	request.Header.Set("Authorization", header)
	return request
}

func mustCreateAPIToken(t *testing.T, auth *AuthService) (string, APIToken) {
	t.Helper()
	token, record, err := auth.CreateAPIToken(" Allowed@Example.com ", " laptop ", DefaultAPITokenLifetime)
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}
	return token, record
}

func TestAPITokenAuthenticatesAsItsOwnerAndRecordsUse(t *testing.T) {
	auth, userID := newAPITokenTestUser(t)
	token, record := mustCreateAPIToken(t, auth)
	if !strings.HasPrefix(token, apiTokenPrefix) || record.Name != "laptop" {
		t.Fatalf("token=%q name=%q, want prefixed token and trimmed name", token[:3], record.Name)
	}
	var stored int
	if err := auth.db.QueryRow(`SELECT COUNT(*) FROM api_tokens WHERE token_hash = ?`, token).Scan(&stored); err != nil || stored != 0 {
		t.Fatalf("plaintext token must never be stored, matches=%d err=%v", stored, err)
	}
	recorder := httptest.NewRecorder()
	gotUserID, ok := auth.Authenticate(recorder, bearerRequest("Bearer "+token))
	if !ok || gotUserID != userID {
		t.Fatalf("Authenticate() = (%q, %t), want (%q, true)", gotUserID, ok, userID)
	}
	if len(recorder.Result().Cookies()) != 0 {
		t.Fatal("token authentication must not set a session cookie")
	}
}

func TestAPITokenUseIsRecorded(t *testing.T) {
	auth, _ := newAPITokenTestUser(t)
	token, _ := mustCreateAPIToken(t, auth)
	if _, ok := auth.Authenticate(httptest.NewRecorder(), bearerRequest("Bearer "+token)); !ok {
		t.Fatal("token was rejected")
	}
	tokens, err := auth.ListAPITokens("allowed@example.com")
	if err != nil || len(tokens) != 1 || tokens[0].LastUsedAt == nil {
		t.Fatalf("ListAPITokens = %+v err=%v, want one token with a last-used time", tokens, err)
	}
}

func TestInvalidBearerNeverFallsBackToSessionCookie(t *testing.T) {
	auth, userID := newAPITokenTestUser(t)
	issueRecorder := httptest.NewRecorder()
	if err := auth.IssueSession(issueRecorder, userID); err != nil {
		t.Fatal(err)
	}
	for _, header := range []string{"Bearer wr_never-issued", "Basic abc", "Bearer not-a-war-room-token"} {
		request := bearerRequest(header)
		request.AddCookie(issuedSessionCookie(t, issueRecorder))
		if _, ok := auth.Authenticate(httptest.NewRecorder(), request); ok {
			t.Fatalf("Authorization %q with a valid cookie was accepted", strings.Fields(header)[0])
		}
	}
}

func TestAPITokenRejectedWhenExpiredRevokedOrNoLongerAllowed(t *testing.T) {
	auth, userID := newAPITokenTestUser(t)
	expired, _ := mustCreateAPIToken(t, auth)
	if _, err := auth.db.Exec(`UPDATE api_tokens SET expires_at = ?`, time.Now().Add(-time.Minute).Unix()); err != nil {
		t.Fatal(err)
	}
	if _, ok := auth.Authenticate(httptest.NewRecorder(), bearerRequest("Bearer "+expired)); ok {
		t.Fatal("expired token was accepted")
	}

	revoked, record := mustCreateAPIToken(t, auth)
	if err := auth.RevokeAPIToken("allowed@example.com", record.ID); err != nil {
		t.Fatalf("RevokeAPIToken: %v", err)
	}
	if _, ok := auth.Authenticate(httptest.NewRecorder(), bearerRequest("Bearer "+revoked)); ok {
		t.Fatal("revoked token was accepted")
	}

	delisted, _ := mustCreateAPIToken(t, auth)
	auth.cfg.AuthAllowedEmails = []string{"someone-else@example.com"}
	if gotUserID, ok := auth.Authenticate(httptest.NewRecorder(), bearerRequest("Bearer "+delisted)); ok {
		t.Fatalf("token for a removed account authenticated as %q (owner %q)", gotUserID, userID)
	}
}

func TestSignOutAllSessionsRevokesAPITokens(t *testing.T) {
	auth, userID := newAPITokenTestUser(t)
	token, _ := mustCreateAPIToken(t, auth)
	if err := auth.SignOutAllSessions(userID); err != nil {
		t.Fatalf("SignOutAllSessions: %v", err)
	}
	if _, ok := auth.Authenticate(httptest.NewRecorder(), bearerRequest("Bearer "+token)); ok {
		t.Fatal("API token remained valid after SignOutAllSessions")
	}
}

func TestCreateAPITokenValidatesRequest(t *testing.T) {
	auth, _ := newAPITokenTestUser(t)
	tests := []struct {
		name     string
		email    string
		label    string
		lifetime time.Duration
		want     error
	}{
		{"zero lifetime", "allowed@example.com", "", 0, ErrInvalidAPITokenRequest},
		{"lifetime too long", "allowed@example.com", "", MaxAPITokenLifetime + time.Hour, ErrInvalidAPITokenRequest},
		{"name too long", "allowed@example.com", strings.Repeat("n", maxAPITokenNameLength+1), time.Hour, ErrInvalidAPITokenRequest},
		{"email not allowed", "stranger@example.com", "", time.Hour, ErrEmailNotAllowed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := auth.CreateAPIToken(test.email, test.label, test.lifetime); !errors.Is(err, test.want) {
				t.Fatalf("CreateAPIToken error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestAPITokenCommandsRequireAnExistingAccount(t *testing.T) {
	auth, _ := newAuthTestService(t)
	if _, _, err := auth.CreateAPIToken("allowed@example.com", "", time.Hour); !errors.Is(err, ErrAPITokenUserNotFound) {
		t.Fatalf("CreateAPIToken error = %v, want ErrAPITokenUserNotFound", err)
	}
	if _, err := auth.ListAPITokens("stranger@example.com"); !errors.Is(err, ErrEmailNotAllowed) {
		t.Fatalf("ListAPITokens error = %v, want ErrEmailNotAllowed", err)
	}
	if err := auth.RevokeAPIToken("stranger@example.com", "id"); !errors.Is(err, ErrEmailNotAllowed) {
		t.Fatalf("RevokeAPIToken error = %v, want ErrEmailNotAllowed", err)
	}
}

func TestRevokeAPITokenOnlyRemovesTheCallersToken(t *testing.T) {
	auth, _ := newAPITokenTestUser(t)
	if err := auth.RevokeAPIToken("allowed@example.com", "missing"); !errors.Is(err, ErrAPITokenNotFound) {
		t.Fatalf("RevokeAPIToken error = %v, want ErrAPITokenNotFound", err)
	}
}

func TestCreateAPITokenSurfacesRandomReadFailure(t *testing.T) {
	auth, _ := newAPITokenTestUser(t)
	original := rand.Reader
	rand.Reader = bytes.NewReader(nil)
	defer func() { rand.Reader = original }()
	if _, _, err := auth.CreateAPIToken("allowed@example.com", "", time.Hour); err == nil {
		t.Fatal("CreateAPIToken succeeded despite a failed random read")
	}
}

func TestAPITokenStorageFailuresAreReported(t *testing.T) {
	auth, _ := newAPITokenTestUser(t)
	token, _ := mustCreateAPIToken(t, auth)
	if _, err := auth.db.Exec(`DROP TABLE api_tokens`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := auth.CreateAPIToken("allowed@example.com", "", time.Hour); err == nil {
		t.Fatal("CreateAPIToken ignored a storage failure")
	}
	if _, err := auth.ListAPITokens("allowed@example.com"); err == nil {
		t.Fatal("ListAPITokens ignored a storage failure")
	}
	if err := auth.RevokeAPIToken("allowed@example.com", "id"); err == nil {
		t.Fatal("RevokeAPIToken ignored a storage failure")
	}
	if _, ok := auth.Authenticate(httptest.NewRecorder(), bearerRequest("Bearer "+token)); ok {
		t.Fatal("Authenticate accepted a token after a lookup failure")
	}
	if err := auth.SignOutAllSessions("user"); err == nil {
		t.Fatal("SignOutAllSessions ignored a token deletion failure")
	}
}

func TestAPITokenAuthenticationTagsTheRequestLog(t *testing.T) {
	auth, _ := newAPITokenTestUser(t)
	token, _ := mustCreateAPIToken(t, auth)
	logged := logging.NewRequest()
	request := bearerRequest("Bearer " + token)
	request = request.WithContext(logging.WithRequest(request.Context(), logged))
	if _, ok := auth.Authenticate(httptest.NewRecorder(), request); !ok {
		t.Fatal("token was rejected")
	}
	if !slices.ContainsFunc(logged.Attrs(), func(attr slog.Attr) bool { return attr.Key == "auth" && attr.Value.String() == "api_token" }) {
		t.Fatalf("request log attrs = %v, want auth=api_token", logged.Attrs())
	}
}

func TestListAPITokensReportsUnreadableRows(t *testing.T) {
	auth, userID := newAPITokenTestUser(t)
	if _, err := auth.db.Exec(`INSERT INTO api_tokens (id, user_id, name, token_hash, created_at, expires_at) VALUES ('bad', ?, '', 'hash', 'not-a-time', 0)`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.ListAPITokens("allowed@example.com"); err == nil {
		t.Fatal("ListAPITokens returned a row it could not read")
	}
}

func TestAPITokenCommandsReportAccountLookupFailures(t *testing.T) {
	auth, _ := newAPITokenTestUser(t)
	if _, err := auth.db.Exec(`ALTER TABLE users RENAME TO users_unavailable`); err != nil {
		t.Fatal(err)
	}
	_, err := auth.ListAPITokens("allowed@example.com")
	if err == nil || errors.Is(err, ErrAPITokenUserNotFound) || errors.Is(err, ErrEmailNotAllowed) {
		t.Fatalf("ListAPITokens error = %v, want the lookup failure", err)
	}
}
