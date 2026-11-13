package mcp

import (
	"database/sql"
	"fmt"
	"slices"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"war-room/backend/pkg/models"
	"war-room/backend/pkg/repository"
	"war-room/backend/pkg/service"
)

// insertCVVersion stores a CV version record for userID without a file.
func insertCVVersion(t *testing.T, db *sql.DB, userID, id string) {
	t.Helper()
	version := &models.CVVersion{ID: id, OriginalName: id + ".pdf", StoredFilename: id + ".pdf", FileSize: 1, SHA256: id, UploadedAt: 1}
	if err := repository.New(db).InsertCVVersion(userID, version); err != nil {
		t.Fatal(err)
	}
}

// insertStrangerJob gives strangerID a job "job-2" of their own.
func insertStrangerJob(t *testing.T, db *sql.DB) {
	t.Helper()
	job := &models.Job{ID: "job-2", CompanyName: "Other", PositionTitle: "Analyst", Status: models.StatusWaiting,
		SalaryType: models.SalaryUnknown, SalaryCurrency: "EUR", RecruiterType: models.RecruiterNone}
	if err := repository.New(db).InsertJob(strangerID, job); err != nil {
		t.Fatal(err)
	}
}

func stageIDs(t *testing.T, job map[string]any) []string {
	t.Helper()
	var ids []string
	for _, stage := range job["stages"].([]any) {
		ids = append(ids, stage.(map[string]any)["id"].(string))
	}
	return ids
}

func TestCountJobsCountsOnlyTheUsersJobsByStatus(t *testing.T) {
	jobs, _ := newJobService(t)
	owner, _ := connect(t, jobs, ownerID)
	mustCall(t, owner, "create_job", map[string]any{"company_name": "Acme", "position_title": "Platform Engineer"})
	counts := mustCall(t, owner, "count_jobs", map[string]any{})
	if counts["all"] != 2.0 || counts["ongoing"] != 1.0 || counts["waiting"] != 1.0 || counts["rejected"] != 0.0 {
		t.Fatalf("count_jobs = %v", counts)
	}
	stranger, _ := connect(t, jobs, strangerID)
	if counts := mustCall(t, stranger, "count_jobs", map[string]any{}); counts["all"] != 0.0 {
		t.Fatalf("stranger count_jobs = %v, want no jobs", counts)
	}
}

func TestListMeetingsReturnsOnlyTheUsersScheduledStages(t *testing.T) {
	jobs, _ := newJobService(t)
	owner, _ := connect(t, jobs, ownerID)
	mustCall(t, owner, "create_stage", map[string]any{"job_id": "job-1", "stage_type": "HR"})
	stage := mustCall(t, owner, "create_stage", map[string]any{"job_id": "job-1", "stage_type": "Technical"})
	mustCall(t, owner, "update_stage", map[string]any{"id": stage["id"], "changes": map[string]any{"meeting_date": "2026-12-01", "meeting_time": "10:30"}})
	listed := mustCall(t, owner, "list_meetings", map[string]any{})
	meetings := listed["meetings"].([]any)
	if len(meetings) != 1 {
		t.Fatalf("list_meetings = %v, want the one scheduled stage", listed)
	}
	if meeting := meetings[0].(map[string]any); meeting["stage_id"] != stage["id"] || meeting["meeting_date"] != "2026-12-01" || meeting["company_name"] != "Example" {
		t.Fatalf("meeting = %v", meeting)
	}
	stranger, _ := connect(t, jobs, strangerID)
	if listed := mustCall(t, stranger, "list_meetings", map[string]any{}); len(listed["meetings"].([]any)) != 0 {
		t.Fatalf("stranger list_meetings = %v", listed)
	}
}

