# 技术架构

本文档描述 seneschal 的技术实现。产品方向见 [docs/PRODUCT.md](docs/PRODUCT.md),实施节奏见 [docs/ROADMAP.md](docs/ROADMAP.md)。

> 本文既是架构文档,也保留开发指南价值(构建命令、约定、注意事项)。

## 目录结构

```
seneschal/
├── cmd/
│   ├── cli/main.go        # CLI 入口(seneschal)
│   └── server/main.go     # HTTP server 入口(seneschal-server)
├── api/                   # HTTP / WebSocket 层
│   ├── handler.go         # REST handler + 执行编排
│   ├── websocket.go       # WS hub + 客户端管理
│   └── types.go           # API DTO
├── workflow/              # 核心执行引擎(可复用 Go 库)
│   ├── workflow.go        # 核心类型:Step / Workflow / StepResult / WorkflowResult
│   ├── context.go         # 变量上下文(mutex + 模板解析)
│   ├── parser.go          # YAML 解析 + 依赖推断(InferDependencies)
│   ├── executor.go        # Executor + DAG 调度(executeDAG)
│   ├── executor_*.go      # 各 action 实现(per-file)
│   ├── rich_printer.go    # lipgloss 富文本输出
│   ├── pretty.go          # 传统 ANSI 输出(legacy)
│   ├── realtime_printer.go # Bubble Tea TUI 输出
│   ├── dag_visualizer.go  # DAG 树形渲染
│   ├── timeline_animator.go # 时间线渲染
│   ├── theme.go           # 主题
│   └── output_mode.go     # 输出模式枚举
├── config/                # server 配置
├── web/
│   ├── embed.go           # //go:embed static/*
│   ├── static/            # 前端构建产物(gitignored except index.html)
│   └── frontend/          # React 源码
├── workflows/             # workflow YAML 文件(磁盘存储)
├── examples/              # 示例 workflow
└── docs/                  # 设计文档
```

## 三入口职责

| 入口 | 职责 | 不做 |
|---|---|---|
| `cmd/cli` | 本机运行/校验/查看 workflow,交互 TUI | 不起 HTTP |
| `cmd/server` | HTTP + WebSocket,内嵌前端,远程运行/编辑/可视化 | 不直接跑 workflow 逻辑,委托给 `workflow/` |
| `workflow/` | 纯执行引擎:DAG 调度、变量、各种 action | 不关心 IO 来源(文件/HTTP 都行)、不关心输出渠道 |

`workflow/` 是无状态内核,`cmd/` 是有状态外壳。内核不依赖外壳。

## 执行管线

```
ParseFile / Parse (parser.go)
        │
        ▼
   Workflow struct
        │
        ▼
InferDependencies (parser.go:193)   ← 5 趟:填运行时元数据、链式依赖、next→depends_on、容器递归、校验
        │
        ▼
Workflow.Validate (parser.go:89)    ← 返回 []error,runWorkflow 校验
        │
        ▼
Executor.Execute (executor.go)
        │
        ▼
executeDAG (executor.go:467)
   ├─ buildDAGGraph   (executor.go:329)   ← 建节点、规约 Next/DependsOn
   ├─ topologicalSort (executor.go:410)   ← Kahn 算法 + 环检测
   └─ wave 并发执行   (executor.go:511)   ← 每层 ready 节点并发跑,WaitGroup + mutex
        │
        ▼
   遇到容器节点(condition/parallel/foreach)→ executeContainerDAG 递归
        │
        ▼
   executeStep (executor.go:677) dispatch → execShell / execHTTP / execAI / ... (容器节点不走这里)
        │
        ▼
   StepResult → 汇总 → WorkflowResult
```

### 容器递归

`condition` / `parallel` / `foreach` 是容器节点。`executeContainerDAG`(`executor_foreach.go:262`)对容器内子步骤再建一个子 DAG 并按 wave 调度。

> ✅ 已统一:三处 wave 调度(`executeDAG`/`executeContainerDAG`/`executeForeach`)已抽出共享的 `runWaves(waveConfig)`(executor.go:754),行为差异(取消检查、join_mode、skipped 合成、事件 parent、失败文案)以选项参数表达。

