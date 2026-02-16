package service

import (
	"context"
	"fmt"
	"log/slog"

	"serverless-platform/internal/deployer"
	"serverless-platform/internal/model"
)

type FunctionService struct {
	deployer *deployer.KnativeDeployer
	logger   *slog.Logger
}

func NewFunctionService(deployer *deployer.KnativeDeployer, logger *slog.Logger) *FunctionService {
	return &FunctionService{
		deployer: deployer,
		logger:   logger,
	}
}

func (s *FunctionService) Create(ctx context.Context, req *model.CreateFunctionRequest) (*model.Function, error) {
	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("validation: %w", err)
	}

	existing, err := s.deployer.Get(ctx, req.Name)
	if err != nil {
		return nil, fmt.Errorf("check existing: %w", err)
	}
	if existing != nil {
		return nil, fmt.Errorf("function %q already exists", req.Name)
	}

	fn := &model.Function{
		Name:       req.Name,
		Runtime:    req.Runtime,
		DeployType: req.DeployType,
		Code:       req.Code,
		Image:      req.Image,
	}

	url, err := s.deployer.Deploy(ctx, fn)
	if err != nil {
		return nil, fmt.Errorf("deploy: %w", err)
	}

	fn.URL = url
	fn.Status = model.StatusDeploying
	s.logger.Info("function created", "name", fn.Name, "deploy_type", fn.DeployType)

	return fn, nil
}

func (s *FunctionService) Get(ctx context.Context, name string) (*model.Function, error) {
	return s.deployer.Get(ctx, name)
}

func (s *FunctionService) List(ctx context.Context) ([]*model.Function, error) {
	return s.deployer.List(ctx)
}

func (s *FunctionService) Update(ctx context.Context, name string, req *model.UpdateFunctionRequest) (*model.Function, error) {
	existing, err := s.deployer.Get(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("get function: %w", err)
	}
	if existing == nil {
		return nil, nil
	}

	if req.Code != "" {
		existing.Code = req.Code
	}
	if req.Image != "" {
		existing.Image = req.Image
	}

	url, err := s.deployer.Deploy(ctx, existing)
	if err != nil {
		return nil, fmt.Errorf("redeploy: %w", err)
	}

	existing.URL = url
	existing.Status = model.StatusDeploying
	s.logger.Info("function updated", "name", name)

	return existing, nil
}

func (s *FunctionService) Delete(ctx context.Context, name string) error {
	if err := s.deployer.Delete(ctx, name); err != nil {
		return fmt.Errorf("delete: %w", err)
	}

	s.logger.Info("function deleted", "name", name)
	return nil
}
