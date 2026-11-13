package backend_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"slices"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"war-room/backend/pkg/models"
)

// These tests drive /api/mcp through the same route table and RequireSession
// middleware as the server, authenticated by real personal API tokens.

const secondIntegrationEmail = "second@example.test"

const toolsListRequest = `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`

// bearerTransport adds a personal API token to every request.
type bearerTransport struct{ header string }

func (b bearerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
	request.Header.Set("Authorization", b.header)
	return http.DefaultTransport.RoundTrip(request)
}

func bearerClient(token string) *http.Client {
	return &http.Client{Transport: bearerTransport{header: "Bearer " + token}}
}

// newMCPStack allowlists both test accounts, signs in the second one, and
// returns the stack with a token for each.
func newMCPStack(t *testing.T) (integrationStack, string, string) {
	t.Helper()
	stack := newIntegrationStack(t)
	t.Cleanup(stack.cleanup)
	stack.cfg.AuthAllowedEmails = []string{integrationTestEmail, secondIntegrationEmail}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	second := &http.Client{Jar: jar}
	response, err := second.Post(stack.server.URL+"/api/test/session", "application/json",
		strings.NewReader(`{"email":"`+secondIntegrationEmail+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	closeIntegrationResource(t, response.Body)
	requireStatus(t, response, http.StatusOK)
	return stack, createIntegrationToken(t, stack, integrationTestEmail), createIntegrationToken(t, stack, secondIntegrationEmail)
}

func createIntegrationToken(t *testing.T, stack integrationStack, email string) string {
	t.Helper()
	token, _, err := stack.auth.CreateAPIToken(email, "integration", 24*time.Hour)
	if err != nil {
		t.Fatalf("create token for %s: %v", email, err)
	}
	return token
}

func connectIntegrationMCP(t *testing.T, stack integrationStack, token string) *sdk.ClientSession {
	t.Helper()
	transport := &sdk.StreamableClientTransport{Endpoint: stack.server.URL + "/api/mcp", HTTPClient: bearerClient(token), MaxRetries: -1}
	session, err := sdk.NewClient(&sdk.Implementation{Name: "integration"}, nil).Connect(context.Background(), transport, nil)
	if err != nil {
		t.Fatalf("connect to /api/mcp: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func callIntegrationTool(t *testing.T, session *sdk.ClientSession, name string, arguments any) (map[string]any, string) {
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
		t.Fatalf("decode %s: %v", name, err)
	}
	return output, ""
}

// postMCP sends one raw JSON-RPC request with the given Authorization header
// (none when empty) and returns the response.
func postMCP(t *testing.T, client *http.Client, serverURL, authorization string) *http.Response {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, serverURL+"/api/mcp", strings.NewReader(toolsListRequest))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeIntegrationResource(t, response.Body) })
	return response
}

func TestMCP_TokenEditsTheOwnersDataSeenByTheAPI(t *testing.T) {
	stack, token, _ := newMCPStack(t)
	session := connectIntegrationMCP(t, stack, token)
	job, message := callIntegrationTool(t, session, "create_job", map[string]any{"company_name": "Acme", "position_title": "AI Ops", "create_default_stages": true})
	if message != "" {
		t.Fatal(message)
	}
	fetched, _ := callIntegrationTool(t, session, "get_job", map[string]any{"id": job["id"]})
	stage := fetched["stages"].([]any)[0].(map[string]any)
	if _, message := callIntegrationTool(t, session, "update_stage", map[string]any{"id": stage["id"], "changes": map[string]any{
		"notes": "Ask about the APIM rollout", "interviewers": []map[string]any{{"name": "Sara", "role": "Department head"}}}}); message != "" {
		t.Fatal(message)
	}

	response := mustRequest(t, stack.server.Client(), http.MethodGet, stack.server.URL+"/api/jobs/"+job["id"].(string), "")
	t.Cleanup(func() { closeIntegrationResource(t, response.Body) })
	requireStatus(t, response, http.StatusOK)
	var seen models.Job
	if err := json.NewDecoder(response.Body).Decode(&seen); err != nil {
		t.Fatal(err)
	}
	updated := seen.Stages[0]
	if updated.Notes != "Ask about the APIM rollout" || len(updated.Interviewers) != 1 || updated.StageType != models.StageType(stage["stage_type"].(string)) {
		t.Fatalf("cookie session sees stage %+v, want the MCP edit with other fields kept", updated)
	}
}

