package model_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"serverless-platform/internal/model"
)

func TestCreateFunctionRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     model.CreateFunctionRequest
		wantErr string
	}{
		{
			name: "valid managed function",
			req: model.CreateFunctionRequest{
				Name:       "hello-world",
				Runtime:    model.RuntimePython,
				DeployType: model.DeployTypeManaged,
				Code:       "def handler(req): return 'hello'",
			},
			wantErr: "",
		},
		{
			name: "valid byoi function",
			req: model.CreateFunctionRequest{
				Name:       "my-service",
				Runtime:    model.RuntimeGo,
				DeployType: model.DeployTypeBYOI,
				Image:      "ghcr.io/user/my-service:latest",
			},
			wantErr: "",
		},
		{
			name: "empty name",
			req: model.CreateFunctionRequest{
				Name:       "",
				Runtime:    model.RuntimePython,
				DeployType: model.DeployTypeManaged,
				Code:       "def handler(req): return 'hello'",
			},
			wantErr: "name is required",
		},
		{
			name: "name too long",
			req: model.CreateFunctionRequest{
				Name:       strings.Repeat("a", 64),
				Runtime:    model.RuntimePython,
				DeployType: model.DeployTypeManaged,
				Code:       "def handler(req): return 'hello'",
			},
			wantErr: "name must be at most 63 characters",
		},
		{
			name: "name with uppercase",
			req: model.CreateFunctionRequest{
				Name:       "Hello",
				Runtime:    model.RuntimePython,
				DeployType: model.DeployTypeManaged,
				Code:       "def handler(req): return 'hello'",
			},
			wantErr: "name must be lowercase",
		},
		{
			name: "name starting with number",
			req: model.CreateFunctionRequest{
				Name:       "1hello",
				Runtime:    model.RuntimePython,
				DeployType: model.DeployTypeManaged,
				Code:       "def handler(req): return 'hello'",
			},
			wantErr: "name must be lowercase",
		},
		{
			name: "invalid deploy type",
			req: model.CreateFunctionRequest{
				Name:       "hello",
				Runtime:    model.RuntimePython,
				DeployType: "invalid",
				Code:       "def handler(req): return 'hello'",
			},
			wantErr: "deploy_type must be",
		},
		{
			name: "managed without code",
			req: model.CreateFunctionRequest{
				Name:       "hello",
				Runtime:    model.RuntimePython,
				DeployType: model.DeployTypeManaged,
			},
			wantErr: "code is required",
		},
		{
			name: "managed with invalid runtime",
			req: model.CreateFunctionRequest{
				Name:       "hello",
				Runtime:    model.RuntimeGo,
				DeployType: model.DeployTypeManaged,
				Code:       "package main",
			},
			wantErr: "managed runtime must be",
		},
		{
			name: "byoi without image",
			req: model.CreateFunctionRequest{
				Name:       "hello",
				Runtime:    model.RuntimeGo,
				DeployType: model.DeployTypeBYOI,
			},
			wantErr: "image is required",
		},
		{
			name: "byoi with invalid runtime",
			req: model.CreateFunctionRequest{
				Name:       "hello",
				Runtime:    "ruby",
				DeployType: model.DeployTypeBYOI,
				Image:      "ghcr.io/user/fn:latest",
			},
			wantErr: "runtime must be",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if tt.wantErr == "" {
				assert.NoError(t, err)
			} else {
				assert.ErrorContains(t, err, tt.wantErr)
			}
		})
	}
}

func TestUpdateFunctionRequest_Validate(t *testing.T) {
	boolPtr := func(b bool) *bool { return &b }

	managed := &model.Function{
		Name:       "hello",
		DeployType: model.DeployTypeManaged,
		Code:       "def handler(req): return 'hello'",
	}

	byoi := &model.Function{
		Name:       "my-svc",
		DeployType: model.DeployTypeBYOI,
		Image:      "ghcr.io/user/svc:latest",
	}

	tests := []struct {
		name     string
		req      model.UpdateFunctionRequest
		existing *model.Function
		wantErr  string
	}{
		{
			name:     "update code on managed",
			req:      model.UpdateFunctionRequest{Code: "def handler(req): return 'updated'"},
			existing: managed,
			wantErr:  "",
		},
		{
			name:     "update image on byoi",
			req:      model.UpdateFunctionRequest{Image: "ghcr.io/user/svc:v2"},
			existing: byoi,
			wantErr:  "",
		},
		{
			name:     "update public flag",
			req:      model.UpdateFunctionRequest{Public: boolPtr(true)},
			existing: managed,
			wantErr:  "",
		},
		{
			name:     "empty update",
			req:      model.UpdateFunctionRequest{},
			existing: managed,
			wantErr:  "at least one field",
		},
		{
			name:     "set image on managed",
			req:      model.UpdateFunctionRequest{Image: "ghcr.io/user/fn:latest"},
			existing: managed,
			wantErr:  "cannot change image for managed",
		},
		{
			name:     "set code on byoi",
			req:      model.UpdateFunctionRequest{Code: "package main"},
			existing: byoi,
			wantErr:  "cannot set code for byoi",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate(tt.existing)
			if tt.wantErr == "" {
				assert.NoError(t, err)
			} else {
				assert.ErrorContains(t, err, tt.wantErr)
			}
		})
	}
}
