package backend_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"war-room/backend/pkg/config"
	"war-room/backend/pkg/database"
	"war-room/backend/pkg/handlers"
	"war-room/backend/pkg/repository"
	"war-room/backend/pkg/service"
)

func setupTestApp(t *testing.T) (*http.ServeMux, *service.JobService) {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{DataDir: dir, AttachmentsDir: dir, LogosDir: dir, DBPath: dir + "/test.db"}
	db, err := database.InitDB(cfg)
	if err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	jobSvc := service.NewJobService(repo, cfg)
	logoSvc := service.NewLogoService(cfg)

	h := handlers.NewHandler(jobSvc, logoSvc, cfg)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	return mux, jobSvc
}

func TestHealthCheck(t *testing.T) {
	mux, _ := setupTestApp(t)
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200 OK, got %d", rec.Code)
	}
}

func TestJobHandlers(t *testing.T) {
	mux, _ := setupTestApp(t)

	// Create Job invalid JSON
	req := httptest.NewRequest(http.MethodPost, "/api/jobs", bytes.NewBufferString("invalid"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 Bad Request, got %d", rec.Code)
	}

	// Create Job valid
	payload := `{"company_name":"Test","position_title":"Developer","status":"ongoing"}`
	req = httptest.NewRequest(http.MethodPost, "/api/jobs", bytes.NewBufferString(payload))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Errorf("Expected 201 Created, got %d", rec.Code)
	}

	var res map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode created job response: %v", err)
	}
	id, _ := res["id"].(string)

	// Get Job
	req = httptest.NewRequest(http.MethodGet, "/api/jobs/"+id, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200 OK, got %d", rec.Code)
	}

	// List Jobs
	req = httptest.NewRequest(http.MethodGet, "/api/jobs", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200 OK, got %d", rec.Code)
	}
}
