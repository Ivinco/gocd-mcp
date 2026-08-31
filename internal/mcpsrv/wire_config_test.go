package mcpsrv_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestGetPipelineConfig_NotFoundIsToolError pins the fix for a server crash: the
// error result's zero output (a nil map → JSON null) failed the SDK's output-schema
// validation, and the resulting nil result tripped a nil-pointer panic in the
// tool-call logger.
func TestGetPipelineConfig_NotFoundIsToolError(t *testing.T) {
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

	res, err := sess.CallTool(ctx, &mcp.CallToolParams{Name: "get_pipeline_config", Arguments: map[string]any{"name": "nope"}})
	if err != nil {
		t.Fatalf("transport error (should be tool error): %v", err)
	}
	if !res.IsError || !strings.Contains(resultText(res), "not found") {
		t.Fatalf("IsError=%v text=%q, want a not-found tool error", res.IsError, resultText(res))
	}
}
