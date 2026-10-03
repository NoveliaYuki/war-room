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
	"os"
	"path/filepath"
	"strconv"
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

var errBackupExportTooLarge = errors.New("backup ZIP exceeds the 500 MiB limit")

type boundedWriter struct {
	writer  io.Writer
	limit   int64
	written int64
}

func (writer *boundedWriter) Write(data []byte) (int, error) {
	remaining := writer.limit - writer.written
	if int64(len(data)) > remaining {
		if remaining <= 0 {
			return 0, errBackupExportTooLarge
		}
		length := int(remaining)
		written, err := writer.writer.Write(data[:length])
		writer.written += int64(written)
		if err != nil {
			return written, err
		}
		if written < length {
			return written, io.ErrShortWrite
		}
		return written, errBackupExportTooLarge
	}
	written, err := writer.writer.Write(data)
	writer.written += int64(written)
	if err == nil && written < len(data) {
		err = io.ErrShortWrite
	}
	return written, err
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

// decodeJSONBody accepts exactly one JSON value followed only by whitespace.
func decodeJSONBody(body io.Reader, target interface{}) error {
	decoder := json.NewDecoder(body)
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing interface{}
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("request body must contain one JSON value")
		}
		return err
	}
	return nil
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
	mux.HandleFunc("GET /api/backup/export", h.handleExportBackup)
	mux.HandleFunc("POST /api/backup/import", h.handleImportBackup)

	mux.HandleFunc("POST /api/attachments", h.handleUploadAttachment)
	mux.HandleFunc("GET /api/attachments/{id}/download", h.handleDownloadAttachment)
	mux.HandleFunc("DELETE /api/attachments/{id}", h.handleDeleteAttachment)

	mux.HandleFunc("GET /api/cv/versions", h.handleListCVVersions)
	mux.HandleFunc("POST /api/cv/versions", h.handleUploadCVVersion)
	mux.HandleFunc("GET /api/cv/versions/{id}/download", h.handleDownloadCVVersion)
	mux.HandleFunc("DELETE /api/cv/versions/{id}", h.handleDeleteCVVersion)
	mux.HandleFunc("PUT /api/jobs/{id}/cv-version", h.handleAssignCVVersion)

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

func (h *Handler) handleListCVVersions(w http.ResponseWriter, _ *http.Request) {
	versions, err := h.jobs.ListCVVersions()
	if err != nil {
		ErrorJSON(w, http.StatusInternalServerError, "Failed to load CV versions")
		return
	}
	JSON(w, http.StatusOK, map[string]any{"versions": versions})
}

func (h *Handler) handleUploadCVVersion(w http.ResponseWriter, r *http.Request) {
	const maxUploadSize = 50 << 20
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		status := http.StatusBadRequest
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			status = http.StatusRequestEntityTooLarge
		}
		ErrorJSON(w, status, "Choose a CV file no larger than 50 MiB")
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()
	file, header, err := r.FormFile("file")
	if err != nil {
		ErrorJSON(w, http.StatusBadRequest, "Choose a CV file to upload")
		return
	}
	defer func() { _ = file.Close() }()
	if header.Size == 0 {
		ErrorJSON(w, http.StatusBadRequest, "CV file cannot be empty")
		return
	}
	filename := filepath.Base(header.Filename)
	buffer := make([]byte, 512)
	read, _ := file.Read(buffer)
	mimeType := http.DetectContentType(buffer[:read])
	version, err := h.jobs.StoreCVVersion(filename, io.MultiReader(bytes.NewReader(buffer[:read]), file), header.Size, mimeType)
	if err != nil {
		ErrorJSON(w, http.StatusInternalServerError, "Failed to save CV version")
		return
	}
	JSON(w, http.StatusCreated, version)
}

