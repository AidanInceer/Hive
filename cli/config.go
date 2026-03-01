package main

import "os"

const (
	defaultURL  = "http://localhost:30800"
	defaultPort = "30800"
	k8sNS       = "hive"
	k8sSvc      = "service/hive-orchestrator"
	k8sSvcPort  = "9000"
)

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
