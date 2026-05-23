# 技术架构

本文档描述 goworkflow 的技术实现。产品方向见 [docs/PRODUCT.md](docs/PRODUCT.md),实施节奏见 [docs/ROADMAP.md](docs/ROADMAP.md)。

> 本文既是架构文档,也保留开发指南价值(构建命令、约定、注意事项)。

## 目录结构

```
goworkflow/
├── cmd/
│   ├── cli/main.go        # CLI 入口(goworkflow)
│   └── server/main.go     # HTTP server 入口(goworkflow-server)
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
   executeStep (executor.go:677) dispatch → execShell / execHTTP / execCondition / ...
        │
        ▼
   StepResult → 汇总 → WorkflowResult
```

### 容器递归

`condition` / `parallel` / `foreach` 是容器节点。`executeContainerDAG`(`executor_foreach.go:262`)对容器内子步骤再建一个子 DAG 并按 wave 调度。

> ⚠️ 已知技术债:`executeDAG`、`executeForeach` 单轮、`executeContainerDAG` 三处 wave 调度逻辑高度重复(~300 行)。计划抽 `runWave(nodes, deps, execFn)` 复用(见 ROADMAP)。

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

`executeStep`(`executor.go:677`)按 `step.Action` switch:

| action | 实现文件 | 备注 |
|---|---|---|
| `shell` | `executor_shell.go` | `exec.CommandContext`,OS 感知 shell 选择,合并 `os.Environ()` |
| `http` | `executor_http.go` | per-step timeout(默认 60s),结构化存 `{status,body,headers}` |
| `condition` | `executor_condition.go` | expr-lang 求值,失败回退 legacy 字符串比较 |
| `parallel` | `executor_parallel.go` | 每子步一 goroutine + WaitGroup + mutex |
| `foreach` | `executor_foreach.go` | `parseItems` 支持字符串/列表/变量;每轮建子 DAG |
| `set`/`sleep`/`log`/`template` | `executor_actions.go` | 简单 |
| `ai`/`ai_decide` | _(Phase 2)_ | 见下文"AI 集成架构" |

> ⚠️ `condition` 有两条执行路径:`execCondition`(顶层)和 `executeContainerDAG` 内联求值,逻辑重复,计划统一(ROADMAP)。

## 输出体系

### 输出模式(`output_mode.go`)

`plain` / `rich` / `dag` / `timeline` / `compact` / `tui`(别名 `text`/`fancy`/`graph`/`time`/`ci`/`realtime`/`progress`)。

### 四套 Printer(已知技术债)

| Printer | 文件 | LOC | 用途 |
|---|---|---|---|
| `PrettyPrinter` | `pretty.go` | 308 | legacy ANSI,默认 |
| `RichPrinter` | `rich_printer.go` | 500 | lipgloss,plain/rich/dag/timeline/compact |
| `RealtimePrinter` | `realtime_printer.go` | 733 | Bubble Tea TUI |
| `DAGVisualizer` + `TimelineAnimator` | 各自文件 | — | RichPrinter 的 footer 辅助 |

问题:
- **无统一接口**:Executor 持有三个 printer 指针,每个调用点都要 `if richPrinter != nil { ... } else if printer != nil { ... }`。
- **重复**:action→icon map 定义三遍且图标不一致(shell: `💻`/`◇`/`◇`);status→icon 定义两遍;`printFinalResult` 逻辑三份。
- **状态字符串裸用**:`"success"`/`"completed"`/`"done"` 三种写法并存,printer 靠 `case` 兜底。

**演进方向**:定义 `Printer` interface,Executor 只持一个 `Printer`,printer 内部自己决定渲染。ROADMAP Phase 2 顺手做(因为要给 AI 流式输出新增渲染,不想再抄一份)。

## API 契约

### REST

所有 `/api` 路由(在 `cmd/server/main.go:81-89` 注册):

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

> ⚠️ **JSON tag 不一致**(技术债):`LogEntry.StepID` 用 `step_id`(snake),`WSProgressEvent.StepID` 用 `stepId`(camel);`conditionResult`/`condition_result` 同样。前端 `useWebSocket.ts` 两种都接来兜底。ROADMAP 统一成一种。

