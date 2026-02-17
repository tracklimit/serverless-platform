package config

import (
	"os"
	"time"
)

type Config struct {
	Port       string
	Namespace  string
	PlatformNS string
	JWTExpiry  time.Duration
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

	return &Config{
		Port:       port,
		Namespace:  ns,
		PlatformNS: platformNS,
		JWTExpiry:  24 * time.Hour,
	}
}
