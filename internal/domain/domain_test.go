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
	schedErr      error // returned by SchedulePipeline
	histErr       error // returned by PipelineHistory
	histErrOnCall int   // 1-based call number histErr fires from; 0 = every call
	histCalls     int
	hist          [][]gocd.HistoryItem // successive PipelineHistory responses (last one repeats)
	histIdx       int
	histAfters    []string // "after" cursor received by each PipelineHistory call
	histNextAfter string   // NextAfter returned with every non-empty page

	// recorded action calls
	calls []string
}

func (f *fakeClient) Dashboard(context.Context) ([]gocd.PipelineSummary, error) {
	return f.dashboard, f.dashErr
}
func (f *fakeClient) PipelineStatus(context.Context, string) (*gocd.PipelineStatus, error) {
	return &gocd.PipelineStatus{}, nil
}
func (f *fakeClient) PipelineHistory(_ context.Context, _ string, after string) (*gocd.HistoryPage, error) {
	f.histCalls++
	f.histAfters = append(f.histAfters, after)
	if f.histErr != nil && (f.histErrOnCall == 0 || f.histCalls >= f.histErrOnCall) {
		return nil, f.histErr
	}
	if len(f.hist) == 0 {
		return &gocd.HistoryPage{}, nil
	}
	i := f.histIdx
	if i >= len(f.hist) {
		i = len(f.hist) - 1
	}
	f.histIdx++
	next := ""
	if len(f.hist[i]) > 0 {
		next = f.histNextAfter
	}
	return &gocd.HistoryPage{Runs: f.hist[i], NextAfter: next}, nil
}

// forcedRun is a history item for a manually/API-forced run, as GoCD records it.
func forcedRun(counter int, login string) gocd.HistoryItem {
	return gocd.HistoryItem{Counter: counter, TriggeredBy: login, TriggerForced: true}
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

func TestPipelineHistory_CursorValidation(t *testing.T) {
	f := &fakeClient{}
	svc := NewService(f)

	// GoCD requires the cursor to be a positive integer; garbage must be rejected
	// before any round-trip. "+7" and unicode digits are real ParseUint boundaries,
	// and the last value overflows uint64.
	for _, after := range []string{"abc", "-5", "0", "12x", " 7", "+7", "١٢", "18446744073709551616"} {
		if _, err := svc.PipelineHistory(context.Background(), "p", after); !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("after %q: expected ErrInvalidArgument, got %v", after, err)
		}
	}
	if f.histCalls != 0 {
		t.Fatalf("invalid cursors must not reach the client, got %d calls", f.histCalls)
	}

	// The accept side of the boundary: leading zeros are a valid positive integer
	// and must be forwarded verbatim, not normalized.
	if _, err := svc.PipelineHistory(context.Background(), "p", "007"); err != nil {
		t.Fatalf("after \"007\": %v", err)
	}
	if len(f.histAfters) != 1 || f.histAfters[0] != "007" {
		t.Fatalf("cursor forwarded = %v, want [007]", f.histAfters)
	}
}

