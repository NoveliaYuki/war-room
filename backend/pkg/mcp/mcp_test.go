package mcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"war-room/backend/pkg/config"
	"war-room/backend/pkg/database"
	"war-room/backend/pkg/logging"
	"war-room/backend/pkg/middleware"
	"war-room/backend/pkg/models"
	"war-room/backend/pkg/repository"
	"war-room/backend/pkg/service"
)

const (
	ownerID    = "mcp-owner"
	strangerID = "mcp-stranger"
	requestID  = "mcp-test-request"
)

var allTools = []string{"list_jobs", "get_job", "create_job", "update_job", "count_jobs", "set_job_cv_version",
	"list_technologies", "create_technology", "update_technology", "list_cv_versions", "create_stage", "update_stage",
	"reorder_stages", "list_meetings", "create_question", "update_question", "reorder_questions",
	"list_search_periods", "create_search_period", "update_search_period"}

// newJobService returns a service over a fresh database holding ownerID's
// job "job-1" and an empty account for strangerID.
func newJobService(t *testing.T) (*service.JobService, *sql.DB) {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{DataDir: dir, DBPath: filepath.Join(dir, "db.sqlite"), BackupPath: filepath.Join(dir, "backup.json"),
		AttachmentsDir: filepath.Join(dir, "attachments"), CVDir: filepath.Join(dir, "cvs"), LogosDir: filepath.Join(dir, "logos")}
	db, err := database.InitDB(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, userID := range []string{ownerID, strangerID} {
		if _, err := db.Exec(`INSERT INTO users (id, google_sub, email, name, picture_url) VALUES (?, ?, ?, ?, '')`,
			userID, "sub-"+userID, userID+"@example.com", userID); err != nil {
			t.Fatal(err)
		}
	}
	repo := repository.New(db)
	job := &models.Job{ID: "job-1", CompanyName: "Example", PositionTitle: "Engineer", Status: models.StatusOngoing,
		SalaryType: models.SalaryUnknown, SalaryCurrency: "EUR", RecruiterType: models.RecruiterNone}
	if err := repo.InsertJob(ownerID, job); err != nil {
		t.Fatal(err)
	}
	return service.NewJobService(repo, cfg), db
}

// connect serves NewHandler as userID, standing in for RequireSession, and
// returns a connected SDK client session plus the request log of each call.
func connect(t *testing.T, jobs JobService, userID string) (*sdk.ClientSession, *[]*logging.Request) {
	t.Helper()
	handler := NewHandler(jobs)
	var requests []*logging.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request := logging.NewRequest()
		request.ID = requestID
		requests = append(requests, request)
		ctx := logging.WithRequest(middleware.ContextWithUserID(r.Context(), userID), request)
		handler.ServeHTTP(w, r.WithContext(ctx))
	}))
	t.Cleanup(server.Close)
	client := sdk.NewClient(&sdk.Implementation{Name: "test"}, nil)
	session, err := client.Connect(context.Background(), &sdk.StreamableClientTransport{Endpoint: server.URL, MaxRetries: -1}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session, &requests
}

// call runs a tool and returns its structured output, or the error text the
// client sees when the tool fails.
func call(t *testing.T, session *sdk.ClientSession, name string, arguments any) (map[string]any, string) {
	t.Helper()
	result, err := session.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	if result.IsError {
		return nil, result.Content[0].(*sdk.TextContent).Text
	}
	var output map[string]any
	raw, _ := json.Marshal(result.StructuredContent)
	if err := json.Unmarshal(raw, &output); err != nil {
		t.Fatalf("decode %s output: %v", name, err)
	}
	return output, ""
}

func mustCall(t *testing.T, session *sdk.ClientSession, name string, arguments any) map[string]any {
	t.Helper()
	output, message := call(t, session, name, arguments)
	if message != "" {
		t.Fatalf("%s failed: %s", name, message)
	}
	return output
}

