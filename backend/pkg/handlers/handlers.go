// Package handlers exposes the HTTP API and serves configured static assets.
package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"war-room/backend/pkg/config"
	"war-room/backend/pkg/middleware"
	"war-room/backend/pkg/models"
	"war-room/backend/pkg/repository"
	"war-room/backend/pkg/service"
)

// Handler groups services and configuration used by HTTP route handlers.
type Handler struct {
	jobs           *service.JobService
	logos          *service.LogoService
	cfg            *config.Config
	drive          *service.DriveBackupService
	auth           *service.AuthService
	backupMaxBytes int64
}

// SetDriveService enables the Google Drive backup endpoints.
func (h *Handler) SetDriveService(drive *service.DriveBackupService) { h.drive = drive }

// SetAuthService enables the Google Sign-In and session endpoints.
func (h *Handler) SetAuthService(auth *service.AuthService) { h.auth = auth }

// errBackupStorage marks failures to stage a backup on local disk.
var errBackupStorage = errors.New("backup staging storage failed")

// maxBackupPlainBytes caps a backup archive and the decrypted import payload.
const maxBackupPlainBytes = 500 << 20

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

// internalError logs err server-side and returns a generic 500 so internal
// details (paths, SQL, upstream messages) never reach the client.
func internalError(w http.ResponseWriter, err error) {
	log.Printf("Internal error: %v", err)
	ErrorJSON(w, http.StatusInternalServerError, "Internal server error")
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
	mux.HandleFunc("GET /api/search-periods", h.handleListSearchPeriods)
	mux.HandleFunc("POST /api/search-periods", h.handleCreateSearchPeriod)
	mux.HandleFunc("PUT /api/search-periods/{id}", h.handleUpdateSearchPeriod)
	mux.HandleFunc("DELETE /api/search-periods/{id}", h.handleDeleteSearchPeriod)
	mux.HandleFunc("GET /api/technologies", h.handleListTechnologies)
	mux.HandleFunc("POST /api/technologies", h.handleCreateTechnology)
	mux.HandleFunc("PUT /api/technologies/{id}", h.handleUpdateTechnology)
	mux.HandleFunc("DELETE /api/technologies/{id}", h.handleDeleteTechnology)
	mux.HandleFunc("DELETE /api/technologies/{id}/assignments", h.handleRemoveTechnologyAssignments)

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
	mux.HandleFunc("GET /api/auth/me", h.handleAuthMe)
	mux.HandleFunc("POST /api/auth/signout", h.handleSignOut)
	mux.HandleFunc("POST /api/account/avatar", h.handleUploadAvatar)
	mux.HandleFunc("GET /api/account/avatar", h.handleGetAvatar)
	mux.HandleFunc("DELETE /api/account/avatar", h.handleDeleteAvatar)
	mux.HandleFunc("GET /api/google/status", h.handleGoogleStatus)
	mux.HandleFunc("GET /api/google/connect", h.handleGoogleConnect)
	mux.HandleFunc("GET /api/google/callback", h.handleGoogleCallback)
	mux.HandleFunc("POST /api/google/revoke", h.handleGoogleRevoke)
	mux.HandleFunc("GET /api/google/folders", h.handleGoogleFolders)
	mux.HandleFunc("GET /api/google/picker-token", h.handleGooglePickerToken)
	mux.HandleFunc("PUT /api/google/folder", h.handleGoogleSetFolder)
	mux.HandleFunc("POST /api/google/backup", h.handleGoogleBackupNow)
	if h.cfg.TestMode {
		mux.HandleFunc("POST /api/test/session", h.handleTestSession)
	}

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

func (h *Handler) handleListSearchPeriods(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.UserIDFromContext(r.Context())
	periods, err := h.jobs.ListSearchPeriods(userID)
	if err != nil {
		internalError(w, err)
		return
	}
	JSON(w, http.StatusOK, periods)
}

func (h *Handler) handleCreateSearchPeriod(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.UserIDFromContext(r.Context())
	var input models.SearchPeriodInput
	r.Body = http.MaxBytesReader(w, r.Body, 1048576)
	if err := decodeJSONBody(r.Body, &input); err != nil {
		ErrorJSON(w, http.StatusBadRequest, "Invalid JSON payload: "+err.Error())
		return
	}
	period, err := h.jobs.CreateSearchPeriod(userID, input)
	if err != nil {
		if errors.Is(err, service.ErrInvalidField) {
			ErrorJSON(w, http.StatusBadRequest, err.Error())
			return
		}
		ErrorJSON(w, http.StatusConflict, err.Error())
		return
	}
	JSON(w, http.StatusCreated, period)
}

func (h *Handler) handleUpdateSearchPeriod(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.UserIDFromContext(r.Context())
	var input models.SearchPeriodInput
	r.Body = http.MaxBytesReader(w, r.Body, 1048576)
	if err := decodeJSONBody(r.Body, &input); err != nil {
		ErrorJSON(w, http.StatusBadRequest, "Invalid JSON payload: "+err.Error())
		return
	}
	err := h.jobs.UpdateSearchPeriod(userID, r.PathValue("id"), input)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			ErrorJSON(w, http.StatusNotFound, "Search period not found")
			return
		}
		if errors.Is(err, service.ErrInvalidField) {
			ErrorJSON(w, http.StatusBadRequest, err.Error())
			return
		}
		ErrorJSON(w, http.StatusConflict, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]bool{"success": true})
}

