package backend_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"war-room/backend/pkg/config"
	"war-room/backend/pkg/middleware"
	"war-room/backend/pkg/service"
)

func TestLogoService_DeduceDomain(t *testing.T) {
	cfg := &config.Config{LogosDir: t.TempDir()}
	svc := service.NewLogoService(cfg)

	tests := []struct {
		company  string
		explicit string
		expected string
	}{
		{"Acme", "", "acme.com"},
		{"Example Company", "", "examplecompany.com"},
		{"Example Company", "example.org", "example.org"},
		{"", "javascript:alert(1)", ""},
	}

	for _, tt := range tests {
		got := svc.DeduceDomain(tt.company, tt.explicit)
		if got != tt.expected {
			t.Errorf("DeduceDomain(%q, %q) = %q; want %q", tt.company, tt.explicit, got, tt.expected)
		}
	}
}

func TestLogoService_UnknownCompany(t *testing.T) {
	cfg := &config.Config{LogosDir: "/tmp"}
	svc := service.NewLogoService(cfg)

	_, _, err := svc.GetLogo("Unknown", "")
	if err == nil {
		t.Errorf("Expected error for Unknown company, got nil")
	}
}

func TestMiddleware_SecurityHeaders(t *testing.T) {
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	handler := middleware.SecurityHeaders(nextHandler)

	req := httptest.NewRequest("GET", "/api/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { closeTestResource(t, res.Body) })

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}

	expectedHeaders := map[string]string{
		"X-Content-Type-Options":     "nosniff",
		"X-Frame-Options":            "DENY",
		"Referrer-Policy":            "strict-origin-when-cross-origin",
		"Cross-Origin-Opener-Policy": "same-origin",
	}

	for k, v := range expectedHeaders {
		got := res.Header.Get(k)
		if got != v {
			t.Errorf("Expected header %s: %s, got %s", k, v, got)
		}
	}
}

func TestMiddleware_CORS(t *testing.T) {
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	corsMw := middleware.CORS("*")
	handler := corsMw(nextHandler)

	// Preflight OPTIONS test
	req := httptest.NewRequest("OPTIONS", "/api/jobs", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("Expected status 204 for OPTIONS preflight, got %d", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("Expected wildcard CORS origin, got %s",
			rec.Header().Get("Access-Control-Allow-Origin"))
	}
	if !strings.Contains(rec.Header().Get("Access-Control-Allow-Methods"), "GET") {
		t.Errorf("Expected allowed methods in CORS header")
	}
}
