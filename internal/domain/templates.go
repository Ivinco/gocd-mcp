package domain

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ivinco/gocd-mcp/internal/gocd"
)

// ListTemplates returns all pipeline templates with the pipelines using each one.
func (s *Service) ListTemplates(ctx context.Context) ([]gocd.TemplateSummary, error) {
	return s.c.ListTemplates(ctx)
}

// TemplateConfig returns a template's config and ETag.
func (s *Service) TemplateConfig(ctx context.Context, name string) (*gocd.TemplateConfig, error) {
	if err := validateTemplateName(name); err != nil {
		return nil, err
	}
	return s.c.TemplateConfig(ctx, name)
}

// CreateTemplate creates a template from a full template object. The object must
// carry a valid name (GoCD reads it from the body, not the URL).
func (s *Service) CreateTemplate(ctx context.Context, template json.RawMessage) error {
	name, err := templateObjectName(template)
	if err != nil {
		return err
	}
	if err := validateTemplateName(name); err != nil {
		return err
	}
	return s.c.CreateTemplate(ctx, template)
}

// UpdateTemplate replaces a template's config using optimistic locking. etag must
// come from a prior TemplateConfig call. GoCD cannot rename templates through this
// API and answers a name mismatch with an unhelpful 422, so the object's name must
// equal name. Returns the new ETag.
func (s *Service) UpdateTemplate(ctx context.Context, name string, template json.RawMessage, etag string) (string, error) {
	if err := validateTemplateName(name); err != nil {
		return "", err
	}
	if strings.TrimSpace(etag) == "" {
		return "", fmt.Errorf("%w: etag is required (get the current template first)", ErrInvalidArgument)
	}
	bodyName, err := templateObjectName(template)
	if err != nil {
		return "", err
	}
	if bodyName != name {
		return "", fmt.Errorf("%w: template object name %q must match %q (templates cannot be renamed)", ErrInvalidArgument, bodyName, name)
	}
	return s.c.UpdateTemplate(ctx, name, template, etag)
}

// DeleteTemplate deletes a template by name.
func (s *Service) DeleteTemplate(ctx context.Context, name string) error {
	if err := validateTemplateName(name); err != nil {
		return err
	}
	return s.c.DeleteTemplate(ctx, name)
}

// templateObjectName extracts the name from a template object, rejecting anything
// that is not a non-empty JSON object.
func templateObjectName(template json.RawMessage) (string, error) {
	if !isJSONObject(template) {
		return "", fmt.Errorf("%w: template must be a JSON object", ErrInvalidArgument)
	}
	var obj struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(template, &obj); err != nil {
		return "", fmt.Errorf("%w: template.name must be a string", ErrInvalidArgument)
	}
	return obj.Name, nil
}
