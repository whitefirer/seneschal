# DSH 插件集成决策备忘

> 状态：决策建议（待落地）
> 日期：2026-08
> 结论一句话：**Go 内核保留，DSH 插件只做薄 TS 适配层；DSH 是 seneschal 的第 5 个渠道，不是新家。**

## 1. 背景

DeepSeek Harness（DSH）是一个 Cordis 插件框架组装的 agent 运行环境。评估「要不要为 seneschal 做一个 DSH 插件」时，先厘清 DSH 里已经存在的三坨「工作流」：

| 层 | 载体 | 定位 |
|---|---|---|
| 编排（LLM 中心） | DSH 内置 `workflow` 工具 | JS 脚本 + 子 agent fan-out，非确定性 |
| 定义（可视化） | `deepseek-flow` 插件 | Markdown 流程图 + 布尔门，**不执行** |
| 执行（确定性） | **seneschal** | YAML + DAG + shell/http/condition/parallel/foreach，AI 仅作标定步骤 |

三者在不同层，**不重叠**。seneschal 补的是 DSH 生态里真正缺的空位：可审计、可版控、可重放的确定性动作执行器。

## 2. 结论

- **Go 版 seneschal 原样保留、继续演进**，确定性执行内核的定位不丢。
- **新建 `dsh-seneschal`（TS 薄插件）**，只做「调 `seneschal-server` 的 HTTP/WS + 把 `ProgressEvent` 翻译进 GUI」这一层适配。
- **不重写执行引擎**。只有日后真遇到「必须进程内互调」的具体需求时，再评估是否只把那一小块挪进 TS。

## 3. 为什么 Go 版要留

seneschal 真正值钱、也最难重做的能力，全在 `workflow/` Go 内核：

- DAG 调度：Kahn 拓扑排序 + 环检测 + wave 并发
- 容器递归：condition/parallel/foreach 的嵌套子 DAG
- 变量上下文：RWMutex + 模板渲染 + `Snapshot()` 并发安全
- `expr-lang` 表达式求值
- shell/http/script 的副作用语义与命令注入面
- 确定性污染传播（`Nondeterministic`/`SideEffecting` taint）+ 智能重放

这些是带测试、带边界条件教训的代码（`ARCHITECTURE.md` 技术债清单里的「✅ 已修」都是踩坑换来的）。用 TS 重写 = 零新增能力的前提下重踩一遍所有坑，还丢掉独立的 Go 库（cenacle 已通过 `RegisterAction` 消费它）、CLI、内嵌前端。

`ARCHITECTURE.md` 已写死分层：「`workflow/` 是无状态内核，`cmd/` 是有状态外壳。内核不依赖外壳。」内核 = Go 保留；外壳 = 想加多少加多少，包括一个 TS 插件。

## 4. DSH 插件 = seneschal 的第 5 个渠道

seneschal 本来的渠道适配层就是为这种事设计的：

```
CLI / Web / IM bot / API  →  助手请求  →  Executor.Execute  →  ProgressEvent  →  各渠道 adapter
```

DSH 插件正好是这条链路上的一个新 adapter，而不是吞掉 seneschal 的新宿主。因此「一等公民」目标不需要重写内核——TS 插件吃 Go server 已暴露的 API + WS 即可：

| 插件要做的 | 对应 seneschal 已有的能力 |
|---|---|
| `seneschal_list/read/validate` 工具 | `GET /api/workflows`、`POST .../validate` |
| `seneschal_run` 工具 | `POST /api/workflows/{name}/run` → `executionId` |
| 结构化结果（`StepResult`/`WorkflowResult`） | 本来就是 JSON-tagged |
| GUI 实时进度 | `GET /api/ws` 的 `WSProgressEvent` 流 + `GET /api/executions/{id}` |

## 5. 分档落地建议

| 档位 | 内容 | 必要性 |
|---|---|---|
| **Tier 0** | 直接用 `bash`/`curl` 调 seneschal | 已存在，零成本 |
| **Tier 1** | 薄插件：`seneschal_list/read/validate/run` 工具，返回结构化 JSON（含 `Nondeterministic`/`SideEffecting` 标记） | 高性价比，1~2 天量级 |
| **Tier 2** | 重插件：`ProgressEvent` 流进 GUI 实时面板 + 执行历史 + DAG/timeline 渲染 | 仅高频通过 GUI 跑 seneschal 才回本 |

**建议：不「必要」，但 Tier 1 值得，Tier 2 暂缓。**

## 6. 什么时候 TS 重写才会赢

只有一种情况：需要**进程内耦合**而非跨进程调用——例如 seneschal 的某 step 要直接调 DSH 的 agent/tool，或 DSH 要在 workflow 运行中途改其变量上下文。此时 IPC 边界才成为真正阻力。

但「seneschal step 调用 AI」已由 `ai`/`ai_decide` action + Provider 抽象（Anthropic/OpenAI/Ollama）解决，方向是「seneschal 调外部 AI」，不需要 DSH 进程内。分发顾虑（不想让用户装 Go）可用「插件自带/自动拉起 `seneschal-server` 二进制」解决，不必重写。

> 2026-08 核实：`ai`/`ai_decide` 已在 `workflow/action.go` 注册为内置 action（均标记 `Nondeterministic: true`），`workflow/executor_ai.go`（约 400 行）为完整实现（流式、会话记忆、token 预算、上下文注入、JSON 展开、容错布尔解析）；`workflow/ai/` 含 Anthropic/OpenAI/Ollama 三个 provider，另有 tool calling 与 `assistant.go` 的 agent loop。README 原先的 `_(Roadmap)_` 标记已移除（文档滞后，非代码缺口）。

## 7. DSH 插件机制速记（已核实）

- 插件 = npm 包，`package.json` 声明 `dsh.bundle.patch` → `cordis.patch.yml` → `insert` 一个 Cordis 插件实例。
- `apply(ctx)` 里可 `ctx.tools.register(defineTool(...))`、`ctx.skills.register(...)`、`ctx.webServer.register(...)`、`ctx.inject(["agents","subagents"])`、`ctx.provide(...)`。
- 可选 `dsh.client` 半边（`platform: "web"` + `inject` UI slots）往 Web GUI 塞 React 界面。
- 安装：`dsh plugin --profile <name> add <package>`（转发给 pnpm，并把声明了 `dsh.bundle` 的包追加进 `dsh.profile.bundles` 层栈）。
- 参考实现：`deepseek-flow`（工具 + skill + 画布 UI）、`dsh-browser-fs`（工具 + 自建 WS 路由）。