func (h *Handler) handleDownloadCVVersion(w http.ResponseWriter, r *http.Request) {
	version, err := h.jobs.GetCVVersion(r.PathValue("id"))
	if err != nil || version == nil {
		ErrorJSON(w, http.StatusNotFound, "CV version not found")
		return
	}
	filename := filepath.Base(version.StoredFilename)
	if filename != version.StoredFilename || filename == "." {
		ErrorJSON(w, http.StatusNotFound, "CV version not found")
		return
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": version.OriginalName}))
	w.Header().Set("Content-Type", version.MimeType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeFile(w, r, filepath.Join(h.cfg.CVDir, filename)) // #nosec G304
}

func (h *Handler) handleDeleteCVVersion(w http.ResponseWriter, r *http.Request) {
	if err := h.jobs.DeleteCVVersion(r.PathValue("id")); err != nil {
		if errors.Is(err, service.ErrCVVersionInUse) {
			ErrorJSON(w, http.StatusConflict, "CV version is assigned to a job")
			return
		}
		if errors.Is(err, sql.ErrNoRows) {
			ErrorJSON(w, http.StatusNotFound, "CV version not found")
			return
		}
		ErrorJSON(w, http.StatusInternalServerError, "Failed to delete CV version")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleAssignCVVersion(w http.ResponseWriter, r *http.Request) {
	var request struct {
		VersionID *string `json:"version_id"`
	}
	if err := decodeJSONBody(r.Body, &request); err != nil {
		ErrorJSON(w, http.StatusBadRequest, "Invalid CV version assignment")
		return
	}
	if err := h.jobs.AssignCVVersion(r.PathValue("id"), request.VersionID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			ErrorJSON(w, http.StatusNotFound, "Job or CV version not found")
			return
		}
		ErrorJSON(w, http.StatusInternalServerError, "Failed to assign CV version")
		return
	}
	job, err := h.jobs.GetFullJobDetails(r.PathValue("id"))
	if err != nil || job == nil {
		ErrorJSON(w, http.StatusInternalServerError, "Failed to load job")
		return
	}
	JSON(w, http.StatusOK, job)
}

func (h *Handler) handleExportBackup(w http.ResponseWriter, _ *http.Request) {
	archive, err := os.CreateTemp(h.cfg.DataDir, ".war-room-export-*.zip")
	if err != nil {
		ErrorJSON(w, http.StatusInternalServerError, "Failed to prepare backup archive")
		return
	}
	defer func() { _ = os.Remove(archive.Name()) }()
	defer func() { _ = archive.Close() }()
	if err := h.jobs.ExportArchive(&boundedWriter{writer: archive, limit: 500 << 20}); err != nil {
		if errors.Is(err, errBackupExportTooLarge) {
			ErrorJSON(w, http.StatusRequestEntityTooLarge, errBackupExportTooLarge.Error())
			return
		}
		ErrorJSON(w, http.StatusInternalServerError, "Failed to create backup archive")
		return
	}
	info, err := archive.Stat()
	if err != nil || archive.Sync() != nil {
		ErrorJSON(w, http.StatusInternalServerError, "Failed to finish backup archive")
		return
	}
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		ErrorJSON(w, http.StatusInternalServerError, "Failed to read backup archive")
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="war-room-backup.zip"`)
	w.Header().Set("Cache-Control", "no-store, must-revalidate")
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	_, _ = io.Copy(w, archive)
}

func (h *Handler) handleImportBackup(w http.ResponseWriter, r *http.Request) {
	const maxArchiveBytes = (500 << 20) + (1 << 20)
	r.Body = http.MaxBytesReader(w, r.Body, maxArchiveBytes)
	// #nosec G120 -- MaxBytesReader bounds the entire multipart request above.
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		ErrorJSON(w, http.StatusBadRequest, "Upload a valid ZIP backup no larger than 500 MiB")
		return
	}
	if r.MultipartForm != nil {
		defer func() { _ = r.MultipartForm.RemoveAll() }()
	}
	file, header, err := r.FormFile("backup")
	if err != nil {
		ErrorJSON(w, http.StatusBadRequest, "Choose a War Room ZIP backup to import")
		return
	}
	defer func() { _ = file.Close() }()
	if header.Size > 500<<20 {
		ErrorJSON(w, http.StatusRequestEntityTooLarge, "Backup ZIP exceeds the 500 MiB limit")
		return
	}
	allowEmpty := r.FormValue("allow_empty") == "true"
	if err := h.jobs.ImportArchive(file, header.Size, allowEmpty); err != nil {
		if errors.Is(err, service.ErrEmptyBackupRequiresConfirmation) {
			JSON(w, http.StatusConflict, map[string]interface{}{
				"error":                 "This backup contains no saved processes.",
				"requires_confirmation": true,
			})
			return
		}
		ErrorJSON(w, http.StatusBadRequest, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]bool{"success": true})
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
	if err := decodeJSONBody(r.Body, &input); err != nil {
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
	if err := decodeJSONBody(r.Body, &payload); err != nil || len(payload.JobIDs) == 0 {
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
	if err := decodeJSONBody(r.Body, &input); err != nil {
		ErrorJSON(w, http.StatusBadRequest, "Invalid JSON payload: "+err.Error())
		return
	}

	job, err := h.jobs.UpdateJob(id, input)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			ErrorJSON(w, http.StatusNotFound, "Job process not found")
			return
		}
		if errors.Is(err, service.ErrPositionTitleRequired) {
			ErrorJSON(w, http.StatusBadRequest, err.Error())
			return
		}
		if errors.Is(err, service.ErrInvalidField) {
			ErrorJSON(w, http.StatusBadRequest, err.Error())
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
	if err := decodeJSONBody(r.Body, &input); err != nil {
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
	if err := decodeJSONBody(r.Body, &payload); err != nil || payload.JobID == "" || len(payload.StageIDs) == 0 {
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
	if err := decodeJSONBody(r.Body, &input); err != nil {
		ErrorJSON(w, http.StatusBadRequest, "Invalid payload: "+err.Error())
		return
	}

	if err := h.jobs.ScheduleMeeting(id, input); err != nil {
		if errors.Is(err, service.ErrInvalidMeetingDate) || errors.Is(err, service.ErrInvalidField) {
			ErrorJSON(w, http.StatusBadRequest, err.Error())
			return
		}
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
	if err := decodeJSONBody(r.Body, &input); err != nil {
		ErrorJSON(w, http.StatusBadRequest, "Invalid payload: "+err.Error())
		return
	}

	_, err := h.jobs.UpdateStage(id, input)
	if err != nil {
		if errors.Is(err, service.ErrInvalidMeetingDate) || errors.Is(err, service.ErrInvalidField) {
			ErrorJSON(w, http.StatusBadRequest, err.Error())
			return
		}
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
	if err := decodeJSONBody(r.Body, &input); err != nil {
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
	if err := decodeJSONBody(r.Body, &payload); err != nil || payload.StageID == "" || len(payload.QuestionIDs) == 0 {
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
	if err := decodeJSONBody(r.Body, &input); err != nil {
		ErrorJSON(w, http.StatusBadRequest, "Invalid payload: "+err.Error())
		return
	}

	_, err := h.jobs.UpdateQuestion(id, input)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			ErrorJSON(w, http.StatusNotFound, "Question not found")
			return
		}
		if errors.Is(err, service.ErrQuestionTextRequired) {
			ErrorJSON(w, http.StatusBadRequest, err.Error())
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
		if errors.Is(err, service.ErrInvalidAttachmentOwner) {
			ErrorJSON(w, http.StatusBadRequest, "job_id and stage_id must reference an owned selection process")
			return
		}
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
