// Package domain holds the GoCD-facing business logic: input validation and
// orchestration of GoCD client calls. It is transport-agnostic (no MCP, no net/http)
// and unit-tested with a fake Client.
package domain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ivinco/gocd-mcp/internal/gocd"
)

// ErrInvalidArgument indicates a caller-supplied argument failed validation.
var ErrInvalidArgument = errors.New("invalid argument")

// Client is the subset of the GoCD API the domain depends on. *gocd.Client
// implements it; tests provide a fake.
type Client interface {
	Dashboard(ctx context.Context) ([]gocd.PipelineSummary, error)
	PipelineStatus(ctx context.Context, name string) (*gocd.PipelineStatus, error)
	PipelineHistory(ctx context.Context, name, after string) (*gocd.HistoryPage, error)
	ListAgents(ctx context.Context) ([]gocd.Agent, error)
	PipelineConfig(ctx context.Context, name string) (*gocd.PipelineConfig, error)
	PipelineInstance(ctx context.Context, name string, counter int) (*gocd.PipelineInstance, error)
	JobConsoleLog(ctx context.Context, pipeline string, pipelineCounter int, stage string, stageCounter int, job string) (string, error)
	ListTemplates(ctx context.Context) ([]gocd.TemplateSummary, error)
	TemplateConfig(ctx context.Context, name string) (*gocd.TemplateConfig, error)

	SchedulePipeline(ctx context.Context, name string) error
	PausePipeline(ctx context.Context, name, cause string) error
	UnpausePipeline(ctx context.Context, name string) error
	CancelStage(ctx context.Context, pipeline string, pipelineCounter int, stage string, stageCounter int) error
	RunStage(ctx context.Context, pipeline string, pipelineCounter int, stage string) error
	CommentOnPipeline(ctx context.Context, name string, counter int, comment string) error

	UpdatePipelineConfig(ctx context.Context, name string, config json.RawMessage, etag string) (string, error)
	CreatePipeline(ctx context.Context, body json.RawMessage) error
	UpdateAgent(ctx context.Context, uuid string, patch json.RawMessage) error
	DeletePipeline(ctx context.Context, name string) error
	CreateTemplate(ctx context.Context, template json.RawMessage) error
	UpdateTemplate(ctx context.Context, name string, template json.RawMessage, etag string) (string, error)
	DeleteTemplate(ctx context.Context, name string) error
}

// Service exposes GoCD operations to the MCP layer.
type Service struct {
	c Client
	// sleep waits for d or until ctx is done; injectable so tests need not sleep.
	sleep func(ctx context.Context, d time.Duration) error
}

// NewService builds a Service over the given GoCD client.
func NewService(c Client) *Service {
	return &Service{c: c, sleep: ctxSleep}
}

// ctxSleep blocks for d unless ctx is cancelled first.
func ctxSleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// ListPipelines returns dashboard pipelines, optionally filtered to one group.
func (s *Service) ListPipelines(ctx context.Context, group string) ([]gocd.PipelineSummary, error) {
	all, err := s.c.Dashboard(ctx)
	if err != nil {
		return nil, err
	}
	if group == "" {
		return all, nil
	}
	filtered := make([]gocd.PipelineSummary, 0)
	for _, p := range all {
		if p.Group == group {
			filtered = append(filtered, p)
		}
	}
	return filtered, nil
}

// PipelineStatus returns the pause/lock state of a pipeline.
func (s *Service) PipelineStatus(ctx context.Context, name string) (*gocd.PipelineStatus, error) {
	if err := validatePipelineName(name); err != nil {
		return nil, err
	}
	return s.c.PipelineStatus(ctx, name)
}

// PipelineHistory returns one page of a pipeline's past runs. after is the opaque
// cursor from the previous page's NextAfter; empty fetches the first (newest) page.
func (s *Service) PipelineHistory(ctx context.Context, name, after string) (*gocd.HistoryPage, error) {
	if err := validatePipelineName(name); err != nil {
		return nil, err
	}
	// GoCD requires the cursor to be a positive integer; catch garbage before the
	// round-trip so the caller gets a consistent invalid-argument error.
	if after != "" {
		if v, err := strconv.ParseUint(after, 10, 64); err != nil || v == 0 {
			return nil, fmt.Errorf("%w: after must be a next_after cursor from a previous page", ErrInvalidArgument)
		}
	}
	return s.c.PipelineHistory(ctx, name, after)
}

// ListAgents returns all build agents.
func (s *Service) ListAgents(ctx context.Context) ([]gocd.Agent, error) {
	return s.c.ListAgents(ctx)
}

// PipelineConfig returns a pipeline's config and ETag.
func (s *Service) PipelineConfig(ctx context.Context, name string) (*gocd.PipelineConfig, error) {
	if err := validatePipelineName(name); err != nil {
		return nil, err
	}
	return s.c.PipelineConfig(ctx, name)
}

// PipelineInstance returns the detail of one pipeline run.
func (s *Service) PipelineInstance(ctx context.Context, name string, counter int) (*gocd.PipelineInstance, error) {
	if err := validatePipelineName(name); err != nil {
		return nil, err
	}
	if counter < 1 {
		return nil, fmt.Errorf("%w: counter must be >= 1", ErrInvalidArgument)
	}
	return s.c.PipelineInstance(ctx, name, counter)
}

// validatePipelineName rejects empty names and names that would break the URL path.
func validatePipelineName(name string) error { return validateName("pipeline", name) }

// validateTemplateName applies the same rules to pipeline template names.
func validateTemplateName(name string) error { return validateName("template", name) }

func validateName(kind, name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("%w: %s name is required", ErrInvalidArgument, kind)
	}
	if strings.ContainsAny(name, "/\\ \t\n") {
		return fmt.Errorf("%w: %s name %q contains invalid characters", ErrInvalidArgument, kind, name)
	}
	return nil
}
