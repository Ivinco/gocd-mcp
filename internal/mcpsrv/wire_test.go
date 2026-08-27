package mcpsrv_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ivinco/gocd-mcp/internal/auth"
	"github.com/ivinco/gocd-mcp/internal/config"
	"github.com/ivinco/gocd-mcp/internal/httpx"
	"github.com/ivinco/gocd-mcp/internal/mcpsrv"
	"github.com/ivinco/gocd-mcp/internal/obs"
)

const goodToken = "goodtoken"

// fakeGoCD stands in for the GoCD server: it accepts goodToken at current_user.
func fakeGoCD(t *testing.T) *httptest.Server {
	t.Helper()
	var triggered atomic.Bool // flips once pipeline "tp" is scheduled, so history advances
	var stageRun atomic.Bool  // flips once tp/1/deploy is run, so the instance shows it scheduled
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+goodToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/go/api/current_user":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"login_name":"alice","enabled":true}`)
		case "/go/api/dashboard":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"_embedded":{"pipeline_groups":[{"name":"g1","pipelines":["p1"]}],"pipelines":[{"name":"p1","pause_info":{"paused":false},"_embedded":{"instances":[]}}]}}`)
		case "/go/api/pipelines/p1/pause":
			w.WriteHeader(http.StatusOK)
		case "/go/api/pipelines/denied/pause":
			w.WriteHeader(http.StatusForbidden) // simulate GoCD RBAC denial
		case "/go/api/pipelines/tp/schedule":
			triggered.Store(true)
			w.WriteHeader(http.StatusAccepted) // GoCD accepts asynchronously (202)
		case "/go/api/pipelines/tp/history":
			w.Header().Set("Content-Type", "application/json")
			// Cursor pagination: the page at cursor 42 is the terminal (oldest) one.
			if r.URL.Query().Get("after") == "42" {
				_, _ = io.WriteString(w, `{"pipelines":[{"counter":1,"label":"1","build_cause":{"approver":"bob","trigger_forced":true},"stages":[]}]}`)
				return
			}
			// A new instance (#2), forced by the authenticated user, materializes only
			// after the schedule POST — confirmation requires that attribution.
			counter := 1
			if triggered.Load() {
				counter = 2
			}
			_, _ = io.WriteString(w, fmt.Sprintf(`{"_links":{"next":{"href":"http://gocd.example/go/api/pipelines/tp/history?after=42"}},"pipelines":[{"counter":%d,"label":"%d","build_cause":{"approver":"alice","trigger_forced":true},"stages":[]}]}`, counter, counter))
		case "/go/api/stages/tp/1/deploy/run":
			stageRun.Store(true)
			w.WriteHeader(http.StatusAccepted)
		case "/go/api/stages/tp/1/build/run":
			// GoCD's synchronous refusal while a stage of the run is active.
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_, _ = io.WriteString(w, `{"message":"Cannot schedule: Pipeline[name='tp', counter='2', label='2'] is still in progress"}`)
		case "/go/api/pipelines/tp/1":
			// Stage counters are strings in this API. "deploy" is a manual stage: pending
			// (counter "1", scheduled false) until run, then scheduled and approved by the
			// authenticated user.
			w.Header().Set("Content-Type", "application/json")
			deploy := `{"name":"deploy","counter":"1","scheduled":false,"approval_type":null,"approved_by":null,"can_run":true,"status":"Unknown","result":null,"jobs":[]}`
			if stageRun.Load() {
				deploy = `{"name":"deploy","counter":"1","scheduled":true,"approval_type":"manual","approved_by":"alice","can_run":false,"status":"Building","result":"Unknown","jobs":[]}`
			}
			_, _ = io.WriteString(w, `{"name":"tp","counter":1,"label":"1","stages":[{"name":"build","counter":"1","scheduled":true,"approval_type":"success","approved_by":"changes","can_run":true,"status":"Passed","result":"Passed","jobs":[]},`+deploy+`]}`)
		case "/go/api/admin/pipelines/stale":
			w.WriteHeader(http.StatusPreconditionFailed) // simulate ETag conflict
		case "/go/api/admin/templates":
			if r.Method == http.MethodPost {
				w.WriteHeader(http.StatusOK)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"_embedded":{"templates":[{"name":"t1","can_edit":true,"can_administer":true,"_embedded":{"pipelines":[{"name":"p1"}]}},{"name":"t2","can_edit":true,"can_administer":true,"_embedded":{"pipelines":[]}}]}}`)
		case "/go/api/admin/templates/t1":
			switch r.Method {
			case http.MethodGet:
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("ETag", `"tpl-etag-1"`)
				_, _ = io.WriteString(w, `{"name":"t1","stages":[{"name":"build"}]}`)
			case http.MethodPut:
				if r.Header.Get("If-Match") != `"tpl-etag-1"` {
					w.WriteHeader(http.StatusPreconditionFailed)
					return
				}
				w.Header().Set("ETag", `"tpl-etag-2"`)
				w.WriteHeader(http.StatusOK)
			case http.MethodDelete:
				// Still used by p1: GoCD refuses and names the pipelines.
				w.WriteHeader(http.StatusUnprocessableEntity)
				_, _ = io.WriteString(w, `{"message":"Validations failed for template with name 't1'. Error(s): [The template 't1' is being referenced by pipeline(s): [p1]]. Please correct and resubmit."}`)
			}
		case "/go/api/admin/templates/t2":
			w.WriteHeader(http.StatusOK) // DELETE of an unused template
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