### 执行编排(`api/handler.go`)

`RunWorkflow`(~580 行)做三件事:
1. 解析 YAML,生成 `exec-YYYYMMDD-HHMMSS-<hex>` ID;
2. 预构建 `StepResult` 树(含 parallel/foreach/condition 嵌套),注册进内存 `executions` map;
3. goroutine 里 `executor.Execute(wf)`,进度回调同时推 WS + 更新内存状态。

> ⚠️ 已知问题:
> - **执行历史全内存**,重启即丢(ROADMAP Phase 4 持久化);
> - `executions` map **无淘汰**,长期运行内存单调上涨;
> - 580 行 + 三层嵌套递归 reconcile 逻辑,**零测试覆盖**。

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
go build -o goworkflow-server ./cmd/server/
go build -o goworkflow ./cmd/cli/

# 运行 server(默认 127.0.0.1:8888)
./start-server.sh
# 或:./goworkflow-server --port 8888

# 前端开发(Vite HMR)
cd web/frontend && npm run dev
```

模块:`goworkflow`,Go 1.24.2。关键依赖:`gorilla/mux`、`gorilla/websocket`、`gopkg.in/yaml.v3`、`charmbracelet/lipgloss`、`charmbracelet/bubbletea`、`expr-lang/expr`。

## 前端

`web/frontend/`:React 18 + TypeScript + Vite 5 + TailwindCSS + React Router v6。关键库:`@xyflow/react`(DAG 编辑器)、`@monaco-editor/react`(YAML 编辑器)、Zustand(主题)、i18next(中英)。Vite 构建到 `../static/`,由 Go server `//go:embed`。

> ⚠️ 巨型组件:`Execution.tsx`(2041 行)、`WorkflowGraphEditor.tsx`(1699)、`WorkflowGraph.tsx`(1558)。维护性隐患,计划拆分。

## 安全

⚠️ **当前版本未做鉴权**。设计上定位为本机/可信内网工具。

**已做(Phase 1)**:
- `goworkflow-server` 默认 bind `127.0.0.1`,不暴露公网;
- workflow name 路径校验,防 `..` 穿越;
- WebSocket `CheckOrigin` 收紧。

**已知未做(ROADMAP)**:
- 无鉴权 / 授权(多用户场景需要);
- 无 TLS / HTTP timeout / 优雅关闭;
- shell action 继承 server 全部环境变量;
- 无请求体大小限制。

**使用约束**:如需远程访问,必须放在带鉴权/TLS/限流的反向代理之后。`shell` action 会以服务进程身份执行任意命令。

## 测试

当前仅 `workflow/context_test.go`(141 行,覆盖 `Context` 的模板/env/duration)。

**ROADMAP**:补 `InferDependencies`、`executeDAG`(含环检测)、`executeForeach`/`parallel` 的表驱动测试;给 `api/handler.go` 的 reconcile 逻辑补测。引入 `go test -race` 到 CI。

## 已知技术债清单

(按优先级,详见 ROADMAP)

1. 三处 wave 调度逻辑重复(~300 行)——抽 `runWave`
2. 四套 Printer 无接口——统一 `Printer` interface
3. ~~`parentId` 共享可变状态~~ ✅ Phase 1 已修(参数化传递,移除 Executor 字段)
4. ~~变量裸 map 遍历~~ ✅ Phase 1 已修(走 `Snapshot()`)
5. JSON tag 不一致(`stepId`/`step_id`)——统一
6. 状态字符串裸用(`"success"`/`"completed"`/`"done"`)——常量化
7. 执行历史全内存、无淘汰——Phase 4 持久化
8. `Workflow.Mode` 字段废弃但保留(兼容旧 YAML)
9. `evaluateExpression` 错误被静默吞(`executor_foreach.go:286,477,497`)
10. `execShell` 用 `context.Background()` 不可取消
11. condition 两条执行路径重复
12. 前端巨型组件(拆分)
13. ~~`hasDAGStructure` 死代码~~ ✅ Phase 1 已删
14. ~~`execHTTP` 每次新建 client~~ ✅ Phase 1 已复用 `e.httpClient`(per-step context 控超时)
