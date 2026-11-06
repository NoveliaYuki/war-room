package handlers

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"war-room/backend/pkg/logging"
	"war-room/backend/pkg/middleware"
	"war-room/backend/pkg/models"
)

// mcpMaxRequestBytes matches the 1MB bound on the JSON API's job payloads.
const mcpMaxRequestBytes = 1 << 20

const mcpInstructions = `War Room tracks the signed-in user's job selection processes.
Read AGENTS.md in the war-room repository for the rules on creating and reviewing job cards.
Look up existing processes with list_jobs before creating one. Technologies are a per-user
catalog: reuse an existing entry or alias before creating a new technology.`

type mcpIDInput struct {
	ID string `json:"id"`
}

type mcpListJobsInput struct {
	Status string `json:"status,omitempty" jsonschema:"waiting, ongoing, accepted, or rejected"`
	Search string `json:"search,omitempty" jsonschema:"text to search for in company, role, and notes"`
}

type mcpUpdateJobInput struct {
	ID      string                `json:"id"`
	Changes models.UpdateJobInput `json:"changes"`
}

type mcpUpdateStageInput struct {
	ID      string                  `json:"id"`
	Changes models.UpdateStageInput `json:"changes"`
}

type mcpCreateTechnologyInput struct {
	Name    string   `json:"name"`
	Aliases []string `json:"aliases,omitempty"`
}

type mcpUpdateTechnologyInput struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Aliases []string `json:"aliases,omitempty"`
}

