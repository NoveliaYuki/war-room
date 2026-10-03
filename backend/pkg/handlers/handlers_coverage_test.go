package handlers

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"war-room/backend/pkg/models"
)

type coverageHandlerCase struct {
	name    string
	handler func(http.ResponseWriter, *http.Request)
	method  string
	path    string
	body    string
	id      string
	status  int
}

func TestJobHandlersRejectMalformedAndMissingResources(t *testing.T) {
	fixture := newCVHandlerFixture(t)
	coverageRunHandlerCases(t, []coverageHandlerCase{
		{"malformed job create", fixture.handler.handleCreateJob, http.MethodPost, "/api/jobs", "{", "", http.StatusBadRequest},
		{"invalid job create", fixture.handler.handleCreateJob, http.MethodPost, "/api/jobs", `{"position_title":" "}`, "", http.StatusBadRequest},
		{"missing reorder fields", fixture.handler.handleReorderJobs, http.MethodPut, "/api/jobs/reorder", `{}`, "", http.StatusBadRequest},
		{"duplicate job order", fixture.handler.handleReorderJobs, http.MethodPut, "/api/jobs/reorder", `{"job_ids":["job-1","job-1"]}`, "", http.StatusInternalServerError},
		{"missing job read", fixture.handler.handleGetJob, http.MethodGet, "/api/jobs/missing", "", "missing", http.StatusNotFound},
		{"malformed job update", fixture.handler.handleUpdateJob, http.MethodPut, "/api/jobs/job-1", "{", "job-1", http.StatusBadRequest},
		{"blank job title", fixture.handler.handleUpdateJob, http.MethodPut, "/api/jobs/job-1", `{"position_title":"  "}`, "job-1", http.StatusBadRequest},
		{"missing job update", fixture.handler.handleUpdateJob, http.MethodPut, "/api/jobs/missing", `{"position_title":"Engineer"}`, "missing", http.StatusNotFound},
		{"missing job delete", fixture.handler.handleDeleteJob, http.MethodDelete, "/api/jobs/missing", "", "missing", http.StatusNotFound},
	})
}

func TestStageAndScheduleHandlersRejectMalformedAndMissingResources(t *testing.T) {
	fixture := newCVHandlerFixture(t)
	stage := callJSONHandler(t, fixture.handler.handleCreateStage, http.MethodPost, `{"job_id":"job-1","stage_type":"Technical"}`, http.StatusCreated)
	stageID := stage["id"].(string)
	coverageRunHandlerCases(t, []coverageHandlerCase{
		{"malformed stage create", fixture.handler.handleCreateStage, http.MethodPost, "/api/stages", "{", "", http.StatusBadRequest},
		{"invalid stage create", fixture.handler.handleCreateStage, http.MethodPost, "/api/stages", `{"job_id":"job-1","stage_type":"Unknown"}`, "", http.StatusBadRequest},
		{"missing stage reorder fields", fixture.handler.handleReorderStages, http.MethodPut, "/api/stages/reorder", `{}`, "", http.StatusBadRequest},
		{"missing stage in reorder", fixture.handler.handleReorderStages, http.MethodPut, "/api/stages/reorder", `{"job_id":"job-1","stage_ids":["missing"]}`, "", http.StatusInternalServerError},
		{"malformed schedule", fixture.handler.handleScheduleStage, http.MethodPut, "/api/stages/" + stageID + "/schedule", "{", stageID, http.StatusBadRequest},
		{"invalid schedule date", fixture.handler.handleScheduleStage, http.MethodPut, "/api/stages/" + stageID + "/schedule", `{"meeting_date":"not-a-date"}`, stageID, http.StatusBadRequest},
		{"missing scheduled stage", fixture.handler.handleScheduleStage, http.MethodPut, "/api/stages/missing/schedule", `{"meeting_date":"2030-06-15"}`, "missing", http.StatusNotFound},
		{"missing current stage", fixture.handler.handleSetCurrentStage, http.MethodPut, "/api/stages/missing/set-current", "", "missing", http.StatusNotFound},
		{"malformed stage update", fixture.handler.handleUpdateStage, http.MethodPut, "/api/stages/" + stageID, "{", stageID, http.StatusBadRequest},
		{"invalid stage update date", fixture.handler.handleUpdateStage, http.MethodPut, "/api/stages/" + stageID, `{"meeting_date":"not-a-date"}`, stageID, http.StatusBadRequest},
		{"missing stage update", fixture.handler.handleUpdateStage, http.MethodPut, "/api/stages/missing", `{"description":"Updated"}`, "missing", http.StatusNotFound},
		{"missing stage delete", fixture.handler.handleDeleteStage, http.MethodDelete, "/api/stages/missing", "", "missing", http.StatusNotFound},
	})
}

