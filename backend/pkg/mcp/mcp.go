// Package mcp serves War Room's Model Context Protocol endpoint, which lets an
// agent read and edit the signed-in user's processes through typed tools.
package mcp

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/jsonschema-go/jsonschema"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"war-room/backend/pkg/logging"
	"war-room/backend/pkg/middleware"
	"war-room/backend/pkg/models"
	"war-room/backend/pkg/repository"
	"war-room/backend/pkg/service"
)

// maxRequestBytes matches the 1MB bound on the JSON API's job payloads.
const maxRequestBytes = 1 << 20

const instructions = `War Room tracks the signed-in user's job selection processes.
Read AGENTS.md in the war-room repository for the rules on creating and reviewing job cards.
Look up existing processes with list_jobs before creating one. Technologies are a per-user
catalog: reuse an existing entry or alias before creating a new technology. Deleting
records, uploading files, and arranging the board stay user actions in the app.`

// JobService is the subset of service.JobService the tools call.
type JobService interface {
	GetAllJobs(userID, status, search string) ([]models.Job, error)
	GetFullJobDetails(userID, id string) (*models.Job, error)
	CreateJob(userID string, input models.CreateJobInput) (*models.Job, error)
	UpdateJob(userID, id string, input models.UpdateJobInput) (*models.Job, error)
	ListTechnologies(userID string) ([]models.Technology, error)
	CreateTechnology(userID, name string, aliases []string) (models.Technology, error)
	UpdateTechnology(userID, id, name string, aliases []string) (models.Technology, error)
	ListCVVersions(userID string) ([]models.CVVersion, error)
	CreateStage(userID string, input models.CreateStageInput) (*models.Stage, error)
	UpdateStage(userID, id string, input models.UpdateStageInput) (*models.Stage, error)
	ReorderStages(userID, jobID string, stageIDs []string) error
	GetJobCounts(userID string) (*models.JobCounts, error)
	AssignCVVersion(userID, jobID string, versionID *string) error
	GetScheduledMeetings(userID string) ([]models.ScheduledMeeting, error)
	CreateQuestion(userID string, input models.CreateQuestionInput) (*models.Question, error)
	UpdateQuestion(userID, id string, input models.UpdateQuestionInput) (*models.Question, error)
	ReorderQuestions(userID, stageID string, questionIDs []string) error
	ListSearchPeriods(userID string) ([]models.SearchPeriod, error)
	CreateSearchPeriod(userID string, input models.SearchPeriodInput) (*models.SearchPeriod, error)
	UpdateSearchPeriod(userID, id string, input models.SearchPeriodInput) error
}

type idInput struct {
	ID string `json:"id"`
}

type listJobsInput struct {
	Status string `json:"status,omitempty" jsonschema:"waiting, ongoing, accepted, or rejected"`
	Search string `json:"search,omitempty" jsonschema:"text to search for in company, role, and notes"`
}

type updateJobInput struct {
	ID      string                `json:"id"`
	Changes models.UpdateJobInput `json:"changes"`
}

type updateStageInput struct {
	ID      string                  `json:"id"`
	Changes models.UpdateStageInput `json:"changes"`
}

type setJobCVVersionInput struct {
	ID        string  `json:"id"`
	VersionID *string `json:"version_id" jsonschema:"a CV version ID from list_cv_versions, or null to clear the job's CV"`
}

type reorderStagesInput struct {
	JobID    string   `json:"job_id"`
	StageIDs []string `json:"stage_ids" jsonschema:"every stage ID of the job, in the new order"`
}

type updateQuestionInput struct {
	ID      string                     `json:"id"`
	Changes models.UpdateQuestionInput `json:"changes"`
}

type reorderQuestionsInput struct {
	StageID     string   `json:"stage_id"`
	QuestionIDs []string `json:"question_ids" jsonschema:"every question ID of the stage, in the new order"`
}

type updateSearchPeriodInput struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	StartDate string `json:"start_date" jsonschema:"YYYY-MM-DD"`
	EndDate   string `json:"end_date,omitempty" jsonschema:"YYYY-MM-DD; omit for a period that is still open"`
}

