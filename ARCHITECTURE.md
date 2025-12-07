# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Run

```bash
# Full build (frontend + server + cli)
./build.sh

# Manual
cd web/frontend && npm run build && cd ../..
go build -o goworkflow-server ./cmd/server/
go build -o goworkflow ./cmd/cli/

# Run server (default port 8888)
./start-server.sh
# or: ./goworkflow-server --port 8888

# Frontend dev (with Vite HMR, proxies /api to :8080)
cd web/frontend && npm run dev
```

Module: `goworkflow`, Go 1.24.2. Key deps: `gorilla/mux`, `gorilla/websocket`, `gopkg.in/yaml.v3`.

## Architecture

Three entry points — `cmd/cli/main.go` (CLI), `cmd/server/main.go` (HTTP server), `workflow/` (core engine).

**Server** registers routes in `cmd/server/main.go` via gorilla/mux, embeds `web/static/*` via `//go:embed`, serves SPA fallback on unmatched routes.

**API routes** (all under `/api`):
- `GET/PUT/DELETE /workflows`, `/workflows/{name}` — CRUD + list
- `POST /workflows/{name}/validate`, `/workflows/{name}/run`
- `GET /executions`, `/executions/{id}`
- `GET /ws` — WebSocket for real-time execution events

**Core engine** (`workflow/`):
- `Workflow` → `Steps` → each `Step` has `Action` (shell/http/condition/parallel/foreach/set/sleep/log/template)
- All execution goes through DAG scheduler: `Executor.Execute()` → `InferDependencies()` → `executeDAG()` (Kahn topological sort, concurrent layer execution)
- Linear mode = chain DAG; DAG mode uses explicit `next`/`depends_on`
- `Context` provides `{{.variable}}` template resolution via Go `text/template`
- Output modes: plain, rich (lipgloss), DAG ASCII, timeline, realtime TUI
- `WorkflowManager` in `loader.go` handles file watching + hot reload

**Frontend** (`web/frontend/`): React 18 + TypeScript + Vite 5 + TailwindCSS + React Router v6. Key libs: `@xyflow/react` (DAG editor), `@monaco-editor/react` (YAML editor), Zustand (theme), i18next (zh/en). Vite builds to `../static/`, embedded by Go server.

## Workflow YAML

```yaml
name: example
mode: linear          # or dag
variables:
  GOOS: linux
steps:
  - name: build
    action: shell
    command: go build
    next: [test]      # DAG mode
  - name: test
    action: shell
    command: go test ./...
    depends_on: [build]
```

Actions: `shell` (command), `http` (url), `condition` (expression, then/else), `parallel` (steps), `foreach` (items, do), `set` (value), `sleep` (duration), `log`, `template`.

Variables: `{{.var}}` in any string field, sourced from YAML `variables` block, step `output_var`/`output_vars`, shell inherits system env.

## Notes

- Server stores workflows as YAML files on disk under `workflows/` directory
- Execution is async — `/run` returns `executionId`, progress via WebSocket or poll `/executions/{id}`
- `web/static/` is build output, gitignored except `index.html`
