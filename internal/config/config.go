package config

import (
	"os"
	"strings"
	"time"
)

type Config struct {
	Port          string
	PlatformNS    string
	DatabaseDSN   string
	PrometheusURL string
	JWTExpiry     time.Duration
	CORSOrigins   []string
}

func Load() *Config {
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

	return &Config{
		Port:          port,
		PlatformNS:    platformNS,
		DatabaseDSN:   os.Getenv("DATABASE_DSN"),
		PrometheusURL: prometheusURL,
		JWTExpiry:     24 * time.Hour,
		CORSOrigins:   corsOrigins,
	}
}