type createTechnologyInput struct {
	Name    string   `json:"name"`
	Aliases []string `json:"aliases,omitempty"`
}

type updateTechnologyInput struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Aliases []string `json:"aliases,omitempty"`
}

// The job and stage inputs reuse the API's request types, whose fields carry
// no omitempty tag; inferred schemas would mark every field required. These
// schemas are built once so the SDK's pointer-keyed cache serves every
// per-request server.
var (
	schemaCache       = sdk.NewSchemaCache()
	createJobSchema   = optionalFieldsSchema[models.CreateJobInput]("company_name", "position_title")
	updateJobSchema   = optionalFieldsSchema[updateJobInput]("id", "changes")
	createStageSchema = optionalFieldsSchema[models.CreateStageInput]("job_id", "stage_type")
	updateStageSchema = optionalFieldsSchema[updateStageInput]("id", "changes")
	// The question and search period inputs reuse API types the same way.
	updateQuestionSchema     = optionalFieldsSchema[updateQuestionInput]("id", "changes")
	createSearchPeriodSchema = optionalFieldsSchema[models.SearchPeriodInput]("name", "start_date")
)

// errInvalidOrdering is the public message for a reorder that names an
// unknown, foreign, or duplicate ID, matching the JSON API.
var errInvalidOrdering = errors.New("invalid ordering: every ID must exist once in this list")

func optionalFieldsSchema[T any](required ...string) *jsonschema.Schema {
	schema, err := jsonschema.For[T](nil)
	if err != nil {
		panic(err) // the input types are static, so this is a programming error
	}
	clearRequired(schema)
	schema.Required = required
	return schema
}

func clearRequired(schema *jsonschema.Schema) {
	if schema == nil {
		return
	}
	schema.Required = nil
	for _, property := range schema.Properties {
		clearRequired(property)
	}
	clearRequired(schema.Items)
}

// NewHandler serves the Model Context Protocol over stateless streamable
// HTTP. Mount it behind middleware.RequireSession: every call acts as the
// authenticated user, typically through a personal API token.
func NewHandler(jobs JobService) http.Handler {
	return sdk.NewStreamableHTTPHandler(func(r *http.Request) *sdk.Server {
		return newServer(r.Context(), jobs)
	}, &sdk.StreamableHTTPOptions{
		Stateless:           true,
		JSONResponse:        true,
		MaxRequestBodyBytes: maxRequestBytes,
	})
}

func newServer(requestContext context.Context, jobs JobService) *sdk.Server {
	userID, _ := middleware.UserIDFromContext(requestContext)
	t := tools{jobs: jobs, userID: userID, requestContext: requestContext}
	server := sdk.NewServer(&sdk.Implementation{Name: "war-room"}, &sdk.ServerOptions{
		Instructions: instructions,
		SchemaCache:  schemaCache,
	})
	t.registerJobTools(server)
	t.registerCatalogTools(server)
	t.registerStageTools(server)
	t.registerQuestionTools(server)
	t.registerSearchPeriodTools(server)
	return server
}

// tools binds tool handlers to the authenticated user of one request.
type tools struct {
	jobs           JobService
	userID         string
	requestContext context.Context
}

