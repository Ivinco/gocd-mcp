// Command gocd-mcp is an MCP server exposing GoCD over Streamable HTTP, authenticated
// per-user via GoCD Personal Access Tokens. See status/architecture.md.
package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ivinco/gocd-mcp/internal/auth"
	"github.com/ivinco/gocd-mcp/internal/config"
	"github.com/ivinco/gocd-mcp/internal/httpx"
	"github.com/ivinco/gocd-mcp/internal/mcpsrv"
	"github.com/ivinco/gocd-mcp/internal/obs"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		// Logger not built yet; fail fast on stderr.
		panic(err)
	}
	log, logCloser, err := obs.NewLogger(cfg.LogLevel, cfg.LogFile)
	if err != nil {
		// Logger not built yet; fail fast on stderr.
		panic(err)
	}
	defer func() { _ = logCloser.Close() }()

	verifier := auth.NewVerifier(cfg.GoCDBaseURL, cfg.GoCDTimeout, cfg.TokenCacheTTL)
	srv := mcpsrv.New(cfg, log)
	mcpHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, nil)
	httpServer := httpx.NewServer(cfg, log, mcpHandler, verifier)

	if !cfg.TLSEnabled() {
		log.Warn("TLS is not configured; serving plain HTTP. Use a TLS-terminating proxy in production (PATs travel in the Authorization header).")
	}

	// Run the server and wait for a shutdown signal.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		log.Info("starting gocd-mcp", "addr", cfg.ListenAddr, "endpoint", cfg.MCPEndpointPath,
			"gocd", cfg.GoCDBaseURL, "toolset", string(cfg.Toolset), "tls", cfg.TLSEnabled())
		if cfg.TLSEnabled() {
			errCh <- httpServer.ListenAndServeTLS(cfg.TLSCertFile, cfg.TLSKeyFile)
		} else {
			errCh <- httpServer.ListenAndServe()
		}
	}()

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server exited", "error", err)
			os.Exit(1)
		}
	case <-ctx.Done():
		log.Info("shutdown signal received")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			log.Error("graceful shutdown failed", "error", err)
			os.Exit(1)
		}
		log.Info("shutdown complete")
	}
}