// stack builds the full MCP HTTP stack (full toolset) pointed at the fake GoCD.
func stack(t *testing.T, gocdURL string) *httptest.Server {
	return stackCfg(t, gocdURL, config.ToolsetFull, nil)
}

// stackCfg builds the stack with an explicit toolset and optional logger.
func stackCfg(t *testing.T, gocdURL string, toolset config.Toolset, log *slog.Logger) *httptest.Server {
	t.Helper()
	cfg := &config.Config{
		GoCDBaseURL:     gocdURL,
		MCPEndpointPath: "/mcp",
		GoCDTimeout:     5 * time.Second,
		TokenCacheTTL:   time.Second,
		Toolset:         toolset,
		LogLevel:        "error",
	}
	if log == nil {
		log, _, _ = obs.NewLogger(cfg.LogLevel, "")
	}
	verifier := auth.NewVerifier(cfg.GoCDBaseURL, cfg.GoCDTimeout, cfg.TokenCacheTTL)
	srv := mcpsrv.New(cfg, log)
	mcpHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, nil)
	return httptest.NewServer(httpx.NewServer(cfg, log, mcpHandler, verifier).Handler)
}

// authRoundTripper injects a bearer token into outgoing requests.
type authRoundTripper struct {
	token string
	base  http.RoundTripper
}

func (a authRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	if a.token != "" {
		r.Header.Set("Authorization", "Bearer "+a.token)
	}
	return a.base.RoundTrip(r)
}

func connect(ctx context.Context, endpoint, token string) (*mcp.ClientSession, error) {
	hc := &http.Client{Transport: authRoundTripper{token: token, base: http.DefaultTransport}}
	// DisableStandaloneSSE: tool calls use POST request/response; the long-lived GET
	// SSE stream is unnecessary for this test and would block Connect.
	tr := &mcp.StreamableClientTransport{Endpoint: endpoint, HTTPClient: hc, MaxRetries: -1, DisableStandaloneSSE: true}
	c := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	return c.Connect(ctx, tr, nil)
}

func TestWhoami_ValidToken(t *testing.T) {
	gocd := fakeGoCD(t)
	defer gocd.Close()
	ts := stack(t, gocd.URL)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sess, err := connect(ctx, ts.URL+"/mcp", goodToken)
	if err != nil {
		t.Fatalf("connect with valid token: %v", err)
	}
	defer sess.Close()

	res, err := sess.CallTool(ctx, &mcp.CallToolParams{Name: "whoami"})
	if err != nil {
		t.Fatalf("call whoami: %v", err)
	}
	if res.IsError {
		t.Fatalf("whoami returned tool error: %+v", res.Content)
	}

	var out struct {
		Login string `json:"login"`
	}
	raw, _ := json.Marshal(res.StructuredContent)
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode structured output %q: %v", raw, err)
	}
	if out.Login != "alice" {
		t.Fatalf("login = %q, want alice", out.Login)
	}
}

