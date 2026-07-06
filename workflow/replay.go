package workflow

import (
	"fmt"
)

// ReplayOptions controls smart-replay behavior.
type ReplayOptions struct {
	// Full forces every step to re-execute, ignoring the replay cache.
	// Equivalent to a normal run but from a historical snapshot.
	Full bool

	// OnlySteps, when non-empty, restricts which steps are allowed to re-run.
	// Steps not in this list that are NOT served from cache are skipped.
	// Steps in this list always re-run even if deterministic. Useful for
	// targeted re-execution. Empty means no restriction (default replay
	// semantics: deterministic cached, nondeterministic re-run).
	OnlySteps []string
}

// Replayer loads a historical execution snapshot and re-runs it with smart
// caching: deterministic steps reuse their recorded output, nondeterministic
// steps (AI and tainted downstream) re-execute against the live provider.
type Replayer struct {
	store ExecutionStore
}

// NewReplayer creates a Replayer backed by the given store.
func NewReplayer(store ExecutionStore) *Replayer {
	return &Replayer{store: store}
}

// Replay loads snapshot `id`, rebuilds the workflow from the stored YAML,
// populates the executor's replay cache from the historical step tree, and
// runs. Returns the new result plus (hits, misses) — how many steps were
// served from cache vs actually executed.
//
// The workflow is rebuilt from the snapshot's stored YAML, NOT from the
// current file — this guarantees the replay matches the original definition
// even if the file has since changed.
func (r *Replayer) Replay(id string, executor *Executor, opts ReplayOptions) (*WorkflowResult, int, int, error) {
	snap, err := r.store.Get(id)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("load execution %s: %w", id, err)
	}

	// Rebuild the workflow from the stored YAML so the replay reflects the
	// definition at execution time, not the current (possibly edited) file.
	if snap.Workflow == "" {
		return nil, 0, 0, fmt.Errorf("snapshot %s has no stored workflow YAML", id)
	}
	wf, err := Parse([]byte(snap.Workflow))
	if err != nil {
		return nil, 0, 0, fmt.Errorf("rebuild workflow from snapshot: %w", err)
	}

	// Build the replay cache from the historical step tree, unless Full was
	// requested (then everything re-runs; no cache).
	if !opts.Full {
		cache := buildReplayCache(snap.Steps, opts)
		executor.SetReplayCache(cache)
	} else {
		executor.SetReplayCache(nil)
	}

	// Restore the original variables so downstream steps see the same inputs
	// (deterministic steps will produce identical outputs).
	if len(snap.Variables) > 0 {
		executor.setReplayVariables(snap.Variables)
	}

	result := executor.Execute(wf)
	hits, misses := executor.ReplayStats()
	return result, hits, misses, nil
}

// buildReplayCache flattens the historical step tree (including container
// children, foreach iterations, condition branches) into a map keyed by step
// ID then Name. OnlySteps, if set, excludes listed steps from the cache so
// they are forced to re-run.
func buildReplayCache(steps []StepResult, opts ReplayOptions) map[string]*StepResult {
	cache := make(map[string]*StepResult)
	onlySet := make(map[string]bool, len(opts.OnlySteps))
	for _, s := range opts.OnlySteps {
		onlySet[s] = true
	}
	add := func(sr *StepResult) {
		// In OnlySteps restrict mode: skip steps explicitly listed for re-run
		// so they are absent from the cache and thus forced to execute.
		if len(opts.OnlySteps) > 0 && (onlySet[sr.Name] || onlySet[sr.ID]) {
			return
		}
		if sr.ID != "" {
			cache[sr.ID] = sr
		}
		if sr.Name != "" {
			if _, exists := cache[sr.Name]; !exists {
				cache[sr.Name] = sr
			}
		}
	}
	walkStepResults(steps, add)
	return cache
}

// walkStepResults recurses into a step tree (children, then/else branches)
// and invokes fn on every node.
func walkStepResults(steps []StepResult, fn func(*StepResult)) {
	for i := range steps {
		fn(&steps[i])
		if len(steps[i].Children) > 0 {
			walkStepResults(steps[i].Children, fn)
		}
		if len(steps[i].ThenChildren) > 0 {
			walkStepResults(steps[i].ThenChildren, fn)
		}
		if len(steps[i].ElseChildren) > 0 {
			walkStepResults(steps[i].ElseChildren, fn)
		}
	}
}

// setReplayVariables presets the executor's context with the historical
// variables, taking priority over the workflow's YAML defaults (same semantics
// as NewExecutor vars / chat fill). Done by writing directly to the context
// before Execute runs.
func (e *Executor) setReplayVariables(vars map[string]string) {
	for k, v := range vars {
		e.context.Set(k, v)
	}
}
