package backend_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
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

func TestAPI_ClearMeetingScheduleClearsJoinURLAndKeepsNotes(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()
	client := ts.Client()
	job := createLifecycleJob(t, client, ts.URL)
	stageID := job.Stages[0].ID
	scheduleLifecycleStage(t, client, ts.URL, stageID)

	response := mustRequest(t, client, http.MethodPut, ts.URL+"/api/stages/"+stageID+"/schedule", `{"meeting_date":null,"meeting_time":null,"meeting_url":null,"meeting_type":"video"}`)
	closeIntegrationResource(t, response.Body)
	requireStatus(t, response, http.StatusOK)
	assertClearedMeetingDetails(t, client, ts.URL, job.ID, stageID)
	assertNoScheduledMeetings(t, client, ts.URL)
}

func TestAPI_RejectsInvalidMeetingDates(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()
	client := ts.Client()
	job := createLifecycleJob(t, client, ts.URL)
	stageID := job.Stages[0].ID
	scheduleLifecycleStage(t, client, ts.URL, stageID)

	for _, testCase := range []struct {
		name, path, body string
	}{
		{"schedule endpoint", "/api/stages/" + stageID + "/schedule", `{"meeting_date":"2031-02-30","meeting_time":"14:00"}`},
		{"stage update endpoint", "/api/stages/" + stageID, `{"meeting_date":"not-a-date"}`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			response := mustRequest(t, client, http.MethodPut, ts.URL+testCase.path, testCase.body)
			closeIntegrationResource(t, response.Body)
			requireStatus(t, response, http.StatusBadRequest)
		})
	}
	assertLifecycleMeeting(t, client, ts.URL)
}

func TestAPI_RejectsInvalidEnumValues(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()
	client := ts.Client()
	job := createLifecycleJob(t, client, ts.URL)
	stageID := job.Stages[0].ID
	cases := []struct{ name, method, path, body string }{
		{"create job status", http.MethodPost, "/api/jobs", `{"company_name":"Example","position_title":"Engineer","status":"invalid"}`},
		{"create job recruiter", http.MethodPost, "/api/jobs", `{"company_name":"Example","position_title":"Engineer","recruiter_type":"invalid"}`},
		{"create job salary", http.MethodPost, "/api/jobs", `{"company_name":"Example","position_title":"Engineer","salary_type":"invalid"}`},
		{"create job work arrangement", http.MethodPost, "/api/jobs", `{"company_name":"Example","position_title":"Engineer","work_arrangement":"invalid"}`},
		{"create job employment", http.MethodPost, "/api/jobs", `{"company_name":"Example","position_title":"Engineer","employment_type":"invalid"}`},
		{"create stage type", http.MethodPost, "/api/stages", `{"job_id":"` + job.ID + `","stage_type":"invalid"}`},
		{"create stage recruiter", http.MethodPost, "/api/stages", `{"job_id":"` + job.ID + `","stage_type":"HR","recruiter_type":"invalid"}`},
		{"update job status", http.MethodPut, "/api/jobs/" + job.ID, `{"status":"invalid"}`},
		{"update job recruiter", http.MethodPut, "/api/jobs/" + job.ID, `{"recruiter_type":"invalid"}`},
		{"update job salary", http.MethodPut, "/api/jobs/" + job.ID, `{"salary_type":"invalid"}`},
		{"update job work arrangement", http.MethodPut, "/api/jobs/" + job.ID, `{"work_arrangement":"invalid"}`},
		{"update job employment", http.MethodPut, "/api/jobs/" + job.ID, `{"employment_type":"invalid"}`},
		{"update stage status", http.MethodPut, "/api/stages/" + stageID, `{"status":"invalid"}`},
		{"update stage recruiter", http.MethodPut, "/api/stages/" + stageID, `{"recruiter_type":"invalid"}`},
		{"update stage meeting", http.MethodPut, "/api/stages/" + stageID, `{"meeting_type":"invalid"}`},
		{"schedule meeting type", http.MethodPut, "/api/stages/" + stageID + "/schedule", `{"meeting_date":"2031-01-01","meeting_time":"09:00","meeting_type":"invalid"}`},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			response := mustRequest(t, client, testCase.method, ts.URL+testCase.path, testCase.body)
			closeIntegrationResource(t, response.Body)
			requireStatus(t, response, http.StatusBadRequest)
		})
	}
}

