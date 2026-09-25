// Package handlers exposes the HTTP API and serves configured static assets.
package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"

	"war-room/backend/pkg/config"
	"war-room/backend/pkg/models"
	"war-room/backend/pkg/service"
)

// Handler groups services and configuration used by HTTP route handlers.
type Handler struct {
	jobs  *service.JobService
	logos *service.LogoService
	cfg   *config.Config
}

// NewHandler constructs an HTTP handler backed by the supplied services.
func NewHandler(jobs *service.JobService, logos *service.LogoService, cfg *config.Config) *Handler {
	return &Handler{
		jobs:  jobs,
		logos: logos,
		cfg:   cfg,
	}
}

// JSON writes data as a JSON response with the supplied status code.
func JSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, must-revalidate")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data) // intentionally ignoring write errors
}

// ErrorJSON writes a JSON error response with the supplied status and message.
func ErrorJSON(w http.ResponseWriter, status int, message string) {
	JSON(w, status, map[string]string{"error": message})
}

// RegisterRoutes registers all API routes using Go 1.22+ ServeMux pattern routing.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/health", h.handleHealthCheck)

	mux.HandleFunc("GET /api/jobs/counts", h.handleJobCounts)
	mux.HandleFunc("GET /api/jobs", h.handleListJobs)
	mux.HandleFunc("POST /api/jobs", h.handleCreateJob)
	mux.HandleFunc("PUT /api/jobs/reorder", h.handleReorderJobs)
	mux.HandleFunc("GET /api/jobs/{id}", h.handleGetJob)
	mux.HandleFunc("PUT /api/jobs/{id}", h.handleUpdateJob)
	mux.HandleFunc("DELETE /api/jobs/{id}", h.handleDeleteJob)

	mux.HandleFunc("POST /api/stages", h.handleCreateStage)
	mux.HandleFunc("PUT /api/stages/reorder", h.handleReorderStages)
	mux.HandleFunc("PUT /api/stages/{id}/schedule", h.handleScheduleStage)
	mux.HandleFunc("PUT /api/stages/{id}/set-current", h.handleSetCurrentStage)
	mux.HandleFunc("PUT /api/stages/{id}", h.handleUpdateStage)
	mux.HandleFunc("DELETE /api/stages/{id}", h.handleDeleteStage)

	mux.HandleFunc("POST /api/questions", h.handleCreateQuestion)
	mux.HandleFunc("PUT /api/questions/reorder", h.handleReorderQuestions)
	mux.HandleFunc("PUT /api/questions/{id}", h.handleUpdateQuestion)
	mux.HandleFunc("DELETE /api/questions/{id}", h.handleDeleteQuestion)

	mux.HandleFunc("GET /api/meetings", h.handleGetMeetings)

	mux.HandleFunc("POST /api/attachments", h.handleUploadAttachment)
	mux.HandleFunc("GET /api/attachments/{id}/download", h.handleDownloadAttachment)
	mux.HandleFunc("DELETE /api/attachments/{id}", h.handleDeleteAttachment)

	mux.HandleFunc("GET /api/company-logo", h.handleGetCompanyLogo)

	// Static Assets fallback (for local single-binary or unified execution)
	if h.cfg.StaticDir != "" {
		fs := http.FileServer(http.Dir(h.cfg.StaticDir))
		mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				http.NotFound(w, r)
				return
			}
			fs.ServeHTTP(w, r)
		})
	}
}

// handleHealthCheck returns a 200 OK status to indicate the service is running.
func (h *Handler) handleHealthCheck(w http.ResponseWriter, r *http.Request) {
	JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) handleJobCounts(w http.ResponseWriter, r *http.Request) {
	counts, err := h.jobs.GetJobCounts()
	if err != nil {
		ErrorJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, counts)
}

func (h *Handler) handleListJobs(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	search := r.URL.Query().Get("search")
	jobs, err := h.jobs.GetAllJobs(status, search)
	if err != nil {
		ErrorJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	if jobs == nil {
		jobs = []models.Job{}
	}
	JSON(w, http.StatusOK, jobs)
}

func (h *Handler) handleCreateJob(w http.ResponseWriter, r *http.Request) {
	var input models.CreateJobInput
	r.Body = http.MaxBytesReader(w, r.Body, 1048576) // 1MB limit
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		ErrorJSON(w, http.StatusBadRequest, "Invalid JSON payload: "+err.Error())
		return
	}

	job, err := h.jobs.CreateJob(input)
	if err != nil {
		ErrorJSON(w, http.StatusBadRequest, err.Error())
		return
	}
	JSON(w, http.StatusCreated, job)
}

