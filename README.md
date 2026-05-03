# goworkflow

A lightweight YAML-driven workflow engine written in Go. Define workflows in YAML, run them from CLI, and change behavior simply by editing the YAML file.

![Workflow Execution](images/night-workflow.png)
![Dark Theme](images/dark-workflow.png)

## Features

- **YAML-first**: Define complete workflows in YAML with variables, conditions, and nested steps
- **Rich Actions**: shell commands, HTTP requests, conditional branching, parallel execution, loops, templates, logging, sleep
- **Hot Reload**: Edit YAML → workflow behavior changes automatically on next run
- **Variable Substitution**: `{{.variable}}` syntax in any string field
- **Zero Dependencies** (beyond `gopkg.in/yaml.v3`): Single binary, no runtime dependencies
- **Dry Run**: Preview workflow execution without running commands

## Quick Start

```bash
# Create a workflow
goworkflow create my-workflow "My first workflow"

# Run it
goworkflow run my-workflow.yaml --verbose

# Validate syntax
goworkflow validate my-workflow.yaml

# View the YAML
goworkflow show my-workflow.yaml

# Show example template
goworkflow template
```

## CLI Commands

| Command | Description |
|---------|-------------|
| `run <file>` | Execute a workflow YAML file |
| `create <name>` | Create a new workflow YAML file |
| `validate <file>` | Validate a workflow YAML file |
| `show <file>` | Display the workflow YAML content |
| `edit <file>` | Open YAML in editor for editing |
| `template` | Show example workflow YAML |

### Run Flags

| Flag | Description |
|------|-------------|
| `--var key=value` | Override or set workflow variables |
| `--verbose` / `-v` | Enable verbose output |
| `--dry-run` | Preview without executing |

## YAML Schema

```yaml
name: my-workflow          # Required: workflow name
version: "1.0"             # Optional: version string
description: "Description" # Optional: human-readable description

variables:                  # Optional: workflow-level variables
  key: value

steps:                      # Required: list of steps
  - name: step-name         # Required: unique step name
    action: shell           # Required: action type
    # ... action-specific fields
```

## Supported Actions

### `shell` - Execute Commands
```yaml
- name: build
  action: shell
  command: "go build -o app ."
  dir: "./src"              # Optional: working directory
  shell: bash               # Optional: sh, bash, cmd, powershell
  env:                      # Optional: step-level env vars
    GOOS: linux
  continue_on_error: false  # Optional: continue on failure
  save_output: build_result # Optional: save output to variable
```

### `http` - HTTP Requests
```yaml
- name: api-call
  action: http
  url: "https://api.example.com/data"
  method: POST              # GET, POST, PUT, DELETE
  headers:
    Content-Type: application/json
  body: '{"key": "{{.value}}"}'
  timeout: "30s"
  save_output: response     # Save response to variable
```

### `condition` - Branching
```yaml
- name: check-env
  action: condition
  expression: "{{.env}} == prod"
  then:
    - name: prod-step
      action: log
      message: "Production!"
  else:
    - name: dev-step
      action: log
      message: "Development!"
```

Supported operators: `==`, `!=`, `contains`, `>`, `<`, `>=`, `<=`, `== empty`

### `set` - Set Variables
```yaml
- name: my-var
  action: set
  value: "computed value {{.other_var}}"
```

### `parallel` - Concurrent Execution
```yaml
- name: parallel-jobs
  action: parallel
  steps:
    - name: job-a
      action: shell
      command: "echo A"
    - name: job-b
      action: shell
      command: "echo B"
```

### `foreach` - Loop Over Items
```yaml
- name: process-services
  action: foreach
  items:
    - "auth"
    - "api"
    - "worker"
  item_var: service         # Default: "item"
  do:
    - name: deploy
      action: log
      message: "Deploying {{.service}}"
```

### `sleep` - Wait
```yaml
- name: wait
  action: sleep
  duration: "5s"            # Go duration format: 1s, 2m, 1h30m
```

### `log` - Print Messages
```yaml
- name: info-msg
  action: log
  message: "Status: {{.status}}"
  level: info               # info, warn, error
```

### `template` - Render Template Files
```yaml
- name: render-config
  action: template
  source: "config.template" # Template file with {{.var}} syntax
  output: "config.yaml"     # Output file path
```

## Edit YAML → Change Behavior

The core idea: **edit the YAML file, re-run, behavior changes**.

```bash
# View current workflow
goworkflow show deploy.yaml

# Edit it (change env from "dev" to "prod", add steps, etc.)
# Any text editor works: VS Code, vim, notepad...

# Re-run - the engine picks up all changes
goworkflow run deploy.yaml --verbose --var ENV=prod
```

## Go Library Usage

```go
package main

import (
    "fmt"
    "goworkflow/workflow"
)

func main() {
    // Create workflow from YAML file
    wf, err := workflow.ParseFile("my-workflow.yaml")
    if err != nil {
        panic(err)
    }

    // Execute
    executor := workflow.NewExecutor(map[string]string{
        "ENV": "production",
    })
    executor.SetVerbose(true)
    result := executor.Execute(wf)
    fmt.Printf("Status: %s\n", result.Status)
}
```

## Build

```bash
cd goworkflow
go mod tidy
go build -o goworkflow.exe .
```

## License

MIT
