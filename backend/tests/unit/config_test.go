package backend_test

import (
	"os"
	"path/filepath"
	"testing"
	"war-room/backend/pkg/config"
)

func TestConfigLoad(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("PORT", "9999")
	t.Setenv("HOST", "127.0.0.1")
	t.Setenv("WARROOM_DATA_DIR", dataDir)
	t.Setenv("STATIC_DIR", "static")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://app.example")

	cfg := config.Load()
	if cfg.Port != 9999 || cfg.Host != "127.0.0.1" {
		t.Fatalf("expected configured address, got %s:%d", cfg.Host, cfg.Port)
	}
	if cfg.DBPath != filepath.Join(dataDir, "jobs.db") || cfg.BackupPath != filepath.Join(dataDir, "backup.json") {
		t.Fatalf("unexpected storage paths: %+v", cfg)
	}
	if cfg.CORSAllowed != "https://app.example" || cfg.StaticDir != "static" {
		t.Fatalf("expected configured CORS and static paths, got %+v", cfg)
	}
	for _, path := range []string{cfg.DataDir, cfg.AttachmentsDir, cfg.LogosDir} {
		if info, err := os.Stat(path); err != nil || !info.IsDir() {
			t.Fatalf("expected runtime directory %q to exist, stat error: %v", path, err)
		}
	}
}

func TestConfigLoadEnablesLogoLookup(t *testing.T) {
	t.Setenv("WARROOM_DATA_DIR", t.TempDir())
	t.Setenv("LOGO_LOOKUP_ENABLED", "true")
	if cfg := config.Load(); !cfg.LogoLookupEnabled {
		t.Fatal("expected remote logo lookup to be enabled by explicit configuration")
	}
}

func TestConfigLoadDefaultsAndInvalidPort(t *testing.T) {
	t.Setenv("PORT", "invalid")
	t.Setenv("HOST", "")
	t.Setenv("WARROOM_DATA_DIR", t.TempDir())
	t.Setenv("ONGOING_DATA_DIR", "")
	t.Setenv("STATIC_DIR", "")
	t.Setenv("CORS_ALLOWED_ORIGINS", "")
	t.Setenv("LOGO_LOOKUP_ENABLED", "invalid")

	cfg := config.Load()
	if cfg.Port != 4040 || cfg.Host != "127.0.0.1" {
		t.Fatalf("expected default address, got %s:%d", cfg.Host, cfg.Port)
	}
	if cfg.CORSAllowed != "http://localhost:3000,http://127.0.0.1:3000" {
		t.Fatalf("unexpected CORS defaults: %q", cfg.CORSAllowed)
	}
	if cfg.LogoLookupEnabled {
		t.Fatal("remote logo lookup must remain disabled for missing or invalid configuration")
	}
}
