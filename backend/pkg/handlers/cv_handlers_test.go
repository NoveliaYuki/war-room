package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"war-room/backend/pkg/config"
	"war-room/backend/pkg/database"
	"war-room/backend/pkg/models"
	"war-room/backend/pkg/repository"
	"war-room/backend/pkg/service"
)

type cvHandlerFixture struct {
	handler *Handler
	repo    *repository.Repository
	db      *sql.DB
}

func newCVHandlerFixture(t *testing.T) cvHandlerFixture {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{DataDir: dir, DBPath: filepath.Join(dir, "db.sqlite"), BackupPath: filepath.Join(dir, "backup.json"),
		AttachmentsDir: filepath.Join(dir, "attachments"), CVDir: filepath.Join(dir, "cvs"), LogosDir: filepath.Join(dir, "logos")}
	db, err := database.InitDB(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, path := range []string{cfg.AttachmentsDir, cfg.CVDir, cfg.LogosDir} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	repo := repository.New(db)
	job := &models.Job{ID: "job-1", CompanyName: "Example", PositionTitle: "Engineer", Status: models.StatusOngoing,
		SalaryType: models.SalaryUnknown, SalaryCurrency: "EUR", RecruiterType: models.RecruiterNone}
	if err := repo.InsertJob(job); err != nil {
		t.Fatal(err)
	}
	return cvHandlerFixture{handler: NewHandler(service.NewJobService(repo, cfg), nil, cfg), repo: repo, db: db}
}

func TestCVVersionHandlersListUploadDownloadAssignDelete(t *testing.T) {
	fixture := newCVHandlerFixture(t)
	list := httptest.NewRecorder()
	fixture.handler.handleListCVVersions(list, httptest.NewRequest(http.MethodGet, "/api/cv/versions", nil))
	if list.Code != http.StatusOK {
		t.Fatalf("list status=%d: %s", list.Code, list.Body.String())
	}
	uploadCVForHandler(t, fixture.handler)
	versions, err := fixture.repo.ListCVVersions()
	if err != nil || len(versions) != 1 {
		t.Fatalf("versions=%v err=%v", versions, err)
	}
	download := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/cv/versions/"+versions[0].ID+"/download", nil)
	request.SetPathValue("id", versions[0].ID)
	fixture.handler.handleDownloadCVVersion(download, request)
	if download.Code != http.StatusOK || download.Body.String() != "cv bytes" {
		t.Fatalf("download status=%d body=%q", download.Code, download.Body.String())
	}
	assignCVForHandler(t, fixture.handler, `{"version_id":"`+versions[0].ID+`"}`)
	delete := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodDelete, "/api/cv/versions/"+versions[0].ID, nil)
	request.SetPathValue("id", versions[0].ID)
	fixture.handler.handleDeleteCVVersion(delete, request)
	if delete.Code != http.StatusConflict {
		t.Fatalf("assigned delete status=%d", delete.Code)
	}
	assignCVForHandler(t, fixture.handler, `{"version_id":null}`)
	delete = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodDelete, "/api/cv/versions/"+versions[0].ID, nil)
	request.SetPathValue("id", versions[0].ID)
	fixture.handler.handleDeleteCVVersion(delete, request)
	if delete.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d: %s", delete.Code, delete.Body.String())
	}
}

