package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// binaryPath is built once via TestMain and reused by all tests.
var binaryPath string

func TestMain(m *testing.M) {
	// Compile the CLI binary to a temp location.
	tmp, err := os.MkdirTemp("", "seneschal-cli-test")
	if err != nil {
		panic(err)
	}
	binaryPath = filepath.Join(tmp, "seneschal-test")
	if err := exec.Command("go", "build", "-o", binaryPath, ".").Run(); err != nil {
		panic(err)
	}
	code := m.Run()
	os.RemoveAll(tmp)
	os.Exit(code)
}

// runCLI executes the compiled binary with given args, returns stdout, stderr, exit code.
func runCLI(t *testing.T, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	// Use testdata as the working directory for relative paths.
	return runCLIInDir(t, projectRoot(), args...)
}

// runCLIInDir is runCLI with an explicit working directory — used by tests
// that want ./executions (the default history dir) to land in a temp dir.
func runCLIInDir(t *testing.T, dir string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	cmd := exec.Command(binaryPath, args...)
	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	cmd.Dir = dir
	err := cmd.Run()
	exitCode = 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("failed to run CLI: %v", err)
		}
	}
	return outBuf.String(), errBuf.String(), exitCode
}

func projectRoot() string {
	// cmd/cli is two levels below project root.
	wd, _ := os.Getwd()
	return filepath.Join(wd, "..", "..")
}

func testdata(file string) string {
	return filepath.Join(projectRoot(), "testdata", file)
}

// ── Run tests ──────────────────────────────────────────────────────────────────

func TestCLI_RunBasic(t *testing.T) {
	stdout, _, code := runCLI(t, "run", testdata("basic.yaml"), "-m", "plain")
	if code != 0 {
		t.Fatalf("exit=%d", code)
	}
	if !strings.Contains(stdout, "Result: OK") {
		t.Errorf("stdout missing 'Result: OK': %s", stdout)
	}
}

func TestCLI_RunCondition(t *testing.T) {
	stdout, _, code := runCLI(t, "run", testdata("condition.yaml"), "-m", "plain")
	if code != 0 {
		t.Fatalf("exit=%d", code)
	}
	if !strings.Contains(stdout, "Result: OK") {
		t.Errorf("expected OK")
	}
}

func TestCLI_RunParallel(t *testing.T) {
	stdout, _, code := runCLI(t, "run", testdata("parallel.yaml"), "-m", "plain")
	if code != 0 {
		t.Fatalf("exit=%d", code)
	}
	if !strings.Contains(stdout, "Result: OK") {
		t.Errorf("expected OK")
	}
}

func TestCLI_RunForeach(t *testing.T) {
	stdout, _, code := runCLI(t, "run", testdata("foreach.yaml"), "-m", "plain")
	if code != 0 {
		t.Fatalf("exit=%d", code)
	}
	if !strings.Contains(stdout, "gamma") {
		t.Errorf("expected foreach to process gamma")
	}
}

func TestCLI_RunDAG(t *testing.T) {
	stdout, _, code := runCLI(t, "run", testdata("dag.yaml"), "-m", "plain")
	if code != 0 {
		t.Fatalf("exit=%d", code)
	}
	if !strings.Contains(stdout, "Result: OK") {
		t.Errorf("expected OK")
	}
}

func TestCLI_RunRetry(t *testing.T) {
	stdout, _, code := runCLI(t, "run", testdata("retry.yaml"), "-m", "plain")
	if code != 0 {
		t.Fatalf("exit=%d", code)
	}
	if !strings.Contains(stdout, "Result: OK") {
		t.Errorf("expected OK")
	}
}

func TestCLI_RunSubWorkflow(t *testing.T) {
	stdout, _, code := runCLI(t, "run", testdata("workflow-call.yaml"), "-m", "plain")
	if code != 0 {
		t.Fatalf("exit=%d", code)
	}
	if !strings.Contains(stdout, "Result: OK") {
		t.Errorf("expected OK")
	}
}

// ── Flag tests ─────────────────────────────────────────────────────────────────

func TestCLI_VarOverride(t *testing.T) {
	stdout, _, code := runCLI(t, "run", testdata("variables.yaml"), "-m", "plain", "--var", "name=overridden")
	if code != 0 {
		t.Fatalf("exit=%d", code)
	}
	if !strings.Contains(stdout, "name=overridden") {
		t.Errorf("expected name=overridden in output: %s", stdout)
	}
}

func TestCLI_DryRun(t *testing.T) {
	stdout, _, code := runCLI(t, "run", testdata("basic.yaml"), "-m", "plain", "--dry-run")
	if code != 0 {
		t.Fatalf("exit=%d", code)
	}
	// Dry run shows steps as skipped (○), no actual execution.
	if !strings.Contains(stdout, "○") {
		t.Errorf("expected skipped steps in dry run output: %s", stdout)
	}
}

// ── Validate ───────────────────────────────────────────────────────────────────

func TestCLI_Validate(t *testing.T) {
	_, _, code := runCLI(t, "validate", testdata("basic.yaml"))
	if code != 0 {
		t.Errorf("valid workflow should pass validation, exit=%d", code)
	}
}