## 内核数据结构

### `Step`(`workflow/workflow.go:4`)

一个扁平 struct,承载**所有** action 类型的字段(shell/http/condition/parallel/foreach/set/sleep/log/template),加上 DAG 字段(`Next`/`DependsOn`/`JoinMode`)和运行时元数据(`ParentId`/`BranchType`/`BranchIndex`,`yaml:"-"`)。

### `Workflow`(`workflow.go:68`)

`Name`/`Version`/`Description`/`Variables`/`Mode`/`Steps`。

> `Mode` 字段("linear"/"dag")已废弃——执行统一走 DAG。字段保留以兼容旧 YAML,新文档标注废弃。

### `Context`(`context.go:14`)

`sync.RWMutex` + `Variables map[string]string` + `Results map[string]string`。通过 `Set`/`Get`/`Snapshot`/`ResolveTemplate` 访问,锁正确。

> ⚠️ **并发注意**:遍历变量必须走 `Snapshot()`,不能直接 range `e.context.Variables`——并发 `Set` 会触发 runtime panic。Phase 1 已修。

### `StepResult` / `WorkflowResult`(`workflow.go:77, 111`)

JSON-tagged 结果容器。`StepResult` 同样是扁平 struct,承载所有 action 的元数据。

**Phase 1 新增确定性字段**(为 AI 集成打地基):

```go
type StepResult struct {
    // ... 现有字段 ...
    Nondeterministic bool `json:"nondeterministic,omitempty"` // 本步非确定(AI 及其下游)
    SideEffecting    bool `json:"sideEffecting,omitempty"`    // 本步有副作用(shell/http/template)
}

type WorkflowResult struct {
    // ... 现有字段 ...
    Nondeterministic bool `json:"nondeterministic,omitempty"` // 整条 workflow 是否含非确定步骤
}
```

## 确定性传播算法(Phase 2 实现)

在 `InferDependencies` 之后、`executeDAG` 之前,加一趟 taint 传播:

```
1. 初始化:action == "ai" || action == "ai_decide" → Nondeterministic = true
2. 反向传播:遍历依赖图,若 A 的输出被 B 消费(B depends_on A),且 A.Nondeterministic → B.Nondeterministic = true
3. 重复直到不动点
4. WorkflowResult.Nondeterministic = OR(所有 step.Nondeterministic)
```

> Phase 1 只加字段占位 + 文档,不实现传播。

## Action dispatch

action 由 `workflow/action.go` 的注册表驱动,不再硬编码 switch。每个 action 是一个 `ActionSpec`(`Name` / `IsContainer` / `SideEffecting` / `Nondeterministic` / `Run` / `Validate` / `CommandForError`),内置 action 在包 `init` 时注册。`executeStep` 按 `step.Action` 查表调用 `spec.Run`;校验走 `spec.Validate`(`ValidateStep` 查表);副作用/确定性标记由 `spec.SideEffecting`/`spec.Nondeterministic` 统一填入(替代原先散落两处的 switch)。

> **扩展机制**:外部包(如 cenacle)无需 fork seneschal,只需在自身 `init` 里 `workflow.RegisterAction(ActionSpec{...})` 即可新增自定义 action(如 `agent`)。handler 通过 `e.GetContext().Set/Get` 读写变量、`e.OnProgress(...)` 发事件;校验/副作用/on_error 命令提取均由注册表覆盖。示例见 `workflow/action_registry_test.go`。

内置 action:

