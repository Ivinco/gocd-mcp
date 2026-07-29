package domain

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ivinco/gocd-mcp/internal/gocd"
)

// noSleep replaces the real poll wait so trigger tests run without delay.
func noSleep(context.Context, time.Duration) error { return nil }

// fakeClient is a programmable domain.Client for unit tests.
type fakeClient struct {
	dashboard []gocd.PipelineSummary
	dashErr   error

	// schedule/verify controls
	schedErr error // returned by SchedulePipeline
	histErr  error // returned by PipelineHistory
	counters []int // successive latest counters returned by PipelineHistory (last value repeats)
	histIdx  int

	// recorded action calls
	calls []string
}

func (f *fakeClient) Dashboard(context.Context) ([]gocd.PipelineSummary, error) {
	return f.dashboard, f.dashErr
}
func (f *fakeClient) PipelineStatus(context.Context, string) (*gocd.PipelineStatus, error) {
	return &gocd.PipelineStatus{}, nil
}
func (f *fakeClient) PipelineHistory(context.Context, string, int) ([]gocd.HistoryItem, error) {
	if f.histErr != nil {
		return nil, f.histErr
	}
	if len(f.counters) == 0 {
		return nil, nil
	}
	i := f.histIdx
	if i >= len(f.counters) {
		i = len(f.counters) - 1
	}
	f.histIdx++
	return []gocd.HistoryItem{{Counter: f.counters[i]}}, nil
}
func (f *fakeClient) ListAgents(context.Context) ([]gocd.Agent, error) { return nil, nil }
func (f *fakeClient) PipelineConfig(context.Context, string) (*gocd.PipelineConfig, error) {
	return &gocd.PipelineConfig{}, nil
}
func (f *fakeClient) JobConsoleLog(context.Context, string, int, string, int, string) (string, error) {
	return "line1\nline2\nline3\n", nil
}
func (f *fakeClient) PipelineInstance(_ context.Context, name string, counter int) (*gocd.PipelineInstance, error) {
	return &gocd.PipelineInstance{Name: name, Counter: counter}, nil
}
func (f *fakeClient) DeletePipeline(_ context.Context, name string) error {
	f.calls = append(f.calls, "delete:"+name)
	return nil
}
func (f *fakeClient) SchedulePipeline(_ context.Context, name string) error {
	f.calls = append(f.calls, "schedule:"+name)
	return f.schedErr
}
func (f *fakeClient) PausePipeline(_ context.Context, name, cause string) error {
	f.calls = append(f.calls, "pause:"+name+":"+cause)
	return nil
}
func (f *fakeClient) UnpausePipeline(_ context.Context, name string) error {
	f.calls = append(f.calls, "unpause:"+name)
	return nil
}
func (f *fakeClient) CancelStage(_ context.Context, pipeline string, pc int, stage string, sc int) error {
	f.calls = append(f.calls, "cancel:"+pipeline+"/"+stage)
	return nil
}
func (f *fakeClient) CommentOnPipeline(_ context.Context, name string, counter int, comment string) error {
	f.calls = append(f.calls, "comment:"+name)
	return nil
}
func (f *fakeClient) UpdatePipelineConfig(_ context.Context, name string, _ json.RawMessage, etag string) (string, error) {
	f.calls = append(f.calls, "update_config:"+name+":"+etag)
	return "new-etag", nil
}
func (f *fakeClient) CreatePipeline(_ context.Context, body json.RawMessage) error {
	f.calls = append(f.calls, "create:"+string(body))
	return nil
}
func (f *fakeClient) UpdateAgent(_ context.Context, uuid string, _ json.RawMessage) error {
	f.calls = append(f.calls, "update_agent:"+uuid)
	return nil
}

func TestListPipelines_GroupFilter(t *testing.T) {
	f := &fakeClient{dashboard: []gocd.PipelineSummary{
		{Group: "a", Name: "p1"},
		{Group: "b", Name: "p2"},
		{Group: "a", Name: "p3"},
	}}
	svc := NewService(f)

	all, err := svc.ListPipelines(context.Background(), "")
	if err != nil || len(all) != 3 {
		t.Fatalf("no filter: got %d pipelines, err=%v", len(all), err)
	}

	a, err := svc.ListPipelines(context.Background(), "a")
	if err != nil {
		t.Fatalf("filter err: %v", err)
	}
	if len(a) != 2 {
		t.Fatalf("group filter: got %d, want 2", len(a))
	}
	for _, p := range a {
		if p.Group != "a" {
			t.Fatalf("unexpected group %q in filtered result", p.Group)
		}
	}
}

