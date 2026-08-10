package gocd_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ivinco/gocd-mcp/internal/gocd"
)

const dashboardJSON = `{"_embedded":{
  "pipeline_groups":[{"name":"grpA","pipelines":["p1"]},{"name":"grpB","pipelines":["p2"]}],
  "pipelines":[
    {"name":"p1","locked":false,"from_config_repo":true,"pause_info":{"paused":true,"pause_reason":"maint"},
     "_embedded":{"instances":[
        {"counter":1,"label":"1","_embedded":{"stages":[{"name":"build","status":"Passed"}]}},
        {"counter":2,"label":"2","_embedded":{"stages":[{"name":"build","status":"Failed"}]}}
     ]}},
    {"name":"p2","locked":true,"from_config_repo":false,"pause_info":{"paused":false},
     "_embedded":{"instances":[]}}
  ]
}}`

func newServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/go/api/dashboard", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(dashboardJSON))
	})
	mux.HandleFunc("/go/api/pipelines/p1/status", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"paused":true,"paused_cause":"x","paused_by":"u","locked":false,"schedulable":true}`))
	})
	mux.HandleFunc("/go/api/pipelines/p1/history", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"pipelines":[{"counter":5,"label":"5","scheduled_date":123,"comment":"c","build_cause":{"approver":"u1","trigger_forced":true,"trigger_message":"Forced by u1"},"stages":[{"name":"build","status":"Passed"}]}]}`))
	})
	mux.HandleFunc("/go/api/agents", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"_embedded":{"agents":[{"uuid":"u1","hostname":"h","ip_address":"1.2.3.4","agent_config_state":"Enabled","agent_state":"Idle","build_state":"Idle","operating_system":"Linux","resources":["docker"],"environments":[{"name":"E1"},{"name":"E2"}]}]}}`))
	})
	mux.HandleFunc("/go/api/admin/pipelines/p1", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("ETag", `"abc123"`)
		_, _ = w.Write([]byte(`{"name":"p1","group":"grpA"}`))
	})
	// error-code endpoints
	mux.HandleFunc("/go/api/pipelines/missing/status", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	mux.HandleFunc("/go/api/pipelines/denied/status", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})
	mux.HandleFunc("/go/api/admin/pipelines/stale", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusPreconditionFailed)
	})
	return httptest.NewServer(mux)
}

func newClient(url string) *gocd.Client {
	return gocd.NewClient(url, "tok", 5*time.Second)
}

func TestDashboard(t *testing.T) {
	srv := newServer(t)
	defer srv.Close()
	pls, err := newClient(srv.URL).Dashboard(context.Background())
	if err != nil {
		t.Fatalf("dashboard: %v", err)
	}
	if len(pls) != 2 {
		t.Fatalf("got %d pipelines, want 2", len(pls))
	}
	byName := map[string]gocd.PipelineSummary{}
	for _, p := range pls {
		byName[p.Name] = p
	}
	p1 := byName["p1"]
	if p1.Group != "grpA" || !p1.Paused || p1.PauseReason != "maint" || !p1.FromConfigRepo {
		t.Fatalf("p1 mapped wrong: %+v", p1)
	}
	if p1.LastRun == nil || p1.LastRun.Counter != 2 || len(p1.LastRun.Stages) != 1 || p1.LastRun.Stages[0].Status != "Failed" {
		t.Fatalf("p1 latest run wrong: %+v", p1.LastRun)
	}
	p2 := byName["p2"]
	if p2.Group != "grpB" || !p2.Locked || p2.LastRun != nil {
		t.Fatalf("p2 mapped wrong: %+v", p2)
	}
}

func TestPipelineStatus(t *testing.T) {
	srv := newServer(t)
	defer srv.Close()
	st, err := newClient(srv.URL).PipelineStatus(context.Background(), "p1")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !st.Paused || st.PausedCause != "x" || st.PausedBy != "u" || !st.Schedulable {
		t.Fatalf("status mapped wrong: %+v", st)
	}
}

func TestPipelineHistory(t *testing.T) {
	srv := newServer(t)
	defer srv.Close()
	runs, err := newClient(srv.URL).PipelineHistory(context.Background(), "p1", 0)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(runs) != 1 || runs[0].Counter != 5 || runs[0].ScheduledDate != 123 || runs[0].Comment != "c" {
		t.Fatalf("history mapped wrong: %+v", runs)
	}
	if runs[0].TriggeredBy != "u1" || !runs[0].TriggerForced {
		t.Fatalf("build cause mapped wrong: %+v", runs[0])
	}
}

func TestListAgents(t *testing.T) {
	srv := newServer(t)
	defer srv.Close()
	agents, err := newClient(srv.URL).ListAgents(context.Background())
	if err != nil {
		t.Fatalf("agents: %v", err)
	}
	if len(agents) != 1 {
		t.Fatalf("got %d agents, want 1", len(agents))
	}
	a := agents[0]
	if a.UUID != "u1" || a.ConfigState != "Enabled" || len(a.Environments) != 2 || a.Environments[0] != "E1" {
		t.Fatalf("agent mapped wrong: %+v", a)
	}
}

func TestPipelineConfig_ETag(t *testing.T) {
	srv := newServer(t)
	defer srv.Close()
	cfg, err := newClient(srv.URL).PipelineConfig(context.Background(), "p1")
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if cfg.ETag != `"abc123"` {
		t.Fatalf("etag = %q, want \"abc123\"", cfg.ETag)
	}
	if !strings.Contains(string(cfg.Config), `"name":"p1"`) {
		t.Fatalf("config raw missing name: %s", cfg.Config)
	}
}

func TestStatusErrorMapping(t *testing.T) {
	srv := newServer(t)
	defer srv.Close()
	c := newClient(srv.URL)

	if _, err := c.PipelineStatus(context.Background(), "missing"); !errors.Is(err, gocd.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if _, err := c.PipelineStatus(context.Background(), "denied"); !errors.Is(err, gocd.ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
	if _, err := c.PipelineConfig(context.Background(), "stale"); !errors.Is(err, gocd.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
}
