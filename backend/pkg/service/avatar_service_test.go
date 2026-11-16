package service

import (
	"bytes"
	"database/sql"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func newAvatarTestUser(t *testing.T, auth *AuthService) string {
	t.Helper()
	user, err := auth.UpsertUserFromGoogle(GoogleIdentity{Sub: "avatar-sub", Email: "allowed@example.com", EmailVerified: true, Name: "Allowed"}, "203.0.113.5")
	if err != nil {
		t.Fatalf("create test user: %v", err)
	}
	return user.ID
}

func encodedPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode PNG fixture: %v", err)
	}
	return buf.Bytes()
}

func encodedJPEG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	img.Set(0, 0, color.RGBA{B: 255, A: 255})
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("encode JPEG fixture: %v", err)
	}
	return buf.Bytes()
}

// minimalPNGHeader builds just enough of a PNG (signature + a valid IHDR
// chunk claiming the given dimensions) for png.DecodeConfig to succeed
// without needing real pixel data -- used to test the oversized-dimension
// guard without allocating gigabytes of actual pixels.
func minimalPNGHeader(width, height uint32) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
	data := make([]byte, 13)
	binary.BigEndian.PutUint32(data[0:4], width)
	binary.BigEndian.PutUint32(data[4:8], height)
	data[8] = 8 // bit depth
	data[9] = 6 // color type: truecolor with alpha
	chunk := append([]byte("IHDR"), data...)
	checksum := crc32.ChecksumIEEE(chunk)
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], 13) // len(data) is always this fixed IHDR payload size
	buf.Write(length[:])
	buf.Write(chunk)
	var crc [4]byte
	binary.BigEndian.PutUint32(crc[:], checksum)
	buf.Write(crc[:])
	return buf.Bytes()
}

func TestSaveAvatarStoresPNGAndUpdatesUser(t *testing.T) {
	auth, _ := newAuthTestService(t)
	userID := newAvatarTestUser(t, auth)
	user, err := auth.SaveAvatar(userID, bytes.NewReader(encodedPNG(t, 4, 4)))
	if err != nil {
		t.Fatalf("SaveAvatar: %v", err)
	}
	if user.AvatarURL == "" {
		t.Fatal("expected AvatarURL to be set after saving an avatar")
	}
	if _, err := os.Stat(filepath.Join(auth.cfg.AvatarsDir, userID+".png")); err != nil {
		t.Fatalf("expected avatar file on disk: %v", err)
	}
}

func TestSaveAvatarStoresJPEGWithJpgExtension(t *testing.T) {
	auth, _ := newAuthTestService(t)
	userID := newAvatarTestUser(t, auth)
	if _, err := auth.SaveAvatar(userID, bytes.NewReader(encodedJPEG(t, 4, 4))); err != nil {
		t.Fatalf("SaveAvatar: %v", err)
	}
	if _, err := os.Stat(filepath.Join(auth.cfg.AvatarsDir, userID+".jpg")); err != nil {
		t.Fatalf("expected .jpg avatar file on disk: %v", err)
	}
}

func TestSaveAvatarReplacesPriorFormatAndRemovesOrphan(t *testing.T) {
	auth, _ := newAuthTestService(t)
	userID := newAvatarTestUser(t, auth)
	if _, err := auth.SaveAvatar(userID, bytes.NewReader(encodedPNG(t, 4, 4))); err != nil {
		t.Fatalf("first SaveAvatar: %v", err)
	}
	pngPath := filepath.Join(auth.cfg.AvatarsDir, userID+".png")
	if _, err := os.Stat(pngPath); err != nil {
		t.Fatalf("expected initial PNG file: %v", err)
	}
	if _, err := auth.SaveAvatar(userID, bytes.NewReader(encodedJPEG(t, 4, 4))); err != nil {
		t.Fatalf("second SaveAvatar: %v", err)
	}
	if _, err := os.Stat(pngPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected the orphaned PNG file to be removed, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(auth.cfg.AvatarsDir, userID+".jpg")); err != nil {
		t.Fatalf("expected the new JPG file to exist: %v", err)
	}
}

func TestSaveAvatarRejectsOversizedUpload(t *testing.T) {
	auth, _ := newAuthTestService(t)
	userID := newAvatarTestUser(t, auth)
	oversized := append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, bytes.Repeat([]byte{0}, maxAvatarUploadSize+10)...)
	if _, err := auth.SaveAvatar(userID, bytes.NewReader(oversized)); err == nil {
		t.Fatal("expected an oversized upload to be rejected")
	}
	entries, _ := os.ReadDir(auth.cfg.AvatarsDir)
	if len(entries) != 0 {
		t.Fatalf("rejected upload left files behind: %v", entries)
	}
}