func TestJobHandlersCoverListCreateReadUpdateDelete(t *testing.T) {
	fixture := newCVHandlerFixture(t)
	for _, test := range []struct {
		name   string
		handle func(http.ResponseWriter, *http.Request)
		method string
		path   string
		body   string
		id     string
		status int
	}{
		{name: "health", handle: fixture.handler.handleHealthCheck, method: http.MethodGet, path: "/api/health", status: http.StatusOK},
		{name: "counts", handle: fixture.handler.handleJobCounts, method: http.MethodGet, path: "/api/jobs/counts", status: http.StatusOK},
		{name: "list", handle: fixture.handler.handleListJobs, method: http.MethodGet, path: "/api/jobs?status=ongoing&search=Example", status: http.StatusOK},
		{name: "read", handle: fixture.handler.handleGetJob, method: http.MethodGet, path: "/api/jobs/job-1", id: "job-1", status: http.StatusOK},
		{name: "create", handle: fixture.handler.handleCreateJob, method: http.MethodPost, path: "/api/jobs", body: `{"company_name":"New Co","position_title":"Engineer","create_default_stages":false}`, status: http.StatusCreated},
	} {
		t.Run(test.name, func(t *testing.T) {
			invokeJobHandler(t, test.handle, test.method, test.path, test.body, test.id, test.status)
		})
	}
	update := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/api/jobs/job-1", bytes.NewBufferString(`{"position_title":"Updated Engineer"}`))
	request.SetPathValue("id", "job-1")
	fixture.handler.handleUpdateJob(update, request)
	if update.Code != http.StatusOK {
		t.Fatalf("update status=%d: %s", update.Code, update.Body.String())
	}
	delete := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodDelete, "/api/jobs/job-1", nil)
	request.SetPathValue("id", "job-1")
	fixture.handler.handleDeleteJob(delete, request)
	if delete.Code != http.StatusOK {
		t.Fatalf("delete status=%d: %s", delete.Code, delete.Body.String())
	}
}

func TestStageQuestionHandlersCoverLifecycle(t *testing.T) {
	fixture := newCVHandlerFixture(t)
	createdStage := callJSONHandler(t, fixture.handler.handleCreateStage, http.MethodPost, `{"job_id":"job-1","stage_type":"Technical"}`, http.StatusCreated)
	stageID := createdStage["id"].(string)
	callIDJSONHandler(t, fixture.handler.handleUpdateStage, stageID, `{"description":"Updated"}`, http.StatusOK)
	callIDHandler(t, fixture.handler.handleSetCurrentStage, stageID, http.MethodPut, http.StatusOK)
	callJSONHandler(t, fixture.handler.handleReorderStages, http.MethodPut, `{"job_id":"job-1","stage_ids":["`+stageID+`"]}`, http.StatusOK)
	callJSONHandler(t, fixture.handler.handleScheduleStage, http.MethodPut, `{"meeting_date":"2030-06-15","meeting_time":"10:00","meeting_type":"video"}`, http.StatusOK, stageID)
	createdQuestion := callJSONHandler(t, fixture.handler.handleCreateQuestion, http.MethodPost, `{"stage_id":"`+stageID+`","question":"Describe the architecture"}`, http.StatusCreated)
	questionID := createdQuestion["id"].(string)
	callIDJSONHandler(t, fixture.handler.handleUpdateQuestion, questionID, `{"answer_notes":"Consider tradeoffs","is_asked":true}`, http.StatusOK)
	callJSONHandler(t, fixture.handler.handleReorderQuestions, http.MethodPut, `{"stage_id":"`+stageID+`","question_ids":["`+questionID+`"]}`, http.StatusOK)
	callIDHandler(t, fixture.handler.handleDeleteQuestion, questionID, http.MethodDelete, http.StatusOK)
	callIDHandler(t, fixture.handler.handleDeleteStage, stageID, http.MethodDelete, http.StatusOK)
}

func callJSONHandler(t *testing.T, handler func(http.ResponseWriter, *http.Request), method, body string, status int, pathID ...string) map[string]any {
	t.Helper()
	path := "/api/test"
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if len(pathID) > 0 {
		request.SetPathValue("id", pathID[0])
	}
	recorder := httptest.NewRecorder()
	handler(recorder, request)
	if recorder.Code != status {
		t.Fatalf("handler status=%d want=%d: %s", recorder.Code, status, recorder.Body.String())
	}
	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		return map[string]any{}
	}
	return response
}

func callIDJSONHandler(t *testing.T, handler func(http.ResponseWriter, *http.Request), id, body string, status int) {
	t.Helper()
	callJSONHandler(t, handler, http.MethodPut, body, status, id)
}

