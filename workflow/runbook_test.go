package workflow

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestRunCron_TriggerOutcomeLogging covers the cron fire path: a failing
// trigger must surface through logFunc (it used to vanish into stdout), and a
// successful fire logs the execution ID.
func TestRunCron_TriggerOutcomeLogging(t *testing.T) {
	tests := []struct {
		name    string
		trigger TriggerFunc
		wantLog string
	}{
		{
			name: "failure surfaced via logFunc",
			trigger: func(rb *RunbookConfig, extraVars map[string]string) (string, error) {
				return "", errors.New("dispatch boom")
			},
			wantLog: `runbook rb: trigger failed: dispatch boom`,
		},
		{
			name: "success logs execution id",
			trigger: func(rb *RunbookConfig, extraVars map[string]string) (string, error) {
				return "exec-123", nil
			},
			wantLog: `runbook rb fired (execution exec-123)`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var mu sync.Mutex
			var logs []string
			m := &RunbookManager{
				trigger: tt.trigger,
				logFunc: func(format string, args ...interface{}) {
					mu.Lock()
					logs = append(logs, fmt.Sprintf(format, args...))
					mu.Unlock()
				},
			}

			stopCh := make(chan struct{})
			go m.runCron("rb#0", &RunbookConfig{Name: "rb"}, 10*time.Millisecond, stopCh)
			defer close(stopCh)

			deadline := time.Now().Add(2 * time.Second)
			for {
				mu.Lock()
				n := len(logs)
				mu.Unlock()
				if n > 0 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("no cron log within 2s")
				}
				time.Sleep(5 * time.Millisecond)
			}

			mu.Lock()
			defer mu.Unlock()
			if !strings.Contains(logs[0], tt.wantLog) {
				t.Fatalf("cron log %q does not contain %q", logs[0], tt.wantLog)
			}
		})
	}
}
