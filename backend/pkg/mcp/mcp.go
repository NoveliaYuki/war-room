// Package mcp serves War Room's Model Context Protocol endpoint, which lets an
// agent read and edit the signed-in user's processes through typed tools.
package mcp

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	"github.com/google/jsonschema-go/jsonschema"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"war-room/backend/pkg/logging"
	"war-room/backend/pkg/middleware"
	"war-room/backend/pkg/models"
	"war-room/backend/pkg/service"
)

// maxRequestBytes matches the 1MB bound on the JSON API's job payloads.
const maxRequestBytes = 1 << 20

const instructions = `War Room tracks the signed-in user's job selection processes.
Read AGENTS.md in the war-room repository for the rules on creating and reviewing job cards.
Look up existing processes with list_jobs before creating one. Technologies are a per-user
catalog: reuse an existing entry or alias before creating a new technology.`

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
)

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
	case errors.Is(err, sql.ErrNoRows):
		return errors.New("not found")
	case request != nil:
		return errors.New("internal server error (request_id " + request.ID + ")")
	default:
		return errors.New("internal server error")
	}
}
