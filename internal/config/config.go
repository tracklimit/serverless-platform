package config

import "os"

type Config struct {
	Port      string
	Namespace string
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

	return &Config{
		Port:      port,
		Namespace: ns,
	}
}
