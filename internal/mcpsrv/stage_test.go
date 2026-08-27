package mcpsrv_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestTriggerStage_EndToEnd(t *testing.T) {
	gocd := fakeGoCD(t)
	defer gocd.Close()

	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	ts := stackCfg(t, gocd.URL, "actions", log)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	sess, err := connect(ctx, ts.URL+"/mcp", goodToken)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer sess.Close()

	// The instance exposes the stage-run fields the tool relies on, with the
	// pending manual stage reported as not scheduled.
	res, err := sess.CallTool(ctx, &mcp.CallToolParams{Name: "get_pipeline_instance", Arguments: map[string]any{"name": "tp", "counter": 1}})
	if err != nil || res.IsError {
		t.Fatalf("get_pipeline_instance: err=%v res=%s", err, resultText(res))
	}
	var inst struct {
		Stages []struct {
			Name       string `json:"name"`
			Counter    int    `json:"counter"`
			Scheduled  bool   `json:"scheduled"`
			ApprovedBy string `json:"approved_by"`
			CanRun     bool   `json:"can_run"`
		} `json:"stages"`
	}
	raw, _ := json.Marshal(res.StructuredContent)
	if err := json.Unmarshal(raw, &inst); err != nil {
		t.Fatalf("decode instance %s: %v", raw, err)
	}
	if len(inst.Stages) != 2 || inst.Stages[1].Name != "deploy" || inst.Stages[1].Scheduled || inst.Stages[1].Counter != 1 || !inst.Stages[1].CanRun {
		t.Fatalf("stages = %+v, want pending manual deploy stage (counter 1, not scheduled, can run)", inst.Stages)
	}
	if b := inst.Stages[0]; !b.Scheduled || b.ApprovedBy != "changes" {
		t.Fatalf("build stage = %+v, want scheduled by changes", b)
	}

	// Running the manual stage is confirmed once the instance shows it scheduled and
	// approved by the caller (the fake flips after the POST).
	res, err = sess.CallTool(ctx, &mcp.CallToolParams{Name: "trigger_stage", Arguments: map[string]any{"pipeline": "tp", "pipeline_counter": 1, "stage": "deploy"}})
	if err != nil || res.IsError {
		t.Fatalf("trigger_stage: err=%v res=%s", err, resultText(res))
	}
	var out struct {
		OK     bool   `json:"ok"`
		Detail string `json:"detail"`
	}
	raw, _ = json.Marshal(res.StructuredContent)
	_ = json.Unmarshal(raw, &out)
	if !out.OK || !strings.Contains(out.Detail, "stage counter 1") {
		t.Fatalf("trigger_stage = %+v, want ok with stage counter 1", out)
	}

	// GoCD's refusal (409) surfaces at once with its reason, not as the ETag hint
	// and not as an unconfirmed result.
	res, err = sess.CallTool(ctx, &mcp.CallToolParams{Name: "trigger_stage", Arguments: map[string]any{"pipeline": "tp", "pipeline_counter": 1, "stage": "build"}})
	if err != nil || !res.IsError || !strings.Contains(resultText(res), "GoCD refused: Cannot schedule") || strings.Contains(resultText(res), "ETag") {
		t.Fatalf("trigger_stage refused: err=%v isError=%v text=%q", err, res.IsError, resultText(res))
	}

	// An unknown stage is rejected before anything is run.
	res, err = sess.CallTool(ctx, &mcp.CallToolParams{Name: "trigger_stage", Arguments: map[string]any{"pipeline": "tp", "pipeline_counter": 1, "stage": "nope"}})
	if err != nil || !res.IsError || !strings.Contains(resultText(res), `no stage "nope"`) {
		t.Fatalf("trigger_stage unknown stage: err=%v isError=%v text=%q", err, res.IsError, resultText(res))
	}

	logs := buf.String()
	if !strings.Contains(logs, `"action":"trigger_stage"`) || !strings.Contains(logs, `"target":"tp/1/deploy"`) {
		t.Fatalf("audit log missing trigger_stage: %s", logs)
	}

	// Gated with the other actions: absent from the read-only toolset.
	ro := stackCfg(t, gocd.URL, "readonly", nil)
	defer ro.Close()
	roSess, err := connect(ctx, ro.URL+"/mcp", goodToken)
	if err != nil {
		t.Fatalf("connect readonly: %v", err)
	}
	defer roSess.Close()
	tools, err := roSess.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	for _, tool := range tools.Tools {
		if tool.Name == "trigger_stage" {
			t.Fatalf("readonly toolset must not expose trigger_stage")
		}
	}
}
