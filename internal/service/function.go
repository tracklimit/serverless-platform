package service

import (
	"context"
	"fmt"
	"log/slog"

	"serverless-platform/internal/auth"
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

func (s *FunctionService) namespace(ctx context.Context) string {
	return "fn-" + auth.WorkspaceSlugFromContext(ctx)
}

func (s *FunctionService) Create(ctx context.Context, req *model.CreateFunctionRequest) (*model.Function, error) {
	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("validation: %w", err)
	}

	ns := s.namespace(ctx)

	if err := s.deployer.EnsureNamespace(ctx, ns); err != nil {
		return nil, fmt.Errorf("ensure namespace: %w", err)
	}
	if err := s.deployer.EnsureNamespaceRBAC(ctx, ns); err != nil {
		return nil, fmt.Errorf("ensure rbac: %w", err)
	}

	existing, err := s.deployer.Get(ctx, ns, req.Name)
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

	url, err := s.deployer.Deploy(ctx, ns, fn)
	if err != nil {
		return nil, fmt.Errorf("deploy: %w", err)
	}

	fn.URL = url
	fn.Status = model.StatusDeploying
	s.logger.Info("function created", "name", fn.Name, "namespace", ns)

	return fn, nil
}

func (s *FunctionService) Get(ctx context.Context, name string) (*model.Function, error) {
	return s.deployer.Get(ctx, s.namespace(ctx), name)
}

func (s *FunctionService) List(ctx context.Context) ([]*model.Function, error) {
	return s.deployer.List(ctx, s.namespace(ctx))
}

func (s *FunctionService) Update(ctx context.Context, name string, req *model.UpdateFunctionRequest) (*model.Function, error) {
	ns := s.namespace(ctx)

	existing, err := s.deployer.Get(ctx, ns, name)
	if err != nil {
		return nil, fmt.Errorf("get function: %w", err)
	}
	if existing == nil {
		return nil, nil
	}

	if err := req.Validate(existing); err != nil {
		return nil, fmt.Errorf("validation: %w", err)
	}

	if req.Code != "" {
		existing.Code = req.Code
	}
	if req.Image != "" {
		existing.Image = req.Image
	}

	url, err := s.deployer.Deploy(ctx, ns, existing)
	if err != nil {
		return nil, fmt.Errorf("redeploy: %w", err)
	}

	existing.URL = url
	existing.Status = model.StatusDeploying
	s.logger.Info("function updated", "name", name, "namespace", ns)

	return existing, nil
}

func (s *FunctionService) InvokeURL(ctx context.Context, name string) string {
	return s.deployer.InvokeURL(s.namespace(ctx), name)
}

func (s *FunctionService) Logs(ctx context.Context, name string, tail int64) (string, error) {
	return s.deployer.Logs(ctx, s.namespace(ctx), name, tail)
}

func (s *FunctionService) Delete(ctx context.Context, name string) error {
	if err := s.deployer.Delete(ctx, s.namespace(ctx), name); err != nil {
		return fmt.Errorf("delete: %w", err)
	}
	s.logger.Info("function deleted", "name", name)
	return nil
}