func TestListToolsExposesEveryToolWithItsRequiredFields(t *testing.T) {
	jobs, _ := newJobService(t)
	session, _ := connect(t, jobs, ownerID)
	result, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	required := map[string][]string{}
	for _, tool := range result.Tools {
		raw, _ := json.Marshal(tool.InputSchema)
		var schema struct {
			Required []string `json:"required"`
		}
		_ = json.Unmarshal(raw, &schema)
		required[tool.Name] = schema.Required
	}
	for _, name := range allTools {
		if _, ok := required[name]; !ok {
			t.Errorf("tool %q missing", name)
		}
	}
	for name, want := range map[string][]string{"create_job": {"company_name", "position_title"}, "update_job": {"id", "changes"},
		"create_stage": {"job_id", "stage_type"}, "update_stage": {"id", "changes"}, "set_job_cv_version": {"id", "version_id"},
		"reorder_stages": {"job_id", "stage_ids"}, "create_question": {"stage_id", "question"}, "update_question": {"id", "changes"},
		"reorder_questions": {"stage_id", "question_ids"}, "create_search_period": {"name", "start_date"},
		"update_search_period": {"id", "name", "start_date"}} {
		if !slices.Equal(required[name], want) {
			t.Errorf("%s required = %v, want %v", name, required[name], want)
		}
	}
}

func TestServerAnnouncesItsInstructions(t *testing.T) {
	jobs, _ := newJobService(t)
	session, _ := connect(t, jobs, ownerID)
	result := session.InitializeResult()
	if result.ServerInfo.Name != "war-room" || !strings.Contains(result.Instructions, "list_jobs before creating") {
		t.Fatalf("initialize = %+v", result)
	}
}

func TestUpdateJobChangesOnlyTheGivenFields(t *testing.T) {
	jobs, _ := newJobService(t)
	session, _ := connect(t, jobs, ownerID)
	mustCall(t, session, "update_job", map[string]any{"id": "job-1", "changes": map[string]any{"company_overview": "Referral note"}})
	job := mustCall(t, session, "update_job", map[string]any{"id": "job-1", "changes": map[string]any{"position_title": "AI Ops"}})
	if job["position_title"] != "AI Ops" || job["company_overview"] != "Referral note" || job["company_name"] != "Example" || job["status"] != "ongoing" {
		t.Fatalf("update_job changed unrelated fields: %v", job)
	}
}

func TestValidationErrorsReachTheClient(t *testing.T) {
	jobs, _ := newJobService(t)
	session, _ := connect(t, jobs, ownerID)
	if _, message := call(t, session, "update_job", map[string]any{"id": "job-1", "changes": map[string]any{"status": "bogus"}}); !strings.HasPrefix(message, service.ErrInvalidField.Error()) {
		t.Fatalf("invalid status = %q, want the service's validation message", message)
	}
	if _, message := call(t, session, "update_job", map[string]any{"id": "job-1", "changes": map[string]any{"position_title": "  "}}); message != service.ErrPositionTitleRequired.Error() {
		t.Fatalf("blank title = %q", message)
	}
}

func TestListJobsFiltersByStatusAndSearch(t *testing.T) {
	jobs, _ := newJobService(t)
	session, _ := connect(t, jobs, ownerID)
	mustCall(t, session, "create_job", map[string]any{"company_name": "Acme", "position_title": "Platform Engineer"})
	for _, test := range []struct {
		arguments map[string]any
		want      int
	}{
		{map[string]any{}, 2},
		{map[string]any{"status": "waiting"}, 1},
		{map[string]any{"search": "Example"}, 1},
		{map[string]any{"status": "rejected"}, 0},
	} {
		listed := mustCall(t, session, "list_jobs", test.arguments)
		if got := len(listed["jobs"].([]any)); got != test.want {
			t.Errorf("list_jobs(%v) returned %d jobs, want %d", test.arguments, got, test.want)
		}
	}
}

