package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"war-room/backend/pkg/config"
)

func TestMCPRouteCoexistsWithStaticFallback(t *testing.T) {
	handler := NewHandler(nil, nil, &config.Config{StaticDir: t.TempDir()})
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
		request := httptest.NewRequest(method, "/api/mcp", nil)
		if _, pattern := mux.Handler(request); pattern != method+" /api/mcp" {
			t.Errorf("%s /api/mcp routed to %q", method, pattern)
		}
	}
	if _, pattern := mux.Handler(httptest.NewRequest(http.MethodGet, "/index.html", nil)); pattern != "GET /" {
		t.Errorf("static request routed to %q", pattern)
	}
}