func (t tools) registerJobTools(server *sdk.Server) {
	addTool(server, t, &sdk.Tool{Name: "list_jobs",
		Description: "List job processes, optionally filtered by status or search text. Use get_job for stages and full details."},
		func(input listJobsInput) (any, error) {
			jobs, err := t.jobs.GetAllJobs(t.userID, input.Status, input.Search)
			if jobs == nil {
				jobs = []models.Job{}
			}
			return map[string]any{"jobs": jobs}, err
		})
	addTool(server, t, &sdk.Tool{Name: "get_job",
		Description: "Get one job process with its stages, interviewers, technologies, CV version, and history."},
		func(input idInput) (any, error) { return t.jobs.GetFullJobDetails(t.userID, input.ID) })
	addTool(server, t, &sdk.Tool{Name: "create_job", InputSchema: createJobSchema,
		Description: "Create a job process. Omitted fields take the app's defaults; a non-referral process gets the latest CV version. Set create_default_stages to add the standard interview stages."},
		func(input models.CreateJobInput) (any, error) { return t.jobs.CreateJob(t.userID, input) })
	addTool(server, t, &sdk.Tool{Name: "update_job", InputSchema: updateJobSchema,
		Description: "Update a job process. Only fields present in changes are modified; technology_ids, when present, replaces the whole Tech Stack."},
		func(input updateJobInput) (any, error) { return t.jobs.UpdateJob(t.userID, input.ID, input.Changes) })
	addTool(server, t, &sdk.Tool{Name: "count_jobs",
		Description: "Count job processes in total and by status (waiting, ongoing, accepted, rejected)."},
		func(struct{}) (any, error) { return t.jobs.GetJobCounts(t.userID) })
	addTool(server, t, &sdk.Tool{Name: "set_job_cv_version",
		Description: "Link a job process to the CV version that was sent, or pass version_id null to unlink it. Returns the updated job."},
		func(input setJobCVVersionInput) (any, error) {
			if err := t.jobs.AssignCVVersion(t.userID, input.ID, input.VersionID); err != nil {
				return nil, err
			}
			return t.jobs.GetFullJobDetails(t.userID, input.ID)
		})
}

func (t tools) registerCatalogTools(server *sdk.Server) {
	addTool(server, t, &sdk.Tool{Name: "list_technologies",
		Description: "List the technology catalog with each entry's aliases."},
		func(struct{}) (any, error) {
			technologies, err := t.jobs.ListTechnologies(t.userID)
			if technologies == nil {
				technologies = []models.Technology{}
			}
			return map[string]any{"technologies": technologies}, err
		})
	addTool(server, t, &sdk.Tool{Name: "create_technology",
		Description: "Add a technology to the catalog with its known alternate names as aliases."},
		func(input createTechnologyInput) (any, error) {
			return t.jobs.CreateTechnology(t.userID, input.Name, input.Aliases)
		})
	addTool(server, t, &sdk.Tool{Name: "update_technology",
		Description: "Rename a technology and replace its aliases."},
		func(input updateTechnologyInput) (any, error) {
			return t.jobs.UpdateTechnology(t.userID, input.ID, input.Name, input.Aliases)
		})
	addTool(server, t, &sdk.Tool{Name: "list_cv_versions",
		Description: "List uploaded CV versions, newest first."},
		func(struct{}) (any, error) {
			versions, err := t.jobs.ListCVVersions(t.userID)
			if versions == nil {
				versions = []models.CVVersion{}
			}
			return map[string]any{"versions": versions}, err
		})
}

func (t tools) registerStageTools(server *sdk.Server) {
	addTool(server, t, &sdk.Tool{Name: "create_stage", InputSchema: createStageSchema,
		Description: "Add an interview stage to a job process. stage_type is HR, Technical, Cultural, or Offer & Decision."},
		func(input models.CreateStageInput) (any, error) { return t.jobs.CreateStage(t.userID, input) })
	addTool(server, t, &sdk.Tool{Name: "update_stage", InputSchema: updateStageSchema,
		Description: "Update an interview stage. Only fields present in changes are modified; interviewers, when present, replaces the panel."},
		func(input updateStageInput) (any, error) {
			return t.jobs.UpdateStage(t.userID, input.ID, input.Changes)
		})
	addTool(server, t, &sdk.Tool{Name: "reorder_stages",
		Description: "Set the order of a job process's interview stages. List every stage ID of the job, first stage first."},
		func(input reorderStagesInput) (any, error) {
			if len(input.StageIDs) == 0 {
				return nil, fmt.Errorf("%w: stage_ids must list the job's stages", service.ErrInvalidField)
			}
			if err := t.jobs.ReorderStages(t.userID, input.JobID, input.StageIDs); err != nil {
				return nil, err
			}
			return map[string]any{"job_id": input.JobID, "stage_ids": input.StageIDs}, nil
		})
	addTool(server, t, &sdk.Tool{Name: "list_meetings",
		Description: "List scheduled interview meetings across all job processes, with each meeting's stage, date, time, link, and company."},
		func(struct{}) (any, error) {
			meetings, err := t.jobs.GetScheduledMeetings(t.userID)
			if meetings == nil {
				meetings = []models.ScheduledMeeting{}
			}
			return map[string]any{"meetings": meetings}, err
		})
}