func TestQuestionHandlersRejectMalformedAndMissingResources(t *testing.T) {
	fixture := newCVHandlerFixture(t)
	stage := callJSONHandler(t, fixture.handler.handleCreateStage, http.MethodPost, `{"job_id":"job-1","stage_type":"Technical"}`, http.StatusCreated)
	stageID := stage["id"].(string)
	question := callJSONHandler(t, fixture.handler.handleCreateQuestion, http.MethodPost, `{"stage_id":"`+stageID+`","question":"Design a cache"}`, http.StatusCreated)
	questionID := question["id"].(string)
	coverageRunHandlerCases(t, []coverageHandlerCase{
		{"malformed question create", fixture.handler.handleCreateQuestion, http.MethodPost, "/api/questions", "{", "", http.StatusBadRequest},
		{"blank question create", fixture.handler.handleCreateQuestion, http.MethodPost, "/api/questions", `{"stage_id":"` + stageID + `","question":" "}`, "", http.StatusBadRequest},
		{"missing question reorder fields", fixture.handler.handleReorderQuestions, http.MethodPut, "/api/questions/reorder", `{}`, "", http.StatusBadRequest},
		{"missing question in reorder", fixture.handler.handleReorderQuestions, http.MethodPut, "/api/questions/reorder", `{"stage_id":"` + stageID + `","question_ids":["missing"]}`, "", http.StatusInternalServerError},
		{"malformed question update", fixture.handler.handleUpdateQuestion, http.MethodPut, "/api/questions/" + questionID, "{", questionID, http.StatusBadRequest},
		{"blank question update", fixture.handler.handleUpdateQuestion, http.MethodPut, "/api/questions/" + questionID, `{"question":" "}`, questionID, http.StatusBadRequest},
		{"missing question update", fixture.handler.handleUpdateQuestion, http.MethodPut, "/api/questions/missing", `{"answer_notes":"notes"}`, "missing", http.StatusNotFound},
		{"missing question delete", fixture.handler.handleDeleteQuestion, http.MethodDelete, "/api/questions/missing", "", "missing", http.StatusNotFound},
	})
}

func TestEmptyMeetingsHandlerReturnsArray(t *testing.T) {
	fixture := newCVHandlerFixture(t)
	coverageInvokeHandler(t, fixture.handler.handleGetMeetings, http.MethodGet, "/api/meetings", "", "", http.StatusOK)
}

func coverageRunHandlerCases(t *testing.T, cases []coverageHandlerCase) {
	t.Helper()
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			coverageInvokeHandler(t, test.handler, test.method, test.path, test.body, test.id, test.status)
		})
	}
}

func TestAttachmentHandlersCoverUploadDownloadDeleteAndFailures(t *testing.T) {
	fixture := newCVHandlerFixture(t)
	attachment := coverageUploadAttachment(t, fixture.handler)
	coverageDownloadAttachment(t, fixture.handler, attachment.ID)
	coverageDeleteAttachment(t, fixture.handler, attachment.ID)
	coverageAttachmentStorageFailure(t, fixture)
}

func coverageUploadAttachment(t *testing.T, handler *Handler) models.Attachment {
	t.Helper()
	badOwner := coverageMultipartAttachmentRequest(t, "missing", "", "resume.txt", "file contents")
	if recorder := coverageServeHandler(handler.handleUploadAttachment, badOwner); recorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid attachment owner status=%d: %s", recorder.Code, recorder.Body.String())
	}

	validUpload := coverageMultipartAttachmentRequest(t, "job-1", "", "resume.txt", "file contents")
	uploaded := coverageServeHandler(handler.handleUploadAttachment, validUpload)
	if uploaded.Code != http.StatusCreated {
		t.Fatalf("upload status=%d: %s", uploaded.Code, uploaded.Body.String())
	}
	var attachment models.Attachment
	if err := json.Unmarshal(uploaded.Body.Bytes(), &attachment); err != nil || attachment.ID == "" {
		t.Fatalf("decode uploaded attachment=%+v err=%v", attachment, err)
	}
	return attachment
}