| action | 实现文件 | 备注 |
|---|---|---|
| `shell` | `executor_shell.go` | `exec.CommandContext`,OS 感知 shell 选择,合并 `os.Environ()` |
| `http` | `executor_http.go` | per-step timeout(默认 60s),结构化存 `{status,body,headers}` |
| `condition` | `executor_condition.go` | 容器;expr-lang 求值,失败回退 legacy 字符串比较 |
| `parallel` | `executor_foreach.go`(`executeContainerDAG`) | 容器;子步 wave 并发 |
| `foreach`/`loop` | `executor_foreach.go` | 容器;`parseItems` 支持字符串/列表/变量;每轮建子 DAG;`loop` 是 `foreach` 别名 |
| `set`/`sleep`/`log`/`template` | `executor_actions.go` | 简单 |
| `script` | `executor_script.go` | 内嵌代码(python/node/...),变量经 stdin JSON |
| `workflow` | `executor_workflow.go` | 子工作流调用 |
| `ai`/`ai_decide` | `executor_ai.go` | 见下文"AI 集成架构" |

> ✅ 已统一:`condition` 旧的顶层直发路径 `execCondition` 已删除;action dispatch / 校验 / 副作用标记 / on_error 命令提取现均走 `ActionSpec` 注册表(2026-08 起)。

## 输出体系

### 输出模式(`output_mode.go`)

`plain` / `rich` / `dag` / `timeline` / `compact` / `tui`(别名 `text`/`fancy`/`graph`/`time`/`ci`/`realtime`/`progress`)。

### Printer 统一接口(已完成)

| Printer | 文件 | LOC | 用途 |
|---|---|---|---|
| `PrettyPrinter` | `pretty.go` | ~350 | legacy ANSI,默认 |
| `RichPrinter` | `rich_printer.go` | ~490 | lipgloss,plain/rich/dag/timeline/compact |
| `RealtimePrinter` | `realtime_printer.go` | ~800 | Bubble Tea TUI |
| `DAGVisualizer` + `TimelineAnimator` | 各自文件 | — | RichPrinter 的 footer 辅助 |

现状:
- 已定义统一 `Printer` interface(`workflow/printer.go`),Executor 只持一个 `Printer`。
- action→icon、status→icon 与 final-result 渲染已抽成共享函数,避免三份漂移。
- TUI 通过 `Runner` / `EventStreamer` 两个窄接口接入生命周期与事件流,不污染普通 Printer。
- 状态字符串由 `status.go` 的 `Status*` 常量表达。

## API 契约

### REST

所有 `/api` 路由(在 `api/routes.go` 注册,`cmd/server/main.go` 只负责静态文件与中间件):

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/workflows` | 列表 |
| GET | `/api/workflows/{name}` | 获取 YAML 内容 |
| PUT | `/api/workflows/{name}` | 保存(body=raw text) |
| DELETE | `/api/workflows/{name}` | 删除 |
| POST | `/api/workflows/{name}/validate` | 校验 |
| POST | `/api/workflows/{name}/run` | 异步运行,返回 `executionId` |
| GET | `/api/executions` | 执行历史列表 |
| GET | `/api/executions/{id}` | 单次执行详情 |
| GET | `/api/ws` | WebSocket |

响应统一 envelope:`{ success, data?, error?, message? }`。

### WebSocket

- 客户端发 `{type: "subscribe"|"unsubscribe", data: {executionId}}`;空 sub 集 = 订阅全部。
- 服务端推 `WSProgressEvent`:`type ∈ {workflow_start, step_start, step_output, step_complete, workflow_end}`。

JSON 字段统一使用 camelCase:`WSProgressEvent.StepID` 与 `LogEntry.StepID` 均为 `stepId`,`ConditionResult` 为 `conditionResult`。

### 执行编排(`api/run_workflow.go` + `api/execution_handlers.go`)

`RunWorkflow` / `ReplayExecution` / Chat `run_workflow` / Runbook 触发现在共享同一套执行管道:
1. 解析 YAML,生成 `exec-YYYYMMDD-HHMMSS-<hex>` ID;
2. 预构建 `StepResult` 树(含 parallel/foreach/condition 嵌套),注册进内存 `executions` map(100 条上限,超限淘汰最旧);
3. goroutine 里 `executor.Execute(wf)`,进度回调同时推 WS + 更新内存状态;
4. 结束统一 `reconcileExecution`:回写状态、持久化到 `FileStore`、广播 `workflow_end`。

> 已知改进空间:
> - `executions` 内存缓存有 100 条上限,磁盘历史由 `FileStore` 轮转保留;
> - `updateStepStatus` / `updateTree` / `findStepDef` / `buildResultMap` 已有单测覆盖,但可继续扩展更多嵌套/foreach 场景。

## AI 集成架构(Phase 2)

### Provider 接口

```go
// workflow/ai/provider.go (Phase 2 新增)
package ai

