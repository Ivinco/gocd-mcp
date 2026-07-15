package gocd

import (
	"context"
	"net/http"
	"strconv"
)

type instanceResp struct {
	Name          string `json:"name"`
	Counter       int    `json:"counter"`
	Label         string `json:"label"`
	ScheduledDate int64  `json:"scheduled_date"`
	Comment       string `json:"comment"`
	Stages        []struct {
		Name   string `json:"name"`
		Status string `json:"status"`
		Result string `json:"result"`
		Jobs   []struct {
			Name          string `json:"name"`
			State         string `json:"state"`
			Result        string `json:"result"`
			ScheduledDate int64  `json:"scheduled_date"`
		} `json:"jobs"`
	} `json:"stages"`
}

// PipelineInstance returns the detail of a single pipeline run, including per-job state.
func (c *Client) PipelineInstance(ctx context.Context, name string, counter int) (*PipelineInstance, error) {
	var resp instanceResp
	if _, err := c.doJSON(ctx, request{
		method: http.MethodGet,
		path:   "/go/api/pipelines/" + esc(name) + "/" + strconv.Itoa(counter),
		accept: acceptInstance,
	}, &resp); err != nil {
		return nil, err
	}
	inst := &PipelineInstance{
		Name:          resp.Name,
		Counter:       resp.Counter,
		Label:         resp.Label,
		ScheduledDate: resp.ScheduledDate,
		Comment:       resp.Comment,
	}
	for _, st := range resp.Stages {
		stage := InstanceStage{Name: st.Name, Status: st.Status, Result: st.Result}
		for _, j := range st.Jobs {
			stage.Jobs = append(stage.Jobs, InstanceJob{
				Name:          j.Name,
				State:         j.State,
				Result:        j.Result,
				ScheduledDate: j.ScheduledDate,
			})
		}
		inst.Stages = append(inst.Stages, stage)
	}
	return inst, nil
}
