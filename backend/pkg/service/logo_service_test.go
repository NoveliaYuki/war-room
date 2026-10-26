package service

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"war-room/backend/pkg/config"
)

func TestLogoServiceCacheAndInputValidation(t *testing.T) {
	logoDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(logoDir, "example.com.svg"), []byte("<svg/>"), 0600); err != nil {
		t.Fatal(err)
	}
	service := NewLogoService(&config.Config{LogosDir: logoDir})
	data, mimeType, err := service.GetLogo("Example", "https://EXAMPLE.com/path")
	if err != nil || string(data) != "<svg/>" || mimeType != "image/svg+xml" {
		t.Fatalf("cached logo = %q, %q, %v", data, mimeType, err)
	}
	for _, input := range []struct{ name, domain string }{{"", "example.com"}, {"unknown", "example.com"}, {"Example", "not a host"}} {
		if _, _, err := service.GetLogo(input.name, input.domain); err == nil {
			t.Errorf("GetLogo(%q, %q) unexpectedly succeeded", input.name, input.domain)
		}
	}
	if _, _, err := service.GetLogo("Acme", "acme.com"); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("disabled uncached lookup error = %v", err)
	}
}

func TestLogoServiceFetchesAndCachesBoundedImage(t *testing.T) {
	logoDir := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("\x89PNG\r\n\x1a\nsmall image"))
	}))
	defer server.Close()
	service := NewLogoServiceWithClient(&config.Config{LogosDir: logoDir, LogoLookupEnabled: true}, server.Client(), func(string) string { return server.URL })
	data, mimeType, err := service.GetLogo("Acme", "acme.test")
	if err != nil || mimeType != "image/png" || len(data) == 0 {
		t.Fatalf("fetch logo = %d bytes, %q, %v", len(data), mimeType, err)
	}
	if _, err := os.Stat(filepath.Join(logoDir, "acme.test.png")); err != nil {
		t.Fatalf("fetched logo was not cached: %v", err)
	}
	if _, _, err := service.GetLogo("Acme", "acme.test"); err != nil {
		t.Fatalf("cached follow-up lookup failed: %v", err)
	}
}

func TestLogoServiceRejectsBadRemoteResponses(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{name: "http status", status: http.StatusNotFound, body: "missing"},
		{name: "not image", status: http.StatusOK, body: "plain text"},
		{name: "empty", status: http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			service := NewLogoServiceWithClient(&config.Config{LogosDir: t.TempDir(), LogoLookupEnabled: true}, server.Client(), func(string) string { return server.URL })
			if _, _, err := service.GetLogo("Acme", "acme.test"); err == nil {
				t.Fatal("invalid remote response unexpectedly succeeded")
			}
		})
	}
}
