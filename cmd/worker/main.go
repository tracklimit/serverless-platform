package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"serverless-platform/internal/config"
	"serverless-platform/internal/db"
	"serverless-platform/internal/deployer"
	"serverless-platform/internal/logging"
	natspkg "serverless-platform/internal/nats"
	"serverless-platform/internal/shutdown"
	"serverless-platform/internal/worker"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	servingclient "knative.dev/serving/pkg/client/clientset/versioned"
)

func main() {
	cfg := config.Load()
	logger := logging.New(cfg.Environment)
	slog.SetDefault(logger)

	slog.Info("starting deploy worker", "environment", cfg.Environment)

	k8sCfg, err := kubeConfig()
	if err != nil {
		slog.Error("failed to load kubernetes config", "error", err)
		os.Exit(1)
	}

	kubeClient, err := kubernetes.NewForConfig(k8sCfg)
	if err != nil {
		slog.Error("failed to create kubernetes client", "error", err)
		os.Exit(1)
	}

	servingClient, err := servingclient.NewForConfig(k8sCfg)
	if err != nil {
		slog.Error("failed to create knative serving client", "error", err)
		os.Exit(1)
	}

	database, err := db.New(cfg.DatabaseDSN)
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}

	natsClient, err := natspkg.NewClient(cfg.NatsURL, cfg.NatsToken)
	if err != nil {
		slog.Error("failed to connect to nats", "error", err)
		os.Exit(1)
	}

	dep := deployer.NewKnativeDeployer(kubeClient, servingClient, cfg.PlatformNS)
	pub := natspkg.NewPublisher(natsClient)

	w := worker.NewDeployWorker(database, dep, natsClient, pub, logger)

	sm := shutdown.NewManager(shutdown.DefaultTimeout)
	sm.Register("database", database)
	sm.Register("nats", natsClient)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := w.Start(ctx); err != nil && ctx.Err() == nil {
			slog.Error("worker error", "error", err)
			os.Exit(1)
		}
	}()

	// Block until shutdown signal. The worker's consume loop checks ctx.Done().
	// The shutdown manager drains HTTP (not applicable here) then closes resources.
	// For the worker, we need a slightly different shutdown flow.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
	<-quit

	slog.Info("shutdown signal received, draining in-flight deploys")
	cancel() // signals the consume loop to stop

	// Give in-flight deploys time to finish
	time.Sleep(5 * time.Second)

	if err := natsClient.Close(); err != nil {
		slog.Error("nats close error", "error", err)
	}
	if err := database.Close(); err != nil {
		slog.Error("database close error", "error", err)
	}

	slog.Info("worker shutdown complete")
}

func kubeConfig() (*rest.Config, error) {
	cfg, err := rest.InClusterConfig()
	if err == nil {
		return cfg, nil
	}
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		kubeconfig = os.Getenv("HOME") + "/.kube/config"
	}
	return clientcmd.BuildConfigFromFlags("", kubeconfig)
}
