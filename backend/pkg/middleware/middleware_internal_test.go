package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleCORSPreflightDecisions(t *testing.T) {
	tests := []struct {
		name    string
		origin  string
		allowed bool
		status  int
	}{
		{name: "no origin", status: http.StatusNoContent},
		{name: "allowed origin", origin: "https://app.example", allowed: true, status: http.StatusNoContent},
		{name: "rejected origin", origin: "https://other.example", status: http.StatusForbidden},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handleCORSPreflight(recorder, test.origin, test.allowed)
			if recorder.Code != test.status {
				t.Fatalf("status=%d, want=%d", recorder.Code, test.status)
			}
			if test.status == http.StatusNoContent && recorder.Header().Get("Access-Control-Allow-Headers") == "" {
				t.Fatal("successful preflight is missing allowed headers")
			}
		})
	}
}

func TestSetCORSOriginHeadersVariants(t *testing.T) {
	tests := []struct {
		name           string
		allowedOrigins string
		origin         string
		allowed        bool
		wantOrigin     string
	}{
		{name: "wildcard without origin", allowedOrigins: "*", allowed: true, wantOrigin: "*"},
		{name: "specific origin", allowedOrigins: "https://app.example", origin: "https://app.example", allowed: true, wantOrigin: "https://app.example"},
		{name: "wildcard origin", allowedOrigins: "*", origin: "https://app.example", allowed: true, wantOrigin: "*"},
		{name: "rejected origin", allowedOrigins: "https://app.example", origin: "https://other.example"},
		{name: "no origin on fixed policy", allowedOrigins: "https://app.example"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			setCORSOriginHeaders(recorder, test.allowedOrigins, test.origin, test.allowed)
			if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != test.wantOrigin {
				t.Fatalf("allow origin=%q, want=%q", got, test.wantOrigin)
			}
			if test.origin != "" && recorder.Header().Get("Vary") != "Origin" {
				t.Fatalf("Vary header=%q", recorder.Header().Get("Vary"))
			}
		})
	}
}
