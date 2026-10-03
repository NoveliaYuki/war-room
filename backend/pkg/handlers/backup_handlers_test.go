package handlers

import (
	"archive/zip"
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBackupHandlersExportAndImportArchive(t *testing.T) {
	source := newCVHandlerFixture(t)
	exported := httptest.NewRecorder()
	source.handler.handleExportBackup(exported, httptest.NewRequest(http.MethodGet, "/api/backup/export", nil))
	if exported.Code != http.StatusOK || exported.Header().Get("Content-Type") != "application/zip" || exported.Body.Len() == 0 {
		t.Fatalf("export status=%d headers=%v bytes=%d", exported.Code, exported.Header(), exported.Body.Len())
	}
	if _, err := zip.NewReader(bytes.NewReader(exported.Body.Bytes()), int64(exported.Body.Len())); err != nil {
		t.Fatalf("export did not produce a ZIP: %v", err)
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

func TestBackupImportRequiresConfirmationForEmptyArchive(t *testing.T) {
	emptySource := newCVHandlerFixture(t)
	if err := emptySource.repo.DeleteJob("job-1"); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if err := emptySource.handler.jobs.ExportArchive(&archive); err != nil {
		t.Fatal(err)
	}
	target := newCVHandlerFixture(t)
	confirmationRequest := backupMultipartRequest(t, archive.Bytes())
	confirmation := httptest.NewRecorder()
	target.handler.handleImportBackup(confirmation, confirmationRequest)
	if confirmation.Code != http.StatusConflict {
		t.Fatalf("empty archive status=%d: %s", confirmation.Code, confirmation.Body.String())
	}

	var fields bytes.Buffer
	writer := multipart.NewWriter(&fields)
	file, err := writer.CreateFormFile("backup", "backup.zip")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(archive.Bytes()); err != nil {
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
	file, err := writer.CreateFormFile("backup", "backup.zip")
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