func TestListPipelines_EndToEnd(t *testing.T) {
	gocd := fakeGoCD(t)
	defer gocd.Close()
	ts := stack(t, gocd.URL)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sess, err := connect(ctx, ts.URL+"/mcp", goodToken)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer sess.Close()

	res, err := sess.CallTool(ctx, &mcp.CallToolParams{Name: "list_pipelines"})
	if err != nil {
		t.Fatalf("call list_pipelines: %v", err)
	}
	if res.IsError {
		t.Fatalf("list_pipelines tool error: %+v", res.Content)
	}

	var out struct {
		Pipelines []struct {
			Group string `json:"group"`
			Name  string `json:"name"`
		} `json:"pipelines"`
	}
	raw, _ := json.Marshal(res.StructuredContent)
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode output %q: %v", raw, err)
	}
	if len(out.Pipelines) != 1 || out.Pipelines[0].Name != "p1" || out.Pipelines[0].Group != "g1" {
		t.Fatalf("unexpected pipelines: %+v", out.Pipelines)
	}
}

func TestActionToolset_Gating(t *testing.T) {
	gocd := fakeGoCD(t)
	defer gocd.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	hasTrigger := func(srv *httptest.Server) bool {
		sess, err := connect(ctx, srv.URL+"/mcp", goodToken)
		if err != nil {
			t.Fatalf("connect: %v", err)
		}
		defer sess.Close()
		res, err := sess.ListTools(ctx, &mcp.ListToolsParams{})
		if err != nil {
			t.Fatalf("list tools: %v", err)
		}
		for _, tool := range res.Tools {
			if tool.Name == "trigger_pipeline" {
				return true
			}
		}
		return false
	}

	ro := stackCfg(t, gocd.URL, "readonly", nil)
	defer ro.Close()
	if hasTrigger(ro) {
		t.Fatalf("readonly toolset must not expose trigger_pipeline")
	}

	full := stackCfg(t, gocd.URL, "full", nil)
	defer full.Close()
	if !hasTrigger(full) {
		t.Fatalf("full toolset must expose trigger_pipeline")
	}
}

func TestPause_EndToEnd_Audit(t *testing.T) {
	gocd := fakeGoCD(t)
	defer gocd.Close()

	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	ts := stackCfg(t, gocd.URL, "full", log)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sess, err := connect(ctx, ts.URL+"/mcp", goodToken)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer sess.Close()

	res, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name:      "pause_pipeline",
		Arguments: map[string]any{"name": "p1", "cause": "maintenance"},
	})
	if err != nil {
		t.Fatalf("pause call: %v", err)
	}
	if res.IsError {
		t.Fatalf("pause returned tool error: %+v", res.Content)
	}
	logs := buf.String()
	if !strings.Contains(logs, "audit") || !strings.Contains(logs, "pause_pipeline") || !strings.Contains(logs, "alice") {
		t.Fatalf("audit log missing expected fields: %s", logs)
	}
	if strings.Contains(logs, goodToken) {
		t.Fatalf("audit log leaked the token")
	}
}

func TestTrigger_EndToEnd_ConfirmsInstance(t *testing.T) {
	gocd := fakeGoCD(t)
	defer gocd.Close()
	ts := stack(t, gocd.URL)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sess, err := connect(ctx, ts.URL+"/mcp", goodToken)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer sess.Close()

	res, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name:      "trigger_pipeline",
		Arguments: map[string]any{"name": "tp"},
	})
	if err != nil {
		t.Fatalf("trigger call: %v", err)
	}
	if res.IsError {
		t.Fatalf("trigger returned tool error: %+v", res.Content)
	}

	var out struct {
		OK     bool   `json:"ok"`
		Detail string `json:"detail"`
	}
	raw, _ := json.Marshal(res.StructuredContent)
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode output %q: %v", raw, err)
	}
	if !out.OK || !strings.Contains(out.Detail, "instance #2") {
		t.Fatalf("trigger result = %+v, want ok with confirmed instance #2", out)
	}
}

