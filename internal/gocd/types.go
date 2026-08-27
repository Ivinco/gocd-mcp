package gocd

import "encoding/json"

// Result types returned by the client. They are focused projections of GoCD's
// (HAL+JSON) responses — only the fields useful to an LLM agent — to keep tool
// output compact.

// PipelineSummary is one pipeline's state on the dashboard.
type PipelineSummary struct {
	Group          string      `json:"group"`
	Name           string      `json:"name"`
	Paused         bool        `json:"paused"`
	PauseReason    string      `json:"pause_reason,omitempty"`
	Locked         bool        `json:"locked"`
	FromConfigRepo bool        `json:"from_config_repo"`
	LastRun        *RunSummary `json:"last_run,omitempty"`
}

// RunSummary is the latest instance of a pipeline.
type RunSummary struct {
	Counter int           `json:"counter"`
	Label   string        `json:"label,omitempty"`
	Stages  []StageStatus `json:"stages,omitempty"`
}

// StageStatus is a stage name and its status (e.g. Passed/Failed/Building).
type StageStatus struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

// PipelineStatus is the pause/lock/schedulable state of a pipeline.
type PipelineStatus struct {
	Paused      bool   `json:"paused"`
	PausedCause string `json:"paused_cause,omitempty"`
	PausedBy    string `json:"paused_by,omitempty"`
	Locked      bool   `json:"locked"`
	Schedulable bool   `json:"schedulable"`
}

// HistoryItem is one past run of a pipeline. TriggeredBy is GoCD's build-cause
// approver: a user login for forced (manual/API) runs, or "timer" / "changes" for
// automatic ones.
type HistoryItem struct {
	Counter       int           `json:"counter"`
	Label         string        `json:"label,omitempty"`
	ScheduledDate int64         `json:"scheduled_date_ms,omitempty"`
	Comment       string        `json:"comment,omitempty"`
	TriggeredBy   string        `json:"triggered_by,omitempty"`
	TriggerForced bool          `json:"trigger_forced"`
	Stages        []StageStatus `json:"stages,omitempty"`
}

// HistoryPage is one page of a pipeline's run history (newest first). NextAfter is
// the opaque cursor for the next (older) page, empty when there are no more pages;
// GoCD's history API paginates by cursor, not offset.
type HistoryPage struct {
	Runs      []HistoryItem `json:"runs"`
	NextAfter string        `json:"next_after,omitempty"`
}

// Agent is a build agent and its state.
type Agent struct {
	UUID         string   `json:"uuid"`
	Hostname     string   `json:"hostname"`
	IPAddress    string   `json:"ip_address,omitempty"`
	ConfigState  string   `json:"config_state,omitempty"`
	AgentState   string   `json:"agent_state,omitempty"`
	BuildState   string   `json:"build_state,omitempty"`
	OS           string   `json:"operating_system,omitempty"`
	Resources    []string `json:"resources,omitempty"`
	Environments []string `json:"environments,omitempty"`
}

// PipelineInstance is the detail of one pipeline run, including per-job state.
type PipelineInstance struct {
	Name          string          `json:"name"`
	Counter       int             `json:"counter"`
	Label         string          `json:"label,omitempty"`
	ScheduledDate int64           `json:"scheduled_date_ms,omitempty"`
	Comment       string          `json:"comment,omitempty"`
	Stages        []InstanceStage `json:"stages"`
}

// InstanceStage is one stage within a pipeline instance. Scheduled is false for a
// stage that has not run yet (e.g. a manual stage awaiting approval; GoCD still
// reports counter 1 for it). ApprovedBy is the user who ran or re-ran the stage, or
// "changes" for automatic runs; CanRun is GoCD's verdict on whether the stage can be
// run now.
type InstanceStage struct {
	Name         string        `json:"name"`
	Counter      int           `json:"counter,omitempty"`
	Scheduled    bool          `json:"scheduled"`
	ApprovalType string        `json:"approval_type,omitempty"`
	ApprovedBy   string        `json:"approved_by,omitempty"`
	CanRun       bool          `json:"can_run"`
	Status       string        `json:"status,omitempty"`
	Result       string        `json:"result,omitempty"`
	Jobs         []InstanceJob `json:"jobs,omitempty"`
}

// InstanceJob is one job within a stage instance.
type InstanceJob struct {
	Name          string `json:"name"`
	State         string `json:"state,omitempty"`
	Result        string `json:"result,omitempty"`
	ScheduledDate int64  `json:"scheduled_date_ms,omitempty"`
}

// PipelineConfig is a pipeline's full configuration plus its ETag, which must be
// supplied as If-Match when updating the config (optimistic locking).
type PipelineConfig struct {
	ETag   string          `json:"etag"`
	Config json.RawMessage `json:"config"`
}

// TemplateSummary is one pipeline template as listed by GoCD: the pipelines that use
// it and whether the caller may edit / administer it.
type TemplateSummary struct {
	Name          string   `json:"name"`
	CanEdit       bool     `json:"can_edit"`
	CanAdminister bool     `json:"can_administer"`
	Pipelines     []string `json:"pipelines"`
}

// TemplateConfig is a pipeline template's full configuration plus its ETag, which
// must be supplied as If-Match when updating the template (optimistic locking).
type TemplateConfig struct {
	ETag   string          `json:"etag"`
	Config json.RawMessage `json:"config"`
}
