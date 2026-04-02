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
	JWTExpiry     time.Duration
	CORSOrigins   []string
	NatsURL       string
	NatsToken     string
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

	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		natsURL = "nats://nats.serverless-platform.svc.cluster.local:4222"
	}

	return &Config{
		Environment:   env,
		Port:          port,
		PlatformNS:    platformNS,
		DatabaseDSN:   os.Getenv("DATABASE_DSN"),
		PrometheusURL: prometheusURL,
		JWTExpiry:     24 * time.Hour,
		CORSOrigins:   corsOrigins,
		NatsURL:       natsURL,
		NatsToken:     os.Getenv("NATS_TOKEN"),
	}
}
