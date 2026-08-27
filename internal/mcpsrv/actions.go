package mcpsrv

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ivinco/gocd-mcp/internal/auth"
	"github.com/ivinco/gocd-mcp/internal/config"
)

func boolPtr(b bool) *bool { return &b }

// actionResult is the common output of state-changing tools.
type actionResult struct {
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

// audit logs one structured line per mutating action: who did what to which target.
// It never logs the PAT.
func audit(ctx context.Context, log *slog.Logger, action, target string) {
	login := ""
	if p, ok := auth.PrincipalFromContext(ctx); ok {
		login = p.Login
	}
	log.InfoContext(ctx, "audit", "action", action, "login", login, "target", target)
}

type pauseInput struct {
	Name  string `json:"name" jsonschema:"the GoCD pipeline name"`
	Cause string `json:"cause" jsonschema:"reason for pausing (required)"`
}

type triggerStageInput struct {
	Pipeline        string `json:"pipeline" jsonschema:"the pipeline name"`
	PipelineCounter int    `json:"pipeline_counter" jsonschema:"the pipeline run counter"`
	Stage           string `json:"stage" jsonschema:"the stage name"`
}

type cancelStageInput struct {
	Pipeline        string `json:"pipeline" jsonschema:"the pipeline name"`
	PipelineCounter int    `json:"pipeline_counter" jsonschema:"the pipeline run counter"`
	Stage           string `json:"stage" jsonschema:"the stage name"`
	StageCounter    int    `json:"stage_counter" jsonschema:"the stage run counter"`
}

type commentInput struct {
	Name    string `json:"name" jsonschema:"the GoCD pipeline name"`
	Counter int    `json:"counter" jsonschema:"the pipeline instance counter"`
	Comment string `json:"comment" jsonschema:"the comment text"`
}

// registerActions adds state-changing tools. Registered only when the toolset is
// "actions" or "full" (see config.Toolset).
func registerActions(s *mcp.Server, cfg *config.Config, log *slog.Logger) {
	if cfg.Toolset != config.ToolsetActions && cfg.Toolset != config.ToolsetFull {
		return
	}

	mcp.AddTool(s, &mcp.Tool{
		Name:        "trigger_pipeline",
		Description: "Trigger (schedule) a GoCD pipeline run with default materials.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPtr(false)},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in pipelineNameInput) (*mcp.CallToolResult, actionResult, error) {
		// The caller's login is what GoCD records as the build-cause approver of a
		// forced run; confirmation matches the new instance against it.
		svc, login, err := serviceWithLogin(ctx, cfg)
		if err != nil {
			return nil, actionResult{}, err
		}
		audit(ctx, log, "trigger_pipeline", in.Name)
		res, err := svc.TriggerPipeline(ctx, in.Name, login)
		if err != nil {
			return toolError(err), actionResult{}, nil
		}
		if !res.Scheduled {
			return nil, actionResult{OK: false, Detail: "schedule request was accepted (or GoCD reported a conflict) but no new instance attributed to this trigger was confirmed within the wait window; GoCD may still schedule it, so check pipeline history before retrying — a blind retry can double-run the pipeline"}, nil
		}
		return nil, actionResult{OK: true, Detail: fmt.Sprintf("scheduled, instance #%d", res.Counter)}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "trigger_stage",
		Description: "Run a single stage of an existing pipeline run: start a manual-approval stage that has not run yet, or re-run a stage that already has (new stage counter). Confirms the stage was scheduled for this call rather than trusting GoCD's async accept.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPtr(false)},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in triggerStageInput) (*mcp.CallToolResult, actionResult, error) {
		// GoCD records the caller's login as the approver of a manual run or re-run;
		// confirmation matches the stage instance against it.
		svc, login, err := serviceWithLogin(ctx, cfg)
		if err != nil {
			return nil, actionResult{}, err
		}
		audit(ctx, log, "trigger_stage", fmt.Sprintf("%s/%d/%s", in.Pipeline, in.PipelineCounter, in.Stage))
		res, err := svc.TriggerStage(ctx, in.Pipeline, in.PipelineCounter, in.Stage, login)
		if err != nil {
			return toolError(err), actionResult{}, nil
		}
		if !res.Scheduled {
			return nil, actionResult{OK: false, Detail: "run request was accepted (or GoCD reported a conflict) but no stage run attributed to this call was confirmed within the wait window; GoCD may still schedule it, so check the pipeline instance before retrying — a blind retry can run the stage twice"}, nil
		}
		return nil, actionResult{OK: true, Detail: fmt.Sprintf("scheduled, stage counter %d", res.StageCounter)}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "pause_pipeline",
		Description: "Pause a GoCD pipeline so it will not schedule new runs. A reason is required.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPtr(false), IdempotentHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in pauseInput) (*mcp.CallToolResult, actionResult, error) {
		svc, err := serviceFor(ctx, cfg)
		if err != nil {
			return nil, actionResult{}, err
		}
		audit(ctx, log, "pause_pipeline", in.Name)
		if err := svc.PausePipeline(ctx, in.Name, in.Cause); err != nil {
			return toolError(err), actionResult{}, nil
		}
		return nil, actionResult{OK: true, Detail: "pipeline paused"}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "unpause_pipeline",
		Description: "Unpause a GoCD pipeline so it can schedule runs again.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPtr(false), IdempotentHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in pipelineNameInput) (*mcp.CallToolResult, actionResult, error) {
		svc, err := serviceFor(ctx, cfg)
		if err != nil {
			return nil, actionResult{}, err
		}
		audit(ctx, log, "unpause_pipeline", in.Name)
		if err := svc.UnpausePipeline(ctx, in.Name); err != nil {
			return toolError(err), actionResult{}, nil
		}
		return nil, actionResult{OK: true, Detail: "pipeline unpaused"}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "cancel_stage",
		Description: "Cancel a currently running stage of a pipeline run.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPtr(true)},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in cancelStageInput) (*mcp.CallToolResult, actionResult, error) {
		svc, err := serviceFor(ctx, cfg)
		if err != nil {
			return nil, actionResult{}, err
		}
		audit(ctx, log, "cancel_stage", in.Pipeline+"/"+in.Stage)
		if err := svc.CancelStage(ctx, in.Pipeline, in.PipelineCounter, in.Stage, in.StageCounter); err != nil {
			return toolError(err), actionResult{}, nil
		}
		return nil, actionResult{OK: true, Detail: "stage cancelled"}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "comment_on_pipeline",
		Description: "Add a comment to a specific pipeline instance (run).",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPtr(false)},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in commentInput) (*mcp.CallToolResult, actionResult, error) {
		svc, err := serviceFor(ctx, cfg)
		if err != nil {
			return nil, actionResult{}, err
		}
		audit(ctx, log, "comment_on_pipeline", in.Name)
		if err := svc.CommentOnPipeline(ctx, in.Name, in.Counter, in.Comment); err != nil {
			return toolError(err), actionResult{}, nil
		}
		return nil, actionResult{OK: true, Detail: "comment added"}, nil
	})
}