type Provider interface {
    // 普通补全,返回最终文本
    Complete(ctx context.Context, req Request) (Response, error)
    // 流式,token 边产生边推
    Stream(ctx context.Context, req Request, onToken func(string)) (Response, error)
}

type Request struct {
    System   string            // 顶层 system prompt(Anthropic 风格)
    Prompt   string            // 用户输入
    Inputs   map[string]string // 显式注入的变量(默认只传 prompt 模板里出现的)
    Model    string
    MaxTokens int
    // ...
}
```

### Provider 实现

| 实现 | 协议 | 配置 | 覆盖 |
|---|---|---|---|
| `AnthropicProvider` | `/v1/messages` | `ANTHROPIC_API_KEY` + `ANTHROPIC_BASE_URL`(默认 `https://api.anthropic.com`) | Claude 原生 + DeepSeek(配 `https://api.deepseek.com/anthropic`) |
| `OpenAIProvider` | `/chat/completions` | `OPENAI_API_KEY` + `OPENAI_BASE_URL` | OpenAI / Moonshot / 智谱 / Groq / Ollama(OpenAI 模式) |
| `OllamaProvider` | `/api/chat` | `OLLAMA_HOST`(默认 localhost) | 本地模型 |

> DeepSeek 同时支持 OpenAI 与 Anthropic 协议。**Phase 2 首先实现 `AnthropicProvider`**——一套代码同时覆盖 Claude 和 DeepSeek(切 `base_url`),正好匹配"默认 Anthropic、先接 DeepSeek"。

### `ai` / `ai_decide` action

在 `executeStep` dispatch 加两个 case(`workflow/executor_ai.go`,Phase 2):

```go
case "ai":
    return e.execAI(ctx, step, parentID)
case "ai_decide":
    return e.execAIDecide(ctx, step, parentID)
```

- `execAI`:调 `provider.Complete/Stream`,`save_output` 存文本。`Nondeterministic = true`。
- `execAIDecide`:调 provider,要求只回 true/false,`save_output` 存 bool。`Nondeterministic = true`。

### 流式输出事件

`ProgressEvent`(`executor.go:16`)新增事件类型:

```go
// 现有: workflow_start / step_start / step_output / step_complete / workflow_end
// 新增:
// ai_token —— AI step 流式 token,Output 字段带增量文本
```

`RealtimePrinter` 详情面板拼接 `ai_token`;server 端 WS 推给前端;未来 IM adapter 翻译成卡片更新。

### 上下文注入

默认只解析 `prompt` 模板里出现的 `{{.var}}`,只把这些变量传给 provider(conservative default)。用户可用 `inputs: [...]` 显式声明。

### 确定性

- `ai`/`ai_decide` step → `Nondeterministic = true`(在 executeStep 里填);
- 下游 step 通过 taint 传播标记;
- 智能重放(Phase 4):重跑历史时 deterministic step 复用记录输出,只重新调 AI step。

## 渠道适配层(Phase 5-6)

```
渠道(CLI / Web / IM bot / API)
        │
        ▼
   助手请求(统一内部格式)
        │
        ▼
   AI 助手(D: 选 workflow + 填参;F: 生成/改 workflow)
        │
        ▼
   Executor.Execute → ProgressEvent 流
        │
        ▼
   渠道 adapter(把 ProgressEvent 翻译成各渠道的渲染)
```

### 飞书 adapter(Phase 6)示例职责

- **入站**:接收飞书 webhook 消息 → 翻译成助手请求;
- **出站**:把 `ProgressEvent` 翻译成飞书互动卡片(可更新消息);
- **鉴权**:校验飞书签名;
- **格式**:工作流结果以结构化卡片呈现(表格、多列、链接)。