func TestPipelineHistory_EndToEnd_ExposesBuildCause(t *testing.T) {
	// The CHANGELOG promises triggered_by/trigger_forced in history output; the JSON
	// tags on gocd.HistoryItem are the only thing producing them, so pin the
	// serialized field names end to end.
	gocd := fakeGoCD(t)
	defer gocd.Close()
	ts := stack(t, gocd.URL)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sess, err := connect(ctx, ts.URL+"/mcp", goodToken)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer sess.Close()

	res, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_pipeline_history",
		Arguments: map[string]any{"name": "tp"},
	})
	if err != nil {
		t.Fatalf("history call: %v", err)
	}
	if res.IsError {
		t.Fatalf("history returned tool error: %+v", res.Content)
	}

	var out struct {
		Runs []struct {
			Counter       int    `json:"counter"`
			TriggeredBy   string `json:"triggered_by"`
			TriggerForced bool   `json:"trigger_forced"`
		} `json:"runs"`
		NextAfter string `json:"next_after"`
	}
	raw, _ := json.Marshal(res.StructuredContent)
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode output %q: %v", raw, err)
	}
	if len(out.Runs) != 1 || out.Runs[0].TriggeredBy != "alice" || !out.Runs[0].TriggerForced {
		t.Fatalf("runs = %+v, want one run triggered_by alice with trigger_forced true", out.Runs)
	}
	if out.NextAfter != "42" {
		t.Fatalf("next_after = %q, want 42 (pagination cursor from the fake's next link)", out.NextAfter)
	}

	// Passing the cursor back as `after` must reach GoCD's query string and fetch the
	// older page — this is the tool-boundary round-trip that the old offset parameter
	// silently failed.
	res, err = sess.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_pipeline_history",
		Arguments: map[string]any{"name": "tp", "after": out.NextAfter},
	})
	if err != nil {
		t.Fatalf("history page 2 call: %v", err)
	}
	if res.IsError {
		t.Fatalf("history page 2 returned tool error: %+v", res.Content)
	}
	raw, _ = json.Marshal(res.StructuredContent)
	out.Runs, out.NextAfter = nil, ""
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode page 2 %q: %v", raw, err)
	}
	if len(out.Runs) != 1 || out.Runs[0].Counter != 1 || out.Runs[0].TriggeredBy != "bob" {
		t.Fatalf("page 2 runs = %+v, want the terminal page (counter 1, triggered_by bob)", out.Runs)
	}
	if out.NextAfter != "" {
		t.Fatalf("page 2 next_after = %q, want empty (last page)", out.NextAfter)
	}

	// The legacy offset parameter is gone from the schema; sending it must fail
	// loudly, not silently return the first page as it did against real GoCD.
	res, err = sess.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_pipeline_history",
		Arguments: map[string]any{"name": "tp", "offset": 1},
	})
	if err == nil && !res.IsError {
		t.Fatalf("legacy offset argument must not silently succeed")
	}
}

func TestPipelineHistoryResource_EndToEnd(t *testing.T) {
	// The history resource serves the first (newest) page as a bare runs array.
	gocd := fakeGoCD(t)
	defer gocd.Close()
	ts := stack(t, gocd.URL)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sess, err := connect(ctx, ts.URL+"/mcp", goodToken)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer sess.Close()

	res, err := sess.ReadResource(ctx, &mcp.ReadResourceParams{URI: "gocd://pipeline/tp/history"})
	if err != nil {
		t.Fatalf("read history resource: %v", err)
	}
	if len(res.Contents) != 1 || res.Contents[0].MIMEType != "application/json" {
		t.Fatalf("contents = %+v, want one application/json entry", res.Contents)
	}

	var runs []struct {
		Counter     int    `json:"counter"`
		TriggeredBy string `json:"triggered_by"`
	}
	if err := json.Unmarshal([]byte(res.Contents[0].Text), &runs); err != nil {
		t.Fatalf("decode resource %q: %v", res.Contents[0].Text, err)
	}
	if len(runs) != 1 || runs[0].Counter != 1 || runs[0].TriggeredBy != "alice" {
		t.Fatalf("runs = %+v, want the first page (counter 1, triggered_by alice)", runs)
	}
}

