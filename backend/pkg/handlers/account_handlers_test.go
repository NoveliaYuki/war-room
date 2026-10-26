package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"war-room/backend/pkg/models"
)

func encodedPNGBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func avatarUploadRequest(t *testing.T, userID string, content []byte, filename string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := withUserID(httptest.NewRequest(http.MethodPost, "/api/account/avatar", &body), userID)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request
}

func TestHandleUploadAvatarRequiresSession(t *testing.T) {
	handler, _, _ := googleHandler(t, true)
	recorder := httptest.NewRecorder()
	handler.handleUploadAvatar(recorder, httptest.NewRequest(http.MethodPost, "/api/account/avatar", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", recorder.Code)
	}
}

func TestHandleUploadAvatarRejectsMissingFile(t *testing.T) {
	handler, _, userID := googleHandler(t, true)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	mustNoErr(t, writer.Close())
	request := withUserID(httptest.NewRequest(http.MethodPost, "/api/account/avatar", &body), userID)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()
	handler.handleUploadAvatar(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400: %s", recorder.Code, recorder.Body.String())
	}
}

func TestHandleUploadAvatarRejectsMalformedMultipartBody(t *testing.T) {
	handler, _, userID := googleHandler(t, true)
	request := withUserID(httptest.NewRequest(http.MethodPost, "/api/account/avatar", bytes.NewBufferString("not multipart")), userID)
	request.Header.Set("Content-Type", "multipart/form-data; boundary=missing")
	recorder := httptest.NewRecorder()
	handler.handleUploadAvatar(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", recorder.Code)
	}
}

func TestHandleUploadAvatarRejectsEmptyFile(t *testing.T) {
	handler, _, userID := googleHandler(t, true)
	recorder := httptest.NewRecorder()
	handler.handleUploadAvatar(recorder, avatarUploadRequest(t, userID, nil, "me.png"))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400: %s", recorder.Code, recorder.Body.String())
	}
}

func TestHandleUploadAvatarRejectsOversizedFile(t *testing.T) {
	handler, _, userID := googleHandler(t, true)
	oversized := append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, bytes.Repeat([]byte{0}, maxAvatarUploadBytes+10)...)
	recorder := httptest.NewRecorder()
	handler.handleUploadAvatar(recorder, avatarUploadRequest(t, userID, oversized, "me.png"))
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d, want 413: %s", recorder.Code, recorder.Body.String())
	}
}

func TestHandleUploadAvatarRejectsNonImageFile(t *testing.T) {
	handler, _, userID := googleHandler(t, true)
	recorder := httptest.NewRecorder()
	handler.handleUploadAvatar(recorder, avatarUploadRequest(t, userID, []byte("plain text, not an image"), "me.txt"))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400: %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "PNG or JPEG") {
		t.Fatalf("body=%s, want a PNG/JPEG rejection message", recorder.Body.String())
	}
}

func TestHandleUploadAvatarSucceedsAndReturnsUpdatedProfile(t *testing.T) {
	handler, _, userID := googleHandler(t, true)
	recorder := httptest.NewRecorder()
	handler.handleUploadAvatar(recorder, avatarUploadRequest(t, userID, encodedPNGBytes(t), "me.png"))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	var user models.User
	if err := json.Unmarshal(recorder.Body.Bytes(), &user); err != nil {
		t.Fatal(err)
	}
	if user.AvatarURL == "" {
		t.Fatal("expected avatar_url to be set in the response")
	}
}

func TestHandleUploadAvatarReportsInternalErrors(t *testing.T) {
	handler, db, userID := googleHandler(t, true)
	mustNoErr(t, db.Close())
	recorder := httptest.NewRecorder()
	handler.handleUploadAvatar(recorder, avatarUploadRequest(t, userID, encodedPNGBytes(t), "me.png"))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d, want 500: %s", recorder.Code, recorder.Body.String())
	}
}

func TestHandleGetAvatarRequiresSession(t *testing.T) {
	handler, _, _ := googleHandler(t, true)
	recorder := httptest.NewRecorder()
	handler.handleGetAvatar(recorder, httptest.NewRequest(http.MethodGet, "/api/account/avatar", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", recorder.Code)
	}
}

func TestHandleGetAvatarReturns404WhenUnset(t *testing.T) {
	handler, _, userID := googleHandler(t, true)
	recorder := httptest.NewRecorder()
	handler.handleGetAvatar(recorder, withUserID(httptest.NewRequest(http.MethodGet, "/api/account/avatar", nil), userID))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", recorder.Code)
	}
}

