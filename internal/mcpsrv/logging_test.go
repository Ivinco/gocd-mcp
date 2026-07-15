package mcpsrv_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestObservability checks that a tool call produces a tool_call log line (with the
// login, tool and outcome) and that the access-log "request" line carries the login.
func TestObservability(t *testing.T) {
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

	res, err := sess.CallTool(ctx, &mcp.CallToolParams{Name: "whoami"})
	if err != nil {
		t.Fatalf("call whoami: %v", err)
	}
	if res.IsError {
		t.Fatalf("whoami tool error: %+v", res.Content)
	}

	// The access-log line is emitted after the HTTP response, so poll for it.
	deadline := time.Now().Add(2 * time.Second)
	var logs string
	for time.Now().Before(deadline) {
		logs = buf.String()
		if strings.Contains(logs, `"msg":"tool_call"`) && strings.Contains(logs, `"msg":"request"`) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Part 2: tool_call line with tool name, login and ok=true.
	if !strings.Contains(logs, `"msg":"tool_call"`) || !strings.Contains(logs, `"tool":"whoami"`) {
		t.Fatalf("missing tool_call log for whoami:\n%s", logs)
	}
	// Part 1: access-log "request" line includes the resolved login.
	if !lineWith(logs, `"msg":"request"`, `"login":"alice"`) {
		t.Fatalf("access log did not include login:\n%s", logs)
	}
	// tool_call also attributes the user.
	if !lineWith(logs, `"msg":"tool_call"`, `"login":"alice"`) {
		t.Fatalf("tool_call log did not include login:\n%s", logs)
	}
	if strings.Contains(logs, goodToken) {
		t.Fatalf("logs leaked the token")
	}
}

// lineWith reports whether some single log line contains both substrings.
func lineWith(logs, a, b string) bool {
	for ln := range strings.SplitSeq(logs, "\n") {
		if strings.Contains(ln, a) && strings.Contains(ln, b) {
			return true
		}
	}
	return false
}
