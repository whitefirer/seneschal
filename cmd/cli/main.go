package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
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
	case "help", "-h", "--help":
		printUsage()
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
	case "replay":
		cmdReplay(os.Args[2:])
	case "history":
		cmdHistory(os.Args[2:])
	case "runbook":
		cmdRunbook(os.Args[2:])
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
	fmt.Println("Commands:")
	fmt.Println()
	fmt.Println("  Workflow Operations:")
	fmt.Println("    run <file>              Execute a workflow YAML file")
	fmt.Println("    validate <file>         Validate workflow syntax")
	fmt.Println("    show <file>             Display workflow YAML content")
	fmt.Println("    edit <file>             Open YAML in editor")
	fmt.Println("    create <name> [desc]    Create a new workflow YAML")
	fmt.Println("    template                Show example workflow YAML")
	fmt.Println("    list <dir>              List all workflows in a directory")
	fmt.Println()
	fmt.Println("  AI Assistant (requires AI provider config):")
	fmt.Println("    chat [intent]           Natural-language workflow trigger")
	fmt.Println("    generate <requirement>  Generate a new workflow from description")
	fmt.Println("    explain <file>          Explain what a workflow does (Chinese)")
	fmt.Println("    fix <file>              Fix workflow validation errors")
	fmt.Println()
	fmt.Println("  Execution History & Replay:")
	fmt.Println("    replay <id>             Smart-replay a past execution")
	fmt.Println("    history list            List execution history")
	fmt.Println("    history show <id>       Show execution details")
	fmt.Println("    history purge [--keep N] Delete old executions (default keep 50)")
	fmt.Println("    history delete <id>     Delete a single execution")
	fmt.Println()
	fmt.Println("  Runbook (Trigger & Schedule):")
	fmt.Println("    runbook list            List all runbooks")
	fmt.Println("    runbook show <name>     Show runbook config")
	fmt.Println("    runbook trigger <name>  Manually trigger a runbook")
	fmt.Println("    runbook create <name>   Create a new runbook")
	fmt.Println()
	fmt.Println("  Output Modes (--output-mode / -m):")
	fmt.Println("    rich                    Rich styled output (default)")
	fmt.Println("    plain                   Plain text output")
	fmt.Println("    compact                 Compact output for CI/CD")
	fmt.Println("    dag                     DAG graph visualization")
	fmt.Println("    timeline                Timeline view with progress bars")
	fmt.Println("    tui                     Terminal UI with live progress")
	fmt.Println("    json                    JSON output (machine-readable)")
	fmt.Println("    html                    HTML report (shareable)")
	fmt.Println()
	fmt.Println("  Run Flags:")
	fmt.Println("    --var key=value         Override workflow variable")
	fmt.Println("    --verbose, -v           Verbose output")
	fmt.Println("    --dry-run               Preview without executing")
	fmt.Println("    --output-mode, -m       Set output mode (see above)")
	fmt.Println("    --theme, -t             Theme: default/claude/dark/light/monokai/ocean")
	fmt.Println("    --force-color, -f       Force color output when piped")
	fmt.Println()
	fmt.Println("  Actions (in workflow YAML):")
	fmt.Println("    shell http condition set sleep log parallel foreach")
	fmt.Println("    template script ai ai_decide workflow")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  goworkflow run deploy.yaml --verbose --var ENV=prod")
	fmt.Println("  goworkflow run deploy.yaml -m html > report.html")
	fmt.Println("  goworkflow run deploy.yaml -m json | jq .status")
	fmt.Println("  goworkflow chat \"部署到staging\"")
	fmt.Println("  goworkflow generate \"每晚跑测试并通知\"")
	fmt.Println("  goworkflow explain deploy.yaml")
	fmt.Println("  goworkflow replay exec-20260707-120000-xxxx")
	fmt.Println("  goworkflow history list")
	fmt.Println("  goworkflow runbook trigger nightly-deploy --var env=prod")
	fmt.Println()
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

	// Persist the execution so it can be replayed. Best-effort: a failure
	// (e.g. no executions dir) is logged but does not fail the run.
	if !dryRun {
		execID := fmt.Sprintf("exec-%s-%s", time.Now().Format("20060102-150405"), randomHexCLI(4))
		if raw, rerr := os.ReadFile(args[0]); rerr == nil {
			store := workflow.NewFileStore(executionsDir())
			dur := ""
			if start, e := parseRFC3339(result.StartTime); e == nil {
				if end, e := parseRFC3339(result.EndTime); e == nil {
					dur = end.Sub(start).String()
				}
			}
			_ = store.Save(workflow.ExecutionSnapshot{
				ExecutionSummary: workflow.ExecutionSummary{
					ID:               execID,
					WorkflowName:     wf.Name,
					WorkflowFile:     filepathBase(args[0]),
					Status:           result.Status,
					StartTime:        result.StartTime,
					EndTime:          result.EndTime,
					Duration:         dur,
					Error:            result.Error,
					StepsCount:       len(wf.Steps),
					Nondeterministic: result.Nondeterministic,
				},
				Steps:     result.Steps,
				Variables: result.Variables,
				Workflow:  string(raw),
			})
			// Print the ID so users can replay it. Goes to stderr to keep stdout clean.
			fmt.Fprintf(os.Stderr, "📝 Execution ID: %s\n", execID)
		}
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

// executionsDir is the default CLI history directory (matches the server
// default so CLI runs and replays share history).
func executionsDir() string { return "./executions" }

// randomHexCLI returns n hex characters of randomness for execution IDs.
func randomHexCLI(n int) string {
	const hex = "0123456789abcdef"
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "0000"
	}
	out := make([]byte, n)
	for i, c := range b {
		out[i] = hex[c%16]
	}
	return string(out)
}

