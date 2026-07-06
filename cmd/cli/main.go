package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"goworkflow/workflow"
	"goworkflow/workflow/ai"
)

const banner = `
  ╔═══════════════════════════════════════════╗
  ║       goworkflow - YAML Workflow Engine     ║
  ║  Edit YAML → Change Behavior Automatically ║
  ╚═══════════════════════════════════════════╝
`

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "run":
		cmdRun(os.Args[2:])
	case "create":
		cmdCreate(os.Args[2:])
	case "validate":
		cmdValidate(os.Args[2:])
	case "show":
		cmdShow(os.Args[2:])
	case "explain":
		cmdExplain(os.Args[2:])
	case "fix":
		cmdFix(os.Args[2:])
	case "generate":
		cmdGenerate(os.Args[2:])
	case "chat":
		cmdChat(os.Args[2:])
	case "edit":
		cmdEdit(os.Args[2:])
	case "template":
		cmdTemplate(os.Args[2:])
	case "list":
		fmt.Println("List all workflow files in a directory:")
		fmt.Println("Usage: goworkflow list <directory>")
		if len(os.Args) > 2 {
			dir := os.Args[2]
			entries, err := os.ReadDir(dir)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			for _, e := range entries {
				if !e.IsDir() && (strings.HasSuffix(e.Name(), ".yaml") || strings.HasSuffix(e.Name(), ".yml")) {
					fmt.Printf("  📄 %s\n", e.Name())
				}
			}
		}
	default:
		fmt.Printf("Unknown command: %s\n\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Print(banner)
	fmt.Println("Usage:")
	fmt.Println("  goworkflow run <file.yaml> [--var key=value ...] [--verbose] [--dry-run] [--output-mode MODE] [--theme THEME] [--force-color]")
	fmt.Println("")
	fmt.Println("Output Modes:")
	fmt.Println("  rich      - Rich styled output (default)")
	fmt.Println("  plain     - Plain text output")
	fmt.Println("  dag       - DAG graph visualization")
	fmt.Println("  timeline  - Timeline view with progress bars")
	fmt.Println("  compact   - Compact output suitable for CI/CD")
	fmt.Println("  tui       - Terminal UI with live progress animation")
	fmt.Println("")
	fmt.Println("Themes:")
	fmt.Println("  default   - Claude Code warm amber theme")
	fmt.Println("  claude    - Same as default")
	fmt.Println("  dark      - Dark purple theme")
	fmt.Println("  light     - Light green theme")
	fmt.Println("  monokai   - Monokai theme")
	fmt.Println("  ocean     - Ocean blue theme")
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  --force-color, -f   Force color output even when piped")
	fmt.Println("  --tui-style STYLE   TUI visual style: hermes (default) or claude")
	fmt.Println("  goworkflow create <name> [description] [--output file.yaml]")
	fmt.Println("  goworkflow validate <file.yaml>")
	fmt.Println("  goworkflow show <file.yaml>")
	fmt.Println("  goworkflow edit <file.yaml>   (opens YAML for editing)")
	fmt.Println("  goworkflow template           (show example workflow YAML)")
	fmt.Println("  goworkflow list <dir.yaml>    (list all workflows in a directory)")
	fmt.Println("")
	fmt.Println("Actions:")
	fmt.Println("  shell      Execute shell commands with variable substitution")
	fmt.Println("  http       Make HTTP requests, save responses to variables")
	fmt.Println("  condition  Branch based on variable values (==, !=, contains, >, <)")
	fmt.Println("  set        Set workflow variables")
	fmt.Println("  sleep      Pause for a duration (e.g., '5s', '1m')")
	fmt.Println("  log        Print log messages (info/warn/error)")
	fmt.Println("  parallel   Execute steps concurrently")
	fmt.Println("  template   Render a template file with variables")
	fmt.Println("  foreach    Iterate over items and execute steps")
	fmt.Println("")
	fmt.Println("Examples:")
	fmt.Println("  goworkflow run deploy.yaml --verbose --var ENV=prod")
	fmt.Println("  goworkflow create my-pipeline 'CI/CD Pipeline' --output pipeline.yaml")
	fmt.Println("  goworkflow validate workflow.yaml")
	fmt.Println("")
}

