package domain

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ivinco/gocd-mcp/internal/gocd"
)

// instWith returns a pipeline instance whose "deploy" stage is in the given state.
func instWith(deploy gocd.InstanceStage) *gocd.PipelineInstance {
	deploy.Name = "deploy"
	return &gocd.PipelineInstance{Name: "p", Counter: 7, Stages: []gocd.InstanceStage{{Name: "build", Counter: 1, Scheduled: true, ApprovedBy: "changes"}, deploy}}
}

func fastService(f *fakeClient) *Service {
	svc := NewService(f)
	svc.sleep = func(context.Context, time.Duration) error { return nil }
	return svc
}

func TestTriggerStage_Validation(t *testing.T) {
	ctx := context.Background()
	f := &fakeClient{inst: []*gocd.PipelineInstance{instWith(gocd.InstanceStage{})}}
	svc := fastService(f)
	cases := []struct {
		pipeline string
		counter  int
		stage    string
		login    string
	}{
		{"", 7, "deploy", "alice"},
		{"p", 0, "deploy", "alice"},
		{"p", 7, "", "alice"},
		{"p", 7, "de ploy", "alice"},
		{"p", 7, "deploy", ""},
		{"p", 7, "no-such-stage", "alice"}, // not in the instance
	}
	for _, c := range cases {
		if _, err := svc.TriggerStage(ctx, c.pipeline, c.counter, c.stage, c.login); !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("%+v: expected ErrInvalidArgument, got %v", c, err)
		}
	}
	if len(f.calls) != 0 {
		t.Fatalf("nothing must be run on invalid input, got %v", f.calls)
	}
}

func TestTriggerStage_ManualStageFirstRun(t *testing.T) {
	// Pending manual stage: GoCD reports counter 1 but scheduled=false. After the run
	// it is scheduled, still counter 1, approved by the caller — that must confirm.
	f := &fakeClient{inst: []*gocd.PipelineInstance{
		instWith(gocd.InstanceStage{Counter: 1, Scheduled: false}),
		instWith(gocd.InstanceStage{Counter: 1, Scheduled: true, ApprovalType: "manual", ApprovedBy: "alice"}),
	}}
	res, err := fastService(f).TriggerStage(context.Background(), "p", 7, "deploy", "alice")
	if err != nil {
		t.Fatalf("trigger stage: %v", err)
	}
	if !res.Scheduled || res.StageCounter != 1 {
		t.Fatalf("result = %+v, want scheduled with stage counter 1", res)
	}
	if len(f.calls) != 1 || f.calls[0] != "run_stage:p/7/deploy" {
		t.Fatalf("calls = %v", f.calls)
	}
}

func TestTriggerStage_RerunAdvancesCounter(t *testing.T) {
	f := &fakeClient{inst: []*gocd.PipelineInstance{
		instWith(gocd.InstanceStage{Counter: 1, Scheduled: true, ApprovedBy: "changes"}),
		instWith(gocd.InstanceStage{Counter: 1, Scheduled: true, ApprovedBy: "changes"}), // not yet
		instWith(gocd.InstanceStage{Counter: 2, Scheduled: true, ApprovedBy: "alice"}),
	}}
	res, err := fastService(f).TriggerStage(context.Background(), "p", 7, "deploy", "alice")
	if err != nil {
		t.Fatalf("trigger stage: %v", err)
	}
	if !res.Scheduled || res.StageCounter != 2 {
		t.Fatalf("result = %+v, want scheduled with stage counter 2", res)
	}
}

func TestTriggerStage_ForeignRunDoesNotConfirm(t *testing.T) {
	// Someone else re-ran the stage inside the window: counter advanced, but it is
	// not ours.
	f := &fakeClient{inst: []*gocd.PipelineInstance{
		instWith(gocd.InstanceStage{Counter: 1, Scheduled: true, ApprovedBy: "changes"}),
		instWith(gocd.InstanceStage{Counter: 2, Scheduled: true, ApprovedBy: "bob"}),
	}}
	res, err := fastService(f).TriggerStage(context.Background(), "p", 7, "deploy", "alice")
	if err != nil {
		t.Fatalf("trigger stage: %v", err)
	}
	if res.Scheduled {
		t.Fatalf("a run approved by another user must not confirm: %+v", res)
	}
	if f.instCalls != 1+scheduleWaitAttempts {
		t.Fatalf("instance polls = %d, want baseline + %d", f.instCalls, scheduleWaitAttempts)
	}
}

