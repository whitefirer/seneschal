package main

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/whitefirer/seneschal/workflow"
)

var runOpts struct {
	vars          []string
	verbose       bool
	dryRun        bool
	outputMode    string
	outputLegacy  string
	theme         string
	forceColor    bool
	tuiStyle      string
	execDir       string
	execDirLegacy string
}

var runCmd = &cobra.Command{
	Use:     "run <file.yaml>",
	Short:   "Execute a workflow YAML file",
	GroupID: "workflow",
	Args:    cobra.MinimumNArgs(1),
	Example: `  seneschal run deploy.yaml --verbose --var ENV=prod
  seneschal run deploy.yaml -m html > report.html
  seneschal run deploy.yaml -m json | jq .status
  seneschal run deploy.yaml --dry-run`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runWorkflowFile(args[0])
	},
}

func init() {
	f := runCmd.Flags()
	f.StringArrayVar(&runOpts.vars, "var", nil, "override workflow variable (key=value, repeatable)")
	f.BoolVarP(&runOpts.verbose, "verbose", "v", false, "verbose output")
	f.BoolVar(&runOpts.dryRun, "dry-run", false, "preview without executing")
	f.StringVarP(&runOpts.outputMode, "output-mode", "m", "rich", "output mode: rich/plain/compact/dag/timeline/tui/json/html")
	f.StringVar(&runOpts.outputLegacy, "output", "", "alias for --output-mode")
	_ = f.MarkDeprecated("output", "use --output-mode instead")
	f.StringVarP(&runOpts.theme, "theme", "t", "default", "theme: default/claude/dark/light/monokai/ocean")
	f.BoolVarP(&runOpts.forceColor, "force-color", "f", false, "force color output when piped")
	f.StringVar(&runOpts.tuiStyle, "tui-style", "", "TUI style: hermes or claude")
	f.StringVar(&runOpts.execDir, "exec-dir", executionsDir(), "directory for execution records")
	f.StringVarP(&runOpts.execDirLegacy, "dir", "d", "", "alias for --exec-dir")
	_ = f.MarkDeprecated("dir", "use --exec-dir instead")
}

// runWorkflowFile executes a workflow YAML file. Ported from the pre-cobra
// cmdRun; flag parsing is now handled by cobra (see runOpts above).
func runWorkflowFile(filePath string) error {
	vars := parseVarFlags(runOpts.vars)

	// Load workflow
	wf, err := workflow.ParseFile(filePath)
	if err != nil {
		return fmt.Errorf("loading workflow: %w", err)
	}

	// Merge CLI vars with workflow vars
	for k, v := range wf.Variables {
		if _, ok := vars[k]; !ok {
			vars[k] = v
		}
	}

	// Parse output mode (deprecated --output alias wins when given).
	outputModeStr := runOpts.outputMode
	if runOpts.outputLegacy != "" {
		outputModeStr = runOpts.outputLegacy
	}
	outputMode := workflow.ParseOutputMode(outputModeStr)

	// Execute
	executor := workflow.NewExecutor(vars)
	executor.SetVerbose(runOpts.verbose)
	executor.SetDryRun(runOpts.dryRun)
	executor.SetForceColor(runOpts.forceColor)
	executor.SetTuiStyle(runOpts.tuiStyle)
	executor.SetOutputMode(outputMode)
	executor.SetTheme(runOpts.theme)

	result := executor.Execute(wf)
	// Surface a configuration error (e.g. missing AI key) that Execute reports
	// before any step runs. Step-level failures are already printed by printers.
	if result != nil && result.Status == "failed" && result.Error != "" {
		return fmt.Errorf("%s", result.Error)
	}

	// Persist the execution so it can be replayed. Best-effort: a failure
	// (e.g. no executions dir) is logged but does not fail the run.
	if !runOpts.dryRun {
		execID := fmt.Sprintf("exec-%s-%s", time.Now().Format("20060102-150405"), randomHexCLI(4))
		if raw, rerr := os.ReadFile(filePath); rerr == nil {
			store := workflow.NewFileStore(resolveDir(runOpts.execDir, runOpts.execDirLegacy))
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
					WorkflowFile:     filepathBase(filePath),
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
	return nil
}
