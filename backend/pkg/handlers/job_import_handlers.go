package handlers

import (
	"errors"
	"net/http"
	"strings"

	"war-room/backend/pkg/jobimport"
	"war-room/backend/pkg/middleware"
	"war-room/backend/pkg/models"
)

// maxImportPreviewBytes bounds one posting pasted into the preview request.
const maxImportPreviewBytes = 256 << 10

// authMeResponse adds the frontend's feature flags to the signed-in user.
type authMeResponse struct {
	*models.User
	JobImportEnabled bool `json:"job_import_enabled"`
}

type jobImportRequest struct {
	URL         string `json:"url"`
	Description string `json:"description"`
}

type jobImportResponse struct {
	Draft        *jobimport.Posting     `json:"draft,omitempty"`
	Technologies []jobimport.Suggestion `json:"technologies"`
	Duplicate    *jobImportDuplicate    `json:"duplicate,omitempty"`
}

type jobImportDuplicate struct {
	ID            string `json:"id"`
	CompanyName   string `json:"company_name"`
	PositionTitle string `json:"position_title"`
}

var jobImportStatuses = map[string]int{
	jobimport.CodeInvalidURL:    http.StatusBadRequest,
	jobimport.CodePasteRequired: http.StatusUnprocessableEntity,
	jobimport.CodeUnsupported:   http.StatusUnprocessableEntity,
	jobimport.CodeNotFound:      http.StatusNotFound,
	jobimport.CodeBlocked:       http.StatusBadGateway,
	jobimport.CodeTimeout:       http.StatusGatewayTimeout,
	jobimport.CodeRateLimited:   http.StatusTooManyRequests,
	jobimport.CodeBusy:          http.StatusServiceUnavailable,
}

// handleJobImportPreview builds a draft from a link or matches technologies in
// pasted text. It saves nothing: the user reviews the draft in the create form.
func (h *Handler) handleJobImportPreview(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.UserIDFromContext(r.Context())
	var input jobImportRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxImportPreviewBytes)
	if err := decodeJSONBody(r.Body, &input); err != nil {
		invalidPayload(w, err)
		return
	}
	response := jobImportResponse{}
	overview := input.Description
	if strings.TrimSpace(input.URL) != "" {
		draft, err := h.previewDraft(r, userID, input.URL)
		if err != nil {
			h.writeJobImportError(w, userID, input.URL, err)
			return
		}
		response.Draft, overview = draft, draft.CompanyOverview
		response.Duplicate = h.findDuplicateJob(userID, input.URL)
	}
	technologies, err := h.jobs.ListTechnologies(userID)
	if err != nil {
		internalError(w, err)
		return
	}
	response.Technologies = jobimport.SuggestTechnologies(overview, technologies)
	JSON(w, http.StatusOK, response)
}

func (h *Handler) previewDraft(r *http.Request, userID, rawURL string) (*jobimport.Posting, error) {
	if !h.cfg.JobImportEnabled {
		return nil, &jobimport.Error{Code: jobimport.CodeBusy, Message: "Job import is disabled."}
	}
	return h.imports.Preview(r.Context(), userID, rawURL)
}

// writeJobImportError still reports a duplicate, because a paste-only link can
// already be saved on a card.
func (h *Handler) writeJobImportError(w http.ResponseWriter, userID, rawURL string, err error) {
	var importError *jobimport.Error
	if !errors.As(err, &importError) {
		internalError(w, err)
		return
	}
	body := map[string]any{"error": importError.Message, "code": importError.Code}
	if duplicate := h.findDuplicateJob(userID, rawURL); duplicate != nil {
		body["duplicate"] = duplicate
	}
	JSON(w, jobImportStatuses[importError.Code], body)
}

func (h *Handler) findDuplicateJob(userID, rawURL string) *jobImportDuplicate {
	target, err := jobimport.Canonicalize(rawURL)
	if err != nil {
		return nil
	}
	jobs, err := h.jobs.GetAllJobs(userID, "all", "")
	if err != nil {
		return nil
	}
	for _, job := range jobs {
		if matchesPosting(job.JobPostURL, target.String()) || matchesPosting(job.ApplicationURL, target.String()) {
			return &jobImportDuplicate{ID: job.ID, CompanyName: job.CompanyName, PositionTitle: job.PositionTitle}
		}
	}
	return nil
}

func matchesPosting(saved *string, target string) bool {
	if saved == nil {
		return false
	}
	parsed, err := jobimport.Canonicalize(*saved)
	return err == nil && parsed.String() == target
}
