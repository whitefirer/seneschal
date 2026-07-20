package ai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"
)

// This file holds the shared HTTP retry layer used by all providers
// (Anthropic, OpenAI, Ollama). It retries transient failures — 429, 5xx,
// network errors — with exponential backoff, and never retries context
// cancellation or non-retryable 4xx.

// isRetryableStatus reports whether an HTTP status code indicates a transient
// error worth retrying (rate limit, server error). 4xx (except 429) are not
// retryable — they indicate a request-level problem (auth, bad input).
func isRetryableStatus(code int) bool {
	switch code {
	case 429, 500, 502, 503, 504:
		return true
	}
	return false
}

// isRetryableNetErr reports whether a network-level error is transient
// (timeout, connection reset). Non-timeout errors (e.g. DNS failure) are also
// retried — the model API is generally reachable, and a blip shouldn't fail
// the whole workflow.
func isRetryableNetErr(err error) bool {
	if err == nil {
		return false
	}
	// Context cancellation is NOT retryable — the caller deliberately cancelled.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	return true // network errors are generally worth one retry
}

// retryableHTTPDo wraps client.Do with automatic retries on transient errors
// (429/5xx/timeout). maxR is the number of retries after the first attempt
// (0 = single attempt, no retries); baseDelay is doubled each retry (capped
// at 30s). Returns the final response (which the caller must close) and the
// total number of retries performed.
func retryableHTTPDo(ctx context.Context, client *http.Client, maxR int, baseDelay time.Duration, req *http.Request) (*http.Response, int, error) {
	for attempt := 0; ; attempt++ {
		resp, err := client.Do(req)

		if err != nil {
			// Network error — retry if transient and we haven't exhausted.
			if isRetryableNetErr(err) && attempt < maxR {
				delay := backoffDelay(baseDelay, attempt)
				select {
				case <-time.After(delay):
				case <-ctx.Done():
					return nil, attempt, ctx.Err()
				}
				continue
			}
			return nil, attempt, err
		}

		// Got an HTTP response. If retryable status and retries left, close
		// the body and retry. Otherwise return the response as-is (the caller
		// reads the body, including error details for non-2xx).
		if isRetryableStatus(resp.StatusCode) && attempt < maxR {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			delay := backoffDelay(baseDelay, attempt)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return nil, attempt, ctx.Err()
			}
			continue
		}

		// Non-retryable or out of retries: return what we have.
		return resp, attempt, nil
	}
}

func backoffDelay(base time.Duration, attempt int) time.Duration {
	d := base * (1 << attempt)
	if d > 30*time.Second {
		return 30 * time.Second
	}
	return d
}