func (h *Handler) handleDeleteSearchPeriod(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.UserIDFromContext(r.Context())
	if err := h.jobs.DeleteSearchPeriod(userID, r.PathValue("id")); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			ErrorJSON(w, http.StatusNotFound, "Search period not found")
			return
		}
		internalError(w, err)
		return
	}
	JSON(w, http.StatusOK, map[string]bool{"success": true})
}

func (h *Handler) handleListTechnologies(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.UserIDFromContext(r.Context())
	items, err := h.jobs.ListTechnologies(userID)
	if err != nil {
		internalError(w, err)
		return
	}
	if items == nil {
		items = []models.Technology{}
	}
	JSON(w, http.StatusOK, items)
}

func (h *Handler) handleCreateTechnology(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.UserIDFromContext(r.Context())
	var payload struct {
		Name    string   `json:"name"`
		Aliases []string `json:"aliases"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	if err := decodeJSONBody(r.Body, &payload); err != nil {
		writeTechnologyPayloadError(w, err)
		return
	}
	item, err := h.jobs.CreateTechnology(userID, payload.Name, payload.Aliases)
	if err != nil {
		writeTechnologyServiceError(w, err)
		return
	}
	JSON(w, http.StatusCreated, item)
}

func (h *Handler) handleUpdateTechnology(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.UserIDFromContext(r.Context())
	var payload struct {
		Name    string   `json:"name"`
		Aliases []string `json:"aliases"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	if err := decodeJSONBody(r.Body, &payload); err != nil {
		writeTechnologyPayloadError(w, err)
		return
	}
	item, err := h.jobs.UpdateTechnology(userID, r.PathValue("id"), payload.Name, payload.Aliases)
	if err != nil {
		writeTechnologyServiceError(w, err)
		return
	}
	JSON(w, http.StatusOK, item)
}

func writeTechnologyPayloadError(w http.ResponseWriter, err error) {
	ErrorJSON(w, http.StatusBadRequest, "Invalid JSON payload: "+err.Error())
}

func writeTechnologyServiceError(w http.ResponseWriter, err error) {
	if errors.Is(err, service.ErrInvalidField) {
		ErrorJSON(w, http.StatusBadRequest, err.Error())
		return
	}
	if errors.Is(err, sql.ErrNoRows) {
		ErrorJSON(w, http.StatusNotFound, "Technology not found")
		return
	}
	internalError(w, err)
}

func (h *Handler) handleDeleteTechnology(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.UserIDFromContext(r.Context())
	if err := h.jobs.DeleteTechnology(userID, r.PathValue("id")); err != nil {
		if errors.Is(err, repository.ErrTechnologyInUse) {
			ErrorJSON(w, http.StatusConflict, "Technology is assigned to one or more processes; remove its assignments first")
		} else if errors.Is(err, sql.ErrNoRows) {
			ErrorJSON(w, http.StatusNotFound, "Technology not found")
		} else {
			internalError(w, err)
		}
		return
	}
	JSON(w, http.StatusOK, map[string]bool{"success": true})
}

func (h *Handler) handleRemoveTechnologyAssignments(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.UserIDFromContext(r.Context())
	count, err := h.jobs.RemoveTechnologyAssignments(userID, r.PathValue("id"))
	if err != nil {
		internalError(w, err)
		return
	}
	JSON(w, http.StatusOK, map[string]int64{"removed": count})
}

func (h *Handler) handleListCVVersions(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.UserIDFromContext(r.Context())
	versions, err := h.jobs.ListCVVersions(userID)
	if err != nil {
		ErrorJSON(w, http.StatusInternalServerError, "Failed to load CV versions")
		return
	}
	JSON(w, http.StatusOK, map[string]any{"versions": versions})
}

func (h *Handler) handleUploadCVVersion(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.UserIDFromContext(r.Context())
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
	version, err := h.jobs.StoreCVVersion(userID, filename, io.MultiReader(bytes.NewReader(buffer[:read]), file), header.Size, mimeType)
	if err != nil {
		ErrorJSON(w, http.StatusInternalServerError, "Failed to save CV version")
		return
	}
	JSON(w, http.StatusCreated, version)
}

func (h *Handler) handleDownloadCVVersion(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.UserIDFromContext(r.Context())
	version, err := h.jobs.GetCVVersion(userID, r.PathValue("id"))
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
	userID, _ := middleware.UserIDFromContext(r.Context())
	if err := h.jobs.DeleteCVVersion(userID, r.PathValue("id")); err != nil {
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
	userID, _ := middleware.UserIDFromContext(r.Context())
	var request struct {
		VersionID *string `json:"version_id"`
	}
	if err := decodeJSONBody(r.Body, &request); err != nil {
		ErrorJSON(w, http.StatusBadRequest, "Invalid CV version assignment")
		return
	}
	if err := h.jobs.AssignCVVersion(userID, r.PathValue("id"), request.VersionID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			ErrorJSON(w, http.StatusNotFound, "Job or CV version not found")
			return
		}
		ErrorJSON(w, http.StatusInternalServerError, "Failed to assign CV version")
		return
	}
	job, err := h.jobs.GetFullJobDetails(userID, r.PathValue("id"))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			ErrorJSON(w, http.StatusNotFound, "Job not found")
			return
		}
		ErrorJSON(w, http.StatusInternalServerError, "Failed to load job")
		return
	}
	JSON(w, http.StatusOK, job)
}

