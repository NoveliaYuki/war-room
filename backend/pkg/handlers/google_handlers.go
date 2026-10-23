package handlers

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"war-room/backend/pkg/service"
)

func (h *Handler) driveOrError(w http.ResponseWriter) bool {
	if h.drive != nil {
		return true
	}
	ErrorJSON(w, http.StatusServiceUnavailable, "Google Drive backup is unavailable")
	return false
}

func (h *Handler) handleGoogleStatus(w http.ResponseWriter, _ *http.Request) {
	if h.drive == nil {
		JSON(w, http.StatusOK, service.DriveStatus{})
		return
	}
	JSON(w, http.StatusOK, h.drive.Status())
}

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
		ErrorJSON(w, http.StatusServiceUnavailable, err.Error())
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
	http.SetCookie(w, &http.Cookie{Name: "warroom_google_oauth_state", Value: state, Path: "/api/google/callback", MaxAge: 600, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, authURL, http.StatusFound)
}

func (h *Handler) handleGoogleCallback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	target := strings.TrimSuffix(h.cfg.GoogleFrontendURL, "/") + "/?google=connected"
	cookie, cookieErr := r.Cookie("warroom_google_oauth_state")
	state := r.URL.Query().Get("state")
	ok := cookieErr == nil && cookie.Value != "" && cookie.Value == state && h.drive != nil && h.drive.HandleCallback(r.URL.Query().Get("code"), state) == nil
	http.SetCookie(w, &http.Cookie{Name: "warroom_google_oauth_state", Value: "", Path: "/api/google/callback", MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	if !ok {
		target = strings.TrimSuffix(h.cfg.GoogleFrontendURL, "/") + "/?google=error"
	}
	http.Redirect(w, r, target, http.StatusFound)
}

func (h *Handler) handleGoogleDisconnect(w http.ResponseWriter, r *http.Request) {
	if !h.validGoogleRequestOrigin(r) {
		ErrorJSON(w, http.StatusForbidden, "Request origin is not allowed")
		return
	}
	if !h.driveOrError(w) {
		return
	}
	if err := h.drive.Disconnect(); err != nil {
		if errors.Is(err, service.ErrGoogleRevocationFailed) {
			googleError(w, err)
			return
		}
		ErrorJSON(w, http.StatusInternalServerError, "Could not disconnect Google account")
		return
	}
	JSON(w, http.StatusOK, map[string]bool{"success": true})
}

func (h *Handler) handleGoogleFolders(w http.ResponseWriter, r *http.Request) {
	if !h.driveOrError(w) {
		return
	}
	folders, err := h.drive.ListFolders(r.URL.Query().Get("q"))
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
	if !h.driveOrError(w) {
		return
	}
	token, err := h.drive.PickerAccessToken()
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
	if err := h.drive.SetFolder(input.FolderID, input.FolderName); err != nil {
		googleError(w, err)
		return
	}
	JSON(w, http.StatusOK, h.drive.Status())
}

func (h *Handler) handleGoogleBackupNow(w http.ResponseWriter, r *http.Request) {
	if !h.validGoogleRequestOrigin(r) {
		ErrorJSON(w, http.StatusForbidden, "Request origin is not allowed")
		return
	}
	if !h.driveOrError(w) {
		return
	}
	if err := h.drive.BackupNow(); err != nil {
		googleError(w, err)
		return
	}
	JSON(w, http.StatusOK, h.drive.Status())
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
