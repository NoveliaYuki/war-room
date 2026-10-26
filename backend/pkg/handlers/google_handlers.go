package handlers

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"war-room/backend/pkg/middleware"
	"war-room/backend/pkg/service"
)

const oauthStateCookieName = "warroom_google_oauth_state"

func (h *Handler) driveOrError(w http.ResponseWriter) bool {
	if h.drive != nil {
		return true
	}
	ErrorJSON(w, http.StatusServiceUnavailable, "Google Drive backup is unavailable")
	return false
}

func (h *Handler) authUserID(r *http.Request) (string, bool) {
	return middleware.UserIDFromContext(r.Context())
}

func (h *Handler) handleGoogleStatus(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authUserID(r)
	if !ok {
		ErrorJSON(w, http.StatusUnauthorized, "sign in required")
		return
	}
	if h.drive == nil {
		JSON(w, http.StatusOK, service.DriveStatus{})
		return
	}
	JSON(w, http.StatusOK, h.drive.Status(userID))
}

// handleGoogleConnect starts the combined sign-in-and-Drive-grant flow. It
// requires no session: successfully completing it is how a session gets
// created in the first place.
func (h *Handler) handleGoogleConnect(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !h.validGoogleRequestOrigin(r) {
		ErrorJSON(w, http.StatusForbidden, "Request origin is not allowed")
		return
	}
	if !h.driveOrError(w) {
		return
	}
	authURL, err := h.drive.AuthURL()
	if err != nil {
		googleError(w, err)
		return
	}
	parsed, err := url.Parse(authURL)
	if err != nil {
		ErrorJSON(w, http.StatusInternalServerError, "Could not start Google sign-in")
		return
	}
	state := parsed.Query().Get("state")
	if state == "" {
		ErrorJSON(w, http.StatusInternalServerError, "Could not start Google sign-in")
		return
	}
	// #nosec G124 -- Secure is cfg.SessionCookieSecure (true in every real deployment;
	// false only for local plain-HTTP testing, which config.Load refuses to combine
	// with test-session minting), so gosec cannot verify it statically.
	http.SetCookie(w, &http.Cookie{Name: oauthStateCookieName, Value: state, Path: "/api/google/callback", MaxAge: 600, HttpOnly: true, Secure: h.cfg.SessionCookieSecure, SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, authURL, http.StatusFound)
}

// handleGoogleCallback completes the combined flow: exchanging the OAuth
// code for identity and Drive credentials, enforcing the sign-in allowlist,
// and — only on success — saving the Drive credentials and issuing a
// session. A rejected or failed attempt creates no user and no session.
func (h *Handler) handleGoogleCallback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	cookie, cookieErr := r.Cookie(oauthStateCookieName)
	// #nosec G124 -- see the Secure-flag note on the matching cookie in handleGoogleConnect above.
	http.SetCookie(w, &http.Cookie{Name: oauthStateCookieName, Value: "", Path: "/api/google/callback", MaxAge: -1, HttpOnly: true, Secure: h.cfg.SessionCookieSecure, SameSite: http.SameSiteLaxMode})

	target := h.completeGoogleCallback(w, r, cookie, cookieErr)
	http.Redirect(w, r, target, http.StatusFound)
}

func (h *Handler) completeGoogleCallback(w http.ResponseWriter, r *http.Request, cookie *http.Cookie, cookieErr error) string {
	frontendURL := strings.TrimSuffix(h.cfg.GoogleFrontendURL, "/")
	state := r.URL.Query().Get("state")
	if cookieErr != nil || cookie.Value == "" || cookie.Value != state || h.drive == nil || h.auth == nil {
		return frontendURL + "/?google=error"
	}
	info, token, err := h.drive.ExchangeCode(r.URL.Query().Get("code"), state)
	if err != nil {
		return frontendURL + "/?google=error"
	}
	user, err := h.auth.UpsertUserFromGoogle(service.GoogleIdentity(info), r.RemoteAddr)
	if err != nil {
		return frontendURL + "/?google=error"
	}
	if err := h.drive.SaveAccount(user.ID, token); err != nil {
		return frontendURL + "/?google=error"
	}
	if err := h.auth.IssueSession(w, user.ID); err != nil {
		return frontendURL + "/?google=error"
	}
	return frontendURL + "/?google=connected"
}

// handleAuthMe returns the signed-in user's public profile. A 401 here
// (produced by the RequireSession middleware) is the frontend's signal to
// show the sign-in screen instead of the board.
func (h *Handler) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authUserID(r)
	if !ok || h.auth == nil {
		ErrorJSON(w, http.StatusUnauthorized, "sign in required")
		return
	}
	user, err := h.auth.GetUser(userID)
	if err != nil {
		ErrorJSON(w, http.StatusUnauthorized, "sign in required")
		return
	}
	JSON(w, http.StatusOK, user)
}

// handleSignOut ends only this session (this device/browser). It does not
// touch the Google Drive grant or any other session the account may have
// open elsewhere.
func (h *Handler) handleSignOut(w http.ResponseWriter, r *http.Request) {
	if h.auth == nil {
		ErrorJSON(w, http.StatusServiceUnavailable, "sign-in is unavailable")
		return
	}
	if err := h.auth.SignOut(w, r); err != nil {
		ErrorJSON(w, http.StatusInternalServerError, "Could not sign out")
		return
	}
	JSON(w, http.StatusOK, map[string]bool{"success": true})
}

