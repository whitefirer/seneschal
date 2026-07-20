package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/whitefirer/seneschal/workflow"
	"github.com/whitefirer/seneschal/workflow/ai"
)

var chatOpts struct {
	workflowsDir       string
	workflowsDirLegacy string
	yes                bool
	outputMode         string
}

var chatCmd = &cobra.Command{
	Use:     "chat [intent...]",
	Short:   "Natural-language workflow trigger",
	GroupID: "ai",
	Args:    cobra.ArbitraryArgs,
	Example: `  seneschal chat "部署到staging"
  seneschal chat --yes --workflows-dir ./workflows "run the tests"`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return chatRun(strings.Join(args, " "))
	},
}

func init() {
	f := chatCmd.Flags()
	f.StringVar(&chatOpts.workflowsDir, "workflows-dir", ".", "directory to discover workflow files in")
	f.StringVarP(&chatOpts.workflowsDirLegacy, "dir", "d", "", "alias for --workflows-dir")
	_ = f.MarkDeprecated("dir", "use --workflows-dir instead")
	f.BoolVarP(&chatOpts.yes, "yes", "y", false, "skip confirmation prompt")
	f.StringVarP(&chatOpts.outputMode, "output-mode", "m", "rich", "output mode for the executed workflow")
}

// chatRun is the natural-language workflow trigger (mode D). It reads an
// intent, discovers workflows via a registry, asks the AI to select one and
// fill variables, then confirms with the user before executing.
// (Former cmdChat; flag parsing now handled by cobra — note the old --dir
// collision is resolved: chat now uses --workflows-dir, with --dir kept as a
// deprecated alias.)
func chatRun(intent string) error {
	dir := resolveDir(chatOpts.workflowsDir, chatOpts.workflowsDirLegacy)

	if strings.TrimSpace(intent) == "" {
		// Prompt for the intent interactively.
		fmt.Print("Describe what you want to do: ")
		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadString('\n')
		intent = strings.TrimSpace(line)
	}
	if strings.TrimSpace(intent) == "" {
		return fmt.Errorf("no intent given")
	}

	// Discover workflows.
	registry := workflow.NewDirRegistry(dir)
	entries, err := registry.List()
	if err != nil {
		return fmt.Errorf("listing workflows in %s: %w", dir, err)
	}
	if len(entries) == 0 {
		return fmt.Errorf("no workflows found in %s — use 'seneschal generate' to create one", dir)
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

	assistant, err := buildAssistantFromEnv()
	if err != nil {
		return err
	}
	ctx, cancel := aiContext()
	defer cancel()

	fmt.Printf("🔎 Interpreting: %s\n", intent)
	sel, err := assistant.SelectWorkflow(ctx, intent, candidates)
	if err != nil {
		return fmt.Errorf("selecting workflow: %w", err)
	}
	if sel.Workflow == "" {
		fmt.Println("🤔 No matching workflow found. Try 'seneschal generate' to create one.")
		return nil
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
		return errExitQuiet
	}

	// Load the full workflow to get its declared variables (for missing-var
	// prompting and for execution).
	wf, _, err := registry.Get(chosen.Name)
	if err != nil {
		return fmt.Errorf("loading %s: %w", chosen.Name, err)
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

	if !chatOpts.yes {
		fmt.Print("Run this workflow? [y/N]: ")
		line, _ := reader.ReadString('\n')
		ans := strings.ToLower(strings.TrimSpace(line))
		if ans != "y" && ans != "yes" {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	// Execute, reusing the run path.
	executor := workflow.NewExecutor(vars)
	executor.SetVerbose(true)
	executor.SetOutputMode(workflow.ParseOutputMode(chatOpts.outputMode))
	executor.SetTheme("default")
	result := executor.Execute(wf)
	if result != nil && result.Status == "failed" && result.Error != "" {
		return fmt.Errorf("%s", result.Error)
	}
	return nil
}