func TestSetJobCVVersionLinksAndClearsTheUsersOwnVersion(t *testing.T) {
	jobs, db := newJobService(t)
	insertCVVersion(t, db, ownerID, "cv-owner")
	insertCVVersion(t, db, strangerID, "cv-stranger")
	insertStrangerJob(t, db)
	owner, _ := connect(t, jobs, ownerID)
	job := mustCall(t, owner, "set_job_cv_version", map[string]any{"id": "job-1", "version_id": "cv-owner"})
	if selected, _ := job["selected_cv_version"].(map[string]any); selected["id"] != "cv-owner" {
		t.Fatalf("set_job_cv_version = %v, want cv-owner selected", job["selected_cv_version"])
	}
	for _, test := range []struct {
		session   string
		arguments map[string]any
	}{
		{ownerID, map[string]any{"id": "job-1", "version_id": "cv-stranger"}},
		{ownerID, map[string]any{"id": "job-1", "version_id": "cv-missing"}},
		{ownerID, map[string]any{"id": "job-missing", "version_id": "cv-owner"}},
		{strangerID, map[string]any{"id": "job-1", "version_id": "cv-stranger"}},
		{strangerID, map[string]any{"id": "job-1", "version_id": nil}},
		{strangerID, map[string]any{"id": "job-2", "version_id": "cv-owner"}},
	} {
		session, _ := connect(t, jobs, test.session)
		if _, message := call(t, session, "set_job_cv_version", test.arguments); message != "not found" {
			t.Errorf("%s set_job_cv_version(%v) = %q, want not found", test.session, test.arguments, message)
		}
	}
	if job := mustCall(t, owner, "get_job", map[string]any{"id": "job-1"}); job["selected_cv_version"].(map[string]any)["id"] != "cv-owner" {
		t.Fatalf("a rejected call changed the CV version: %v", job["selected_cv_version"])
	}
	if job := mustCall(t, owner, "set_job_cv_version", map[string]any{"id": "job-1", "version_id": nil}); job["selected_cv_version"] != nil {
		t.Fatalf("version_id null left %v selected", job["selected_cv_version"])
	}
	if _, message := call(t, owner, "set_job_cv_version", map[string]any{"id": "job-1"}); message == "" {
		t.Fatal("set_job_cv_version without version_id was accepted")
	}
}

func TestReorderStagesStoresTheGivenOrder(t *testing.T) {
	jobs, db := newJobService(t)
	insertStrangerJob(t, db)
	owner, _ := connect(t, jobs, ownerID)
	first := mustCall(t, owner, "create_stage", map[string]any{"job_id": "job-1", "stage_type": "HR"})
	second := mustCall(t, owner, "create_stage", map[string]any{"job_id": "job-1", "stage_type": "Technical"})
	want := []string{second["id"].(string), first["id"].(string)}
	result := mustCall(t, owner, "reorder_stages", map[string]any{"job_id": "job-1", "stage_ids": want})
	if result["job_id"] != "job-1" {
		t.Fatalf("reorder_stages = %v", result)
	}
	if got := stageIDs(t, mustCall(t, owner, "get_job", map[string]any{"id": "job-1"})); !slices.Equal(got, want) {
		t.Fatalf("stage order = %v, want %v", got, want)
	}
	for _, test := range []struct {
		arguments map[string]any
		want      string
	}{
		{map[string]any{"job_id": "job-1", "stage_ids": []string{}}, service.ErrInvalidField.Error() + ": stage_ids must list the job's stages"},
		{map[string]any{"job_id": "job-1", "stage_ids": []string{want[0], want[0]}}, errInvalidOrdering.Error()},
		{map[string]any{"job_id": "job-1", "stage_ids": []string{"stage-missing"}}, errInvalidOrdering.Error()},
		{map[string]any{"job_id": "job-missing", "stage_ids": want}, "not found"},
	} {
		if _, message := call(t, owner, "reorder_stages", test.arguments); message != test.want {
			t.Errorf("reorder_stages(%v) = %q, want %q", test.arguments, message, test.want)
		}
	}
	stranger, _ := connect(t, jobs, strangerID)
	if _, message := call(t, stranger, "reorder_stages", map[string]any{"job_id": "job-1", "stage_ids": want}); message != "not found" {
		t.Errorf("stranger reorder_stages on the owner's job = %q, want not found", message)
	}
	if _, message := call(t, stranger, "reorder_stages", map[string]any{"job_id": "job-2", "stage_ids": want}); message != errInvalidOrdering.Error() {
		t.Errorf("stranger reorder_stages with the owner's stages = %q, want %q", message, errInvalidOrdering)
	}
	if got := stageIDs(t, mustCall(t, owner, "get_job", map[string]any{"id": "job-1"})); !slices.Equal(got, want) {
		t.Fatalf("a rejected reorder changed the stage order to %v", got)
	}
}

// questionTexts returns the question texts of a job's first stage in order.
func questionTexts(t *testing.T, job map[string]any) []string {
	t.Helper()
	var texts []string
	for _, question := range job["stages"].([]any)[0].(map[string]any)["questions"].([]any) {
		texts = append(texts, question.(map[string]any)["question"].(string))
	}
	return texts
}

