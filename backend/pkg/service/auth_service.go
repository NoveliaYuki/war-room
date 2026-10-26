package service

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"war-room/backend/pkg/config"
	"war-room/backend/pkg/models"
	"war-room/backend/pkg/repository"
)

const (
	sessionCookieName = "warroom_session"
	// A session slides forward on use rather than expiring on a fixed
	// schedule: an actively-used session never needs re-authentication, and
	// an abandoned one dies within sessionLifetime of its last use.
	sessionLifetime         = 30 * 24 * time.Hour
	sessionRenewalThreshold = 15 * 24 * time.Hour
)

// ErrEmailNotAllowed indicates the signed-in Google account is not on the
// AUTH_ALLOWED_EMAILS allowlist, or Google reported its email as unverified.
var ErrEmailNotAllowed = errors.New("this google account is not authorized for war room")

// GoogleIdentity is the identity claims read from Google's userinfo endpoint.
type GoogleIdentity struct {
	Sub           string
	Email         string
	EmailVerified bool
	Name          string
	Picture       string
}

// AuthService manages user identity and sessions, backed by the users and
// sessions tables in the shared application database.
type AuthService struct {
	db  *sql.DB
	cfg *config.Config
}

// NewAuthService creates the service.
func NewAuthService(db *sql.DB, cfg *config.Config) *AuthService {
	return &AuthService{db: db, cfg: cfg}
}

func (a *AuthService) emailAllowed(email string) bool {
	for _, allowed := range a.cfg.AuthAllowedEmails {
		if allowed == email {
			return true
		}
	}
	return false
}

// UpsertUserFromGoogle enforces the allowlist and email_verified, then
// creates or updates the user row keyed by Google's stable subject ID
// (google_sub survives an email change; email alone would not). The very
// first user ever created in this install adopts every pre-account
// (user_id IS NULL) row in the same transaction. A rejection is logged with
// the attempted email, verification state, and remote address, and creates
// no user and no session.
func (a *AuthService) UpsertUserFromGoogle(identity GoogleIdentity, remoteAddr string) (*models.User, error) {
	email := strings.ToLower(strings.TrimSpace(identity.Email))
	if !identity.EmailVerified || !a.emailAllowed(email) {
		log.Printf("rejected google sign-in attempt: email=%q verified=%t remote=%s", email, identity.EmailVerified, remoteAddr)
		return nil, ErrEmailNotAllowed
	}
	return a.upsertUserAndClaimIfFirst(identity, email)
}

// CreateTestSession upserts a user directly, bypassing Google and the
// allowlist entirely, and issues a session for them. It exists only to let
// the Go-Playwright e2e suite authenticate without a real OAuth round-trip.
// Callers MUST gate this behind cfg.TestMode: config.Load already refuses to
// start with TestMode enabled alongside a Secure session cookie, which is
// what keeps this from being reachable in a real deployment.
func (a *AuthService) CreateTestSession(w http.ResponseWriter, email string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return errors.New("test session requires an email")
	}
	identity := GoogleIdentity{Sub: "test-session:" + email, Email: email, EmailVerified: true, Name: email}
	user, err := a.upsertUserAndClaimIfFirst(identity, email)
	if err != nil {
		return err
	}
	return a.IssueSession(w, user.ID)
}

