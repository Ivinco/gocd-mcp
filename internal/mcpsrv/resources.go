package mcpsrv

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ivinco/gocd-mcp/internal/config"
)

// registerResources mirrors document-like read data as MCP resources (see
// architecture.md §5.0/§5.2): the same data tools expose, but addressable by URI for
// a user/app to attach as context.
func registerResources(s *mcp.Server, cfg *config.Config) {
	s.AddResource(&mcp.Resource{
		Name:        "dashboard",
		URI:         "gocd://dashboard",
		Description: "All GoCD pipelines and their latest run status (JSON).",
		MIMEType:    "application/json",
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		svc, err := serviceFor(ctx, cfg)
		if err != nil {
			return nil, err
		}
		pls, err := svc.ListPipelines(ctx, "")
		if err != nil {
			return nil, err
		}
		return jsonResource(req.Params.URI, pls)
	})

	s.AddResource(&mcp.Resource{
		Name:        "agents",
		URI:         "gocd://agents",
		Description: "All GoCD build agents and their state (JSON).",
		MIMEType:    "application/json",
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		svc, err := serviceFor(ctx, cfg)
		if err != nil {
			return nil, err
		}
		agents, err := svc.ListAgents(ctx)
		if err != nil {
			return nil, err
		}
		return jsonResource(req.Params.URI, agents)
	})

	s.AddResourceTemplate(&mcp.ResourceTemplate{
		Name:        "pipeline-config",
		URITemplate: "gocd://pipeline/{name}/config",
		Description: "Full configuration JSON of a GoCD pipeline.",
		MIMEType:    "application/json",
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		name, err := pipelineNameFromURI(req.Params.URI, "/config")
		if err != nil {
			return nil, err
		}
		svc, err := serviceFor(ctx, cfg)
		if err != nil {
			return nil, err
		}
		cfgResult, err := svc.PipelineConfig(ctx, name)
		if err != nil {
			return nil, err
		}
		return jsonResource(req.Params.URI, json.RawMessage(cfgResult.Config))
	})

	s.AddResourceTemplate(&mcp.ResourceTemplate{
		Name:        "pipeline-history",
		URITemplate: "gocd://pipeline/{name}/history",
		Description: "Recent runs of a GoCD pipeline (JSON).",
		MIMEType:    "application/json",
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		name, err := pipelineNameFromURI(req.Params.URI, "/history")
		if err != nil {
			return nil, err
		}
		svc, err := serviceFor(ctx, cfg)
		if err != nil {
			return nil, err
		}
		runs, err := svc.PipelineHistory(ctx, name, 0)
		if err != nil {
			return nil, err
		}
		return jsonResource(req.Params.URI, runs)
	})
}

// pipelineNameFromURI extracts {name} from gocd://pipeline/{name}<suffix>.
func pipelineNameFromURI(uri, suffix string) (string, error) {
	const prefix = "gocd://pipeline/"
	if !strings.HasPrefix(uri, prefix) || !strings.HasSuffix(uri, suffix) {
		return "", fmt.Errorf("unrecognized resource URI %q", uri)
	}
	name := strings.TrimSuffix(strings.TrimPrefix(uri, prefix), suffix)
	if name == "" {
		return "", fmt.Errorf("missing pipeline name in URI %q", uri)
	}
	return name, nil
}

func jsonResource(uri string, v any) (*mcp.ReadResourceResult, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{{URI: uri, MIMEType: "application/json", Text: string(b)}},
	}, nil
}
