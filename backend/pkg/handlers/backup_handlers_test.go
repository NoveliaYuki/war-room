package handlers

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"war-room/backend/pkg/config"
	"war-room/backend/pkg/service"
)

func TestBackupHandlersExportAndImportArchive(t *testing.T) {
	source := newCVHandlerFixture(t)
	exported := httptest.NewRecorder()
	source.handler.handleExportBackup(exported, httptest.NewRequest(http.MethodGet, "/api/backup/export", nil))
	if exported.Code != http.StatusOK || exported.Header().Get("Content-Type") != "application/octet-stream" || exported.Body.Len() == 0 {
		t.Fatalf("export status=%d headers=%v bytes=%d", exported.Code, exported.Header(), exported.Body.Len())
	}
	if !bytes.HasPrefix(exported.Body.Bytes(), []byte("WRBACKUP\x01")) {
		t.Fatal("export did not produce an encrypted backup")
	}

	target := newCVHandlerFixture(t)
	request := backupMultipartRequest(t, exported.Body.Bytes())
	imported := httptest.NewRecorder()
	target.handler.handleImportBackup(imported, request)
	if imported.Code != http.StatusOK {
		t.Fatalf("import status=%d: %s", imported.Code, imported.Body.String())
	}
	jobs, err := target.repo.GetAllJobs("", "")
	if err != nil || len(jobs) != 1 || jobs[0].ID != "job-1" {
		t.Fatalf("imported jobs=%+v err=%v", jobs, err)
	}
}

func TestBackupImportAuthenticationFailuresPreserveSavedData(t *testing.T) {
	source := newCVHandlerFixture(t)
	if err := source.repo.DeleteJob("job-1"); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if err := source.handler.jobs.ExportArchive(&archive); err != nil {
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
				target.handler.SetDriveService(service.NewDriveBackupService(&config.Config{GoogleBackupEncryptionKey: test.key}, nil))
			}
			response := httptest.NewRecorder()
			target.handler.handleImportBackup(response, backupMultipartRequest(t, test.data))
			if response.Code != test.status {
				t.Fatalf("import status=%d want=%d", response.Code, test.status)
			}
			jobs, err := target.repo.GetAllJobs("", "")
			if err != nil || len(jobs) != 1 || jobs[0].ID != "job-1" {
				t.Fatalf("failed import changed saved jobs: count=%d err=%v", len(jobs), err)
			}
		})
	}
}

func TestBackupExportRequiresEncryptionKey(t *testing.T) {
	for _, drive := range []*service.DriveBackupService{nil, service.NewDriveBackupService(&config.Config{}, nil)} {
		fixture := newCVHandlerFixture(t)
		fixture.handler.SetDriveService(drive)
		response := httptest.NewRecorder()
		fixture.handler.handleExportBackup(response, httptest.NewRequest(http.MethodGet, "/api/backup/export", nil))
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
	fixture.handler.handleExportBackup(response, httptest.NewRequest(http.MethodGet, "/api/backup/export", nil))
	if response.Code != http.StatusInternalServerError || bytes.HasPrefix(response.Body.Bytes(), []byte("PK")) {
		t.Fatalf("nonce failure export status=%d", response.Code)
	}
}

func TestReadExportArchiveRejectsUnavailableAndOversizedStorage(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "archive")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("oversize")); err != nil {
		t.Fatal(err)
	}
	if _, err := readExportArchive(file, 1); err != errBackupExportTooLarge {
		t.Fatalf("oversize archive error=%v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := readExportArchive(file, 100); err == nil {
		t.Fatal("closed archive must not be read")
	}
	directory, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = directory.Close() }()
	if _, err := readExportArchive(directory, 100); err == nil {
		t.Fatal("directory must not be read as an archive")
	}
}

func TestBackupExportEnforcesSizeLimit(t *testing.T) {
	fixture := newCVHandlerFixture(t)
	fixture.handler.backupMaxBytes = 1
	response := httptest.NewRecorder()
	fixture.handler.handleExportBackup(response, httptest.NewRequest(http.MethodGet, "/api/backup/export", nil))
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize export=%d body=%s", response.Code, response.Body.String())
	}
}

func TestBackupImportDecryptsDriveArchive(t *testing.T) {
	source := newCVHandlerFixture(t)
	var archive bytes.Buffer
	if err := source.handler.jobs.ExportArchive(&archive); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{GoogleBackupEncryptionKey: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="}
	drive := service.NewDriveBackupService(cfg, nil)
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
	jobs, err := target.repo.GetAllJobs("", "")
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
	drive := service.NewDriveBackupService(&config.Config{GoogleBackupEncryptionKey: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="}, nil)
	fixture.handler.SetDriveService(drive)
	wrongKey := httptest.NewRecorder()
	fixture.handler.handleImportBackup(wrongKey, backupMultipartRequest(t, encrypted))
	if wrongKey.Code != http.StatusBadRequest {
		t.Fatalf("encrypted import with invalid envelope=%d", wrongKey.Code)
	}
}

func TestBackupImportRejectsMalformedAndMissingFiles(t *testing.T) {
	fixture := newCVHandlerFixture(t)
	malformed := httptest.NewRecorder()
	fixture.handler.handleImportBackup(malformed, httptest.NewRequest(http.MethodPost, "/api/backup/import", bytes.NewBufferString("invalid")))
	if malformed.Code != http.StatusBadRequest {
		t.Fatalf("malformed multipart status=%d", malformed.Code)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	missingRequest := httptest.NewRequest(http.MethodPost, "/api/backup/import", &body)
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

func TestBackupImportRejectsOversizeAndUnreadableMultipartFiles(t *testing.T) {
	oversize := newCVHandlerFixture(t)
	oversizeRequest := requestWithBackupHeader((500 << 20) + (1 << 20) + 1)
	oversizeResponse := httptest.NewRecorder()
	oversize.handler.handleImportBackup(oversizeResponse, oversizeRequest)
	if oversizeResponse.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize import=%d", oversizeResponse.Code)
	}
	unreadable := newCVHandlerFixture(t)
	unreadableResponse := httptest.NewRecorder()
	unreadable.handler.handleImportBackup(unreadableResponse, requestWithBackupHeader(1))
	if unreadableResponse.Code != http.StatusBadRequest {
		t.Fatalf("unreadable import=%d", unreadableResponse.Code)
	}
}

func requestWithBackupHeader(size int64) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/api/backup/import", nil)
	request.MultipartForm = &multipart.Form{File: map[string][]*multipart.FileHeader{
		"backup": {{Filename: "backup.wrbackup", Size: size}},
	}}
	return request
}

func TestBackupImportRequiresConfirmationForEmptyArchive(t *testing.T) {
	emptySource := newCVHandlerFixture(t)
	if err := emptySource.repo.DeleteJob("job-1"); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if err := emptySource.handler.jobs.ExportArchive(&archive); err != nil {
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
	confirmedRequest := httptest.NewRequest(http.MethodPost, "/api/backup/import", &fields)
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
	request := httptest.NewRequest(http.MethodPost, "/api/backup/import", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request
}