func TestAnotherUsersRecordsAreNotFound(t *testing.T) {
	jobs, _ := newJobService(t)
	owner, _ := connect(t, jobs, ownerID)
	stage := mustCall(t, owner, "create_stage", map[string]any{"job_id": "job-1", "stage_type": "HR"})
	stranger, _ := connect(t, jobs, strangerID)
	for _, test := range []struct {
		name      string
		arguments map[string]any
	}{
		{"get_job", map[string]any{"id": "job-1"}},
		{"update_job", map[string]any{"id": "job-1", "changes": map[string]any{"position_title": "Hijacked"}}},
		{"create_stage", map[string]any{"job_id": "job-1", "stage_type": "HR"}},
		{"update_stage", map[string]any{"id": stage["id"], "changes": map[string]any{"notes": "Hijacked"}}},
	} {
		if _, message := call(t, stranger, test.name, test.arguments); message != "not found" {
			t.Errorf("%s on another user's record = %q, want not found", test.name, message)
		}
	}
	if listed := mustCall(t, stranger, "list_jobs", map[string]any{}); len(listed["jobs"].([]any)) != 0 {
		t.Fatalf("list_jobs returned another user's jobs: %v", listed)
	}
	if job := mustCall(t, owner, "get_job", map[string]any{"id": "job-1"}); job["position_title"] != "Engineer" {
		t.Fatalf("stranger changed the owner's job: %v", job)
	}
}

func TestCreateJobAppliesDefaultsAndStages(t *testing.T) {
	jobs, _ := newJobService(t)
	session, _ := connect(t, jobs, ownerID)
	job := mustCall(t, session, "create_job", map[string]any{"company_name": "Acme", "position_title": "Platform Engineer", "create_default_stages": true})
	if job["status"] != "waiting" {
		t.Fatalf("create_job status = %v, want waiting", job["status"])
	}
	fetched := mustCall(t, session, "get_job", map[string]any{"id": job["id"]})
	if stages, _ := fetched["stages"].([]any); len(stages) == 0 {
		t.Fatalf("create_default_stages added no stages: %v", fetched)
	}
}

func TestStagesAreCreatedAndPartiallyUpdated(t *testing.T) {
	jobs, _ := newJobService(t)
	session, _ := connect(t, jobs, ownerID)
	stage := mustCall(t, session, "create_stage", map[string]any{"job_id": "job-1", "stage_type": "Technical", "notes": "Bring examples",
		"interviewers": []map[string]any{{"name": "Sara", "role": "Department head"}}})
	updated := mustCall(t, session, "update_stage", map[string]any{"id": stage["id"], "changes": map[string]any{"custom_title": "System design"}})
	if updated["custom_title"] != "System design" || updated["notes"] != "Bring examples" || len(updated["interviewers"].([]any)) != 1 {
		t.Fatalf("update_stage changed unrelated fields: %v", updated)
	}
	updated = mustCall(t, session, "update_stage", map[string]any{"id": stage["id"], "changes": map[string]any{
		"interviewers": []map[string]any{{"name": "Sergi", "role": "Department head"}, {"name": "Edith", "role": "Department head"}}}})
	if panel := updated["interviewers"].([]any); len(panel) != 2 || panel[0].(map[string]any)["name"] != "Sergi" {
		t.Fatalf("interviewers were not replaced: %v", updated["interviewers"])
	}
	if _, message := call(t, session, "update_stage", map[string]any{"id": stage["id"], "changes": map[string]any{"meeting_date": "2026-02-30"}}); message != service.ErrInvalidMeetingDate.Error() {
		t.Fatalf("invalid meeting date = %q", message)
	}
}