func assertClearedMeetingDetails(t *testing.T, client *http.Client, serverURL, jobID, stageID string) {
	t.Helper()
	details := mustRequest(t, client, http.MethodGet, serverURL+"/api/jobs/"+jobID, "")
	t.Cleanup(func() { closeIntegrationResource(t, details.Body) })
	var updated models.Job
	mustDecode(t, details, &updated)
	scheduledStage := findIntegrationStage(updated.Stages, stageID)
	if scheduledStage == nil {
		t.Fatal("scheduled stage is missing from updated job")
	}
	if hasScheduledMeetingDetails(scheduledStage) {
		t.Fatalf("cleared schedule retained meeting details: %+v", scheduledStage)
	}
	if scheduledStage.Notes != "Discuss platform security goals and interview expectations." {
		t.Fatalf("clearing schedule changed notes: %+v", scheduledStage.Notes)
	}
}

func findIntegrationStage(stages []models.Stage, stageID string) *models.Stage {
	for index := range stages {
		if stages[index].ID == stageID {
			return &stages[index]
		}
	}
	return nil
}

func hasScheduledMeetingDetails(stage *models.Stage) bool {
	return stage.MeetingDate != nil && *stage.MeetingDate != "" ||
		stage.MeetingTime != nil && *stage.MeetingTime != "" ||
		stage.MeetingURL != nil && *stage.MeetingURL != ""
}

func assertNoScheduledMeetings(t *testing.T, client *http.Client, serverURL string) {
	t.Helper()
	meetingsResponse := mustRequest(t, client, http.MethodGet, serverURL+"/api/meetings", "")
	t.Cleanup(func() { closeIntegrationResource(t, meetingsResponse.Body) })
	var meetings []models.ScheduledMeeting
	mustDecode(t, meetingsResponse, &meetings)
	if len(meetings) != 0 {
		t.Fatalf("cleared schedule still appears in meeting list: %d meetings", len(meetings))
	}
}

func TestAPI_UpdatesRejectBlankRequiredTextWithoutChangingRecords(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()
	client := ts.Client()
	job := createLifecycleJob(t, client, ts.URL)
	stageID := job.Stages[0].ID
	questionResponse := mustRequest(t, client, http.MethodPost, ts.URL+"/api/questions", encodeJSON(t, models.CreateQuestionInput{
		StageID: stageID, Question: "Existing question",
	}))
	t.Cleanup(func() { closeIntegrationResource(t, questionResponse.Body) })
	requireStatus(t, questionResponse, http.StatusCreated)

	for _, testCase := range []struct {
		name, path, body string
	}{
		{"job title", "/api/jobs/" + job.ID, `{"position_title":" \t "}`},
		{"question text", "/api/questions/" + readQuestionID(t, questionResponse), `{"question":" \t "}`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			response := mustRequest(t, client, http.MethodPut, ts.URL+testCase.path, testCase.body)
			closeIntegrationResource(t, response.Body)
			requireStatus(t, response, http.StatusBadRequest)
		})
	}

	details := mustRequest(t, client, http.MethodGet, ts.URL+"/api/jobs/"+job.ID, "")
	t.Cleanup(func() { closeIntegrationResource(t, details.Body) })
	var unchanged models.Job
	mustDecode(t, details, &unchanged)
	if unchanged.PositionTitle != job.PositionTitle {
		t.Fatalf("blank update changed position title to %q", unchanged.PositionTitle)
	}
	if got := unchanged.Stages[0].Questions; len(got) != 1 || got[0].Question != "Existing question" {
		t.Fatalf("blank update changed questions: %+v", got)
	}
}

func readQuestionID(t *testing.T, response *http.Response) string {
	t.Helper()
	var question models.Question
	mustDecode(t, response, &question)
	if question.ID == "" {
		t.Fatal("create question response has no ID")
	}
	return question.ID
}

