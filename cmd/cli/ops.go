package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/whitefirer/seneschal/workflow"
)

// ── create ───────────────────────────────────────────────────────────────────

var createOpts struct {
	output string
}

var createCmd = &cobra.Command{
	Use:     "create <name> [description...]",
	Short:   "Create a new workflow YAML",
	GroupID: "workflow",
	Args:    cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return createWorkflow(args[0], strings.Join(args[1:], " "))
	},
}

func init() {
	createCmd.Flags().StringVarP(&createOpts.output, "output", "o", "", "output file (default <name>.yaml)")
}

// createWorkflow is the former cmdCreate logic. Note: --output now actually
// works — the old hand-rolled parser swallowed it as an output-MODE alias.
func createWorkflow(name, description string) error {
	outputFile := createOpts.output
	if outputFile == "" {
		outputFile = name + ".yaml"
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
		Name:    "done",
		Action:  "log",
		Message: "Workflow completed successfully!",
		Level:   "info",
	})

	// Save
	if err := wf.Save(outputFile); err != nil {
		return fmt.Errorf("saving workflow: %w", err)
	}

	fmt.Printf("✅ Created workflow '%s' → %s\n", name, outputFile)
	fmt.Println("Edit the YAML file to customize your workflow, then run:")
	fmt.Printf("  seneschal run %s --verbose\n", outputFile)
	return nil
}

// ── validate ─────────────────────────────────────────────────────────────────

var validateCmd = &cobra.Command{
	Use:     "validate <file.yaml>",
	Short:   "Validate workflow syntax",
	GroupID: "workflow",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return validateWorkflow(args[0])
	},
}

// validateWorkflow is the former cmdValidate logic. Validation errors are
// printed to stdout (preserved — e2e asserts on stdout), so the returned
// error is the quiet sentinel.
func validateWorkflow(filePath string) error {
	wf, err := workflow.ParseFile(filePath)
	if err != nil {
		return err
	}

	errs := wf.Validate()
	if len(errs) == 0 {
		fmt.Printf("✅ Workflow '%s' is valid (%d steps)\n", wf.Name, len(wf.Steps))
		return nil
	}
	fmt.Printf("❌ Workflow '%s' has %d error(s):\n", wf.Name, len(errs))
	for _, e := range errs {
		fmt.Printf("  • %v\n", e)
	}
	return errExitQuiet
}

// ── show ─────────────────────────────────────────────────────────────────────

var showCmd = &cobra.Command{
	Use:     "show <file.yaml>",
	Short:   "Display workflow YAML content",
	GroupID: "workflow",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := os.ReadFile(args[0])
		if err != nil {
			return fmt.Errorf("reading file: %w", err)
		}
		fmt.Println(string(data))
		return nil
	},
}

// ── edit ─────────────────────────────────────────────────────────────────────

var editCmd = &cobra.Command{
	Use:     "edit <file.yaml>",
	Short:   "Open YAML in editor",
	GroupID: "workflow",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		filePath := args[0]

		// Read and parse to validate
		if _, err := workflow.ParseFile(filePath); err != nil {
			return fmt.Errorf("reading file: %w", err)
		}

		// We don't launch an editor from here; just point the user at the file.
		fmt.Printf("📝 File: %s\n", filePath)
		fmt.Println("Edit this file to change workflow behavior. After editing, run:")
		fmt.Printf("  seneschal run %s --verbose\n", filePath)
		return nil
	},
}

// ── template ─────────────────────────────────────────────────────────────────

var templateCmd = &cobra.Command{
	Use:     "template",
	Short:   "Show example workflow YAML",
	GroupID: "workflow",
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Example workflow YAML:")
		fmt.Print(templateYAML)
		return nil
	},
}

const templateYAML = `name: example-workflow
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

// ── list ─────────────────────────────────────────────────────────────────────

var listCmd = &cobra.Command{
	Use:     "list [directory]",
	Short:   "List all workflows in a directory",
	GroupID: "workflow",
	Args:    cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("List all workflow files in a directory:")
		fmt.Println("Usage: seneschal list <directory>")
		if len(args) == 0 {
			return nil
		}
		entries, err := os.ReadDir(args[0])
		if err != nil {
			return err
		}
		for _, e := range entries {
			if !e.IsDir() && (strings.HasSuffix(e.Name(), ".yaml") || strings.HasSuffix(e.Name(), ".yml")) {
				fmt.Printf("  📄 %s\n", e.Name())
			}
		}
		return nil
	},
}
