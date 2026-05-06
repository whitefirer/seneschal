package workflow

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

func (e *Executor) execShell(step Step) (string, error) {
	// Support both 'command' and 'shell' fields
	command := step.Command
	if command == "" {
		command = step.Shell
	}
	
	command, err := e.context.ResolveTemplate(command)
	if err != nil {
		return "", fmt.Errorf("resolve command template: %w", err)
	}

	// Resolve working directory
	dir := step.Dir
	if dir != "" {
		dir, err = e.context.ResolveTemplate(dir)
		if err != nil {
			return "", fmt.Errorf("resolve dir template: %w", err)
		}
	}

	// Determine shell
	shell, args := e.getShell(step.Shell)
	args = append(args, command)

	// Print command with pretty output
	if e.richPrinter != nil {
		e.richPrinter.PrintShell(step.Name, command, 0)
	} else if e.printer != nil {
		e.printer.PrintShellCommand(command)
	}

	cmd := exec.CommandContext(context.Background(), shell, args...)
	if dir != "" {
		cmd.Dir = dir
	}

	// Merge environment
	env, err := e.context.MergeEnv(step.Env)
	if err != nil {
		return "", err
	}

	// Inherit system environment, then overlay workflow variables
	// This ensures HOME, PATH, etc. are available while still allowing overrides
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err = cmd.Run()
	duration := time.Since(start)

	output := stdout.String()
	if stderr.Len() > 0 {
		if output != "" {
			output += "\n"
		}
		output += stderr.String()
	}

	if err != nil {
		return output, fmt.Errorf("command failed (exit %v, %s): %s", err, duration.Truncate(time.Millisecond), stderr.String())
	}

	e.context.SetResult(step.Name, output)

	// Save output to variable if specified (HTTP action compatibility)
	if step.SaveOutput != "" {
		e.context.Set(step.SaveOutput, strings.TrimSpace(output))
	}

	// Shell action: save entire output to single variable
	if step.OutputVar != "" {
		e.context.Set(step.OutputVar, strings.TrimSpace(output))
	}

	// Shell action: parse KEY=VALUE lines and save each as variable
	if len(step.OutputVars) > 0 {
		lines := strings.Split(output, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.Contains(line, "=") {
				parts := strings.SplitN(line, "=", 2)
				if len(parts) == 2 {
					key := strings.TrimSpace(parts[0])
					value := strings.TrimSpace(parts[1])
					// Only save if key is in the output_vars list
					for _, expectedKey := range step.OutputVars {
						if key == expectedKey {
							e.context.Set(key, value)
							break
						}
					}
				}
			}
		}
	}

	return output, nil
}

func (e *Executor) getShell(shell string) (string, []string) {
	if shell != "" {
		switch shell {
		case "bash":
			if runtime.GOOS == "windows" {
				// Try Git Bash
				gitBash := "C:\\Program Files\\Git\\bin\\bash.exe"
				if _, err := os.Stat(gitBash); err == nil {
					return gitBash, []string{"-c"}
				}
			}
			return "bash", []string{"-c"}
		case "sh":
			return "sh", []string{"-c"}
		case "powershell", "pwsh":
			return "powershell", []string{"-NoProfile", "-Command"}
		case "cmd":
			return "cmd", []string{"/C"}
		}
	}

	// Default shell by OS
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/C"}
	}
	return "sh", []string{"-c"}
}

