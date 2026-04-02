package model

import "time"

type DeployStatus string

const (
	DeployStatusQueued    DeployStatus = "queued"
	DeployStatusBuilding  DeployStatus = "building"
	DeployStatusDeploying DeployStatus = "deploying"
	DeployStatusRunning   DeployStatus = "running"
	DeployStatusFailed    DeployStatus = "failed"
)

type Deployment struct {
	ID              int64        `json:"id"`
	FunctionID      int64        `json:"function_id"`
	FunctionName    string       `json:"function_name"`
	WorkspaceID     int64        `json:"-"`
	Status          DeployStatus `json:"status"`
	KnativeRevision string       `json:"knative_revision,omitempty"`
	Error           string       `json:"error,omitempty"`
	CreatedAt       time.Time    `json:"created_at"`
	StartedAt       time.Time    `json:"started_at,omitempty"`
	CompletedAt     time.Time    `json:"completed_at,omitempty"`
}
