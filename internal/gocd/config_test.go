package gocd_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ivinco/gocd-mcp/internal/gocd"
)

func TestUpdatePipelineConfig(t *testing.T) {
	var rec capture
	var ifMatch string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		rec.method = r.Method
		rec.path = r.URL.Path
		rec.accept = r.Header.Get("Accept")
		rec.body = strings.TrimSpace(string(b))
		ifMatch = r.Header.Get("If-Match")
		w.Header().Set("ETag", `"etag-2"`)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	newETag, err := gocd.NewClient(srv.URL, "tok", 5*time.Second).
		UpdatePipelineConfig(context.Background(), "p", json.RawMessage(`{"name":"p"}`), `"etag-1"`)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if rec.method != http.MethodPut || rec.path != "/go/api/admin/pipelines/p" {
		t.Fatalf("update request wrong: %s %s", rec.method, rec.path)
	}
	if rec.accept != "application/vnd.go.cd.v11+json" {
		t.Fatalf("update accept = %q, want v11", rec.accept)
	}
	if ifMatch != `"etag-1"` {
		t.Fatalf("If-Match = %q, want \"etag-1\"", ifMatch)
	}
	if newETag != `"etag-2"` {
		t.Fatalf("new etag = %q, want \"etag-2\"", newETag)
	}
}

func TestCreatePipeline_Request(t *testing.T) {
	var rec capture
	srv := captureServer(t, &rec)
	defer srv.Close()
	body := json.RawMessage(`{"group":"g","pipeline":{"name":"p"}}`)
	if err := gocd.NewClient(srv.URL, "tok", 5*time.Second).CreatePipeline(context.Background(), body); err != nil {
		t.Fatalf("create: %v", err)
	}
	if rec.method != http.MethodPost || rec.path != "/go/api/admin/pipelines" {
		t.Fatalf("create request wrong: %s %s", rec.method, rec.path)
	}
	if rec.accept != "application/vnd.go.cd.v11+json" || !strings.Contains(rec.body, `"group":"g"`) {
		t.Fatalf("create accept/body wrong: accept=%s body=%s", rec.accept, rec.body)
	}
}

func TestDeletePipeline_Request(t *testing.T) {
	var rec capture
	srv := captureServer(t, &rec)
	defer srv.Close()
	if err := gocd.NewClient(srv.URL, "tok", 5*time.Second).DeletePipeline(context.Background(), "p"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if rec.method != http.MethodDelete || rec.path != "/go/api/admin/pipelines/p" {
		t.Fatalf("delete request wrong: %s %s", rec.method, rec.path)
	}
	if rec.accept != "application/vnd.go.cd.v11+json" {
		t.Fatalf("delete accept = %q, want v11", rec.accept)
	}
}

func TestPipelineInstance(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// GoCD serializes stage counters as strings here; the unscheduled second stage
		// is how a manual stage awaiting approval looks (counter "1", scheduled false).
		_, _ = w.Write([]byte(`{"name":"p","counter":3,"label":"3","scheduled_date":123,"comment":"c",
			"stages":[{"name":"run","counter":"2","scheduled":true,"approval_type":"manual","approved_by":"alice","can_run":true,"status":"Passed","result":"Passed",
			"jobs":[{"name":"echo","state":"Completed","result":"Passed","scheduled_date":456}]},
			{"name":"deploy","counter":"1","scheduled":false,"approval_type":null,"approved_by":null,"can_run":true,"status":"Unknown","result":null}]}`))
	}))
	defer srv.Close()
	inst, err := gocd.NewClient(srv.URL, "tok", 5*time.Second).PipelineInstance(context.Background(), "p", 3)
	if err != nil {
		t.Fatalf("instance: %v", err)
	}
	if inst.Counter != 3 || len(inst.Stages) != 2 {
		t.Fatalf("instance mapped wrong: %+v", inst)
	}
	st := inst.Stages[0]
	if st.Name != "run" || st.Result != "Passed" || len(st.Jobs) != 1 {
		t.Fatalf("stage mapped wrong: %+v", st)
	}
	if st.Counter != 2 || !st.Scheduled || st.ApprovalType != "manual" || st.ApprovedBy != "alice" || !st.CanRun {
		t.Fatalf("stage run fields mapped wrong: %+v", st)
	}
	if d := inst.Stages[1]; d.Name != "deploy" || d.Counter != 1 || d.Scheduled || d.ApprovedBy != "" {
		t.Fatalf("unscheduled stage mapped wrong: %+v", d)
	}
	if j := st.Jobs[0]; j.Name != "echo" || j.State != "Completed" || j.Result != "Passed" {
		t.Fatalf("job mapped wrong: %+v", j)
	}
}

func TestUpdateAgent_Request(t *testing.T) {
	var rec capture
	srv := captureServer(t, &rec)
	defer srv.Close()
	if err := gocd.NewClient(srv.URL, "tok", 5*time.Second).
		UpdateAgent(context.Background(), "uuid-1", json.RawMessage(`{"agent_config_state":"Disabled"}`)); err != nil {
		t.Fatalf("update agent: %v", err)
	}
	if rec.method != http.MethodPatch || rec.path != "/go/api/agents/uuid-1" {
		t.Fatalf("agent request wrong: %s %s", rec.method, rec.path)
	}
	if rec.accept != "application/vnd.go.cd.v7+json" {
		t.Fatalf("agent accept = %q, want v7", rec.accept)
	}
}
