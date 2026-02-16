package model

import (
	"fmt"
	"regexp"
	"time"
)

type DeployType string

const (
	DeployTypeManaged DeployType = "managed"
	DeployTypeBYOI    DeployType = "byoi"
)

type Runtime string

const (
	RuntimePython Runtime = "python"
	RuntimeNodeJS Runtime = "nodejs"
	RuntimeGo     Runtime = "go"
)

type Status string

const (
	StatusPending   Status = "pending"
	StatusDeploying Status = "deploying"
	StatusReady     Status = "ready"
	StatusFailed    Status = "failed"
)

type Function struct {
	Name       string     `json:"name"`
	Runtime    Runtime    `json:"runtime"`
	DeployType DeployType `json:"deploy_type"`
	Code       string     `json:"code,omitempty"`
	Image      string     `json:"image,omitempty"`
	Status     Status     `json:"status"`
	URL        string     `json:"url,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

type CreateFunctionRequest struct {
	Name       string     `json:"name"`
	Runtime    Runtime    `json:"runtime"`
	DeployType DeployType `json:"deploy_type"`
	Code       string     `json:"code,omitempty"`
	Image      string     `json:"image,omitempty"`
}

type UpdateFunctionRequest struct {
	Code  string `json:"code,omitempty"`
	Image string `json:"image,omitempty"`
}

var nameRegex = regexp.MustCompile(`^[a-z][a-z0-9-]{0,61}[a-z0-9]$`)

func (r *CreateFunctionRequest) Validate() error {
	if r.Name == "" {
		return fmt.Errorf("name is required")
	}
	if len(r.Name) > 63 {
		return fmt.Errorf("name must be at most 63 characters")
	}
	if !nameRegex.MatchString(r.Name) {
		return fmt.Errorf("name must be lowercase alphanumeric with hyphens, starting with a letter")
	}

	switch r.DeployType {
	case DeployTypeManaged:
		if err := r.validateManaged(); err != nil {
			return err
		}
	case DeployTypeBYOI:
		if err := r.validateBYOI(); err != nil {
			return err
		}
	default:
		return fmt.Errorf("deploy_type must be 'managed' or 'byoi'")
	}

	return nil
}

func (r *CreateFunctionRequest) validateManaged() error {
	switch r.Runtime {
	case RuntimePython, RuntimeNodeJS:
	default:
		return fmt.Errorf("managed runtime must be 'python' or 'nodejs'")
	}
	if r.Code == "" {
		return fmt.Errorf("code is required for managed deploy type")
	}
	if len(r.Code) > 1_000_000 {
		return fmt.Errorf("code must be under 1MB")
	}
	return nil
}

func (r *CreateFunctionRequest) validateBYOI() error {
	switch r.Runtime {
	case RuntimePython, RuntimeNodeJS, RuntimeGo:
	default:
		return fmt.Errorf("runtime must be 'python', 'nodejs', or 'go'")
	}
	if r.Image == "" {
		return fmt.Errorf("image is required for byoi deploy type")
	}
	return nil
}