func (h *Handler) handleReorderJobs(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		JobIDs []string `json:"job_ids"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1048576) // 1MB limit
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || len(payload.JobIDs) == 0 {
		ErrorJSON(w, http.StatusBadRequest, "Invalid payload: job_ids array required")
		return
	}

	if err := h.jobs.ReorderJobs(payload.JobIDs); err != nil {
		ErrorJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]bool{"success": true})
}

func (h *Handler) handleGetJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	job, err := h.jobs.GetFullJobDetails(id)
	if err != nil {
		ErrorJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	if job == nil {
		ErrorJSON(w, http.StatusNotFound, "Job process not found")
		return
	}
	JSON(w, http.StatusOK, job)
}

func (h *Handler) handleUpdateJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var input models.UpdateJobInput
	r.Body = http.MaxBytesReader(w, r.Body, 1048576) // 1MB limit
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		ErrorJSON(w, http.StatusBadRequest, "Invalid JSON payload: "+err.Error())
		return
	}

	job, err := h.jobs.UpdateJob(id, input)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			ErrorJSON(w, http.StatusNotFound, "Job process not found")
			return
		}
		ErrorJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, job)
}

func (h *Handler) handleDeleteJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.jobs.DeleteJob(id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			ErrorJSON(w, http.StatusNotFound, "Job process not found")
			return
		}
		ErrorJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]interface{}{"success": true, "id": id})
}

func (h *Handler) handleCreateStage(w http.ResponseWriter, r *http.Request) {
	var input models.CreateStageInput
	r.Body = http.MaxBytesReader(w, r.Body, 1048576) // 1MB limit
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		ErrorJSON(w, http.StatusBadRequest, "Invalid payload: "+err.Error())
		return
	}

	stage, err := h.jobs.CreateStage(input)
	if err != nil {
		ErrorJSON(w, http.StatusBadRequest, err.Error())
		return
	}
	JSON(w, http.StatusCreated, map[string]interface{}{"success": true, "id": stage.ID})
}

func (h *Handler) handleReorderStages(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		JobID    string   `json:"job_id"`
		StageIDs []string `json:"stage_ids"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1048576) // 1MB limit
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload.JobID == "" || len(payload.StageIDs) == 0 {
		ErrorJSON(w, http.StatusBadRequest, "Invalid payload: job_id and stage_ids array required")
		return
	}

	if err := h.jobs.ReorderStages(payload.JobID, payload.StageIDs); err != nil {
		ErrorJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]bool{"success": true})
}

func (h *Handler) handleScheduleStage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var input models.ScheduleMeetingInput
	r.Body = http.MaxBytesReader(w, r.Body, 1048576) // 1MB limit
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		ErrorJSON(w, http.StatusBadRequest, "Invalid payload: "+err.Error())
		return
	}

	if err := h.jobs.ScheduleMeeting(id, input); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			ErrorJSON(w, http.StatusNotFound, "Stage not found")
			return
		}
		ErrorJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]interface{}{"success": true, "stage_id": id})
}

func (h *Handler) handleSetCurrentStage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	stages, err := h.jobs.SetCurrentStage(id)
	if err != nil {
		ErrorJSON(w, http.StatusNotFound, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"stage_id": id,
		"stages":   stages,
	})
}

func (h *Handler) handleUpdateStage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var input models.UpdateStageInput
	r.Body = http.MaxBytesReader(w, r.Body, 1048576) // 1MB limit
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		ErrorJSON(w, http.StatusBadRequest, "Invalid payload: "+err.Error())
		return
	}

	_, err := h.jobs.UpdateStage(id, input)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			ErrorJSON(w, http.StatusNotFound, "Stage not found")
			return
		}
		ErrorJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]interface{}{"success": true, "id": id})
}

func (h *Handler) handleDeleteStage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.jobs.DeleteStage(id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			ErrorJSON(w, http.StatusNotFound, "Stage not found")
			return
		}
		ErrorJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]interface{}{"success": true, "id": id})
}

func (h *Handler) handleCreateQuestion(w http.ResponseWriter, r *http.Request) {
	var input models.CreateQuestionInput
	r.Body = http.MaxBytesReader(w, r.Body, 1048576) // 1MB limit
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		ErrorJSON(w, http.StatusBadRequest, "Invalid payload: "+err.Error())
		return
	}

	q, err := h.jobs.CreateQuestion(input)
	if err != nil {
		ErrorJSON(w, http.StatusBadRequest, err.Error())
		return
	}
	JSON(w, http.StatusCreated, map[string]interface{}{"success": true, "id": q.ID})
}

