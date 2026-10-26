package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"war-room/backend/pkg/models"
)

const otherUserID = "handler-user-other"

func seedOtherUser(t *testing.T, fixture cvHandlerFixture) {
	t.Helper()
	if _, err := fixture.db.Exec(`INSERT INTO users (id, google_sub, email, name, picture_url) VALUES (?, ?, ?, ?, ?)`,
		otherUserID, "sub-"+otherUserID, otherUserID+"@example.com", otherUserID, ""); err != nil {
		t.Fatal(err)
	}
}

// TestHandlersTreatAnotherUsersJobAsNotFound exercises the HTTP layer (not
// just the service layer) for the "wrong user" ownership checks: a request
// for another account's job must come back indistinguishable from a
// genuinely missing job.
func TestHandlersTreatAnotherUsersJobAsNotFound(t *testing.T) {
	fixture := newCVHandlerFixture(t)
	seedOtherUser(t, fixture)

	t.Run("get job", func(t *testing.T) {
		request := withUserID(httptest.NewRequest(http.MethodGet, "/api/jobs/job-1", nil), otherUserID)
		request.SetPathValue("id", "job-1")
		rec := httptest.NewRecorder()
		fixture.handler.handleGetJob(rec, request)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("get another user's job=%d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("create stage on another user's job", func(t *testing.T) {
		body := strings.NewReader(`{"job_id":"job-1","stage_type":"HR"}`)
		request := withUserID(httptest.NewRequest(http.MethodPost, "/api/stages", body), otherUserID)
		rec := httptest.NewRecorder()
		fixture.handler.handleCreateStage(rec, request)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("create stage on another user's job=%d body=%s", rec.Code, rec.Body.String())
		}
	})

	stage, err := fixture.handler.jobs.CreateStage(testUserID, models.CreateStageInput{JobID: "job-1", StageType: models.StageHR})
	if err != nil {
		t.Fatal(err)
	}
	t.Run("create question on another user's stage", func(t *testing.T) {
		body := strings.NewReader(`{"stage_id":"` + stage.ID + `","question":"Why us?"}`)
		request := withUserID(httptest.NewRequest(http.MethodPost, "/api/questions", body), otherUserID)
		rec := httptest.NewRecorder()
		fixture.handler.handleCreateQuestion(rec, request)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("create question on another user's stage=%d body=%s", rec.Code, rec.Body.String())
		}
	})

	// Destructive: must run last, since it breaks the stages table that the
	// subtests above still rely on.
	t.Run("database failure maps to 500 (not 404)", func(t *testing.T) {
		if _, err := fixture.db.Exec(`DROP TABLE stages`); err != nil {
			t.Fatal(err)
		}
		request := withUserID(httptest.NewRequest(http.MethodGet, "/api/jobs/job-1", nil), testUserID)
		request.SetPathValue("id", "job-1")
		rec := httptest.NewRecorder()
		fixture.handler.handleGetJob(rec, request)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("get job with broken storage=%d body=%s", rec.Code, rec.Body.String())
		}
	})
}

// TestHandleAssignCVVersionReturns404WhenJobDisappearsBeforeReload covers the
// TOCTOU window in handleAssignCVVersion: AssignCVVersion's UPDATE succeeds
// (the job exists at that instant), but the immediate GetFullJobDetails
// reload no longer finds it. A trigger stands in for a concurrent delete
// landing in between the two calls.
func TestHandleAssignCVVersionReturns404WhenJobDisappearsBeforeReload(t *testing.T) {
	fixture := newCVHandlerFixture(t)
	if _, err := fixture.db.Exec(`
		CREATE TRIGGER race_delete_job_on_assign AFTER UPDATE ON jobs
		WHEN NEW.id = 'job-1'
		BEGIN DELETE FROM jobs WHERE id = 'job-1'; END;`); err != nil {
		t.Fatal(err)
	}
	body := strings.NewReader(`{"version_id":null}`)
	request := withUserID(httptest.NewRequest(http.MethodPut, "/api/jobs/job-1/cv-version", body), testUserID)
	request.SetPathValue("id", "job-1")
	rec := httptest.NewRecorder()
	fixture.handler.handleAssignCVVersion(rec, request)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("assign with job deleted mid-request=%d body=%s", rec.Code, rec.Body.String())
	}
}
