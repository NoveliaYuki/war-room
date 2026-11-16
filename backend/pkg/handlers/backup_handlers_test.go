package handlers

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"war-room/backend/pkg/config"
	"war-room/backend/pkg/service"
)

func TestBackupHandlersExportAndImportArchive(t *testing.T) {
	source := newCVHandlerFixture(t)
	exported := httptest.NewRecorder()
	source.handler.handleExportBackup(exported, withUserID(httptest.NewRequest(http.MethodGet, "/api/backup/export", nil), testUserID))
	if exported.Code != http.StatusOK || exported.Header().Get("Content-Type") != "application/octet-stream" || exported.Body.Len() == 0 {
		t.Fatalf("export status=%d headers=%v bytes=%d", exported.Code, exported.Header(), exported.Body.Len())
	}
	if !bytes.HasPrefix(exported.Body.Bytes(), []byte("WRBACKUP\x02")) {
		t.Fatal("export did not produce an encrypted backup")
	}

	target := newCVHandlerFixture(t)
	request := backupMultipartRequest(t, exported.Body.Bytes())
	imported := httptest.NewRecorder()
	target.handler.handleImportBackup(imported, request)
	if imported.Code != http.StatusOK {
		t.Fatalf("import status=%d: %s", imported.Code, imported.Body.String())
	}
	jobs, err := target.repo.GetAllJobs(testUserID, "", "")
	if err != nil || len(jobs) != 1 || jobs[0].ID != "job-1" {
		t.Fatalf("imported jobs=%+v err=%v", jobs, err)
	}
}

func TestBackupImportAuthenticationFailuresPreserveSavedData(t *testing.T) {
	source := newCVHandlerFixture(t)
	if err := source.repo.DeleteJob(testUserID, "job-1"); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if err := source.handler.jobs.ExportArchive(testUserID, &archive); err != nil {
		t.Fatal(err)
	}
	encrypted, err := source.handler.drive.EncryptBackup(archive.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	tampered := bytes.Clone(encrypted)
	tampered[len(tampered)-1] ^= 1
	for _, test := range []struct {
		name   string
		data   []byte
		key    string
		status int
	}{
		{name: "plaintext ZIP", data: archive.Bytes(), status: http.StatusBadRequest},
		{name: "tampered ciphertext", data: tampered, status: http.StatusBadRequest},
		{name: "wrong key", data: encrypted, key: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32)), status: http.StatusBadRequest},
		{name: "missing key", data: encrypted, key: "invalid", status: http.StatusServiceUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			target := newCVHandlerFixture(t)
			if test.key != "" {
				target.handler.SetDriveService(service.NewDriveBackupService(nil, &config.Config{GoogleBackupEncryptionKey: test.key}, nil))
			}
			response := httptest.NewRecorder()
			target.handler.handleImportBackup(response, backupMultipartRequest(t, test.data))
			if response.Code != test.status {
				t.Fatalf("import status=%d want=%d", response.Code, test.status)
			}
			jobs, err := target.repo.GetAllJobs(testUserID, "", "")
			if err != nil || len(jobs) != 1 || jobs[0].ID != "job-1" {
				t.Fatalf("failed import changed saved jobs: count=%d err=%v", len(jobs), err)
			}
		})
	}
}

func TestBackupExportRequiresEncryptionKey(t *testing.T) {
	for _, drive := range []*service.DriveBackupService{nil, service.NewDriveBackupService(nil, &config.Config{}, nil)} {
		fixture := newCVHandlerFixture(t)
		fixture.handler.SetDriveService(drive)
		response := httptest.NewRecorder()
		fixture.handler.handleExportBackup(response, withUserID(httptest.NewRequest(http.MethodGet, "/api/backup/export", nil), testUserID))
		if response.Code != http.StatusServiceUnavailable || bytes.HasPrefix(response.Body.Bytes(), []byte("PK")) {
			t.Fatalf("export without key status=%d", response.Code)
		}
	}
}

