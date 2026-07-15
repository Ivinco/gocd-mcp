package gocd

import (
	"context"
	"fmt"
	"io"
	"net/http"
)

// maxConsoleBytes caps how much of a console log we read into memory.
const maxConsoleBytes = 2 << 20 // 2 MiB

// JobConsoleLog fetches the raw console log of a job run via the GoCD files API.
// The log is plain text (not versioned JSON). The result is capped at maxConsoleBytes.
func (c *Client) JobConsoleLog(ctx context.Context, pipeline string, pipelineCounter int, stage string, stageCounter int, job string) (string, error) {
	path := fmt.Sprintf("/go/files/%s/%d/%s/%d/%s/cruise-output/console.log",
		esc(pipeline), pipelineCounter, esc(stage), stageCounter, esc(job))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if err := statusError(resp); err != nil {
		return "", err
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxConsoleBytes))
	if err != nil {
		return "", err
	}
	return string(b), nil
}
