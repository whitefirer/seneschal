package workflow

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Watcher monitors a workflow YAML file for changes and reloads it.
type Watcher struct {
	filePath    string
	lastModTime string
	onChange    func(*Workflow)
	running     bool
}

// NewWatcher creates a file watcher for a workflow YAML file.
func NewWatcher(filePath string, onChange func(*Workflow)) *Watcher {
	return &Watcher{
		filePath: filePath,
		onChange: onChange,
	}
}

// checkModification reads the file and checks if it has changed.
func (w *Watcher) checkModification() (bool, error) {
	info, err := os.Stat(w.filePath)
	if err != nil {
		return false, err
	}

	currentMod := info.ModTime().String()
	if currentMod != w.lastModTime {
		w.lastModTime = currentMod
		return true, nil
	}
	return false, nil
}

// LoadAndNotify loads the workflow and calls onChange if it changed.
func (w *Watcher) LoadAndNotify() error {
	changed, err := w.checkModification()
	if err != nil {
		return fmt.Errorf("check modification: %w", err)
	}

	if !changed {
		return nil
	}

	wf, err := ParseFile(w.filePath)
	if err != nil {
		return fmt.Errorf("parse file: %w", err)
	}

	// Validate
	if errs := wf.Validate(); len(errs) > 0 {
		var msg []string
		for _, e := range errs {
			msg = append(msg, e.Error())
		}
		return fmt.Errorf("validation failed: %s", strings.Join(msg, "; "))
	}

	if w.onChange != nil {
		w.onChange(wf)
	}

	return nil
}

// WorkflowManager manages multiple workflows and supports YAML hot-reload.
type WorkflowManager struct {
	workflows map[string]*Workflow
	watchers  map[string]*Watcher
}

// NewWorkflowManager creates a new workflow manager.
func NewWorkflowManager() *WorkflowManager {
	return &WorkflowManager{
		workflows: make(map[string]*Workflow),
		watchers:  make(map[string]*Watcher),
	}
}

// AddWorkflow adds a workflow from a YAML file.
func (m *WorkflowManager) AddWorkflow(name, filePath string) error {
	wf, err := ParseFile(filePath)
	if err != nil {
		return fmt.Errorf("load workflow '%s': %w", name, err)
	}

	if errs := wf.Validate(); len(errs) > 0 {
		var msg []string
		for _, e := range errs {
			msg = append(msg, e.Error())
		}
		return fmt.Errorf("validate workflow '%s': %s", name, strings.Join(msg, "; "))
	}

	m.workflows[name] = wf

	// Set up watcher
	m.watchers[name] = NewWatcher(filePath, func(updated *Workflow) {
		m.workflows[name] = updated
		fmt.Printf("🔄 Workflow '%s' reloaded from %s\n", name, filePath)
	})

	return nil
}

// AddWorkflowFromYAML adds a workflow from YAML content and saves it.
func (m *WorkflowManager) AddWorkflowFromYAML(name, yamlContent, savePath string) error {
	var wf Workflow
	if err := yaml.Unmarshal([]byte(yamlContent), &wf); err != nil {
		return fmt.Errorf("parse YAML: %w", err)
	}

	if errs := wf.Validate(); len(errs) > 0 {
		var msg []string
		for _, e := range errs {
			msg = append(msg, e.Error())
		}
		return fmt.Errorf("validate workflow '%s': %s", name, strings.Join(msg, "; "))
	}

	m.workflows[name] = &wf

	if savePath != "" {
		if err := wf.Save(savePath); err != nil {
			return fmt.Errorf("save workflow: %w", err)
		}
		m.watchers[name] = NewWatcher(savePath, func(updated *Workflow) {
			m.workflows[name] = updated
			fmt.Printf("🔄 Workflow '%s' reloaded from %s\n", name, savePath)
		})
	}

	return nil
}

// GetWorkflow retrieves a workflow by name.
func (m *WorkflowManager) GetWorkflow(name string) (*Workflow, bool) {
	wf, ok := m.workflows[name]
	return wf, ok
}

// ListWorkflows returns the names of all registered workflows.
func (m *WorkflowManager) ListWorkflows() []string {
	names := make([]string, 0, len(m.workflows))
	for name := range m.workflows {
		names = append(names, name)
	}
	return names
}

// ReloadAll checks all watched workflow files for changes.
func (m *WorkflowManager) ReloadAll() []error {
	var errs []error
	for name, watcher := range m.watchers {
		if err := watcher.LoadAndNotify(); err != nil {
			errs = append(errs, fmt.Errorf("reload '%s': %w", name, err))
		}
	}
	return errs
}

// RemoveWorkflow removes a workflow from the manager.
func (m *WorkflowManager) RemoveWorkflow(name string) {
	delete(m.workflows, name)
	delete(m.watchers, name)
}

// EditWorkflowYAML edits a workflow by reading the YAML, applying a modify function, and saving it back.
// After saving, the watcher will detect the change and reload automatically.
func (m *WorkflowManager) EditWorkflowYAML(name, filePath string, modify func([]byte) ([]byte, error)) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	modified, err := modify(data)
	if err != nil {
		return fmt.Errorf("modify: %w", err)
	}

	if err := os.WriteFile(filePath, modified, 0644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	// Force reload
	if watcher, ok := m.watchers[name]; ok {
		_ = watcher.LoadAndNotify()
	}

	return nil
}

// ToYAMLString returns the YAML string representation of a workflow.
func (m *WorkflowManager) ToYAMLString(name string) (string, error) {
	wf, ok := m.workflows[name]
	if !ok {
		return "", fmt.Errorf("workflow '%s' not found", name)
	}
	return wf.ToYAMLString()
}
