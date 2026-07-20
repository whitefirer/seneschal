package workflow

import (
	"fmt"
	"os"
	"strings"
	"time"
)

func (e *Executor) execSet(step Step) (string, error) {
	value, err := e.context.ResolveTemplate(step.Value)
	if err != nil {
		return "", fmt.Errorf("resolve value template: %w", err)
	}

	e.context.Set(step.Name, value)

	// Generate ID if not present
	stepID := step.ID
	if stepID == "" {
		stepID = fmt.Sprintf("step-%s", strings.ToLower(strings.ReplaceAll(step.Name, " ", "-")))
	}

	output := fmt.Sprintf("Set %s = %s", step.Name, value)

	// Note: step_output is sent by executeStep() to avoid duplication

	if e.verbose {
		fmt.Printf("    = %s → %s\n", step.Name, value)
	}
	return output, nil
}

func (e *Executor) execSleep(step Step) (string, error) {
	duration, err := ParseDuration(step.Duration)
	if err != nil {
		return "", fmt.Errorf("parse duration: %w", err)
	}

	// Generate ID if not present
	stepID := step.ID
	if stepID == "" {
		stepID = fmt.Sprintf("step-%s", strings.ToLower(strings.ReplaceAll(step.Name, " ", "-")))
	}

	// Print sleep with pretty output
	e.printer.PrintSleep(duration.String())

	// Send sleep start event (progress indicator)
	e.sendEvent("step_output", step.Name, stepID, "sleep", "running", fmt.Sprintf("Sleeping for %s...", duration.String()), "", 0, "", nil)

	time.Sleep(duration)

	// Note: final output is sent by executeStep() to avoid duplication
	output := fmt.Sprintf("Slept for %s", duration.String())

	return output, nil
}

func (e *Executor) execLog(step Step) string {
	level := step.Level
	if level == "" {
		level = "info"
	}

	msg, err := e.context.ResolveTemplate(step.Message)
	if err != nil {
		msg = step.Message
	}

	// Generate ID if not present
	stepID := step.ID
	if stepID == "" {
		stepID = fmt.Sprintf("step-%s", strings.ToLower(strings.ReplaceAll(step.Name, " ", "-")))
	}

	// Print log with pretty output
	e.printer.PrintLog(step.Name, level, msg, 0)

	// Format output (note: step_output is sent by executeStep() to avoid duplication)
	output := fmt.Sprintf("[%s] %s", strings.ToUpper(level), msg)

	return output
}

func (e *Executor) execTemplate(step Step) (string, error) {
	result, err := e.context.ResolveTemplateFromFile(step.Source)
	if err != nil {
		return "", fmt.Errorf("render template: %w", err)
	}

	// Resolve output path
	outputPath, err := e.context.ResolveTemplate(step.Output)
	if err != nil {
		return "", fmt.Errorf("resolve output path: %w", err)
	}

	if e.verbose {
		fmt.Printf("    %s → %s\n", step.Source, outputPath)
	}

	if err := os.WriteFile(outputPath, []byte(result), 0644); err != nil {
		return "", fmt.Errorf("write output: %w", err)
	}

	// Generate ID if not present
	stepID := step.ID
	if stepID == "" {
		stepID = fmt.Sprintf("step-%s", strings.ToLower(strings.ReplaceAll(step.Name, " ", "-")))
	}

	output := fmt.Sprintf("Rendered %s → %s (%d bytes)", step.Source, outputPath, len(result))

	// Note: step_output is sent by executeStep() to avoid duplication

	e.context.SetResult(step.Name, fmt.Sprintf("wrote %d bytes to %s", len(result), outputPath))
	return output, nil
}