func parseRFC3339(s string) (time.Time, error) { return time.Parse(time.RFC3339, s) }
func filepathBase(p string) string {
	// strip dir + yaml suffix to mirror the server's WorkflowFile field.
	base := p
	if i := strings.LastIndexAny(base, "/\\"); i >= 0 {
		base = base[i+1:]
	}
	return strings.TrimSuffix(strings.TrimSuffix(base, ".yaml"), ".yml")
}

// cmdReplay loads a historical execution and re-runs it: deterministic steps
// reuse their recorded output, nondeterministic (AI) steps re-execute.
func cmdReplay(args []string) {
	if len(args) == 0 {
		fmt.Println("Error: please specify an execution ID")
		fmt.Println("Usage: goworkflow replay <exec-id> [--dir DIR] [--full] [--step NAME]")
		os.Exit(1)
	}

	dir := executionsDir()
	full := false
	outputMode := "rich"
	var onlySteps []string
	id := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--dir", "-d":
			if i+1 < len(args) {
				dir = args[i+1]
				i++
			}
		case "--full":
			full = true
		case "--output-mode", "-m":
			if i+1 < len(args) {
				outputMode = args[i+1]
				i++
			}
		case "--step":
			if i+1 < len(args) {
				onlySteps = append(onlySteps, args[i+1])
				i++
			}
		default:
			if id == "" {
				id = args[i]
			}
		}
	}
	if id == "" {
		fmt.Fprintln(os.Stderr, "Error: execution ID required")
		os.Exit(1)
	}

	store := workflow.NewFileStore(dir)
	replayer := workflow.NewReplayer(store)

	// Build an AI provider from env so AI steps can re-run. If unavailable,
	// replay still works for purely deterministic workflows.
	executor := workflow.NewExecutor(nil)
	executor.SetVerbose(true)
	executor.SetOutputMode(workflow.ParseOutputMode(outputMode))
	executor.SetTheme("default")
	if p, err := ai.BuildProvider(ai.Config{}); err == nil {
		executor.SetAIProvider(p)
	}

	opts := workflow.ReplayOptions{Full: full, OnlySteps: onlySteps}
	result, hits, misses, err := replayer.Replay(id, executor, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	mode := "smart"
	if full {
		mode = "full"
	}
	fmt.Fprintf(os.Stderr, "\n🔁 Replay (%s): %d step(s) reused, %d step(s) re-executed\n", mode, hits, misses)
	if result != nil {
		fmt.Fprintf(os.Stderr, "   Result: %s\n", result.Status)
	}
}