func (a *AuthService) upsertUserAndClaimIfFirst(identity GoogleIdentity, email string) (*models.User, error) {
	if identity.Sub == "" {
		return nil, errors.New("google identity is missing a subject id")
	}
	tx, err := a.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var userCountBefore int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&userCountBefore); err != nil {
		return nil, fmt.Errorf("count existing users: %w", err)
	}

	user, err := upsertUserRow(tx, identity, email)
	if err != nil {
		return nil, err
	}

	// Pre-account data is adopted only in a single-account deployment; with
	// several allowed accounts there is no way to tell whose it is.
	if userCountBefore == 0 && len(a.cfg.AuthAllowedEmails) == 1 {
		if err := repository.ClaimUnownedData(tx, user.ID); err != nil {
			return nil, fmt.Errorf("adopt pre-account data: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return user, nil
}

func upsertUserRow(tx *sql.Tx, identity GoogleIdentity, email string) (*models.User, error) {
	var existingID string
	err := tx.QueryRow(`SELECT id FROM users WHERE google_sub = ?`, identity.Sub).Scan(&existingID)
	switch {
	case err == nil:
		if _, err := tx.Exec(`UPDATE users SET email = ?, name = ?, picture_url = ?, updated_at = unixepoch() WHERE id = ?`,
			email, identity.Name, identity.Picture, existingID); err != nil {
			return nil, fmt.Errorf("update user: %w", err)
		}
		var createdAt int64
		if err := tx.QueryRow(`SELECT created_at FROM users WHERE id = ?`, existingID).Scan(&createdAt); err != nil {
			return nil, fmt.Errorf("read updated user: %w", err)
		}
		return &models.User{ID: existingID, GoogleSub: identity.Sub, Email: email, Name: identity.Name, PictureURL: identity.Picture, CreatedAt: createdAt}, nil
	case errors.Is(err, sql.ErrNoRows):
		id := uuid.NewString()
		now := time.Now().Unix()
		if _, err := tx.Exec(`INSERT INTO users (id, google_sub, email, name, picture_url, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			id, identity.Sub, email, identity.Name, identity.Picture, now, now); err != nil {
			return nil, fmt.Errorf("create user: %w", err)
		}
		return &models.User{ID: id, GoogleSub: identity.Sub, Email: email, Name: identity.Name, PictureURL: identity.Picture, CreatedAt: now}, nil
	default:
		return nil, fmt.Errorf("look up user: %w", err)
	}
}

// IssueSession creates a new session row and sets the session cookie. Call
// this once per successful sign-in; it does not delete any other session
// the user may already have open on another device.
func (a *AuthService) IssueSession(w http.ResponseWriter, userID string) error {
	token, err := randomSessionToken()
	if err != nil {
		return err
	}
	_, _ = a.db.Exec(`DELETE FROM sessions WHERE expires_at < ?`, time.Now().Unix()) // purge abandoned sessions
	expiresAt := time.Now().Add(sessionLifetime)
	if _, err := a.db.Exec(`INSERT INTO sessions (id, user_id, token_hash, expires_at) VALUES (?, ?, ?, ?)`,
		uuid.NewString(), userID, hashSessionToken(token), expiresAt.Unix()); err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	a.setSessionCookie(w, token, expiresAt)
	return nil
}

// Authenticate implements middleware.SessionAuthenticator. An expired or
// unrecognized cookie is treated as signed-out; a session nearing expiry is
// silently extended and the cookie re-issued (the sliding-expiry policy).
func (a *AuthService) Authenticate(w http.ResponseWriter, r *http.Request) (string, bool) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return "", false
	}
	tokenHash := hashSessionToken(cookie.Value)
	var userID string
	var expiresAtUnix int64
	err = a.db.QueryRow(`SELECT user_id, expires_at FROM sessions WHERE token_hash = ?`, tokenHash).Scan(&userID, &expiresAtUnix)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false
	}
	if err != nil {
		log.Printf("session lookup failed: %v", err)
		return "", false
	}
	expiresAt := time.Unix(expiresAtUnix, 0)
	if time.Now().After(expiresAt) {
		_, _ = a.db.Exec(`DELETE FROM sessions WHERE token_hash = ?`, tokenHash)
		return "", false
	}
	if time.Until(expiresAt) < sessionRenewalThreshold {
		renewedExpiry := time.Now().Add(sessionLifetime)
		if _, err := a.db.Exec(`UPDATE sessions SET expires_at = ? WHERE token_hash = ?`, renewedExpiry.Unix(), tokenHash); err == nil {
			a.setSessionCookie(w, cookie.Value, renewedExpiry)
		}
	}
	return userID, true
}

// SignOut deletes only the session named by the request's cookie (this
// device/browser), leaving any other active sessions and the Google Drive
// grant untouched. Signing out of your identity is not the same as revoking
// Google Drive access.
func (a *AuthService) SignOut(w http.ResponseWriter, r *http.Request) error {
	cookie, err := r.Cookie(sessionCookieName)
	if err == nil && cookie.Value != "" {
		if _, err := a.db.Exec(`DELETE FROM sessions WHERE token_hash = ?`, hashSessionToken(cookie.Value)); err != nil {
			return fmt.Errorf("delete session: %w", err)
		}
	}
	a.clearSessionCookie(w)
	return nil
}

// SignOutAllSessions deletes every session for userID, across every device.
// Revoking Google Drive access calls this too, because identity and the
// Drive grant are now the same OAuth consent.
func (a *AuthService) SignOutAllSessions(userID string) error {
	_, err := a.db.Exec(`DELETE FROM sessions WHERE user_id = ?`, userID)
	return err
}

// ClearSessionCookie removes the session cookie from the response without
// touching any session row. Used after revocation, where SignOutAllSessions
// already deleted the rows.
func (a *AuthService) ClearSessionCookie(w http.ResponseWriter) {
	a.clearSessionCookie(w)
}

// DatabaseForTesting exposes the underlying database so other packages'
// tests can inject failures (e.g. dropping a table) to exercise error paths.
func (a *AuthService) DatabaseForTesting() *sql.DB { return a.db }

// GetUser returns the signed-in user's public profile for GET /api/auth/me.
// AvatarURL is set only when a custom avatar exists; the frontend falls back
// to PictureURL (Google's photo), then to a fallback initial, when it is empty.
func (a *AuthService) GetUser(userID string) (*models.User, error) {
	var user models.User
	var avatarPath string
	var avatarUpdatedAt int64
	err := a.db.QueryRow(`SELECT id, email, name, picture_url, created_at, avatar_path, avatar_updated_at FROM users WHERE id = ?`, userID).
		Scan(&user.ID, &user.Email, &user.Name, &user.PictureURL, &user.CreatedAt, &avatarPath, &avatarUpdatedAt)
	if err != nil {
		return nil, err
	}
	if avatarPath != "" {
		user.AvatarURL = fmt.Sprintf("/api/account/avatar?v=%d", avatarUpdatedAt)
	}
	return &user, nil
}

func (a *AuthService) setSessionCookie(w http.ResponseWriter, token string, expiresAt time.Time) {
	// #nosec G124 -- Secure is cfg.SessionCookieSecure (true in every real deployment;
	// false only for local plain-HTTP testing, which config.Load refuses to combine
	// with test-session minting), so gosec cannot verify it statically.
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Value: token, Path: "/", HttpOnly: true,
		Secure: a.cfg.SessionCookieSecure, SameSite: http.SameSiteLaxMode, Expires: expiresAt,
	})
}

func (a *AuthService) clearSessionCookie(w http.ResponseWriter) {
	// #nosec G124 -- see the Secure-flag note in setSessionCookie above.
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Value: "", Path: "/", HttpOnly: true,
		Secure: a.cfg.SessionCookieSecure, SameSite: http.SameSiteLaxMode, MaxAge: -1,
	})
}

func randomSessionToken() (string, error) {
	raw := make([]byte, 32)
	// io.ReadFull(rand.Reader, ...) rather than the rand.Read convenience
	// function: the latter terminates the process on a read failure instead
	// of returning an error, which would crash the whole server over one
	// failed session issuance.
	if _, err := io.ReadFull(rand.Reader, raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func hashSessionToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
