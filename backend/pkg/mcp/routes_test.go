package mcp

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// routeTools names the MCP tool that does what each API route does.
var routeTools = map[string]string{
	"GET /api/jobs":                    "list_jobs",
	"GET /api/jobs/{id}":               "get_job",
	"POST /api/jobs":                   "create_job",
	"PUT /api/jobs/{id}":               "update_job",
	"GET /api/technologies":            "list_technologies",
	"POST /api/technologies":           "create_technology",
	"PUT /api/technologies/{id}":       "update_technology",
	"GET /api/cv/versions":             "list_cv_versions",
	"POST /api/stages":                 "create_stage",
	"PUT /api/stages/{id}":             "update_stage",
	"PUT /api/stages/{id}/schedule":    "update_stage",
	"PUT /api/stages/{id}/set-current": "update_stage",
}

// routesWithoutTools records why an API route has no MCP tool. A new route
// must be added to routeTools with a tool, or here with a reason.
var routesWithoutTools = map[string]string{ // #nosec G101 -- route paths and reasons, not credentials.
	"GET /":                                     "serves the browser app",
	"GET /api/health":                           "reports container health",
	"POST /api/test/session":                    "exists only in test mode",
	"GET /api/auth/me":                          "describes the browser session",
	"POST /api/auth/signout":                    "ends the browser session",
	"GET /api/google/callback":                  "completes Google sign-in in the browser",
	"GET /api/google/connect":                   "starts Google sign-in in the browser",
	"GET /api/google/folders":                   "Drive setup happens in the browser",
	"GET /api/google/picker-token":              "Drive setup happens in the browser",
	"GET /api/google/status":                    "Drive setup happens in the browser",
	"PUT /api/google/folder":                    "Drive setup happens in the browser",
	"POST /api/google/backup":                   "Drive backups are started in the browser",
	"POST /api/google/revoke":                   "revoking Google access is a user decision in the browser",
	"GET /api/backup/export":                    "backups are encrypted files the user downloads",
	"POST /api/backup/import":                   "restoring a backup replaces data and needs the user",
	"GET /api/account/avatar":                   "profile photos are image files",
	"POST /api/account/avatar":                  "profile photos are image files",
	"DELETE /api/account/avatar":                "profile photos are image files",
	"GET /api/company-logo":                     "logos are image files",
	"POST /api/attachments":                     "attachments are uploaded files",
	"GET /api/attachments/{id}/download":        "attachments are uploaded files",
	"DELETE /api/attachments/{id}":              "attachments are uploaded files",
	"POST /api/cv/versions":                     "CV versions are uploaded files",
	"GET /api/cv/versions/{id}/download":        "CV versions are uploaded files",
	"DELETE /api/cv/versions/{id}":              "deleting stays a user action in the app",
	"DELETE /api/jobs/{id}":                     "deleting stays a user action in the app",
	"DELETE /api/stages/{id}":                   "deleting stays a user action in the app",
	"DELETE /api/questions/{id}":                "deleting stays a user action in the app",
	"DELETE /api/search-periods/{id}":           "deleting stays a user action in the app",
	"DELETE /api/technologies/{id}":             "deleting stays a user action in the app",
	"DELETE /api/technologies/{id}/assignments": "deleting stays a user action in the app",
	"POST /api/jobs/import-preview":             "agents read postings themselves and call create_job",
	"PUT /api/jobs/{id}/cv-version":             "not available to agents yet; create_job links the latest CV",
	"GET /api/jobs/counts":                      "not available to agents yet; list_jobs returns every status",
	"PUT /api/jobs/reorder":                     "board order is arranged by the user",
	"GET /api/meetings":                         "not available to agents yet; get_job returns each stage's meeting",
	"POST /api/questions":                       "not available to agents yet",
	"PUT /api/questions/{id}":                   "not available to agents yet",
	"PUT /api/questions/reorder":                "not available to agents yet",
	"PUT /api/stages/reorder":                   "not available to agents yet",
	"GET /api/search-periods":                   "not available to agents yet",
	"POST /api/search-periods":                  "not available to agents yet",
	"PUT /api/search-periods/{id}":              "not available to agents yet",
}

var routePattern = regexp.MustCompile(`mux\.HandleFunc\("([A-Z]+ /[^"]*)"`)

// TestEveryAPIRouteIsCoveredOrExplained keeps the MCP server current with the
// API: a new route fails this test until it gets a tool or a recorded reason.
func TestEveryAPIRouteIsCoveredOrExplained(t *testing.T) {
	routes := registeredRoutes(t)
	tools := registeredToolNames(t)
	for route := range routes {
		tool, covered := routeTools[route]
		_, explained := routesWithoutTools[route]
		switch {
		case covered && explained:
			t.Errorf("%s is listed both with a tool and without one", route)
		case covered && !tools[tool]:
			t.Errorf("%s maps to %q, which the MCP server does not register", route, tool)
		case !covered && !explained:
			t.Errorf("%s has no MCP tool: add one in backend/pkg/mcp, or record why not in routesWithoutTools", route)
		}
	}
	assertListedRoutesExist(t, routes)
}

// assertListedRoutesExist reports listed routes the API no longer registers.
func assertListedRoutesExist(t *testing.T, routes map[string]bool) {
	t.Helper()
	for _, listed := range []map[string]string{routeTools, routesWithoutTools} {
		for route := range listed {
			if !routes[route] {
				t.Errorf("%s is listed but no longer registered; remove it", route)
			}
		}
	}
}

func registeredRoutes(t *testing.T) map[string]bool {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "handlers", "*.go"))
	if err != nil {
		t.Fatalf("find handler sources: %v", err)
	}
	routes := map[string]bool{}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		source, err := os.ReadFile(file) // #nosec G304 -- reads this repository's handler sources.
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		for _, match := range routePattern.FindAllStringSubmatch(string(source), -1) {
			routes[match[1]] = true
		}
	}
	if len(routes) < 40 {
		t.Fatalf("found only %d API routes; the route pattern no longer matches the handlers", len(routes))
	}
	return routes
}

func registeredToolNames(t *testing.T) map[string]bool {
	t.Helper()
	source, err := os.ReadFile("mcp.go")
	if err != nil {
		t.Fatalf("read mcp.go: %v", err)
	}
	names := map[string]bool{}
	for _, match := range regexp.MustCompile(`Name: "([a-z_]+)"`).FindAllStringSubmatch(string(source), -1) {
		names[match[1]] = true
	}
	return names
}