func TestPipelineHistory_PassesCursorAndReturnsNext(t *testing.T) {
	f := &fakeClient{
		hist:          [][]gocd.HistoryItem{{forcedRun(5, "alice")}},
		histNextAfter: "37957",
	}
	svc := NewService(f)

	page, err := svc.PipelineHistory(context.Background(), "p", "12345")
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(f.histAfters) != 1 || f.histAfters[0] != "12345" {
		t.Fatalf("cursor passed to client = %v, want [12345]", f.histAfters)
	}
	if page.NextAfter != "37957" {
		t.Fatalf("NextAfter = %q, want 37957", page.NextAfter)
	}

	// Empty cursor (first page) is valid and passed through as-is.
	if _, err := svc.PipelineHistory(context.Background(), "p", ""); err != nil {
		t.Fatalf("first page: %v", err)
	}
	if f.histAfters[1] != "" {
		t.Fatalf("first-page cursor = %q, want empty", f.histAfters[1])
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
	f := &fakeClient{hist: [][]gocd.HistoryItem{
		{forcedRun(1, "alice")},                        // baseline 1
		{forcedRun(1, "alice")},                        // first poll: nothing new
		{forcedRun(2, "alice"), forcedRun(1, "alice")}, // instance #2 appears on the 2nd poll
	}}
	svc := NewService(f)
	svc.sleep = noSleep

	res, err := svc.TriggerPipeline(context.Background(), "p", "alice")
	if err != nil {
		t.Fatalf("trigger: %v", err)
	}
	if !res.Scheduled || res.Counter != 2 {
		t.Fatalf("res = %+v, want {Scheduled:true Counter:2}", res)
	}
	for i, after := range f.histAfters {
		if after != "" {
			t.Fatalf("trigger polling must always read the first history page, call %d used cursor %q", i+1, after)
		}
	}
}

func TestTriggerPipeline_AcceptedButNotConfirmed(t *testing.T) {
	f := &fakeClient{hist: [][]gocd.HistoryItem{{forcedRun(1, "alice")}}} // nothing new ever appears
	svc := NewService(f)
	svc.sleep = noSleep

	res, err := svc.TriggerPipeline(context.Background(), "p", "alice")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Scheduled {
		t.Fatalf("res = %+v, want Scheduled:false (no instance materialized)", res)
	}
}

func TestTriggerPipeline_ForeignRunDoesNotConfirm(t *testing.T) {
	// The core attribution scenario: the counter advances inside the wait window, but
	// the new run is not attributable to us — a forced run by another user, a timer,
	// a material change, or a non-forced run under our own login. None of them may
	// confirm this trigger.
	for _, tc := range []struct {
		name    string
		foreign gocd.HistoryItem
	}{
		{"forced by another user", forcedRun(42, "bob")},
		{"timer", gocd.HistoryItem{Counter: 42, TriggeredBy: "timer"}},
		{"material change", gocd.HistoryItem{Counter: 42, TriggeredBy: "changes"}},
		{"same user but not forced", gocd.HistoryItem{Counter: 42, TriggeredBy: "alice"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeClient{hist: [][]gocd.HistoryItem{
				{forcedRun(41, "bob")},             // baseline 41
				{tc.foreign, forcedRun(41, "bob")}, // counter grows, but the run is foreign
			}}
			svc := NewService(f)
			svc.sleep = noSleep

			res, err := svc.TriggerPipeline(context.Background(), "p", "alice")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.Scheduled {
				t.Fatalf("res = %+v, want Scheduled:false (foreign run must not confirm)", res)
			}
		})
	}
}

func TestTriggerPipeline_ForeignThenOwnRun(t *testing.T) {
	// A foreign run lands first, ours materializes later in the window: the result
	// must be OUR instance, not the foreign one that advanced the counter.
	f := &fakeClient{hist: [][]gocd.HistoryItem{
		{forcedRun(41, "alice")},                       // baseline 41
		{forcedRun(42, "bob"), forcedRun(41, "alice")}, // foreign run advances the counter
		{forcedRun(43, "alice"), forcedRun(42, "bob"), forcedRun(41, "alice")},
	}}
	svc := NewService(f)
	svc.sleep = noSleep

	res, err := svc.TriggerPipeline(context.Background(), "p", "alice")
	if err != nil {
		t.Fatalf("trigger: %v", err)
	}
	if !res.Scheduled || res.Counter != 43 {
		t.Fatalf("res = %+v, want {Scheduled:true Counter:43} (the attributed run)", res)
	}
}

func TestTriggerPipeline_PicksEarliestOwnRun(t *testing.T) {
	// Two same-user runs inside the window (concurrent triggers): attribution cannot
	// tell them apart, so we deterministically report the earliest new one.
	f := &fakeClient{hist: [][]gocd.HistoryItem{
		{forcedRun(41, "alice")},
		{forcedRun(43, "alice"), forcedRun(42, "alice"), forcedRun(41, "alice")},
	}}
	svc := NewService(f)
	svc.sleep = noSleep

	res, err := svc.TriggerPipeline(context.Background(), "p", "alice")
	if err != nil {
		t.Fatalf("trigger: %v", err)
	}
	if !res.Scheduled || res.Counter != 42 {
		t.Fatalf("res = %+v, want {Scheduled:true Counter:42} (earliest new own run)", res)
	}
}

func TestTriggerPipeline_EmptyLoginRejected(t *testing.T) {
	f := &fakeClient{}
	svc := NewService(f)
	svc.sleep = noSleep

	if _, err := svc.TriggerPipeline(context.Background(), "p", "  "); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for empty login, got %v", err)
	}
	if len(f.calls) != 0 {
		t.Fatalf("SchedulePipeline must not be called without a login: %v", f.calls)
	}
	if f.histCalls != 0 {
		t.Fatalf("the login guard must fire before the baseline read, got %d history calls", f.histCalls)
	}
}

