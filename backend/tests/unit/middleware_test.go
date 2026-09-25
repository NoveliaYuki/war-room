package backend_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"war-room/backend/pkg/middleware"
)

func TestMiddleware(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	chain := middleware.Chain(handler, middleware.RequestLogger, middleware.SecurityHeaders, middleware.CORS("http://localhost:3000"))

	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	rec := httptest.NewRecorder()

	chain.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("Expected 204 No Content for OPTIONS, got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	rec = httptest.NewRecorder()
	chain.ServeHTTP(rec, req)

	if rec.Header().Get("Access-Control-Allow-Origin") != "http://localhost:3000" {
		t.Errorf("Expected CORS origin header")
	}
}

func TestCORSRejectsUnapprovedOriginAndOmitsCredentials(t *testing.T) {
	handler := middleware.CORS("http://localhost:3000,https://app.example")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodOptions, "/api/jobs", nil)
	req.Header.Set("Origin", "https://attacker.example")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected unapproved preflight to be denied, got %d", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "" || rec.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Fatalf("unexpected CORS permission headers: %v", rec.Header())
	}
}

func TestCORSRejectsSimpleRequestsBeforeHandler(t *testing.T) {
	handlerCalled := false
	handler := middleware.CORS("http://localhost:3000")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusCreated)
	}))
	req := httptest.NewRequest(http.MethodPost, "/api/jobs", nil)
	req.Header.Set("Origin", "https://attacker.example")
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected unapproved simple request to be denied, got %d", rec.Code)
	}
	if handlerCalled {
		t.Fatal("handler ran for an unapproved origin")
	}
}

func TestCORSWildcardDoesNotEnableCredentials(t *testing.T) {
	handler := middleware.CORS("*")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/jobs", nil)
	req.Header.Set("Origin", "https://client.example")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("expected wildcard origin, got %q", rec.Header().Get("Access-Control-Allow-Origin"))
	}
	if rec.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Fatalf("wildcard CORS must not allow credentials")
	}
}
