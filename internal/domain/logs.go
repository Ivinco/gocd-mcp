package domain

import (
	"context"
	"fmt"
	"strings"
)

// JobConsoleLog returns the console log of a job run, optionally trimmed to the last
// tailLines lines (tailLines <= 0 means the whole log).
func (s *Service) JobConsoleLog(ctx context.Context, pipeline string, pipelineCounter int, stage string, stageCounter int, job string, tailLines int) (string, error) {
	if err := validatePipelineName(pipeline); err != nil {
		return "", err
	}
	if strings.TrimSpace(stage) == "" || strings.TrimSpace(job) == "" {
		return "", fmt.Errorf("%w: stage and job are required", ErrInvalidArgument)
	}
	if pipelineCounter < 1 || stageCounter < 1 {
		return "", fmt.Errorf("%w: counters must be >= 1", ErrInvalidArgument)
	}
	log, err := s.c.JobConsoleLog(ctx, pipeline, pipelineCounter, stage, stageCounter, job)
	if err != nil {
		return "", err
	}
	return tail(log, tailLines), nil
}

// tail returns the last n lines of s (or all of s if n <= 0).
func tail(s string, n int) string {
	if n <= 0 {
		return s
	}
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) <= n {
		return strings.Join(lines, "\n")
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}
