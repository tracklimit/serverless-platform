package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"serverless-platform/internal/db"
	"serverless-platform/internal/model"
	natspkg "serverless-platform/internal/nats"
)

type store interface {
	UpdateDeploymentStatus(ctx context.Context, id int64, status string) error
	UpdateDeploymentError(ctx context.Context, id int64, errMsg string) error
	ListActiveDeployments(ctx context.Context) ([]*db.Deployment, error)
	GetFunction(ctx context.Context, workspaceID int64, name string) (*db.Function, error)
	GetWorkspaceByID(ctx context.Context, id int64) (*db.Workspace, error)
}

type deployService interface {
	EnsureNamespace(ctx context.Context, namespace string) error
	EnsureNamespaceRBAC(ctx context.Context, namespace string) error
	Deploy(ctx context.Context, namespace string, fn *model.Function) (string, error)
}

type statusPublisher interface {
	PublishDeployStatus(ctx context.Context, status *natspkg.DeployStatusEvent) error
}

type DeployWorker struct {
	db        store
	deployer  deployService
	nats      *natspkg.Client
	publisher statusPublisher
	logger    *slog.Logger
	consumer  jetstream.Consumer
}

func NewDeployWorker(
	database *db.DB,
	dep deployService,
	natsClient *natspkg.Client,
	pub *natspkg.Publisher,
	logger *slog.Logger,
) *DeployWorker {
	return &DeployWorker{
		db:        database,
		deployer:  dep,
		nats:      natsClient,
		publisher: pub,
		logger:    logger,
	}
}

func (w *DeployWorker) Start(ctx context.Context) error {
	stream, err := w.nats.EnsureStream(ctx)
	if err != nil {
		return fmt.Errorf("ensure stream: %w", err)
	}

	consumer, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:       natspkg.ConsumerDeploy,
		AckPolicy:     jetstream.AckExplicitPolicy,
		FilterSubject: natspkg.SubjectDeployRequest,
		MaxDeliver:    4, // 1 initial + 3 retries
		AckWait:       2 * time.Minute,
		BackOff: []time.Duration{
			5 * time.Second,
			15 * time.Second,
			60 * time.Second,
		},
	})
	if err != nil {
		return fmt.Errorf("create consumer: %w", err)
	}
	w.consumer = consumer

	// Recovery sweep: resume any deployments that were in-progress when the previous
	// worker instance terminated. These are database rows with active status but no
	// corresponding NATS message (the message was already acked or the worker crashed
	// before acking).
	if err := w.recoverActiveDeployments(ctx); err != nil {
		w.logger.Error("recovery sweep failed", "error", err)
	}

	w.logger.Info("deploy worker started, consuming messages")
	return w.consume(ctx)
}

func (w *DeployWorker) consume(ctx context.Context) error {
	for {
		msg, err := w.consumer.Next(jetstream.FetchMaxWait(10 * time.Second))
		if err != nil {
			if err := ctx.Err(); err != nil {
				return err
			}
			w.logger.Debug("consumer fetch error", "error", err)
			continue
		}
		w.handleMessage(ctx, msg)
	}
}

func (w *DeployWorker) handleMessage(ctx context.Context, msg jetstream.Msg) {
	var req natspkg.DeployRequest
	if err := json.Unmarshal(msg.Data(), &req); err != nil {
		w.logger.Error("invalid deploy request", "error", err)
		_ = msg.Term() // unrecoverable, do not retry
		return
	}

	logger := w.logger.With("deployment_id", req.DeploymentID, "function", req.FunctionName)
	logger.Info("processing deploy request")

	if err := w.executeDeploy(ctx, &req); err != nil {
		meta, _ := msg.Metadata()
		if meta == nil {
			logger.Warn("deploy failed, metadata unavailable", "error", err)
			_ = msg.Nak()
			return
		}
		if meta.NumDelivered >= 4 {
			logger.Error("deploy failed after max retries, sending to dead letter", "error", err)
			_ = w.db.UpdateDeploymentError(ctx, req.DeploymentID, err.Error())
			w.publishStatus(ctx, &req, string(model.DeployStatusFailed), "", err.Error())
			_ = msg.Term()
			return
		}
		logger.Warn("deploy failed, will retry", "error", err, "attempt", meta.NumDelivered)
		_ = msg.Nak()
		return
	}

	_ = msg.Ack()
}