func TestQuestionsAreCreatedUpdatedAndReordered(t *testing.T) {
	jobs, _ := newJobService(t)
	owner, _ := connect(t, jobs, ownerID)
	stage := mustCall(t, owner, "create_stage", map[string]any{"job_id": "job-1", "stage_type": "Technical"})
	first := mustCall(t, owner, "create_question", map[string]any{"stage_id": stage["id"], "question": "  Why this team?  ", "answer_notes": "Platform work"})
	second := mustCall(t, owner, "create_question", map[string]any{"stage_id": stage["id"], "question": "What is on call like?"})
	if first["question"] != "Why this team?" || first["is_asked"] != false {
		t.Fatalf("create_question = %v", first)
	}
	updated := mustCall(t, owner, "update_question", map[string]any{"id": first["id"], "changes": map[string]any{"is_asked": true}})
	if updated["is_asked"] != true || updated["question"] != "Why this team?" || updated["answer_notes"] != "Platform work" {
		t.Fatalf("update_question changed unrelated fields: %v", updated)
	}
	if unchanged := mustCall(t, owner, "update_question", map[string]any{"id": first["id"], "changes": map[string]any{}}); unchanged["is_asked"] != true {
		t.Fatalf("empty update_question = %v", unchanged)
	}
	mustCall(t, owner, "reorder_questions", map[string]any{"stage_id": stage["id"], "question_ids": []any{second["id"], first["id"]}})
	if got := questionTexts(t, mustCall(t, owner, "get_job", map[string]any{"id": "job-1"})); !slices.Equal(got, []string{"What is on call like?", "Why this team?"}) {
		t.Fatalf("question order = %v", got)
	}
	for _, test := range []struct {
		name      string
		arguments map[string]any
		want      string
	}{
		{"create_question", map[string]any{"stage_id": stage["id"], "question": " "}, service.ErrQuestionTextRequired.Error()},
		{"create_question", map[string]any{"stage_id": "stage-missing", "question": "Why?"}, "not found"},
		{"update_question", map[string]any{"id": first["id"], "changes": map[string]any{"question": ""}}, service.ErrQuestionTextRequired.Error()},
		{"update_question", map[string]any{"id": "question-missing", "changes": map[string]any{}}, "not found"},
		{"update_question", map[string]any{"id": "question-missing", "changes": map[string]any{"is_asked": true}}, "not found"},
		{"reorder_questions", map[string]any{"stage_id": stage["id"], "question_ids": []string{}}, service.ErrInvalidField.Error() + ": question_ids must list the stage's questions"},
		{"reorder_questions", map[string]any{"stage_id": stage["id"], "question_ids": []any{first["id"], first["id"]}}, errInvalidOrdering.Error()},
		{"reorder_questions", map[string]any{"stage_id": "stage-missing", "question_ids": []any{first["id"]}}, "not found"},
	} {
		if _, message := call(t, owner, test.name, test.arguments); message != test.want {
			t.Errorf("%s(%v) = %q, want %q", test.name, test.arguments, message, test.want)
		}
	}
}

func TestAnotherUsersQuestionsAndPeriodsAreNotFound(t *testing.T) {
	jobs, _ := newJobService(t)
	owner, _ := connect(t, jobs, ownerID)
	stage := mustCall(t, owner, "create_stage", map[string]any{"job_id": "job-1", "stage_type": "HR"})
	question := mustCall(t, owner, "create_question", map[string]any{"stage_id": stage["id"], "question": "Why us?"})
	period := mustCall(t, owner, "create_search_period", map[string]any{"name": "Spring", "start_date": "2026-03-01"})
	stranger, _ := connect(t, jobs, strangerID)
	for _, test := range []struct {
		name      string
		arguments map[string]any
	}{
		{"create_question", map[string]any{"stage_id": stage["id"], "question": "Hijacked"}},
		{"update_question", map[string]any{"id": question["id"], "changes": map[string]any{"question": "Hijacked"}}},
		{"update_question", map[string]any{"id": question["id"], "changes": map[string]any{}}},
		{"reorder_questions", map[string]any{"stage_id": stage["id"], "question_ids": []any{question["id"]}}},
		{"update_search_period", map[string]any{"id": period["id"], "name": "Hijacked", "start_date": "2025-01-01"}},
	} {
		if _, message := call(t, stranger, test.name, test.arguments); message != "not found" {
			t.Errorf("stranger %s = %q, want not found", test.name, message)
		}
	}
	if listed := mustCall(t, stranger, "list_search_periods", map[string]any{}); len(listed["search_periods"].([]any)) != 0 {
		t.Fatalf("stranger lists %v", listed)
	}
	job := mustCall(t, owner, "get_job", map[string]any{"id": "job-1"})
	if got := questionTexts(t, job); !slices.Equal(got, []string{"Why us?"}) {
		t.Fatalf("stranger changed the owner's questions: %v", got)
	}
	periods := mustCall(t, owner, "list_search_periods", map[string]any{})["search_periods"].([]any)
	if len(periods) != 1 || periods[0].(map[string]any)["name"] != "Spring" {
		t.Fatalf("stranger changed the owner's periods: %v", periods)
	}
}

