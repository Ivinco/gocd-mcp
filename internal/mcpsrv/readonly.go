package mcpsrv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ivinco/gocd-mcp/internal/auth"
	"github.com/ivinco/gocd-mcp/internal/config"
	"github.com/ivinco/gocd-mcp/internal/domain"
	"github.com/ivinco/gocd-mcp/internal/gocd"
)

// serviceWithLogin builds a per-request domain.Service bound to the authenticated
// user's PAT and returns that user's GoCD login, for tools whose confirmation is
// attributed to the caller.
func serviceWithLogin(ctx context.Context, cfg *config.Config) (*domain.Service, string, error) {
	p, ok := auth.PrincipalFromContext(ctx)
	if !ok {
		return nil, "", fmt.Errorf("no authenticated principal in context")
	}
	return domain.NewService(gocd.NewClient(cfg.GoCDBaseURL, p.PAT, cfg.GoCDTimeout)), p.Login, nil
}

// serviceFor is serviceWithLogin for tools that do not need the login.
func serviceFor(ctx context.Context, cfg *config.Config) (*domain.Service, error) {
	svc, _, err := serviceWithLogin(ctx, cfg)
	return svc, err
}

// toolError maps an error to a user-facing tool error result (so the model can react),
// translating GoCD sentinels into friendly messages.
func toolError(err error) *mcp.CallToolResult {
	msg := err.Error()
	var apiErr *gocd.APIError
	switch {
	case errors.Is(err, domain.ErrInvalidArgument):
		// already user-facing
	case errors.Is(err, gocd.ErrUnauthorized):
		msg = "GoCD rejected the token (unauthorized)"
	case errors.Is(err, gocd.ErrForbidden):
		msg = "your GoCD user lacks permission for this operation"
	case errors.Is(err, gocd.ErrNotFound):
		msg = "not found in GoCD"
	case errors.Is(err, gocd.ErrPreconditionFailed):
		msg = "version conflict (ETag mismatch); re-read and retry"
	case errors.Is(err, gocd.ErrConflict):
		// GoCD refused because of the current state and usually says why ("Cannot
		// schedule: ... is still in progress").
		msg = "GoCD refused the request (conflict); check the current state before retrying"
		var ce *gocd.ConflictError
		if errors.As(err, &ce) && ce.Message != "" {
			msg = "GoCD refused: " + ce.Message
		}
	case errors.As(err, &apiErr) && apiErr.Message != "":
		// GoCD's own explanation (validation failures name the offending fields).
		msg = fmt.Sprintf("GoCD rejected the request (HTTP %d): %s", apiErr.StatusCode, apiErr.Message)
	}
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
	}
}

// readOnly is the annotation shared by all read-only tools.
func readOnly() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true}
}

type listPipelinesInput struct {
	Group string `json:"group,omitempty" jsonschema:"optional pipeline group name to filter by"`
}
type listPipelinesOutput struct {
	Pipelines []gocd.PipelineSummary `json:"pipelines"`
}

type pipelineNameInput struct {
	Name string `json:"name" jsonschema:"the GoCD pipeline name"`
}

type instanceInput struct {
	Name    string `json:"name" jsonschema:"the GoCD pipeline name"`
	Counter int    `json:"counter" jsonschema:"the pipeline run counter"`
}

type historyInput struct {
	Name  string `json:"name" jsonschema:"the GoCD pipeline name"`
	After string `json:"after,omitempty" jsonschema:"opaque pagination cursor: the next_after value from the previous page; omit for the first (newest) page"`
}
type historyOutput struct {
	Runs      []gocd.HistoryItem `json:"runs"`
	NextAfter string             `json:"next_after,omitempty" jsonschema:"opaque cursor for the next (older) page; pass it back as after to continue; absent on the last page"`
}

type agentsOutput struct {
	Agents []gocd.Agent `json:"agents"`
}

type consoleLogInput struct {
	Pipeline        string `json:"pipeline" jsonschema:"the pipeline name"`
	PipelineCounter int    `json:"pipeline_counter" jsonschema:"the pipeline run counter"`
	Stage           string `json:"stage" jsonschema:"the stage name"`
	StageCounter    int    `json:"stage_counter" jsonschema:"the stage run counter (usually 1)"`
	Job             string `json:"job" jsonschema:"the job name"`
	TailLines       int    `json:"tail_lines,omitempty" jsonschema:"return only the last N lines (default 200, 0 = whole log)"`
}
type consoleLogOutput struct {
	Log string `json:"log"`
}

// Map-typed output fields carry omitempty: on a tool error the zero value would
// serialize as null, which fails the SDK's output-schema validation (inferred map
// schemas are not nullable) and turns a clean tool error into a handler failure.
type pipelineConfigOutput struct {
	ETag   string         `json:"etag"`
	Config map[string]any `json:"config,omitempty"`
}

type templateNameInput struct {
	Name string `json:"name" jsonschema:"the GoCD pipeline template name"`
}
type templatesOutput struct {
	Templates []gocd.TemplateSummary `json:"templates"`
}
type templateConfigOutput struct {
	ETag     string         `json:"etag"`
	Template map[string]any `json:"template,omitempty"`
}

