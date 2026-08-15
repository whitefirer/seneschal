package workflow

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
)

func TestRenderHTMLReport(t *testing.T) {
	res := &WorkflowResult{
		Name:   "demo <workflow>",
		Status: "success",
		Steps: []StepResult{
			{Name: "a <b>", Action: "log", Status: "success", Output: "hello"},
			{Name: "child", Action: "shell", Status: "failed", Error: "boom", Children: []StepResult{
				{Name: "nested", Action: "log", Status: "success", Output: "ok"},
			}},
		},
	}
	html := renderHTMLReport(res)
	if !strings.Contains(html, "demo &lt;workflow&gt;") {
		t.Error("workflow name should be HTML-escaped")
	}
	if !strings.Contains(html, "a &lt;b&gt;") {
		t.Error("step name should be HTML-escaped")
	}
	if !strings.Contains(html, "nested") {
		t.Error("nested children should be rendered")
	}
}

func TestHTMLReport_StatusColors(t *testing.T) {
	if html := renderHTMLReport(&WorkflowResult{Name: "x", Status: "failed"}); !strings.Contains(html, "#dc2626") {
		t.Error("failed report should use the red status color")
	}
	if html := renderHTMLReport(&WorkflowResult{Name: "x", Status: "success"}); !strings.Contains(html, "#16a34a") {
		t.Error("success report should use the green status color")
	}
}

func TestExportJSON_WritesMaskedResult(t *testing.T) {
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()

	res := &WorkflowResult{
		Name:              "export",
		Status:            "success",
		SensitivePatterns: []string{"api_key"},
		Variables:         map[string]string{"api_key": "sk-secret", "env": "prod"},
		Steps:             []StepResult{{Name: "a", Action: "log", Status: "success", Output: "ok"}},
	}
	exportResult(OutputModeJSON, res)
	w.Close()
	os.Stdout = old

	data, _ := io.ReadAll(r)
	var got WorkflowResult
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("exportJSON output is not valid JSON: %v\n%s", err, string(data))
	}
	if got.Variables["api_key"] != "***" {
		t.Errorf("sensitive variable not masked in JSON export: %+v", got.Variables)
	}
	if got.Variables["env"] != "prod" {
		t.Errorf("non-sensitive variable should remain visible: %+v", got.Variables)
	}
}

func TestTruncateStr(t *testing.T) {
	if got := truncateStr("hello", 3); got != "hel...(截断)" {
		t.Errorf("truncateStr(hello,3)=%q", got)
	}
	if got := truncateStr("hi", 10); got != "hi" {
		t.Errorf("truncateStr(hi,10)=%q", got)
	}
}