// cmdHistory manages execution history: list, show, purge, delete.
// Usage:
//   goworkflow history list [--dir DIR]
//   goworkflow history show <id> [--dir DIR]
//   goworkflow history purge [--dir DIR] [--keep N]
//   goworkflow history delete <id> [--dir DIR]
func cmdHistory(args []string) {
	if len(args) == 0 {
		fmt.Println(`Usage:
  goworkflow history list [--dir DIR]
  goworkflow history show <id> [--dir DIR]
  goworkflow history purge [--dir DIR] [--keep N]
  goworkflow history delete <id> [--dir DIR]`)
		return
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "list":
		historyList(rest)
	case "show":
		historyShow(rest)
	case "purge":
		historyPurge(rest)
	case "delete", "rm":
		historyDelete(rest)
	default:
		fmt.Fprintf(os.Stderr, "Unknown history subcommand: %s\n", sub)
		os.Exit(1)
	}
}

// historyDir extracts --dir from args, defaulting to ./executions.
func historyDir(args []string) string {
	dir := executionsDir()
	for i := 0; i < len(args); i++ {
		if args[i] == "--dir" || args[i] == "-d" {
			if i+1 < len(args) {
				return args[i+1]
			}
		}
	}
	return dir
}

func historyList(args []string) {
	store := workflow.NewFileStore(historyDir(args))
	summaries, err := store.List()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if len(summaries) == 0 {
		fmt.Println("(no execution history)")
		return
	}
	fmt.Printf("%-28s  %-20s  %-8s  %-20s  %s\n", "ID", "WORKFLOW", "STATUS", "STARTED", "DURATION")
	for _, s := range summaries {
		dur := s.Duration
		if dur == "" {
			dur = "-"
		}
		nd := ""
		if s.Nondeterministic {
			nd = " ⚡"
		}
		fmt.Printf("%-28s  %-20s  %-8s  %-20s  %s%s\n", s.ID, truncStr(s.WorkflowName, 20), s.Status, s.StartTime, dur, nd)
	}
}

func historyShow(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Error: execution ID required")
		os.Exit(1)
	}
	id := args[0]
	store := workflow.NewFileStore(historyDir(args))
	snap, err := store.Get(id)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("ID:          %s\n", snap.ID)
	fmt.Printf("Workflow:    %s\n", snap.WorkflowName)
	fmt.Printf("Status:      %s\n", snap.Status)
	if snap.StartTime != "" {
		fmt.Printf("Started:     %s\n", snap.StartTime)
	}
	if snap.EndTime != "" {
		fmt.Printf("Ended:       %s\n", snap.EndTime)
	}
	if snap.Duration != "" {
		fmt.Printf("Duration:    %s\n", snap.Duration)
	}
	if snap.Error != "" {
		fmt.Printf("Error:       %s\n", snap.Error)
	}
	if snap.Nondeterministic {
		fmt.Printf("Nondeterm:   true (contains AI steps)\n")
	}
	if len(snap.Variables) > 0 {
		fmt.Println("Variables:")
		for _, k := range sortedKeys(snap.Variables) {
			fmt.Printf("  %s = %s\n", k, snap.Variables[k])
		}
	}
	if len(snap.Steps) > 0 {
		fmt.Println("Steps:")
		for _, s := range snap.Steps {
			flag := ""
			if s.Nondeterministic {
				flag = " ⚡AI"
			}
			fmt.Printf("  [%s] %s (%s)%s\n", s.Status, s.Name, s.Action, flag)
			if s.Output != "" {
				for _, line := range strings.Split(strings.TrimSpace(s.Output), "\n") {
					fmt.Printf("      %s\n", line)
				}
			}
		}
	}
}

func historyPurge(args []string) {
	dir := historyDir(args)
	keep := 50
	for i := 0; i < len(args); i++ {
		if args[i] == "--keep" || args[i] == "-k" {
			if i+1 < len(args) {
				if n, err := fmt.Sscanf(args[i+1], "%d", &keep); err == nil && n == 1 {
					i++
				}
			}
		}
	}
	store := workflow.NewFileStore(dir)
	deleted, err := store.Purge(keep)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Deleted %d execution(s), kept most recent %d.\n", deleted, keep)
}