func TestMCP_TokensOnlyReachTheirOwnersData(t *testing.T) {
	stack, ownerToken, strangerToken := newMCPStack(t)
	owner := connectIntegrationMCP(t, stack, ownerToken)
	job, _ := callIntegrationTool(t, owner, "create_job", map[string]any{"company_name": "Acme", "position_title": "AI Ops"})
	stranger := connectIntegrationMCP(t, stack, strangerToken)
	if listed, _ := callIntegrationTool(t, stranger, "list_jobs", map[string]any{}); len(listed["jobs"].([]any)) != 0 {
		t.Fatalf("stranger lists %v", listed)
	}
	for _, call := range []struct {
		name      string
		arguments map[string]any
	}{
		{"get_job", map[string]any{"id": job["id"]}},
		{"update_job", map[string]any{"id": job["id"], "changes": map[string]any{"position_title": "Hijacked"}}},
	} {
		if _, message := callIntegrationTool(t, stranger, call.name, call.arguments); message != "not found" {
			t.Errorf("stranger %s = %q, want not found", call.name, message)
		}
	}
}

func TestMCP_RejectsRequestsWithoutAUsableToken(t *testing.T) {
	stack, token, _ := newMCPStack(t)
	cookieClient := stack.server.Client()
	anonymous := &http.Client{}
	for _, test := range []struct {
		name          string
		client        *http.Client
		authorization string
	}{
		{"no credentials", anonymous, ""},
		{"unknown token with a valid cookie", cookieClient, "Bearer wr_never-issued"},
		{"other scheme with a valid cookie", cookieClient, "Basic " + token},
		{"token without the scheme", anonymous, token},
	} {
		if response := postMCP(t, test.client, stack.server.URL, test.authorization); response.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s: status = %d, want 401", test.name, response.StatusCode)
		}
	}
	if response := postMCP(t, anonymous, stack.server.URL, "Bearer "+token); response.StatusCode != http.StatusOK {
		t.Fatalf("valid token status = %d, want 200", response.StatusCode)
	}
}

func TestMCP_RevokedExpiredAndDisallowedTokensStopWorking(t *testing.T) {
	for _, test := range []struct {
		name    string
		disable func(t *testing.T, stack integrationStack)
	}{
		{"revoked", func(t *testing.T, stack integrationStack) {
			tokens, err := stack.auth.ListAPITokens(integrationTestEmail)
			if err != nil || len(tokens) != 1 {
				t.Fatalf("tokens = %v err=%v", tokens, err)
			}
			if err := stack.auth.RevokeAPIToken(integrationTestEmail, tokens[0].ID); err != nil {
				t.Fatal(err)
			}
		}},
		{"expired", func(t *testing.T, stack integrationStack) {
			if _, err := stack.db.Exec(`UPDATE api_tokens SET expires_at = ?`, time.Now().Add(-time.Minute).Unix()); err != nil {
				t.Fatal(err)
			}
		}},
		{"removed from the allowlist", func(_ *testing.T, stack integrationStack) {
			stack.cfg.AuthAllowedEmails = []string{secondIntegrationEmail}
		}},
		{"signed out everywhere", func(t *testing.T, stack integrationStack) {
			var userID string
			if err := stack.db.QueryRow(`SELECT id FROM users WHERE email = ?`, integrationTestEmail).Scan(&userID); err != nil {
				t.Fatal(err)
			}
			if err := stack.auth.SignOutAllSessions(userID); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			stack, token, _ := newMCPStack(t)
			anonymous := &http.Client{}
			if response := postMCP(t, anonymous, stack.server.URL, "Bearer "+token); response.StatusCode != http.StatusOK {
				t.Fatalf("before: status = %d", response.StatusCode)
			}
			test.disable(t, stack)
			if response := postMCP(t, anonymous, stack.server.URL, "Bearer "+token); response.StatusCode != http.StatusUnauthorized {
				t.Fatalf("after: status = %d, want 401", response.StatusCode)
			}
		})
	}
}

