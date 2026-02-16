package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	servingclient "knative.dev/serving/pkg/client/clientset/versioned"

	"serverless-platform/internal/config"
	"serverless-platform/internal/deployer"
	"serverless-platform/internal/handler"
	"serverless-platform/internal/service"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg := config.Load()

	k8sCfg, err := kubeConfig()
	if err != nil {
		logger.Error("failed to load kubernetes config", "error", err)
		os.Exit(1)
	}

	kubeClient, err := kubernetes.NewForConfig(k8sCfg)
	if err != nil {
		logger.Error("failed to create kubernetes client", "error", err)
		os.Exit(1)
	}

	servingClient, err := servingclient.NewForConfig(k8sCfg)
	if err != nil {
		logger.Error("failed to create knative serving client", "error", err)
		os.Exit(1)
	}

	dep := deployer.NewKnativeDeployer(kubeClient, servingClient, cfg.Namespace)
	svc := service.NewFunctionService(dep, logger)

	healthHandler := handler.NewHealthHandler(kubeClient)
	functionHandler := handler.NewFunctionHandler(svc)

	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)

	r.Get("/healthz", healthHandler.Healthz)
	r.Get("/readyz", healthHandler.Readyz)
	r.Mount("/api/v1/functions", functionHandler.Routes())

	server := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	go func() {
		logger.Info("starting server", "port", cfg.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-done
	logger.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown error", "error", err)
	}
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