func callIDHandler(t *testing.T, handler func(http.ResponseWriter, *http.Request), id, method string, status int) {
	t.Helper()
	request := httptest.NewRequest(method, "/api/test", nil)
	request.SetPathValue("id", id)
	recorder := httptest.NewRecorder()
	handler(recorder, request)
	if recorder.Code != status {
		t.Fatalf("handler id=%s status=%d want=%d: %s", id, recorder.Code, status, recorder.Body.String())
	}
}

func invokeJobHandler(t *testing.T, handle func(http.ResponseWriter, *http.Request), method, path, body, id string, status int) {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if id != "" {
		request.SetPathValue("id", id)
	}
	handle(recorder, request)
	if recorder.Code != status {
		t.Fatalf("%s status=%d want=%d: %s", path, recorder.Code, status, recorder.Body.String())
	}
}

func TestCVVersionHandlersRejectInvalidRequests(t *testing.T) {
	fixture := newCVHandlerFixture(t)
	badUpload := httptest.NewRecorder()
	fixture.handler.handleUploadCVVersion(badUpload, httptest.NewRequest(http.MethodPost, "/api/cv/versions", bytes.NewBufferString("bad")))
	if badUpload.Code != http.StatusBadRequest {
		t.Fatalf("malformed upload status=%d", badUpload.Code)
	}
	noFile := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/cv/versions", nil)
	request.Header.Set("Content-Type", "multipart/form-data; boundary=not-found")
	fixture.handler.handleUploadCVVersion(noFile, request)
	if noFile.Code != http.StatusBadRequest {
		t.Fatalf("missing upload file status=%d", noFile.Code)
	}
	missing := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/api/cv/versions/missing/download", nil)
	request.SetPathValue("id", "missing")
	fixture.handler.handleDownloadCVVersion(missing, request)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing download status=%d", missing.Code)
	}
	assign := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPut, "/api/jobs/missing/cv-version", bytes.NewBufferString("{"))
	request.SetPathValue("id", "missing")
	fixture.handler.handleAssignCVVersion(assign, request)
	if assign.Code != http.StatusBadRequest {
		t.Fatalf("invalid assign status=%d", assign.Code)
	}
	delete := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodDelete, "/api/cv/versions/missing", nil)
	request.SetPathValue("id", "missing")
	fixture.handler.handleDeleteCVVersion(delete, request)
	if delete.Code != http.StatusNotFound {
		t.Fatalf("missing delete status=%d", delete.Code)
	}
	assign = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPut, "/api/jobs/job-1/cv-version", bytes.NewBufferString(`{"version_id":"missing"}`))
	request.SetPathValue("id", "job-1")
	fixture.handler.handleAssignCVVersion(assign, request)
	if assign.Code != http.StatusNotFound {
		t.Fatalf("missing version assign status=%d", assign.Code)
	}
	if err := fixture.db.Close(); err != nil {
		t.Fatal(err)
	}
	list := httptest.NewRecorder()
	fixture.handler.handleListCVVersions(list, httptest.NewRequest(http.MethodGet, "/api/cv/versions", nil))
	if list.Code != http.StatusInternalServerError {
		t.Fatalf("list error status=%d", list.Code)
	}
}

func uploadCVForHandler(t *testing.T, handler *Handler) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "resume.pdf")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("cv bytes"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/cv/versions", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()
	handler.handleUploadCVVersion(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("upload status=%d: %s", recorder.Code, recorder.Body.String())
	}
	var response models.CVVersion
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil || response.ID == "" {
		t.Fatalf("invalid upload response: %v %s", err, recorder.Body.String())
	}
}

func assignCVForHandler(t *testing.T, handler *Handler, body string) {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/api/jobs/job-1/cv-version", bytes.NewBufferString(body))
	request.SetPathValue("id", "job-1")
	handler.handleAssignCVVersion(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("assign status=%d: %s", recorder.Code, recorder.Body.String())
	}
}
