package domain

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// UpdatePipelineConfig replaces a pipeline's config using optimistic locking. etag
// must come from a prior PipelineConfig call. Returns the new ETag.
func (s *Service) UpdatePipelineConfig(ctx context.Context, name string, config json.RawMessage, etag string) (string, error) {
	if err := validatePipelineName(name); err != nil {
		return "", err
	}
	if strings.TrimSpace(etag) == "" {
		return "", fmt.Errorf("%w: etag is required (get the current config first)", ErrInvalidArgument)
	}
	if !isJSONObject(config) {
		return "", fmt.Errorf("%w: config must be a JSON object", ErrInvalidArgument)
	}
	return s.c.UpdatePipelineConfig(ctx, name, config, etag)
}

// CreatePipeline creates a new pipeline in the given group.
func (s *Service) CreatePipeline(ctx context.Context, group string, pipeline json.RawMessage) error {
	if strings.TrimSpace(group) == "" {
		return fmt.Errorf("%w: group is required", ErrInvalidArgument)
	}
	if !isJSONObject(pipeline) {
		return fmt.Errorf("%w: pipeline must be a JSON object", ErrInvalidArgument)
	}
	body, err := json.Marshal(struct {
		Group    string          `json:"group"`
		Pipeline json.RawMessage `json:"pipeline"`
	}{Group: group, Pipeline: pipeline})
	if err != nil {
		return err
	}
	return s.c.CreatePipeline(ctx, body)
}

// DeletePipeline deletes a pipeline by name.
func (s *Service) DeletePipeline(ctx context.Context, name string) error {
	if err := validatePipelineName(name); err != nil {
		return err
	}
	return s.c.DeletePipeline(ctx, name)
}

// UpdateAgent patches a build agent.
func (s *Service) UpdateAgent(ctx context.Context, uuid string, patch json.RawMessage) error {
	if strings.TrimSpace(uuid) == "" {
		return fmt.Errorf("%w: agent uuid is required", ErrInvalidArgument)
	}
	if !isJSONObject(patch) {
		return fmt.Errorf("%w: patch must be a JSON object", ErrInvalidArgument)
	}
	return s.c.UpdateAgent(ctx, uuid, patch)
}

// isJSONObject reports whether raw is a non-empty JSON object.
func isJSONObject(raw json.RawMessage) bool {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return false
	}
	return len(m) > 0
}
