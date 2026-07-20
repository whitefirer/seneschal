package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/whitefirer/seneschal/workflow"
	"gopkg.in/yaml.v3"
)

var runbookOpts struct {
	dir       string
	dirLegacy string
	server    string
	vars      []string
}

// runbookCmd manages runbooks: list, show, trigger, create.
var runbookCmd = &cobra.Command{
	Use:     "runbook",
	Short:   "Manage runbooks (trigger & schedule)",
	GroupID: "runbook",
}

func init() {
	p := runbookCmd.PersistentFlags()
	p.StringVar(&runbookOpts.dir, "runbooks-dir", "./runbooks", "directory of runbook definitions")
	p.StringVarP(&runbookOpts.dirLegacy, "dir", "d", "", "alias for --runbooks-dir")
	_ = p.MarkDeprecated("dir", "use --runbooks-dir instead")
	p.StringVarP(&runbookOpts.server, "server", "s", "", "query a seneschal server API instead of local files")
	p.StringArrayVar(&runbookOpts.vars, "var", nil, "override runbook variable (key=value, repeatable)")
	runbookCmd.AddCommand(runbookListCmd, runbookShowCmd, runbookTriggerCmd, runbookCreateCmd)
}

// runbookDir resolves the runbooks directory from the flags (deprecated
// --dir alias wins when given).
func runbookDir() string {
	return resolveDir(runbookOpts.dir, runbookOpts.dirLegacy)
}

// ── runbook list ─────────────────────────────────────────────────────────────

var runbookListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all runbooks",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runbookList(runbookDir(), runbookOpts.server)
	},
}

func runbookList(dir, serverURL string) error {
	if serverURL != "" {
		// Query server API.
		resp, err := http.Get(serverURL + "/api/runbooks")
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		var result struct {
			Success bool                     `json:"success"`
			Data    []map[string]interface{} `json:"data"`
		}
		json.NewDecoder(resp.Body).Decode(&result)
		if !result.Success || len(result.Data) == 0 {
			fmt.Println("(no runbooks)")
			return nil
		}
		fmt.Printf("%-25s  %-25s  %s\n", "NAME", "WORKFLOW", "TRIGGERS")
		for _, rb := range result.Data {
			name, _ := rb["Name"].(string)
			wf, _ := rb["Workflow"].(string)
			triggers := formatTriggersJSON(rb["Triggers"])
			fmt.Printf("%-25s  %-25s  %s\n", name, wf, triggers)
		}
		return nil
	}
	// Local: scan dir.
	mgr := workflow.NewRunbookManager(dir, ".", nil, nil)
	mgr.LoadDir()
	runbooks := mgr.List()
	if len(runbooks) == 0 {
		fmt.Println("(no runbooks)")
		return nil
	}
	fmt.Printf("%-25s  %-25s  %s\n", "NAME", "WORKFLOW", "TRIGGERS")
	for _, rb := range runbooks {
		fmt.Printf("%-25s  %-25s  %s\n", rb.Name, rb.Workflow, formatTriggers(rb.Triggers))
	}
	return nil
}

// ── runbook show ─────────────────────────────────────────────────────────────

var runbookShowCmd = &cobra.Command{
	Use:   "show <name>",
	Short: "Show runbook config",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runbookShow(args[0], runbookDir(), runbookOpts.server)
	},
}

func runbookShow(name, dir, serverURL string) error {
	if serverURL != "" {
		resp, err := http.Get(serverURL + "/api/runbooks/" + name)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		var result struct {
			Success bool                   `json:"success"`
			Data    map[string]interface{} `json:"data"`
		}
		json.NewDecoder(resp.Body).Decode(&result)
		if !result.Success {
			fmt.Fprintln(os.Stderr, "Runbook not found")
			return errExitQuiet
		}
		data, _ := json.MarshalIndent(result.Data, "", "  ")
		fmt.Println(string(data))
		return nil
	}
	mgr := workflow.NewRunbookManager(dir, ".", nil, nil)
	mgr.LoadDir()
	rb := mgr.Get(name)
	if rb == nil {
		fmt.Fprintln(os.Stderr, "Runbook not found")
		return errExitQuiet
	}
	fmt.Printf("Name:     %s\n", rb.Name)
	fmt.Printf("Workflow: %s\n", rb.Workflow)
	fmt.Printf("Triggers: %s\n", formatTriggers(rb.Triggers))
	if len(rb.Variables) > 0 {
		fmt.Println("Variables:")
		for k, v := range rb.Variables {
			fmt.Printf("  %s = %s\n", k, v)
		}
	}
	return nil
}

// ── runbook trigger ──────────────────────────────────────────────────────────

var runbookTriggerCmd = &cobra.Command{
	Use:   "trigger <name>",
	Short: "Manually trigger a runbook",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runbookTrigger(args[0], parseVarFlags(runbookOpts.vars), runbookDir(), runbookOpts.server)
	},
}