func historyDelete(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Error: execution ID required")
		os.Exit(1)
	}
	id := args[0]
	store := workflow.NewFileStore(historyDir(args))
	if err := store.Delete(id); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Deleted %s\n", id)
}

func truncStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
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

// cmdRunbook manages runbooks: list, show, trigger, create.
func cmdRunbook(args []string) {
	if len(args) == 0 {
		fmt.Println(`Usage:
  goworkflow runbook list [--dir DIR] [--server URL]
  goworkflow runbook show <name> [--dir DIR] [--server URL]
  goworkflow runbook trigger <name> [--var key=val ...] [--dir DIR] [--server URL]
  goworkflow runbook create <name> --workflow <file> [--cron "..."] [--webhook "/path"] [--var key=val ...] [--dir DIR]`)
		return
	}

	sub := args[0]
	rest := args[1:]

	// Parse common flags: --dir, --server
	dir := "./runbooks"
	serverURL := ""
	vars := map[string]string{}
	positional := []string{}
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case "--dir", "-d":
			if i+1 < len(rest) {
				dir = rest[i+1]
				i++
			}
		case "--server", "-s":
			if i+1 < len(rest) {
				serverURL = rest[i+1]
				i++
			}
		case "--var":
			if i+1 < len(rest) {
				kv := strings.SplitN(rest[i+1], "=", 2)
				if len(kv) == 2 {
					vars[kv[0]] = kv[1]
				}
				i++
			}
		default:
			positional = append(positional, rest[i])
		}
	}

	switch sub {
	case "list":
		runbookList(dir, serverURL)
	case "show":
		if len(positional) == 0 {
			fmt.Fprintln(os.Stderr, "Error: runbook name required")
			os.Exit(1)
		}
		runbookShow(positional[0], dir, serverURL)
	case "trigger":
		if len(positional) == 0 {
			fmt.Fprintln(os.Stderr, "Error: runbook name required")
			os.Exit(1)
		}
		runbookTrigger(positional[0], vars, dir, serverURL)
	case "create":
		if len(positional) == 0 {
			fmt.Fprintln(os.Stderr, "Error: runbook name required")
			os.Exit(1)
		}
		runbookCreate(positional[0], rest, dir)
	default:
		fmt.Fprintf(os.Stderr, "Unknown runbook subcommand: %s\n", sub)
		os.Exit(1)
	}
}

func runbookList(dir, serverURL string) {
	if serverURL != "" {
		// Query server API.
		resp, err := http.Get(serverURL + "/api/runbooks")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		defer resp.Body.Close()
		var result struct {
			Success bool                     `json:"success"`
			Data    []map[string]interface{} `json:"data"`
		}
		json.NewDecoder(resp.Body).Decode(&result)
		if !result.Success || len(result.Data) == 0 {
			fmt.Println("(no runbooks)")
			return
		}
		fmt.Printf("%-25s  %-25s  %s\n", "NAME", "WORKFLOW", "TRIGGERS")
		for _, rb := range result.Data {
			name, _ := rb["Name"].(string)
			wf, _ := rb["Workflow"].(string)
			triggers := formatTriggersJSON(rb["Triggers"])
			fmt.Printf("%-25s  %-25s  %s\n", name, wf, triggers)
		}
		return
	}
	// Local: scan dir.
	mgr := workflow.NewRunbookManager(dir, ".", nil, nil)
	mgr.LoadDir()
	runbooks := mgr.List()
	if len(runbooks) == 0 {
		fmt.Println("(no runbooks)")
		return
	}
	fmt.Printf("%-25s  %-25s  %s\n", "NAME", "WORKFLOW", "TRIGGERS")
	for _, rb := range runbooks {
		fmt.Printf("%-25s  %-25s  %s\n", rb.Name, rb.Workflow, formatTriggers(rb.Triggers))
	}
}

