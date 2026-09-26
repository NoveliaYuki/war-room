package backend_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"war-room/backend/pkg/config"
	"war-room/backend/pkg/database"
	"war-room/backend/pkg/handlers"
	"war-room/backend/pkg/middleware"
	"war-room/backend/pkg/models"
	"war-room/backend/pkg/repository"
	"war-room/backend/pkg/service"
)

func closeIntegrationResource(t *testing.T, resource interface{ Close() error }) {
	t.Helper()
	if err := resource.Close(); err != nil {
		t.Errorf("close test resource: %v", err)
	}
}

func setupTestServer(t *testing.T) (*httptest.Server, *config.Config, func()) {
	tmpDir, err := os.MkdirTemp("", "warroom_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	cfg := &config.Config{
		Port:           0,
		Host:           "127.0.0.1",
		DataDir:        tmpDir,
		DBPath:         filepath.Join(tmpDir, "test_jobs.db"),
		BackupPath:     filepath.Join(tmpDir, "test_backup.json"),
		AttachmentsDir: filepath.Join(tmpDir, "attachments"),
		LogosDir:       filepath.Join(tmpDir, "logos"),
		CORSAllowed:    "*",
	}
	for _, directory := range []string{cfg.AttachmentsDir, cfg.LogosDir} {
		if err := os.MkdirAll(directory, 0700); err != nil {
			t.Fatalf("create integration directory: %v", err)
		}
	}

	db, err := database.InitDB(cfg)
	if err != nil {
		t.Fatalf("Failed to init test db: %v", err)
	}

	repo := repository.New(db)
	jobSvc := service.NewJobService(repo, cfg)
	logoSvc := service.NewLogoService(cfg)
	h := handlers.NewHandler(jobSvc, logoSvc, cfg)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	handler := middleware.Chain(mux, middleware.SecurityHeaders, middleware.CORS("*"))

	server := httptest.NewServer(handler)

	cleanup := func() {
		server.Close()
		closeIntegrationResource(t, db)
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Errorf("remove integration test directory: %v", err)
		}
	}

	return server, cfg, cleanup
}

// TestAPI_JobLifecycle covers creating, scheduling, querying, and deleting a job.
func TestAPI_JobLifecycle(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()
	client := ts.Client()
	assertEmptyJobCounts(t, client, ts.URL)
	job := createLifecycleJob(t, client, ts.URL)
	assertLifecycleJobDetails(t, client, ts.URL, job.ID)
	stageID := job.Stages[0].ID
	setLifecycleCurrentStage(t, client, ts.URL, stageID)
	scheduleLifecycleStage(t, client, ts.URL, stageID)
	assertLifecycleMeeting(t, client, ts.URL)
	createLifecycleQuestion(t, client, ts.URL, stageID)
	deleteLifecycleJob(t, client, ts.URL, job.ID)
}

func assertEmptyJobCounts(t *testing.T, client *http.Client, serverURL string) {
	t.Helper()
	response := mustRequest(t, client, http.MethodGet, serverURL+"/api/jobs/counts", "")
	t.Cleanup(func() { closeIntegrationResource(t, response.Body) })
	requireStatus(t, response, http.StatusOK)
	var counts models.JobCounts
	mustDecode(t, response, &counts)
	if counts.All != 0 {
		t.Fatalf("job count=%d, want 0", counts.All)
	}
}

