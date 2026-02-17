package config

import (
	"os"
	"strings"
	"time"
)

type Config struct {
	Port        string
	Namespace   string
	PlatformNS  string
	JWTExpiry   time.Duration
	CORSOrigins []string
}

func Load() *Config {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	ns := os.Getenv("FUNCTIONS_NAMESPACE")
	if ns == "" {
		ns = "functions"
	}

	platformNS := os.Getenv("PLATFORM_NAMESPACE")
	if platformNS == "" {
		platformNS = "serverless-platform"
	}

	corsOrigins := []string{"*"}
	if origins := os.Getenv("CORS_ORIGINS"); origins != "" {
		corsOrigins = strings.Split(origins, ",")
	}

	return &Config{
		Port:        port,
		Namespace:   ns,
		PlatformNS:  platformNS,
		JWTExpiry:   24 * time.Hour,
		CORSOrigins: corsOrigins,
	}
}