func TestAPI_JSONMutationEndpointsRejectTrailingValues(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()
	client := ts.Client()
	job := createLifecycleJob(t, client, ts.URL)
	stageID := job.Stages[0].ID

	questionResponse := mustRequest(t, client, http.MethodPost, ts.URL+"/api/questions", encodeJSON(t, models.CreateQuestionInput{
		StageID:  stageID,
		Question: "Existing question",
	}))
	t.Cleanup(func() { closeIntegrationResource(t, questionResponse.Body) })
	requireStatus(t, questionResponse, http.StatusCreated)
	var question struct {
		ID string `json:"id"`
	}
	mustDecode(t, questionResponse, &question)
	if question.ID == "" {
		t.Fatal("create question response has no ID")
	}

	requests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"create job", http.MethodPost, "/api/jobs", `{"company_name":"Trailing","position_title":"Payload"}`},
		{"update job", http.MethodPut, "/api/jobs/" + job.ID, `{"position_title":"Changed"}`},
		{"reorder jobs", http.MethodPut, "/api/jobs/reorder", `{"job_ids":["` + job.ID + `"]}`},
		{"create stage", http.MethodPost, "/api/stages", `{"job_id":"` + job.ID + `","stage_type":"Technical"}`},
		{"reorder stages", http.MethodPut, "/api/stages/reorder", `{"job_id":"` + job.ID + `","stage_ids":["` + stageID + `"]}`},
		{"schedule stage", http.MethodPut, "/api/stages/" + stageID + "/schedule", `{}`},
		{"update stage", http.MethodPut, "/api/stages/" + stageID, `{"meeting_type":"video"}`},
		{"create question", http.MethodPost, "/api/questions", `{"stage_id":"` + stageID + `","question":"Trailing"}`},
		{"reorder questions", http.MethodPut, "/api/questions/reorder", `{"stage_id":"` + stageID + `","question_ids":["` + question.ID + `"]}`},
		{"update question", http.MethodPut, "/api/questions/" + question.ID, `{"question":"Changed"}`},
	}

	for _, testCase := range requests {
		for _, trailing := range []struct {
			name string
			data string
		}{{"second value", ` {}`}, {"invalid data", ` trailing`}} {
			t.Run(testCase.name+"/"+trailing.name, func(t *testing.T) {
				response := mustRequest(t, client, testCase.method, ts.URL+testCase.path, testCase.body+trailing.data)
				closeIntegrationResource(t, response.Body)
				requireStatus(t, response, http.StatusBadRequest)
			})
		}
	}

	unchanged := mustRequest(t, client, http.MethodGet, ts.URL+"/api/jobs/"+job.ID, "")
	t.Cleanup(func() { closeIntegrationResource(t, unchanged.Body) })
	var unchangedJob models.Job
	mustDecode(t, unchanged, &unchangedJob)
	if unchangedJob.PositionTitle != job.PositionTitle {
		t.Fatalf("trailing JSON changed the job title to %q", unchangedJob.PositionTitle)
	}
	jobs := mustRequest(t, client, http.MethodGet, ts.URL+"/api/jobs", "")
	t.Cleanup(func() { closeIntegrationResource(t, jobs.Body) })
	var allJobs []models.Job
	mustDecode(t, jobs, &allJobs)
	if len(allJobs) != 1 {
		t.Fatalf("trailing JSON created a job; count=%d", len(allJobs))
	}

	whitespace := mustRequest(t, client, http.MethodPost, ts.URL+"/api/jobs", "{\"company_name\":\"Whitespace\",\"position_title\":\"Accepted\"} \n\t")
	t.Cleanup(func() { closeIntegrationResource(t, whitespace.Body) })
	requireStatus(t, whitespace, http.StatusCreated)
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
	notes := "Discuss platform security goals and interview expectations."
	payload := models.ScheduleMeetingInput{MeetingDate: "2001-01-02", MeetingTime: "14:00", MeetingURL: &meetingURL, Notes: &notes}
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
	if meetings[0].StageNotes != "Discuss platform security goals and interview expectations." {
		t.Errorf("meeting notes = %q, want saved schedule notes", meetings[0].StageNotes)
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

func TestAPI_AttachmentLifecycleEnforcesOwnershipAndSafeNames(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()
	client := ts.Client()
	job := createLifecycleJob(t, client, ts.URL)
	foreignJob := createLifecycleJob(t, client, ts.URL)
	foreignStage := foreignJob.Stages[0].ID
	assertAttachmentUploadRejected(t, client, ts.URL, "missing-job", "", "resume")
	assertAttachmentUploadRejected(t, client, ts.URL, job.ID, foreignStage, "resume")

	body, contentType := attachmentUploadBody(t, job.ID, "", "../resume.txt", []byte("resume contents"))
	request, err := http.NewRequest(http.MethodPost, ts.URL+"/api/attachments", body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", contentType)
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("upload attachment: %v", err)
	}
	t.Cleanup(func() { closeIntegrationResource(t, response.Body) })
	requireStatus(t, response, http.StatusCreated)
	var attachment models.Attachment
	mustDecode(t, response, &attachment)
	if attachment.ID == "" || attachment.OriginalName != "resume.txt" || attachment.JobID != job.ID {
		t.Fatalf("uploaded attachment has unexpected metadata: %#v", attachment)
	}

	download := mustRequest(t, client, http.MethodGet, ts.URL+"/api/attachments/"+attachment.ID+"/download", "")
	t.Cleanup(func() { closeIntegrationResource(t, download.Body) })
	requireStatus(t, download, http.StatusOK)
	contents, err := io.ReadAll(download.Body)
	if err != nil || string(contents) != "resume contents" || download.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("download contents=%q nosniff=%q err=%v", contents, download.Header.Get("X-Content-Type-Options"), err)
	}

	deleted := mustRequest(t, client, http.MethodDelete, ts.URL+"/api/attachments/"+attachment.ID, "")
	closeIntegrationResource(t, deleted.Body)
	requireStatus(t, deleted, http.StatusOK)
	missing := mustRequest(t, client, http.MethodGet, ts.URL+"/api/attachments/"+attachment.ID+"/download", "")
	closeIntegrationResource(t, missing.Body)
	requireStatus(t, missing, http.StatusNotFound)
}

