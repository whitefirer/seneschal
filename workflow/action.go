package workflow

import (
	"fmt"
	"sync"
)

// ActionHandler runs a single step. The handler writes the action's outcome
// to result (Output, Children, ConditionResult, token counts, and any
// action-specific display metadata like ShellCommand or HTTPUrl), and returns
// a non-nil error to mark the step failed.
//
// The Executor owns the shared step lifecycle around the handler — retry,
// on_error: ai, status, duration, events, and hooks — so a handler only needs
// to express "what this action does". Handlers run concurrently (DAG waves,
// parallel, foreach), so any shared state must be synchronized (the context
// and token accounting are already safe).
//
// To reach workflow variables from a handler, use e.GetContext().Set/Get.
// To emit real-time progress, call e.OnProgress(ProgressEvent{...}).
type ActionHandler func(e *Executor, step Step, result *StepResult, stepID string, depth int, parentID string) error

// ActionSpec describes a workflow action and how it executes.
type ActionSpec struct {
	// Name is the action string used in YAML (e.g. "shell", "agent").
	Name string
	// IsContainer marks an action whose children are scheduled as a sub-DAG
	// (condition/parallel/foreach) rather than dispatched through Run.
	// Container actions route to executeContainerDAG and never call Run.
	IsContainer bool
	// SideEffecting marks an action as having external side effects
	// (shell/http/template write the host/network). Drives the StepResult
	// SideEffecting flag.
	SideEffecting bool
	// Nondeterministic marks an action as probabilistic (ai/ai_decide).
	// Drives the StepResult Nondeterministic flag.
	Nondeterministic bool
	// Run executes one step. Ignored for container actions.
	Run ActionHandler
	// Validate optionally checks the step's fields. It receives the step and
	// its index; return nil (or empty) when valid. ValidateStep has already
	// applied the "name is required" check.
	Validate func(step Step, index int) []error
	// CommandForError optionally extracts the command/prompt string shown to
	// the AI error analyzer (on_error: ai). Defaults to "".
	CommandForError func(step Step) string
}

// actionRegistry holds all registered actions. Built-in actions are
// registered at package init; external packages register custom actions via
// RegisterAction in their own init, which runs after this package's init.
var actionRegistry = struct {
	sync.RWMutex
	specs map[string]ActionSpec
}{specs: make(map[string]ActionSpec)}

// RegisterAction registers a custom action. It returns an error if the name
// is empty, already registered, or (for non-container actions) lacks a Run
// handler. Call it from package init before any workflow is executed.
func RegisterAction(spec ActionSpec) error {
	if spec.Name == "" {
		return fmt.Errorf("action spec requires a name")
	}
	if !spec.IsContainer && spec.Run == nil {
		return fmt.Errorf("action %q requires a Run handler (or IsContainer)", spec.Name)
	}
	actionRegistry.Lock()
	defer actionRegistry.Unlock()
	if _, exists := actionRegistry.specs[spec.Name]; exists {
		return fmt.Errorf("action %q already registered", spec.Name)
	}
	actionRegistry.specs[spec.Name] = spec
	return nil
}

// MustRegisterAction is RegisterAction but panics on error (for init use).
func MustRegisterAction(spec ActionSpec) {
	if err := RegisterAction(spec); err != nil {
		panic(err)
	}
}

// LookupAction returns the registered ActionSpec for name, and whether it
// exists.
func LookupAction(name string) (ActionSpec, bool) {
	actionRegistry.RLock()
	defer actionRegistry.RUnlock()
	spec, ok := actionRegistry.specs[name]
	return spec, ok
}

// isContainerAction reports whether the action schedules child steps as a
// sub-DAG (condition/parallel/foreach/loop) rather than dispatching through a
// Run handler. It consults the registry so external packages can register
// custom container actions.
func isContainerAction(action string) bool {
	spec, ok := LookupAction(action)
	return ok && spec.IsContainer
}

