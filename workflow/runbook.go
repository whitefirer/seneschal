package workflow

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// TriggerType defines how a runbook is triggered.
type TriggerType string

const (
	TriggerManual  TriggerType = "manual"
	TriggerCron    TriggerType = "cron"
	TriggerWebhook TriggerType = "webhook"
)

// TriggerConfig is one trigger declaration in a runbook.
type TriggerConfig struct {
	Type TriggerType `yaml:"type"`
	Cron string      `yaml:"cron,omitempty"` // for type: cron — cron expression (simplified: "*/N * * * *" or "M H * * *")
	Path string      `yaml:"path,omitempty"` // for type: webhook — URL path suffix
}

// RunbookConfig is a complete runbook definition loaded from YAML.
type RunbookConfig struct {
	Name      string            `yaml:"name"`
	Workflow  string            `yaml:"workflow"` // path to the workflow YAML (relative to runbooks/ or workflows/ dir)
	Triggers  []TriggerConfig   `yaml:"triggers"`
	Variables map[string]string `yaml:"variables,omitempty"`
	FileName  string            `yaml:"-"` // set by loader
	FilePath  string            `yaml:"-"` // absolute path
}

// TriggerFunc is the callback invoked when a runbook is triggered.
// It returns the ID of the execution it started (empty when none was
// started), or an error when the trigger could not be dispatched — e.g. the
// referenced workflow file is missing or unparseable.
type TriggerFunc func(rb *RunbookConfig, extraVars map[string]string) (execID string, err error)

// ErrTriggerDispatch wraps an error returned by the TriggerFunc callback
// itself, distinguishing server-side dispatch failures (broken workflow
// reference, parse error) from pre-dispatch validation errors (unknown
// runbook, no manual/webhook trigger, unknown webhook path). Callers can
// branch on it with errors.Is.
var ErrTriggerDispatch = errors.New("trigger dispatch failed")

// TriggerSourceExtraVar is the reserved extraVars key the manager uses to
// tell the trigger callback where a fire came from — the values are the
// TriggerType strings ("manual", "webhook", "cron"). The TriggerFunc
// signature has no source channel of its own, so the manager passes it
// through extraVars; callbacks must treat the key as metadata and strip it
// before merging extraVars into the workflow's variables (MakeTriggerCallback
// does). A user-supplied variable of the same name would be overridden by
// the manager, hence the deliberately awkward name.
const TriggerSourceExtraVar = "_seneschal_trigger_source"

// withTriggerSource returns a copy of extraVars carrying the trigger source.
// The copy keeps the caller's map (e.g. an HTTP request body) untouched.
func withTriggerSource(extraVars map[string]string, source TriggerType) map[string]string {
	vars := make(map[string]string, len(extraVars)+1)
	for k, v := range extraVars {
		vars[k] = v
	}
	vars[TriggerSourceExtraVar] = string(source)
	return vars
}

// RunbookManager loads, watches, and dispatches runbooks.
type RunbookManager struct {
	mu           sync.RWMutex
	runbooks     map[string]*RunbookConfig // keyed by name
	dir          string
	workflowsDir string                   // for resolving relative workflow paths
	trigger      TriggerFunc              // callback when a runbook fires
	cronStop     map[string]chan struct{} // cron stop channels keyed by "name#index"
	logFunc      func(format string, args ...interface{})
}

// NewRunbookManager creates a manager. trigger is called when a runbook fires
// (cron, webhook, or manual). logFunc is for status messages (nil = silent).
func NewRunbookManager(dir, workflowsDir string, trigger TriggerFunc, logFunc func(string, ...interface{})) *RunbookManager {
	if logFunc == nil {
		logFunc = func(string, ...interface{}) {}
	}
	return &RunbookManager{
		runbooks:     make(map[string]*RunbookConfig),
		dir:          dir,
		workflowsDir: workflowsDir,
		trigger:      trigger,
		cronStop:     make(map[string]chan struct{}),
		logFunc:      logFunc,
	}
}

// LoadDir scans the runbook directory and loads all .yaml files.
func (m *RunbookManager) LoadDir() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.loadDirLocked()
}