func createLifecycleJob(t *testing.T, client *http.Client, serverURL string) models.Job {
	t.Helper()
	minSal := int64(37000)
	maxSal := int64(83000)
	recName := "Casey Example"
	employmentType := models.EmploymentTypePermanentB2B
	companyDomain := "example.test"
	createPayload := models.CreateJobInput{
		CompanyName:    "Example Company",
		PositionTitle:  "Software Engineer",
		SalaryMin:      &minSal,
		SalaryMax:      &maxSal,
		RecruiterName:  &recName,
		EmploymentType: &employmentType,
		CompanyDomain:  &companyDomain,
	}
	response := mustRequest(t, client, http.MethodPost, serverURL+"/api/jobs", encodeJSON(t, createPayload))
	t.Cleanup(func() { closeIntegrationResource(t, response.Body) })
	requireStatus(t, response, http.StatusCreated)
	var created models.Job
	mustDecode(t, response, &created)
	if created.EmploymentType != employmentType {
		t.Errorf("created employment type = %q, want %q", created.EmploymentType, employmentType)
	}
	if created.ID == "" || len(created.Stages) != 4 {
		t.Fatalf("created job has incomplete defaults: id=%q stages=%d", created.ID, len(created.Stages))
	}
	return created
}

func assertLifecycleJobDetails(t *testing.T, client *http.Client, serverURL, jobID string) {
	t.Helper()
	response := mustRequest(t, client, http.MethodGet, serverURL+"/api/jobs/"+jobID, "")
	t.Cleanup(func() { closeIntegrationResource(t, response.Body) })
	requireStatus(t, response, http.StatusOK)
}

func setLifecycleCurrentStage(t *testing.T, client *http.Client, serverURL, stageID string) {
	t.Helper()
	url := fmt.Sprintf("%s/api/stages/%s/set-current", serverURL, stageID)
	response := mustRequest(t, client, http.MethodPut, url, "")
	t.Cleanup(func() { closeIntegrationResource(t, response.Body) })
	requireStatus(t, response, http.StatusOK)
}

func scheduleLifecycleStage(t *testing.T, client *http.Client, serverURL, stageID string) {
	t.Helper()
	meetingURL := "https://meet.example.com/abc-def-ghi"
	payload := models.ScheduleMeetingInput{MeetingDate: "2001-01-02", MeetingTime: "14:00", MeetingURL: &meetingURL}
	url := fmt.Sprintf("%s/api/stages/%s/schedule", serverURL, stageID)
	response := mustRequest(t, client, http.MethodPut, url, encodeJSON(t, payload))
	t.Cleanup(func() { closeIntegrationResource(t, response.Body) })
	requireStatus(t, response, http.StatusOK)
}

func assertLifecycleMeeting(t *testing.T, client *http.Client, serverURL string) {
	t.Helper()
	response := mustRequest(t, client, http.MethodGet, serverURL+"/api/meetings", "")
	t.Cleanup(func() { closeIntegrationResource(t, response.Body) })
	var meetings []models.ScheduledMeeting
	mustDecode(t, response, &meetings)
	if len(meetings) == 0 {
		t.Fatal("expected a scheduled meeting")
	}
	if meetings[0].CompanyDomain != "example.test" {
		t.Errorf("meeting company domain = %q, want %q", meetings[0].CompanyDomain, "example.test")
	}
}

func createLifecycleQuestion(t *testing.T, client *http.Client, serverURL, stageID string) {
	t.Helper()
	payload := models.CreateQuestionInput{StageID: stageID, Question: "What is your experience with Kubernetes security posture management?"}
	response := mustRequest(t, client, http.MethodPost, serverURL+"/api/questions", encodeJSON(t, payload))
	t.Cleanup(func() { closeIntegrationResource(t, response.Body) })
	requireStatus(t, response, http.StatusCreated)
}

func deleteLifecycleJob(t *testing.T, client *http.Client, serverURL, jobID string) {
	t.Helper()
	url := fmt.Sprintf("%s/api/jobs/%s", serverURL, jobID)
	response := mustRequest(t, client, http.MethodDelete, url, "")
	closeIntegrationResource(t, response.Body)
	requireStatus(t, response, http.StatusOK)
	response = mustRequest(t, client, http.MethodGet, url, "")
	closeIntegrationResource(t, response.Body)
	requireStatus(t, response, http.StatusNotFound)
}

func encodeJSON(t *testing.T, value interface{}) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func mustRequest(t *testing.T, client *http.Client, method, target, body string) *http.Response {
	t.Helper()
	var requestBody io.Reader
	if body != "" {
		requestBody = strings.NewReader(body)
	}
	request, err := http.NewRequest(method, target, requestBody)
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("%s %s failed: %v", method, target, err)
	}
	return response
}

