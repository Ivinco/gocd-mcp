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
// pipeline instance attributed to this trigger has been observed; Counter is that
// instance's counter.
type TriggerResult struct {
	Scheduled bool
	Counter   int
}

// Scheduling in GoCD is asynchronous: POST /schedule returns 202 Accepted and the
// instance materializes later. We poll the pipeline's history until a new instance
// attributed to this trigger appears, bounded so a slow (or dropped) schedule does not
// block forever. The window is kept generous because under load GoCD can take several
// seconds to materialize an instance.
const (
	scheduleWaitAttempts = 16
	scheduleWaitInterval = 500 * time.Millisecond
)

// TriggerPipeline schedules a pipeline run and confirms an instance was actually
// created for this trigger. GoCD's 202 Accepted only means the request was queued, so
// returning success on 202 alone can report a run that never happens; instead we watch
// the pipeline's history and report Scheduled only once a new instance appears that is
// attributed to this trigger: a forced run approved by the calling user. Counter growth
// alone is not proof — a timer, a material change or another user can start a run of
// the same pipeline inside the wait window. Two concurrent forced triggers by the SAME
// user remain indistinguishable (GoCD records only the approver); that residual
// ambiguity is accepted and documented.
//
// A 409 from GoCD is deliberately NOT surfaced as an error: GoCD can answer 409 and
// still schedule the run asynchronously, so treating it as a failure produces a
// false negative (and any "retry" advice risks double-running the pipeline). A conflict
// is therefore folded into the same confirmation wait as a 202; if no attributed
// instance appears we return an unconfirmed result (Scheduled=false, nil error),
// leaving the caller to verify rather than blindly retry. Errors are returned only
// while nothing has been scheduled yet (validation, baseline read, non-conflict POST
// failures); once the request is accepted, confirmation failures also degrade to
// unconfirmed, for the same double-run reason.
func (s *Service) TriggerPipeline(ctx context.Context, name, login string) (TriggerResult, error) {
	if err := validatePipelineName(name); err != nil {
		return TriggerResult{}, err
	}
	if strings.TrimSpace(login) == "" {
		return TriggerResult{}, fmt.Errorf("%w: login is required", ErrInvalidArgument)
	}

	base, err := s.latestCounter(ctx, name)
	if err != nil {
		return TriggerResult{}, err
	}

	if err := s.c.SchedulePipeline(ctx, name); err != nil && !errors.Is(err, gocd.ErrConflict) {
		return TriggerResult{}, err
	}

	// From here on the schedule request has been accepted, so a hard error would
	// invite the caller to retry — and a retry after an accepted schedule can
	// double-run the pipeline. Every confirmation failure below (cancelled request,
	// history read error) therefore degrades to the unconfirmed result instead.
	for range scheduleWaitAttempts {
		if err := s.sleep(ctx, scheduleWaitInterval); err != nil {
			return TriggerResult{Scheduled: false}, nil
		}
		counter, ok, err := s.newInstanceBy(ctx, name, base, login)
		if err != nil {
			return TriggerResult{Scheduled: false}, nil
		}
		if ok {
			return TriggerResult{Scheduled: true, Counter: counter}, nil
		}
	}

	// Accepted (202) or conflicted (409) but no attributed instance appeared in the
	// window: unconfirmed, not an error. GoCD may still schedule it, so the caller
	// must verify before retrying.
	return TriggerResult{Scheduled: false}, nil
}

// newInstanceBy looks for a run created after base that is attributed to this trigger:
// a forced run approved by login. When several qualify (concurrent triggers by the same
// user), it returns the earliest, so repeated polls settle on the same instance.
// Only the first history page is inspected: our run could only escape it if 10+ newer
// runs of the same pipeline landed between two 500ms polls, which GoCD's multi-second
// materialization latency makes unreachable in practice — and even then the outcome is
// the safe unconfirmed result, so walking pages here isn't worth the complexity.
func (s *Service) newInstanceBy(ctx context.Context, name string, base int, login string) (int, bool, error) {
	page, err := s.c.PipelineHistory(ctx, name, "")
	if err != nil {
		return 0, false, err
	}
	found := 0
	for _, h := range page.Runs {
		if h.Counter > base && h.TriggerForced && h.TriggeredBy == login {
			if found == 0 || h.Counter < found {
				found = h.Counter
			}
		}
	}
	return found, found != 0, nil
}

// latestCounter returns the highest run counter in a pipeline's history, or 0 if it
// has never run. Used as the baseline a new instance must exceed.
func (s *Service) latestCounter(ctx context.Context, name string) (int, error) {
	page, err := s.c.PipelineHistory(ctx, name, "")
	if err != nil {
		return 0, err
	}
	max := 0
	for _, h := range page.Runs {
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
	if err := validateStageName(stage); err != nil {
		return err
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

// validateStageName rejects empty stage names and names that would break the URL path.
func validateStageName(stage string) error {
	if strings.TrimSpace(stage) == "" || strings.ContainsAny(stage, "/\\ \t\n") {
		return fmt.Errorf("%w: invalid stage name %q", ErrInvalidArgument, stage)
	}
	return nil
}
