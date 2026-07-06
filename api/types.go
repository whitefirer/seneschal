package api

import (
	"time"

	"goworkflow/workflow"
)

// API Response types

type APIResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

type WorkflowInfo struct {
	Name        string    `json:"name"`
	FileName    string    `json:"fileName"`
	Version     string    `json:"version,omitempty"`
	Description string    `json:"description,omitempty"`
	Steps       int       `json:"steps"`
	Variables   int       `json:"variables"`
	ModifiedAt  time.Time `json:"modifiedAt"`
	Size        int64     `json:"size"`
}

type WorkflowContent struct {
	Name     string `json:"name"`
	FileName string `json:"fileName"`
	Content  string `json:"content"`
}

type RunRequest struct {
	Variables map[string]string `json:"variables,omitempty"`
	DryRun    bool              `json:"dryRun,omitempty"`
}

// ChatRequest is the body for POST /api/chat.
type ChatRequest struct {
	Message string `json:"message"`           // user's natural-language intent
	Dir     string `json:"dir,omitempty"`      // workflow directory; defaults to the server's workflows dir
}

type ExecutionRecord struct {
	ID           string    `json:"id"`
	WorkflowName string    `json:"workflowName"`
	WorkflowFile string    `json:"workflowFile"`
	Status       string    `json:"status"`
	StartTime    string    `json:"startTime"`
	EndTime      string    `json:"endTime"`
	Duration     string    `json:"duration"`
	Error        string    `json:"error,omitempty"`
	StepsCount   int       `json:"stepsCount"`
}

type ExecutionDetail struct {
	ExecutionRecord
	Logs     []LogEntry   `json:"logs"`
	Steps    []workflow.StepResult `json:"steps"`
	Workflow string                `json:"workflow"`
}

type LogEntry struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Message   string `json:"message"`
	Step      string `json:"step,omitempty"`
	StepID    string `json:"step_id,omitempty"`
}

// WebSocket message types

type WSMessage struct {
	Type   string                 `json:"type"`
	Data   map[string]interface{} `json:"data,omitempty"`
	Error  string                 `json:"error,omitempty"`
}

type WSProgressEvent struct {
	Type         string `json:"type"`
	ExecutionID  string `json:"executionId"`
	WorkflowName string `json:"workflowName"`
	WorkflowFile string `json:"workflowFile,omitempty"`
	StepID       string `json:"stepId,omitempty"`
	StepName     string `json:"stepName,omitempty"`
	Action       string `json:"action,omitempty"`
	Status       string `json:"status,omitempty"`
	Output       string `json:"output,omitempty"`
	Error        string `json:"error,omitempty"`
	Duration     string `json:"duration,omitempty"`
	Timestamp    string `json:"timestamp"`
	// 格式化的日志消息，前端直接使用
	LogMessage   string `json:"logMessage,omitempty"`
	LogLevel     string `json:"logLevel,omitempty"`
	// Condition 特有字段
	ConditionResult *bool `json:"conditionResult,omitempty"` // 条件求值结果
}
