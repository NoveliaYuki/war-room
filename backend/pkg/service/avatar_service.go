package service

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // registers the JPEG decoder for image.DecodeConfig
	_ "image/png"  // registers the PNG decoder for image.DecodeConfig
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"war-room/backend/pkg/models"
)

// ErrInvalidAvatarImage indicates the uploaded file is not a decodable PNG or JPEG.
var ErrInvalidAvatarImage = errors.New("file is not a valid PNG or JPEG image")

const (
	maxAvatarUploadSize = 5 << 20 // 5 MiB, smaller than the 50 MiB CV cap -- these are profile photos
	maxAvatarDimension  = 8000    // guards against a tiny file claiming absurd pixel dimensions
)

var avatarUploadLock sync.Mutex

// SaveAvatar validates, stores, and records a custom profile photo for
// userID, replacing any previous one (including a prior one saved under a
// different extension, so no orphan file accumulates), and returns the
// updated profile.
func (a *AuthService) SaveAvatar(userID string, content io.Reader) (*models.User, error) {
	avatarUploadLock.Lock()
	defer avatarUploadLock.Unlock()

	tempPath, extension, err := stageAvatarUpload(a.cfg.AvatarsDir, content)
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.Remove(tempPath) }()

	oldPath, err := a.currentAvatarPath(userID)
	if err != nil {
		return nil, err
	}
	filename := userID + "." + extension
	destination := filepath.Join(a.cfg.AvatarsDir, filename)
	if err := os.Rename(tempPath, destination); err != nil {
		return nil, fmt.Errorf("install avatar file: %w", err)
	}
	now := time.Now().Unix()
	if _, err := a.db.Exec(`UPDATE users SET avatar_path = ?, avatar_updated_at = ? WHERE id = ?`, filename, now, userID); err != nil {
		if oldPath != filename {
			_ = os.Remove(destination) // same-name replacements keep the file: it is already a valid avatar
		}
		return nil, fmt.Errorf("save avatar: %w", err)
	}
	if oldPath != "" && oldPath != filename {
		_ = os.Remove(filepath.Join(a.cfg.AvatarsDir, oldPath)) // drop the stale file from a prior format
	}
	return a.GetUser(userID)
}

// RemoveAvatar clears userID's custom avatar, reverting display to their
// Google photo (or a fallback initial), and returns the updated profile.
func (a *AuthService) RemoveAvatar(userID string) (*models.User, error) {
	avatarUploadLock.Lock()
	defer avatarUploadLock.Unlock()

	oldPath, err := a.currentAvatarPath(userID)
	if err != nil {
		return nil, err
	}
	if _, err := a.db.Exec(`UPDATE users SET avatar_path = '', avatar_updated_at = 0 WHERE id = ?`, userID); err != nil {
		return nil, fmt.Errorf("remove avatar: %w", err)
	}
	if oldPath != "" {
		_ = os.Remove(filepath.Join(a.cfg.AvatarsDir, oldPath))
	}
	return a.GetUser(userID)
}

// AvatarFile returns the absolute path and MIME type of userID's stored
// avatar, or ("", "", sql.ErrNoRows) when they have none. The returned path
// is built entirely from userID and the stored extension -- never from
// request-supplied input -- so no path-traversal guard is needed at the caller.
func (a *AuthService) AvatarFile(userID string) (string, string, error) {
	path, err := a.currentAvatarPath(userID)
	if err != nil {
		return "", "", err
	}
	if path == "" {
		return "", "", sql.ErrNoRows
	}
	mimeType := "image/png"
	if strings.HasSuffix(path, ".jpg") {
		mimeType = "image/jpeg"
	}
	return filepath.Join(a.cfg.AvatarsDir, path), mimeType, nil
}

func (a *AuthService) currentAvatarPath(userID string) (string, error) {
	var path string
	if err := a.db.QueryRow(`SELECT avatar_path FROM users WHERE id = ?`, userID).Scan(&path); err != nil {
		return "", fmt.Errorf("look up current avatar: %w", err)
	}
	return path, nil
}

// stageAvatarUpload writes content to a temp file under directory (mirroring
// cv_service.go's stageCVUpload), sniffing and then fully decode-validating
// it as PNG or JPEG before returning. The caller is responsible for
// os.Rename-ing (on success) or os.Remove-ing (on failure) the returned
// temp path.
func stageAvatarUpload(directory string, content io.Reader) (tempPath, extension string, err error) {
	temp, createErr := os.CreateTemp(directory, ".avatar-upload-*")
	if createErr != nil {
		return "", "", fmt.Errorf("create temporary avatar file: %w", createErr)
	}
	tempPath = temp.Name()
	// Named returns: every error branch below sets err and returns bare, so
	// this defer always sees the real tempPath to clean up (returning a
	// literal "" for tempPath here would blank it before the defer runs).
	defer func() {
		if err != nil {
			_ = os.Remove(tempPath)
		}
	}()

	header := make([]byte, 512)
	read, _ := io.ReadFull(content, header)
	sniffed := http.DetectContentType(header[:read])
	var ok bool
	extension, ok = avatarExtensionForMimeType(sniffed)
	if !ok {
		_ = temp.Close()
		err = ErrInvalidAvatarImage
		return
	}

	remaining := int64(maxAvatarUploadSize-read) + 1
	limited := io.LimitReader(content, remaining)
	written, copyErr := io.Copy(temp, io.MultiReader(bytes.NewReader(header[:read]), limited))
	closeErr := temp.Close()
	if copyErr != nil {
		err = fmt.Errorf("write avatar upload: %w", copyErr)
		return
	}
	if closeErr != nil {
		err = fmt.Errorf("close avatar upload: %w", closeErr)
		return
	}
	if written == 0 {
		err = errors.New("avatar image cannot be empty")
		return
	}
	if written > maxAvatarUploadSize {
		err = fmt.Errorf("avatar image exceeds the %d MiB limit", maxAvatarUploadSize>>20)
		return
	}
	if err = verifyDecodableImage(tempPath, sniffed); err != nil {
		return
	}
	return tempPath, extension, nil
}

func avatarExtensionForMimeType(mimeType string) (string, bool) {
	switch mimeType {
	case "image/png":
		return "png", true
	case "image/jpeg":
		return "jpg", true
	default:
		return "", false
	}
}

// verifyDecodableImage rejects files that merely spoof PNG/JPEG magic bytes.
// It decodes only the header (image.DecodeConfig), not the full pixel data,
// to avoid a decompression-bomb-sized allocation from a tiny spoofed file.
func verifyDecodableImage(path, sniffedMimeType string) error {
	file, err := os.Open(path) // #nosec G304 -- path is our own just-written temp file under AvatarsDir, never request input.
	if err != nil {
		return fmt.Errorf("open staged avatar: %w", err)
	}
	defer func() { _ = file.Close() }()
	config, format, err := image.DecodeConfig(file)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidAvatarImage, err)
	}
	decodedMimeType := map[string]string{"png": "image/png", "jpeg": "image/jpeg"}[format]
	if decodedMimeType == "" || decodedMimeType != sniffedMimeType {
		return ErrInvalidAvatarImage // the sniffed and decoded formats disagree: treat as spoofed
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > maxAvatarDimension || config.Height > maxAvatarDimension {
		return ErrInvalidAvatarImage
	}
	return nil
}