func TestMCP_TokenRequestsRecordUseWithoutIssuingACookie(t *testing.T) {
	stack, token, _ := newMCPStack(t)
	response := postMCP(t, &http.Client{}, stack.server.URL, "Bearer "+token)
	if response.StatusCode != http.StatusOK || len(response.Cookies()) != 0 {
		t.Fatalf("status = %d cookies = %v, want 200 and no cookie", response.StatusCode, response.Cookies())
	}
	tokens, err := stack.auth.ListAPITokens(integrationTestEmail)
	if err != nil || len(tokens) != 1 || tokens[0].LastUsedAt == nil {
		t.Fatalf("tokens = %+v err=%v, want a last-used time", tokens, err)
	}
}

func TestMCP_BrowserSessionAndRESTRoutesShareTheSameAuth(t *testing.T) {
	stack, token, _ := newMCPStack(t)
	if response := postMCP(t, stack.server.Client(), stack.server.URL, ""); response.StatusCode != http.StatusOK {
		t.Fatalf("cookie session on /api/mcp status = %d, want 200", response.StatusCode)
	}
	response := mustRequest(t, bearerClient(token), http.MethodGet, stack.server.URL+"/api/jobs", "")
	t.Cleanup(func() { closeIntegrationResource(t, response.Body) })
	requireStatus(t, response, http.StatusOK)
}

// mcpTools is every tool the server must offer agents, sorted.
var mcpTools = []string{"count_jobs", "create_job", "create_question", "create_search_period", "create_stage",
	"create_technology", "get_job", "list_cv_versions", "list_jobs", "list_meetings", "list_search_periods",
	"list_technologies", "reorder_questions", "reorder_stages", "set_job_cv_version", "update_job",
	"update_question", "update_search_period", "update_stage", "update_technology"}

func TestMCP_ToolCatalogIsComplete(t *testing.T) {
	stack, token, _ := newMCPStack(t)
	result, err := connectIntegrationMCP(t, stack, token).ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(result.Tools))
	for _, tool := range result.Tools {
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	if !slices.Equal(names, mcpTools) {
		t.Fatalf("tools = %v, want %v", names, mcpTools)
	}
}

func mustIntegrationTool(t *testing.T, session *sdk.ClientSession, name string, arguments any) map[string]any {
	t.Helper()
	output, message := callIntegrationTool(t, session, name, arguments)
	if message != "" {
		t.Fatalf("%s: %s", name, message)
	}
	return output
}

// getIntegrationJob reads a job through the owner's browser session.
func getIntegrationJob(t *testing.T, stack integrationStack, id string) models.Job {
	t.Helper()
	response := mustRequest(t, stack.server.Client(), http.MethodGet, stack.server.URL+"/api/jobs/"+id, "")
	t.Cleanup(func() { closeIntegrationResource(t, response.Body) })
	requireStatus(t, response, http.StatusOK)
	var job models.Job
	if err := json.NewDecoder(response.Body).Decode(&job); err != nil {
		t.Fatal(err)
	}
	return job
}

// ownerProcess is the owner's job with two stages, two questions on the
// first stage, a CV version, and a search period, created through the
// owner's token (the CV through the owner's browser session).
type ownerProcess struct {
	jobID, versionID, periodID string
	stageIDs, questionIDs      []string
}