// backupTransferSlots bounds concurrent backup exports and imports, which each
// stage up to one archive in a temp file and use CPU for encryption.
var backupTransferSlots = make(chan struct{}, 2)

// acquireBackupSlot reserves a transfer slot or reports 429 when all are busy.
func acquireBackupSlot(w http.ResponseWriter) bool {
	select {
	case backupTransferSlots <- struct{}{}:
		return true
	default:
		ErrorJSON(w, http.StatusTooManyRequests, "Another backup transfer is in progress; try again shortly")
		return false
	}
}

func releaseBackupSlot() { <-backupTransferSlots }

func (h *Handler) handleExportBackup(w http.ResponseWriter, r *http.Request) {
	if h.drive == nil {
		ErrorJSON(w, http.StatusServiceUnavailable, "Encrypted backup is unavailable")
		return
	}
	if !acquireBackupSlot(w) {
		return
	}
	defer releaseBackupSlot()
	userID, _ := middleware.UserIDFromContext(r.Context())
	archive, size, err := h.drive.SealArchiveFile(h.cfg.DataDir, h.backupPlainLimit(), func(out io.Writer) error {
		return h.jobs.ExportArchive(userID, out)
	})
	if err != nil {
		h.writeBackupExportError(w, err)
		return
	}
	defer service.DiscardTempFile(archive)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="war-room-backup.wrbackup"`)
	w.Header().Set("Cache-Control", "no-store, must-revalidate")
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	_, _ = io.Copy(w, archive)
}

func (h *Handler) writeBackupExportError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrGoogleBackupKeyMissing):
		ErrorJSON(w, http.StatusServiceUnavailable, "Encrypted backups are not configured on this server")
	case errors.Is(err, service.ErrBackupTooLarge):
		ErrorJSON(w, http.StatusRequestEntityTooLarge, "Backup archive exceeds the 500 MiB limit")
	default:
		log.Printf("backup export failed: %v", err)
		ErrorJSON(w, http.StatusInternalServerError, "Failed to create backup archive")
	}
}

// backupPlainLimit is the largest archive an export may produce and an import
// may decrypt; tests lower it through backupMaxBytes.
func (h *Handler) backupPlainLimit() int64 {
	if h.backupMaxBytes > 0 {
		return h.backupMaxBytes
	}
	return maxBackupPlainBytes
}

func (h *Handler) handleImportBackup(w http.ResponseWriter, r *http.Request) {
	if h.drive == nil {
		ErrorJSON(w, http.StatusServiceUnavailable, "Encrypted backup restore is unavailable")
		return
	}
	if !acquireBackupSlot(w) {
		return
	}
	defer releaseBackupSlot()
	userID, _ := middleware.UserIDFromContext(r.Context())
	upload, ok := h.stageBackupUpload(w, r)
	if !ok {
		return
	}
	defer service.DiscardTempFile(upload.archive)
	if err := h.jobs.ImportArchive(userID, upload.archive, upload.size, upload.allowEmpty); err != nil {
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

// stagedBackup is a decrypted, authenticated archive waiting in a temp file.
type stagedBackup struct {
	archive    *os.File
	size       int64
	allowEmpty bool
}

// stageBackupUpload streams the multipart upload straight into a decrypted
// temp file, so a backup is never held in memory and nothing is trusted before
// its final authentication tag verifies. The caller must DiscardTempFile it.
func (h *Handler) stageBackupUpload(w http.ResponseWriter, r *http.Request) (stagedBackup, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, h.backupPlainLimit()+(1<<20))
	form, err := r.MultipartReader()
	if err != nil {
		ErrorJSON(w, http.StatusBadRequest, "Upload an encrypted War Room backup no larger than 500 MiB")
		return stagedBackup{}, false
	}
	var staged stagedBackup
	for {
		part, err := form.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			h.discardStaged(staged)
			writeBackupUploadReadError(w, err)
			return stagedBackup{}, false
		}
		if err := h.readBackupPart(part, &staged); err != nil {
			h.discardStaged(staged)
			h.writeBackupStageError(w, err)
			return stagedBackup{}, false
		}
	}
	if staged.archive == nil {
		ErrorJSON(w, http.StatusBadRequest, "Choose an encrypted War Room backup to import")
		return stagedBackup{}, false
	}
	return staged, true
}

func (h *Handler) readBackupPart(part *multipart.Part, staged *stagedBackup) error {
	defer func() { _ = part.Close() }()
	switch part.FormName() {
	case "allow_empty":
		value, err := io.ReadAll(io.LimitReader(part, 16))
		staged.allowEmpty = err == nil && string(value) == "true"
		return err
	case "backup":
		if staged.archive != nil {
			return nil
		}
		return h.decryptBackupPart(part, staged)
	}
	return nil
}

func (h *Handler) decryptBackupPart(part io.Reader, staged *stagedBackup) error {
	archive, err := os.CreateTemp(h.cfg.DataDir, ".war-room-import-*.zip")
	if err != nil {
		return errors.Join(errBackupStorage, err)
	}
	if err := h.drive.DecryptBackupTo(archive, part, h.backupPlainLimit()); err != nil {
		service.DiscardTempFile(archive)
		return err
	}
	info, err := archive.Stat()
	if err != nil {
		service.DiscardTempFile(archive)
		return errors.Join(errBackupStorage, err)
	}
	staged.archive, staged.size = archive, info.Size()
	return nil
}

func (h *Handler) discardStaged(staged stagedBackup) {
	if staged.archive != nil {
		service.DiscardTempFile(staged.archive)
	}
}

func writeBackupUploadReadError(w http.ResponseWriter, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		ErrorJSON(w, http.StatusRequestEntityTooLarge, "Backup exceeds the 500 MiB limit")
		return
	}
	ErrorJSON(w, http.StatusBadRequest, "Choose a readable War Room backup to import")
}

func (h *Handler) writeBackupStageError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrGoogleBackupKeyMissing):
		ErrorJSON(w, http.StatusServiceUnavailable, "Encrypted backup restore is not configured on this server")
	case errors.Is(err, service.ErrBackupFormat):
		ErrorJSON(w, http.StatusBadRequest, "Only encrypted War Room backups can be imported")
	case errors.Is(err, service.ErrBackupAuth):
		ErrorJSON(w, http.StatusBadRequest, "Encrypted backup could not be unlocked with this installation's key")
	case errors.Is(err, service.ErrBackupTooLarge):
		ErrorJSON(w, http.StatusRequestEntityTooLarge, "Backup exceeds the 500 MiB limit")
	case errors.Is(err, errBackupStorage):
		log.Printf("backup import staging failed: %v", err)
		ErrorJSON(w, http.StatusInternalServerError, "Failed to prepare backup import")
	default:
		writeBackupUploadReadError(w, err)
	}
}

// handleHealthCheck returns a 200 OK status to indicate the service is running.
func (h *Handler) handleHealthCheck(w http.ResponseWriter, r *http.Request) {
	JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) handleJobCounts(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.UserIDFromContext(r.Context())
	counts, err := h.jobs.GetJobCounts(userID)
	if err != nil {
		internalError(w, err)
		return
	}
	JSON(w, http.StatusOK, counts)
}

func (h *Handler) handleListJobs(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.UserIDFromContext(r.Context())
	status := r.URL.Query().Get("status")
	search := r.URL.Query().Get("search")
	jobs, err := h.jobs.GetAllJobs(userID, status, search)
	if err != nil {
		internalError(w, err)
		return
	}
	if jobs == nil {
		jobs = []models.Job{}
	}
	JSON(w, http.StatusOK, jobs)
}

func (h *Handler) handleCreateJob(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.UserIDFromContext(r.Context())
	var input models.CreateJobInput
	r.Body = http.MaxBytesReader(w, r.Body, 1048576) // 1MB limit
	if err := decodeJSONBody(r.Body, &input); err != nil {
		ErrorJSON(w, http.StatusBadRequest, "Invalid JSON payload: "+err.Error())
		return
	}

	job, err := h.jobs.CreateJob(userID, input)
	if err != nil {
		ErrorJSON(w, http.StatusBadRequest, err.Error())
		return
	}
	JSON(w, http.StatusCreated, job)
}

// writeReorderError reports unknown, duplicate, or foreign IDs as client errors.
func writeReorderError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, repository.ErrInvalidReorder):
		ErrorJSON(w, http.StatusBadRequest, "Invalid ordering: every ID must exist once in this list")
	case errors.Is(err, sql.ErrNoRows):
		ErrorJSON(w, http.StatusNotFound, "Not found")
	default:
		internalError(w, err)
	}
}

func (h *Handler) handleReorderJobs(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.UserIDFromContext(r.Context())
	var payload struct {
		JobIDs []string `json:"job_ids"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1048576) // 1MB limit
	if err := decodeJSONBody(r.Body, &payload); err != nil || len(payload.JobIDs) == 0 {
		ErrorJSON(w, http.StatusBadRequest, "Invalid payload: job_ids array required")
		return
	}

	if err := h.jobs.ReorderJobs(userID, payload.JobIDs); err != nil {
		writeReorderError(w, err)
		return
	}
	JSON(w, http.StatusOK, map[string]bool{"success": true})
}

