package service

import (
	"context"
	"fmt"
	"log/slog"

	"serverless-platform/internal/auth"
	"serverless-platform/internal/db"
	"serverless-platform/internal/deployer"
	"serverless-platform/internal/model"
)

type FunctionService struct {
	db       *db.DB
	deployer *deployer.KnativeDeployer
	logger   *slog.Logger
}

func NewFunctionService(database *db.DB, dep *deployer.KnativeDeployer, logger *slog.Logger) *FunctionService {
	return &FunctionService{
		db:       database,
		deployer: dep,
		logger:   logger,
	}
}

func (s *FunctionService) namespace(ctx context.Context) string {
	return "fn-" + auth.WorkspaceSlugFromContext(ctx)
}

func (s *FunctionService) workspaceID(ctx context.Context) (int64, error) {
	slug := auth.WorkspaceSlugFromContext(ctx)
	ws, err := s.db.GetWorkspaceBySlug(ctx, slug)
	if err != nil {
		return 0, fmt.Errorf("get workspace: %w", err)
	}
	if ws == nil {
		return 0, fmt.Errorf("workspace not found")
	}
	return ws.ID, nil
}

func (s *FunctionService) Create(ctx context.Context, req *model.CreateFunctionRequest) (*model.Function, error) {
	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("validation: %w", err)
	}

	wsID, err := s.workspaceID(ctx)
	if err != nil {
		return nil, err
	}

	existing, err := s.db.GetFunction(ctx, wsID, req.Name)
	if err != nil {
		return nil, fmt.Errorf("check existing: %w", err)
	}
	if existing != nil {
		return nil, fmt.Errorf("function %q already exists", req.Name)
	}

	ns := s.namespace(ctx)
	if err := s.deployer.EnsureNamespace(ctx, ns); err != nil {
		return nil, fmt.Errorf("ensure namespace: %w", err)
	}
	if err := s.deployer.EnsureNamespaceRBAC(ctx, ns); err != nil {
		return nil, fmt.Errorf("ensure rbac: %w", err)
	}

	dbFn, err := s.db.CreateFunction(ctx, wsID, req.Name, string(req.Runtime), string(req.DeployType), req.Code, req.Image, req.Public)
	if err != nil {
		return nil, fmt.Errorf("save function: %w", err)
	}

	fn := dbFunctionToModel(dbFn)
	url, err := s.deployer.Deploy(ctx, ns, fn)
	if err != nil {
		_ = s.db.DeleteFunction(ctx, wsID, req.Name)
		return nil, fmt.Errorf("deploy: %w", err)
	}

	fn.URL = url
	fn.Status = model.StatusDeploying
	s.logger.Info("function created", "name", fn.Name, "namespace", ns)
	return fn, nil
}

func (s *FunctionService) Get(ctx context.Context, name string) (*model.Function, error) {
	wsID, err := s.workspaceID(ctx)
	if err != nil {
		return nil, err
	}

	dbFn, err := s.db.GetFunction(ctx, wsID, name)
	if err != nil {
		return nil, err
	}
	if dbFn == nil {
		return nil, nil
	}

	fn := dbFunctionToModel(dbFn)
	if kFn, _ := s.deployer.Get(ctx, s.namespace(ctx), name); kFn != nil {
		fn.Status = kFn.Status
		fn.URL = kFn.URL
	}
	return fn, nil
}

// ListParams holds pagination and filter parameters for the function service.
type ListParams struct {
	Limit      int
	Offset     int
	Sort       string
	Order      string
	Runtime    string
	DeployType string
}

func (s *FunctionService) List(ctx context.Context, params ListParams) ([]*model.Function, int, error) {
	wsID, err := s.workspaceID(ctx)
	if err != nil {
		return nil, 0, err
	}

	dbFunctions, total, err := s.db.ListFunctions(ctx, db.ListFunctionsParams{
		WorkspaceID: wsID,
		Limit:       params.Limit,
		Offset:      params.Offset,
		Sort:        params.Sort,
		Order:       params.Order,
		Runtime:     params.Runtime,
		DeployType:  params.DeployType,
	})
	if err != nil {
		return nil, 0, err
	}

	ns := s.namespace(ctx)
	functions := make([]*model.Function, 0, len(dbFunctions))
	for _, f := range dbFunctions {
		fn := dbFunctionToModel(f)
		if ksvc, err := s.deployer.Get(ctx, ns, f.Name); err == nil && ksvc != nil {
			fn.Status = ksvc.Status
			fn.URL = ksvc.URL
		}
		functions = append(functions, fn)
	}

	return functions, total, nil
}

func (s *FunctionService) Update(ctx context.Context, name string, req *model.UpdateFunctionRequest) (*model.Function, error) {
	wsID, err := s.workspaceID(ctx)
	if err != nil {
		return nil, err
	}

	dbFn, err := s.db.GetFunction(ctx, wsID, name)
	if err != nil {
		return nil, fmt.Errorf("get function: %w", err)
	}
	if dbFn == nil {
		return nil, nil
	}

	if err := req.Validate(dbFunctionToModel(dbFn)); err != nil {
		return nil, fmt.Errorf("validation: %w", err)
	}

	code := dbFn.Code
	image := dbFn.Image
	public := dbFn.Public
	if req.Code != "" {
		code = req.Code
	}
	if req.Image != "" {
		image = req.Image
	}
	if req.Public != nil {
		public = *req.Public
	}

	updatedFn, err := s.db.UpdateFunction(ctx, wsID, name, code, image, public)
	if err != nil {
		return nil, fmt.Errorf("update function: %w", err)
	}

	fn := dbFunctionToModel(updatedFn)
	url, err := s.deployer.Deploy(ctx, s.namespace(ctx), fn)
	if err != nil {
		return nil, fmt.Errorf("redeploy: %w", err)
	}

	fn.URL = url
	fn.Status = model.StatusDeploying
	s.logger.Info("function updated", "name", name)
	return fn, nil
}

func (s *FunctionService) Delete(ctx context.Context, name string) error {
	wsID, err := s.workspaceID(ctx)
	if err != nil {
		return err
	}

	if err := s.db.DeleteFunction(ctx, wsID, name); err != nil {
		return fmt.Errorf("delete function record: %w", err)
	}

	if err := s.deployer.Delete(ctx, s.namespace(ctx), name); err != nil {
		s.logger.Warn("failed to delete knative service", "name", name, "error", err)
	}

	s.logger.Info("function deleted", "name", name)
	return nil
}

func (s *FunctionService) InvokeURL(ctx context.Context, name string) string {
	return s.deployer.InvokeURL(s.namespace(ctx), name)
}

func (s *FunctionService) Logs(ctx context.Context, name string, tail int64) (string, error) {
	return s.deployer.Logs(ctx, s.namespace(ctx), name, tail)
}

func dbFunctionToModel(f *db.Function) *model.Function {
	return &model.Function{
		Name:       f.Name,
		Runtime:    model.Runtime(f.Runtime),
		DeployType: model.DeployType(f.DeployType),
		Code:       f.Code,
		Image:      f.Image,
		Public:     f.Public,
		Status:     model.StatusPending,
		CreatedAt:  f.CreatedAt,
		UpdatedAt:  f.UpdatedAt,
	}
}
