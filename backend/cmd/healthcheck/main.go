// Command healthcheck probes the backend API and its database readiness.
package main

import (
	"net/http"
	"os"
	"time"
)

// /api/jobs/counts now requires a signed-in session; probe the unauthenticated
// health endpoint instead so the container healthcheck does not depend on auth.
const healthURL = "http://127.0.0.1:4040/api/health"

func main() {
	os.Exit(run())
}

func run() int {
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Get(healthURL)
	if err != nil {
		return 1
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}
