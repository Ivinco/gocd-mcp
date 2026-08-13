// Package mcpsrv builds the MCP server and registers tools, resources and prompts,
// mapping MCP calls onto the domain layer. It is intentionally thin: argument parsing
// and validation live on typed structs / the domain, not here.
package mcpsrv

import (
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ivinco/gocd-mcp/internal/config"
)

// Version is the server implementation version reported to clients.
const Version = "1.1.0"

// New builds a configured MCP server. The toolset registered depends on cfg.Toolset.
// log is used for the audit trail of mutating actions.
func New(cfg *config.Config, log *slog.Logger) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "gocd-mcp", Version: Version}, nil)

	// Observability: log every tool call (user, outcome, latency) for reads and writes.
	s.AddReceivingMiddleware(toolCallLogger(log))

	// Always available: identity check.
	registerWhoami(s)

	// Read-only tools form the base of every toolset.
	registerReadOnly(s, cfg)
	registerResources(s, cfg)

	// Action tools (gated to actions/full) and config-editing tools (full only).
	registerActions(s, cfg, log)
	registerConfig(s, cfg, log)
	return s
}
