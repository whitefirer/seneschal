package workflow

import "testing"

func TestPropagateDeterminism_DirectTaint(t *testing.T) {
	// build -> ai(summarize) -> report (depends_on summarize).
	// summarize is nondeterministic; report should become tainted.
	steps := []StepResult{
		{Name: "build", ID: "build", Status: "success"},
		{Name: "summarize", ID: "summarize", Status: "success", Nondeterministic: true, DependsOn: []string{"build"}},
		{Name: "report", ID: "report", Status: "success", DependsOn: []string{"summarize"}},
	}
	if got := propagateDeterminism(steps); !got {
		t.Fatal("expected workflow nondeterministic=true")
	}
	if !steps[1].Nondeterministic {
		t.Error("summarize should remain nondeterministic")
	}
	if !steps[2].Nondeterministic {
		t.Error("report depends_on summarize → should be tainted nondeterministic")
	}
	if steps[0].Nondeterministic {
		t.Error("build has no AI dependency → should stay deterministic")
	}
}

func TestPropagateDeterminism_TransitiveTaint(t *testing.T) {
	// ai -> mid -> tail : taint must reach tail through mid.
	steps := []StepResult{
		{Name: "ai", ID: "ai", Status: "success", Nondeterministic: true},
		{Name: "mid", ID: "mid", Status: "success", DependsOn: []string{"ai"}},
		{Name: "tail", ID: "tail", Status: "success", DependsOn: []string{"mid"}},
	}
	propagateDeterminism(steps)
	if !steps[1].Nondeterministic || !steps[2].Nondeterministic {
		t.Errorf("taint should reach mid and tail: mid=%v tail=%v", steps[1].Nondeterministic, steps[2].Nondeterministic)
	}
}

func TestPropagateDeterminism_NoAI(t *testing.T) {
	steps := []StepResult{
		{Name: "build", ID: "build", Status: "success"},
		{Name: "test", ID: "test", Status: "success", DependsOn: []string{"build"}},
	}
	if got := propagateDeterminism(steps); got {
		t.Fatal("pure-shell workflow should be deterministic")
	}
}

func TestPropagateDeterminism_IndependentBranch(t *testing.T) {
	// ai -> A ; B independent of ai. B stays deterministic.
	steps := []StepResult{
		{Name: "ai", ID: "ai", Status: "success", Nondeterministic: true},
		{Name: "A", ID: "A", Status: "success", DependsOn: []string{"ai"}},
		{Name: "B", ID: "B", Status: "success"},
	}
	propagateDeterminism(steps)
	if !steps[1].Nondeterministic {
		t.Error("A depends_on ai → tainted")
	}
	if steps[2].Nondeterministic {
		t.Error("B is independent → should stay deterministic")
	}
}

func TestPropagateDeterminism_ContainerChild(t *testing.T) {
	// A parallel whose child is an ai step: container becomes nondeterministic.
	steps := []StepResult{
		{Name: "par", ID: "par", Status: "success", Children: []StepResult{
			{Name: "ai", ID: "ai", Status: "success", Nondeterministic: true},
		}},
	}
	if got := propagateDeterminism(steps); !got {
		t.Fatal("container with ai child → workflow nondeterministic")
	}
	if !steps[0].Nondeterministic {
		t.Error("container should inherit nondeterminism from ai child")
	}
}

func TestPropagateDeterminism_IDNameMismatch(t *testing.T) {
	// Regression guard: the codebase has two ID conventions. A step's result
	// ID may be "step-summarize" (generated) while DependsOn refers to it by
	// the raw name "summarize". Propagation must resolve both.
	steps := []StepResult{
		{Name: "summarize", ID: "step-summarize", Status: "success", Nondeterministic: true},
		{Name: "report", ID: "step-report", Status: "success", DependsOn: []string{"summarize"}},
	}
	propagateDeterminism(steps)
	if !steps[1].Nondeterministic {
		t.Error("report depends_on 'summarize' (raw name) → must resolve to step-summarize and be tainted")
	}
}

func TestParseBoolAnswer(t *testing.T) {
	cases := map[string]bool{
		"true":                true,
		"True":                true,
		"TRUE":                true,
		"yes":                 true,
		"1":                   true,
		"是":                   true,
		"false":               false,
		"no":                  false,
		"0":                   false,
		"否":                   false,
		"The answer is true.": true, // first token after punctuation split
		"":                    false,
		"maybe":               false,
	}
	for in, want := range cases {
		if got := parseBoolAnswer(in); got != want {
			t.Errorf("parseBoolAnswer(%q) = %v, want %v", in, got, want)
		}
	}
}