func runbookShow(name, dir, serverURL string) {
	if serverURL != "" {
		resp, err := http.Get(serverURL + "/api/runbooks/" + name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		defer resp.Body.Close()
		var result struct {
			Success bool                   `json:"success"`
			Data    map[string]interface{} `json:"data"`
		}
		json.NewDecoder(resp.Body).Decode(&result)
		if !result.Success {
			fmt.Fprintln(os.Stderr, "Runbook not found")
			os.Exit(1)
		}
		data, _ := json.MarshalIndent(result.Data, "", "  ")
		fmt.Println(string(data))
		return
	}
	mgr := workflow.NewRunbookManager(dir, ".", nil, nil)
	mgr.LoadDir()
	rb := mgr.Get(name)
	if rb == nil {
		fmt.Fprintln(os.Stderr, "Runbook not found")
		os.Exit(1)
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
}

func runbookTrigger(name string, vars map[string]string, dir, serverURL string) {
	if serverURL != "" {
		body, _ := json.Marshal(vars)
		resp, err := http.Post(serverURL+"/api/runbooks/"+name+"/trigger", "application/json", bytes.NewReader(body))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		defer resp.Body.Close()
		var result struct {
			Success bool   `json:"success"`
			Error   string `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&result)
		if !result.Success {
			fmt.Fprintf(os.Stderr, "Error: %s\n", result.Error)
			os.Exit(1)
		}
		fmt.Printf("✅ Triggered: %s\n", name)
		return
	}
	// Local: resolve and execute directly.
	mgr := workflow.NewRunbookManager(dir, ".", nil, nil)
	mgr.LoadDir()
	rb := mgr.Get(name)
	if rb == nil {
		fmt.Fprintf(os.Stderr, "Runbook %q not found\n", name)
		os.Exit(1)
	}
	wfPath, err := mgr.ResolveWorkflowPath(rb)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	wf, err := workflow.ParseFile(wfPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
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
		fmt.Fprintf(os.Stderr, "Error: %s\n", result.Error)
		os.Exit(1)
	}
}

func runbookCreate(name string, args []string, dir string) {
	// Parse create-specific flags.
	wfFile := ""
	cron := ""
	webhookPath := ""
	vars := map[string]string{}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--workflow", "-w":
			if i+1 < len(args) {
				wfFile = args[i+1]
				i++
			}
		case "--cron":
			if i+1 < len(args) {
				cron = args[i+1]
				i++
			}
		case "--webhook":
			if i+1 < len(args) {
				webhookPath = args[i+1]
				i++
			}
		case "--var":
			if i+1 < len(args) {
				kv := strings.SplitN(args[i+1], "=", 2)
				if len(kv) == 2 {
					vars[kv[0]] = kv[1]
				}
				i++
			}
		}
	}
	if wfFile == "" {
		fmt.Fprintln(os.Stderr, "Error: --workflow is required")
		os.Exit(1)
	}

	// Build runbook YAML.
	var sb strings.Builder
	fmt.Fprintf(&sb, "name: %s\n", name)
	fmt.Fprintf(&sb, "workflow: %s\n", wfFile)
	fmt.Fprintf(&sb, "triggers:\n")
	fmt.Fprintf(&sb, "  - type: manual\n")
	if cron != "" {
		fmt.Fprintf(&sb, "  - type: cron\n")
		fmt.Fprintf(&sb, "    cron: \"%s\"\n", cron)
	}
	if webhookPath != "" {
		fmt.Fprintf(&sb, "  - type: webhook\n")
		fmt.Fprintf(&sb, "    path: \"%s\"\n", webhookPath)
	}
	if len(vars) > 0 {
		fmt.Fprintf(&sb, "variables:\n")
		for k, v := range vars {
			fmt.Fprintf(&sb, "  %s: %s\n", k, v)
		}
	}

	// Write file.
	os.MkdirAll(dir, 0755)
	path := filepath.Join(dir, name+".yaml")
	if err := os.WriteFile(path, []byte(sb.String()), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✅ Created runbook: %s\n", path)
	fmt.Println(sb.String())
}

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