func TestTechnologyCatalogAndTechStack(t *testing.T) {
	jobs, _ := newJobService(t)
	session, _ := connect(t, jobs, ownerID)
	if catalog := mustCall(t, session, "list_technologies", map[string]any{}); len(catalog["technologies"].([]any)) != 0 {
		t.Fatalf("new catalog = %v, want empty", catalog)
	}
	technology := mustCall(t, session, "create_technology", map[string]any{"name": "Azure API Management", "aliases": []string{"APIM"}})
	renamed := mustCall(t, session, "update_technology", map[string]any{"id": technology["id"], "name": "Azure API Management", "aliases": []string{"APIM", "Azure APIM"}})
	if aliases := renamed["aliases"].([]any); len(aliases) != 2 {
		t.Fatalf("update_technology aliases = %v", aliases)
	}
	mustCall(t, session, "update_job", map[string]any{"id": "job-1", "changes": map[string]any{"technology_ids": []any{technology["id"]}}})
	job := mustCall(t, session, "get_job", map[string]any{"id": "job-1"})
	if stack := job["technologies"].([]any); len(stack) != 1 || stack[0].(map[string]any)["name"] != "Azure API Management" {
		t.Fatalf("job technologies = %v", job["technologies"])
	}
}

func TestListCVVersionsReturnsAnEmptyList(t *testing.T) {
	jobs, _ := newJobService(t)
	session, _ := connect(t, jobs, ownerID)
	if versions := mustCall(t, session, "list_cv_versions", map[string]any{}); versions["versions"] == nil || len(versions["versions"].([]any)) != 0 {
		t.Fatalf("list_cv_versions = %v, want an empty list", versions)
	}
}

func TestArgumentsOutsideTheSchemaAreRejected(t *testing.T) {
	jobs, _ := newJobService(t)
	session, _ := connect(t, jobs, ownerID)
	for _, test := range []struct {
		name      string
		arguments map[string]any
	}{
		{"create_job", map[string]any{"company_name": "Acme"}},
		{"create_job", map[string]any{"company_name": "Acme", "position_title": "Engineer", "unknown": true}},
		{"update_job", map[string]any{"id": "job-1", "changes": map[string]any{"positon_title": "Typo"}}},
		{"update_job", map[string]any{"id": "job-1"}},
		{"create_stage", map[string]any{"job_id": "job-1"}},
		{"update_stage", map[string]any{"id": "stage-1", "changes": map[string]any{"notes": 7}}},
		{"get_job", map[string]any{"id": 1}},
	} {
		if _, message := call(t, session, test.name, test.arguments); message == "" {
			t.Errorf("%s(%v) was accepted", test.name, test.arguments)
		}
	}
	if job := mustCall(t, session, "get_job", map[string]any{"id": "job-1"}); job["position_title"] != "Engineer" {
		t.Fatalf("a rejected call changed the job: %v", job)
	}
}

func TestInternalErrorsAreHiddenAndRecorded(t *testing.T) {
	jobs, db := newJobService(t)
	session, requests := connect(t, jobs, ownerID)
	if _, err := db.Exec(`DROP TABLE stages`); err != nil {
		t.Fatal(err)
	}
	_, message := call(t, session, "get_job", map[string]any{"id": "job-1"})
	if message != "internal server error (request_id "+requestID+")" {
		t.Fatalf("internal failure message = %q", message)
	}
	last := (*requests)[len(*requests)-1]
	if last.Err() == nil || !strings.Contains(last.Err().Error(), "stages") {
		t.Fatalf("request log error = %v, want the database failure", last.Err())
	}
}

// fakeJobs fails every call with err and returns nil lists, recording the
// user each call acted as.
type fakeJobs struct {
	err   error
	users []string
}

func (f *fakeJobs) seen(userID string) { f.users = append(f.users, userID) }

func (f *fakeJobs) GetAllJobs(userID, _, _ string) ([]models.Job, error) {
	f.seen(userID)
	return nil, f.err
}

func (f *fakeJobs) GetFullJobDetails(userID, _ string) (*models.Job, error) {
	f.seen(userID)
	return nil, f.err
}

func (f *fakeJobs) CreateJob(userID string, _ models.CreateJobInput) (*models.Job, error) {
	f.seen(userID)
	return nil, f.err
}

