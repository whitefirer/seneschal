package workflow

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func (e *Executor) execHTTP(step Step) (string, error) {
	url, err := e.context.ResolveTemplate(step.URL)
	if err != nil {
		return "", fmt.Errorf("resolve URL template: %w", err)
	}

	method := step.Method
	if method == "" {
		method = "GET"
	}

	// Print HTTP call with pretty output (will update with status after)
	if e.verbose {
		fmt.Printf("    %s%s %s%s\n", ColorMagenta, method, ColorReset, url)
	}

	var bodyReader io.Reader
	if step.Body != "" {
		bodyStr, err := e.context.ResolveTemplate(step.Body)
		if err != nil {
			return "", fmt.Errorf("resolve body template: %w", err)
		}
		bodyReader = strings.NewReader(bodyStr)
	}

	// Parse timeout
	timeout := 60 * time.Second
	if step.Timeout != "" {
		if parsed, err := ParseDuration(step.Timeout); err == nil {
			timeout = parsed
		}
	}

	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	// Set headers
	for k, v := range step.Headers {
		resolved, err := e.context.ResolveTemplate(v)
		if err != nil {
			return "", fmt.Errorf("resolve header template: %w", err)
		}
		req.Header.Set(k, resolved)
	}

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	duration := time.Since(start)
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response body: %w", err)
	}

	output := fmt.Sprintf("Status: %d (%s)\n%s", resp.StatusCode, duration.Truncate(time.Millisecond), string(body))

	// Print HTTP response with pretty output
	if e.printer != nil {
		e.printer.PrintHTTPCall(method, url, resp.StatusCode, duration.Truncate(time.Millisecond))
	}

	e.context.SetResult(step.Name, output)

	// Save output to variable if specified
	if step.SaveOutput != "" {
		// Store as structured data
		resultData := map[string]interface{}{
			"status":  resp.StatusCode,
			"body":    string(body),
			"headers": resp.Header,
		}
		if jsonData, err := json.Marshal(resultData); err == nil {
			e.context.Set(step.SaveOutput, string(jsonData))
		}
	}

	return output, nil
}