func parseFlags(args []string) (vars map[string]string, verbose bool, dryRun bool, outputMode string, themeName string, forceColor bool, tuiStyle string, remaining []string) {
	outputMode = "rich"
	themeName = "default"
	vars = make(map[string]string)
	remaining = make([]string, 0)
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--var":
			if i+1 < len(args) {
				parts := strings.SplitN(args[i+1], "=", 2)
				if len(parts) == 2 {
					vars[parts[0]] = parts[1]
				}
				i++
			}
		case "--verbose", "-v":
			verbose = true
		case "--dry-run":
			dryRun = true
		case "--output-mode", "--output", "-m":
			if i+1 < len(args) {
				outputMode = args[i+1]
				i++
			}
		case "--theme", "-t":
			if i+1 < len(args) {
				themeName = args[i+1]
				i++
			}
		case "--force-color", "-f":
			forceColor = true
		case "--tui-style":
			if i+1 < len(args) {
				tuiStyle = args[i+1]
				i++
			}
		default:
			remaining = append(remaining, args[i])
		}
	}
	return
}

func cmdRun(args []string) {
	vars, verbose, dryRun, outputModeStr, themeName, forceColor, tuiStyle, remaining := parseFlags(args)
	if len(remaining) == 0 {
		fmt.Println("Error: please specify a workflow YAML file")
		fmt.Println("Usage: goworkflow run <file.yaml> [--var key=value ...] [--verbose] [--dry-run]")
		os.Exit(1)
	}

	filePath := remaining[0]

	// Load workflow
	wf, err := workflow.ParseFile(filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading workflow: %v\n", err)
		os.Exit(1)
	}

	// Merge CLI vars with workflow vars
	for k, v := range wf.Variables {
		if _, ok := vars[k]; !ok {
			vars[k] = v
		}
	}

	// Parse output mode
	outputMode := workflow.ParseOutputMode(outputModeStr)

	// Execute
	executor := workflow.NewExecutor(vars)
	executor.SetVerbose(verbose)
	executor.SetDryRun(dryRun)
	executor.SetForceColor(forceColor)
	executor.SetTuiStyle(tuiStyle)
	executor.SetOutputMode(outputMode)
	executor.SetTheme(themeName)

	result := executor.Execute(wf)
	// Surface a configuration error (e.g. missing AI key) that Execute reports
	// before any step runs. Step-level failures are already printed by printers.
	if result != nil && result.Status == "failed" && result.Error != "" {
		fmt.Fprintf(os.Stderr, "Error: %s\n", result.Error)
		os.Exit(1)
	}
}

func cmdCreate(args []string) {
	_, _, _, _, _, _, _, remaining := parseFlags(args)

	if len(remaining) == 0 {
		fmt.Println("Error: please specify a workflow name")
		fmt.Println("Usage: goworkflow create <name> [description] [--output file.yaml]")
		os.Exit(1)
	}

	name := remaining[0]
	description := ""
	outputFile := name + ".yaml"

	for i := 1; i < len(remaining); i++ {
		if remaining[i] == "--output" || remaining[i] == "-o" {
			if i+1 < len(remaining) {
				outputFile = remaining[i+1]
				i++
			}
		} else {
			if description != "" {
				description += " "
			}
			description += remaining[i]
		}
	}

	wf := workflow.CreateWorkflow(name, description)

	// Add some example variables and steps
	wf.SetVariable("greeting", "Hello, World!")

	wf.AddStep(workflow.Step{
		Name:        "start",
		Action:      "log",
		Message:     "Workflow started: {{.greeting}}",
		Level:       "info",
		Description: "Log a start message",
	})

	wf.AddStep(workflow.Step{
		Name:        "check-env",
		Action:      "shell",
		Command:     "echo \"Running on $(uname -s)\"",
		Description: "Check the current environment",
	})

	wf.AddStep(workflow.Step{
		Name:        "done",
		Action:      "log",
		Message:     "Workflow completed successfully!",
		Level:       "info",
	})

	// Save
	if err := wf.Save(outputFile); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving workflow: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✅ Created workflow '%s' → %s\n", name, outputFile)
	fmt.Println("Edit the YAML file to customize your workflow, then run:")
	fmt.Printf("  goworkflow run %s --verbose\n", outputFile)
}

