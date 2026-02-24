package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"golang.org/x/crypto/bcrypt"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	servingclient "knative.dev/serving/pkg/client/clientset/versioned"

	"serverless-platform/internal/auth"
	"serverless-platform/internal/config"
	"serverless-platform/internal/db"
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

	database, err := db.New(cfg.DatabaseDSN)
	if err != nil {
		logger.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer func() { _ = database.Close() }()

	if err := db.RunMigrations(database); err != nil {
		logger.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}

	if err := migrateAdminFromSecret(context.Background(), kubeClient, database, cfg.PlatformNS, logger); err != nil {
		logger.Error("failed to migrate admin credentials", "error", err)
		os.Exit(1)
	}

	signingKey, err := loadOrCreateSigningKey(kubeClient, cfg.PlatformNS)
	if err != nil {
		logger.Error("failed to load JWT signing key", "error", err)
		os.Exit(1)
	}

	tokenService := auth.NewTokenService(signingKey, cfg.JWTExpiry)

	dep := deployer.NewKnativeDeployer(kubeClient, servingClient, cfg.PlatformNS)
	svc := service.NewFunctionService(database, dep, logger)

	healthHandler := handler.NewHealthHandler(kubeClient)
	authHandler := handler.NewAuthHandler(database, tokenService)
	userHandler := handler.NewUserHandler(database)
	workspaceHandler := handler.NewWorkspaceHandler(database)
	functionHandler := handler.NewFunctionHandler(svc)
	metricsHandler := handler.NewMetricsHandler(cfg.PrometheusURL)

	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   cfg.CORSOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Authorization", "Content-Type"},
		ExposedHeaders:   []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Public routes
	r.Get("/healthz", healthHandler.Healthz)
	r.Get("/readyz", healthHandler.Readyz)
	r.Post("/api/v1/auth/login", authHandler.Login)

	// Authenticated routes
	r.Group(func(r chi.Router) {
		r.Use(auth.Middleware(tokenService))
		r.Post("/api/v1/auth/change-password", authHandler.ChangePassword)

		// Admin-only routes
		r.Group(func(r chi.Router) {
			r.Use(auth.AdminOnly)
			r.Mount("/api/v1/users", userHandler.Routes())
			r.Mount("/api/v1/workspaces", workspaceHandler.AdminRoutes())
		})

		// Workspace-scoped routes
		r.Group(func(r chi.Router) {
			r.Use(auth.WorkspaceRequired)
			r.Mount("/api/v1/functions", functionHandler.Routes(metricsHandler))
			r.Mount("/api/v1/workspace", workspaceHandler.OwnerRoutes())
		})
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

// migrateAdminFromSecret seeds the first admin user from the K8s Secret (or
// defaults to admin/admin) when no users exist in the database yet.
func migrateAdminFromSecret(ctx context.Context, kubeClient kubernetes.Interface, database *db.DB, namespace string, logger *slog.Logger) error {
	has, err := database.HasAnyUser(ctx)
	if err != nil {
		return fmt.Errorf("check users: %w", err)
	}
	if has {
		return nil // already seeded
	}

	var passwordHash string

	secret, err := kubeClient.CoreV1().Secrets(namespace).Get(ctx, "platform-credentials", metav1.GetOptions{})
	if err == nil {
		if h, ok := secret.Data["password-hash"]; ok && len(h) > 0 {
			passwordHash = string(h)
			logger.Info("migrated admin user from K8s Secret to PostgreSQL")
		}
	} else if !k8serrors.IsNotFound(err) {
		return fmt.Errorf("read platform-credentials secret: %w", err)
	}

	if passwordHash == "" {
		hash, err := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.DefaultCost)
		if err != nil {
			return fmt.Errorf("hash password: %w", err)
		}
		passwordHash = string(hash)
		logger.Warn("no existing credentials found — seeding admin/admin (change immediately)")
	}

	_, err = database.CreateUser(ctx, "admin", passwordHash, true)
	return err
}