// registerBuiltinActions registers the built-in action set. It runs at
// package init, before any external package's init.
func registerBuiltinActions() {
	builtins := []ActionSpec{
		// ── leaf actions ────────────────────────────────────────────────
		{
			Name:          "shell",
			SideEffecting: true,
			Run: func(e *Executor, step Step, r *StepResult, stepID string, depth int, parentID string) error {
				out, err := e.execShell(step)
				r.Output = out
				if step.Command != "" {
					r.ShellCommand = step.Command
				} else {
					r.ShellCommand = step.Shell
				}
				return err
			},
			Validate: func(step Step, index int) []error {
				if step.Command == "" && step.Shell == "" {
					return []error{fmt.Errorf("step[%d] (%s): shell action requires 'command' or 'shell'", index, step.Name)}
				}
				return nil
			},
			CommandForError: func(step Step) string {
				if step.Command != "" {
					return step.Command
				}
				return step.Shell
			},
		},
		{
			Name:          "http",
			SideEffecting: true,
			Run: func(e *Executor, step Step, r *StepResult, stepID string, depth int, parentID string) error {
				out, err := e.execHTTP(step)
				r.Output = out
				r.HTTPUrl = step.URL
				r.HTTPMethod = step.Method
				return err
			},
			Validate: func(step Step, index int) []error {
				if step.URL == "" {
					return []error{fmt.Errorf("step[%d] (%s): http action requires 'url'", index, step.Name)}
				}
				return nil
			},
			CommandForError: func(step Step) string { return step.URL },
		},
		{
			Name: "set",
			Run: func(e *Executor, step Step, r *StepResult, stepID string, depth int, parentID string) error {
				out, err := e.execSet(step)
				r.Output = out
				return err
			},
			// value can reference other vars, so empty is ok for pure deletion.
		},
		{
			Name: "sleep",
			Run: func(e *Executor, step Step, r *StepResult, stepID string, depth int, parentID string) error {
				out, err := e.execSleep(step)
				r.Output = out
				r.SleepDuration = step.Duration
				return err
			},
			Validate: func(step Step, index int) []error {
				if step.Duration == "" {
					return []error{fmt.Errorf("step[%d] (%s): sleep action requires 'duration'", index, step.Name)}
				}
				return nil
			},
		},
		{
			Name: "log",
			Run: func(e *Executor, step Step, r *StepResult, stepID string, depth int, parentID string) error {
				r.Output = e.execLog(step)
				r.LogMessage = step.Message
				return nil
			},
			// message is optional, level defaults to info.
		},
		{
			Name:          "template",
			SideEffecting: true,
			Run: func(e *Executor, step Step, r *StepResult, stepID string, depth int, parentID string) error {
				out, err := e.execTemplate(step)
				r.Output = out
				return err
			},
			Validate: func(step Step, index int) []error {
				if step.Source == "" || step.Output == "" {
					return []error{fmt.Errorf("step[%d] (%s): template action requires 'source' and 'output'", index, step.Name)}
				}
				return nil
			},
		},
		{
			Name:             "ai",
			Nondeterministic: true,
			Run: func(e *Executor, step Step, r *StepResult, stepID string, depth int, parentID string) error {
				out, inTok, outTok, err := e.execAI(step, stepID, depth, parentID)
				r.Output = out
				// Token counts travel with the return value — parallel AI
				// steps each get their own counts, not a shared slot's.
				r.InputTokens = inTok
				r.OutputTokens = outTok
				return err
			},
			Validate: func(step Step, index int) []error {
				if step.Prompt == "" {
					return []error{fmt.Errorf("step[%d] (%s): ai action requires 'prompt'", index, step.Name)}
				}
				return nil
			},
			CommandForError: func(step Step) string { return step.Prompt },
		},
		{
			Name:             "ai_decide",
			Nondeterministic: true,
			Run: func(e *Executor, step Step, r *StepResult, stepID string, depth int, parentID string) error {
				decided, inTok, outTok, err := e.execAIDecide(step, stepID, depth, parentID)
				r.InputTokens = inTok
				r.OutputTokens = outTok
				if err != nil {
					return err
				}
				r.ConditionResult = &decided
				r.Output = fmt.Sprintf("decided: %v", decided)
				return nil
			},
			Validate: func(step Step, index int) []error {
				if step.Question == "" {
					return []error{fmt.Errorf("step[%d] (%s): ai_decide action requires 'question'", index, step.Name)}
				}
				return nil
			},
			CommandForError: func(step Step) string { return step.Question },
		},
		{
			Name:          "script",
			SideEffecting: true,
			Run: func(e *Executor, step Step, r *StepResult, stepID string, depth int, parentID string) error {
				out, err := e.execScript(step)
				r.Output = out
				return err
			},
			Validate: func(step Step, index int) []error {
				var errs []error
				if step.Lang == "" {
					errs = append(errs, fmt.Errorf("step[%d] (%s): script action requires 'lang' (e.g. python, node)", index, step.Name))
				}
				if step.Code == "" {
					errs = append(errs, fmt.Errorf("step[%d] (%s): script action requires 'code'", index, step.Name))
				}
				return errs
			},
			CommandForError: func(step Step) string { return step.Code },
		},
		{
			Name:          "workflow",
			SideEffecting: true,
			Run: func(e *Executor, step Step, r *StepResult, stepID string, depth int, parentID string) error {
				out, children, err := e.execWorkflow(step)
				r.Output = out
				r.Children = children
				return err
			},
			Validate: func(step Step, index int) []error {
				if step.Source == "" {
					return []error{fmt.Errorf("step[%d] (%s): workflow action requires 'source' (path to sub-workflow YAML)", index, step.Name)}
				}
				return nil
			},
		},

		// ── container actions ───────────────────────────────────────────
		{
			Name:        "condition",
			IsContainer: true,
			Validate: func(step Step, index int) []error {
				if step.Expression == "" {
					return []error{fmt.Errorf("step[%d] (%s): condition action requires 'expression'", index, step.Name)}
				}
				return nil
			},
		},
		{
			Name:        "parallel",
			IsContainer: true,
			Validate: func(step Step, index int) []error {
				var errs []error
				for j, sub := range step.Steps {
					for _, e := range ValidateStep(sub, j) {
						errs = append(errs, fmt.Errorf("step[%d] (%s) parallel[%d]: %v", index, step.Name, j, e))
					}
				}
				return errs
			},
		},
		{
			Name:        "foreach",
			IsContainer: true,
			Validate: func(step Step, index int) []error {
				if step.Do == nil || len(step.Do) == 0 {
					return []error{fmt.Errorf("step[%d] (%s): foreach action requires 'do' steps", index, step.Name)}
				}
				return nil
			},
		},
		{
			// "loop" is a legacy alias for "foreach" — the executor and parser
			// treat them identically. Previously the alias was accepted by the
			// scheduler but rejected by ValidateStep ("unknown action"), which
			// made it unreachable; registering it makes the alias consistent.
			Name:        "loop",
			IsContainer: true,
			Validate: func(step Step, index int) []error {
				if step.Do == nil || len(step.Do) == 0 {
					return []error{fmt.Errorf("step[%d] (%s): loop action requires 'do' steps", index, step.Name)}
				}
				return nil
			},
		},
	}

	for _, spec := range builtins {
		MustRegisterAction(spec)
	}
}

func init() {
	registerBuiltinActions()
}