func (h *Handler) handleGetJob(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.UserIDFromContext(r.Context())
	id := r.PathValue("id")
	job, err := h.jobs.GetFullJobDetails(userID, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			ErrorJSON(w, http.StatusNotFound, "Job process not found")
			return
		}
		internalError(w, err)
		return
	}
	JSON(w, http.StatusOK, job)
}

func (h *Handler) handleUpdateJob(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.UserIDFromContext(r.Context())
	id := r.PathValue("id")
	var input models.UpdateJobInput
	r.Body = http.MaxBytesReader(w, r.Body, 1048576) // 1MB limit
	if err := decodeJSONBody(r.Body, &input); err != nil {
		ErrorJSON(w, http.StatusBadRequest, "Invalid JSON payload: "+err.Error())
		return
	}

	job, err := h.jobs.UpdateJob(userID, id, input)
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
		internalError(w, err)
		return
	}
	JSON(w, http.StatusOK, job)
}

func (h *Handler) handleDeleteJob(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.UserIDFromContext(r.Context())
	id := r.PathValue("id")
	if err := h.jobs.DeleteJob(userID, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			ErrorJSON(w, http.StatusNotFound, "Job process not found")
			return
		}
		internalError(w, err)
		return
	}
	JSON(w, http.StatusOK, map[string]interface{}{"success": true, "id": id})
}