// handleGoogleRevoke revokes the Google Drive grant and ends every session
// for the account: identity and Drive are the same OAuth consent now, so
// revoking one must revoke both.
func (h *Handler) handleGoogleRevoke(w http.ResponseWriter, r *http.Request) {
	if !h.validGoogleRequestOrigin(r) {
		ErrorJSON(w, http.StatusForbidden, "Request origin is not allowed")
		return
	}
	if !h.driveOrError(w) {
		return
	}
	userID, ok := h.authUserID(r)
	if !ok || h.auth == nil {
		ErrorJSON(w, http.StatusUnauthorized, "sign in required")
		return
	}
	if err := h.drive.Revoke(userID); err != nil {
		if errors.Is(err, service.ErrGoogleRevocationFailed) {
			_ = h.auth.SignOutAllSessions(userID)
			h.auth.ClearSessionCookie(w)
			googleError(w, err)
			return
		}
		ErrorJSON(w, http.StatusInternalServerError, "Could not revoke Google Drive access")
		return
	}
	if err := h.auth.SignOutAllSessions(userID); err != nil {
		ErrorJSON(w, http.StatusInternalServerError, "Could not sign out")
		return
	}
	h.auth.ClearSessionCookie(w)
	JSON(w, http.StatusOK, map[string]bool{"success": true})
}

func (h *Handler) handleGoogleFolders(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authUserID(r)
	if !ok {
		ErrorJSON(w, http.StatusUnauthorized, "sign in required")
		return
	}
	if !h.driveOrError(w) {
		return
	}
	folders, err := h.drive.ListFolders(userID, r.URL.Query().Get("q"))
	if err != nil {
		googleError(w, err)
		return
	}
	JSON(w, http.StatusOK, folders)
}

func (h *Handler) handleGooglePickerToken(w http.ResponseWriter, r *http.Request) {
	if !h.validGoogleRequestOrigin(r) {
		ErrorJSON(w, http.StatusForbidden, "Request origin is not allowed")
		return
	}
	userID, ok := h.authUserID(r)
	if !ok {
		ErrorJSON(w, http.StatusUnauthorized, "sign in required")
		return
	}
	if !h.driveOrError(w) {
		return
	}
	token, err := h.drive.PickerAccessToken(userID)
	if err != nil {
		googleError(w, err)
		return
	}
	JSON(w, http.StatusOK, map[string]string{"access_token": token})
}

func (h *Handler) handleGoogleSetFolder(w http.ResponseWriter, r *http.Request) {
	if !h.validGoogleRequestOrigin(r) {
		ErrorJSON(w, http.StatusForbidden, "Request origin is not allowed")
		return
	}
	userID, ok := h.authUserID(r)
	if !ok {
		ErrorJSON(w, http.StatusUnauthorized, "sign in required")
		return
	}
	if !h.driveOrError(w) {
		return
	}
	var input struct {
		FolderID   string `json:"folder_id"`
		FolderName string `json:"folder_name"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	if err := decodeJSONBody(r.Body, &input); err != nil {
		ErrorJSON(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}
	if err := h.drive.SetFolder(userID, input.FolderID, input.FolderName); err != nil {
		googleError(w, err)
		return
	}
	JSON(w, http.StatusOK, h.drive.Status(userID))
}

func (h *Handler) handleGoogleBackupNow(w http.ResponseWriter, r *http.Request) {
	if !h.validGoogleRequestOrigin(r) {
		ErrorJSON(w, http.StatusForbidden, "Request origin is not allowed")
		return
	}
	userID, ok := h.authUserID(r)
	if !ok {
		ErrorJSON(w, http.StatusUnauthorized, "sign in required")
		return
	}
	if !h.driveOrError(w) {
		return
	}
	if err := h.drive.BackupNow(userID); err != nil {
		googleError(w, err)
		return
	}
	JSON(w, http.StatusOK, h.drive.Status(userID))
}

// handleTestSession mints a session for the Go-Playwright e2e suite without
// a real Google round-trip, bypassing the allowlist entirely. It is
// registered only when cfg.TestMode is set, which config.Load already
// refuses to combine with a Secure session cookie, and cfg.TestMode is
// never set outside the isolated test-backend compose service.
func (h *Handler) handleTestSession(w http.ResponseWriter, r *http.Request) {
	if h.auth == nil {
		ErrorJSON(w, http.StatusServiceUnavailable, "sign-in is unavailable")
		return
	}
	var input struct {
		Email string `json:"email"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	_ = decodeJSONBody(r.Body, &input)
	email := input.Email
	if email == "" && len(h.cfg.AuthAllowedEmails) > 0 {
		email = h.cfg.AuthAllowedEmails[0]
	}
	if err := h.auth.CreateTestSession(w, email); err != nil {
		internalError(w, err)
		return
	}
	JSON(w, http.StatusOK, map[string]bool{"success": true})
}

func (h *Handler) validGoogleRequestOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return r.Header.Get("Sec-Fetch-Site") != "cross-site"
	}
	frontend, err := url.Parse(h.cfg.GoogleFrontendURL)
	if err != nil || frontend.Scheme == "" || frontend.Host == "" {
		return false
	}
	return origin == frontend.Scheme+"://"+frontend.Host
}

func googleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrGoogleNotConfigured):
		ErrorJSON(w, http.StatusServiceUnavailable, err.Error())
	case errors.Is(err, service.ErrGoogleNotConnected):
		ErrorJSON(w, http.StatusConflict, err.Error())
	case errors.Is(err, service.ErrGoogleRevocationFailed):
		ErrorJSON(w, http.StatusBadGateway, err.Error())
	default:
		ErrorJSON(w, http.StatusBadGateway, err.Error())
	}
}
