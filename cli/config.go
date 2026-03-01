package main

import (
	"os"
)

// ── Configuration ──────────────────────────────────────────────────────────────

const (
	defaultURL  = "http://localhost:30800"
	defaultPort = "30800"
	k8sNS       = "hive"
	k8sSvc      = "service/hive-orchestrator"
	k8sSvcPort  = "9000"
)

// envOr returns the value of an environment variable, or a fallback.
func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
