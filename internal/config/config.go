package config

import (
	"os"
	"strings"
	"time"
)

type Config struct {
	Environment   string
	Port          string
	PlatformNS    string
	DatabaseDSN   string
	PrometheusURL string
	LokiURL       string
	JWTExpiry     time.Duration
	CORSOrigins   []string
	NatsURL       string
	NatsToken     string
	OTLPEndpoint  string
}

func Load() *Config {
	env := os.Getenv("ENVIRONMENT")
	if env == "" {
		env = "development"
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	platformNS := os.Getenv("PLATFORM_NAMESPACE")
	if platformNS == "" {
		platformNS = "serverless-platform"
	}

	corsOrigins := []string{"*"}
	if origins := os.Getenv("CORS_ORIGINS"); origins != "" {
		corsOrigins = strings.Split(origins, ",")
	}

	prometheusURL := os.Getenv("PROMETHEUS_URL")
	if prometheusURL == "" {
		prometheusURL = "http://prometheus-operated.monitoring.svc.cluster.local:9090"
	}

	lokiURL := os.Getenv("LOKI_URL")
	if lokiURL == "" {
		lokiURL = "http://loki.monitoring.svc.cluster.local:3100"
	}

	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		natsURL = "nats://nats.serverless-platform.svc.cluster.local:4222"
	}

	otlpEndpoint := os.Getenv("OTLP_ENDPOINT")
	if otlpEndpoint == "" {
		otlpEndpoint = "tempo.monitoring.svc.cluster.local:4317"
	}

	return &Config{
		Environment:   env,
		Port:          port,
		PlatformNS:    platformNS,
		DatabaseDSN:   os.Getenv("DATABASE_DSN"),
		PrometheusURL: prometheusURL,
		LokiURL:       lokiURL,
		JWTExpiry:     24 * time.Hour,
		CORSOrigins:   corsOrigins,
		NatsURL:       natsURL,
		NatsToken:     os.Getenv("NATS_TOKEN"),
		OTLPEndpoint:  otlpEndpoint,
	}
}
