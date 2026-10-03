package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestJobStatusUpdateAndReorderSucceed(t *testing.T) {
	fixture := newCVHandlerFixture(t)
	coverageInvokeHandler(t, fixture.handler.handleUpdateJob, http.MethodPut, "/api/jobs/job-1", `{"status":"rejected"}`, "job-1", http.StatusOK)
	coverageInvokeHandler(t, fixture.handler.handleReorderJobs, http.MethodPut, "/api/jobs/reorder", `{"job_ids":["job-1"]}`, "", http.StatusOK)
}

func TestMutationHandlersReturnInternalErrorsWhenDatabaseIsUnavailable(t *testing.T) {
	fixture := newCVHandlerFixture(t)
	stage := callJSONHandler(t, fixture.handler.handleCreateStage, http.MethodPost, `{"job_id":"job-1","stage_type":"Technical"}`, http.StatusCreated)
	stageID := stage["id"].(string)
	question := callJSONHandler(t, fixture.handler.handleCreateQuestion, http.MethodPost, `{"stage_id":"`+stageID+`","question":"Design a cache"}`, http.StatusCreated)
	questionID := question["id"].(string)
	if err := fixture.db.Close(); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		handler func(http.ResponseWriter, *http.Request)
		method  string
		path    string
		body    string
		id      string
	}{
		{"job update", fixture.handler.handleUpdateJob, http.MethodPut, "/api/jobs/job-1", `{"position_title":"Updated"}`, "job-1"},
		{"job delete", fixture.handler.handleDeleteJob, http.MethodDelete, "/api/jobs/job-1", "", "job-1"},
		{"stage update", fixture.handler.handleUpdateStage, http.MethodPut, "/api/stages/" + stageID, `{"description":"Updated"}`, stageID},
		{"stage schedule", fixture.handler.handleScheduleStage, http.MethodPut, "/api/stages/" + stageID + "/schedule", `{"meeting_date":"2030-06-15"}`, stageID},
		{"stage delete", fixture.handler.handleDeleteStage, http.MethodDelete, "/api/stages/" + stageID, "", stageID},
		{"question update", fixture.handler.handleUpdateQuestion, http.MethodPut, "/api/questions/" + questionID, `{"answer_notes":"Updated"}`, questionID},
		{"question delete", fixture.handler.handleDeleteQuestion, http.MethodDelete, "/api/questions/" + questionID, "", questionID},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
			request.SetPathValue("id", test.id)
			recorder := httptest.NewRecorder()
			test.handler(recorder, request)
			if recorder.Code != http.StatusInternalServerError {
				t.Fatalf("status=%d, want %d: %s", recorder.Code, http.StatusInternalServerError, recorder.Body.String())
			}
		})
	}
}
