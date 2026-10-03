package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAbsoluteDataDirectoryFallsBackWhenWorkingDirectoryDisappears(t *testing.T) {
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	removedDirectory := t.TempDir()
	if err := os.Chdir(removedDirectory); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(workingDirectory); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	}()
	if err := os.RemoveAll(removedDirectory); err != nil {
		t.Fatal(err)
	}
	if got := absoluteDataDirectory(filepath.Join("relative", "data")); got != filepath.Join("relative", "data") {
		t.Fatalf("fallback path=%q", got)
	}
}

func TestCreateDataDirectoriesReturnsFilesystemErrors(t *testing.T) {
	root := t.TempDir()
	blockingFile := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(blockingFile, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := createDataDirectories(&Config{
		DataDir: blockingFile, AttachmentsDir: filepath.Join(root, "attachments"), LogosDir: filepath.Join(root, "logos"),
	}); err == nil {
		t.Fatal("directory creation under a file should fail")
	}
}

func TestLoadUsesEnvironmentOverridesAndCreatesDirectories(t *testing.T) {
	directory := t.TempDir()
	static := filepath.Join(directory, "static")
	t.Setenv("WARROOM_DATA_DIR", directory)
	t.Setenv("PORT", "5050")
	t.Setenv("HOST", "0.0.0.0")
	t.Setenv("STATIC_DIR", static)
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://app.example")
	t.Setenv("LOGO_LOOKUP_ENABLED", "true")
	t.Setenv("WARROOM_INITIAL_BACKUP", filepath.Join(directory, "seed.json"))
	cfg := Load()
	if cfg.Port != 5050 || cfg.Host != "0.0.0.0" || cfg.StaticDir != static || cfg.CORSAllowed != "https://app.example" || !cfg.LogoLookupEnabled {
		t.Fatalf("environment overrides not applied: %+v", cfg)
	}
	for _, path := range []string{cfg.DataDir, cfg.AttachmentsDir, cfg.CVDir, cfg.LogosDir} {
		if info, err := os.Stat(path); err != nil || !info.IsDir() {
			t.Errorf("directory %q not created: %v", path, err)
		}
	}
}

func TestConfigurationHelpersFallBackForInvalidValues(t *testing.T) {
	t.Setenv("PORT", "invalid")
	if got := configuredPort(); got != 4040 {
		t.Fatalf("invalid port=%d", got)
	}
	t.Setenv("PORT", "0")
	if got := configuredPort(); got != 4040 {
		t.Fatalf("zero port=%d", got)
	}
	t.Setenv("LOGO_LOOKUP_ENABLED", "invalid")
	if boolOrDefault("LOGO_LOOKUP_ENABLED", true) != true {
		t.Fatal("invalid bool did not preserve fallback")
	}
	t.Setenv("LOGO_LOOKUP_ENABLED", "false")
	if boolOrDefault("LOGO_LOOKUP_ENABLED", true) {
		t.Fatal("false bool override was ignored")
	}
	t.Setenv("WARROOM_DATA_DIR", "")
	t.Setenv("ONGOING_DATA_DIR", "legacy-data")
	if got := configuredDataDirectory(); got != "legacy-data" {
		t.Fatalf("legacy data directory=%q", got)
	}
	t.Setenv("ONGOING_DATA_DIR", "")
	if got := configuredDataDirectory(); got != "data" {
		t.Fatalf("default data directory=%q", got)
	}
	t.Setenv("STATIC_DIR", "")
	if got := configuredStaticDirectory(); got != "frontend/public" {
		t.Fatalf("default static directory=%q", got)
	}
}
