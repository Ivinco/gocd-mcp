package domain

import (
	"context"
	"fmt"
	"strings"

	"github.com/ivinco/gocd-mcp/internal/gocd"
)

// StageRunResult reports the outcome of a stage run. Scheduled is true only once a
// stage instance attributed to this run has been observed; StageCounter is its
// counter.
type StageRunResult struct {
	Scheduled    bool
	StageCounter int
}

// TriggerStage runs one stage of an existing pipeline instance: a manual-approval
// stage that has not run yet, or a fresh run of a stage that already has. GoCD's 202
// only queues the request, so — as with TriggerPipeline — success is reported only
// once the pipeline instance shows the stage scheduled, with a counter above the
// baseline read before the request, and approved by the calling user (GoCD records
// the approver of manual runs and re-runs; automatic runs carry "changes"). Every
// failure after the request was accepted degrades to the unconfirmed result, for the
// double-run reasons documented on TriggerPipeline.
//
// Unlike a pipeline schedule, a 409 here is a synchronous refusal — GoCD answers
// "Cannot schedule: ... is still in progress" while any stage of the run is active,
// and schedules nothing — so it is returned as an error carrying GoCD's reason
// rather than folded into the wait.
//
// Residual: the pipeline instance shows only a stage's latest run. If someone else
// re-runs the stage inside the wait window — possible only once ours has finished,
// since GoCD refuses concurrent runs — theirs hides ours and the call reports
// unconfirmed although our run happened. Accepted and documented; confirming through
// the stage history API (one entry per run, with its approver) would close it.
func (s *Service) TriggerStage(ctx context.Context, pipeline string, pipelineCounter int, stage, login string) (StageRunResult, error) {
	if err := validatePipelineName(pipeline); err != nil {
		return StageRunResult{}, err
	}
	if err := validateStageName(stage); err != nil {
		return StageRunResult{}, err
	}
	if pipelineCounter < 1 {
		return StageRunResult{}, fmt.Errorf("%w: pipeline counter must be >= 1", ErrInvalidArgument)
	}
	if strings.TrimSpace(login) == "" {
		return StageRunResult{}, fmt.Errorf("%w: login is required", ErrInvalidArgument)
	}

	// Baseline: the stage's current counter, or 0 if it has never been scheduled
	// (GoCD reports counter 1 for a pending manual stage, so scheduled must be read).
	st, err := s.findStage(ctx, pipeline, pipelineCounter, stage)
	if err != nil {
		return StageRunResult{}, err
	}
	base := 0
	if st.Scheduled {
		base = st.Counter
	}

	if err := s.c.RunStage(ctx, pipeline, pipelineCounter, stage); err != nil {
		return StageRunResult{}, err
	}

	for range scheduleWaitAttempts {
		if err := s.sleep(ctx, scheduleWaitInterval); err != nil {
			return StageRunResult{}, nil
		}
		st, err := s.findStage(ctx, pipeline, pipelineCounter, stage)
		if err != nil {
			return StageRunResult{}, nil
		}
		if st.Scheduled && st.Counter > base && st.ApprovedBy == login {
			return StageRunResult{Scheduled: true, StageCounter: st.Counter}, nil
		}
	}
	return StageRunResult{}, nil
}

// findStage reads a pipeline instance and returns the named stage. A stage name the
// instance does not have is an invalid argument, caught before anything is run.
func (s *Service) findStage(ctx context.Context, pipeline string, pipelineCounter int, stage string) (gocd.InstanceStage, error) {
	inst, err := s.c.PipelineInstance(ctx, pipeline, pipelineCounter)
	if err != nil {
		return gocd.InstanceStage{}, err
	}
	for _, st := range inst.Stages {
		if st.Name == stage {
			return st, nil
		}
	}
	return gocd.InstanceStage{}, fmt.Errorf("%w: pipeline %s/%d has no stage %q", ErrInvalidArgument, pipeline, pipelineCounter, stage)
}
