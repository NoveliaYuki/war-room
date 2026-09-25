package e2e_test

import (
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"
)

func TestHealthAndFrontendHandleConcurrentRequests(t *testing.T) {
	const (
		concurrentRequests = 12
		latencyBudget      = 2 * time.Second
	)
	client := &http.Client{Timeout: latencyBudget}
	endpoints := []string{baseURL + "/", baseURL + "/api/health", baseURL + "/api/jobs/counts"}

	for _, endpoint := range endpoints {
		t.Run(endpoint, func(t *testing.T) {
			var waitGroup sync.WaitGroup
			start := time.Now()
			errors := make(chan error, concurrentRequests*2)
			for range concurrentRequests {
				waitGroup.Add(1)
				go func() {
					defer waitGroup.Done()
					// Each endpoint derives from the configured E2E base URL and fixed paths.
					response, err := client.Get(endpoint) // #nosec G107 -- endpoints use the configured E2E host and fixed paths
					if err != nil {
						errors <- err
						return
					}
					defer func() {
						if closeErr := response.Body.Close(); closeErr != nil {
							errors <- closeErr
						}
					}()
					if response.StatusCode != http.StatusOK {
						errors <- fmt.Errorf("unexpected HTTP status %d", response.StatusCode)
					}
				}()
			}
			waitGroup.Wait()
			close(errors)
			for err := range errors {
				t.Errorf("request failed: %v", err)
			}
			if elapsed := time.Since(start); elapsed > latencyBudget {
				t.Errorf("%d concurrent requests took %s; limit is %s", concurrentRequests, elapsed, latencyBudget)
			}
		})
	}
}