// registerReadOnly adds the read-only tool set. These are available in every toolset.
func registerReadOnly(s *mcp.Server, cfg *config.Config) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_pipelines",
		Description: "List GoCD pipelines from the dashboard with their latest run status, pause and lock state. Optionally filter by pipeline group.",
		Annotations: readOnly(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in listPipelinesInput) (*mcp.CallToolResult, listPipelinesOutput, error) {
		svc, err := serviceFor(ctx, cfg)
		if err != nil {
			return nil, listPipelinesOutput{}, err
		}
		pls, err := svc.ListPipelines(ctx, in.Group)
		if err != nil {
			return toolError(err), listPipelinesOutput{}, nil
		}
		return nil, listPipelinesOutput{Pipelines: pls}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_pipeline_status",
		Description: "Get the pause, lock and schedulable state of a single GoCD pipeline.",
		Annotations: readOnly(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in pipelineNameInput) (*mcp.CallToolResult, gocd.PipelineStatus, error) {
		svc, err := serviceFor(ctx, cfg)
		if err != nil {
			return nil, gocd.PipelineStatus{}, err
		}
		st, err := svc.PipelineStatus(ctx, in.Name)
		if err != nil {
			return toolError(err), gocd.PipelineStatus{}, nil
		}
		return nil, *st, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_pipeline_history",
		Description: "Get past runs of a GoCD pipeline (newest first), with stage statuses and who triggered each run (build cause). Paginated by cursor: pass the previous page's next_after as after to fetch older runs.",
		Annotations: readOnly(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in historyInput) (*mcp.CallToolResult, historyOutput, error) {
		svc, err := serviceFor(ctx, cfg)
		if err != nil {
			return nil, historyOutput{}, err
		}
		page, err := svc.PipelineHistory(ctx, in.Name, in.After)
		if err != nil {
			return toolError(err), historyOutput{}, nil
		}
		return nil, historyOutput{Runs: page.Runs, NextAfter: page.NextAfter}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_agents",
		Description: "List GoCD build agents with their config state, runtime state, OS, resources and environments.",
		Annotations: readOnly(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, agentsOutput, error) {
		svc, err := serviceFor(ctx, cfg)
		if err != nil {
			return nil, agentsOutput{}, err
		}
		agents, err := svc.ListAgents(ctx)
		if err != nil {
			return toolError(err), agentsOutput{}, nil
		}
		return nil, agentsOutput{Agents: agents}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_pipeline_instance",
		Description: "Get the detail of a specific pipeline run (counter), including per-stage and per-job state/result.",
		Annotations: readOnly(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in instanceInput) (*mcp.CallToolResult, gocd.PipelineInstance, error) {
		svc, err := serviceFor(ctx, cfg)
		if err != nil {
			return nil, gocd.PipelineInstance{}, err
		}
		inst, err := svc.PipelineInstance(ctx, in.Name, in.Counter)
		if err != nil {
			return toolError(err), gocd.PipelineInstance{}, nil
		}
		return nil, *inst, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_job_console_log",
		Description: "Get the console log of a specific job run. Returns the last tail_lines lines (default 200) to keep output compact.",
		Annotations: readOnly(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in consoleLogInput) (*mcp.CallToolResult, consoleLogOutput, error) {
		svc, err := serviceFor(ctx, cfg)
		if err != nil {
			return nil, consoleLogOutput{}, err
		}
		tailLines := in.TailLines
		if tailLines == 0 {
			tailLines = 200
		}
		log, err := svc.JobConsoleLog(ctx, in.Pipeline, in.PipelineCounter, in.Stage, in.StageCounter, in.Job, tailLines)
		if err != nil {
			return toolError(err), consoleLogOutput{}, nil
		}
		return nil, consoleLogOutput{Log: log}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_pipeline_config",
		Description: "Get the full configuration of a GoCD pipeline plus its ETag (needed to update the config later).",
		Annotations: readOnly(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in pipelineNameInput) (*mcp.CallToolResult, pipelineConfigOutput, error) {
		svc, err := serviceFor(ctx, cfg)
		if err != nil {
			return nil, pipelineConfigOutput{}, err
		}
		cfgResult, err := svc.PipelineConfig(ctx, in.Name)
		if err != nil {
			return toolError(err), pipelineConfigOutput{}, nil
		}
		var m map[string]any
		if err := json.Unmarshal(cfgResult.Config, &m); err != nil {
			return toolError(fmt.Errorf("decode pipeline config: %w", err)), pipelineConfigOutput{}, nil
		}
		return nil, pipelineConfigOutput{ETag: cfgResult.ETag, Config: m}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_templates",
		Description: "List GoCD pipeline templates with the pipelines using each one and whether you can edit / administer it.",
		Annotations: readOnly(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, templatesOutput, error) {
		svc, err := serviceFor(ctx, cfg)
		if err != nil {
			return nil, templatesOutput{}, err
		}
		tpls, err := svc.ListTemplates(ctx)
		if err != nil {
			return toolError(err), templatesOutput{}, nil
		}
		return nil, templatesOutput{Templates: tpls}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_template",
		Description: "Get the full configuration of a GoCD pipeline template (name and stages) plus its ETag (needed to update the template later).",
		Annotations: readOnly(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in templateNameInput) (*mcp.CallToolResult, templateConfigOutput, error) {
		svc, err := serviceFor(ctx, cfg)
		if err != nil {
			return nil, templateConfigOutput{}, err
		}
		tpl, err := svc.TemplateConfig(ctx, in.Name)
		if err != nil {
			return toolError(err), templateConfigOutput{}, nil
		}
		var m map[string]any
		if err := json.Unmarshal(tpl.Config, &m); err != nil {
			return toolError(fmt.Errorf("decode template config: %w", err)), templateConfigOutput{}, nil
		}
		return nil, templateConfigOutput{ETag: tpl.ETag, Template: m}, nil
	})
}
