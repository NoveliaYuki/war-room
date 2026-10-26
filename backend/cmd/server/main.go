package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
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
	"war-room/backend/pkg/middleware"
	"war-room/backend/pkg/repository"
	"war-room/backend/pkg/service"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("Starting War Room Selection Process Command Center...")

	cfg := config.Load()
	log.Printf("Configuration loaded: Host=%s Port=%d DataDir=%s StaticDir=%s",
		cfg.Host, cfg.Port, cfg.DataDir, cfg.StaticDir)

	service.RemoveStaleBackupTempFiles(cfg.DataDir)

	db, err := database.InitDB(cfg)
	if err != nil {
		log.Fatalf("Fatal: Failed to initialize database: %v", err)
	}
	defer func() { _ = db.Close() }()
	removeStaleGoogleDriveFile(cfg)

	repo := repository.New(db)
	schedulerContext, stopScheduler := context.WithCancel(context.Background())
	defer stopScheduler()
	jobSvc := service.NewJobService(repo, cfg)
	if err := jobSvc.MigrateLegacyCVAttachments(); err != nil {
		log.Fatalf("Fatal: Failed to migrate legacy CV attachments: %v", err)
	}
	logoSvc := service.NewLogoService(cfg)
	authSvc := service.NewAuthService(db, cfg)
	driveSvc := service.NewDriveBackupService(db, cfg, func(userID string, w io.Writer) error { return jobSvc.ExportArchive(userID, w) })
	logGoogleConfigurationIssues(driveSvc)
	jobSvc.SetDriveTrigger(driveSvc.Trigger)
	service.StartInactiveWaitingSchedulerWithCallback(schedulerContext, repo, 24*time.Hour, driveSvc.Trigger)
	h := handlers.NewHandler(jobSvc, logoSvc, cfg)
	h.SetDriveService(driveSvc)
	h.SetAuthService(authSvc)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	handlerWithMiddleware := middleware.Chain(
		mux,
		middleware.SecurityHeaders,
		middleware.CORS(cfg.CORSAllowed),
		middleware.Logger,
		middleware.RequireSession(authSvc, "/api/test/session"),
	)

	srv := newHTTPServer(cfg, handlerWithMiddleware)
	runServer(srv, cfg)
}

// logGoogleConfigurationIssues prints exactly which Google-related
// environment variables are missing or invalid to the server's own log.
// This detail must never reach an HTTP client (handleGoogleConnect requires
// no session, so any unauthenticated caller could read it); the generic
// service.ErrGoogleNotConfigured message is what HTTP responses carry.
func logGoogleConfigurationIssues(drive *service.DriveBackupService) {
	if issues := drive.ConfigurationIssues(); len(issues) > 0 {
		log.Printf("Warning: Google sign-in is not configured: %s", strings.Join(issues, "; "))
	}
}

// removeStaleGoogleDriveFile deletes the pre-multi-user Drive credential
// file. Credentials now live per-user in the google_drive_accounts table;
// this file is dead weight that would otherwise sit on disk holding a live
// refresh token nothing reads anymore.
func removeStaleGoogleDriveFile(cfg *config.Config) {
	path := filepath.Join(cfg.DataDir, "google-drive.json")
	if err := os.Remove(path); err == nil {
		log.Printf("Removed pre-multi-user Google Drive credential file %s; reconnect Drive after signing in.", path)
	} else if !os.IsNotExist(err) {
		log.Printf("Warning: could not remove stale Google Drive credential file %s: %v", path, err)
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
		IdleTimeout:       120 * time.Second,
	}
}

func runServer(srv *http.Server, cfg *config.Config) {
	// Server run context for graceful shutdown
	serverErrChan := make(chan error, 1)
	go func() {
		log.Printf("War Room backend server running at http://%s:%d", cfg.Host, cfg.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrChan <- err
		}
	}()

	// Listen for termination signals
	shutdownChan := make(chan os.Signal, 1)
	signal.Notify(shutdownChan, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)

	select {
	case err := <-serverErrChan:
		log.Fatalf("Server startup failed: %v", err)
	case sig := <-shutdownChan:
		log.Printf("Received signal %v. Initiating graceful shutdown...", sig)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("Graceful shutdown error: %v", err)
	} else {
		log.Println("War Room backend server gracefully stopped.")
	}
}