func TestTriggerPipeline_ConflictButMaterializes(t *testing.T) {
	// GoCD rejects the POST with a conflict, but the run still materializes: we must
	// confirm success from the attributed instance, not treat the 409 as a failure.
	f := &fakeClient{schedErr: gocd.ErrConflict, hist: [][]gocd.HistoryItem{
		{},                      // baseline: pipeline has never run
		{forcedRun(1, "alice")}, // the run materializes despite the 409
	}}
	svc := NewService(f)
	svc.sleep = noSleep

	res, err := svc.TriggerPipeline(context.Background(), "p", "alice")
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
	f := &fakeClient{schedErr: gocd.ErrConflict, hist: [][]gocd.HistoryItem{{forcedRun(5, "alice")}}}
	svc := NewService(f)
	svc.sleep = noSleep

	res, err := svc.TriggerPipeline(context.Background(), "p", "alice")
	if err != nil {
		t.Fatalf("409 must not be an error, got %v", err)
	}
	if res.Scheduled {
		t.Fatalf("res = %+v, want Scheduled:false (unconfirmed)", res)
	}
}

func TestTriggerPipeline_PollFailureDegradesToUnconfirmed(t *testing.T) {
	// The schedule was accepted; a history failure during confirmation must NOT become
	// a tool error (an error invites a retry, and a retry can double-run the pipeline).
	sentinel := errors.New("gocd went away")
	f := &fakeClient{
		hist:          [][]gocd.HistoryItem{{forcedRun(3, "alice")}},
		histErr:       sentinel,
		histErrOnCall: 2, // baseline ok, polls fail
	}
	svc := NewService(f)
	svc.sleep = noSleep

	res, err := svc.TriggerPipeline(context.Background(), "p", "alice")
	if err != nil {
		t.Fatalf("post-accept poll failure must degrade to unconfirmed, got error %v", err)
	}
	if res.Scheduled {
		t.Fatalf("res = %+v, want Scheduled:false", res)
	}
}

func TestTriggerPipeline_CancelDuringWaitDegradesToUnconfirmed(t *testing.T) {
	// A cancelled request (client disconnect / MCP timeout) after the schedule was
	// accepted must also degrade to unconfirmed, not to an error.
	f := &fakeClient{hist: [][]gocd.HistoryItem{{forcedRun(3, "alice")}}}
	svc := NewService(f)
	svc.sleep = func(context.Context, time.Duration) error { return context.Canceled }

	res, err := svc.TriggerPipeline(context.Background(), "p", "alice")
	if err != nil {
		t.Fatalf("cancel during wait must degrade to unconfirmed, got error %v", err)
	}
	if res.Scheduled {
		t.Fatalf("res = %+v, want Scheduled:false", res)
	}
}

func TestTriggerPipeline_BaselineErrorStillFails(t *testing.T) {
	// Before the schedule request nothing has been triggered, so a baseline history
	// failure is still a genuine error.
	sentinel := errors.New("boom")
	f := &fakeClient{histErr: sentinel} // fails from the first (baseline) call
	svc := NewService(f)
	svc.sleep = noSleep

	if _, err := svc.TriggerPipeline(context.Background(), "p", "alice"); !errors.Is(err, sentinel) {
		t.Fatalf("expected baseline error to propagate, got %v", err)
	}
	if len(f.calls) != 0 {
		t.Fatalf("SchedulePipeline must not be called after a failed baseline read: %v", f.calls)
	}
}

func TestTriggerPipeline_HardScheduleErrorPropagates(t *testing.T) {
	sentinel := errors.New("boom")
	f := &fakeClient{schedErr: sentinel, hist: [][]gocd.HistoryItem{{forcedRun(1, "alice")}}}
	svc := NewService(f)
	svc.sleep = noSleep

	if _, err := svc.TriggerPipeline(context.Background(), "p", "alice"); !errors.Is(err, sentinel) {
		t.Fatalf("expected propagated schedule error, got %v", err)
	}
}

func TestActionHappyPathReachesClient(t *testing.T) {
	ctx := context.Background()
	f := &fakeClient{hist: [][]gocd.HistoryItem{
		{}, // baseline: no runs yet
		{forcedRun(1, "alice")},
	}}
	svc := NewService(f)
	svc.sleep = noSleep

	if res, err := svc.TriggerPipeline(ctx, "p", "alice"); err != nil || !res.Scheduled {
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
