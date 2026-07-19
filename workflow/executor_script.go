package workflow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// langRuntimes maps a user-facing lang name to the actual interpreter command.
// Adding a new language is just one line here.
var langRuntimes = map[string]string{
	"python":  "python3",
	"python3": "python3",
	"node":    "node",
	"nodejs":  "node",
	"ruby":    "ruby",
	"lua":     "lua",
	"perl":    "perl",
}

// scriptTimeout is the default timeout for script execution.
const scriptTimeout = "60s"

// execScript runs an inline script in the specified language. Variables are
// passed as JSON on stdin; stdout is captured as the output. This is like
// shell but with structured I/O and language-specific syntax.
//
// The script receives all workflow variables as a JSON object on stdin:
//
//	{"key": "value", ...}
//
// The script's stdout (first line, or full output) is stored via save_output.
func (e *Executor) execScript(step Step) (string, error) {
	cmd, ok := langRuntimes[strings.ToLower(step.Lang)]
	if !ok {
		return "", fmt.Errorf("script step '%s': unsupported language %q (supported: python, node, ruby, lua, perl)", step.Name, step.Lang)
	}

	// Check the interpreter exists.
	if _, err := exec.LookPath(cmd); err != nil {
		return "", fmt.Errorf("script step '%s': %s interpreter not found in PATH", step.Name, cmd)
	}

	// Build the JSON payload from current variables.
	vars := e.context.Snapshot()
	payload, err := json.Marshal(vars)
	if err != nil {
		return "", fmt.Errorf("script step '%s': marshal variables: %w", step.Name, err)
	}

	// Resolve templates in the code (so {{.var}} works in code too).
	code, err := e.context.ResolveTemplate(step.Code)
	if err != nil {
		return "", fmt.Errorf("script step '%s': resolve code template: %w", step.Name, err)
	}

	// For Python and Node, we can pass code via stdin with "-" or -c.
	// Python:  python3 -c "code"  OR  python3 - <<'EOF'
	// Node:    node -e "code"     OR  node --input-type=module
	// Simplest cross-language: write to a temp file is fragile; instead use -c/-e.
	var args []string
	switch cmd {
	case "python3":
		args = []string{"-c", code}
	case "node":
		args = []string{"-e", code}
	case "ruby":
		args = []string{"-e", code}
	case "lua":
		// Lua's -e doesn't support multi-line well; use stdin.
		args = []string{"-"}
	case "perl":
		args = []string{"-e", code}
	default:
		args = []string{"-c", code}
	}

	c := exec.Command(cmd, args...)
	c.Stdin = bytes.NewReader(payload)

	// Merge step env with context variables.
	env, err := e.context.MergeEnv(step.Env)
	if err != nil {
		return "", fmt.Errorf("script step '%s': merge env: %w", step.Name, err)
	}
	c.Env = toEnvSlice(env)

	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr

	if err := c.Run(); err != nil {
		return "", fmt.Errorf("script failed (%v): %s", err, strings.TrimSpace(stderr.String()))
	}

	output := strings.TrimSpace(stdout.String())

	// Persist output if save_output is set.
	if step.SaveOutput != "" {
		e.context.Set(step.SaveOutput, output)
	}
	e.context.SetResult(step.Name, output)

	return output, nil
}

// toEnvSlice converts a map to "key=value" slices for exec.Env.
func toEnvSlice(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	return out
}