func TestListPipelines_PropagatesError(t *testing.T) {
	sentinel := errors.New("boom")
	svc := NewService(&fakeClient{dashErr: sentinel})
	if _, err := svc.ListPipelines(context.Background(), ""); !errors.Is(err, sentinel) {
		t.Fatalf("expected propagated error, got %v", err)
	}
}

func TestValidatePipelineName(t *testing.T) {
	svc := NewService(&fakeClient{})
	for _, name := range []string{"", "   ", "a/b", "has space", "a\tb"} {
		if _, err := svc.PipelineStatus(context.Background(), name); !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("name %q: expected ErrInvalidArgument, got %v", name, err)
		}
	}
	if _, err := svc.PipelineStatus(context.Background(), "valid-pipeline_1"); err != nil {
		t.Fatalf("valid name rejected: %v", err)
	}
}

func TestPipelineHistory_NegativeOffset(t *testing.T) {
	svc := NewService(&fakeClient{})
	if _, err := svc.PipelineHistory(context.Background(), "p", -1); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for negative offset, got %v", err)
	}
}

func TestActionValidation(t *testing.T) {
	ctx := context.Background()
	svc := NewService(&fakeClient{})

	if err := svc.PausePipeline(ctx, "p", ""); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("pause without cause: expected ErrInvalidArgument, got %v", err)
	}
	if err := svc.CancelStage(ctx, "p", 0, "s", 1); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("cancel with bad counter: expected ErrInvalidArgument, got %v", err)
	}
	if err := svc.CancelStage(ctx, "p", 1, "", 1); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("cancel with empty stage: expected ErrInvalidArgument, got %v", err)
	}
	if err := svc.CommentOnPipeline(ctx, "p", 1, ""); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("comment without text: expected ErrInvalidArgument, got %v", err)
	}
	if err := svc.CommentOnPipeline(ctx, "p", 0, "hi"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("comment with bad counter: expected ErrInvalidArgument, got %v", err)
	}
}

func TestJobConsoleLog_Tail(t *testing.T) {
	svc := NewService(&fakeClient{}) // fake returns "line1\nline2\nline3\n"
	out, err := svc.JobConsoleLog(context.Background(), "p", 1, "s", 1, "j", 2)
	if err != nil {
		t.Fatalf("log: %v", err)
	}
	if out != "line2\nline3" {
		t.Fatalf("tail(2) = %q, want last two lines", out)
	}
}

func TestConfigEditValidation(t *testing.T) {
	ctx := context.Background()
	svc := NewService(&fakeClient{})
	obj := json.RawMessage(`{"name":"p"}`)

	if _, err := svc.UpdatePipelineConfig(ctx, "p", obj, ""); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("update without etag: expected ErrInvalidArgument, got %v", err)
	}
	if _, err := svc.UpdatePipelineConfig(ctx, "p", json.RawMessage(`[]`), "etag"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("update with non-object config: expected ErrInvalidArgument, got %v", err)
	}
	if err := svc.CreatePipeline(ctx, "", obj); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("create without group: expected ErrInvalidArgument, got %v", err)
	}
	if err := svc.UpdateAgent(ctx, "", obj); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("update agent without uuid: expected ErrInvalidArgument, got %v", err)
	}
}

func TestUpdateConfig_PassesETag(t *testing.T) {
	f := &fakeClient{}
	svc := NewService(f)
	newETag, err := svc.UpdatePipelineConfig(context.Background(), "p", json.RawMessage(`{"name":"p"}`), "v1-etag")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if newETag != "new-etag" {
		t.Fatalf("new etag = %q", newETag)
	}
	if len(f.calls) != 1 || f.calls[0] != "update_config:p:v1-etag" {
		t.Fatalf("calls = %v", f.calls)
	}
}