func TestSaveAvatarRejectsNonImageContentType(t *testing.T) {
	auth, _ := newAuthTestService(t)
	userID := newAvatarTestUser(t, auth)
	_, err := auth.SaveAvatar(userID, bytes.NewReader([]byte("not an image, just plain text")))
	if !errors.Is(err, ErrInvalidAvatarImage) {
		t.Fatalf("SaveAvatar error=%v, want ErrInvalidAvatarImage", err)
	}
}

func TestSaveAvatarRejectsSpoofedMagicBytesThatFailToDecode(t *testing.T) {
	auth, _ := newAuthTestService(t)
	userID := newAvatarTestUser(t, auth)
	spoofed := append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, []byte("this is not a real PNG body")...)
	_, err := auth.SaveAvatar(userID, bytes.NewReader(spoofed))
	if !errors.Is(err, ErrInvalidAvatarImage) {
		t.Fatalf("SaveAvatar error=%v, want ErrInvalidAvatarImage for spoofed magic bytes", err)
	}
}

func TestVerifyDecodableImageRejectsOversizedDimensions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "huge.png")
	if err := os.WriteFile(path, minimalPNGHeader(maxAvatarDimension+1, 10), 0600); err != nil {
		t.Fatal(err)
	}
	if err := verifyDecodableImage(path, "image/png"); !errors.Is(err, ErrInvalidAvatarImage) {
		t.Fatalf("verifyDecodableImage error=%v, want ErrInvalidAvatarImage for an oversized image", err)
	}
}

func TestRemoveAvatarClearsPathAndDeletesFile(t *testing.T) {
	auth, _ := newAuthTestService(t)
	userID := newAvatarTestUser(t, auth)
	if _, err := auth.SaveAvatar(userID, bytes.NewReader(encodedPNG(t, 4, 4))); err != nil {
		t.Fatalf("SaveAvatar: %v", err)
	}
	user, err := auth.RemoveAvatar(userID)
	if err != nil {
		t.Fatalf("RemoveAvatar: %v", err)
	}
	if user.AvatarURL != "" {
		t.Fatalf("AvatarURL=%q, want empty after removal", user.AvatarURL)
	}
	if _, err := os.Stat(filepath.Join(auth.cfg.AvatarsDir, userID+".png")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected the avatar file to be removed, stat err=%v", err)
	}
	if _, _, err := auth.AvatarFile(userID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("AvatarFile error=%v, want sql.ErrNoRows after removal", err)
	}
}

func TestRemoveAvatarIsNoOpWhenNoneSet(t *testing.T) {
	auth, _ := newAuthTestService(t)
	userID := newAvatarTestUser(t, auth)
	if _, err := auth.RemoveAvatar(userID); err != nil {
		t.Fatalf("RemoveAvatar on a user with no avatar should not error: %v", err)
	}
}

func TestAvatarFileReturnsNotFoundWhenUnset(t *testing.T) {
	auth, _ := newAuthTestService(t)
	userID := newAvatarTestUser(t, auth)
	if _, _, err := auth.AvatarFile(userID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("AvatarFile error=%v, want sql.ErrNoRows for a user with no avatar", err)
	}
}

func TestAvatarFileReturnsStoredPathAndMimeType(t *testing.T) {
	auth, _ := newAuthTestService(t)
	userID := newAvatarTestUser(t, auth)
	if _, err := auth.SaveAvatar(userID, bytes.NewReader(encodedJPEG(t, 4, 4))); err != nil {
		t.Fatalf("SaveAvatar: %v", err)
	}
	path, mimeType, err := auth.AvatarFile(userID)
	if err != nil {
		t.Fatalf("AvatarFile: %v", err)
	}
	if mimeType != "image/jpeg" {
		t.Fatalf("mimeType=%q, want image/jpeg", mimeType)
	}
	if filepath.Base(path) != userID+".jpg" {
		t.Fatalf("path=%q, want basename %s", path, userID+".jpg")
	}
}

