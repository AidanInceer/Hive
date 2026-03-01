package main

import (
	"fmt"
	"net"
	"os/exec"
	"time"
)

// ── Auto-tunnel ────────────────────────────────────────────────────────────────
// If the orchestrator isn't already reachable, start kubectl port-forward in
// the background so the user never has to manage tunnels manually.

// portOpen does a quick TCP dial to check if something is listening.
func portOpen(host, port string, timeout time.Duration) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), timeout)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// ensureTunnel starts kubectl port-forward if nothing is listening on the
// local port.  Returns a cleanup func and any error.
func ensureTunnel() (cleanup func(), err error) {
	noop := func() {}

	// Already reachable — nothing to do.
	if portOpen("127.0.0.1", defaultPort, 500*time.Millisecond) {
		return noop, nil
	}

	// Check kubectl is available.
	kubectlPath, err := exec.LookPath("kubectl")
	if err != nil {
		return noop, fmt.Errorf("orchestrator not reachable on port %s and kubectl not found on PATH", defaultPort)
	}

	// Start port-forward in the background.
	cmd := exec.Command(kubectlPath,
		"port-forward", k8sSvc,
		defaultPort+":"+k8sSvcPort,
		"-n", k8sNS,
	)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return noop, fmt.Errorf("failed to start port-forward: %w", err)
	}

	// Wait for the port to come up (up to 5 s).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if portOpen("127.0.0.1", defaultPort, 300*time.Millisecond) {
			return func() {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
			}, nil
		}
		time.Sleep(250 * time.Millisecond)
	}

	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	return noop, fmt.Errorf("port-forward started but orchestrator not reachable after 5 s (is minikube running?)")
}