func TestCLI_ValidateInvalid(t *testing.T) {
	// Write a broken workflow.
	tmp := t.TempDir()
	bad := filepath.Join(tmp, "bad.yaml")
	os.WriteFile(bad, []byte("name: bad\nsteps:\n  - action: shell\n"), 0644)
	stdout, _, code := runCLI(t, "validate", bad)
	if code == 0 {
		t.Error("invalid workflow should fail validation")
	}
	// Validate prints errors to stdout (via fmt.Printf), not stderr.
	combined := stdout
	if !strings.Contains(combined, "error") && !strings.Contains(combined, "Error") {
		t.Errorf("expected error message in output: %s", combined)
	}
}

// ── Output modes ───────────────────────────────────────────────────────────────

func TestCLI_OutputJSON(t *testing.T) {
	stdout, _, code := runCLI(t, "run", testdata("output-json.yaml"), "-m", "json")
	if code != 0 {
		t.Fatalf("exit=%d", code)
	}
	// stdout should be valid JSON.
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &result); err != nil {
		t.Errorf("output is not valid JSON: %v\n%s", err, stdout)
	}
	if result["status"] != "success" {
		t.Errorf("status=%v want success", result["status"])
	}
}

func TestCLI_OutputHTML(t *testing.T) {
	stdout, _, code := runCLI(t, "run", testdata("output-json.yaml"), "-m", "html")
	if code != 0 {
		t.Fatalf("exit=%d", code)
	}
	if !strings.Contains(stdout, "<!DOCTYPE html>") {
		t.Errorf("expected HTML output")
	}
}

// ── History ────────────────────────────────────────────────────────────────────

func TestCLI_HistoryList(t *testing.T) {
	// Run a workflow first to create history.
	runCLI(t, "run", testdata("basic.yaml"), "-m", "plain")
	// Now list history.
	stdout, _, code := runCLI(t, "history", "list")
	if code != 0 {
		t.Fatalf("exit=%d", code)
	}
	// Should have at least one execution.
	if strings.Contains(stdout, "no execution history") {
		t.Errorf("expected at least one execution in history")
	}
}

func TestCLI_HistoryShowMasksSensitiveVars(t *testing.T) {
	tmp := t.TempDir()
	secrets := []string{"s3cr3t-token-abcdef12345", "p@ssw0rd-db-98765"}
	wfPath := filepath.Join(tmp, "sensitive-demo.yaml")
	wfYAML := `name: sensitive-demo
variables:
  api_token: s3cr3t-token-abcdef12345
  db_secret: p@ssw0rd-db-98765
  plain_var: hello-world
sensitive:
  - api_token
  - "*_secret"
steps:
  - name: leak
    action: shell
    command: echo "token={{.api_token}} secret={{.db_secret}} plain={{.plain_var}}"
`
	if err := os.WriteFile(wfPath, []byte(wfYAML), 0644); err != nil {
		t.Fatal(err)
	}

	// Run with cwd=tmp so ./executions (the default history dir) lands in tmp.
	_, stderr, code := runCLIInDir(t, tmp, "run", wfPath, "-m", "plain")
	if code != 0 {
		t.Fatalf("run exit=%d stderr=%s", code, stderr)
	}
	execID := ""
	for _, line := range strings.Split(stderr, "\n") {
		if idx := strings.Index(line, "Execution ID:"); idx >= 0 {
			execID = strings.TrimSpace(line[idx+len("Execution ID:"):])
		}
	}
	if execID == "" {
		t.Fatalf("no execution ID in stderr: %s", stderr)
	}

	stdout, _, code := runCLIInDir(t, tmp, "history", "show", execID)
	if code != 0 {
		t.Fatalf("history show exit=%d", code)
	}
	// No real secret anywhere in the output.
	for _, secret := range secrets {
		if strings.Contains(stdout, secret) {
			t.Errorf("history show leaked secret %q:\n%s", secret, stdout)
		}
	}
	// Sensitive vars are masked (exact pattern + glob pattern); the
	// non-sensitive var is untouched.
	for _, want := range []string{"api_token = ***", "db_secret = ***", "plain_var = hello-world"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("history show missing %q:\n%s", want, stdout)
		}
	}
	// Step output arrives already masked by the engine at finalize time
	// (******) — the CLI must display it as-is, not mask again.
	if !strings.Contains(stdout, "******") {
		t.Errorf("expected engine-masked ****** in step output:\n%s", stdout)
	}

	// history list shows only summaries — no variable values can leak.
	listOut, _, code := runCLIInDir(t, tmp, "history", "list")
	if code != 0 {
		t.Fatalf("history list exit=%d", code)
	}
	for _, secret := range secrets {
		if strings.Contains(listOut, secret) {
			t.Errorf("history list leaked secret %q:\n%s", secret, listOut)
		}
	}

	// The on-disk snapshot must keep the REAL values — replay re-injects
	// them. Masking is display-only and must not touch the store.
	raw, err := os.ReadFile(filepath.Join(tmp, "executions", execID+".json"))
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	for _, secret := range secrets {
		if !strings.Contains(string(raw), secret) {
			t.Errorf("snapshot lost real value %q — replay chain broken", secret)
		}
	}
}

// ── Template ───────────────────────────────────────────────────────────────────

func TestCLI_Template(t *testing.T) {
	stdout, _, code := runCLI(t, "template")
	if code != 0 {
		t.Fatalf("exit=%d", code)
	}
	if !strings.Contains(stdout, "name:") || !strings.Contains(stdout, "steps:") {
		t.Errorf("expected template YAML output")
	}
}
