package service

import (
	"context"
	"fmt"
	"log/slog"

	"serverless-platform/internal/auth"
	"serverless-platform/internal/db"
	"serverless-platform/internal/deployer"
	"serverless-platform/internal/model"
	natspkg "serverless-platform/internal/nats"
)

type FunctionService struct {
	db            *db.DB
	deployer      *deployer.KnativeDeployer
	publisher     *natspkg.Publisher
	logger        *slog.Logger
	publicBaseURL string
}

func NewFunctionService(database *db.DB, dep *deployer.KnativeDeployer, pub *natspkg.Publisher, logger *slog.Logger, publicBaseURL string) *FunctionService {
	return &FunctionService{
		db:            database,
		deployer:      dep,
		publisher:     pub,
		logger:        logger,
		publicBaseURL: publicBaseURL,
	}
}

// publicInvokeURL returns the externally advertised URL for a public
// function, or empty string for private functions or when no public base
// URL is configured. The pattern is intentionally distinct from the
// authenticated proxy endpoint (/api/v1/functions/{name}/invoke) — that
// path requires a workspace JWT and is only useful to authenticated
// console users. A public function URL must be callable anonymously from
// outside the platform, so it lives under /fn/{workspace}/{name} where
// the workspace slug disambiguates tenants.
//
// The cluster-internal Knative URL the deployer reports is never returned
// to the client; it isn't reachable from outside the cluster and would
// mislead users into trying to call it directly.
func (s *FunctionService) publicInvokeURL(ctx context.Context, fn *model.Function) string {
	if !fn.Public || s.publicBaseURL == "" {
		return ""
	}
	workspace := auth.WorkspaceSlugFromContext(ctx)
	if workspace == "" {
		return ""
	}
	return s.publicBaseURL + "/fn/" + workspace + "/" + fn.Name
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

func (s *FunctionService) Create(ctx context.Context, req *model.CreateFunctionRequest) (*model.Function, *model.Deployment, error) {
	if err := req.Validate(); err != nil {
		return nil, nil, fmt.Errorf("validation: %w", err)
	}

	wsID, err := s.workspaceID(ctx)
	if err != nil {
		return nil, nil, err
	}

	existing, err := s.db.GetFunction(ctx, wsID, req.Name)
	if err != nil {
		return nil, nil, fmt.Errorf("check existing: %w", err)
	}
	if existing != nil {
		return nil, nil, fmt.Errorf("function %q already exists", req.Name)
	}

	dbFn, err := s.db.CreateFunction(ctx, wsID, req.Name, string(req.Runtime), string(req.DeployType), req.Code, req.Image, req.Public)
	if err != nil {
		return nil, nil, fmt.Errorf("save function: %w", err)
	}

	dep, err := s.db.CreateDeployment(ctx, dbFn.ID, wsID)
	if err != nil {
		return nil, nil, fmt.Errorf("create deployment: %w", err)
	}

	deployReq := &natspkg.DeployRequest{
		DeploymentID: dep.ID,
		WorkspaceID:  wsID,
		FunctionName: req.Name,
		Namespace:    s.namespace(ctx),
		Runtime:      string(req.Runtime),
		DeployType:   string(req.DeployType),
		Code:         req.Code,
		Image:        req.Image,
		Public:       req.Public,
	}
	if err := s.publisher.PublishDeployRequest(ctx, deployReq); err != nil {
		s.logger.Error("failed to publish deploy request", "error", err, "deployment_id", dep.ID)
		_ = s.db.UpdateDeploymentError(ctx, dep.ID, "failed to queue deployment")
		return nil, nil, fmt.Errorf("queue deployment: %w", err)
	}

	fn := dbFunctionToModel(dbFn)
	fn.Status = model.StatusPending

	deployment := &model.Deployment{
		ID:         dep.ID,
		FunctionID: dbFn.ID,
		Status:     model.DeployStatusQueued,
		CreatedAt:  dep.CreatedAt,
	}

	s.logger.Info("deployment queued", "function", req.Name, "deployment_id", dep.ID)
	return fn, deployment, nil
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
	}
	fn.URL = s.publicInvokeURL(ctx, fn)
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
		}
		fn.URL = s.publicInvokeURL(ctx, fn)
		functions = append(functions, fn)
	}

	return functions, total, nil
}

