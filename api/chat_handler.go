package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/whitefirer/seneschal/workflow"
	"github.com/whitefirer/seneschal/workflow/ai"
)

// ChatHandler handles POST /api/chat — an AI agent that streams
// server-sent events. The agent autonomously picks tools (list/select/
// generate/modify/explain/validate/run) based on the user's intent via
// multi-turn tool use.
func (h *Handler) ChatHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, errorResp("Method not allowed"))
		return
	}

	var req ChatRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errorResp("invalid request body"))
			return
		}
	}
	if req.Message == "" {
		writeJSON(w, http.StatusBadRequest, errorResp("message is required"))
		return
	}

	// Build conversation history from the client-sent messages (if any).
	// The frontend sends prior user+assistant turns so the agent has context.
	var priorMessages []ai.AnthropicRawMessage
	if len(req.History) > 0 {
		for _, h := range req.History {
			role := "user"
			if h.Role == "assistant" {
				role = "assistant"
			}
			priorMessages = append(priorMessages, ai.AnthropicRawMessage{
				Role:    role,
				Content: []ai.AnthropicRawContent{{Type: "text", Text: h.Content}},
			})
		}
	}

	provider, err := ai.BuildProvider(h.aiConfig)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, errorResp("AI unavailable: "+err.Error()))
		return
	}
	assistant := ai.NewAssistant(provider)

	dir := req.Dir
	if dir == "" {
		dir = h.workflowsDir
	}

	// SSE setup.
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, errorResp("streaming not supported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	sendEvent := func(eventType string, data interface{}) {
		payload, _ := json.Marshal(data)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, payload)
		flusher.Flush()
	}

	// Build the tool executor with the registry + assistant.
	exec := &chatToolExecutor{
		assistant: assistant,
		registry:  workflow.NewDirRegistry(dir),
		store:     h.store,
	}

	ctx, cancel := context.WithTimeout(r.Context(), 180*time.Second)
	defer cancel()

	system := `你是 seneschal AI 助手。根据用户意图选择合适的工具。用中文回复。

## 核心原则
你是有上下文记忆的对话助手。记住之前的对话内容。

## 工具使用指南
- **用户想运行/执行/部署/跑** → 先调 select_workflow 找到工作流，找到后直接调 run_workflow 执行。不需要等用户确认——用户说了"运行"就是要运行。
- **如果工作流有变量且用户没指定值** → 用默认值运行。不要追问。
- **用户说"创建/生成/新建"** → 调用 generate_workflow
- **用户想看有哪些** → 调用 list_workflows
- **工具返回的结果是可信的** — 如果 list_workflows 返回了内容，说明系统里有工作流。

## 上下文记忆
- 记住之前的对话。如果用户上一轮说了"运行 basic"，这一轮说"你好"，理解为在回答变量问题。
- 不要在每轮重新介绍自己。保持对话连贯。`

	err = assistant.RunAgent(ctx, system, req.Message, ai.AgentTools(), exec, 8, priorMessages, func(ev ai.AgentEvent) {
		// Forward agent events as SSE. For tool_result, try to enrich
		// selection/generate results so the frontend can render cards.
		switch ev.Type {
		case "thinking":
			sendEvent("thinking", map[string]interface{}{"message": req.Message})
		case "tool_call":
			sendEvent("tool_call", map[string]string{"tool": ev.Tool, "input": ev.Input})
		case "tool_result":
			// Enrich: if select_workflow returned JSON, parse and add step preview.
			data := map[string]interface{}{"tool": ev.Tool, "output": ev.Output}
			if ev.Tool == "select_workflow" {
				if enriched := exec.enrichSelection(ev.Output); enriched != nil {
					data["selection"] = enriched
				}
			}
			if ev.Tool == "generate_workflow" {
				data["yaml"] = ev.Output
			}
			if ev.Tool == "run_workflow" {
				// Extract executionId from [EXEC_ID:xxx] marker for frontend.
				if idx := strings.Index(ev.Output, "[EXEC_ID:"); idx >= 0 {
					rest := ev.Output[idx+len("[EXEC_ID:"):]
					if endIdx := strings.Index(rest, "]"); endIdx > 0 {
						data["executionId"] = rest[:endIdx]
					}
				}
			}
			sendEvent("tool_result", data)
		case "text":
			sendEvent("text", map[string]string{"content": ev.Output})
		case "done":
			sendEvent("done", map[string]bool{"ok": true})
		case "error":
			sendEvent("error", map[string]string{"error": ev.Error})
		}
	})
	if err != nil && ctx.Err() == nil {
		sendEvent("error", map[string]string{"error": err.Error()})
	}
}

