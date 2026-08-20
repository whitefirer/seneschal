package api

import (
"reflect"
"testing"

"github.com/whitefirer/seneschal/workflow"
)

func TestBuildStepsNestedTree(t *testing.T) {
steps := []workflow.Step{
{Name: "Build", Action: "shell"},
{
Name:     "Check",
Action:   "condition",
Expression: `{{.env}} == prod`,
Then: []workflow.Step{{Name: "Prod", Action: "log"}},
Else: []workflow.Step{{Name: "Dev", Action: "log"}},
},
{
Name:   "Deploy",
Action: "parallel",
Steps: []workflow.Step{
{Name: "A", Action: "shell"},
{Name: "B", Action: "http"},
},
},
}

got := buildSteps(steps, "")
if len(got) != 3 {
t.Fatalf("len=%d want 3", len(got))
}
if got[0].ID != "step-build" || got[0].Status != "pending" {
t.Errorf("build step id/status = %q/%q", got[0].ID, got[0].Status)
}
cond := got[1]
if len(cond.ThenChildren) != 1 || cond.ThenChildren[0].ID != "step-prod" {
t.Errorf("then children = %+v", cond.ThenChildren)
}
if len(cond.ElseChildren) != 1 || cond.ElseChildren[0].ID != "step-dev" {
t.Errorf("else children = %+v", cond.ElseChildren)
}
par := got[2]
if len(par.Children) != 2 || par.Children[0].ID != "step-a" || par.Children[1].ID != "step-b" {
t.Errorf("parallel children = %+v", par.Children)
}
}

func TestUpdateStepStatusRunsAndCompletes(t *testing.T) {
steps := []workflow.StepResult{
{ID: "step-a", Name: "a", Action: "shell", Status: "pending"},
{ID: "step-b", Name: "b", Action: "shell", Status: "pending"},
}

// Start a
if !updateStepStatus(steps, "step-a", workflow.ProgressEvent{Type: "step_start", Name: "a", Time: "t1"}) {
t.Fatal("expected step-a start match")
}
if steps[0].Status != "running" || steps[0].StartTime != "t1" {
t.Errorf("a = %+v", steps[0])
}

// Output
updateStepStatus(steps, "step-a", workflow.ProgressEvent{Type: "step_output", Name: "a", Output: "hello"})
if steps[0].Output != "hello" {
t.Errorf("output = %q", steps[0].Output)
}

// Complete
updateStepStatus(steps, "step-a", workflow.ProgressEvent{Type: "step_complete", Name: "a", Status: "success", Duration: "1s", Time: "t2"})
if steps[0].Status != "success" || steps[0].Duration != "1s" || steps[0].EndTime != "t2" {
t.Errorf("a = %+v", steps[0])
}
}

func TestUpdateStepStatusParentParallel(t *testing.T) {
steps := []workflow.StepResult{
{
ID: "step-par", Name: "par", Action: "parallel", Status: "pending",
Children: []workflow.StepResult{
{ID: "step-a", Name: "a", Status: "pending"},
{ID: "step-b", Name: "b", Status: "pending"},
},
},
}

updateStepStatus(steps, "step-a", workflow.ProgressEvent{Type: "step_start", Name: "a", Time: "t"})
if steps[0].Status != "running" {
t.Errorf("parent after child start = %q want running", steps[0].Status)
}
updateStepStatus(steps, "step-a", workflow.ProgressEvent{Type: "step_complete", Name: "a", Status: "success", Time: "t"})
updateStepStatus(steps, "step-b", workflow.ProgressEvent{Type: "step_complete", Name: "b", Status: "failed", Time: "t"})
if steps[0].Status != "failed" {
t.Errorf("parent after children complete = %q want failed", steps[0].Status)
}
}

func TestFindStepDefNested(t *testing.T) {
steps := []workflow.Step{
{Name: "outer", Action: "parallel", Steps: []workflow.Step{
{Name: "inner", Action: "condition", Then: []workflow.Step{{Name: "deep", Action: "log"}}},
}},
}

deep := findStepDef(steps, "deep")
if deep == nil || deep.Name != "deep" {
t.Fatalf("find deep = %+v", deep)
}
if findStepDef(steps, "missing") != nil {
t.Fatal("expected nil for missing step")
}
}

func TestBuildResultMapIncludesNested(t *testing.T) {
steps := []workflow.StepResult{
{ID: "top", Name: "top", Children: []workflow.StepResult{{ID: "child", Name: "child"}}},
{ID: "cond", Name: "cond", ThenChildren: []workflow.StepResult{{ID: "then", Name: "then"}}},
}
m := make(map[string]workflow.StepResult)
buildResultMap(steps, m)
for _, key := range []string{"top", "child", "cond", "then"} {
if _, ok := m[key]; !ok {
t.Errorf("resultMap missing %q", key)
}
}
}

func TestUpdateTreeMergesResults(t *testing.T) {
wfSteps := []workflow.Step{
{Name: "a", Action: "shell"},
{Name: "b", Action: "shell"},
}
existing := buildSteps(wfSteps, "")
resultMap := map[string]workflow.StepResult{
"step-a": {ID: "step-a", Name: "a", Status: "success", Output: "out-a"},
"step-b": {ID: "step-b", Name: "b", Status: "failed", Error: "boom"},
}
updateTree(existing, wfSteps, resultMap)

if existing[0].Status != "success" || existing[0].Output != "out-a" {
t.Errorf("a = %+v", existing[0])
}
if existing[1].Status != "failed" || existing[1].Error != "boom" {
t.Errorf("b = %+v", existing[1])
}
}

func TestUpdateTreeConsumesResultMap(t *testing.T) {
wfSteps := []workflow.Step{{Name: "a", Action: "shell"}}
existing := buildSteps(wfSteps, "")
resultMap := map[string]workflow.StepResult{
"step-a": {ID: "step-a", Name: "a", Status: "success"},
}
updateTree(existing, wfSteps, resultMap)
if len(resultMap) != 0 {
t.Errorf("resultMap not consumed: %v", reflect.ValueOf(resultMap).MapKeys())
}
}
