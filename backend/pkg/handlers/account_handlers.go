package handlers

import (
	"errors"
	"net/http"

	"war-room/backend/pkg/service"
)

const maxAvatarUploadBytes = 5<<20 + 1024 // 5 MiB plus a little multipart overhead slack

func (h *Handler) handleUploadAvatar(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authUserID(r)
	if !ok || h.auth == nil {
		ErrorJSON(w, http.StatusUnauthorized, "sign in required")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAvatarUploadBytes)
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		status := http.StatusBadRequest
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			status = http.StatusRequestEntityTooLarge
		}
		ErrorJSON(w, status, "Choose an image no larger than 5 MiB")
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()
	file, header, err := r.FormFile("file")
	if err != nil {
		ErrorJSON(w, http.StatusBadRequest, "Choose an image to upload")
		return
	}
	defer func() { _ = file.Close() }()
	if header.Size == 0 {
		ErrorJSON(w, http.StatusBadRequest, "Image cannot be empty")
		return
	}
	user, err := h.auth.SaveAvatar(userID, file)
	if err != nil {
		if errors.Is(err, service.ErrInvalidAvatarImage) {
			ErrorJSON(w, http.StatusBadRequest, "Choose a PNG or JPEG image")
			return
		}
		ErrorJSON(w, http.StatusInternalServerError, "Failed to save profile picture")
		return
	}
	JSON(w, http.StatusOK, user)
}

// handleGetAvatar serves the signed-in user's stored avatar bytes directly.
// The path is built server-side by AuthService.AvatarFile from userID and a
// decode-validated extension -- never from request input -- so no
// path-safety guard (unlike handleDownloadCVVersion's filepath.Base check on
// a DB-sourced filename) is needed here.
func (h *Handler) handleGetAvatar(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authUserID(r)
	if !ok || h.auth == nil {
		ErrorJSON(w, http.StatusUnauthorized, "sign in required")
		return
	}
	path, mimeType, err := h.auth.AvatarFile(userID)
	if err != nil {
		ErrorJSON(w, http.StatusNotFound, "No custom profile picture set")
		return
	}
	w.Header().Set("Content-Type", mimeType)
	// private (not public): this is per-user content behind a session cookie.
	// The ?v= query param already busts the cache on change, so a long
	// max-age is safe and correct, not just permissible.
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeFile(w, r, path) // #nosec G304 -- path is server-built; see the AuthService.AvatarFile comment.
}

func (h *Handler) handleDeleteAvatar(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authUserID(r)
	if !ok || h.auth == nil {
		ErrorJSON(w, http.StatusUnauthorized, "sign in required")
		return
	}
	user, err := h.auth.RemoveAvatar(userID)
	if err != nil {
		ErrorJSON(w, http.StatusInternalServerError, "Failed to remove profile picture")
		return
	}
	JSON(w, http.StatusOK, user)
}
