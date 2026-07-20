package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/whitefirer/seneschal/workflow"
)

var historyOpts struct {
	execDir       string
	execDirLegacy string
}

// historyCmd manages execution history: list, show, purge, delete.
var historyCmd = &cobra.Command{
	Use:     "history",
	Short:   "Manage execution history",
	GroupID: "history",
}

func init() {
	p := historyCmd.PersistentFlags()
	p.StringVar(&historyOpts.execDir, "exec-dir", executionsDir(), "directory of execution records")
	p.StringVarP(&historyOpts.execDirLegacy, "dir", "d", "", "alias for --exec-dir")
	_ = p.MarkDeprecated("dir", "use --exec-dir instead")
	historyCmd.AddCommand(historyListCmd, historyShowCmd, historyPurgeCmd, historyDeleteCmd)
}

// historyDir resolves the executions directory from the flags (deprecated
// --dir alias wins when given).
func historyDir() string {
	return resolveDir(historyOpts.execDir, historyOpts.execDirLegacy)
}

// ── history list ─────────────────────────────────────────────────────────────

var historyListCmd = &cobra.Command{
	Use:   "list",
	Short: "List execution history",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return historyList(historyDir())
	},
}

func historyList(dir string) error {
	store := workflow.NewFileStore(dir)
	summaries, err := store.List()
	if err != nil {
		return err
	}
	if len(summaries) == 0 {
		fmt.Println("(no execution history)")
		return nil
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
	return nil
}

// ── history show ─────────────────────────────────────────────────────────────

var historyShowCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Show execution details",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return historyShow(args[0], historyDir())
	},
}

func historyShow(id, dir string) error {
	store := workflow.NewFileStore(dir)
	snap, err := store.Get(id)
	if err != nil {
		return err
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
	// Mask sensitive variables for display only. The snapshot stores real
	// values on purpose — replay re-injects them — so masking happens here at
	// presentation time, using the sensitive: declarations from the workflow
	// YAML embedded in the snapshot. Step outputs need no extra masking: the
	// engine already replaced sensitive values with ****** at finalize time.
	displayVars := snap.Variables
	if patterns := sensitivePatternsFromYAML(snap.Workflow); len(patterns) > 0 {
		displayVars = workflow.MaskVariables(snap.Variables, patterns)
	}
	if len(displayVars) > 0 {
		fmt.Println("Variables:")
		for _, k := range sortedKeys(displayVars) {
			fmt.Printf("  %s = %s\n", k, displayVars[k])
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
	return nil
}

// sensitivePatternsFromYAML extracts the workflow's sensitive variable
// patterns from the YAML stored in a snapshot. Fail-open: it returns nil when
// the YAML is missing or unparseable, in which case variables print as-is —
// matching the engine's behavior for workflows without a sensitive:
// declaration. (Same approach as api/mask.go's sensitivePatternsFromYAML.)
func sensitivePatternsFromYAML(raw string) []string {
	if raw == "" {
		return nil
	}
	wf, err := workflow.Parse([]byte(raw))
	if err != nil {
		return nil
	}
	return wf.Sensitive
}

// ── history purge ────────────────────────────────────────────────────────────

var historyPurgeOpts struct {
	keep int
}

var historyPurgeCmd = &cobra.Command{
	Use:   "purge",
	Short: "Delete old executions",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return historyPurge(historyDir(), historyPurgeOpts.keep)
	},
}

func init() {
	historyPurgeCmd.Flags().IntVarP(&historyPurgeOpts.keep, "keep", "k", 50, "number of recent executions to keep")
}

func historyPurge(dir string, keep int) error {
	store := workflow.NewFileStore(dir)
	deleted, err := store.Purge(keep)
	if err != nil {
		return err
	}
	fmt.Printf("Deleted %d execution(s), kept most recent %d.\n", deleted, keep)
	return nil
}

// ── history delete ───────────────────────────────────────────────────────────

var historyDeleteCmd = &cobra.Command{
	Use:     "delete <id>",
	Aliases: []string{"rm"},
	Short:   "Delete a single execution",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return historyDelete(args[0], historyDir())
	},
}

func historyDelete(id, dir string) error {
	store := workflow.NewFileStore(dir)
	if err := store.Delete(id); err != nil {
		return err
	}
	fmt.Printf("Deleted %s\n", id)
	return nil
}
