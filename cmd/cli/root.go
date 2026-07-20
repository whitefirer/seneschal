package main

import (
	"errors"

	"github.com/spf13/cobra"
)

const banner = `
  ╔═══════════════════════════════════════════╗
  ║       seneschal - YAML Workflow Engine      ║
  ║  Edit YAML → Change Behavior Automatically ║
  ╚═══════════════════════════════════════════╝
`

// errExitQuiet is a sentinel error: the command already printed its precise
// error message (e.g. validate prints errors to stdout), so main exits 1
// without printing "Error: ..." again.
var errExitQuiet = errors.New("quiet exit")

var rootCmd = &cobra.Command{
	Use:   "seneschal",
	Short: "YAML Workflow Engine — edit YAML, change behavior automatically",
	Long: banner + `
seneschal executes YAML-defined workflows.

Actions (in workflow YAML):
  shell http condition set sleep log parallel foreach
  template script ai ai_decide workflow

Output Modes (--output-mode / -m):
  rich (default) plain compact dag timeline tui json html`,
	Example: `  seneschal run deploy.yaml --verbose --var ENV=prod
  seneschal run deploy.yaml -m html > report.html
  seneschal run deploy.yaml -m json | jq .status
  seneschal chat "部署到staging"
  seneschal generate "每晚跑测试并通知"
  seneschal explain deploy.yaml
  seneschal replay exec-20260707-120000-xxxx
  seneschal history list
  seneschal runbook trigger nightly-deploy --var env=prod`,
	// Errors are printed by main (uniform "Error: ..." format), usage text is
	// not dumped on runtime failures — matches the pre-cobra CLI behavior.
	SilenceErrors: true,
	SilenceUsage:  true,
	// Bare `seneschal` prints help and exits 1 (pre-cobra behavior).
	RunE: func(cmd *cobra.Command, args []string) error {
		_ = cmd.Help()
		return errExitQuiet
	},
}

func init() {
	rootCmd.AddGroup(
		&cobra.Group{ID: "workflow", Title: "Workflow Operations:"},
		&cobra.Group{ID: "ai", Title: "AI Assistant (requires AI provider config):"},
		&cobra.Group{ID: "history", Title: "Execution History & Replay:"},
		&cobra.Group{ID: "runbook", Title: "Runbook (Trigger & Schedule):"},
	)
	rootCmd.AddCommand(
		runCmd, createCmd, validateCmd, showCmd, editCmd, templateCmd, listCmd,
		explainCmd, fixCmd, generateCmd, chatCmd,
		replayCmd, historyCmd,
		runbookCmd,
	)
}