func requireStatus(t *testing.T, response *http.Response, want int) {
	t.Helper()
	if response.StatusCode != want {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("status=%d, want %d; response=%s", response.StatusCode, want, body)
	}
}

func mustDecode(t *testing.T, response *http.Response, target interface{}) {
	t.Helper()
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		t.Fatal(err)
	}
}

func TestAPI_JobSearchAndFiltering(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	client := ts.Client()

	jobsToCreate := []struct {
		company string
		title   string
		status  models.JobStatus
	}{
		{"Example Company", "Security Engineer", models.StatusOngoing},
		{"Example Labs", "Security Analyst", models.StatusOngoing},
		{"Example Company", "Platform Engineer", models.StatusAccepted},
		{"Legacy Corp", "Cobol Developer", models.StatusRejected},
	}

	for _, j := range jobsToCreate {
		st := j.status
		p, _ := json.Marshal(models.CreateJobInput{
			CompanyName:   j.company,
			PositionTitle: j.title,
			Status:        &st,
		})
		res, err := client.Post(ts.URL+"/api/jobs", "application/json", bytes.NewReader(p))
		if err != nil || res.StatusCode != http.StatusCreated {
			t.Fatalf("Failed to seed job %s: %v", j.company, err)
		}
		closeIntegrationResource(t, res.Body)
	}

	// Filter by ongoing
	resp, err := client.Get(ts.URL + "/api/jobs?status=ongoing")
	if err != nil {
		t.Fatalf("Filter ongoing failed: %v", err)
	}
	t.Cleanup(func() { closeIntegrationResource(t, resp.Body) })
	var ongoingJobs []models.Job
	if err := json.NewDecoder(resp.Body).Decode(&ongoingJobs); err != nil {
		t.Fatalf("decode ongoing jobs: %v", err)
	}
	if len(ongoingJobs) != 2 {
		t.Errorf("Expected 2 ongoing jobs, got %d", len(ongoingJobs))
	}

	// Search for 'Security'
	resp, err = client.Get(ts.URL + "/api/jobs?search=Security")
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	t.Cleanup(func() { closeIntegrationResource(t, resp.Body) })
	var searchJobs []models.Job
	if err := json.NewDecoder(resp.Body).Decode(&searchJobs); err != nil {
		t.Fatalf("decode search results: %v", err)
	}
	if len(searchJobs) != 2 {
		t.Errorf("Expected 2 search results for 'Security', got %d", len(searchJobs))
	}
}

func TestAPI_AutoRestoreFromBackup(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "warroom_restore_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Errorf("remove restore test directory: %v", err)
		}
	}()

	backupFile := filepath.Join(tmpDir, "backup.json")
	dbFile := filepath.Join(tmpDir, "jobs.db")

	sampleBackup := []models.Job{
		{
			ID:            "restored-uuid-1",
			CompanyName:   "Restored Corp",
			PositionTitle: "Principal AI Architect",
			Status:        models.StatusOngoing,
			SalaryType:    models.SalaryNoMax,
			OrderIndex:    0,
			CreatedAt:     1790069989,
			UpdatedAt:     1790069989,
		},
	}
	backupData, _ := json.Marshal(sampleBackup)
	if err := os.WriteFile(backupFile, backupData, 0600); err != nil {
		t.Fatalf("Failed to write mock backup.json: %v", err)
	}

	cfg := &config.Config{
		Port:       0,
		Host:       "127.0.0.1",
		DataDir:    tmpDir,
		DBPath:     dbFile,
		BackupPath: backupFile,
	}

	db, err := database.InitDB(cfg)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	t.Cleanup(func() { closeIntegrationResource(t, db) })

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM jobs WHERE id = 'restored-uuid-1'").Scan(&count); err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected 1 restored job, found %d", count)
	}
}
