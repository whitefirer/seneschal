package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/whitefirer/seneschal/workflow"
	"github.com/whitefirer/seneschal/workflow/ai"
)

var replayOpts struct {
	execDir       string
	execDirLegacy string
	full          bool
	outputMode    string
	onlySteps     []string
}

var replayCmd = &cobra.Command{
	Use:     "replay <exec-id>",
	Short:   "Smart-replay a past execution",
	GroupID: "history",
	Args:    cobra.ExactArgs(1),
	Example: `  seneschal replay exec-20260707-120000-xxxx
  seneschal replay exec-20260707-120000-xxxx --full
  seneschal replay exec-20260707-120000-xxxx --step build`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return replayExecution(args[0])
	},
}

func init() {
	f := replayCmd.Flags()
	f.StringVar(&replayOpts.execDir, "exec-dir", executionsDir(), "directory of execution records")
	f.StringVarP(&replayOpts.execDirLegacy, "dir", "d", "", "alias for --exec-dir")
	_ = f.MarkDeprecated("dir", "use --exec-dir instead")
	f.BoolVar(&replayOpts.full, "full", false, "re-execute every step (ignore the cache)")
	f.StringVarP(&replayOpts.outputMode, "output-mode", "m", "rich", "output mode for the re-executed workflow")
	f.StringArrayVar(&replayOpts.onlySteps, "step", nil, "restrict re-execution to this step (repeatable)")
}

// replayExecution loads a historical execution and re-runs it: deterministic
// steps reuse their recorded output, nondeterministic (AI) steps re-execute.
// (Former cmdReplay.)
func replayExecution(id string) error {
	store := workflow.NewFileStore(resolveDir(replayOpts.execDir, replayOpts.execDirLegacy))
	replayer := workflow.NewReplayer(store)

	// Build an AI provider from env so AI steps can re-run. If unavailable,
	// replay still works for purely deterministic workflows.
	executor := workflow.NewExecutor(nil)
	executor.SetVerbose(true)
	executor.SetOutputMode(workflow.ParseOutputMode(replayOpts.outputMode))
	executor.SetTheme("default")
	if p, err := ai.BuildProvider(ai.Config{}); err == nil {
		executor.SetAIProvider(p)
	}

	opts := workflow.ReplayOptions{Full: replayOpts.full, OnlySteps: replayOpts.onlySteps}
	result, hits, misses, err := replayer.Replay(id, executor, opts)
	if err != nil {
		return err
	}

	mode := "smart"
	if replayOpts.full {
		mode = "full"
	}
	fmt.Fprintf(os.Stderr, "\n🔁 Replay (%s): %d step(s) reused, %d step(s) re-executed\n", mode, hits, misses)
	if result != nil {
		fmt.Fprintf(os.Stderr, "   Result: %s\n", result.Status)
	}
	return nil
}
