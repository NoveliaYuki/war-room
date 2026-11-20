package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"war-room/backend/pkg/config"
	"war-room/backend/pkg/database"
	"war-room/backend/pkg/handlers"
	"war-room/backend/pkg/logging"
	"war-room/backend/pkg/middleware"
	"war-room/backend/pkg/repository"
	"war-room/backend/pkg/service"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "api-token" {
		os.Exit(runAPITokenCLI(os.Args[2:], os.Stdout, os.Stderr))
	}
	cfg := config.Load()
	if err := logging.Setup(os.Stdout, cfg.LogLevel, cfg.LogFormat); err != nil {
		slog.Warn("invalid logging settings; using defaults where needed", "error", err.Error())
	}
	slog.Info("starting war room backend", "host", cfg.Host, "port", cfg.Port,
		"data_dir", cfg.DataDir, "static_dir", cfg.StaticDir, "log_level", cfg.LogLevel, "log_format", cfg.LogFormat,
		"test_mode", cfg.TestMode, "https_only_sessions", cfg.SessionCookieSecure,
		"job_import_enabled", cfg.JobImportEnabled, "logo_lookup_enabled", cfg.LogoLookupEnabled,
		"backup_scheduler_enabled", cfg.BackupSchedulerEnabled,
		"allowed_email_count", len(cfg.AuthAllowedEmails))

	service.RemoveStaleBackupTempFiles(cfg.DataDir)

	db, err := database.InitDB(cfg)
	if err != nil {
		fatal("could not initialize database", err)
	}
	defer func() { _ = db.Close() }()
	removeStaleGoogleDriveFile(cfg)

	repo := repository.New(db)
	schedulerContext, stopScheduler := context.WithCancel(context.Background())
	defer stopScheduler()
	jobSvc := service.NewJobService(repo, cfg)
	if err := jobSvc.MigrateLegacyCVAttachments(); err != nil {
		fatal("could not migrate legacy CV attachments", err)
	}
	logoSvc := service.NewLogoService(cfg)
	authSvc := service.NewAuthService(db, cfg)
	driveSvc := service.NewDriveBackupService(db, cfg, func(userID string, w io.Writer) error { return jobSvc.ExportArchive(userID, w) })
	logGoogleConfigurationIssues(driveSvc)
	jobSvc.SetDriveTrigger(driveSvc.Trigger)
	service.StartInactiveWaitingSchedulerWithCallback(schedulerContext, repo, 24*time.Hour, driveSvc.Trigger)
	scheduleSvc := service.NewBackupScheduleService(repo, time.Now)
	defer startBackupScheduler(schedulerContext, cfg, repo, driveSvc)()
	h := handlers.NewHandler(jobSvc, logoSvc, cfg)
	h.SetDriveService(driveSvc)
	h.SetBackupScheduleService(scheduleSvc)
	h.SetAuthService(authSvc)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	handlerWithMiddleware := middleware.Chain(
		mux,
		middleware.Logger,
		middleware.Recoverer,
		middleware.SecurityHeaders,
		middleware.CORS(cfg.CORSAllowed),
		middleware.RequireSession(authSvc, "/api/test/session"),
	)

	srv := newHTTPServer(cfg, handlerWithMiddleware)
	runServer(srv, cfg)
}

// startBackupScheduler starts the automatic Drive backup scheduler, checking
// for due backups every minute, and returns a function that stops it. Stop
// runs before the database closes and waits briefly for running uploads.
func startBackupScheduler(ctx context.Context, cfg *config.Config, repo *repository.Repository, drive *service.DriveBackupService) func() {
	if !cfg.BackupSchedulerEnabled {
		slog.Info("scheduled google drive backups are disabled")
		return func() {}
	}
	ticker := time.NewTicker(time.Minute)
	scheduler := service.NewBackupScheduler(repo, drive.BackupNow, time.Now)
	scheduler.Start(ctx, ticker.C)
	return func() {
		ticker.Stop()
		if !scheduler.Stop(10 * time.Second) {
			slog.Warn("scheduled google drive backups were still running at shutdown")
		}
	}
}

// logGoogleConfigurationIssues prints exactly which Google-related
// environment variables are missing or invalid to the server's own log.
// This detail must never reach an HTTP client (handleGoogleConnect requires
// no session, so any unauthenticated caller could read it); the generic
// service.ErrGoogleNotConfigured message is what HTTP responses carry.
func logGoogleConfigurationIssues(drive *service.DriveBackupService) {
	if issues := drive.ConfigurationIssues(); len(issues) > 0 {
		slog.Warn("google sign-in is not configured", "issues", strings.Join(issues, "; "))
	}
}

// removeStaleGoogleDriveFile deletes the pre-multi-user Drive credential
// file. Credentials now live per-user in the google_drive_accounts table;
// this file is dead weight that would otherwise sit on disk holding a live
// refresh token nothing reads anymore.
func removeStaleGoogleDriveFile(cfg *config.Config) {
	path := filepath.Join(cfg.DataDir, "google-drive.json")
	if err := os.Remove(path); err == nil {
		slog.Info("removed pre-multi-user google drive credential file; reconnect Drive after signing in", "path", path)
	} else if !os.IsNotExist(err) {
		slog.Warn("could not remove stale google drive credential file", "path", path, "error", err.Error())
	}
}

func newHTTPServer(cfg *config.Config, handler http.Handler) *http.Server {
	serverAddr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	// No whole-request Read/Write timeouts: they would cut off large backup
	// transfers and uploads on slow links. Body sizes are bounded per handler.
	return &http.Server{
		Addr:              serverAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ErrorLog:          slog.NewLogLogger(slog.Default().Handler(), slog.LevelWarn),
		IdleTimeout:       120 * time.Second,
	}
}

func runServer(srv *http.Server, cfg *config.Config) {
	// Server run context for graceful shutdown
	serverErrChan := make(chan error, 1)
	go func() {
		slog.Info("war room backend listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrChan <- err
		}
	}()

	// Listen for termination signals
	shutdownChan := make(chan os.Signal, 1)
	signal.Notify(shutdownChan, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)

	select {
	case err := <-serverErrChan:
		fatal("server startup failed", err)
	case sig := <-shutdownChan:
		slog.Info("shutdown signal received; draining requests", "signal", sig.String())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("graceful shutdown failed", "error", err.Error())
	} else {
		slog.Info("war room backend stopped")
	}
}

// fatal logs err as the reason the process cannot continue, then exits.
func fatal(message string, err error) {
	slog.Error(message, "error", err.Error())
	os.Exit(1)
}