func (t tools) registerQuestionTools(server *sdk.Server) {
	addTool(server, t, &sdk.Tool{Name: "create_question",
		Description: "Add a question to prepare for an interview stage, with optional answer notes. New questions go last."},
		func(input models.CreateQuestionInput) (any, error) { return t.jobs.CreateQuestion(t.userID, input) })
	addTool(server, t, &sdk.Tool{Name: "update_question", InputSchema: updateQuestionSchema,
		Description: "Update an interview question. Only fields present in changes are modified; set is_asked once it came up in the interview."},
		func(input updateQuestionInput) (any, error) {
			question, err := t.jobs.UpdateQuestion(t.userID, input.ID, input.Changes)
			if err == nil && question == nil {
				// An empty change set skips the scoped UPDATE, so a missing
				// or foreign question surfaces only as a nil reload.
				err = sql.ErrNoRows
			}
			return question, err
		})
	addTool(server, t, &sdk.Tool{Name: "reorder_questions",
		Description: "Set the order of an interview stage's questions. List every question ID of the stage, first question first."},
		func(input reorderQuestionsInput) (any, error) {
			if len(input.QuestionIDs) == 0 {
				return nil, fmt.Errorf("%w: question_ids must list the stage's questions", service.ErrInvalidField)
			}
			if err := t.jobs.ReorderQuestions(t.userID, input.StageID, input.QuestionIDs); err != nil {
				return nil, err
			}
			return map[string]any{"stage_id": input.StageID, "question_ids": input.QuestionIDs}, nil
		})
}

func (t tools) registerSearchPeriodTools(server *sdk.Server) {
	addTool(server, t, &sdk.Tool{Name: "list_search_periods",
		Description: "List job search periods, newest first, with how many processes each one holds."},
		func(struct{}) (any, error) {
			periods, err := t.jobs.ListSearchPeriods(t.userID)
			if periods == nil {
				periods = []models.SearchPeriod{}
			}
			return map[string]any{"search_periods": periods}, err
		})
	addTool(server, t, &sdk.Tool{Name: "create_search_period", InputSchema: createSearchPeriodSchema,
		Description: "Start a job search period. Dates are YYYY-MM-DD; omit end_date while the search is still open. Periods may not overlap, and unassigned processes created within its dates join it."},
		func(input models.SearchPeriodInput) (any, error) { return t.jobs.CreateSearchPeriod(t.userID, input) })
	addTool(server, t, &sdk.Tool{Name: "update_search_period",
		Description: "Replace a search period's name and dates; an omitted end_date reopens it. Periods may not overlap; processes already in it stay, and unassigned ones created within the new dates join it."},
		func(input updateSearchPeriodInput) (any, error) {
			period := models.SearchPeriodInput{Name: input.Name, StartDate: input.StartDate, EndDate: input.EndDate}
			if err := t.jobs.UpdateSearchPeriod(t.userID, input.ID, period); err != nil {
				return nil, err
			}
			return map[string]any{"id": input.ID, "success": true}, nil
		})
}

func addTool[In any](server *sdk.Server, t tools, tool *sdk.Tool, call func(In) (any, error)) {
	sdk.AddTool(server, tool, func(_ context.Context, _ *sdk.CallToolRequest, input In) (*sdk.CallToolResult, any, error) {
		output, err := call(input)
		if err != nil {
			return nil, nil, t.publicError(err)
		}
		return nil, output, nil
	})
}

// publicError mirrors the JSON API's error handling: validation messages
// reach the client, while every other failure is logged with the request and
// hidden.
func (t tools) publicError(err error) error {
	request := logging.RequestFrom(t.requestContext)
	if request != nil {
		request.RecordError(err)
	}
	switch {
	case service.IsValidationError(err):
		return err
	case errors.Is(err, repository.ErrInvalidReorder):
		return errInvalidOrdering
	case errors.Is(err, sql.ErrNoRows):
		return errors.New("not found")
	case request != nil:
		return errors.New("internal server error (request_id " + request.ID + ")")
	default:
		return errors.New("internal server error")
	}
}
