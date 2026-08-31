package mcpsrv

import (
	"context"
	"log/slog"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ivinco/gocd-mcp/internal/auth"
	"github.com/ivinco/gocd-mcp/internal/obs"
)

// toolCallLogger logs every tools/call with the calling user, outcome and latency,
// giving per-request observability for both reads and writes (the audit log covers
// only mutations). Tool arguments are not logged, to avoid leaking config bodies.
func toolCallLogger(log *slog.Logger) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if method != "tools/call" {
				return next(ctx, method, req)
			}
			start := time.Now()
			tool := ""
			// On the server side, tools/call params arrive as CallToolParamsRaw.
			if p, ok := req.GetParams().(*mcp.CallToolParamsRaw); ok {
				tool = p.Name
			}

			res, err := next(ctx, method, req)

			ok := err == nil
			// res may be a typed nil (*CallToolResult)(nil) when the SDK's own tool
			// wrapper fails, e.g. on output-schema validation — never dereference it.
			if ctr, isCTR := res.(*mcp.CallToolResult); isCTR && ctr != nil && ctr.IsError {
				ok = false
			}
			login := ""
			if p, found := auth.PrincipalFromContext(ctx); found {
				login = p.Login
			}
			attrs := []any{
				"tool", tool,
				"login", login,
				"ok", ok,
				"duration_ms", time.Since(start).Milliseconds(),
			}
			if id := obs.RequestIDFromContext(ctx); id != "" {
				attrs = append(attrs, "request_id", id)
			}
			log.InfoContext(ctx, "tool_call", attrs...)
			return res, err
		}
	}
}