func TestSearchPeriodsAreCreatedAndReplaced(t *testing.T) {
	jobs, _ := newJobService(t)
	owner, _ := connect(t, jobs, ownerID)
	if listed := mustCall(t, owner, "list_search_periods", map[string]any{}); len(listed["search_periods"].([]any)) != 0 {
		t.Fatalf("new account periods = %v", listed)
	}
	spring := mustCall(t, owner, "create_search_period", map[string]any{"name": " Spring ", "start_date": "2026-03-01", "end_date": "2026-05-31"})
	if spring["name"] != "Spring" || spring["end_date"] != "2026-05-31" {
		t.Fatalf("create_search_period = %v", spring)
	}
	autumn := mustCall(t, owner, "create_search_period", map[string]any{"name": "Autumn", "start_date": "2026-09-01"})
	if result := mustCall(t, owner, "update_search_period", map[string]any{"id": spring["id"], "name": "Early spring", "start_date": "2026-02-01", "end_date": "2026-04-30"}); result["id"] != spring["id"] || result["success"] != true {
		t.Fatalf("update_search_period = %v", result)
	}
	assertPeriodsAfterUpdate(t, owner, autumn["id"])
}

// assertPeriodsAfterUpdate checks autumn is listed first and the replaced
// spring period carries its new name and dates.
func assertPeriodsAfterUpdate(t *testing.T, owner *sdk.ClientSession, autumnID any) {
	t.Helper()
	periods := mustCall(t, owner, "list_search_periods", map[string]any{})["search_periods"].([]any)
	if len(periods) != 2 || periods[0].(map[string]any)["id"] != autumnID {
		t.Fatalf("periods = %v, want autumn first", periods)
	}
	if renamed := periods[1].(map[string]any); renamed["name"] != "Early spring" || renamed["start_date"] != "2026-02-01" || renamed["end_date"] != "2026-04-30" {
		t.Fatalf("updated period = %v", renamed)
	}
}

func TestInvalidSearchPeriodsAreRejected(t *testing.T) {
	jobs, _ := newJobService(t)
	owner, _ := connect(t, jobs, ownerID)
	spring := mustCall(t, owner, "create_search_period", map[string]any{"name": "Spring", "start_date": "2026-02-01", "end_date": "2026-04-30"})
	mustCall(t, owner, "create_search_period", map[string]any{"name": "Autumn", "start_date": "2026-09-01"})
	invalid := service.ErrInvalidField.Error() + ": "
	for _, test := range []struct {
		name      string
		arguments map[string]any
		want      string
	}{
		{"create_search_period", map[string]any{"name": "Overlap", "start_date": "2026-10-01"}, invalid + "search period dates overlap an existing period"},
		{"create_search_period", map[string]any{"name": "Bad", "start_date": "2026-02-30"}, invalid + "dates must be valid YYYY-MM-DD calendar dates"},
		{"create_search_period", map[string]any{"name": " ", "start_date": "2025-01-01"}, invalid + "period name must contain 1 to 100 characters"},
		{"update_search_period", map[string]any{"id": spring["id"], "name": "Spring", "start_date": "2026-03-01"}, invalid + "search period dates overlap an existing period"},
		{"update_search_period", map[string]any{"id": spring["id"], "name": "Spring", "start_date": "2026-04-01", "end_date": "2026-03-01"}, invalid + "end date must be on or after start date"},
		{"update_search_period", map[string]any{"id": "period-missing", "name": "Old", "start_date": "2020-01-01", "end_date": "2020-02-01"}, "not found"},
	} {
		if _, message := call(t, owner, test.name, test.arguments); message != test.want {
			t.Errorf("%s(%v) = %q, want %q", test.name, test.arguments, message, test.want)
		}
	}
}

func TestReorderErrorsReachTheClientFromEveryTool(t *testing.T) {
	session, _ := connect(t, &fakeJobs{err: fmt.Errorf("%w: stage %q", repository.ErrInvalidReorder, "stage-1")}, ownerID)
	for _, name := range []string{"reorder_stages", "reorder_questions"} {
		if _, message := call(t, session, name, validArguments[name]); message != errInvalidOrdering.Error() {
			t.Errorf("%s = %q, want %q", name, message, errInvalidOrdering)
		}
	}
}
