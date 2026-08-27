package mcpsrv

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ivinco/gocd-mcp/internal/config"
)

type updateConfigInput struct {
	Name   string         `json:"name" jsonschema:"the GoCD pipeline name"`
	ETag   string         `json:"etag" jsonschema:"the ETag from get_pipeline_config (optimistic locking)"`
	Config map[string]any `json:"config" jsonschema:"the full pipeline config object to write"`
}
type updateConfigOutput struct {
	OK   bool   `json:"ok"`
	ETag string `json:"etag" jsonschema:"the new ETag after the update"`
}

type createPipelineInput struct {
	Group    string         `json:"group" jsonschema:"the pipeline group to create the pipeline in"`
	Pipeline map[string]any `json:"pipeline" jsonschema:"the pipeline config object"`
}

type updateAgentInput struct {
	UUID  string         `json:"uuid" jsonschema:"the agent UUID"`
	Patch map[string]any `json:"patch" jsonschema:"fields to change, e.g. {\"agent_config_state\":\"Disabled\"}"`
}

type createTemplateInput struct {
	Template map[string]any `json:"template" jsonschema:"the full template object: {\"name\": ..., \"stages\": [...]}"`
}

type updateTemplateInput struct {
	Name     string         `json:"name" jsonschema:"the GoCD pipeline template name"`
	ETag     string         `json:"etag" jsonschema:"the ETag from get_template (optimistic locking)"`
	Template map[string]any `json:"template" jsonschema:"the full template object to write; its name must equal name (templates cannot be renamed)"`
}

// registerConfig adds config-editing tools. Registered only for the "full" toolset.
// All are marked destructive.
func registerConfig(s *mcp.Server, cfg *config.Config, log *slog.Logger) {
	if cfg.Toolset != config.ToolsetFull {
		return
	}

	mcp.AddTool(s, &mcp.Tool{
		Name:        "update_pipeline_config",
		Description: "Replace a GoCD pipeline's configuration. Requires the ETag from get_pipeline_config (optimistic locking); on a version conflict, re-read the config and retry.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPtr(true)},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in updateConfigInput) (*mcp.CallToolResult, updateConfigOutput, error) {
		svc, err := serviceFor(ctx, cfg)
		if err != nil {
			return nil, updateConfigOutput{}, err
		}
		raw, err := json.Marshal(in.Config)
		if err != nil {
			return toolError(err), updateConfigOutput{}, nil
		}
		audit(ctx, log, "update_pipeline_config", in.Name)
		newETag, err := svc.UpdatePipelineConfig(ctx, in.Name, raw, in.ETag)
		if err != nil {
			return toolError(err), updateConfigOutput{}, nil
		}
		return nil, updateConfigOutput{OK: true, ETag: newETag}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "create_pipeline",
		Description: "Create a new GoCD pipeline in a pipeline group.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPtr(true)},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in createPipelineInput) (*mcp.CallToolResult, actionResult, error) {
		svc, err := serviceFor(ctx, cfg)
		if err != nil {
			return nil, actionResult{}, err
		}
		raw, err := json.Marshal(in.Pipeline)
		if err != nil {
			return toolError(err), actionResult{}, nil
		}
		audit(ctx, log, "create_pipeline", in.Group)
		if err := svc.CreatePipeline(ctx, in.Group, raw); err != nil {
			return toolError(err), actionResult{}, nil
		}
		return nil, actionResult{OK: true, Detail: "pipeline created"}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "delete_pipeline",
		Description: "Delete a GoCD pipeline by name. This is irreversible.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPtr(true)},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in pipelineNameInput) (*mcp.CallToolResult, actionResult, error) {
		svc, err := serviceFor(ctx, cfg)
		if err != nil {
			return nil, actionResult{}, err
		}
		audit(ctx, log, "delete_pipeline", in.Name)
		if err := svc.DeletePipeline(ctx, in.Name); err != nil {
			return toolError(err), actionResult{}, nil
		}
		return nil, actionResult{OK: true, Detail: "pipeline deleted"}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "update_agent",
		Description: "Update a GoCD build agent (e.g. enable/disable, change resources or environments).",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPtr(true)},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in updateAgentInput) (*mcp.CallToolResult, actionResult, error) {
		svc, err := serviceFor(ctx, cfg)
		if err != nil {
			return nil, actionResult{}, err
		}
		raw, err := json.Marshal(in.Patch)
		if err != nil {
			return toolError(err), actionResult{}, nil
		}
		audit(ctx, log, "update_agent", in.UUID)
		if err := svc.UpdateAgent(ctx, in.UUID, raw); err != nil {
			return toolError(err), actionResult{}, nil
		}
		return nil, actionResult{OK: true, Detail: "agent updated"}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "create_template",
		Description: "Create a new GoCD pipeline template from a full template object (name and stages).",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPtr(true)},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in createTemplateInput) (*mcp.CallToolResult, actionResult, error) {
		svc, err := serviceFor(ctx, cfg)
		if err != nil {
			return nil, actionResult{}, err
		}
		raw, err := json.Marshal(in.Template)
		if err != nil {
			return toolError(err), actionResult{}, nil
		}
		name, _ := in.Template["name"].(string)
		audit(ctx, log, "create_template", name)
		if err := svc.CreateTemplate(ctx, raw); err != nil {
			return toolError(err), actionResult{}, nil
		}
		return nil, actionResult{OK: true, Detail: "template created"}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "update_template",
		Description: "Replace a GoCD pipeline template's configuration. Requires the ETag from get_template (optimistic locking); on a version conflict, re-read the template and retry. Templates cannot be renamed.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPtr(true)},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in updateTemplateInput) (*mcp.CallToolResult, updateConfigOutput, error) {
		svc, err := serviceFor(ctx, cfg)
		if err != nil {
			return nil, updateConfigOutput{}, err
		}
		raw, err := json.Marshal(in.Template)
		if err != nil {
			return toolError(err), updateConfigOutput{}, nil
		}
		audit(ctx, log, "update_template", in.Name)
		newETag, err := svc.UpdateTemplate(ctx, in.Name, raw, in.ETag)
		if err != nil {
			return toolError(err), updateConfigOutput{}, nil
		}
		return nil, updateConfigOutput{OK: true, ETag: newETag}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "delete_template",
		Description: "Delete a GoCD pipeline template by name. GoCD refuses while any pipeline still uses the template. This is irreversible.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPtr(true)},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in templateNameInput) (*mcp.CallToolResult, actionResult, error) {
		svc, err := serviceFor(ctx, cfg)
		if err != nil {
			return nil, actionResult{}, err
		}
		audit(ctx, log, "delete_template", in.Name)
		if err := svc.DeleteTemplate(ctx, in.Name); err != nil {
			return toolError(err), actionResult{}, nil
		}
		return nil, actionResult{OK: true, Detail: "template deleted"}, nil
	})
}