// The job and stage inputs reuse the API's request types, whose fields carry
// no omitempty tag; inferred schemas would mark every field required. These
// schemas are built once so the SDK's pointer-keyed cache serves every
// per-request server.
var (
	mcpSchemaCache       = mcp.NewSchemaCache()
	mcpCreateJobSchema   = optionalFieldsSchema[models.CreateJobInput]("company_name", "position_title")
	mcpUpdateJobSchema   = optionalFieldsSchema[mcpUpdateJobInput]("id", "changes")
	mcpCreateStageSchema = optionalFieldsSchema[models.CreateStageInput]("job_id", "stage_type")
	mcpUpdateStageSchema = optionalFieldsSchema[mcpUpdateStageInput]("id", "changes")
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

// newMCPHandler serves the Model Context Protocol at /api/mcp. It runs behind
// RequireSession, so every call acts as the authenticated user, typically
// through a personal API token.
func (h *Handler) newMCPHandler() http.Handler {
	return mcp.NewStreamableHTTPHandler(h.mcpServerFor, &mcp.StreamableHTTPOptions{
		Stateless:           true,
		JSONResponse:        true,
		MaxRequestBodyBytes: mcpMaxRequestBytes,
	})
}

func (h *Handler) mcpServerFor(r *http.Request) *mcp.Server {
	userID, _ := middleware.UserIDFromContext(r.Context())
	tools := mcpTools{handler: h, userID: userID, requestContext: r.Context()}
	server := mcp.NewServer(&mcp.Implementation{Name: "war-room"}, &mcp.ServerOptions{
		Instructions: mcpInstructions,
		SchemaCache:  mcpSchemaCache,
	})
	tools.registerJobTools(server)
	tools.registerCatalogTools(server)
	tools.registerStageTools(server)
	return server
}

// mcpTools binds tool handlers to the authenticated user of one request.
type mcpTools struct {
	handler        *Handler
	userID         string
	requestContext context.Context
}

func (t mcpTools) registerJobTools(server *mcp.Server) {
	addMCPTool(server, t, &mcp.Tool{Name: "list_jobs",
		Description: "List job processes, optionally filtered by status or search text. Use get_job for stages and full details."},
		func(input mcpListJobsInput) (any, error) {
			jobs, err := t.handler.jobs.GetAllJobs(t.userID, input.Status, input.Search)
			if jobs == nil {
				jobs = []models.Job{}
			}
			return map[string]any{"jobs": jobs}, err
		})
	addMCPTool(server, t, &mcp.Tool{Name: "get_job",
		Description: "Get one job process with its stages, interviewers, technologies, CV version, and history."},
		func(input mcpIDInput) (any, error) { return t.handler.jobs.GetFullJobDetails(t.userID, input.ID) })
	addMCPTool(server, t, &mcp.Tool{Name: "create_job", InputSchema: mcpCreateJobSchema,
		Description: "Create a job process. Omitted fields take the app's defaults; a non-referral process gets the latest CV version. Set create_default_stages to add the standard interview stages."},
		func(input models.CreateJobInput) (any, error) { return t.handler.jobs.CreateJob(t.userID, input) })
	addMCPTool(server, t, &mcp.Tool{Name: "update_job", InputSchema: mcpUpdateJobSchema,
		Description: "Update a job process. Only fields present in changes are modified; technology_ids, when present, replaces the whole Tech Stack."},
		func(input mcpUpdateJobInput) (any, error) {
			return t.handler.jobs.UpdateJob(t.userID, input.ID, input.Changes)
		})
}

func (t mcpTools) registerCatalogTools(server *mcp.Server) {
	addMCPTool(server, t, &mcp.Tool{Name: "list_technologies",
		Description: "List the technology catalog with each entry's aliases."},
		func(struct{}) (any, error) {
			technologies, err := t.handler.jobs.ListTechnologies(t.userID)
			return map[string]any{"technologies": technologies}, err
		})
	addMCPTool(server, t, &mcp.Tool{Name: "create_technology",
		Description: "Add a technology to the catalog with its known alternate names as aliases."},
		func(input mcpCreateTechnologyInput) (any, error) {
			return t.handler.jobs.CreateTechnology(t.userID, input.Name, input.Aliases)
		})
	addMCPTool(server, t, &mcp.Tool{Name: "update_technology",
		Description: "Rename a technology and replace its aliases."},
		func(input mcpUpdateTechnologyInput) (any, error) {
			return t.handler.jobs.UpdateTechnology(t.userID, input.ID, input.Name, input.Aliases)
		})
	addMCPTool(server, t, &mcp.Tool{Name: "list_cv_versions",
		Description: "List uploaded CV versions, newest first."},
		func(struct{}) (any, error) {
			versions, err := t.handler.jobs.ListCVVersions(t.userID)
			return map[string]any{"versions": versions}, err
		})
}

func (t mcpTools) registerStageTools(server *mcp.Server) {
	addMCPTool(server, t, &mcp.Tool{Name: "create_stage", InputSchema: mcpCreateStageSchema,
		Description: "Add an interview stage to a job process. stage_type is HR, Technical, Cultural, or Offer & Decision."},
		func(input models.CreateStageInput) (any, error) { return t.handler.jobs.CreateStage(t.userID, input) })
	addMCPTool(server, t, &mcp.Tool{Name: "update_stage", InputSchema: mcpUpdateStageSchema,
		Description: "Update an interview stage. Only fields present in changes are modified; interviewers, when present, replaces the panel."},
		func(input mcpUpdateStageInput) (any, error) {
			return t.handler.jobs.UpdateStage(t.userID, input.ID, input.Changes)
		})
}

func addMCPTool[In any](server *mcp.Server, tools mcpTools, tool *mcp.Tool, call func(In) (any, error)) {
	mcp.AddTool(server, tool, func(_ context.Context, _ *mcp.CallToolRequest, input In) (*mcp.CallToolResult, any, error) {
		output, err := call(input)
		if err != nil {
			return nil, nil, tools.publicError(err)
		}
		return nil, output, nil
	})
}

// publicError mirrors writeServiceError: validation messages reach the
// client, while every other failure is logged with the request and hidden.
func (t mcpTools) publicError(err error) error {
	request := logging.RequestFrom(t.requestContext)
	if request != nil {
		request.RecordError(err)
	}
	switch {
	case isPublicValidationError(err):
		return err
	case errors.Is(err, sql.ErrNoRows):
		return errors.New("not found")
	case request != nil:
		return errors.New("internal server error (request_id " + request.ID + ")")
	default:
		return errors.New("internal server error")
	}
}
