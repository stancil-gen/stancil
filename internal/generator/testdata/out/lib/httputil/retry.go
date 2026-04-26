package httputil

import (
	"bytes"
	"io"
	"math"
	"net/http"
	"time"
)

// RetryConfig controls retry behavior for HTTP calls.
type RetryConfig struct {
	Attempts int    // max retry attempts (0 = no retry)
	Backoff  string // "exponential" | "linear" | "" (no delay)
	OnStatus []int  // status codes that trigger a retry (e.g. 429, 502, 503)
}

// DoWithRetry executes an HTTP request with retry logic.
// The request body is buffered so it can be replayed on retries.
func DoWithRetry(client *http.Client, req *http.Request, cfg RetryConfig) (*http.Response, error) {
	// Buffer the body for replay on retries
	var bodyBytes []byte
	if req.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		req.Body.Close()
	}

	retryableStatus := make(map[int]bool, len(cfg.OnStatus))
	for _, s := range cfg.OnStatus {
		retryableStatus[s] = true
	}

	maxAttempts := cfg.Attempts + 1
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	var lastResp *http.Response
	var lastErr error

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			delay := backoffDelay(attempt, cfg.Backoff)
			time.Sleep(delay)
		}

		// Reset body for each attempt
		if bodyBytes != nil {
			req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			req.ContentLength = int64(len(bodyBytes))
		}

		lastResp, lastErr = client.Do(req)
		if lastErr != nil {
			continue
		}

		if !retryableStatus[lastResp.StatusCode] {
			return lastResp, nil
		}

		// Retryable status — close body and retry
		lastResp.Body.Close()
		lastResp = nil
	}

	if lastErr != nil {
		return nil, lastErr
	}

	// All retries exhausted with retryable status — return last response
	// Re-execute one final time to get a fresh response body
	if bodyBytes != nil {
		req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		req.ContentLength = int64(len(bodyBytes))
	}
	return client.Do(req)
}

func backoffDelay(attempt int, strategy string) time.Duration {
	switch strategy {
	case "exponential":
		return time.Duration(math.Pow(2, float64(attempt-1))) * time.Second
	case "linear":
		return time.Duration(attempt) * time.Second
	default:
		return time.Second
	}
}
