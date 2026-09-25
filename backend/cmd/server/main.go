package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
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

	db, err := database.InitDB(cfg)
	if err != nil {
		log.Fatalf("Fatal: Failed to initialize database: %v", err)
	}
	defer func() { _ = db.Close() }()

	repo := repository.New(db)
	jobSvc := service.NewJobService(repo, cfg)
	logoSvc := service.NewLogoService(cfg)
	h := handlers.NewHandler(jobSvc, logoSvc, cfg)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	handlerWithMiddleware := middleware.Chain(
		mux,
		middleware.SecurityHeaders,
		middleware.CORS(cfg.CORSAllowed),
		middleware.Logger,
	)

	srv := newHTTPServer(cfg, handlerWithMiddleware)
	runServer(srv, cfg)
}

func newHTTPServer(cfg *config.Config, handler http.Handler) *http.Server {
	serverAddr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	return &http.Server{
		Addr:              serverAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
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