func (h *Handler) handleReorderQuestions(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		StageID     string   `json:"stage_id"`
		QuestionIDs []string `json:"question_ids"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1048576) // 1MB limit
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload.StageID == "" || len(payload.QuestionIDs) == 0 {
		ErrorJSON(w, http.StatusBadRequest, "Invalid payload: stage_id and question_ids array required")
		return
	}

	if err := h.jobs.ReorderQuestions(payload.StageID, payload.QuestionIDs); err != nil {
		ErrorJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]interface{}{"success": true, "stage_id": payload.StageID})
}

func (h *Handler) handleUpdateQuestion(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var input models.UpdateQuestionInput
	r.Body = http.MaxBytesReader(w, r.Body, 1048576) // 1MB limit
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		ErrorJSON(w, http.StatusBadRequest, "Invalid payload: "+err.Error())
		return
	}

	_, err := h.jobs.UpdateQuestion(id, input)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			ErrorJSON(w, http.StatusNotFound, "Question not found")
			return
		}
		ErrorJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]interface{}{"success": true, "id": id})
}

func (h *Handler) handleDeleteQuestion(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.jobs.DeleteQuestion(id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			ErrorJSON(w, http.StatusNotFound, "Question not found")
			return
		}
		ErrorJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]interface{}{"success": true, "id": id})
}

func (h *Handler) handleGetMeetings(w http.ResponseWriter, r *http.Request) {
	meetings, err := h.jobs.GetScheduledMeetings()
	if err != nil {
		ErrorJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	if meetings == nil {
		meetings = []models.ScheduledMeeting{}
	}
	JSON(w, http.StatusOK, meetings)
}

func (h *Handler) handleUploadAttachment(w http.ResponseWriter, r *http.Request) {
	jobID, stageID, file, header, cleanup, ok := parseAttachmentUpload(w, r)
	if !ok {
		return
	}
	defer cleanup()
	defer func() { _ = file.Close() }()
	att, err := h.storeUploadedAttachment(jobID, stageID, file, header)
	if err != nil {
		ErrorJSON(w, http.StatusInternalServerError, "Failed to save attachment")
		return
	}
	JSON(w, http.StatusCreated, att)
}

func parseAttachmentUpload(w http.ResponseWriter, r *http.Request) (string, *string, multipart.File, *multipart.FileHeader, func(), bool) {
	const maxUploadSize = 50 << 20
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)
	// ParseMultipartForm spills beyond 10 MiB to disk, while MaxBytesReader caps the entire request at 50 MiB.
	if err := r.ParseMultipartForm(10 << 20); err != nil { // #nosec G120
		status := http.StatusBadRequest
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			status = http.StatusRequestEntityTooLarge
		}
		ErrorJSON(w, status, "Failed to parse multipart form or file too large")
		return "", nil, nil, nil, nil, false
	}
	cleanup := func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}
	jobID := r.FormValue("job_id")
	if jobID == "" {
		ErrorJSON(w, http.StatusBadRequest, "job_id is required")
		cleanup()
		return "", nil, nil, nil, nil, false
	}
	var stageID *string
	if value := r.FormValue("stage_id"); value != "" && value != "null" {
		stageID = &value
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		ErrorJSON(w, http.StatusBadRequest, "file is required")
		cleanup()
		return "", nil, nil, nil, nil, false
	}
	return jobID, stageID, file, header, cleanup, true
}

func (h *Handler) storeUploadedAttachment(jobID string, stageID *string, file multipart.File, header *multipart.FileHeader) (*models.Attachment, error) {
	prefix := make([]byte, 512)
	n, err := io.ReadFull(file, prefix)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, err
	}
	mimeType := http.DetectContentType(prefix[:n])
	content := io.MultiReader(bytes.NewReader(prefix[:n]), file)
	cleanFilename := filepath.Base(header.Filename)
	return h.jobs.StoreAttachment(jobID, stageID, cleanFilename, content, header.Size, mimeType)
}

func (h *Handler) handleDownloadAttachment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	att, err := h.jobs.GetAttachmentByID(id)
	if err != nil || att == nil {
		ErrorJSON(w, http.StatusNotFound, "Attachment not found")
		return
	}
	filename := filepath.Base(att.StoredFilename)
	if filename != att.StoredFilename || filename == "." || filename == string(filepath.Separator) {
		ErrorJSON(w, http.StatusNotFound, "Attachment not found")
		return
	}

	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": att.OriginalName})
	w.Header().Set("Content-Disposition", disposition)
	w.Header().Set("Content-Type", att.MimeType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// filename is checked as a basename above, and attachments are stored under this configured directory.
	http.ServeFile(w, r, filepath.Join(h.cfg.AttachmentsDir, filename)) // #nosec G304
}

func (h *Handler) handleDeleteAttachment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.jobs.DeleteAttachment(id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			ErrorJSON(w, http.StatusNotFound, "Attachment not found")
			return
		}
		ErrorJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]interface{}{"success": true, "id": id})
}

func (h *Handler) handleGetCompanyLogo(w http.ResponseWriter, r *http.Request) {
	company := r.URL.Query().Get("company")
	domain := r.URL.Query().Get("domain")

	if strings.ToLower(strings.TrimSpace(company)) == "unknown" || company == "" {
		http.Error(w, "Unknown company logo not tracked", http.StatusNotFound)
		return
	}

	data, mime, err := h.logos.GetLogo(company, domain)
	if err != nil || len(data) == 0 {
		http.Error(w, "Logo not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "public, max-age=604800, immutable")
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data) // #nosec G705 -- logo data is served with nosniff and a sandbox CSP.
}
