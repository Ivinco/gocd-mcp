package mcpsrv

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ivinco/gocd-mcp/internal/auth"
)

type whoamiInput struct{}

type whoamiOutput struct {
	Login string `json:"login" jsonschema:"the authenticated GoCD login name"`
}

// registerWhoami adds a read-only tool that echoes the authenticated GoCD login.
// It is the end-to-end identity smoke test for the auth path.
func registerWhoami(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "whoami",
		Description: "Return the GoCD login name of the authenticated user. Verifies the presented bearer token.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ whoamiInput) (*mcp.CallToolResult, whoamiOutput, error) {
		p, ok := auth.PrincipalFromContext(ctx)
		if !ok {
			return nil, whoamiOutput{}, fmt.Errorf("no authenticated principal in context")
		}
		return nil, whoamiOutput{Login: p.Login}, nil
	})
}
