package gocd

// GoCD API media-type versions, verified against GoCD 25.4.0. Each GoCD endpoint
// negotiates its own version via the Accept header: application/vnd.go.cd.vN+json.
const (
	acceptCurrentUser    = "application/vnd.go.cd.v1+json"
	acceptPipelineStatus = "application/vnd.go.cd.v1+json"
	acceptHistory        = "application/vnd.go.cd.v1+json"
	acceptPipelineConfig = "application/vnd.go.cd.v11+json"
	acceptSchedule       = "application/vnd.go.cd.v1+json"
	acceptStageRun       = "application/vnd.go.cd.v2+json"
	acceptPause          = "application/vnd.go.cd.v1+json"
	acceptUnpause        = "application/vnd.go.cd.v1+json"
	acceptDashboard      = "application/vnd.go.cd.v4+json"
	acceptAgents         = "application/vnd.go.cd.v7+json"
	acceptCancelStage    = "application/vnd.go.cd.v3+json"
	acceptComment        = "application/vnd.go.cd.v1+json"
	acceptInstance       = "application/vnd.go.cd.v1+json"
	acceptTemplates      = "application/vnd.go.cd.v7+json"
	// delete pipeline reuses the admin pipelines config media type (v11).
)

// headerConfirm is required by GoCD for mutating POST endpoints (trigger, stage run).
const headerConfirm = "X-GoCD-Confirm"
