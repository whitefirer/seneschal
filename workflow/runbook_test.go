package workflow

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/robfig/cron/v3"
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
			go m.runCron("rb#0", &RunbookConfig{Name: "rb"}, cron.ConstantDelaySchedule{Delay: 10 * time.Millisecond}, stopCh)
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

// TestParseCron verifies the cron expression parser: full 5-field cron
// (time-aligned) and the Go-duration shortcut (fixed interval).
func TestParseCron(t *testing.T) {
	// Reference instant: 2024-01-15 10:30:00 UTC (a Monday).
	ref := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	cases := []struct {
		expr    string
		want    time.Time
		wantErr bool
	}{
		// Go duration shortcut — fixed interval, unaligned.
		{"5m", ref.Add(5 * time.Minute), false},
		{"30s", ref.Add(30 * time.Second), false},
		// Full cron — aligned to real cron boundaries.
		{"*/5 * * * *", time.Date(2024, 1, 15, 10, 35, 0, 0, time.UTC), false}, // every 5 min (:00/:05/...)
		{"0 2 * * *", time.Date(2024, 1, 16, 2, 0, 0, 0, time.UTC), false},     // daily at 02:00
		{"0 9 * * 1", time.Date(2024, 1, 22, 9, 0, 0, 0, time.UTC), false},     // next Monday 09:00
		{"30 4 1 * *", time.Date(2024, 2, 1, 4, 30, 0, 0, time.UTC), false},    // 1st of month 04:30
		{"not-a-cron", time.Time{}, true},
	}

	for _, tc := range cases {
		sched, err := parseCron(tc.expr)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseCron(%q): expected error, got a schedule", tc.expr)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseCron(%q): %v", tc.expr, err)
			continue
		}
		if got := sched.Next(ref); !got.Equal(tc.want) {
			t.Errorf("parseCron(%q).Next(%v) = %v, want %v", tc.expr, ref, got, tc.want)
		}
	}
}
