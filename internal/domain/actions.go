package domain

import (
	"context"
	"fmt"
	"strings"
)

// TriggerPipeline schedules a pipeline run.
func (s *Service) TriggerPipeline(ctx context.Context, name string) error {
	if err := validatePipelineName(name); err != nil {
		return err
	}
	return s.c.SchedulePipeline(ctx, name)
}

// PausePipeline pauses a pipeline. A non-empty cause is required.
func (s *Service) PausePipeline(ctx context.Context, name, cause string) error {
	if err := validatePipelineName(name); err != nil {
		return err
	}
	if strings.TrimSpace(cause) == "" {
		return fmt.Errorf("%w: pause cause is required", ErrInvalidArgument)
	}
	return s.c.PausePipeline(ctx, name, cause)
}

// UnpausePipeline resumes a paused pipeline.
func (s *Service) UnpausePipeline(ctx context.Context, name string) error {
	if err := validatePipelineName(name); err != nil {
		return err
	}
	return s.c.UnpausePipeline(ctx, name)
}

// CancelStage cancels a running stage.
func (s *Service) CancelStage(ctx context.Context, pipeline string, pipelineCounter int, stage string, stageCounter int) error {
	if err := validatePipelineName(pipeline); err != nil {
		return err
	}
	if strings.TrimSpace(stage) == "" || strings.ContainsAny(stage, "/\\ \t\n") {
		return fmt.Errorf("%w: invalid stage name %q", ErrInvalidArgument, stage)
	}
	if pipelineCounter < 1 || stageCounter < 1 {
		return fmt.Errorf("%w: counters must be >= 1", ErrInvalidArgument)
	}
	return s.c.CancelStage(ctx, pipeline, pipelineCounter, stage, stageCounter)
}

// CommentOnPipeline adds a comment to a pipeline instance.
func (s *Service) CommentOnPipeline(ctx context.Context, name string, counter int, comment string) error {
	if err := validatePipelineName(name); err != nil {
		return err
	}
	if counter < 1 {
		return fmt.Errorf("%w: counter must be >= 1", ErrInvalidArgument)
	}
	if strings.TrimSpace(comment) == "" {
		return fmt.Errorf("%w: comment text is required", ErrInvalidArgument)
	}
	return s.c.CommentOnPipeline(ctx, name, counter, comment)
}