func cmdValidate(args []string) {
	if len(args) == 0 {
		fmt.Println("Error: please specify a workflow YAML file")
		os.Exit(1)
	}

	wf, err := workflow.ParseFile(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	errs := wf.Validate()
	if len(errs) == 0 {
		fmt.Printf("✅ Workflow '%s' is valid (%d steps)\n", wf.Name, len(wf.Steps))
	} else {
		fmt.Printf("❌ Workflow '%s' has %d error(s):\n", wf.Name, len(errs))
		for _, e := range errs {
			fmt.Printf("  • %v\n", e)
		}
		os.Exit(1)
	}
}

func cmdShow(args []string) {
	if len(args) == 0 {
		fmt.Println("Error: please specify a workflow YAML file")
		os.Exit(1)
	}

	data, err := os.ReadFile(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading file: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(string(data))
}

// buildAssistantFromEnv creates an AI Assistant from environment variables,
// for commands (explain/fix/generate/chat) that need an LLM but are not tied
// to a specific workflow's ai: config. Exits with a clear message if no key.
func buildAssistantFromEnv() *ai.Assistant {
	// Reuse the workflow/ai config: empty provider defaults to "anthropic",
	// which reads ANTHROPIC_API_KEY / DEEPSEEK_API_KEY + ANTHROPIC_BASE_URL.
	p, err := ai.BuildProvider(ai.Config{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "AI unavailable: %v\n", err)
		os.Exit(1)
	}
	return ai.NewAssistant(p)
}

// aiContext returns a context with a generous timeout for assistant calls.
func aiContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 120*time.Second)
}

// cmdExplain reads a workflow YAML file and prints a Chinese explanation of
// what it does, generated by the AI assistant.
func cmdExplain(args []string) {
	if len(args) == 0 {
		fmt.Println("Error: please specify a workflow YAML file")
		fmt.Println("Usage: goworkflow explain <file.yaml>")
		os.Exit(1)
	}

	data, err := os.ReadFile(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading file: %v\n", err)
		os.Exit(1)
	}

	assistant := buildAssistantFromEnv()
	ctx, cancel := aiContext()
	defer cancel()

	explanation, err := assistant.Explain(ctx, string(data))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(explanation)
}

// cmdFix validates a workflow YAML and, if there are errors, asks the AI to
// produce a fixed version (printed to stdout for redirection). If the workflow
// is already valid, reports that and exits.
func cmdFix(args []string) {
	if len(args) == 0 {
		fmt.Println("Error: please specify a workflow YAML file")
		fmt.Println("Usage: goworkflow fix <file.yaml>")
		os.Exit(1)
	}

	data, err := os.ReadFile(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading file: %v\n", err)
		os.Exit(1)
	}

	// Validate first.
	wf, perr := workflow.Parse(data)
	if perr == nil {
		errs := wf.Validate()
		if len(errs) == 0 {
			fmt.Fprintln(os.Stderr, "✅ Workflow is already valid; nothing to fix.")
			return
		}
		// Format validation errors for the model.
		var sb strings.Builder
		for _, e := range errs {
			fmt.Fprintf(&sb, "  - %v\n", e)
		}
		perr = fmt.Errorf("validation errors:\n%s", sb.String())
	}

	assistant := buildAssistantFromEnv()
	ctx, cancel := aiContext()
	defer cancel()

	fixed, err := assistant.Fix(ctx, string(data), perr.Error())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	// Print only the YAML so users can redirect: goworkflow fix x.yaml > fixed.yaml
	fmt.Print(fixed)
	if !strings.HasSuffix(fixed, "\n") {
		fmt.Println()
	}
}