func runbookTrigger(name string, vars map[string]string, dir, serverURL string) error {
	if serverURL != "" {
		body, _ := json.Marshal(vars)
		resp, err := http.Post(serverURL+"/api/runbooks/"+name+"/trigger", "application/json", bytes.NewReader(body))
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		var result struct {
			Success bool   `json:"success"`
			Error   string `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&result)
		if !result.Success {
			return fmt.Errorf("%s", result.Error)
		}
		fmt.Printf("✅ Triggered: %s\n", name)
		return nil
	}
	// Local: resolve and execute directly.
	mgr := workflow.NewRunbookManager(dir, ".", nil, nil)
	mgr.LoadDir()
	rb := mgr.Get(name)
	if rb == nil {
		return fmt.Errorf("runbook %q not found", name)
	}
	wfPath, err := mgr.ResolveWorkflowPath(rb)
	if err != nil {
		return err
	}
	wf, err := workflow.ParseFile(wfPath)
	if err != nil {
		return err
	}
	// Merge variables.
	allVars := make(map[string]string)
	for k, v := range rb.Variables {
		allVars[k] = v
	}
	for k, v := range vars {
		allVars[k] = v
	}
	executor := workflow.NewExecutor(allVars)
	executor.SetVerbose(true)
	executor.SetOutputMode(workflow.ParseOutputMode("rich"))
	executor.SetTheme("default")
	result := executor.Execute(wf)
	if result != nil && result.Status == "failed" && result.Error != "" {
		return fmt.Errorf("%s", result.Error)
	}
	return nil
}

// ── runbook create ───────────────────────────────────────────────────────────

var runbookCreateOpts struct {
	workflow string
	cron     string
	webhook  string
}

var runbookCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a new runbook",
	Args:  cobra.ExactArgs(1),
	Example: `  seneschal runbook create nightly --workflow deploy.yaml --cron "0 2 * * *"
  seneschal runbook create on-push --workflow ci.yaml --webhook /hooks/push --var env=prod`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runbookCreate(args[0], parseVarFlags(runbookOpts.vars), runbookDir())
	},
}

func init() {
	f := runbookCreateCmd.Flags()
	f.StringVarP(&runbookCreateOpts.workflow, "workflow", "w", "", "workflow file to run (required)")
	_ = runbookCreateCmd.MarkFlagRequired("workflow")
	f.StringVar(&runbookCreateOpts.cron, "cron", "", "cron expression trigger")
	f.StringVar(&runbookCreateOpts.webhook, "webhook", "", "webhook path trigger")
}

func runbookCreate(name string, vars map[string]string, dir string) error {
	wfFile := runbookCreateOpts.workflow
	cron := runbookCreateOpts.cron
	webhookPath := runbookCreateOpts.webhook

	// Build runbook YAML via the yaml package so values are properly quoted/
	// escaped (hand-written Fprintf output broke on values containing ':',
	// '#', leading spaces, etc.).
	triggers := []workflow.TriggerConfig{{Type: workflow.TriggerManual}}
	if cron != "" {
		triggers = append(triggers, workflow.TriggerConfig{Type: workflow.TriggerCron, Cron: cron})
	}
	if webhookPath != "" {
		triggers = append(triggers, workflow.TriggerConfig{Type: workflow.TriggerWebhook, Path: webhookPath})
	}
	rb := workflow.RunbookConfig{
		Name:      name,
		Workflow:  wfFile,
		Triggers:  triggers,
		Variables: vars,
	}
	data, err := yaml.Marshal(rb)
	if err != nil {
		return fmt.Errorf("marshal runbook: %w", err)
	}

	// Write file.
	os.MkdirAll(dir, 0755)
	path := filepath.Join(dir, name+".yaml")
	if err := os.WriteFile(path, data, 0644); err != nil {
		return err
	}
	fmt.Printf("✅ Created runbook: %s\n", path)
	fmt.Println(string(data))
	return nil
}

// ── trigger formatting helpers ───────────────────────────────────────────────

func formatTriggers(triggers []workflow.TriggerConfig) string {
	var parts []string
	for _, t := range triggers {
		switch t.Type {
		case workflow.TriggerManual:
			parts = append(parts, "manual")
		case workflow.TriggerCron:
			parts = append(parts, "cron:"+t.Cron)
		case workflow.TriggerWebhook:
			parts = append(parts, "webhook:"+t.Path)
		}
	}
	return strings.Join(parts, ", ")
}

func formatTriggersJSON(raw interface{}) string {
	arr, ok := raw.([]interface{})
	if !ok {
		return ""
	}
	var parts []string
	for _, item := range arr {
		m, _ := item.(map[string]interface{})
		t, _ := m["Type"].(string)
		switch t {
		case "manual":
			parts = append(parts, "manual")
		case "cron":
			c, _ := m["Cron"].(string)
			parts = append(parts, "cron:"+c)
		case "webhook":
			p, _ := m["Path"].(string)
			parts = append(parts, "webhook:"+p)
		}
	}
	return strings.Join(parts, ", ")
}