func (h *Handler) handleCreateStage(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.UserIDFromContext(r.Context())
	var input models.CreateStageInput
	r.Body = http.MaxBytesReader(w, r.Body, 1048576) // 1MB limit
	if err := decodeJSONBody(r.Body, &input); err != nil {
		ErrorJSON(w, http.StatusBadRequest, "Invalid payload: "+err.Error())
		return
	}

	stage, err := h.jobs.CreateStage(userID, input)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			ErrorJSON(w, http.StatusNotFound, "Job process not found")
			return
		}
		ErrorJSON(w, http.StatusBadRequest, err.Error())
		return
	}
	JSON(w, http.StatusCreated, map[string]interface{}{"success": true, "id": stage.ID})
}

func (h *Handler) handleReorderStages(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.UserIDFromContext(r.Context())
	var payload struct {
		JobID    string   `json:"job_id"`
		StageIDs []string `json:"stage_ids"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1048576) // 1MB limit
	if err := decodeJSONBody(r.Body, &payload); err != nil || payload.JobID == "" || len(payload.StageIDs) == 0 {
		ErrorJSON(w, http.StatusBadRequest, "Invalid payload: job_id and stage_ids array required")
		return
	}

	if err := h.jobs.ReorderStages(userID, payload.JobID, payload.StageIDs); err != nil {
		writeReorderError(w, err)
		return
	}
	JSON(w, http.StatusOK, map[string]bool{"success": true})
}

func (h *Handler) handleScheduleStage(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.UserIDFromContext(r.Context())
	id := r.PathValue("id")
	var input models.ScheduleMeetingInput
	r.Body = http.MaxBytesReader(w, r.Body, 1048576) // 1MB limit
	if err := decodeJSONBody(r.Body, &input); err != nil {
		ErrorJSON(w, http.StatusBadRequest, "Invalid payload: "+err.Error())
		return
	}

	if err := h.jobs.ScheduleMeeting(userID, id, input); err != nil {
		if errors.Is(err, service.ErrInvalidMeetingDate) || errors.Is(err, service.ErrInvalidField) {
			ErrorJSON(w, http.StatusBadRequest, err.Error())
			return
		}
		if errors.Is(err, sql.ErrNoRows) {
			ErrorJSON(w, http.StatusNotFound, "Stage not found")
			return
		}
		internalError(w, err)
		return
	}
	JSON(w, http.StatusOK, map[string]interface{}{"success": true, "stage_id": id})
}

func (h *Handler) handleSetCurrentStage(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.UserIDFromContext(r.Context())
	id := r.PathValue("id")
	stages, err := h.jobs.SetCurrentStage(userID, id)
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
	userID, _ := middleware.UserIDFromContext(r.Context())
	id := r.PathValue("id")
	var input models.UpdateStageInput
	r.Body = http.MaxBytesReader(w, r.Body, 1048576) // 1MB limit
	if err := decodeJSONBody(r.Body, &input); err != nil {
		ErrorJSON(w, http.StatusBadRequest, "Invalid payload: "+err.Error())
		return
	}

	_, err := h.jobs.UpdateStage(userID, id, input)
	if err != nil {
		if errors.Is(err, service.ErrInvalidMeetingDate) || errors.Is(err, service.ErrInvalidField) {
			ErrorJSON(w, http.StatusBadRequest, err.Error())
			return
		}
		if errors.Is(err, sql.ErrNoRows) {
			ErrorJSON(w, http.StatusNotFound, "Stage not found")
			return
		}
		internalError(w, err)
		return
	}
	JSON(w, http.StatusOK, map[string]interface{}{"success": true, "id": id})
}

func (h *Handler) handleDeleteStage(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.UserIDFromContext(r.Context())
	id := r.PathValue("id")
	if err := h.jobs.DeleteStage(userID, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			ErrorJSON(w, http.StatusNotFound, "Stage not found")
			return
		}
		internalError(w, err)
		return
	}
	JSON(w, http.StatusOK, map[string]interface{}{"success": true, "id": id})
}

func (h *Handler) handleCreateQuestion(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.UserIDFromContext(r.Context())
	var input models.CreateQuestionInput
	r.Body = http.MaxBytesReader(w, r.Body, 1048576) // 1MB limit
	if err := decodeJSONBody(r.Body, &input); err != nil {
		ErrorJSON(w, http.StatusBadRequest, "Invalid payload: "+err.Error())
		return
	}

	q, err := h.jobs.CreateQuestion(userID, input)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			ErrorJSON(w, http.StatusNotFound, "Stage not found")
			return
		}
		ErrorJSON(w, http.StatusBadRequest, err.Error())
		return
	}
	JSON(w, http.StatusCreated, map[string]interface{}{"success": true, "id": q.ID})
}

func (h *Handler) handleReorderQuestions(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.UserIDFromContext(r.Context())
	var payload struct {
		StageID     string   `json:"stage_id"`
		QuestionIDs []string `json:"question_ids"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1048576) // 1MB limit
	if err := decodeJSONBody(r.Body, &payload); err != nil || payload.StageID == "" || len(payload.QuestionIDs) == 0 {
		ErrorJSON(w, http.StatusBadRequest, "Invalid payload: stage_id and question_ids array required")
		return
	}

	if err := h.jobs.ReorderQuestions(userID, payload.StageID, payload.QuestionIDs); err != nil {
		writeReorderError(w, err)
		return
	}
	JSON(w, http.StatusOK, map[string]interface{}{"success": true, "stage_id": payload.StageID})
}