func newOwnerProcess(t *testing.T, stack integrationStack, owner *sdk.ClientSession) ownerProcess {
	t.Helper()
	version := uploadTestCV(t, stack.server.Client(), stack.server.URL, "resume.pdf", "owner CV bytes")
	job := mustIntegrationTool(t, owner, "create_job", map[string]any{"company_name": "Acme", "position_title": "AI Ops", "is_referral": true})
	process := ownerProcess{jobID: job["id"].(string), versionID: version.ID}
	for _, stageType := range []string{"HR", "Technical"} {
		stage := mustIntegrationTool(t, owner, "create_stage", map[string]any{"job_id": process.jobID, "stage_type": stageType})
		process.stageIDs = append(process.stageIDs, stage["id"].(string))
	}
	for _, text := range []string{"Why this team?", "What is on call like?"} {
		question := mustIntegrationTool(t, owner, "create_question", map[string]any{"stage_id": process.stageIDs[0], "question": text})
		process.questionIDs = append(process.questionIDs, question["id"].(string))
	}
	period := mustIntegrationTool(t, owner, "create_search_period", map[string]any{"name": "Spring", "start_date": "2020-03-01", "end_date": "2020-05-31"})
	process.periodID = period["id"].(string)
	return process
}

func TestMCP_ProcessToolsEditTheOwnersDataSeenByTheAPI(t *testing.T) {
	stack, token, _ := newMCPStack(t)
	owner := connectIntegrationMCP(t, stack, token)
	process := newOwnerProcess(t, stack, owner)
	mustIntegrationTool(t, owner, "set_job_cv_version", map[string]any{"id": process.jobID, "version_id": process.versionID})
	mustIntegrationTool(t, owner, "reorder_stages", map[string]any{"job_id": process.jobID, "stage_ids": []string{process.stageIDs[1], process.stageIDs[0]}})
	mustIntegrationTool(t, owner, "reorder_questions", map[string]any{"stage_id": process.stageIDs[0], "question_ids": []string{process.questionIDs[1], process.questionIDs[0]}})
	mustIntegrationTool(t, owner, "update_question", map[string]any{"id": process.questionIDs[0], "changes": map[string]any{"is_asked": true}})
	mustIntegrationTool(t, owner, "update_stage", map[string]any{"id": process.stageIDs[1], "changes": map[string]any{"meeting_date": "2026-12-01", "meeting_time": "09:00"}})
	mustIntegrationTool(t, owner, "update_search_period", map[string]any{"id": process.periodID, "name": "Early spring", "start_date": "2020-02-01", "end_date": "2020-05-31"})
	assertOwnerEditsSeenByTheAPI(t, stack, process)
	assertOwnerEditsSeenByTheTools(t, owner)
}

// assertOwnerEditsSeenByTheAPI checks the owner's browser session sees the
// CV, stage order, and question edits made through MCP.
func assertOwnerEditsSeenByTheAPI(t *testing.T, stack integrationStack, process ownerProcess) {
	t.Helper()
	job := getIntegrationJob(t, stack, process.jobID)
	if job.SelectedCVVersion == nil || job.SelectedCVVersion.ID != process.versionID {
		t.Fatalf("selected CV = %+v, want %s", job.SelectedCVVersion, process.versionID)
	}
	if job.Stages[0].ID != process.stageIDs[1] || job.Stages[1].ID != process.stageIDs[0] {
		t.Fatalf("stage order = %s, %s", job.Stages[0].ID, job.Stages[1].ID)
	}
	questions := job.Stages[1].Questions
	if len(questions) != 2 || questions[0].ID != process.questionIDs[1] || !questions[1].IsAsked {
		t.Fatalf("questions = %+v", questions)
	}
}

func assertOwnerEditsSeenByTheTools(t *testing.T, owner *sdk.ClientSession) {
	t.Helper()
	if meetings := mustIntegrationTool(t, owner, "list_meetings", map[string]any{}); len(meetings["meetings"].([]any)) != 1 {
		t.Fatalf("list_meetings = %v", meetings)
	}
	// Scheduling a meeting moves a waiting process to ongoing.
	if counts := mustIntegrationTool(t, owner, "count_jobs", map[string]any{}); counts["all"] != 1.0 || counts["ongoing"] != 1.0 || counts["waiting"] != 0.0 {
		t.Fatalf("count_jobs = %v", counts)
	}
	periods := mustIntegrationTool(t, owner, "list_search_periods", map[string]any{})["search_periods"].([]any)
	if len(periods) != 1 || periods[0].(map[string]any)["name"] != "Early spring" {
		t.Fatalf("list_search_periods = %v", periods)
	}
}