// cmdGenerate generates a workflow YAML from a natural-language requirement.
// Prints to stdout (so users can redirect), auto-validates, and optionally
// saves to a file with --save.
func cmdGenerate(args []string) {
	if len(args) == 0 {
		fmt.Println("Error: please describe the workflow you want")
		fmt.Println(`Usage: goworkflow generate "<requirement>" [--save file.yaml]`)
		os.Exit(1)
	}

	savePath := ""
	requirementParts := []string{}
	for i := 0; i < len(args); i++ {
		if args[i] == "--save" || args[i] == "-o" {
			if i+1 < len(args) {
				savePath = args[i+1]
				i++
			}
			continue
		}
		requirementParts = append(requirementParts, args[i])
	}
	if len(requirementParts) == 0 {
		fmt.Fprintln(os.Stderr, "Error: no requirement given")
		os.Exit(1)
	}
	requirement := strings.Join(requirementParts, " ")

	assistant := buildAssistantFromEnv()
	ctx, cancel := aiContext()
	defer cancel()

	yaml, err := assistant.Generate(ctx, requirement)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Validate and report (non-fatal — print the YAML regardless so the user
	// can edit it).
	if wf, perr := workflow.Parse([]byte(yaml)); perr == nil {
		if errs := wf.Validate(); len(errs) == 0 {
			fmt.Fprintf(os.Stderr, "✅ Generated workflow '%s' is valid (%d steps)\n", wf.Name, len(wf.Steps))
		} else {
			fmt.Fprintf(os.Stderr, "⚠️  Generated workflow has %d validation issue(s):\n", len(errs))
			for _, e := range errs {
				fmt.Fprintf(os.Stderr, "  - %v\n", e)
			}
			fmt.Fprintln(os.Stderr, "Edit the output before using it.")
		}
	} else {
		fmt.Fprintf(os.Stderr, "⚠️  Generated YAML failed to parse: %v\n", perr)
	}

	if savePath != "" {
		if err := os.WriteFile(savePath, []byte(yaml), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "Error saving: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "💾 Saved to %s\n", savePath)
		return
	}
	// No --save: print YAML to stdout for redirection.
	fmt.Print(yaml)
	if !strings.HasSuffix(yaml, "\n") {
		fmt.Println()
	}
}

// cmdChat is the natural-language workflow trigger (mode D). It reads an
// intent, discovers workflows via a registry, asks the AI to select one and
// fill variables, then confirms with the user before executing.
func cmdChat(args []string) {
	// Parse flags: --dir, --yes, --output-mode; rest is the intent.
	dir := "."
	yes := false
	outputMode := "rich"
	intentParts := []string{}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--dir", "-d":
			if i+1 < len(args) {
				dir = args[i+1]
				i++
			}
		case "--yes", "-y":
			yes = true
		case "--output-mode", "-m":
			if i+1 < len(args) {
				outputMode = args[i+1]
				i++
			}
		default:
			intentParts = append(intentParts, args[i])
		}
	}

	intent := strings.Join(intentParts, " ")
	if strings.TrimSpace(intent) == "" {
		// Prompt for the intent interactively.
		fmt.Print("Describe what you want to do: ")
		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadString('\n')
		intent = strings.TrimSpace(line)
	}
	if strings.TrimSpace(intent) == "" {
		fmt.Fprintln(os.Stderr, "No intent given.")
		os.Exit(1)
	}

	// Discover workflows.
	registry := workflow.NewDirRegistry(dir)
	entries, err := registry.List()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing workflows in %s: %v\n", dir, err)
		os.Exit(1)
	}
	if len(entries) == 0 {
		fmt.Fprintf(os.Stderr, "No workflows found in %s. Use 'goworkflow generate' to create one.\n", dir)
		os.Exit(1)
	}

	// Convert to assistant candidates, exposing declared variable names.
	candidates := make([]ai.CandidateEntry, 0, len(entries))
	for _, e := range entries {
		var vars []string
		if wf, _, gerr := registry.Get(e.Name); gerr == nil {
			for k := range wf.Variables {
				vars = append(vars, k)
			}
		}
		candidates = append(candidates, ai.CandidateEntry{
			Name: e.Name, FileName: e.FileName,
			Description: e.Description, Steps: e.Steps, Variables: vars,
		})
	}

	assistant := buildAssistantFromEnv()
	ctx, cancel := aiContext()
	defer cancel()

	fmt.Printf("🔎 Interpreting: %s\n", intent)
	sel, err := assistant.SelectWorkflow(ctx, intent, candidates)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error selecting workflow: %v\n", err)
		os.Exit(1)
	}
	if sel.Workflow == "" {
		fmt.Println("🤔 No matching workflow found. Try 'goworkflow generate' to create one.")
		os.Exit(0)
	}

	// Verify the selection actually exists (the model can hallucinate).
	var chosen *workflow.WorkflowEntry
	for i := range entries {
		if entries[i].Name == sel.Workflow || entries[i].FileName == sel.Workflow {
			chosen = &entries[i]
			break
		}
	}
	if chosen == nil {
		fmt.Fprintf(os.Stderr, "Error: model selected %q which is not in the registry.\n", sel.Workflow)
		fmt.Fprintln(os.Stderr, "Available workflows:")
		for _, e := range entries {
			fmt.Fprintf(os.Stderr, "  - %s (%s)\n", e.Name, e.FileName)
		}
		os.Exit(1)
	}

	// Load the full workflow to get its declared variables (for missing-var
	// prompting and for execution).
	wf, _, err := registry.Get(chosen.Name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading %s: %v\n", chosen.Name, err)
		os.Exit(1)
	}

	// Merge: workflow defaults <- model-suggested values.
	vars := make(map[string]string)
	for k, v := range wf.Variables {
		vars[k] = v
	}
	for k, v := range sel.Variables {
		vars[k] = v
	}

	// Prompt for any declared variable still empty.
	reader := bufio.NewReader(os.Stdin)
	for k := range wf.Variables {
		if vars[k] == "" {
			fmt.Printf("  %s = ", k)
			line, _ := reader.ReadString('\n')
			vars[k] = strings.TrimSpace(line)
		}
	}

	// Show the plan and confirm.
	fmt.Println()
	fmt.Printf("📋 Selected workflow: %s", chosen.Name)
	if chosen.Description != "" {
		fmt.Printf("  (%s)", chosen.Description)
	}
	fmt.Println()
	if len(vars) > 0 {
		fmt.Println("Variables:")
		keys := sortedKeys(vars)
		for _, k := range keys {
			fmt.Printf("  %s = %s\n", k, vars[k])
		}
	}
	fmt.Printf("Steps: %d\n", len(wf.Steps))
	fmt.Println()

	if !yes {
		fmt.Print("Run this workflow? [y/N]: ")
		line, _ := reader.ReadString('\n')
		ans := strings.ToLower(strings.TrimSpace(line))
		if ans != "y" && ans != "yes" {
			fmt.Println("Cancelled.")
			return
		}
	}

	// Execute, reusing the run path.
	executor := workflow.NewExecutor(vars)
	executor.SetVerbose(true)
	executor.SetOutputMode(workflow.ParseOutputMode(outputMode))
	executor.SetTheme("default")
	result := executor.Execute(wf)
	if result != nil && result.Status == "failed" && result.Error != "" {
		fmt.Fprintf(os.Stderr, "Error: %s\n", result.Error)
		os.Exit(1)
	}
}

