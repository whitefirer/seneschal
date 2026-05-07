package workflow

import (
	"fmt"
	"strings"
	"sync"
)

func (e *Executor) execParallel(step Step, depth int, result *WorkflowResult) (string, []StepResult, error) {
	// Print parallel with pretty output
	if e.printer != nil {
		e.printer.PrintParallel(len(step.Steps))
	}

	// Generate ID if not present
	stepID := step.ID
	if stepID == "" {
		stepID = fmt.Sprintf("step-%s", strings.ToLower(strings.ReplaceAll(step.Name, " ", "-")))
	}

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		outputs  []string
		hasError bool
		firstErr error
		children []StepResult
		successCount int
		failedCount  int
	)

	for i, s := range step.Steps {
		// Generate ID for child step if not present
		childID := s.ID
		if childID == "" {
			if s.Name != "" {
				childID = fmt.Sprintf("step-%s", strings.ToLower(strings.ReplaceAll(s.Name, " ", "-")))
			} else {
				childID = fmt.Sprintf("%s-child-%d", stepID, i)
			}
		}
		
		wg.Add(1)
		go func(s Step, childID string) {
			defer wg.Done()
			
			// Note: step_start, step_output, step_complete are all sent by executeStep()
			// Don't send again to avoid duplicate output
			
			childResult := e.executeStep(s, depth+1, result)
			
			mu.Lock()
			defer mu.Unlock()
			if childResult.Output != "" {
				outputs = append(outputs, fmt.Sprintf("[%s] %s", s.Name, childResult.Output))
			}
			if childResult.Status == "success" {
				successCount++
			} else if childResult.Status == "failed" {
				failedCount++
				hasError = true
				if firstErr == nil {
					firstErr = fmt.Errorf("parallel step '%s' failed: %s", s.Name, childResult.Error)
				}
			}
			children = append(children, childResult)
		}(s, childID)
	}

	wg.Wait()

	// Add summary output for parallel
	summaryOutput := fmt.Sprintf("并行完成: %d个任务, %d成功, %d失败", len(step.Steps), successCount, failedCount)
	
	if hasError && firstErr != nil {
		if len(outputs) > 0 {
			return summaryOutput + "\n" + strings.Join(outputs, "\n"), children, firstErr
		}
		return summaryOutput, children, firstErr
	}
	if len(outputs) > 0 {
		return summaryOutput + "\n" + strings.Join(outputs, "\n"), children, nil
	}
	return summaryOutput, children, nil
}