func (f *fakeJobs) UpdateJob(userID, _ string, _ models.UpdateJobInput) (*models.Job, error) {
	f.seen(userID)
	return nil, f.err
}

func (f *fakeJobs) ListTechnologies(userID string) ([]models.Technology, error) {
	f.seen(userID)
	return nil, f.err
}

func (f *fakeJobs) CreateTechnology(userID, _ string, _ []string) (models.Technology, error) {
	f.seen(userID)
	return models.Technology{}, f.err
}

func (f *fakeJobs) UpdateTechnology(userID, _, _ string, _ []string) (models.Technology, error) {
	f.seen(userID)
	return models.Technology{}, f.err
}

func (f *fakeJobs) ListCVVersions(userID string) ([]models.CVVersion, error) {
	f.seen(userID)
	return nil, f.err
}

func (f *fakeJobs) CreateStage(userID string, _ models.CreateStageInput) (*models.Stage, error) {
	f.seen(userID)
	return nil, f.err
}

func (f *fakeJobs) UpdateStage(userID, _ string, _ models.UpdateStageInput) (*models.Stage, error) {
	f.seen(userID)
	return nil, f.err
}

func (f *fakeJobs) ReorderStages(userID, _ string, _ []string) error {
	f.seen(userID)
	return f.err
}

func (f *fakeJobs) GetJobCounts(userID string) (*models.JobCounts, error) {
	f.seen(userID)
	return nil, f.err
}

func (f *fakeJobs) AssignCVVersion(userID, _ string, _ *string) error {
	f.seen(userID)
	return f.err
}

func (f *fakeJobs) GetScheduledMeetings(userID string) ([]models.ScheduledMeeting, error) {
	f.seen(userID)
	return nil, f.err
}

func (f *fakeJobs) CreateQuestion(userID string, _ models.CreateQuestionInput) (*models.Question, error) {
	f.seen(userID)
	return nil, f.err
}

func (f *fakeJobs) UpdateQuestion(userID, _ string, _ models.UpdateQuestionInput) (*models.Question, error) {
	f.seen(userID)
	return nil, f.err
}

func (f *fakeJobs) ReorderQuestions(userID, _ string, _ []string) error {
	f.seen(userID)
	return f.err
}

func (f *fakeJobs) ListSearchPeriods(userID string) ([]models.SearchPeriod, error) {
	f.seen(userID)
	return nil, f.err
}

func (f *fakeJobs) CreateSearchPeriod(userID string, _ models.SearchPeriodInput) (*models.SearchPeriod, error) {
	f.seen(userID)
	return nil, f.err
}

func (f *fakeJobs) UpdateSearchPeriod(userID, _ string, _ models.SearchPeriodInput) error {
	f.seen(userID)
	return f.err
}

var validArguments = map[string]map[string]any{
	"list_jobs":            {},
	"get_job":              {"id": "job-1"},
	"create_job":           {"company_name": "Acme", "position_title": "Engineer"},
	"update_job":           {"id": "job-1", "changes": map[string]any{}},
	"list_technologies":    {},
	"create_technology":    {"name": "Go"},
	"update_technology":    {"id": "tech-1", "name": "Go"},
	"list_cv_versions":     {},
	"create_stage":         {"job_id": "job-1", "stage_type": "HR"},
	"update_stage":         {"id": "stage-1", "changes": map[string]any{}},
	"count_jobs":           {},
	"set_job_cv_version":   {"id": "job-1", "version_id": nil},
	"reorder_stages":       {"job_id": "job-1", "stage_ids": []string{"stage-1"}},
	"list_meetings":        {},
	"create_question":      {"stage_id": "stage-1", "question": "Why us?"},
	"update_question":      {"id": "question-1", "changes": map[string]any{}},
	"reorder_questions":    {"stage_id": "stage-1", "question_ids": []string{"question-1"}},
	"list_search_periods":  {},
	"create_search_period": {"name": "Spring", "start_date": "2026-03-01"},
	"update_search_period": {"id": "period-1", "name": "Spring", "start_date": "2026-03-01"},
}