func (h *Handler) handleUpdateQuestion(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.UserIDFromContext(r.Context())
	id := r.PathValue("id")
	var input models.UpdateQuestionInput
	r.Body = http.MaxBytesReader(w, r.Body, 1048576) // 1MB limit
	if err := decodeJSONBody(r.Body, &input); err != nil {
		ErrorJSON(w, http.StatusBadRequest, "Invalid payload: "+err.Error())
		return
	}

	_, err := h.jobs.UpdateQuestion(userID, id, input)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			ErrorJSON(w, http.StatusNotFound, "Question not found")
			return
		}
		if errors.Is(err, service.ErrQuestionTextRequired) {
			ErrorJSON(w, http.StatusBadRequest, err.Error())
			return
		}
		internalError(w, err)
		return
	}
	JSON(w, http.StatusOK, map[string]interface{}{"success": true, "id": id})
}

func (h *Handler) handleDeleteQuestion(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.UserIDFromContext(r.Context())
	id := r.PathValue("id")
	if err := h.jobs.DeleteQuestion(userID, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			ErrorJSON(w, http.StatusNotFound, "Question not found")
			return
		}
		internalError(w, err)
		return
	}
	JSON(w, http.StatusOK, map[string]interface{}{"success": true, "id": id})
}

func (h *Handler) handleGetMeetings(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.UserIDFromContext(r.Context())
	meetings, err := h.jobs.GetScheduledMeetings(userID)
	if err != nil {
		internalError(w, err)
		return
	}
	if meetings == nil {
		meetings = []models.ScheduledMeeting{}
	}
	JSON(w, http.StatusOK, meetings)
}

