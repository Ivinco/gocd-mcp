package gocd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
)

// --- dashboard ---

type dashboardResp struct {
	Embedded struct {
		PipelineGroups []struct {
			Name      string   `json:"name"`
			Pipelines []string `json:"pipelines"`
		} `json:"pipeline_groups"`
		Pipelines []dashPipeline `json:"pipelines"`
	} `json:"_embedded"`
}

type dashPipeline struct {
	Name           string `json:"name"`
	Locked         bool   `json:"locked"`
	FromConfigRepo bool   `json:"from_config_repo"`
	PauseInfo      struct {
		Paused      bool   `json:"paused"`
		PauseReason string `json:"pause_reason"`
	} `json:"pause_info"`
	Embedded struct {
		Instances []dashInstance `json:"instances"`
	} `json:"_embedded"`
}

type dashInstance struct {
	Counter  int    `json:"counter"`
	Label    string `json:"label"`
	Embedded struct {
		Stages []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"stages"`
	} `json:"_embedded"`
}

// Dashboard returns a flattened view of all pipelines and their latest state.
func (c *Client) Dashboard(ctx context.Context) ([]PipelineSummary, error) {
	var resp dashboardResp
	if _, err := c.doJSON(ctx, request{
		method: http.MethodGet,
		path:   "/go/api/dashboard",
		accept: acceptDashboard,
	}, &resp); err != nil {
		return nil, err
	}

	// Map each pipeline name to its group.
	groupOf := make(map[string]string)
	for _, g := range resp.Embedded.PipelineGroups {
		for _, name := range g.Pipelines {
			groupOf[name] = g.Name
		}
	}

	out := make([]PipelineSummary, 0, len(resp.Embedded.Pipelines))
	for _, p := range resp.Embedded.Pipelines {
		s := PipelineSummary{
			Group:          groupOf[p.Name],
			Name:           p.Name,
			Paused:         p.PauseInfo.Paused,
			PauseReason:    p.PauseInfo.PauseReason,
			Locked:         p.Locked,
			FromConfigRepo: p.FromConfigRepo,
		}
		if latest := latestInstance(p.Embedded.Instances); latest != nil {
			run := &RunSummary{Counter: latest.Counter, Label: latest.Label}
			for _, st := range latest.Embedded.Stages {
				run.Stages = append(run.Stages, StageStatus{Name: st.Name, Status: st.Status})
			}
			s.LastRun = run
		}
		out = append(out, s)
	}
	return out, nil
}

// latestInstance returns the instance with the highest counter, or nil if none.
func latestInstance(instances []dashInstance) *dashInstance {
	var latest *dashInstance
	for i := range instances {
		if latest == nil || instances[i].Counter > latest.Counter {
			latest = &instances[i]
		}
	}
	return latest
}

// --- status ---

// PipelineStatus returns the pause/lock/schedulable state of a pipeline.
func (c *Client) PipelineStatus(ctx context.Context, name string) (*PipelineStatus, error) {
	var s PipelineStatus
	if _, err := c.doJSON(ctx, request{
		method: http.MethodGet,
		path:   "/go/api/pipelines/" + url.PathEscape(name) + "/status",
		accept: acceptPipelineStatus,
	}, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// --- history ---

type historyResp struct {
	Pipelines []struct {
		Counter       int    `json:"counter"`
		Label         string `json:"label"`
		ScheduledDate int64  `json:"scheduled_date"`
		Comment       string `json:"comment"`
		BuildCause    struct {
			Approver      string `json:"approver"`
			TriggerForced bool   `json:"trigger_forced"`
		} `json:"build_cause"`
		Stages []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"stages"`
	} `json:"pipelines"`
}

// PipelineHistory returns past runs of a pipeline, newest first, starting at offset.
func (c *Client) PipelineHistory(ctx context.Context, name string, offset int) ([]HistoryItem, error) {
	if offset < 0 {
		offset = 0
	}
	var resp historyResp
	if _, err := c.doJSON(ctx, request{
		method: http.MethodGet,
		path:   "/go/api/pipelines/" + url.PathEscape(name) + "/history?offset=" + strconv.Itoa(offset),
		accept: acceptHistory,
	}, &resp); err != nil {
		return nil, err
	}
	out := make([]HistoryItem, 0, len(resp.Pipelines))
	for _, p := range resp.Pipelines {
		item := HistoryItem{
			Counter:       p.Counter,
			Label:         p.Label,
			ScheduledDate: p.ScheduledDate,
			Comment:       p.Comment,
			TriggeredBy:   p.BuildCause.Approver,
			TriggerForced: p.BuildCause.TriggerForced,
		}
		for _, st := range p.Stages {
			item.Stages = append(item.Stages, StageStatus{Name: st.Name, Status: st.Status})
		}
		out = append(out, item)
	}
	return out, nil
}

// --- config ---

// PipelineConfig returns the full pipeline config JSON and its ETag.
func (c *Client) PipelineConfig(ctx context.Context, name string) (*PipelineConfig, error) {
	var raw json.RawMessage
	etag, err := c.doJSON(ctx, request{
		method: http.MethodGet,
		path:   "/go/api/admin/pipelines/" + url.PathEscape(name),
		accept: acceptPipelineConfig,
	}, &raw)
	if err != nil {
		return nil, err
	}
	return &PipelineConfig{ETag: etag, Config: raw}, nil
}