func TestHandleGetAvatarServesStoredBytesWithPrivateCaching(t *testing.T) {
	handler, _, userID := googleHandler(t, true)
	imageBytes := encodedPNGBytes(t)
	upload := httptest.NewRecorder()
	handler.handleUploadAvatar(upload, avatarUploadRequest(t, userID, imageBytes, "me.png"))
	if upload.Code != http.StatusOK {
		t.Fatalf("upload status=%d: %s", upload.Code, upload.Body.String())
	}

	recorder := httptest.NewRecorder()
	handler.handleGetAvatar(recorder, withUserID(httptest.NewRequest(http.MethodGet, "/api/account/avatar", nil), userID))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("Content-Type=%q, want image/png", recorder.Header().Get("Content-Type"))
	}
	if !strings.HasPrefix(recorder.Header().Get("Cache-Control"), "private") {
		t.Fatalf("Cache-Control=%q, want it to start with private", recorder.Header().Get("Cache-Control"))
	}
	if !bytes.Equal(recorder.Body.Bytes(), imageBytes) {
		t.Fatal("served bytes did not match the uploaded image")
	}
}

func TestHandleDeleteAvatarRequiresSession(t *testing.T) {
	handler, _, _ := googleHandler(t, true)
	recorder := httptest.NewRecorder()
	handler.handleDeleteAvatar(recorder, httptest.NewRequest(http.MethodDelete, "/api/account/avatar", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", recorder.Code)
	}
}

func TestHandleDeleteAvatarRevertsToGooglePhoto(t *testing.T) {
	handler, db, userID := googleHandler(t, true)
	_, err := db.Exec(`UPDATE users SET picture_url = ? WHERE id = ?`, "https://google.example/photo.jpg", userID)
	mustNoErr(t, err)
	upload := httptest.NewRecorder()
	handler.handleUploadAvatar(upload, avatarUploadRequest(t, userID, encodedPNGBytes(t), "me.png"))
	if upload.Code != http.StatusOK {
		t.Fatalf("upload status=%d: %s", upload.Code, upload.Body.String())
	}

	recorder := httptest.NewRecorder()
	handler.handleDeleteAvatar(recorder, withUserID(httptest.NewRequest(http.MethodDelete, "/api/account/avatar", nil), userID))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	var user models.User
	if err := json.Unmarshal(recorder.Body.Bytes(), &user); err != nil {
		t.Fatal(err)
	}
	if user.AvatarURL != "" {
		t.Fatalf("avatar_url=%q, want empty after removal", user.AvatarURL)
	}
	if user.PictureURL != "https://google.example/photo.jpg" {
		t.Fatalf("picture_url=%q, removal should not touch the Google photo", user.PictureURL)
	}
}

func TestHandleDeleteAvatarReportsInternalErrors(t *testing.T) {
	handler, db, userID := googleHandler(t, true)
	mustNoErr(t, db.Close())
	recorder := httptest.NewRecorder()
	handler.handleDeleteAvatar(recorder, withUserID(httptest.NewRequest(http.MethodDelete, "/api/account/avatar", nil), userID))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d, want 500: %s", recorder.Code, recorder.Body.String())
	}
}

func TestHandleDeleteAvatarWhenNoneSetSucceeds(t *testing.T) {
	handler, _, userID := googleHandler(t, true)
	recorder := httptest.NewRecorder()
	handler.handleDeleteAvatar(recorder, withUserID(httptest.NewRequest(http.MethodDelete, "/api/account/avatar", nil), userID))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200: %s", recorder.Code, recorder.Body.String())
	}
}

func TestInternalErrorHidesDetails(t *testing.T) {
	recorder := httptest.NewRecorder()
	internalError(recorder, errors.New("sqlite: secret detail"))
	if recorder.Code != http.StatusInternalServerError || strings.Contains(recorder.Body.String(), "secret") {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestBackupTransfersAreLimitedToTwoAtATime(t *testing.T) {
	for range 2 {
		if !acquireBackupSlot(httptest.NewRecorder()) {
			t.Fatal("the first two transfers must be admitted")
		}
	}
	busy := httptest.NewRecorder()
	if acquireBackupSlot(busy) || busy.Code != http.StatusTooManyRequests {
		t.Fatalf("third transfer: status=%d, want 429", busy.Code)
	}
	releaseBackupSlot()
	releaseBackupSlot()
	if !acquireBackupSlot(httptest.NewRecorder()) {
		t.Fatal("a released slot must be reusable")
	}
	releaseBackupSlot()
}