func (m *RunbookManager) loadDirLocked() error {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // no runbooks dir — fine
		}
		return fmt.Errorf("read runbooks dir: %w", err)
	}

	loaded := make(map[string]*RunbookConfig)
	for _, e := range entries {
		if e.IsDir() || (!strings.HasSuffix(e.Name(), ".yaml") && !strings.HasSuffix(e.Name(), ".yml")) {
			continue
		}
		path := filepath.Join(m.dir, e.Name())
		rb, err := loadRunbookFile(path)
		if err != nil {
			m.logFunc("⚠️ runbook %s: %v", e.Name(), err)
			continue
		}
		loaded[rb.Name] = rb
	}

	// Stop all old crons, start new ones.
	m.stopAllCronsLocked()
	m.runbooks = loaded
	m.startAllCronsLocked()

	m.logFunc("📋 loaded %d runbook(s)", len(loaded))
	return nil
}

func loadRunbookFile(path string) (*RunbookConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var rb RunbookConfig
	if err := yaml.Unmarshal(data, &rb); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	if rb.Name == "" {
		// Derive name from filename.
		rb.Name = strings.TrimSuffix(strings.TrimSuffix(filepath.Base(path), ".yaml"), ".yml")
	}
	if rb.Workflow == "" {
		return nil, fmt.Errorf("runbook %s: 'workflow' field is required", rb.Name)
	}
	rb.FileName = filepath.Base(path)
	rb.FilePath = path
	return &rb, nil
}

// List returns all loaded runbooks.
func (m *RunbookManager) List() []*RunbookConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*RunbookConfig, 0, len(m.runbooks))
	for _, rb := range m.runbooks {
		out = append(out, rb)
	}
	return out
}

// Get returns a runbook by name, or nil.
func (m *RunbookManager) Get(name string) *RunbookConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.runbooks[name]
}

// ResolveWorkflowPath finds the workflow file referenced by a runbook.
// Tries: runbook dir + Workflow, workflows dir + Workflow, absolute.
func (m *RunbookManager) ResolveWorkflowPath(rb *RunbookConfig) (string, error) {
	wfPath := rb.Workflow
	// Try relative to runbooks dir.
	candidate := filepath.Join(m.dir, wfPath)
	if fileExists(candidate) {
		return candidate, nil
	}
	// Try relative to workflows dir.
	candidate = filepath.Join(m.workflowsDir, wfPath)
	if fileExists(candidate) {
		return candidate, nil
	}
	// Try absolute.
	if filepath.IsAbs(wfPath) && fileExists(wfPath) {
		return wfPath, nil
	}
	return "", fmt.Errorf("workflow file not found: %s (tried %s, %s)", wfPath, m.dir, m.workflowsDir)
}

// Trigger fires a runbook manually or from webhook. It returns the execution
// ID reported by the trigger callback. Errors from the callback are wrapped
// with ErrTriggerDispatch; lookup/policy failures are plain errors.
func (m *RunbookManager) Trigger(name string, extraVars map[string]string) (string, error) {
	m.mu.RLock()
	rb := m.runbooks[name]
	m.mu.RUnlock()
	if rb == nil {
		return "", fmt.Errorf("runbook %q not found", name)
	}
	// Check it has a manual or webhook trigger (or no triggers = manual-only).
	if len(rb.Triggers) > 0 {
		hasManual := false
		for _, t := range rb.Triggers {
			if t.Type == TriggerManual || t.Type == TriggerWebhook {
				hasManual = true
				break
			}
		}
		if !hasManual {
			return "", fmt.Errorf("runbook %q has no manual/webhook trigger", name)
		}
	}
	if m.trigger == nil {
		return "", nil
	}
	execID, err := m.trigger(rb, withTriggerSource(extraVars, TriggerManual))
	if err != nil {
		return "", fmt.Errorf("runbook %q: %w: %v", name, ErrTriggerDispatch, err)
	}
	return execID, nil
}

// TriggerByPath fires a runbook by its webhook path. It returns the execution
// ID reported by the trigger callback. Errors from the callback are wrapped
// with ErrTriggerDispatch; an unknown path is a plain error.
func (m *RunbookManager) TriggerByPath(path string, extraVars map[string]string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, rb := range m.runbooks {
		for _, t := range rb.Triggers {
			if t.Type == TriggerWebhook && t.Path == path {
				if m.trigger == nil {
					return "", nil
				}
				execID, err := m.trigger(rb, withTriggerSource(extraVars, TriggerWebhook))
				if err != nil {
					return "", fmt.Errorf("runbook %q: %w: %v", rb.Name, ErrTriggerDispatch, err)
				}
				return execID, nil
			}
		}
	}
	return "", fmt.Errorf("no runbook with webhook path %q", path)
}

// ── Cron scheduling ───────────────────────────────────────────────────────────

