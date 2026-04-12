package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"serverless-platform/internal/logging"
	"serverless-platform/internal/shutdown"
	"time"

	platformMetrics "serverless-platform/internal/metrics"
	customMiddleware "serverless-platform/internal/middleware"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/crypto/bcrypt"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	servingclient "knative.dev/serving/pkg/client/clientset/versioned"

	"serverless-platform/internal/auth"
	"serverless-platform/internal/cache"
	"serverless-platform/internal/config"
	"serverless-platform/internal/db"
	"serverless-platform/internal/deployer"
	"serverless-platform/internal/handler"
	natspkg "serverless-platform/internal/nats"
	"serverless-platform/internal/ratelimit"
	"serverless-platform/internal/service"
	"serverless-platform/internal/tracing"
)

func main() {
	cfg := config.Load()

	logger := logging.New(cfg.Environment)
	slog.SetDefault(logger)

	slog.Info("starting platform API", "environment", cfg.Environment, "port", cfg.Port)

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
	if err := db.RunMigrations(database, "migrations"); err != nil {
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

	natsClient, err := natspkg.NewClient(cfg.NatsURL, cfg.NatsToken)
	if err != nil {
		logger.Error("failed to connect to nats", "error", err)
		os.Exit(1)
	}
	if _, err := natsClient.EnsureStream(context.Background()); err != nil {
		logger.Error("failed to ensure nats stream", "error", err)
		os.Exit(1)
	}

	shutdownTracer, err := tracing.Init(context.Background(), "serverless-platform-api", cfg.OTLPEndpoint)
	if err != nil {
		logger.Error("failed to init tracing", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := shutdownTracer(context.Background()); err != nil {
			logger.Error("tracer shutdown error", "error", err)
		}
	}()

	natsPublisher := natspkg.NewPublisher(natsClient)
	dep := deployer.NewKnativeDeployer(kubeClient, servingClient, cfg.PlatformNS)
	svc := service.NewFunctionService(database, dep, natsPublisher, logger)
	cacheStore := cache.NewNoOpStore()
	limiter := ratelimit.NewInMemoryLimiter(60, time.Minute)

	healthHandler := handler.NewHealthHandler(kubeClient)
	authHandler := handler.NewAuthHandler(database, tokenService)
	userHandler := handler.NewUserHandler(database)
	workspaceHandler := handler.NewWorkspaceHandler(database, dep)
	functionHandler := handler.NewFunctionHandler(svc, cacheStore)
	deploymentHandler := handler.NewDeploymentHandler(database)
	metricsHandler := handler.NewMetricsHandler(cfg.PrometheusURL)
	logsHandler := handler.NewLogsHandler(cfg.LokiURL, cfg.PlatformNS, database)

	r := chi.NewRouter()
	r.Use(tracing.HTTP)
	r.Use(platformMetrics.HTTP)
	r.Use(customMiddleware.RequestID)
	r.Use(customMiddleware.Logger(logger, "/health", "/readyz"))
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
		r.Use(customMiddleware.RateLimit(limiter))
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

			// Cached sub-group: idempotent GETs safe to serve from the cache.
			r.Group(func(r chi.Router) {
				r.Use(customMiddleware.Cache(cacheStore, 30*time.Second))
				r.Mount("/api/v1/functions", functionHandler.Routes(metricsHandler))
				r.Mount("/api/v1/workspace", workspaceHandler.OwnerRoutes())
				r.Get("/api/v1/deploys/{id}", deploymentHandler.Get)
				r.Get("/api/v1/functions/{name}/deploys", deploymentHandler.ListByFunction)
				r.Get("/api/v1/metrics/query_range", metricsHandler.QueryRange)
			})

			// Streaming sub-group: SSE responses must not be cached or buffered.
			r.Get("/api/v1/logs/stream", logsHandler.Stream)
		})
	})

	server := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.Handler())
		slog.Info("starting metrics server", "port", "9090")
		if err := http.ListenAndServe(":9090", mux); err != nil {
			slog.Error("metrics server error", "error", err)
		}
	}()

	sm := shutdown.NewManager(shutdown.DefaultTimeout)
	sm.Register("database", database)
	sm.Register("nats", natsClient)

	go func() {
		slog.Info("starting server", "port", cfg.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	if err := sm.Wait(server); err != nil {
		slog.Error("shutdown timed out", "error", err)
		os.Exit(1)
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
	secretName := "jwt-signing-key" //nolint:gosec // K8s Secret resource name, not a credential

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