func TestEveryToolMapsServiceErrorsForTheClient(t *testing.T) {
	for _, test := range []struct {
		err  error
		want string
	}{
		{fmt.Errorf("wrapped: %w", service.ErrInvalidField), "wrapped: " + service.ErrInvalidField.Error()},
		{fmt.Errorf("lookup: %w", sql.ErrNoRows), "not found"},
		{errors.New("disk full"), "internal server error (request_id " + requestID + ")"},
	} {
		jobs := &fakeJobs{err: test.err}
		session, _ := connect(t, jobs, ownerID)
		for _, name := range allTools {
			if _, message := call(t, session, name, validArguments[name]); message != test.want {
				t.Errorf("%s with %v = %q, want %q", name, test.err, message, test.want)
			}
		}
		if len(jobs.users) != len(allTools) || slices.ContainsFunc(jobs.users, func(user string) bool { return user != ownerID }) {
			t.Errorf("tools acted as %v, want %s for each of %d calls", jobs.users, ownerID, len(allTools))
		}
	}
}

func TestListsAreNeverNull(t *testing.T) {
	session, _ := connect(t, &fakeJobs{}, ownerID)
	for name, key := range map[string]string{"list_jobs": "jobs", "list_technologies": "technologies", "list_cv_versions": "versions",
		"list_meetings": "meetings", "list_search_periods": "search_periods"} {
		output := mustCall(t, session, name, map[string]any{})
		if list, ok := output[key].([]any); !ok || len(list) != 0 {
			t.Errorf("%s %s = %#v, want an empty list", name, key, output[key])
		}
	}
}

func TestPublicErrorOutsideALoggedRequest(t *testing.T) {
	if got := (tools{requestContext: context.Background()}).publicError(errors.New("disk full")).Error(); got != "internal server error" {
		t.Fatalf("error outside a logged request = %q", got)
	}
}

func TestRequestsWithoutAUserActAsNoOne(t *testing.T) {
	jobs := &fakeJobs{}
	server := newServer(context.Background(), jobs)
	clientTransport, serverTransport := sdk.NewInMemoryTransports()
	if _, err := server.Connect(context.Background(), serverTransport, nil); err != nil {
		t.Fatal(err)
	}
	session, err := sdk.NewClient(&sdk.Implementation{Name: "test"}, nil).Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close() }()
	mustCall(t, session, "list_jobs", map[string]any{})
	if len(jobs.users) != 1 || jobs.users[0] != "" {
		t.Fatalf("users = %v, want one call with no user", jobs.users)
	}
}

func TestHandlerBoundsRequestBodies(t *testing.T) {
	handler := NewHandler(&fakeJobs{})
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"pad":"` + strings.Repeat("x", maxRequestBytes) + `"}}`
	request := httptest.NewRequest(http.MethodPost, "/api/mcp", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code < http.StatusBadRequest {
		t.Fatalf("oversized body status = %d, want a client error", recorder.Code)
	}
}

func TestOptionalFieldsSchemaPanicsOnUnsupportedTypes(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("optionalFieldsSchema accepted a channel type")
		}
	}()
	optionalFieldsSchema[chan int]()
}

func TestOptionalFieldsSchemaClearsNestedRequiredFields(t *testing.T) {
	schema := optionalFieldsSchema[updateStageInput]("id")
	interviewers := schema.Properties["changes"].Properties["interviewers"]
	if len(schema.Properties["changes"].Required) != 0 || interviewers.Items == nil || len(interviewers.Items.Required) != 0 {
		t.Fatalf("nested schemas still require fields: %+v", schema.Properties["changes"])
	}
	if !slices.Equal(schema.Required, []string{"id"}) {
		t.Fatalf("required = %v", schema.Required)
	}
}