func TestTriggerStage_ConflictIsAnErrorNotAWait(t *testing.T) {
	// GoCD's 409 for a stage run is a synchronous refusal that schedules nothing, so
	// it must surface immediately with its reason — no polling, no unconfirmed result.
	refusal := &gocd.ConflictError{StatusCode: 409, Message: "Cannot schedule: Pipeline[name='p', counter='7', label='7'] is still in progress"}
	f := &fakeClient{runStageErr: refusal, inst: []*gocd.PipelineInstance{
		instWith(gocd.InstanceStage{Counter: 1, Scheduled: false}),
	}}
	res, err := fastService(f).TriggerStage(context.Background(), "p", 7, "deploy", "alice")
	if !errors.Is(err, gocd.ErrConflict) || !strings.Contains(err.Error(), "still in progress") {
		t.Fatalf("409 must be returned with GoCD's reason, got res=%+v err=%v", res, err)
	}
	if f.instCalls != 1 {
		t.Fatalf("instance reads = %d, want only the baseline (no confirmation polling after a refusal)", f.instCalls)
	}
}

func TestTriggerStage_HardErrorsPropagate(t *testing.T) {
	ctx := context.Background()
	// Baseline read fails: nothing has been requested, so the error is returned.
	f := &fakeClient{instErr: gocd.ErrNotFound}
	if _, err := fastService(f).TriggerStage(ctx, "p", 7, "deploy", "alice"); !errors.Is(err, gocd.ErrNotFound) {
		t.Fatalf("baseline error: got %v", err)
	}
	if len(f.calls) != 0 {
		t.Fatalf("nothing must be run when the baseline read fails, got %v", f.calls)
	}
	// Non-conflict failure of the run request is an error too.
	f = &fakeClient{runStageErr: gocd.ErrForbidden, inst: []*gocd.PipelineInstance{instWith(gocd.InstanceStage{Counter: 1, Scheduled: false})}}
	if _, err := fastService(f).TriggerStage(ctx, "p", 7, "deploy", "alice"); !errors.Is(err, gocd.ErrForbidden) {
		t.Fatalf("run error: got %v", err)
	}
}

func TestTriggerStage_PollFailureDegradesToUnconfirmed(t *testing.T) {
	// Once the request is accepted, a failing instance read must not become an error
	// that invites a retry.
	f := &fakeClient{instErr: gocd.ErrNotFound, instErrOnCall: 2, inst: []*gocd.PipelineInstance{instWith(gocd.InstanceStage{Counter: 1, Scheduled: false})}}
	res, err := fastService(f).TriggerStage(context.Background(), "p", 7, "deploy", "alice")
	if err != nil || res.Scheduled {
		t.Fatalf("poll failure must be unconfirmed, not an error: res=%+v err=%v", res, err)
	}
	if len(f.calls) != 1 {
		t.Fatalf("calls = %v, want exactly one run request", f.calls)
	}
}

func TestTriggerPipeline_PreconditionFailedIsNotFolded(t *testing.T) {
	// Only a real 409 is folded into the confirmation wait; a 412 is an error.
	f := &fakeClient{schedErr: &gocd.ConflictError{StatusCode: 412}}
	svc := fastService(f)
	if _, err := svc.TriggerPipeline(context.Background(), "p", "alice"); !errors.Is(err, gocd.ErrPreconditionFailed) {
		t.Fatalf("412 must propagate as ErrPreconditionFailed, got %v", err)
	}
	if f.histCalls != 1 {
		t.Fatalf("history reads = %d, want only the baseline", f.histCalls)
	}
}