func (h *Handler) handleUploadAttachment(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.UserIDFromContext(r.Context())
	jobID, stageID, file, header, cleanup, ok := parseAttachmentUpload(w, r)
	if !ok {
		return
	}
	defer cleanup()
	defer func() { _ = file.Close() }()
	att, err := h.storeUploadedAttachment(userID, jobID, stageID, file, header)
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

func (h *Handler) storeUploadedAttachment(userID, jobID string, stageID *string, file multipart.File, header *multipart.FileHeader) (*models.Attachment, error) {
	prefix := make([]byte, 512)
	n, err := io.ReadFull(file, prefix)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, err
	}
	mimeType := http.DetectContentType(prefix[:n])
	content := io.MultiReader(bytes.NewReader(prefix[:n]), file)
	cleanFilename := filepath.Base(header.Filename)
	return h.jobs.StoreAttachment(userID, jobID, stageID, cleanFilename, content, header.Size, mimeType)
}

func (h *Handler) handleDownloadAttachment(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.UserIDFromContext(r.Context())
	id := r.PathValue("id")
	att, err := h.jobs.GetAttachmentByID(userID, id)
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
	userID, _ := middleware.UserIDFromContext(r.Context())
	id := r.PathValue("id")
	if err := h.jobs.DeleteAttachment(userID, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			ErrorJSON(w, http.StatusNotFound, "Attachment not found")
			return
		}
		internalError(w, err)
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
