package gocd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
)

// confirmHeaders returns the header GoCD requires to authorize state-changing POSTs.
func confirmHeaders() map[string]string {
	return map[string]string{headerConfirm: "true"}
}

func esc(s string) string { return url.PathEscape(s) }

// SchedulePipeline triggers a pipeline run with default materials. Requires the
// confirm header.
func (c *Client) SchedulePipeline(ctx context.Context, name string) error {
	_, err := c.doJSON(ctx, request{
		method:  http.MethodPost,
		path:    "/go/api/pipelines/" + esc(name) + "/schedule",
		accept:  acceptSchedule,
		headers: confirmHeaders(),
	}, nil)
	return err
}

// PausePipeline pauses a pipeline with the given cause.
func (c *Client) PausePipeline(ctx context.Context, name, cause string) error {
	body, _ := json.Marshal(map[string]string{"pause_cause": cause})
	_, err := c.doJSON(ctx, request{
		method:  http.MethodPost,
		path:    "/go/api/pipelines/" + esc(name) + "/pause",
		accept:  acceptPause,
		body:    body,
		headers: confirmHeaders(),
	}, nil)
	return err
}

// UnpausePipeline resumes a paused pipeline. GoCD requires the confirm header here.
func (c *Client) UnpausePipeline(ctx context.Context, name string) error {
	_, err := c.doJSON(ctx, request{
		method:  http.MethodPost,
		path:    "/go/api/pipelines/" + esc(name) + "/unpause",
		accept:  acceptUnpause,
		headers: confirmHeaders(),
	}, nil)
	return err
}

// CancelStage cancels a running stage. Requires the confirm header.
func (c *Client) CancelStage(ctx context.Context, pipeline string, pipelineCounter int, stage string, stageCounter int) error {
	path := "/go/api/stages/" + esc(pipeline) + "/" + strconv.Itoa(pipelineCounter) +
		"/" + esc(stage) + "/" + strconv.Itoa(stageCounter) + "/cancel"
	_, err := c.doJSON(ctx, request{
		method:  http.MethodPost,
		path:    path,
		accept:  acceptCancelStage,
		headers: confirmHeaders(),
	}, nil)
	return err
}

// RunStage runs (or re-runs) one stage of an existing pipeline instance. Requires
// the confirm header; GoCD answers 202 and schedules the stage asynchronously.
func (c *Client) RunStage(ctx context.Context, pipeline string, pipelineCounter int, stage string) error {
	path := "/go/api/stages/" + esc(pipeline) + "/" + strconv.Itoa(pipelineCounter) +
		"/" + esc(stage) + "/run"
	_, err := c.doJSON(ctx, request{
		method:  http.MethodPost,
		path:    path,
		accept:  acceptStageRun,
		headers: confirmHeaders(),
	}, nil)
	return err
}

// CommentOnPipeline adds a comment to a pipeline instance.
func (c *Client) CommentOnPipeline(ctx context.Context, name string, counter int, comment string) error {
	body, _ := json.Marshal(map[string]string{"comment": comment})
	_, err := c.doJSON(ctx, request{
		method: http.MethodPost,
		path:   "/go/api/pipelines/" + esc(name) + "/" + strconv.Itoa(counter) + "/comment",
		accept: acceptComment,
		body:   body,
	}, nil)
	return err
}
