package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"war-room/backend/pkg/config"
	"war-room/backend/pkg/logging"
	"war-room/backend/pkg/middleware"
)

// connectMCP serves the MCP handler as userID, standing in for RequireSession,
// and returns a connected SDK client session.
func connectMCP(t *testing.T, handler *Handler, userID string) *mcp.ClientSession {
	t.Helper()
	mcpHandler := handler.newMCPHandler()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request := logging.NewRequest()
		request.ID = "mcp-test-request"
		ctx := logging.WithRequest(middleware.ContextWithUserID(r.Context(), userID), request)
		mcpHandler.ServeHTTP(w, r.WithContext(ctx))
	}))
	t.Cleanup(server.Close)
	client := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: server.URL, MaxRetries: -1}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func callMCPTool(t *testing.T, session *mcp.ClientSession, name string, arguments any) (map[string]any, string) {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	if result.IsError {
		return nil, result.Content[0].(*mcp.TextContent).Text
	}
	var output map[string]any
	raw, _ := json.Marshal(result.StructuredContent)
	if err := json.Unmarshal(raw, &output); err != nil {
		t.Fatalf("decode %s output: %v", name, err)
	}
	return output, ""
}

func TestMCPListsWarRoomTools(t *testing.T) {
	session := connectMCP(t, newCVHandlerFixture(t).handler, testUserID)
	result, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range result.Tools {
		names = append(names, tool.Name)
	}
	for _, want := range []string{"list_jobs", "get_job", "create_job", "update_job", "list_technologies",
		"create_technology", "update_technology", "list_cv_versions", "create_stage", "update_stage"} {
		if !slices.Contains(names, want) {
			t.Errorf("tool %q missing from %v", want, names)
		}
	}
}

func TestMCPUpdateJobChangesOnlyTheGivenFields(t *testing.T) {
	fixture := newCVHandlerFixture(t)
	session := connectMCP(t, fixture.handler, testUserID)
	if _, message := callMCPTool(t, session, "update_job", map[string]any{"id": "job-1", "changes": map[string]any{"company_overview": "Referral note"}}); message != "" {
		t.Fatal(message)
	}
	job, message := callMCPTool(t, session, "update_job", map[string]any{"id": "job-1", "changes": map[string]any{"position_title": "IAOps"}})
	if message != "" {
		t.Fatal(message)
	}
	if job["position_title"] != "IAOps" || job["company_overview"] != "Referral note" || job["company_name"] != "Example" {
		t.Fatalf("update_job changed unrelated fields: %v", job)
	}
	if _, message := callMCPTool(t, session, "update_job", map[string]any{"id": "job-1", "changes": map[string]any{"status": "bogus"}}); !strings.Contains(message, "invalid") {
		t.Fatalf("validation error = %q, want the service's validation message", message)
	}
}

func TestMCPTreatsAnotherUsersJobAsNotFound(t *testing.T) {
	fixture := newCVHandlerFixture(t)
	seedOtherUser(t, fixture)
	session := connectMCP(t, fixture.handler, otherUserID)
	for _, call := range []struct {
		name      string
		arguments map[string]any
	}{
		{"get_job", map[string]any{"id": "job-1"}},
		{"update_job", map[string]any{"id": "job-1", "changes": map[string]any{"position_title": "Hijacked"}}},
		{"create_stage", map[string]any{"job_id": "job-1", "stage_type": "HR"}},
	} {
		if _, message := callMCPTool(t, session, call.name, call.arguments); message != "not found" {
			t.Fatalf("%s on another user's job = %q, want not found", call.name, message)
		}
	}
	listed, _ := callMCPTool(t, session, "list_jobs", map[string]any{})
	if jobs := listed["jobs"].([]any); len(jobs) != 0 {
		t.Fatalf("list_jobs returned another user's jobs: %v", jobs)
	}
}

func TestMCPCreatesJobsAndStages(t *testing.T) {
	session := connectMCP(t, newCVHandlerFixture(t).handler, testUserID)
	job, message := callMCPTool(t, session, "create_job", map[string]any{"company_name": "Acme", "position_title": "Platform Engineer"})
	if message != "" || job["status"] != "waiting" {
		t.Fatalf("create_job = %v %q, want a waiting job", job, message)
	}
	stage, message := callMCPTool(t, session, "create_stage", map[string]any{"job_id": job["id"], "stage_type": "Technical"})
	if message != "" {
		t.Fatal(message)
	}
	updated, message := callMCPTool(t, session, "update_stage", map[string]any{"id": stage["id"], "changes": map[string]any{"custom_title": "System design"}})
	if message != "" || updated["custom_title"] != "System design" {
		t.Fatalf("update_stage = %v %q", updated, message)
	}
	fetched, message := callMCPTool(t, session, "get_job", map[string]any{"id": job["id"]})
	if stages, _ := fetched["stages"].([]any); message != "" || len(stages) != 1 {
		t.Fatalf("get_job = %v %q, want the created stage", fetched, message)
	}
}

func TestMCPManagesTechnologiesAndListsCVVersions(t *testing.T) {
	session := connectMCP(t, newCVHandlerFixture(t).handler, testUserID)
	technology, message := callMCPTool(t, session, "create_technology", map[string]any{"name": "Kubernetes", "aliases": []string{"K8s"}})
	if message != "" {
		t.Fatal(message)
	}
	if _, message := callMCPTool(t, session, "update_technology", map[string]any{"id": technology["id"], "name": "Kubernetes", "aliases": []string{"K8s", "Kube"}}); message != "" {
		t.Fatal(message)
	}
	catalog, _ := callMCPTool(t, session, "list_technologies", map[string]any{})
	if len(catalog["technologies"].([]any)) != 1 {
		t.Fatalf("list_technologies = %v", catalog)
	}
	if versions, message := callMCPTool(t, session, "list_cv_versions", map[string]any{}); message != "" || versions == nil {
		t.Fatalf("list_cv_versions = %v %q", versions, message)
	}
}

func TestMCPRejectsArgumentsOutsideTheSchema(t *testing.T) {
	session := connectMCP(t, newCVHandlerFixture(t).handler, testUserID)
	if _, message := callMCPTool(t, session, "create_job", map[string]any{"company_name": "Acme"}); message == "" {
		t.Fatal("create_job without position_title was accepted")
	}
	if _, message := callMCPTool(t, session, "update_job", map[string]any{"id": "job-1", "changes": map[string]any{"positon_title": "Typo"}}); message == "" {
		t.Fatal("update_job accepted an unknown field")
	}
}

func TestMCPHidesInternalErrors(t *testing.T) {
	fixture := newCVHandlerFixture(t)
	session := connectMCP(t, fixture.handler, testUserID)
	if _, err := fixture.db.Exec(`DROP TABLE stages`); err != nil {
		t.Fatal(err)
	}
	_, message := callMCPTool(t, session, "get_job", map[string]any{"id": "job-1"})
	if message != "internal server error (request_id mcp-test-request)" {
		t.Fatalf("internal failure message = %q", message)
	}
	if got := (mcpTools{requestContext: context.Background()}).publicError(errors.New("disk full")).Error(); got != "internal server error" {
		t.Fatalf("error outside a logged request = %q", got)
	}
}

func TestMCPRouteCoexistsWithStaticFallback(t *testing.T) {
	handler := NewHandler(nil, nil, &config.Config{StaticDir: t.TempDir()})
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	request := httptest.NewRequest(http.MethodPost, "/api/mcp", nil)
	if _, pattern := mux.Handler(request); pattern != "POST /api/mcp" {
		t.Fatalf("POST /api/mcp routed to %q", pattern)
	}
}