// chatToolExecutor implements ai.ToolExecutor for the chat handler.
type chatToolExecutor struct {
	assistant *ai.Assistant
	registry  *workflow.DirRegistry
	store     workflow.ExecutionStore
}

func (e *chatToolExecutor) ExecuteTool(name string, input json.RawMessage) (string, error) {
	var params map[string]interface{}
	json.Unmarshal(input, &params)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	switch name {
	case "list_workflows":
		entries, err := e.registry.List()
		if err != nil {
			return "", fmt.Errorf("list workflows: %w", err)
		}
		result := ""

		for _, entry := range entries {
			result += fmt.Sprintf("- %s (%s)", entry.Name, entry.FileName)
			if entry.Description != "" {
				result += " | " + entry.Description
			}
			result += "\n"
		}
		if result == "" {
			result = "(没有可用的工作流)"
		}
		return result, nil

	case "select_workflow":
		intent, _ := params["intent"].(string)
		entries, err := e.registry.List()
		if err != nil {
			return "", fmt.Errorf("list: %w", err)
		}
		if len(entries) == 0 {
			return "没有找到任何工作流。请用 generate_workflow 创建一个新工作流。", nil
		}
		candidates := entriesToCandidates(e.registry, entries)
		sel, err := e.assistant.SelectWorkflow(ctx, intent, candidates)
		if err != nil {
			return "", err
		}
		if sel.Workflow == "" {
			return fmt.Sprintf("没有找到匹配 '%s' 的工作流。可用的工作流有: %s", intent, candidateList(candidates)), nil
		}
		// Return human-readable text (not JSON) so the model can understand it.
		var sb strings.Builder
		fmt.Fprintf(&sb, "找到匹配的工作流: %s\n", sel.Workflow)
		fmt.Fprintf(&sb, "文件名: (从工作流列表中查找)\n")
		fmt.Fprintf(&sb, "置信度: %.0f%%\n", sel.Confidence*100)
		if len(sel.Variables) > 0 {
			sb.WriteString("建议变量:\n")
			for k, v := range sel.Variables {
				fmt.Fprintf(&sb, "  %s = %s\n", k, v)
			}
		}
		// Also store as JSON for frontend enrichment.
		out, _ := json.Marshal(sel)
		// Append a hidden JSON marker the frontend can parse.
		fmt.Fprintf(&sb, "\n[JSON:%s]", string(out))
		return sb.String(), nil

	case "generate_workflow":
		requirement, _ := params["requirement"].(string)
		return e.assistant.Generate(ctx, requirement)

	case "modify_workflow":
		yamlContent, _ := params["yaml"].(string)
		instruction, _ := params["instruction"].(string)
		return e.assistant.Modify(ctx, yamlContent, instruction)

	case "explain_workflow":
		yamlContent, _ := params["yaml"].(string)
		return e.assistant.Explain(ctx, yamlContent)

	case "validate_workflow":
		yamlContent, _ := params["yaml"].(string)
		wf, err := workflow.Parse([]byte(yamlContent))
		if err != nil {
			return fmt.Sprintf("❌ 解析失败: %v", err), nil
		}
		errs := wf.Validate()
		if len(errs) == 0 {
			return fmt.Sprintf("✅ 工作流 '%s' 校验通过 (%d 步)", wf.Name, len(wf.Steps)), nil
		}
		msg := fmt.Sprintf("❌ %d 个错误:\n", len(errs))
		for _, e := range errs {
			msg += fmt.Sprintf("  - %v\n", e)
		}
		return msg, nil

	case "run_workflow":
		fileName, _ := params["fileName"].(string)
		vars := make(map[string]string)
		if v, ok := params["variables"].(map[string]interface{}); ok {
			for k, val := range v {
				vars[k] = fmt.Sprintf("%v", val)
			}
		}
		wf, _, err := e.registry.Get(fileName)
		if err != nil {
			return "", fmt.Errorf("workflow not found: %s", fileName)
		}
		executor := workflow.NewExecutor(vars)
		result := executor.Execute(wf)
		// Register execution so /api/executions/{id} can find it.
		execID := fmt.Sprintf("exec-%s-%s", time.Now().Format("20060102-150405"), randomHex(4))
		// Read raw YAML for snapshot.
		_, rawYAMLBytes, _ := e.registry.Get(wf.Name)
		rawYAML := ""
		if rawYAMLBytes != nil {
			rawYAML = string(rawYAMLBytes)
		}
		snap := workflow.ExecutionSnapshot{
			ExecutionSummary: workflow.ExecutionSummary{
				ID:             execID,
				WorkflowName:   wf.Name,
				WorkflowFile:   fileName,
				Status:         result.Status,
				StartTime:      result.StartTime,
				EndTime:        result.EndTime,
				StepsCount:     len(wf.Steps),
				Nondeterministic: result.Nondeterministic,
			},
			Steps:     result.Steps,
			Variables: result.Variables,
			Workflow:  rawYAML,
		}
		if e.store != nil {
			e.store.Save(snap)
		}
		summary := fmt.Sprintf("工作流 %s 执行完成: %s (%d 步)\n执行ID: %s\n[EXEC_ID:%s]",
			wf.Name, result.Status, len(result.Steps), execID, execID)
		return summary, nil

	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

func candidateList(cs []ai.CandidateEntry) string {
	var parts []string
	for _, c := range cs {
		parts = append(parts, c.Name)
	}
	return strings.Join(parts, ", ")
}

func indexOf(s, substr string) int {
	return strings.Index(s, substr)
}

func readFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// enrichSelection parses a select_workflow tool result and adds step preview
// data so the frontend can render a selection card. The output may contain
// a [JSON:{...}] marker appended by the tool executor.
func (e *chatToolExecutor) enrichSelection(raw string) map[string]interface{} {
	// Try to extract [JSON:...] marker first.
	jsonStr := raw
	if idx := strings.Index(raw, "[JSON:"); idx >= 0 {
		rest := raw[idx+len("[JSON:"):]
		if endIdx := strings.Index(rest, "]"); endIdx > 0 {
			jsonStr = rest[:endIdx]
		}
	}
	var sel ai.Selection
	if err := json.Unmarshal([]byte(jsonStr), &sel); err != nil {
		return nil
	}
	if sel.Workflow == "" {
		return nil
	}
	data := map[string]interface{}{
		"workflow":   sel.Workflow,
		"variables":  sel.Variables,
		"confidence": sel.Confidence,
	}
	entries, _ := e.registry.List()
	for _, entry := range entries {
		if entry.Name == sel.Workflow {
			data["fileName"] = entry.FileName
			if wf, _, err := e.registry.Get(sel.Workflow); err == nil {
				data["steps"] = buildStepSummary(wf.Steps)
			}
			break
		}
	}
	return data
}

// entriesToCandidates converts DirRegistry entries to assistant candidates.
func entriesToCandidates(registry *workflow.DirRegistry, entries []workflow.WorkflowEntry) []ai.CandidateEntry {
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
	return candidates
}

// buildStepSummary flattens the step tree for frontend DAG preview.
func buildStepSummary(steps []workflow.Step) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(steps))
	for _, s := range steps {
		entry := map[string]interface{}{"name": s.Name, "action": s.Action}
		if len(s.Next) > 0 {
			entry["next"] = s.Next
		}
		if len(s.DependsOn) > 0 {
			entry["depends_on"] = s.DependsOn
		}
		if s.Action == "condition" {
			if len(s.Then) > 0 {
				entry["then"] = buildStepSummary(s.Then)
			}
			if len(s.Else) > 0 {
				entry["else"] = buildStepSummary(s.Else)
			}
		}
		if (s.Action == "foreach" || s.Action == "loop") && len(s.Do) > 0 {
			entry["do"] = buildStepSummary(s.Do)
		}
		if s.Action == "parallel" && len(s.Steps) > 0 {
			entry["steps"] = buildStepSummary(s.Steps)
		}
		out = append(out, entry)
	}
	return out
}
