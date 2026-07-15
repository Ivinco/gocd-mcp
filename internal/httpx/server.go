// Package httpx wires the net/http transport: TLS server, middleware chain,
// health probes, and the authenticated MCP endpoint.
package httpx

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"

	"github.com/ivinco/gocd-mcp/internal/auth"
	"github.com/ivinco/gocd-mcp/internal/config"
	"github.com/ivinco/gocd-mcp/internal/gocd"
)

// NewServer builds the *http.Server. mcpHandler is the MCP Streamable HTTP handler;
// it is mounted behind bearer-token auth at cfg.MCPEndpointPath. Health probes are
// unauthenticated.
func NewServer(cfg *config.Config, log *slog.Logger, mcpHandler http.Handler, verifier *auth.Verifier) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), cfg.GoCDTimeout)
		defer cancel()
		if err := gocd.Reachable(ctx, cfg.GoCDBaseURL, cfg.GoCDTimeout); err != nil {
			http.Error(w, "gocd unreachable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	})

	authMW := mcpauth.RequireBearerToken(verifier.Verify, &mcpauth.RequireBearerTokenOptions{})
	// captureLoginMW sits below auth so it sees the resolved principal and can record
	// the login for the access log (which is above auth and otherwise can't see it).
	mux.Handle(cfg.MCPEndpointPath, authMW(captureLoginMW(mcpHandler)))

	handler := recoverMW(log, requestIDMW(accessLogMW(log, mux)))

	return &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
}
