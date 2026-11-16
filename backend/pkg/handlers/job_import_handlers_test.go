package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"war-room/backend/pkg/models"
)

func previewImport(fixture cvHandlerFixture, body string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := withUserID(httptest.NewRequest(http.MethodPost, "/api/jobs/import-preview", strings.NewReader(body)), testUserID)
	fixture.handler.handleJobImportPreview(recorder, request)
	return recorder
}

func TestJobImportPreviewMatchesPastedTextWithoutFetching(t *testing.T) {
	fixture := newCVHandlerFixture(t)
	recorder := previewImport(fixture, `{"description":"## Requirements\n- Python"}`)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"name":"Python"`) {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), `"draft"`) {
		t.Fatalf("pasted text produced a draft: %s", recorder.Body.String())
	}
}

func TestJobImportPreviewReportsCodes(t *testing.T) {
	fixture := newCVHandlerFixture(t)
	fixture.handler.cfg.JobImportEnabled = true
	for body, want := range map[string]int{
		`{`:                                http.StatusBadRequest,
		`{"url":"http://example.com/job"}`: http.StatusBadRequest,
		`{"url":"https://www.linkedin.com/jobs/view/1"}`: http.StatusUnprocessableEntity,
	} {
		if recorder := previewImport(fixture, body); recorder.Code != want {
			t.Errorf("%s: status=%d body=%s", body, recorder.Code, recorder.Body.String())
		}
	}
}

func TestJobImportPreviewDisabledSendsNothing(t *testing.T) {
	fixture := newCVHandlerFixture(t)
	fixture.handler.cfg.JobImportEnabled = false
	recorder := previewImport(fixture, `{"url":"https://example.com/job"}`)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestJobImportReportsSavedPosting(t *testing.T) {
	fixture := newCVHandlerFixture(t)
	fixture.handler.cfg.JobImportEnabled = true
	saved := "https://www.linkedin.com/jobs/view/1?trk=feed"
	if _, err := fixture.handler.jobs.CreateJob(testUserID, models.CreateJobInput{PositionTitle: "Engineer", CompanyName: "Acme", JobPostURL: &saved}); err != nil {
		t.Fatal(err)
	}
	recorder := previewImport(fixture, `{"url":"https://www.linkedin.com/jobs/view/1/"}`)
	var body struct {
		Code      string              `json:"code"`
		Duplicate *jobImportDuplicate `json:"duplicate"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != "paste_required" || body.Duplicate == nil || body.Duplicate.CompanyName != "Acme" {
		t.Fatalf("body = %s", recorder.Body.String())
	}
}

func TestMatchesPostingIgnoresEmptyAndInvalidLinks(t *testing.T) {
	invalid := "://"
	if matchesPosting(nil, "https://a.example") || matchesPosting(&invalid, "https://a.example") {
		t.Fatal("empty or invalid link matched")
	}
}

func TestWriteJobImportErrorHidesUnknownFailures(t *testing.T) {
	fixture := newCVHandlerFixture(t)
	recorder := httptest.NewRecorder()
	fixture.handler.writeJobImportError(recorder, testUserID, "https://a.example", errors.New("internal detail"))
	if recorder.Code != http.StatusInternalServerError || strings.Contains(recorder.Body.String(), "internal detail") {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if fixture.handler.findDuplicateJob(testUserID, "not a url") != nil {
		t.Fatal("invalid link matched a job")
	}
}
