package main

import (
	"context"
	"crypto/rand"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	servingclient "knative.dev/serving/pkg/client/clientset/versioned"

	"serverless-platform/internal/auth"
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

	signingKey, err := loadOrCreateSigningKey(kubeClient, cfg.PlatformNS)
	if err != nil {
		logger.Error("failed to load JWT signing key", "error", err)
		os.Exit(1)
	}

	tokenService := auth.NewTokenService(signingKey, cfg.JWTExpiry)

	dep := deployer.NewKnativeDeployer(kubeClient, servingClient, cfg.Namespace)
	svc := service.NewFunctionService(dep, logger)

	healthHandler := handler.NewHealthHandler(kubeClient)
	authHandler := handler.NewAuthHandler(kubeClient, tokenService, cfg.PlatformNS)
	functionHandler := handler.NewFunctionHandler(svc)

	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)

	// Public routes
	r.Get("/healthz", healthHandler.Healthz)
	r.Get("/readyz", healthHandler.Readyz)
	r.Post("/api/v1/auth/login", authHandler.Login)

	// Protected routes
	r.Group(func(r chi.Router) {
		r.Use(auth.Middleware(tokenService))
		r.Mount("/api/v1/functions", functionHandler.Routes())
	})

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

func loadOrCreateSigningKey(kubeClient kubernetes.Interface, namespace string) ([]byte, error) {
	ctx := context.Background()
	secretName := "jwt-signing-key"

	secret, err := kubeClient.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err == nil {
		if key, ok := secret.Data["key"]; ok && len(key) > 0 {
			return key, nil
		}
	}

	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}

	secret = &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
		},
		Data: map[string][]byte{
			"key": key,
		},
	}

	_, err = kubeClient.CoreV1().Secrets(namespace).Create(ctx, secret, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}

	return key, nil
}
