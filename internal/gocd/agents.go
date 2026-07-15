package gocd

import (
	"context"
	"net/http"
)

type agentsResp struct {
	Embedded struct {
		Agents []struct {
			UUID         string   `json:"uuid"`
			Hostname     string   `json:"hostname"`
			IPAddress    string   `json:"ip_address"`
			ConfigState  string   `json:"agent_config_state"`
			AgentState   string   `json:"agent_state"`
			BuildState   string   `json:"build_state"`
			OS           string   `json:"operating_system"`
			Resources    []string `json:"resources"`
			Environments []struct {
				Name string `json:"name"`
			} `json:"environments"`
		} `json:"agents"`
	} `json:"_embedded"`
}

// ListAgents returns all build agents and their state.
func (c *Client) ListAgents(ctx context.Context) ([]Agent, error) {
	var resp agentsResp
	if _, err := c.doJSON(ctx, request{
		method: http.MethodGet,
		path:   "/go/api/agents",
		accept: acceptAgents,
	}, &resp); err != nil {
		return nil, err
	}
	out := make([]Agent, 0, len(resp.Embedded.Agents))
	for _, a := range resp.Embedded.Agents {
		agent := Agent{
			UUID:        a.UUID,
			Hostname:    a.Hostname,
			IPAddress:   a.IPAddress,
			ConfigState: a.ConfigState,
			AgentState:  a.AgentState,
			BuildState:  a.BuildState,
			OS:          a.OS,
			Resources:   a.Resources,
		}
		for _, e := range a.Environments {
			agent.Environments = append(agent.Environments, e.Name)
		}
		out = append(out, agent)
	}
	return out, nil
}