func TestGetUserComputesAvatarURLOnlyWhenPathSet(t *testing.T) {
	auth, _ := newAuthTestService(t)
	userID := newAvatarTestUser(t, auth)
	before, err := auth.GetUser(userID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if before.AvatarURL != "" {
		t.Fatalf("AvatarURL=%q, want empty before any avatar is saved", before.AvatarURL)
	}
	if _, err := auth.SaveAvatar(userID, bytes.NewReader(encodedPNG(t, 4, 4))); err != nil {
		t.Fatalf("SaveAvatar: %v", err)
	}
	after, err := auth.GetUser(userID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if after.AvatarURL == "" {
		t.Fatal("AvatarURL should be set once a custom avatar exists")
	}
}

func TestSaveAvatarKeepsPreviousFileWhenDatabaseUpdateFails(t *testing.T) {
	auth, _ := newAuthTestService(t)
	userID := newAvatarTestUser(t, auth)
	if _, err := auth.SaveAvatar(userID, bytes.NewReader(encodedPNG(t, 4, 4))); err != nil {
		t.Fatalf("SaveAvatar: %v", err)
	}
	if _, err := auth.db.Exec(`CREATE TRIGGER block_avatar_update BEFORE UPDATE OF avatar_path ON users BEGIN SELECT RAISE(ABORT, 'blocked'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.SaveAvatar(userID, bytes.NewReader(encodedPNG(t, 5, 5))); err == nil {
		t.Fatal("expected the blocked database update to fail SaveAvatar")
	}
	if _, err := os.Stat(filepath.Join(auth.cfg.AvatarsDir, userID+".png")); err != nil {
		t.Fatalf("same-name avatar must survive a failed update: %v", err)
	}
	if _, err := auth.SaveAvatar(userID, bytes.NewReader(encodedJPEG(t, 4, 4))); err == nil {
		t.Fatal("expected the blocked database update to fail the format change")
	}
	if _, err := os.Stat(filepath.Join(auth.cfg.AvatarsDir, userID+".jpg")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("new-format file must be removed after a failed update, stat err=%v", err)
	}
}

func TestSaveAndRemoveAvatarRejectUnknownUser(t *testing.T) {
	auth, _ := newAuthTestService(t)
	if _, err := auth.SaveAvatar("missing", bytes.NewReader(encodedPNG(t, 4, 4))); err == nil {
		t.Fatal("SaveAvatar must fail for an unknown user")
	}
	if _, err := auth.RemoveAvatar("missing"); err == nil {
		t.Fatal("RemoveAvatar must fail for an unknown user")
	}
}

func TestSaveAvatarFailsWhenDestinationCannotBeInstalled(t *testing.T) {
	auth, _ := newAuthTestService(t)
	userID := newAvatarTestUser(t, auth)
	if err := os.Mkdir(filepath.Join(auth.cfg.AvatarsDir, userID+".png"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.SaveAvatar(userID, bytes.NewReader(encodedPNG(t, 4, 4))); err == nil {
		t.Fatal("expected installing over a directory to fail")
	}
}

func TestSaveAvatarFailsWhenAvatarDirectoryIsMissing(t *testing.T) {
	auth, _ := newAuthTestService(t)
	userID := newAvatarTestUser(t, auth)
	if err := os.RemoveAll(auth.cfg.AvatarsDir); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.SaveAvatar(userID, bytes.NewReader(encodedPNG(t, 4, 4))); err == nil {
		t.Fatal("expected a missing avatar directory to fail")
	}
}

func TestRemoveAvatarReportsDatabaseFailure(t *testing.T) {
	auth, _ := newAuthTestService(t)
	userID := newAvatarTestUser(t, auth)
	if _, err := auth.db.Exec(`CREATE TRIGGER block_avatar_clear BEFORE UPDATE OF avatar_path ON users BEGIN SELECT RAISE(ABORT, 'blocked'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.RemoveAvatar(userID); err == nil {
		t.Fatal("expected the blocked database update to fail RemoveAvatar")
	}
}

func TestAvatarFileRejectsUnknownUser(t *testing.T) {
	auth, _ := newAuthTestService(t)
	if _, _, err := auth.AvatarFile("missing"); err == nil || errors.Is(err, sql.ErrNoRows) && err.Error() == sql.ErrNoRows.Error() {
		t.Fatalf("AvatarFile error=%v, want a lookup failure for an unknown user", err)
	}
}

func TestVerifyDecodableImageRejectsMissingFileAndFormatMismatch(t *testing.T) {
	dir := t.TempDir()
	if err := verifyDecodableImage(filepath.Join(dir, "missing.png"), "image/png"); err == nil {
		t.Fatal("expected an error for a missing staged file")
	}
	path := filepath.Join(dir, "real.png")
	if err := os.WriteFile(path, encodedPNG(t, 4, 4), 0600); err != nil {
		t.Fatal(err)
	}
	if err := verifyDecodableImage(path, "image/jpeg"); !errors.Is(err, ErrInvalidAvatarImage) {
		t.Fatalf("verifyDecodableImage error=%v, want ErrInvalidAvatarImage when sniffed and decoded formats disagree", err)
	}
}
