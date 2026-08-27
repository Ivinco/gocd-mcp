package gocd

import (
	"context"
	"encoding/json"
	"net/http"
)

type templatesResp struct {
	Embedded struct {
		Templates []struct {
			Name          string `json:"name"`
			CanEdit       bool   `json:"can_edit"`
			CanAdminister bool   `json:"can_administer"`
			Embedded      struct {
				Pipelines []struct {
					Name string `json:"name"`
				} `json:"pipelines"`
			} `json:"_embedded"`
		} `json:"templates"`
	} `json:"_embedded"`
}

// ListTemplates returns all pipeline templates with the pipelines using each one.
func (c *Client) ListTemplates(ctx context.Context) ([]TemplateSummary, error) {
	var resp templatesResp
	if _, err := c.doJSON(ctx, request{
		method: http.MethodGet,
		path:   "/go/api/admin/templates",
		accept: acceptTemplates,
	}, &resp); err != nil {
		return nil, err
	}
	out := make([]TemplateSummary, 0, len(resp.Embedded.Templates))
	for _, t := range resp.Embedded.Templates {
		tpl := TemplateSummary{
			Name:          t.Name,
			CanEdit:       t.CanEdit,
			CanAdminister: t.CanAdminister,
			Pipelines:     make([]string, 0, len(t.Embedded.Pipelines)),
		}
		for _, p := range t.Embedded.Pipelines {
			tpl.Pipelines = append(tpl.Pipelines, p.Name)
		}
		out = append(out, tpl)
	}
	return out, nil
}

// TemplateConfig returns the full template config JSON and its ETag.
func (c *Client) TemplateConfig(ctx context.Context, name string) (*TemplateConfig, error) {
	var raw json.RawMessage
	etag, err := c.doJSON(ctx, request{
		method: http.MethodGet,
		path:   "/go/api/admin/templates/" + esc(name),
		accept: acceptTemplates,
	}, &raw)
	if err != nil {
		return nil, err
	}
	return &TemplateConfig{ETag: etag, Config: raw}, nil
}

// CreateTemplate creates a template. template must be the full template object
// ({"name": ..., "stages": [...]}).
func (c *Client) CreateTemplate(ctx context.Context, template json.RawMessage) error {
	_, err := c.doJSON(ctx, request{
		method: http.MethodPost,
		path:   "/go/api/admin/templates",
		accept: acceptTemplates,
		body:   template,
	}, nil)
	return err
}

// UpdateTemplate replaces a template's config. etag must be the value from a prior
// TemplateConfig call (optimistic locking via If-Match). Returns the new ETag.
func (c *Client) UpdateTemplate(ctx context.Context, name string, template json.RawMessage, etag string) (string, error) {
	newETag, err := c.doJSON(ctx, request{
		method:  http.MethodPut,
		path:    "/go/api/admin/templates/" + esc(name),
		accept:  acceptTemplates,
		body:    template,
		headers: map[string]string{"If-Match": etag},
	}, nil)
	return newETag, err
}

// DeleteTemplate deletes a template by name. GoCD refuses (422) while any pipeline
// still uses the template.
func (c *Client) DeleteTemplate(ctx context.Context, name string) error {
	_, err := c.doJSON(ctx, request{
		method: http.MethodDelete,
		path:   "/go/api/admin/templates/" + esc(name),
		accept: acceptTemplates,
	}, nil)
	return err
}