func TestCreatePipeline_WrapsBody(t *testing.T) {
	f := &fakeClient{}
	svc := NewService(f)
	if err := svc.CreatePipeline(context.Background(), "grpA", json.RawMessage(`{"name":"p"}`)); err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(f.calls) != 1 {
		t.Fatalf("calls = %v", f.calls)
	}
	// body must be {"group":"grpA","pipeline":{...}}
	want := `create:{"group":"grpA","pipeline":{"name":"p"}}`
	if f.calls[0] != want {
		t.Fatalf("create body = %q, want %q", f.calls[0], want)
	}
}

func TestTriggerPipeline_ConfirmsNewInstance(t *testing.T) {
	f := &fakeClient{counters: []int{1, 1, 2}} // baseline 1; instance #2 appears on the 2nd poll
	svc := NewService(f)
	svc.sleep = noSleep

	res, err := svc.TriggerPipeline(context.Background(), "p")
	if err != nil {
		t.Fatalf("trigger: %v", err)
	}
	if !res.Scheduled || res.Counter != 2 {
		t.Fatalf("res = %+v, want {Scheduled:true Counter:2}", res)
	}
}

func TestTriggerPipeline_AcceptedButNotConfirmed(t *testing.T) {
	f := &fakeClient{counters: []int{1}} // counter never advances past baseline
	svc := NewService(f)
	svc.sleep = noSleep

	res, err := svc.TriggerPipeline(context.Background(), "p")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Scheduled {
		t.Fatalf("res = %+v, want Scheduled:false (no instance materialized)", res)
	}
}

func TestTriggerPipeline_ConflictButMaterializes(t *testing.T) {
	// GoCD rejects the POST with a conflict, but the run still materializes: we must
	// confirm success from the counter, not treat the 409 as a failure.
	f := &fakeClient{schedErr: gocd.ErrConflict, counters: []int{0, 1}}
	svc := NewService(f)
	svc.sleep = noSleep

	res, err := svc.TriggerPipeline(context.Background(), "p")
	if err != nil {
		t.Fatalf("trigger: %v", err)
	}
	if !res.Scheduled || res.Counter != 1 {
		t.Fatalf("res = %+v, want {Scheduled:true Counter:1}", res)
	}
}

func TestTriggerPipeline_ConflictNoInstance(t *testing.T) {
	// A 409 with no new instance is NOT an error: GoCD may still schedule it, so we
	// report unconfirmed and let the caller verify rather than risk a double-run.
	f := &fakeClient{schedErr: gocd.ErrConflict, counters: []int{5}} // conflict and nothing new
	svc := NewService(f)
	svc.sleep = noSleep

	res, err := svc.TriggerPipeline(context.Background(), "p")
	if err != nil {
		t.Fatalf("409 must not be an error, got %v", err)
	}
	if res.Scheduled {
		t.Fatalf("res = %+v, want Scheduled:false (unconfirmed)", res)
	}
}

func TestTriggerPipeline_HardScheduleErrorPropagates(t *testing.T) {
	sentinel := errors.New("boom")
	f := &fakeClient{schedErr: sentinel, counters: []int{1}}
	svc := NewService(f)
	svc.sleep = noSleep

	if _, err := svc.TriggerPipeline(context.Background(), "p"); !errors.Is(err, sentinel) {
		t.Fatalf("expected propagated schedule error, got %v", err)
	}
}

func TestActionHappyPathReachesClient(t *testing.T) {
	ctx := context.Background()
	f := &fakeClient{counters: []int{0, 1}} // baseline 0, then a new instance appears
	svc := NewService(f)
	svc.sleep = noSleep

	if res, err := svc.TriggerPipeline(ctx, "p"); err != nil || !res.Scheduled {
		t.Fatalf("trigger: res=%+v err=%v", res, err)
	}
	if err := svc.PausePipeline(ctx, "p", "maint"); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if err := svc.CancelStage(ctx, "p", 2, "build", 1); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	want := []string{"schedule:p", "pause:p:maint", "cancel:p/build"}
	if len(f.calls) != len(want) {
		t.Fatalf("calls = %v, want %v", f.calls, want)
	}
	for i := range want {
		if f.calls[i] != want[i] {
			t.Fatalf("call[%d] = %q, want %q", i, f.calls[i], want[i])
		}
	}
}
