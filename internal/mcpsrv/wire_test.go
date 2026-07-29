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
			// A new instance (#2) materializes only after the schedule POST.
			counter := 1
			if triggered.Load() {
				counter = 2
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, fmt.Sprintf(`{"pipelines":[{"counter":%d,"label":"%d","stages":[]}]}`, counter, counter))
		case "/go/api/admin/pipelines/stale":
			w.WriteHeader(http.StatusPreconditionFailed) // simulate ETag conflict
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
