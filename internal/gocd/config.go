package gocd

import (
	"context"
	"encoding/json"
	"net/http"
)

// UpdatePipelineConfig replaces a pipeline's config. etag must be the value from a
// prior PipelineConfig call (optimistic locking via If-Match). Returns the new ETag.
func (c *Client) UpdatePipelineConfig(ctx context.Context, name string, config json.RawMessage, etag string) (string, error) {
	newETag, err := c.doJSON(ctx, request{
		method:  http.MethodPut,
		path:    "/go/api/admin/pipelines/" + esc(name),
		accept:  acceptPipelineConfig,
		body:    config,
		headers: map[string]string{"If-Match": etag},
	}, nil)
	return newETag, err
}

// CreatePipeline creates a new pipeline. body must be {"group": "...", "pipeline": {...}}.
func (c *Client) CreatePipeline(ctx context.Context, body json.RawMessage) error {
	_, err := c.doJSON(ctx, request{
		method: http.MethodPost,
		path:   "/go/api/admin/pipelines",
		accept: acceptPipelineConfig,
		body:   body,
	}, nil)
	return err
}

// DeletePipeline deletes a pipeline by name.
func (c *Client) DeletePipeline(ctx context.Context, name string) error {
	_, err := c.doJSON(ctx, request{
		method: http.MethodDelete,
		path:   "/go/api/admin/pipelines/" + esc(name),
		accept: acceptPipelineConfig, // admin pipelines API, v11
	}, nil)
	return err
}

// UpdateAgent patches a build agent (e.g. enable/disable, resources, environments).
func (c *Client) UpdateAgent(ctx context.Context, uuid string, patch json.RawMessage) error {
	_, err := c.doJSON(ctx, request{
		method: http.MethodPatch,
		path:   "/go/api/agents/" + esc(uuid),
		accept: acceptAgents,
		body:   patch,
	}, nil)
	return err
}