func assertAttachmentUploadRejected(t *testing.T, client *http.Client, serverURL, jobID, stageID, contents string) {
	t.Helper()
	body, contentType := attachmentUploadBody(t, jobID, stageID, "resume.txt", []byte(contents))
	request, err := http.NewRequest(http.MethodPost, serverURL+"/api/attachments", body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", contentType)
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("post invalid attachment: %v", err)
	}
	closeIntegrationResource(t, response.Body)
	requireStatus(t, response, http.StatusBadRequest)
}

func attachmentUploadBody(t *testing.T, jobID, stageID, filename string, contents []byte) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if jobID != "" {
		if err := writer.WriteField("job_id", jobID); err != nil {
			t.Fatal(err)
		}
	}
	if stageID != "" {
		if err := writer.WriteField("stage_id", stageID); err != nil {
			t.Fatal(err)
		}
	}
	file, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(contents); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return &body, writer.FormDataContentType()
}

func TestAPI_BackupArchiveRoundTripAndMalformedImportRollback(t *testing.T) {
	source, _, closeSource := setupTestServer(t)
	defer closeSource()
	sourceClient := source.Client()
	job := createLifecycleJob(t, sourceClient, source.URL)
	archiveResponse := mustRequest(t, sourceClient, http.MethodGet, source.URL+"/api/backup/export", "")
	t.Cleanup(func() { closeIntegrationResource(t, archiveResponse.Body) })
	requireStatus(t, archiveResponse, http.StatusOK)
	if archiveResponse.Header.Get("Content-Type") != "application/zip" {
		t.Fatalf("backup content type=%q", archiveResponse.Header.Get("Content-Type"))
	}
	archive, err := io.ReadAll(archiveResponse.Body)
	if err != nil || len(archive) < 4 || string(archive[:2]) != "PK" {
		t.Fatalf("exported archive length=%d signature=%q err=%v", len(archive), archive[:min(len(archive), 4)], err)
	}

	target, _, closeTarget := setupTestServer(t)
	defer closeTarget()
	client := target.Client()
	importBackup(t, client, target.URL, archive)
	assertImportedJob(t, client, target.URL, job.ID)
	response := postBackup(t, client, target.URL, []byte("not a zip archive"))
	closeIntegrationResource(t, response.Body)
	requireStatus(t, response, http.StatusBadRequest)
	assertImportedJob(t, client, target.URL, job.ID)
}

func importBackup(t *testing.T, client *http.Client, serverURL string, archive []byte) {
	t.Helper()
	response := postBackup(t, client, serverURL, archive)
	t.Cleanup(func() { closeIntegrationResource(t, response.Body) })
	requireStatus(t, response, http.StatusOK)
}

func postBackup(t *testing.T, client *http.Client, serverURL string, archive []byte) *http.Response {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	file, err := writer.CreateFormFile("backup", "backup.zip")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(archive); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, serverURL+"/api/backup/import", &body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("post backup archive: %v", err)
	}
	return response
}

func assertImportedJob(t *testing.T, client *http.Client, serverURL, jobID string) {
	t.Helper()
	response := mustRequest(t, client, http.MethodGet, serverURL+"/api/jobs/"+jobID, "")
	t.Cleanup(func() { closeIntegrationResource(t, response.Body) })
	requireStatus(t, response, http.StatusOK)
	var job models.Job
	mustDecode(t, response, &job)
	if job.ID != jobID {
		t.Fatalf("restored job ID=%q, want %q", job.ID, jobID)
	}
}
