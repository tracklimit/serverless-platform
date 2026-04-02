package nats

type DeployRequest struct {
	DeploymentID int64  `json:"deployment_id"`
	WorkspaceID  int64  `json:"workspace_id"`
	FunctionName string `json:"function_name"`
	Namespace    string `json:"namespace"`
	Runtime      string `json:"runtime"`
	DeployType   string `json:"deploy_type"`
	Code         string `json:"code,omitempty"`
	Image        string `json:"image,omitempty"`
	Public       bool   `json:"public"`
}

type DeployStatusEvent struct {
	DeploymentID int64  `json:"deployment_id"`
	FunctionName string `json:"function_name"`
	Status       string `json:"status"`
	URL          string `json:"url,omitempty"`
	Error        string `json:"error,omitempty"`
}
