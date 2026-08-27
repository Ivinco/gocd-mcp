package mcpsrv

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The SDK's tool wrapper can fail after the handler ran (e.g. output-schema
// validation) and then returns a typed-nil *CallToolResult with an error. The
// logger must pass that through, not dereference it.
func TestToolCallLogger_TolerantOfNilResult(t *testing.T) {
	failing := func(context.Context, string, mcp.Request) (mcp.Result, error) {
		return (*mcp.CallToolResult)(nil), errors.New("validating tool output: boom")
	}
	h := toolCallLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))(failing)
	req := &mcp.ServerRequest[*mcp.CallToolParamsRaw]{Params: &mcp.CallToolParamsRaw{Name: "x"}}
	res, err := h(context.Background(), "tools/call", req)
	if err == nil || err.Error() != "validating tool output: boom" {
		t.Fatalf("err = %v, want the handler's error passed through", err)
	}
	if ctr, ok := res.(*mcp.CallToolResult); !ok || ctr != nil {
		t.Fatalf("res = %#v, want the typed-nil result passed through", res)
	}
}
