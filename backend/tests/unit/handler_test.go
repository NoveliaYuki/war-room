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

func TestTechnologyCatalogHandlersAndAssignmentDeletion(t *testing.T) {
	mux, _ := setupTestApp(t)
	assertEmptyTechnologyCatalog(t, mux)
	technology := createTechnologyHandler(t, mux)
	assertTechnologyHandlerStatus(t, mux, http.MethodPut, "/api/technologies/"+technology.ID, `{"name":"Kubernetes","aliases":["K8s","Kube"]}`, http.StatusOK)
	assertTechnologyHandlerStatus(t, mux, http.MethodPut, "/api/technologies/missing", `{"name":"Missing","aliases":[]}`, http.StatusNotFound)
	assertTechnologyHandlerStatus(t, mux, http.MethodPost, "/api/technologies", `{"name":"K8s"}`, http.StatusBadRequest)

	createAndAssignTechnologyJob(t, mux, technology.ID)
	assertTechnologyCatalogShowsJob(t, mux)
	assertTechnologyDeleteLifecycle(t, mux, technology.ID)
	assertTechnologyHandlerStatus(t, mux, http.MethodDelete, "/api/technologies/unknown", "", http.StatusNotFound)
}

func assertEmptyTechnologyCatalog(t *testing.T, mux *http.ServeMux) {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/api/technologies", nil)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "[]\n" {
		t.Fatalf("empty technology catalog status=%d body=%s", response.Code, response.Body.String())
	}
}

func createAndAssignTechnologyJob(t *testing.T, mux *http.ServeMux, technologyID string) {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/jobs", bytes.NewBufferString(`{"company_name":"Example","position_title":"Engineer"}`))
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	var job struct {
		ID string `json:"id"`
	}
	if response.Code != http.StatusCreated || json.Unmarshal(response.Body.Bytes(), &job) != nil {
		t.Fatalf("create process status=%d body=%s", response.Code, response.Body.String())
	}
	request = httptest.NewRequest(http.MethodPut, "/api/jobs/"+job.ID, bytes.NewBufferString(`{"technology_ids":["`+technologyID+`"]}`))
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("assign technology status=%d body=%s", response.Code, response.Body.String())
	}
}

func assertTechnologyCatalogShowsJob(t *testing.T, mux *http.ServeMux) {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/api/technologies", nil)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"company_name":"Example"`)) {
		t.Fatalf("catalog usage response=%d body=%s", response.Code, response.Body.String())
	}
}

func assertTechnologyDeleteLifecycle(t *testing.T, mux *http.ServeMux, id string) {
	t.Helper()
	assertTechnologyHandlerStatus(t, mux, http.MethodDelete, "/api/technologies/"+id, "", http.StatusConflict)
	assertTechnologyHandlerStatus(t, mux, http.MethodDelete, "/api/technologies/"+id+"/assignments", "", http.StatusOK)
	assertTechnologyHandlerStatus(t, mux, http.MethodDelete, "/api/technologies/"+id, "", http.StatusOK)
}

func createTechnologyHandler(t *testing.T, mux *http.ServeMux) struct {
	ID string `json:"id"`
} {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/technologies", bytes.NewBufferString(`{"name":"Kubernetes","aliases":["K8s"]}`))
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	var technology struct {
		ID string `json:"id"`
	}
	if response.Code != http.StatusCreated || json.Unmarshal(response.Body.Bytes(), &technology) != nil || technology.ID == "" {
		t.Fatalf("create technology status=%d body=%s", response.Code, response.Body.String())
	}
	return technology
}

func assertTechnologyHandlerStatus(t *testing.T, mux *http.ServeMux, method, path, body string, want int) {
	t.Helper()
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != want {
		t.Fatalf("%s %s status=%d want=%d body=%s", method, path, response.Code, want, response.Body.String())
	}
}

func TestTechnologyHandlersRejectMalformedInputs(t *testing.T) {
	mux, _ := setupTestApp(t)
	for _, test := range []struct {
		method, path, body string
		want               int
	}{
		{http.MethodPost, "/api/technologies", "{", http.StatusBadRequest},
		{http.MethodPost, "/api/technologies", `{"name":" "}`, http.StatusBadRequest},
		{http.MethodPut, "/api/technologies/id", "{", http.StatusBadRequest},
		{http.MethodPut, "/api/technologies/id", `{"name":"","aliases":[]}`, http.StatusBadRequest},
		{http.MethodDelete, "/api/technologies/missing/assignments", "", http.StatusOK},
	} {
		request := httptest.NewRequest(test.method, test.path, bytes.NewBufferString(test.body))
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		if response.Code != test.want {
			t.Errorf("%s %s status=%d want=%d body=%s", test.method, test.path, response.Code, test.want, response.Body.String())
		}
	}
}

func TestTechnologyHandlersReportCatalogStorageFailures(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{DataDir: dir, AttachmentsDir: dir, LogosDir: dir, DBPath: dir + "/test.db"}
	db, err := database.InitDB(cfg)
	if err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	jobService := service.NewJobService(repo, cfg)
	mux := http.NewServeMux()
	handlers.NewHandler(jobService, service.NewLogoService(cfg), cfg).RegisterRoutes(mux)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/technologies", ""},
		{http.MethodPost, "/api/technologies", `{"name":"Go"}`},
		{http.MethodPut, "/api/technologies/one", `{"name":"Go"}`},
		{http.MethodDelete, "/api/technologies/one", ""},
		{http.MethodDelete, "/api/technologies/one/assignments", ""},
	} {
		request := httptest.NewRequest(test.method, test.path, bytes.NewBufferString(test.body))
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		if response.Code != http.StatusInternalServerError {
			t.Errorf("%s %s status=%d body=%s", test.method, test.path, response.Code, response.Body.String())
		}
	}
}

func TestBackupImportRequiresArchiveFile(t *testing.T) {
	mux, _ := setupTestApp(t)
	request := httptest.NewRequest(http.MethodPost, "/api/backup/import", bytes.NewBuffer(nil))
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("missing archive status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestBackupExportReportsStorageFailures(t *testing.T) {
	t.Run("temporary file creation", func(t *testing.T) {
		dir := t.TempDir()
		cfg := &config.Config{DataDir: dir + "/missing", AttachmentsDir: dir, LogosDir: dir, DBPath: dir + "/test.db"}
		db, err := database.InitDB(cfg)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = db.Close() })
		assertBackupExportStatus(t, handlers.NewHandler(service.NewJobService(repository.New(db), cfg), service.NewLogoService(cfg), cfg), http.StatusInternalServerError)
	})
	t.Run("database export", func(t *testing.T) {
		dir := t.TempDir()
		cfg := &config.Config{DataDir: dir, AttachmentsDir: dir, LogosDir: dir, DBPath: dir + "/test.db"}
		db, err := database.InitDB(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		assertBackupExportStatus(t, handlers.NewHandler(service.NewJobService(repository.New(db), cfg), service.NewLogoService(cfg), cfg), http.StatusInternalServerError)
	})
}

func assertBackupExportStatus(t *testing.T, handler *handlers.Handler, want int) {
	t.Helper()
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/backup/export", nil))
	if response.Code != want {
		t.Fatalf("backup export status=%d want=%d body=%s", response.Code, want, response.Body.String())
	}
}