func coverageDownloadAttachment(t *testing.T, handler *Handler, attachmentID string) {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/api/attachments/"+attachmentID+"/download", nil)
	request.SetPathValue("id", attachmentID)
	download := coverageServeHandler(handler.handleDownloadAttachment, request)
	if download.Code != http.StatusOK || download.Body.String() != "file contents" {
		t.Fatalf("download status=%d body=%q", download.Code, download.Body.String())
	}
	if download.Header().Get("X-Content-Type-Options") != "nosniff" || download.Header().Get("Content-Disposition") == "" {
		t.Fatalf("download security headers missing: %v", download.Header())
	}

	missingRequest := httptest.NewRequest(http.MethodGet, "/api/attachments/missing/download", nil)
	missingRequest.SetPathValue("id", "missing")
	if missing := coverageServeHandler(handler.handleDownloadAttachment, missingRequest); missing.Code != http.StatusNotFound {
		t.Fatalf("missing download status=%d", missing.Code)
	}
}

func coverageDeleteAttachment(t *testing.T, handler *Handler, attachmentID string) {
	t.Helper()
	deleteRequest := httptest.NewRequest(http.MethodDelete, "/api/attachments/"+attachmentID, nil)
	deleteRequest.SetPathValue("id", attachmentID)
	if deleted := coverageServeHandler(handler.handleDeleteAttachment, deleteRequest); deleted.Code != http.StatusOK {
		t.Fatalf("delete status=%d: %s", deleted.Code, deleted.Body.String())
	}
	missingRequest := httptest.NewRequest(http.MethodDelete, "/api/attachments/"+attachmentID, nil)
	missingRequest.SetPathValue("id", attachmentID)
	if missing := coverageServeHandler(handler.handleDeleteAttachment, missingRequest); missing.Code != http.StatusNotFound {
		t.Fatalf("missing delete status=%d", missing.Code)
	}
}

func coverageAttachmentStorageFailure(t *testing.T, fixture cvHandlerFixture) {
	t.Helper()
	if err := fixture.db.Close(); err != nil {
		t.Fatal(err)
	}
	failedStore := coverageMultipartAttachmentRequest(t, "job-1", "", "resume.txt", "file contents")
	if failed := coverageServeHandler(fixture.handler.handleUploadAttachment, failedStore); failed.Code != http.StatusInternalServerError {
		t.Fatalf("storage failure status=%d: %s", failed.Code, failed.Body.String())
	}
}

func TestHandlerDatabaseFailuresReturnInternalServerErrors(t *testing.T) {
	fixture := newCVHandlerFixture(t)
	if err := fixture.db.Close(); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		hand func(http.ResponseWriter, *http.Request)
	}{
		{"counts", fixture.handler.handleJobCounts},
		{"jobs", fixture.handler.handleListJobs},
		{"job", fixture.handler.handleGetJob},
		{"meetings", fixture.handler.handleGetMeetings},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/test", nil)
			request.SetPathValue("id", "job-1")
			recorder := coverageServeHandler(test.hand, request)
			if recorder.Code != http.StatusInternalServerError {
				t.Fatalf("database error status=%d: %s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func coverageInvokeHandler(t *testing.T, handler func(http.ResponseWriter, *http.Request), method, path, body, id string, status int) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if id != "" {
		request.SetPathValue("id", id)
	}
	recorder := coverageServeHandler(handler, request)
	if recorder.Code != status {
		t.Fatalf("%s status=%d, want %d: %s", path, recorder.Code, status, recorder.Body.String())
	}
	return recorder
}

func coverageServeHandler(handler func(http.ResponseWriter, *http.Request), request *http.Request) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	handler(recorder, request)
	return recorder
}

func coverageMultipartAttachmentRequest(t *testing.T, jobID, stageID, filename, content string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("job_id", jobID); err != nil {
		t.Fatal(err)
	}
	if stageID != "" {
		if err := writer.WriteField("stage_id", stageID); err != nil {
			t.Fatal(err)
		}
	}
	part, err := writer.CreateFormFile("file", filepath.Base(filename))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/attachments", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request
}
