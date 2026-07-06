package workflow

// propagateDeterminism propagates the Nondeterministic flag along step
// dependencies (taint analysis) and reports whether the step tree as a whole
// is nondeterministic.
//
// Rules (see docs/PRODUCT.md "双确定性模型"):
//  1. A step already marked Nondeterministic (set by executeStep for ai /
//     ai_decide) stays so.
//  2. Taint flows along DependsOn: if step B depends_on A and A is
//     nondeterministic, then B is nondeterministic too (B consumes A's
//     output).
//  3. The propagation runs to a fixed point.
//  4. Children of a container (parallel/foreach/condition branches) are
//     propagated independently; a container is nondeterministic if any of its
//     children (or ThenChildren/ElseChildren) is.
//
// Steps are matched by their result ID. The step tree is treated in place.
func propagateDeterminism(steps []StepResult) bool {
	// Build an index of top-level steps by both ID and Name so that DependsOn
	// references (which may use either the raw name "summarize" or the
	// generated id "step-summarize") resolve. This tolerates the two ID
	// conventions present in the codebase.
	byKey := make(map[string]*StepResult, len(steps)*2)
	for i := range steps {
		s := &steps[i]
		propagateContainer(s)
		if s.ID != "" {
			byKey[s.ID] = s
		}
		if s.Name != "" {
			// Don't overwrite an existing ID-keyed entry with a name that
			// happens to collide; prefer the first (ID) registration.
			if _, exists := byKey[s.Name]; !exists {
				byKey[s.Name] = s
			}
		}
	}

	// Fixed-point taint along DependsOn.
	changed := true
	for changed {
		changed = false
		for i := range steps {
			s := &steps[i]
			if s.Nondeterministic {
				continue
			}
			for _, dep := range s.DependsOn {
				if depStep, ok := byKey[dep]; ok && depStep.Nondeterministic {
					s.Nondeterministic = true
					changed = true
					break
				}
			}
		}
	}

	// Workflow is nondeterministic if any top-level step is.
	any := false
	for i := range steps {
		if steps[i].Nondeterministic {
			any = true
			break
		}
	}
	return any
}

// propagateContainer recurses into a step's child slices (Children for
// parallel/foreach, ThenChildren/ElseChildren for condition), propagates
// determinism within each, and marks the container Nondeterministic if any
// child is.
func propagateContainer(s *StepResult) {
	if len(s.Children) > 0 {
		if propagateDeterminism(s.Children) {
			s.Nondeterministic = true
		}
	}
	if len(s.ThenChildren) > 0 {
		if propagateDeterminism(s.ThenChildren) {
			s.Nondeterministic = true
		}
	}
	if len(s.ElseChildren) > 0 {
		if propagateDeterminism(s.ElseChildren) {
			s.Nondeterministic = true
		}
	}
}
