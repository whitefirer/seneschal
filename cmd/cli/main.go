package main

import (
	"fmt"
	"os"
	"strings"

	"goworkflow/workflow"
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
	fmt.Println("  goworkflow run <file.yaml> [--var key=value ...] [--verbose] [--dry-run] [--output-mode MODE]")
	fmt.Println("")
	fmt.Println("Output Modes:")
	fmt.Println("  plain     - Plain text output (default)")
	fmt.Println("  rich      - Rich styled output with colors and icons")
	fmt.Println("  dag       - DAG graph visualization")
	fmt.Println("  timeline  - Timeline view with progress bars")
	fmt.Println("  compact   - Compact output suitable for CI/CD")
	fmt.Println("  realtime  - Real-time TUI progress with animation")
	fmt.Println("")
	fmt.Println("Themes:")
	fmt.Println("  default   - Default blue theme")
	fmt.Println("  dark      - Dark purple theme")
	fmt.Println("  light     - Light green theme")
	fmt.Println("  monokai   - Monokai theme")
	fmt.Println("  ocean     - Ocean blue theme")
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

func parseFlags(args []string) (vars map[string]string, verbose bool, dryRun bool, outputMode string, themeName string, remaining []string) {
	outputMode = "plain"
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
		default:
			remaining = append(remaining, args[i])
		}
	}
	return
}

func cmdRun(args []string) {
	vars, verbose, dryRun, outputModeStr, themeName, remaining := parseFlags(args)
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
	executor.SetOutputMode(outputMode)
	executor.SetTheme(themeName)

	executor.Execute(wf)
}

func cmdCreate(args []string) {
	_, _, _, _, _, remaining := parseFlags(args)

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