func TestBackupExportDoesNotReturnPlaintextWhenNonceGenerationFails(t *testing.T) {
	fixture := newCVHandlerFixture(t)
	original := rand.Reader
	rand.Reader = bytes.NewReader(nil)
	defer func() { rand.Reader = original }()
	response := httptest.NewRecorder()
	fixture.handler.handleExportBackup(response, withUserID(httptest.NewRequest(http.MethodGet, "/api/backup/export", nil), testUserID))
	if response.Code != http.StatusInternalServerError || bytes.HasPrefix(response.Body.Bytes(), []byte("PK")) {
		t.Fatalf("nonce failure export status=%d", response.Code)
	}
}

func TestBackupExportEnforcesSizeLimit(t *testing.T) {
	fixture := newCVHandlerFixture(t)
	fixture.handler.backupMaxBytes = 1
	response := httptest.NewRecorder()
	fixture.handler.handleExportBackup(response, withUserID(httptest.NewRequest(http.MethodGet, "/api/backup/export", nil), testUserID))
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize export=%d body=%s", response.Code, response.Body.String())
	}
}

func TestBackupImportDecryptsDriveArchive(t *testing.T) {
	source := newCVHandlerFixture(t)
	var archive bytes.Buffer
	if err := source.handler.jobs.ExportArchive(testUserID, &archive); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{GoogleBackupEncryptionKey: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="}
	drive := service.NewDriveBackupService(nil, cfg, nil)
	encrypted, err := drive.EncryptBackup(archive.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	target := newCVHandlerFixture(t)
	target.handler.SetDriveService(drive)
	response := httptest.NewRecorder()
	target.handler.handleImportBackup(response, backupMultipartRequest(t, encrypted))
	if response.Code != http.StatusOK {
		t.Fatalf("encrypted import=%d: %s", response.Code, response.Body.String())
	}
	jobs, err := target.repo.GetAllJobs(testUserID, "", "")
	if err != nil || len(jobs) != 1 || jobs[0].ID != "job-1" {
		t.Fatalf("imported jobs=%+v err=%v", jobs, err)
	}
}

func TestBackupImportRejectsEncryptedArchiveWithoutKey(t *testing.T) {
	fixture := newCVHandlerFixture(t)
	encrypted := []byte("WRBACKUP\x01invalid")
	fixture.handler.SetDriveService(nil)
	withoutService := httptest.NewRecorder()
	fixture.handler.handleImportBackup(withoutService, backupMultipartRequest(t, encrypted))
	if withoutService.Code != http.StatusServiceUnavailable {
		t.Fatalf("encrypted import without service=%d", withoutService.Code)
	}
	drive := service.NewDriveBackupService(nil, &config.Config{GoogleBackupEncryptionKey: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="}, nil)
	fixture.handler.SetDriveService(drive)
	wrongKey := httptest.NewRecorder()
	fixture.handler.handleImportBackup(wrongKey, backupMultipartRequest(t, encrypted))
	if wrongKey.Code != http.StatusBadRequest {
		t.Fatalf("encrypted import with invalid envelope=%d", wrongKey.Code)
	}
}

// TestBackupImportSurfacesGenericArchiveErrorsAsBadRequest covers the
// ImportArchive failure branch distinct from the "empty backup needs
// confirmation" conflict: a validly encrypted payload whose decrypted
// contents aren't a zip archive at all.
func TestBackupImportSurfacesGenericArchiveErrorsAsBadRequest(t *testing.T) {
	fixture := newCVHandlerFixture(t)
	encrypted, err := fixture.handler.drive.EncryptBackup([]byte("not a zip file"))
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	fixture.handler.handleImportBackup(response, backupMultipartRequest(t, encrypted))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("generic import archive error status=%d body=%s", response.Code, response.Body.String())
	}
}

// TestBackupImportRejectsPayloadsAboveTheLimit covers both size guards: the
// request body cap and the decrypted-archive cap.
func TestBackupImportRejectsPayloadsAboveTheLimit(t *testing.T) {
	fixture := newCVHandlerFixture(t)
	fixture.handler.backupMaxBytes = 8
	encrypted, err := fixture.handler.drive.EncryptBackup(make([]byte, 9))
	if err != nil {
		t.Fatal(err)
	}
	decrypted := httptest.NewRecorder()
	fixture.handler.handleImportBackup(decrypted, backupMultipartRequest(t, encrypted))
	if decrypted.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized decrypted payload status=%d body=%s", decrypted.Code, decrypted.Body.String())
	}
	large, err := fixture.handler.drive.EncryptBackup(make([]byte, 2<<20))
	if err != nil {
		t.Fatal(err)
	}
	body := httptest.NewRecorder()
	fixture.handler.handleImportBackup(body, backupMultipartRequest(t, large))
	if body.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized request status=%d body=%s", body.Code, body.Body.String())
	}
}

func TestBackupImportRejectsMalformedAndMissingFiles(t *testing.T) {
	fixture := newCVHandlerFixture(t)
	malformed := httptest.NewRecorder()
	fixture.handler.handleImportBackup(malformed, withUserID(httptest.NewRequest(http.MethodPost, "/api/backup/import", bytes.NewBufferString("invalid")), testUserID))
	if malformed.Code != http.StatusBadRequest {
		t.Fatalf("malformed multipart status=%d", malformed.Code)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	missingRequest := withUserID(httptest.NewRequest(http.MethodPost, "/api/backup/import", &body), testUserID)
	missingRequest.Header.Set("Content-Type", writer.FormDataContentType())
	missing := httptest.NewRecorder()
	fixture.handler.handleImportBackup(missing, missingRequest)
	if missing.Code != http.StatusBadRequest {
		t.Fatalf("missing file status=%d", missing.Code)
	}
	invalid := httptest.NewRecorder()
	fixture.handler.handleImportBackup(invalid, backupMultipartRequest(t, []byte("not a zip")))
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid archive status=%d", invalid.Code)
	}
}

func TestBackupImportRejectsTruncatedMultipartUpload(t *testing.T) {
	fixture := newCVHandlerFixture(t)
	encrypted, err := fixture.handler.drive.EncryptBackup([]byte("payload"))
	if err != nil {
		t.Fatal(err)
	}
	request := backupMultipartRequest(t, encrypted)
	body, err := io.ReadAll(request.Body)
	if err != nil {
		t.Fatal(err)
	}
	request.Body = io.NopCloser(bytes.NewReader(body[:len(body)-40]))
	response := httptest.NewRecorder()
	fixture.handler.handleImportBackup(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("truncated upload status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestBackupImportAndExportReportUnwritableStorage(t *testing.T) {
	fixture := newCVHandlerFixture(t)
	encrypted, err := fixture.handler.drive.EncryptBackup([]byte("payload"))
	if err != nil {
		t.Fatal(err)
	}
	fixture.handler.cfg.DataDir = t.TempDir() + "/missing"
	imported := httptest.NewRecorder()
	fixture.handler.handleImportBackup(imported, backupMultipartRequest(t, encrypted))
	if imported.Code != http.StatusInternalServerError {
		t.Fatalf("import without storage status=%d body=%s", imported.Code, imported.Body.String())
	}
	exported := httptest.NewRecorder()
	fixture.handler.handleExportBackup(exported, withUserID(httptest.NewRequest(http.MethodGet, "/api/backup/export", nil), testUserID))
	if exported.Code != http.StatusInternalServerError {
		t.Fatalf("export without storage status=%d body=%s", exported.Code, exported.Body.String())
	}
}

func TestBackupExportAnnouncesExactContentLength(t *testing.T) {
	fixture := newCVHandlerFixture(t)
	response := httptest.NewRecorder()
	fixture.handler.handleExportBackup(response, withUserID(httptest.NewRequest(http.MethodGet, "/api/backup/export", nil), testUserID))
	if response.Code != http.StatusOK || response.Header().Get("Content-Length") != strconv.Itoa(response.Body.Len()) {
		t.Fatalf("export status=%d length header=%q body=%d", response.Code, response.Header().Get("Content-Length"), response.Body.Len())
	}
}

func TestBackupImportLeavesNoTemporaryFiles(t *testing.T) {
	fixture := newCVHandlerFixture(t)
	encrypted, err := fixture.handler.drive.EncryptBackup([]byte("not a zip file"))
	if err != nil {
		t.Fatal(err)
	}
	fixture.handler.handleImportBackup(httptest.NewRecorder(), backupMultipartRequest(t, encrypted))
	fixture.handler.handleExportBackup(httptest.NewRecorder(), withUserID(httptest.NewRequest(http.MethodGet, "/api/backup/export", nil), testUserID))
	entries, err := os.ReadDir(fixture.handler.cfg.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".war-room-") {
			t.Fatalf("temporary backup file left behind: %s", entry.Name())
		}
	}
}

func TestBackupImportAcceptsLegacySingleShotBackups(t *testing.T) {
	source := newCVHandlerFixture(t)
	var archive bytes.Buffer
	if err := source.handler.jobs.ExportArchive(testUserID, &archive); err != nil {
		t.Fatal(err)
	}
	key, err := base64.StdEncoding.DecodeString(source.handler.cfg.GoogleBackupEncryptionKey)
	if err != nil {
		t.Fatal(err)
	}
	legacy := sealLegacyBackup(t, key, archive.Bytes())
	target := newCVHandlerFixture(t)
	response := httptest.NewRecorder()
	target.handler.handleImportBackup(response, backupMultipartRequest(t, legacy))
	if response.Code != http.StatusOK {
		t.Fatalf("legacy import status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestBackupImportReadsFieldsInAnyOrderAndIgnoresDuplicates(t *testing.T) {
	emptySource := newCVHandlerFixture(t)
	if err := emptySource.repo.DeleteJob(testUserID, "job-1"); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if err := emptySource.handler.jobs.ExportArchive(testUserID, &archive); err != nil {
		t.Fatal(err)
	}
	encrypted, err := emptySource.handler.drive.EncryptBackup(archive.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	var fields bytes.Buffer
	writer := multipart.NewWriter(&fields)
	for _, step := range []func() error{
		func() error { return writer.WriteField("allow_empty", "true") },
		func() error { return writer.WriteField("unrelated", "ignored") },
		func() error {
			file, err := writer.CreateFormFile("backup", "backup.wrbackup")
			if err == nil {
				_, err = file.Write(encrypted)
			}
			return err
		},
		func() error {
			file, err := writer.CreateFormFile("backup", "second.wrbackup")
			if err == nil {
				_, err = file.Write([]byte("ignored duplicate"))
			}
			return err
		},
	} {
		if err := step(); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := withUserID(httptest.NewRequest(http.MethodPost, "/api/backup/import", &fields), testUserID)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	target := newCVHandlerFixture(t)
	response := httptest.NewRecorder()
	target.handler.handleImportBackup(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("import status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestBackupImportRequiresConfirmationForEmptyArchive(t *testing.T) {
	emptySource := newCVHandlerFixture(t)
	if err := emptySource.repo.DeleteJob(testUserID, "job-1"); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if err := emptySource.handler.jobs.ExportArchive(testUserID, &archive); err != nil {
		t.Fatal(err)
	}
	encrypted, err := emptySource.handler.drive.EncryptBackup(archive.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	target := newCVHandlerFixture(t)
	confirmationRequest := backupMultipartRequest(t, encrypted)
	confirmation := httptest.NewRecorder()
	target.handler.handleImportBackup(confirmation, confirmationRequest)
	if confirmation.Code != http.StatusConflict {
		t.Fatalf("empty archive status=%d: %s", confirmation.Code, confirmation.Body.String())
	}

	var fields bytes.Buffer
	writer := multipart.NewWriter(&fields)
	file, err := writer.CreateFormFile("backup", "backup.wrbackup")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(encrypted); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("allow_empty", "true"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	confirmedRequest := withUserID(httptest.NewRequest(http.MethodPost, "/api/backup/import", &fields), testUserID)
	confirmedRequest.Header.Set("Content-Type", writer.FormDataContentType())
	confirmed := httptest.NewRecorder()
	target.handler.handleImportBackup(confirmed, confirmedRequest)
	if confirmed.Code != http.StatusOK {
		t.Fatalf("confirmed empty archive status=%d: %s", confirmed.Code, confirmed.Body.String())
	}
}

func backupMultipartRequest(t *testing.T, content []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	file, err := writer.CreateFormFile("backup", "backup.wrbackup")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := withUserID(httptest.NewRequest(http.MethodPost, "/api/backup/import", &body), testUserID)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request
}

// sealLegacyBackup builds the single-shot "WRBACKUP\x01" envelope older
// installations produced, so import keeps accepting their files.
func sealLegacyBackup(t *testing.T, key, plaintext []byte) []byte {
	t.Helper()
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	sealed := append([]byte("WRBACKUP\x01"), nonce...)
	return aead.Seal(sealed, nonce, plaintext, []byte("war-room-backup-v1"))
}

// TestBackupImportDiscardsStagedArchiveWhenLaterPartsFail covers the cleanup
// after the backup part decrypted successfully but the form then breaks.
func TestBackupImportDiscardsStagedArchiveWhenLaterPartsFail(t *testing.T) {
	fixture := newCVHandlerFixture(t)
	encrypted, err := fixture.handler.drive.EncryptBackup([]byte("payload"))
	if err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	file, err := writer.CreateFormFile("backup", "backup.wrbackup")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(encrypted); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.CreateFormField("allow_empty"); err != nil {
		t.Fatal(err)
	}
	request := withUserID(httptest.NewRequest(http.MethodPost, "/api/backup/import", &body), testUserID)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	fixture.handler.handleImportBackup(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("broken trailing part status=%d body=%s", response.Code, response.Body.String())
	}
	entries, err := os.ReadDir(fixture.handler.cfg.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".war-room-") {
			t.Fatalf("staged archive left behind: %s", entry.Name())
		}
	}
}

func TestReorderEndpointsRejectUnknownDuplicateAndForeignIDs(t *testing.T) {
	fixture := newCVHandlerFixture(t)
	for _, test := range []struct {
		name    string
		handler http.HandlerFunc
		body    string
		status  int
	}{
		{"unknown job", fixture.handler.handleReorderJobs, `{"job_ids":["missing"]}`, http.StatusBadRequest},
		{"duplicate job", fixture.handler.handleReorderJobs, `{"job_ids":["job-1","job-1"]}`, http.StatusBadRequest},
		{"unknown stage", fixture.handler.handleReorderStages, `{"job_id":"job-1","stage_ids":["missing"]}`, http.StatusBadRequest},
		{"foreign job for stages", fixture.handler.handleReorderStages, `{"job_id":"other-job","stage_ids":["a"]}`, http.StatusNotFound},
		{"foreign stage for questions", fixture.handler.handleReorderQuestions, `{"stage_id":"other-stage","question_ids":["a"]}`, http.StatusNotFound},
		{"valid job order", fixture.handler.handleReorderJobs, `{"job_ids":["job-1"]}`, http.StatusOK},
	} {
		request := withUserID(httptest.NewRequest(http.MethodPut, "/api/reorder", strings.NewReader(test.body)), testUserID)
		response := httptest.NewRecorder()
		test.handler(response, request)
		if response.Code != test.status {
			t.Errorf("%s: status=%d want=%d body=%s", test.name, response.Code, test.status, response.Body.String())
		}
	}
}
