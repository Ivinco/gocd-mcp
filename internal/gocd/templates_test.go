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

const acceptTemplatesV7 = "application/vnd.go.cd.v7+json"

func TestListTemplates(t *testing.T) {
	var rec capture
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.method = r.Method
		rec.path = r.URL.Path
		rec.accept = r.Header.Get("Accept")
		// Shape as served by GoCD 25.4.0 (HAL: pipelines nested under _embedded).
		_, _ = io.WriteString(w, `{"_embedded":{"templates":[
			{"name":"t1","can_edit":true,"can_administer":true,"_embedded":{"pipelines":[{"name":"p1","can_administer":true},{"name":"p2"}]}},
			{"name":"t2","can_edit":false,"can_administer":false,"_embedded":{"pipelines":[]}}]}}`)
	}))
	defer srv.Close()

	tpls, err := gocd.NewClient(srv.URL, "tok", 5*time.Second).ListTemplates(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if rec.method != http.MethodGet || rec.path != "/go/api/admin/templates" || rec.accept != acceptTemplatesV7 {
		t.Fatalf("list request wrong: %s %s accept=%s", rec.method, rec.path, rec.accept)
	}
	if len(tpls) != 2 {
		t.Fatalf("templates = %+v, want 2", tpls)
	}
	t1 := tpls[0]
	if t1.Name != "t1" || !t1.CanEdit || !t1.CanAdminister || strings.Join(t1.Pipelines, ",") != "p1,p2" {
		t.Fatalf("t1 mapped wrong: %+v", t1)
	}
	// An unused template must report an empty list, not null.
	if t2 := tpls[1]; t2.Name != "t2" || t2.CanEdit || t2.Pipelines == nil || len(t2.Pipelines) != 0 {
		t.Fatalf("t2 mapped wrong: %+v", t2)
	}
}

func TestTemplateConfig(t *testing.T) {
	var rec capture
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.method = r.Method
		rec.path = r.URL.Path
		rec.escaped = r.URL.EscapedPath()
		rec.accept = r.Header.Get("Accept")
		w.Header().Set("ETag", `"tpl-etag"`)
		_, _ = io.WriteString(w, `{"name":"my tpl","stages":[{"name":"s1"}]}`)
	}))
	defer srv.Close()

	cfg, err := gocd.NewClient(srv.URL, "tok", 5*time.Second).TemplateConfig(context.Background(), "my tpl")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if rec.method != http.MethodGet || rec.path != "/go/api/admin/templates/my tpl" || rec.escaped != "/go/api/admin/templates/my%20tpl" {
		t.Fatalf("get request wrong: %s %s (%s)", rec.method, rec.path, rec.escaped)
	}
	if rec.accept != acceptTemplatesV7 {
		t.Fatalf("get accept = %q, want v7", rec.accept)
	}
	if cfg.ETag != `"tpl-etag"` || !strings.Contains(string(cfg.Config), `"stages"`) {
		t.Fatalf("config mapped wrong: %+v", cfg)
	}
}

func TestCreateTemplate_Request(t *testing.T) {
	var rec capture
	srv := captureServer(t, &rec)
	defer srv.Close()
	body := json.RawMessage(`{"name":"t","stages":[{"name":"s1"}]}`)
	if err := gocd.NewClient(srv.URL, "tok", 5*time.Second).CreateTemplate(context.Background(), body); err != nil {
		t.Fatalf("create: %v", err)
	}
	if rec.method != http.MethodPost || rec.path != "/go/api/admin/templates" {
		t.Fatalf("create request wrong: %s %s", rec.method, rec.path)
	}
	if rec.accept != acceptTemplatesV7 || rec.body != string(body) {
		t.Fatalf("create accept/body wrong: accept=%s body=%s", rec.accept, rec.body)
	}
}

func TestUpdateTemplate(t *testing.T) {
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
		UpdateTemplate(context.Background(), "t", json.RawMessage(`{"name":"t","stages":[]}`), `"etag-1"`)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if rec.method != http.MethodPut || rec.path != "/go/api/admin/templates/t" || rec.accept != acceptTemplatesV7 {
		t.Fatalf("update request wrong: %s %s accept=%s", rec.method, rec.path, rec.accept)
	}
	if ifMatch != `"etag-1"` || !strings.Contains(rec.body, `"stages"`) {
		t.Fatalf("If-Match = %q body = %q", ifMatch, rec.body)
	}
	if newETag != `"etag-2"` {
		t.Fatalf("new etag = %q, want \"etag-2\"", newETag)
	}
}

func TestDeleteTemplate_Request(t *testing.T) {
	var rec capture
	srv := captureServer(t, &rec)
	defer srv.Close()
	if err := gocd.NewClient(srv.URL, "tok", 5*time.Second).DeleteTemplate(context.Background(), "t"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if rec.method != http.MethodDelete || rec.path != "/go/api/admin/templates/t" || rec.accept != acceptTemplatesV7 {
		t.Fatalf("delete request wrong: %s %s accept=%s", rec.method, rec.path, rec.accept)
	}
}
