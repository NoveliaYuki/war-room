package handlers

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"war-room/backend/pkg/models"
)

func TestCVHandlersReportMissingFilesAndRecords(t *testing.T) {
	fixture := newCVHandlerFixture(t)
	missingUpload := multipartCVRequest(t, false, nil)
	if response := coverageServeHandler(fixture.handler.handleUploadCVVersion, missingUpload); response.Code != http.StatusBadRequest {
		t.Fatalf("missing CV file status=%d", response.Code)
	}
	emptyUpload := multipartCVRequest(t, true, nil)
	if response := coverageServeHandler(fixture.handler.handleUploadCVVersion, emptyUpload); response.Code != http.StatusBadRequest {
		t.Fatalf("empty CV file status=%d", response.Code)
	}
	badDownload := cvRequestWithID(http.MethodGet, "missing", nil)
	if response := coverageServeHandler(fixture.handler.handleDownloadCVVersion, badDownload); response.Code != http.StatusNotFound {
		t.Fatalf("missing CV download status=%d", response.Code)
	}
	unsafe := &models.CVVersion{ID: "unsafe-path", Version: 1, OriginalName: "resume.pdf", StoredFilename: "../outside.pdf", FileSize: 1, MimeType: "application/pdf", SHA256: "hash", UploadedAt: 1}
	if err := fixture.repo.InsertCVVersion(unsafe); err != nil {
		t.Fatal(err)
	}
	unsafeDownload := cvRequestWithID(http.MethodGet, unsafe.ID, nil)
	if response := coverageServeHandler(fixture.handler.handleDownloadCVVersion, unsafeDownload); response.Code != http.StatusNotFound {
		t.Fatalf("unsafe CV download path status=%d", response.Code)
	}
	badDelete := cvRequestWithID(http.MethodDelete, "missing", nil)
	if response := coverageServeHandler(fixture.handler.handleDeleteCVVersion, badDelete); response.Code != http.StatusNotFound {
		t.Fatalf("missing CV delete status=%d", response.Code)
	}
	badAssign := cvRequestWithID(http.MethodPut, "missing", bytes.NewBufferString(`{"version_id":"no-such-version"}`))
	if response := coverageServeHandler(fixture.handler.handleAssignCVVersion, badAssign); response.Code != http.StatusNotFound {
		t.Fatalf("missing CV assignment status=%d", response.Code)
	}
}

func TestCVUploadRejectsRequestAboveLimit(t *testing.T) {
	fixture := newCVHandlerFixture(t)
	request := multipartCVRequest(t, true, make([]byte, (50<<20)+1))
	response := coverageServeHandler(fixture.handler.handleUploadCVVersion, request)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized CV upload status=%d", response.Code)
	}
}

func TestCVHandlersCoverStorageFailureAndMissingBlob(t *testing.T) {
	fixture := newCVHandlerFixture(t)
	version := &models.CVVersion{ID: "cv-missing-blob", Version: 1, OriginalName: "resume.pdf", StoredFilename: "missing.pdf", FileSize: 4, MimeType: "application/pdf", SHA256: "hash", UploadedAt: 1}
	if err := fixture.repo.InsertCVVersion(version); err != nil {
		t.Fatal(err)
	}
	missingBlob := cvRequestWithID(http.MethodGet, version.ID, nil)
	if response := coverageServeHandler(fixture.handler.handleDownloadCVVersion, missingBlob); response.Code != http.StatusNotFound {
		t.Fatalf("missing blob status=%d", response.Code)
	}
	if err := fixture.db.Close(); err != nil {
		t.Fatal(err)
	}
	if response := coverageServeHandler(fixture.handler.handleUploadCVVersion, multipartCVRequest(t, true, []byte("cv"))); response.Code != http.StatusInternalServerError {
		t.Fatalf("storage failure status=%d", response.Code)
	}
	deleteRequest := cvRequestWithID(http.MethodDelete, version.ID, nil)
	if response := coverageServeHandler(fixture.handler.handleDeleteCVVersion, deleteRequest); response.Code != http.StatusInternalServerError {
		t.Fatalf("delete failure status=%d", response.Code)
	}
	assignRequest := cvRequestWithID(http.MethodPut, "job-1", bytes.NewBufferString(`{"version_id":null}`))
	if response := coverageServeHandler(fixture.handler.handleAssignCVVersion, assignRequest); response.Code != http.StatusInternalServerError {
		t.Fatalf("assignment failure status=%d", response.Code)
	}
	exportResponse := httptest.NewRecorder()
	fixture.handler.handleExportBackup(exportResponse, httptest.NewRequest(http.MethodGet, "/api/backup/export", nil))
	if exportResponse.Code != http.StatusInternalServerError {
		t.Fatalf("export database failure status=%d", exportResponse.Code)
	}
}

func TestCVAssignmentReturnsErrorWhenReloadFails(t *testing.T) {
	fixture := newCVHandlerFixture(t)
	if _, err := fixture.db.Exec(`CREATE TRIGGER corrupt_interviewers_after_cv_update AFTER UPDATE OF cv_version_id ON jobs BEGIN UPDATE jobs SET interviewers_json = '{' WHERE id = NEW.id; END`); err != nil {
		t.Fatal(err)
	}
	request := cvRequestWithID(http.MethodPut, "job-1", bytes.NewBufferString(`{"version_id":null}`))
	response := coverageServeHandler(fixture.handler.handleAssignCVVersion, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("assignment reload failure status=%d: %s", response.Code, response.Body.String())
	}
}

func multipartCVRequest(t *testing.T, includeFile bool, content []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if includeFile {
		file, err := writer.CreateFormFile("file", "resume.pdf")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/cv/versions", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request
}

func cvRequestWithID(method, id string, body *bytes.Buffer) *http.Request {
	if body == nil {
		body = bytes.NewBuffer(nil)
	}
	request := httptest.NewRequest(method, "/api/cv/versions/"+id, body)
	request.SetPathValue("id", id)
	return request
}

func TestExportBackupHandlesUnavailableDestination(t *testing.T) {
	fixture := newCVHandlerFixture(t)
	fixture.handler.cfg.DataDir = filepath.Join(t.TempDir(), "missing")
	response := httptest.NewRecorder()
	fixture.handler.handleExportBackup(response, httptest.NewRequest(http.MethodGet, "/api/backup/export", nil))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("export status=%d", response.Code)
	}
}
