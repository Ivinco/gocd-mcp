package domain

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ivinco/gocd-mcp/internal/gocd"
)

// TriggerResult reports the outcome of a trigger. Scheduled is true only once a new
// pipeline instance has been observed; Counter is that instance's counter.
type TriggerResult struct {
	Scheduled bool
	Counter   int
}

// Scheduling in GoCD is asynchronous: POST /schedule returns 202 Accepted and the
// instance materializes later. We poll the pipeline's latest counter until it grows,
// bounded so a slow (or dropped) schedule does not block forever. The window is kept
// generous because under load GoCD can take several seconds to materialize an instance.
const (
	scheduleWaitAttempts = 16
	scheduleWaitInterval = 500 * time.Millisecond
)

// TriggerPipeline schedules a pipeline run and confirms an instance was actually
// created. GoCD's 202 Accepted only means the request was queued, so returning success
// on 202 alone can report a run that never happens; instead we watch the pipeline's
// counter and report Scheduled only once it advances.
//
// A 409 from GoCD is deliberately NOT surfaced as an error: GoCD can answer 409 and
// still schedule the run asynchronously, so treating it as a failure produces a
// false negative (and any "retry" advice risks double-running the pipeline). A conflict
// is therefore folded into the same confirmation wait as a 202; if the counter never
// advances we return an unconfirmed result (Scheduled=false, nil error), leaving the
// caller to verify rather than blindly retry. Only genuine non-conflict failures
// (auth, not-found, network) are returned as errors, immediately.
func (s *Service) TriggerPipeline(ctx context.Context, name string) (TriggerResult, error) {
	if err := validatePipelineName(name); err != nil {
		return TriggerResult{}, err
	}

	base, err := s.latestCounter(ctx, name)
	if err != nil {
		return TriggerResult{}, err
	}

	if err := s.c.SchedulePipeline(ctx, name); err != nil && !errors.Is(err, gocd.ErrConflict) {
		return TriggerResult{}, err
	}

	for range scheduleWaitAttempts {
		if err := s.sleep(ctx, scheduleWaitInterval); err != nil {
			return TriggerResult{}, err
		}
		cur, err := s.latestCounter(ctx, name)
		if err != nil {
			return TriggerResult{}, err
		}
		if cur > base {
			return TriggerResult{Scheduled: true, Counter: cur}, nil
		}
	}

	// Accepted (202) or conflicted (409) but no new instance appeared in the window:
	// unconfirmed, not an error. GoCD may still schedule it, so the caller must verify
	// before retrying.
	return TriggerResult{Scheduled: false}, nil
}

// latestCounter returns the highest run counter in a pipeline's history, or 0 if it
// has never run. Used to detect that a trigger produced a new instance.
func (s *Service) latestCounter(ctx context.Context, name string) (int, error) {
	hist, err := s.c.PipelineHistory(ctx, name, 0)
	if err != nil {
		return 0, err
	}
	max := 0
	for _, h := range hist {
		if h.Counter > max {
			max = h.Counter
		}
	}
	return max, nil
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
