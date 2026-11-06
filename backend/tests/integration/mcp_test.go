package backend_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
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

func TestMCP_ToolCatalogIsComplete(t *testing.T) {
	stack, token, _ := newMCPStack(t)
	result, err := connectIntegrationMCP(t, stack, token).ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Tools) != 10 {
		names := make([]string, 0, len(result.Tools))
		for _, tool := range result.Tools {
			names = append(names, tool.Name)
		}
		t.Fatalf("tools = %v, want 10", names)
	}
}
