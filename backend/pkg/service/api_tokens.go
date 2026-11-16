package service

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"war-room/backend/pkg/logging"
)

const (
	// apiTokenPrefix makes tokens recognizable to secret scanners and lets
	// Authenticate reject malformed bearer values without a database lookup.
	apiTokenPrefix        = "wr_"
	maxAPITokenNameLength = 100

	// DefaultAPITokenLifetime is used when the issuer does not choose one.
	DefaultAPITokenLifetime = 90 * 24 * time.Hour
	// MaxAPITokenLifetime bounds every token; none is issued without expiry.
	MaxAPITokenLifetime = 365 * 24 * time.Hour
)

var (
	// ErrAPITokenUserNotFound means the allowlisted email has never signed in,
	// so there is no account for a token to act as.
	ErrAPITokenUserNotFound = errors.New("no account exists for this email; sign in with Google once first")
	// ErrAPITokenNotFound means no token with that ID belongs to the user.
	ErrAPITokenNotFound = errors.New("api token not found")
	// ErrInvalidAPITokenRequest describes an unusable lifetime or name.
	ErrInvalidAPITokenRequest = errors.New("api token lifetime must be between 1 and 365 days and its name at most 100 characters")
)

// APIToken describes an issued token. The secret itself is never stored.
type APIToken struct {
	ID         string
	Name       string
	CreatedAt  time.Time
	LastUsedAt *time.Time
	ExpiresAt  time.Time
}

// CreateAPIToken issues a personal API token for the allowlisted account with
// email. The returned secret is shown once; only its hash is persisted.
func (a *AuthService) CreateAPIToken(email, name string, lifetime time.Duration) (string, APIToken, error) {
	name = strings.TrimSpace(name)
	if lifetime <= 0 || lifetime > MaxAPITokenLifetime || len(name) > maxAPITokenNameLength {
		return "", APIToken{}, ErrInvalidAPITokenRequest
	}
	userID, err := a.allowedUserIDByEmail(email)
	if err != nil {
		return "", APIToken{}, err
	}
	secret, err := randomSessionToken()
	if err != nil {
		return "", APIToken{}, fmt.Errorf("generate api token: %w", err)
	}
	token := apiTokenPrefix + secret
	now := time.Now()
	record := APIToken{ID: uuid.NewString(), Name: name, CreatedAt: now, ExpiresAt: now.Add(lifetime)}
	if _, err := a.db.Exec(`INSERT INTO api_tokens (id, user_id, name, token_hash, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?)`,
		record.ID, userID, record.Name, hashSessionToken(token), now.Unix(), record.ExpiresAt.Unix()); err != nil {
		return "", APIToken{}, fmt.Errorf("store api token: %w", err)
	}
	return token, record, nil
}

// ListAPITokens returns the tokens issued to the account with email, newest first.
func (a *AuthService) ListAPITokens(email string) ([]APIToken, error) {
	userID, err := a.allowedUserIDByEmail(email)
	if err != nil {
		return nil, err
	}
	rows, err := a.db.Query(`SELECT id, name, created_at, last_used_at, expires_at FROM api_tokens WHERE user_id = ? ORDER BY created_at DESC, id`, userID)
	if err != nil {
		return nil, fmt.Errorf("list api tokens: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var tokens []APIToken
	for rows.Next() {
		var token APIToken
		var createdAt, expiresAt int64
		var lastUsedAt sql.NullInt64
		if err := rows.Scan(&token.ID, &token.Name, &createdAt, &lastUsedAt, &expiresAt); err != nil {
			return nil, fmt.Errorf("read api token: %w", err)
		}
		token.CreatedAt, token.ExpiresAt = time.Unix(createdAt, 0), time.Unix(expiresAt, 0)
		if lastUsedAt.Valid {
			used := time.Unix(lastUsedAt.Int64, 0)
			token.LastUsedAt = &used
		}
		tokens = append(tokens, token)
	}
	return tokens, rows.Err()
}

// RevokeAPIToken deletes one token belonging to the account with email.
func (a *AuthService) RevokeAPIToken(email, id string) error {
	userID, err := a.allowedUserIDByEmail(email)
	if err != nil {
		return err
	}
	result, err := a.db.Exec(`DELETE FROM api_tokens WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return fmt.Errorf("revoke api token: %w", err)
	}
	if count, err := result.RowsAffected(); err != nil || count == 0 {
		return ErrAPITokenNotFound
	}
	return nil
}

func (a *AuthService) allowedUserIDByEmail(email string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if !a.emailAllowed(email) {
		return "", ErrEmailNotAllowed
	}
	var userID string
	err := a.db.QueryRow(`SELECT id FROM users WHERE email = ?`, email).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrAPITokenUserNotFound
	}
	if err != nil {
		return "", fmt.Errorf("find user: %w", err)
	}
	return userID, nil
}

// authenticateAPIToken resolves an Authorization header to its owner. The
// allowlist is re-checked on every request so removing an email from
// AUTH_ALLOWED_EMAILS cuts off its tokens without revoking them one by one.
func (a *AuthService) authenticateAPIToken(r *http.Request, header string) (string, bool) {
	token, found := strings.CutPrefix(header, "Bearer ")
	if !found || !strings.HasPrefix(token, apiTokenPrefix) {
		logging.AuditFailure(r.Context(), "api_token.denied", "reason", "malformed authorization header")
		return "", false
	}
	tokenHash := hashSessionToken(token)
	var userID, email string
	var expiresAt int64
	err := a.db.QueryRow(`SELECT t.user_id, u.email, t.expires_at FROM api_tokens t JOIN users u ON u.id = t.user_id WHERE t.token_hash = ?`,
		tokenHash).Scan(&userID, &email, &expiresAt)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			slog.Error("api token lookup failed", "error", err.Error())
		}
		logging.AuditFailure(r.Context(), "api_token.denied", "reason", "unknown token")
		return "", false
	}
	if reason := a.apiTokenDenialReason(email, expiresAt); reason != "" {
		logging.AuditFailure(r.Context(), "api_token.denied", "reason", reason, "user_id", userID)
		return "", false
	}
	_, _ = a.db.Exec(`UPDATE api_tokens SET last_used_at = ? WHERE token_hash = ?`, time.Now().Unix(), tokenHash)
	if req := logging.RequestFrom(r.Context()); req != nil {
		req.AddAttrs(slog.String("auth", "api_token"))
	}
	return userID, true
}

func (a *AuthService) apiTokenDenialReason(email string, expiresAt int64) string {
	if time.Now().Unix() >= expiresAt {
		return "expired token"
	}
	if !a.emailAllowed(email) {
		return "account no longer allowed"
	}
	return ""
}