func TestMCP_ProcessToolsCannotReachAnotherUsersRecords(t *testing.T) {
	stack, ownerToken, strangerToken := newMCPStack(t)
	owner := connectIntegrationMCP(t, stack, ownerToken)
	process := newOwnerProcess(t, stack, owner)
	mustIntegrationTool(t, owner, "set_job_cv_version", map[string]any{"id": process.jobID, "version_id": process.versionID})
	stranger := connectIntegrationMCP(t, stack, strangerToken)
	strangerJob := mustIntegrationTool(t, stranger, "create_job", map[string]any{"company_name": "Other", "position_title": "Analyst", "is_referral": true})
	for _, call := range []struct {
		name      string
		arguments map[string]any
		want      string
	}{
		{"set_job_cv_version", map[string]any{"id": process.jobID, "version_id": nil}, "not found"},
		{"set_job_cv_version", map[string]any{"id": strangerJob["id"], "version_id": process.versionID}, "not found"},
		{"reorder_stages", map[string]any{"job_id": process.jobID, "stage_ids": []string{process.stageIDs[1], process.stageIDs[0]}}, "not found"},
		{"reorder_stages", map[string]any{"job_id": strangerJob["id"], "stage_ids": process.stageIDs}, "invalid ordering: every ID must exist once in this list"},
		{"create_question", map[string]any{"stage_id": process.stageIDs[0], "question": "Hijacked"}, "not found"},
		{"update_question", map[string]any{"id": process.questionIDs[0], "changes": map[string]any{"question": "Hijacked"}}, "not found"},
		{"update_question", map[string]any{"id": process.questionIDs[0], "changes": map[string]any{}}, "not found"},
		{"reorder_questions", map[string]any{"stage_id": process.stageIDs[0], "question_ids": []string{process.questionIDs[1], process.questionIDs[0]}}, "not found"},
		{"update_search_period", map[string]any{"id": process.periodID, "name": "Hijacked", "start_date": "2020-03-01"}, "not found"},
	} {
		if _, message := callIntegrationTool(t, stranger, call.name, call.arguments); message != call.want {
			t.Errorf("stranger %s(%v) = %q, want %q", call.name, call.arguments, message, call.want)
		}
	}
	assertStrangerListsOnlyTheirOwn(t, stranger)
	assertOwnerProcessUnchanged(t, stack, owner, process)
}

func assertStrangerListsOnlyTheirOwn(t *testing.T, stranger *sdk.ClientSession) {
	t.Helper()
	for name, key := range map[string]string{"list_meetings": "meetings", "list_search_periods": "search_periods"} {
		if listed := mustIntegrationTool(t, stranger, name, map[string]any{}); len(listed[key].([]any)) != 0 {
			t.Errorf("stranger %s = %v", name, listed)
		}
	}
	if counts := mustIntegrationTool(t, stranger, "count_jobs", map[string]any{}); counts["all"] != 1.0 {
		t.Errorf("stranger count_jobs = %v, want only their own job", counts)
	}
}

func assertOwnerProcessUnchanged(t *testing.T, stack integrationStack, owner *sdk.ClientSession, process ownerProcess) {
	t.Helper()
	job := getIntegrationJob(t, stack, process.jobID)
	if job.SelectedCVVersion == nil || job.Stages[0].ID != process.stageIDs[0] {
		t.Fatalf("stranger changed the owner's job: CV %+v, first stage %s", job.SelectedCVVersion, job.Stages[0].ID)
	}
	if questions := job.Stages[0].Questions; len(questions) != 2 || questions[0].Question != "Why this team?" {
		t.Fatalf("stranger changed the owner's questions: %+v", questions)
	}
	periods := mustIntegrationTool(t, owner, "list_search_periods", map[string]any{})["search_periods"].([]any)
	if len(periods) != 1 || periods[0].(map[string]any)["name"] != "Spring" {
		t.Fatalf("stranger changed the owner's periods: %v", periods)
	}
}