// startAllCrons starts timers for all cron triggers. Caller must hold mu.
func (m *RunbookManager) startAllCronsLocked() {
	for name, rb := range m.runbooks {
		for i, t := range rb.Triggers {
			if t.Type != TriggerCron {
				continue
			}
			key := fmt.Sprintf("%s#%d", name, i)
			interval := parseSimpleCron(t.Cron)
			if interval <= 0 {
				m.logFunc("⚠️ runbook %s: invalid cron %q", name, t.Cron)
				continue
			}
			stopCh := make(chan struct{})
			m.cronStop[key] = stopCh
			go m.runCron(key, rb, interval, stopCh)
			m.logFunc("⏰ runbook %s: cron %q (every %s)", name, t.Cron, interval)
		}
	}
}

func (m *RunbookManager) runCron(key string, rb *RunbookConfig, interval time.Duration, stopCh chan struct{}) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if m.trigger != nil {
				execID, err := m.trigger(rb, withTriggerSource(nil, TriggerCron))
				if err != nil {
					// A failing trigger used to vanish into stdout; surface it
					// through logFunc so the scheduler has visibility.
					m.logFunc("⚠️ runbook %s: trigger failed: %v", rb.Name, err)
				} else {
					m.logFunc("📋 runbook %s fired (execution %s)", rb.Name, execID)
				}
			}
		case <-stopCh:
			return
		}
	}
}

// stopAllCrons stops all cron timers. Caller must hold mu.
func (m *RunbookManager) stopAllCronsLocked() {
	for key, stopCh := range m.cronStop {
		close(stopCh)
		delete(m.cronStop, key)
	}
}

// Watch polls the runbook directory every pollInterval and hot-reloads on
// changes. Blocks; run in a goroutine.
func (m *RunbookManager) Watch(pollInterval time.Duration) {
	lastSnapshot := m.snapshot()
	for {
		time.Sleep(pollInterval)
		current := m.snapshot()
		if current != lastSnapshot {
			m.logFunc("🔄 runbooks changed, reloading...")
			m.mu.Lock()
			m.stopAllCronsLocked()
			m.loadDirLocked()
			m.mu.Unlock()
			lastSnapshot = m.snapshot()
		}
	}
}

// snapshot returns a fingerprint of the runbook dir (filenames + mtimes).
func (m *RunbookManager) snapshot() string {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return ""
	}
	var sb strings.Builder
	for _, e := range entries {
		if e.IsDir() || (!strings.HasSuffix(e.Name(), ".yaml") && !strings.HasSuffix(e.Name(), ".yml")) {
			continue
		}
		info, _ := e.Info()
		fmt.Fprintf(&sb, "%s:%d ", e.Name(), info.ModTime().UnixNano())
	}
	return sb.String()
}

// ── Helpers ────────────────────────────────────────────────────────────────────

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// parseSimpleCron converts a simplified cron expression to a Duration.
// Supports:
//
//	"*/N * * * *" → every N minutes
//	"M H * * *"   → daily at H:M (best-effort, computes next run)
//	"Ns" / "Nm"   → Go duration (shortcut)
//
// Returns 0 if unparseable.
func parseSimpleCron(cron string) time.Duration {
	cron = strings.TrimSpace(cron)

	// Go duration shortcut (non-standard but convenient).
	if d, err := time.ParseDuration(cron); err == nil {
		return d
	}

	parts := strings.Fields(cron)
	if len(parts) != 5 {
		return 0
	}

	// "*/N * * * *" — every N minutes.
	if strings.HasPrefix(parts[0], "*/") {
		n := parseInt(strings.TrimPrefix(parts[0], "*/"))
		if n > 0 && parts[1] == "*" {
			return time.Duration(n) * time.Minute
		}
	}

	// "M H * * *" — daily at H:M. Compute interval as 24h (simplified: we
	// just run every 24h, ignoring exact time-of-day alignment).
	min := parseInt(parts[0])
	hour := parseInt(parts[1])
	if min >= 0 && hour >= 0 && parts[2] == "*" && parts[3] == "*" && parts[4] == "*" {
		// For simplicity, treat as "every 24h" — exact time alignment requires
		// a real cron parser. Good enough for MVP.
		return 24 * time.Hour
	}

	// "*/N */M * * *" — every N minutes within every M hours.
	if strings.HasPrefix(parts[0], "*/") && strings.HasPrefix(parts[1], "*/") {
		n := parseInt(strings.TrimPrefix(parts[0], "*/"))
		if n > 0 {
			return time.Duration(n) * time.Minute
		}
	}

	return 0
}

func parseInt(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return -1
		}
		n = n*10 + int(c-'0')
	}
	return n
}