func (w *DeployWorker) executeDeploy(ctx context.Context, req *natspkg.DeployRequest) error {
	// Transition: QUEUED → BUILDING
	_ = w.db.UpdateDeploymentStatus(ctx, req.DeploymentID, string(model.DeployStatusBuilding))
	w.publishStatus(ctx, req, string(model.DeployStatusBuilding), "", "")

	if err := w.deployer.EnsureNamespace(ctx, req.Namespace); err != nil {
		return fmt.Errorf("ensure namespace: %w", err)
	}
	if err := w.deployer.EnsureNamespaceRBAC(ctx, req.Namespace); err != nil {
		return fmt.Errorf("ensure rbac: %w", err)
	}

	// Transition: BUILDING → DEPLOYING
	_ = w.db.UpdateDeploymentStatus(ctx, req.DeploymentID, string(model.DeployStatusDeploying))
	w.publishStatus(ctx, req, string(model.DeployStatusDeploying), "", "")

	fn := &model.Function{
		Name:       req.FunctionName,
		Runtime:    model.Runtime(req.Runtime),
		DeployType: model.DeployType(req.DeployType),
		Code:       req.Code,
		Image:      req.Image,
		Public:     req.Public,
	}

	url, err := w.deployer.Deploy(ctx, req.Namespace, fn)
	if err != nil {
		return fmt.Errorf("deploy to knative: %w", err)
	}

	// Transition: DEPLOYING → RUNNING
	_ = w.db.UpdateDeploymentStatus(ctx, req.DeploymentID, string(model.DeployStatusRunning))
	w.publishStatus(ctx, req, string(model.DeployStatusRunning), url, "")

	w.logger.Info("deployment completed", "deployment_id", req.DeploymentID, "url", url)
	return nil
}

func (w *DeployWorker) publishStatus(ctx context.Context, req *natspkg.DeployRequest, status, url, errMsg string) {
	_ = w.publisher.PublishDeployStatus(ctx, &natspkg.DeployStatusEvent{
		DeploymentID: req.DeploymentID,
		FunctionName: req.FunctionName,
		Status:       status,
		URL:          url,
		Error:        errMsg,
	})
}

func (w *DeployWorker) recoverActiveDeployments(ctx context.Context) error {
	active, err := w.db.ListActiveDeployments(ctx)
	if err != nil {
		return err
	}
	if len(active) == 0 {
		return nil
	}

	w.logger.Info("recovering active deployments", "count", len(active))
	for _, dep := range active {
		ws, err := w.db.GetWorkspaceByID(ctx, dep.WorkspaceID)
		if err != nil || ws == nil {
			w.logger.Error("cannot recover deployment: workspace not found",
				"deployment_id", dep.ID, "workspace_id", dep.WorkspaceID)
			_ = w.db.UpdateDeploymentError(ctx, dep.ID, "workspace not found during recovery")
			continue
		}

		// Re-fetch the function to build a deploy request
		fn, err := w.db.GetFunction(ctx, dep.WorkspaceID, dep.FunctionName)
		if err != nil || fn == nil {
			w.logger.Error("cannot recover deployment: function not found",
				"deployment_id", dep.ID, "function", dep.FunctionName)
			_ = w.db.UpdateDeploymentError(ctx, dep.ID, "function not found during recovery")
			continue
		}

		req := &natspkg.DeployRequest{
			DeploymentID: dep.ID,
			WorkspaceID:  dep.WorkspaceID,
			FunctionName: dep.FunctionName,
			Namespace:    "fn-" + ws.Slug,
			Runtime:      fn.Runtime,
			DeployType:   fn.DeployType,
			Code:         fn.Code,
			Image:        fn.Image,
			Public:       fn.Public,
		}

		if err := w.executeDeploy(ctx, req); err != nil {
			w.logger.Error("recovery deploy failed", "deployment_id", dep.ID, "error", err)
			_ = w.db.UpdateDeploymentError(ctx, dep.ID, "recovery failed: "+err.Error())
		}
	}
	return nil
}