// sortedKeys returns map keys in sorted order for deterministic display.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// simple insertion sort (maps are small here)
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j-1] > keys[j]; j-- {
			keys[j-1], keys[j] = keys[j], keys[j-1]
		}
	}
	return keys
}

func cmdEdit(args []string) {
	if len(args) == 0 {
		fmt.Println("Error: please specify a workflow YAML file")
		os.Exit(1)
	}

	filePath := args[0]

	// Read and parse to validate
	_, err := workflow.ParseFile(filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading file: %v\n", err)
		os.Exit(1)
	}

	// Try to open in default editor
	editors := []string{
		"code",
		"notepad",
		"vim",
		"nano",
	}

	editor := ""
	for _, e := range editors {
		if _, err := os.Stat(filePath); err == nil {
			// Check if editor exists
			_, err := os.Stat(e)
			if err == nil {
				editor = e
				break
			}
		}
	}

	if editor != "" {
		fmt.Printf("Opening %s with %s...\n", filePath, editor)
		// We can't actually run the editor from here, but we can show instructions
		fmt.Println("Edit the YAML file, save it, then run the workflow again to see changes.")
	} else {
		fmt.Printf("📝 File: %s\n", filePath)
		fmt.Println("Edit this file to change workflow behavior. After editing, run:")
		fmt.Printf("  goworkflow run %s --verbose\n", filePath)
	}
}

func cmdTemplate(args []string) {
	example := `name: example-workflow
version: "1.0"
description: "A complete example demonstrating all workflow features"

variables:
  app_name: MyApp
  env: dev
  version: "1.0.0"

steps:
  # Log a message
  - name: greet
    action: log
    message: "Deploying {{.app_name}} v{{.version}} to {{.env}}"
    level: info

  # Execute shell commands
  - name: check-env
    action: shell
    command: echo "Current directory: $(pwd)"
    description: "Show current directory"

  # Set a variable
  - name: set-timestamp
    action: set
    value: "2026-01-01T00:00:00Z"

  # HTTP request
  - name: fetch-data
    action: http
    url: "https://httpbin.org/get"
    method: GET
    timeout: "10s"
    save_output: response

  # Conditional branching
  - name: check-response
    action: condition
    expression: "{{.env}} == prod"
    then:
      - name: deploy-prod
        action: log
        message: "Production deployment!"
        level: warn
    else:
      - name: deploy-dev
        action: log
        message: "Development deployment"

  # Parallel execution
  - name: parallel-tasks
    action: parallel
    steps:
      - name: task-a
        action: shell
        command: echo "Task A completed"
      - name: task-b
        action: shell
        command: echo "Task B completed"
      - name: task-c
        action: shell
        command: echo "Task C completed"

  # Sleep
  - name: wait
    action: sleep
    duration: "1s"

  # Foreach loop
  - name: iterate
    action: foreach
    items:
      - "alpha"
      - "beta"
      - "gamma"
    item_var: service
    do:
      - name: process-{{ "{{.service}}" }}
        action: log
        message: "Processing {{.service}}"

  # Final log
  - name: complete
    action: log
    message: "Workflow finished!"
    level: info
`
	fmt.Println("Example workflow YAML:")
	fmt.Println(example)
}
