// Package config loads application settings from the process environment.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// Config contains runtime paths and network settings for the application.
type Config struct {
	Port                      int
	Host                      string
	DataDir                   string
	DBPath                    string
	BackupPath                string
	InitialBackupPath         string
	AttachmentsDir            string
	CVDir                     string
	LogosDir                  string
	StaticDir                 string
	CORSAllowed               string
	LogoLookupEnabled         bool
	GoogleClientID            string
	GoogleClientSecret        string
	GoogleBackupEncryptionKey string
	GooglePickerAPIKey        string
	GoogleCloudProjectNumber  string
	GoogleRedirectURI         string
	GoogleFrontendURL         string
	GoogleDrivePath           string
}

// Load reads configuration, applies defaults, and creates writable data directories.
func Load() *Config {
	absDataDir := absoluteDataDirectory(configuredDataDirectory())
	cfg := &Config{
		Port:                      configuredPort(),
		Host:                      valueOrDefault("HOST", "127.0.0.1"),
		DataDir:                   absDataDir,
		DBPath:                    filepath.Join(absDataDir, "jobs.db"),
		BackupPath:                filepath.Join(absDataDir, "backup.json"),
		InitialBackupPath:         os.Getenv("WARROOM_INITIAL_BACKUP"),
		AttachmentsDir:            filepath.Join(absDataDir, "attachments"),
		CVDir:                     filepath.Join(absDataDir, "cvs"),
		LogosDir:                  filepath.Join(absDataDir, "logos"),
		StaticDir:                 configuredStaticDirectory(),
		CORSAllowed:               valueOrDefault("CORS_ALLOWED_ORIGINS", "http://localhost:3000,http://127.0.0.1:3000"),
		LogoLookupEnabled:         boolOrDefault("LOGO_LOOKUP_ENABLED", false),
		GoogleClientID:            os.Getenv("GOOGLE_CLIENT_ID"),
		GoogleClientSecret:        os.Getenv("GOOGLE_CLIENT_SECRET"),
		GoogleBackupEncryptionKey: os.Getenv("GOOGLE_BACKUP_ENCRYPTION_KEY"),
		GooglePickerAPIKey:        os.Getenv("GOOGLE_PICKER_API_KEY"),
		GoogleCloudProjectNumber:  os.Getenv("GOOGLE_CLOUD_PROJECT_NUMBER"),
		GoogleRedirectURI:         valueOrDefault("GOOGLE_REDIRECT_URI", "http://localhost:4040/api/google/callback"),
		GoogleFrontendURL:         valueOrDefault("GOOGLE_FRONTEND_URL", "http://localhost:3000"),
		GoogleDrivePath:           filepath.Join(absDataDir, "google-drive.json"),
	}

	if err := createDataDirectories(cfg); err != nil {
		panic(err)
	}

	return cfg
}

func configuredPort() int {
	port, err := strconv.Atoi(os.Getenv("PORT"))
	if err != nil || port <= 0 {
		return 4040
	}
	return port
}

func configuredDataDirectory() string {
	if directory := os.Getenv("WARROOM_DATA_DIR"); directory != "" {
		return directory
	}
	return valueOrDefault("ONGOING_DATA_DIR", "data")
}

func absoluteDataDirectory(directory string) string {
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return directory
	}
	return absolute
}

func configuredStaticDirectory() string {
	if directory := os.Getenv("STATIC_DIR"); directory != "" {
		return directory
	}
	for _, directory := range []string{"frontend/public", "public"} {
		if _, err := os.Stat(directory); err == nil {
			return directory
		}
	}
	return "frontend/public"
}

func valueOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func boolOrDefault(name string, fallback bool) bool {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	enabled, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return enabled
}

func createDataDirectories(cfg *Config) error {
	for _, directory := range []string{cfg.DataDir, cfg.AttachmentsDir, cfg.CVDir, cfg.LogosDir} {
		if err := os.MkdirAll(directory, 0750); err != nil {
			return fmt.Errorf("create data directory %q: %w", directory, err)
		}
	}
	return nil
}