**核心:引擎与助手完全不知道消息来自飞书**。Web chat(Phase 5)先做,验证渠道无关架构,IM bot 作为新 adapter 接入。

## 构建 & 运行

```bash
# 全量构建(前端 + server + cli)
./build.sh

# 手动
cd web/frontend && npm run build && cd ../..
go build -o seneschal-server ./cmd/server/
go build -o seneschal ./cmd/cli/

# 运行 server(默认 127.0.0.1:8888)
./start-server.sh
# 或:./seneschal-server --port 8888

# 前端开发(Vite HMR)
cd web/frontend && npm run dev
```

模块:`github.com/whitefirer/seneschal`,Go 1.24.2。关键依赖:`gorilla/mux`、`gorilla/websocket`、`gopkg.in/yaml.v3`、`charmbracelet/lipgloss`、`charmbracelet/bubbletea`、`expr-lang/expr`。

## 前端

`web/frontend/`:React 18 + TypeScript + Vite 5 + TailwindCSS + React Router v6。关键库:`@xyflow/react`(DAG 编辑器)、`@monaco-editor/react`(YAML 编辑器)、Zustand(主题)、i18next(中英)。Vite 构建到 `../static/`,由 Go server `//go:embed`。

> 组件已按职责拆分:`Execution.tsx`(~370 行)、`WorkflowGraph.tsx`(~375 行)、`GraphEditor.tsx`(~320 行),并抽出 `execution/StepList*`、`StepDetailPanel`、`LogPanel` 等。`LogPanel.tsx`(~900 行)仍是后续可继续拆分的对象。

## 安全

⚠️ **当前定位为本机/可信内网工具**,已实现基础安全防线;多用户/公网部署仍需更强隔离。

**已做**:
- `seneschal-server` 默认 bind `127.0.0.1`,不暴露公网;
- workflow name / runbook workflow 路径校验,防 `..` 穿越;
- WebSocket `CheckOrigin` + CORS 白名单收紧;
- 可选 `auth_token` Bearer 鉴权(绑定非 loopback 且未配置时启动打印醒目警告);
- `http.Server` 超时、请求体大小限制、优雅关闭;
- 变量/输出脱敏(`sensitive:` 声明)。

**已知未做(ROADMAP / 长期)**:
- 多用户授权与执行隔离;
- TLS(建议由反向代理终结);
- 执行沙箱(shell/script/template 仍以服务进程身份运行);
- template 输出已限制在工作流目录内,但 shell/script 的任意命令能力是设计使然。

**使用约束**:如需远程访问,必须放在带鉴权/TLS/限流的反向代理之后。`shell` action 会以服务进程身份执行任意命令。

## 测试

当前覆盖:
- Go:`workflow/`(executor/parser/replay/runbook/mask/hook/determinism 等)、`api/`(REST e2e、安全、runbook、replay、step-tree 单测)、`cmd/`(CLI e2e)、`config/`。
- 前端:Vitest 单测(utils/stepGraph/execution 组件等)。
- CI:`gofmt`、`go vet`、`go build`、`go test -race`、前端 lint/typecheck/test/build。

## 已知技术债清单

(按优先级,详见 ROADMAP)

1. `Workflow.Mode` 字段废弃但保留(兼容旧 YAML)
2. `Step` 是扁平 struct,承载所有 action 字段,后续 action 增多会继续膨胀
3. `LogPanel.tsx`(~900 行)仍偏大,可继续拆分
4. 结构化日志/metrics 尚未落地(目前 `log.Printf` / `fmt.Printf` 为主)
5. ~~Chat/Replay/Runbook 触发未走统一执行管道~~ ✅ 已统一到 `Handler.StartRunFromWorkflow`
6. `openai` provider 对本地自定义 base_url 允许空 key(官方端点会强制校验)
7. 执行沙箱(sandbox/WASM/Docker)未做,shell/script 仍以服务进程身份执行
8. 多用户授权、执行隔离、TLS 终结仍为长期规划