func TestForbidden_MapsToToolError(t *testing.T) {
	gocd := fakeGoCD(t)
	defer gocd.Close()
	ts := stack(t, gocd.URL)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sess, err := connect(ctx, ts.URL+"/mcp", goodToken)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer sess.Close()

	res, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name:      "pause_pipeline",
		Arguments: map[string]any{"name": "denied", "cause": "x"},
	})
	if err != nil {
		t.Fatalf("transport error (should be a tool error, not protocol): %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected IsError for forbidden GoCD response")
	}
}

func TestConfigToolset_Gating(t *testing.T) {
	gocd := fakeGoCD(t)
	defer gocd.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	hasTool := func(srv *httptest.Server, name string) bool {
		sess, err := connect(ctx, srv.URL+"/mcp", goodToken)
		if err != nil {
			t.Fatalf("connect: %v", err)
		}
		defer sess.Close()
		res, err := sess.ListTools(ctx, &mcp.ListToolsParams{})
		if err != nil {
			t.Fatalf("list tools: %v", err)
		}
		for _, tool := range res.Tools {
			if tool.Name == name {
				return true
			}
		}
		return false
	}

	actions := stackCfg(t, gocd.URL, "actions", nil)
	defer actions.Close()
	if hasTool(actions, "update_pipeline_config") {
		t.Fatalf("actions toolset must not expose update_pipeline_config")
	}

	full := stackCfg(t, gocd.URL, "full", nil)
	defer full.Close()
	if !hasTool(full, "update_pipeline_config") {
		t.Fatalf("full toolset must expose update_pipeline_config")
	}

	// Template tools follow the same tiers: reads everywhere, writes only in full.
	readonly := stackCfg(t, gocd.URL, "readonly", nil)
	defer readonly.Close()
	for _, name := range []string{"list_templates", "get_template"} {
		if !hasTool(readonly, name) {
			t.Fatalf("readonly toolset must expose %s", name)
		}
	}
	for _, name := range []string{"create_template", "update_template", "delete_template"} {
		if hasTool(actions, name) {
			t.Fatalf("actions toolset must not expose %s", name)
		}
		if !hasTool(full, name) {
			t.Fatalf("full toolset must expose %s", name)
		}
	}
}

func TestUpdateConfig_ETagConflict(t *testing.T) {
	gocd := fakeGoCD(t)
	defer gocd.Close()
	ts := stack(t, gocd.URL)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sess, err := connect(ctx, ts.URL+"/mcp", goodToken)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer sess.Close()

	res, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name: "update_pipeline_config",
		Arguments: map[string]any{
			"name":   "stale",
			"etag":   "old-etag",
			"config": map[string]any{"name": "stale"},
		},
	})
	if err != nil {
		t.Fatalf("transport error (should be tool error): %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected IsError on ETag conflict")
	}
	var text string
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			text += tc.Text
		}
	}
	if !strings.Contains(text, "conflict") {
		t.Fatalf("expected conflict message, got %q", text)
	}
}

func TestWhoami_BadAndMissingToken(t *testing.T) {
	gocd := fakeGoCD(t)
	defer gocd.Close()
	ts := stack(t, gocd.URL)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for _, tc := range []struct{ name, token string }{
		{"bad token", "wrongtoken"},
		{"missing token", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sess, err := connect(ctx, ts.URL+"/mcp", tc.token)
			if err == nil {
				sess.Close()
				t.Fatalf("expected connect to fail for %s", tc.name)
			}
		})
	}
}

// TestUnauthenticated401 checks the raw transport-level 401 for an unauthenticated
// request. Under the PAT identity model there is no OAuth authorization server, so we
// do not emit RFC 9728 discovery params in WWW-Authenticate (see architecture.md §2).
func TestUnauthenticated401(t *testing.T) {
	gocd := fakeGoCD(t)
	defer gocd.Close()
	ts := stack(t, gocd.URL)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestHealthz(t *testing.T) {
	gocd := fakeGoCD(t)
	defer gocd.Close()
	ts := stack(t, gocd.URL)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("healthz: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz status = %d, want 200", resp.StatusCode)
	}
}
