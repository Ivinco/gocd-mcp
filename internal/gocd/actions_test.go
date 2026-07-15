package gocd_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ivinco/gocd-mcp/internal/gocd"
)

// capture records the most recent request the test server received.
type capture struct {
	method  string
	path    string
	escaped string
	accept  string
	confirm string
	body    string
}

func captureServer(t *testing.T, rec *capture) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		rec.method = r.Method
		rec.path = r.URL.Path
		rec.escaped = r.URL.EscapedPath()
		rec.accept = r.Header.Get("Accept")
		rec.confirm = r.Header.Get("X-GoCD-Confirm")
		rec.body = strings.TrimSpace(string(b))
		w.WriteHeader(http.StatusOK)
	}))
}

func TestSchedulePipeline(t *testing.T) {
	var rec capture
	srv := captureServer(t, &rec)
	defer srv.Close()
	if err := gocd.NewClient(srv.URL, "tok", 5*time.Second).SchedulePipeline(context.Background(), "my pipe"); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	if rec.method != http.MethodPost || rec.escaped != "/go/api/pipelines/my%20pipe/schedule" {
		t.Fatalf("schedule request wrong: %s %s", rec.method, rec.escaped)
	}
	if rec.accept != "application/vnd.go.cd.v1+json" {
		t.Fatalf("schedule accept = %q", rec.accept)
	}
	if rec.confirm != "true" {
		t.Fatalf("schedule missing confirm header, got %q", rec.confirm)
	}
}

func TestPausePipeline(t *testing.T) {
	var rec capture
	srv := captureServer(t, &rec)
	defer srv.Close()
	if err := gocd.NewClient(srv.URL, "tok", 5*time.Second).PausePipeline(context.Background(), "p", "maintenance"); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if rec.path != "/go/api/pipelines/p/pause" || !strings.Contains(rec.body, `"pause_cause":"maintenance"`) {
		t.Fatalf("pause request wrong: path=%s body=%s", rec.path, rec.body)
	}
	if rec.confirm != "true" {
		t.Fatalf("pause missing confirm header")
	}
}

func TestUnpausePipeline(t *testing.T) {
	var rec capture
	srv := captureServer(t, &rec)
	defer srv.Close()
	if err := gocd.NewClient(srv.URL, "tok", 5*time.Second).UnpausePipeline(context.Background(), "p"); err != nil {
		t.Fatalf("unpause: %v", err)
	}
	if rec.method != http.MethodPost || rec.path != "/go/api/pipelines/p/unpause" {
		t.Fatalf("unpause request wrong: %s %s", rec.method, rec.path)
	}
	// GoCD requires the confirm header for unpause.
	if rec.confirm != "true" {
		t.Fatalf("unpause missing confirm header")
	}
}

func TestCancelStage(t *testing.T) {
	var rec capture
	srv := captureServer(t, &rec)
	defer srv.Close()
	if err := gocd.NewClient(srv.URL, "tok", 5*time.Second).CancelStage(context.Background(), "p", 4, "build", 2); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if rec.path != "/go/api/stages/p/4/build/2/cancel" {
		t.Fatalf("cancel path = %s", rec.path)
	}
	if rec.accept != "application/vnd.go.cd.v3+json" {
		t.Fatalf("cancel accept = %q, want v3", rec.accept)
	}
	if rec.confirm != "true" {
		t.Fatalf("cancel missing confirm header")
	}
}

func TestCommentOnPipeline(t *testing.T) {
	var rec capture
	srv := captureServer(t, &rec)
	defer srv.Close()
	if err := gocd.NewClient(srv.URL, "tok", 5*time.Second).CommentOnPipeline(context.Background(), "p", 7, "looks good"); err != nil {
		t.Fatalf("comment: %v", err)
	}
	if rec.path != "/go/api/pipelines/p/7/comment" || !strings.Contains(rec.body, `"comment":"looks good"`) {
		t.Fatalf("comment request wrong: path=%s body=%s", rec.path, rec.body)
	}
	if rec.accept != "application/vnd.go.cd.v1+json" {
		t.Fatalf("comment accept = %q", rec.accept)
	}
}