func (s *FunctionService) Update(ctx context.Context, name string, req *model.UpdateFunctionRequest) (*model.Function, *model.Deployment, error) {
	wsID, err := s.workspaceID(ctx)
	if err != nil {
		return nil, nil, err
	}

	dbFn, err := s.db.GetFunction(ctx, wsID, name)
	if err != nil {
		return nil, nil, fmt.Errorf("get function: %w", err)
	}
	if dbFn == nil {
		return nil, nil, nil
	}

	if err := req.Validate(dbFunctionToModel(dbFn)); err != nil {
		return nil, nil, fmt.Errorf("validation: %w", err)
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
		return nil, nil, fmt.Errorf("update function: %w", err)
	}
	if updatedFn == nil {
		return nil, nil, nil
	}

	dep, err := s.db.CreateDeployment(ctx, updatedFn.ID, wsID)
	if err != nil {
		return nil, nil, fmt.Errorf("create deployment: %w", err)
	}

	deployReq := &natspkg.DeployRequest{
		DeploymentID: dep.ID,
		WorkspaceID:  wsID,
		FunctionName: name,
		Namespace:    s.namespace(ctx),
		Runtime:      updatedFn.Runtime,
		DeployType:   updatedFn.DeployType,
		Code:         code,
		Image:        image,
		Public:       public,
	}
	if err := s.publisher.PublishDeployRequest(ctx, deployReq); err != nil {
		s.logger.Error("failed to publish deploy request", "error", err, "deployment_id", dep.ID)
		_ = s.db.UpdateDeploymentError(ctx, dep.ID, "failed to queue deployment")
		return nil, nil, fmt.Errorf("queue deployment: %w", err)
	}

	fn := dbFunctionToModel(updatedFn)
	fn.Status = model.StatusPending
	fn.URL = s.publicInvokeURL(ctx, fn)

	deployment := &model.Deployment{
		ID:         dep.ID,
		FunctionID: updatedFn.ID,
		Status:     model.DeployStatusQueued,
		CreatedAt:  dep.CreatedAt,
	}

	s.logger.Info("deployment queued", "function", name, "deployment_id", dep.ID)
	return fn, deployment, nil
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

// GetPublic looks up a function by (workspaceSlug, name) for the unauthenticated
// public-invoke route. It deliberately collapses "not found" and "exists but
// private" into the same nil return so the handler can emit a single 404 for
// both cases — leaking the existence of private functions to anonymous callers
// would let them enumerate tenants by probing names.
func (s *FunctionService) GetPublic(ctx context.Context, workspaceSlug, name string) (*model.Function, error) {
	ws, err := s.db.GetWorkspaceBySlug(ctx, workspaceSlug)
	if err != nil {
		return nil, fmt.Errorf("get workspace: %w", err)
	}
	if ws == nil {
		return nil, nil
	}

	dbFn, err := s.db.GetFunction(ctx, ws.ID, name)
	if err != nil {
		return nil, err
	}
	if dbFn == nil || !dbFn.Public {
		return nil, nil
	}

	return dbFunctionToModel(dbFn), nil
}

// PublicInvokeTarget returns the cluster-internal Knative URL for a function
// identified by workspace slug. Used by the anonymous /fn/{workspace}/{name}
// proxy, which must construct the namespace without a workspace context.
func (s *FunctionService) PublicInvokeTarget(workspaceSlug, name string) string {
	return s.deployer.InvokeURL("fn-"+workspaceSlug, name)
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
