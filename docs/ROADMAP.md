# 实施路线

本文档是 goworkflow 的分阶段实施路线。每个 Phase 尽量自洽可发布、可回滚。依赖关系单向向后。

产品背景见 [PRODUCT.md](PRODUCT.md),技术细节见 [ARCHITECTURE.md](../ARCHITECTURE.md)。

## 状态总览

| Phase | 名称 | 状态 | 依赖 |
|---|---|---|---|
| 0 | 文档 | ✅ 完成 | — |
| 1 | 地基(并发/安全/确定性字段) | ✅ 完成 | 0 |
| 2 | AI 内核(provider + ai action) | ✅ 完成 | 1 |
| 3 | 助手(chat / generate / explain / fix) | ✅ 完成 | 2 |
| 4 | 持久化与智能重放 | ✅ 完成 | 2 |
| 5 | 前端 AI 集成(Web chat + ai_token 流式) | 🚧 进行中(M1 完成,M2/M3 待做) | 2, 3 |
| 5.5 | Artifact 管理 | 📋 计划 | 4 |
| 6 | IM 渠道(飞书等) | 📋 计划 | 2, 3, 5 |
| 7 | 容错(on_error: ai) | 📋 计划 | 2 |
| 8 | 更多 provider(OpenAI 兼容 / Ollama) | 📋 计划 | 2 |
| 9 | 执行沙箱(sandbox) | 📋 计划 | 5.5 |
| 10 | Playbook(可分享可执行文档) | 📋 计划 | 6 |
| 11 | 项目文档站点(VitePress + asciinema) | 📋 计划 | — |
| 12 | Hook 与通知(hook/通知渠道) | 📋 计划 | 5.5, 6 |
| 13 | 前端架构优化(组件拆分/JSON tag 统一) | 📋 计划 | 5 |
| 14 | (暂缓)AI 动态编排 | 🅿️ 暂缓 | 全部 |

---

## Phase 0 — 文档 ✅

**目标**:确立产品方向、技术架构、实施节奏的共识。

**交付**:
- [x] `README.md` 重写(用户向,AI 愿景 + 多渠道 + 安全告示)
- [x] `docs/PRODUCT.md`(产品定位、AI 6 模式、双确定性模型、Provider、渠道)
- [x] `ARCHITECTURE.md` 重写(技术架构、AI 集成、渠道适配层)
- [x] `docs/ROADMAP.md`(本文档)

---

## Phase 1 — 地基 ✅ 完成

**目标**:为 AI 集成打地基,顺手修真实 bug。不引入 AI 依赖。

### 交付
- [ ] **并发 bugfix**:`e.context.Variables` 裸 map 遍历 → 走 `Snapshot()`;`e.parentId` 共享可变状态 → 参数化传递
- [ ] **路径穿越校验**:`api/handler.go` 加 `safePath(name)`,防 `..` 越界
- [ ] **server 默认 bind 127.0.0.1**:`config.ServerConfig` 加 `Host` 字段
- [ ] **StepResult 确定性字段**:`Nondeterministic` + `SideEffecting` 占位(本阶段不实现传播)
- [ ] **死代码清理**:`hasDAGStructure`、`execHTTP` 复用 `httpClient`
- [ ] `go vet ./... && go build ./...` 通过

### 为什么这些放最前
- 并发 race 在 `parallel` 嵌套 `foreach`/`condition` 时会真实触发,AI step 会大量并发,必须先稳;
- 路径穿越单机也会因 `..` 误操作覆盖到 workflows 目录外;
- 确定性字段是 Phase 2 传播算法的地基,提前占位避免届时改全局结构。

---

## Phase 2 — AI 内核 ✅ 完成

**目标**:让 workflow 能在一个步骤里调用 AI。

### 交付
- [ ] `workflow/ai/provider.go` —— `Provider` interface(`Complete` / `Stream`)
- [ ] `workflow/ai/anthropic.go` —— Anthropic 协议实现(`base_url` 可配 → Claude + DeepSeek 同一套代码)
- [ ] `workflow/executor_ai.go` —— `ai` action(文本生成)
- [ ] `workflow/executor_ai.go` —— `ai_decide` action(布尔判断)
- [ ] `executeStep` dispatch 加 `ai` / `ai_decide` case
- [ ] `ProgressEvent` 加 `ai_token` 事件类型
- [ ] `StepResult.Nondeterministic` 在 ai step 填 `true`
- [ ] **确定性传播算法**:InferDependencies 之后加 taint 传播
- [ ] 配置:API key 走环境变量(`ANTHROPIC_API_KEY` / `DEEPSEEK_API_KEY` + `ANTHROPIC_BASE_URL`)
- [ ] `RealtimePrinter` 支持 `ai_token` 流式拼接
- [ ] CLI flag:`--ai-provider`、`--ai-model`
- [ ] 顺手:**Printer interface 统一**(因为要给 AI 流式新增渲染,不想抄第 4 份)
- [ ] 顺手:**状态字符串常量化**(`"success"`/`"completed"`/`"done"` 统一)
- [ ] 表驱动测试:`ai_decide` bool 解析、传播算法

### 验收
```yaml
variables:
  log_text: "..."  # 假设是一段日志
steps:
  - name: summarize
    action: ai
    prompt: "用一句话总结:{{.log_text}}"
    save_output: summary
  - name: is_error
    action: ai_decide
    question: "这段日志是否报错?{{.log_text}}"
    save_output: is_error
  - name: report
    action: log
    message: "摘要:{{.summary}} | 是否报错:{{.is_error}}"
```

`ANTHROPIC_BASE_URL=https://api.deepseek.com/anthropic DEEPSEEK_API_KEY=xxx goworkflow run demo.yaml` 能跑通,DeepSeek 出摘要与判断。

---

## Phase 3 — 助手(D + F) ✅ 完成

**目标**:自然语言选/填/跑已有 workflow,以及生成/改/解释 workflow。

### 交付
- [ ] `workflow/ai/assistant.go` —— 助手核心(渠道无关)
  - 列出 `workflows/` 下所有 YAML 的 name + description
  - D:选 workflow + 识别需要哪些 var + 让用户确认
  - F:generate / explain / fix / edit
- [ ] `cmd/cli` 加 `chat` / `generate` / `explain` / `fix` 子命令
- [ ] 助手用 workflow schema + 已有 YAML 做 few-shot 知识来源
- [ ] 助手产出的 YAML **必经 `validate`**,错误可回喂 AI 修

### 验收
```bash
goworkflow chat "部署 staging"   # 找到 deploy-staging.yaml → 问环境变量 → 执行
goworkflow generate "每晚跑测试并邮件汇报"  # 生成 YAML 草稿 → 确认 → 存盘
goworkflow explain deploy.yaml    # 解释这段 YAML 在干嘛
```

---

## Phase 4 — 持久化与智能重放 ✅ 完成

**目标**:执行历史落盘,可重放,内存不泄漏。

### 交付
- [ ] 执行历史持久化(执行结束写 `executions/<id>.json`,含每步输入输出 + 确定性标记)
- [ ] `executions` 内存 map 加 TTL / 环形缓冲回收
- [ ] 智能重放:重跑历史执行时,deterministic step 复用记录输出,只重新调 AI step
- [ ] server 重启后历史仍可见
- [ ] `go test -race` 到 CI

### 为什么放这
智能重放依赖 Phase 2 的确定性标记。历史落盘的结构要在 AI step 上线后定型(带 input/output/determinism),所以不提前做。

---

## Phase 5 — 前端 AI 集成 🚧 进行中

**目标**:Web UI 能聊天触发 + 实时看 AI 流式输出 + 输出格式扩展。

### M1 — 入口 chat + ai_token 流式 ✅ 完成
- [x] `/api/chat` SSE 端点(thinking → selection → done 事件流)
- [x] ChatPanel 组件(消息流 + 输入 + 选 wf 确认卡片 + markdown 渲染)
- [x] Execution.tsx `ai_token` 流式渲染(逐字 append + MarkdownView)
- [x] react-markdown + remark-gfm 依赖
- [x] `/chat` 路由 + 导航入口

### M2 — 执行页浮动助手 📋 待做
- [ ] 执行页加浮动 Chat 面板,针对当前执行问答
- [ ] 后端:把执行状态(step tree + 变量 + 输出)喂给 AI 的能力(类似 explain 但针对进行中的执行)
- [ ] 助手能回答"这步为什么失败"、"这个变量什么意思"

### M3 — 输出格式扩展 + 按 step 选 model 📋 待做
- [ ] `--output json`:输出 `WorkflowResult` 的 JSON(机器可读,脚本/管道友好)
- [ ] `--output html`:生成可分享的 HTML 执行报告(独立输出格式,不走 artifact 机制)
- [ ] 按 step 选 model:Step 加 `model` 字段,覆盖 workflow 级默认(架构已留口 `ai.Request.Model`)
- [ ] AI step 在 DAG 图上标 `Nondeterministic` 视觉提示

---

## Phase 5.5 — Artifact 管理 📋 计划

**目标**:workflow 的执行产物(artifact)可声明、可追踪、可从历史取回。

### 交付
- [ ] **声明式 artifact**:Step 加 `artifacts: [path...]` 字段,执行后引擎收集这些路径
- [ ] **ArtifactStore 抽象**(类比 ExecutionStore):文件/对象存储可替换
- [ ] **历史集成**:ExecutionSnapshot 记录 artifact 元信息(路径、大小、hash、mime)
- [ ] **重放集成**:deterministic step 复用时,其 artifact 也恢复(原地或从仓库)
- [ ] **Web 下载**:前端可浏览/下载历史执行的 artifact
- [ ] **生命周期**:artifact 跟 execution 走(删历史 = 删产物),或独立 TTL

### 为什么独立 Phase
artifact 和执行历史强相关(Phase 4 已完成),但比 M2/M3 复杂。HTML 报告是独立输出格式(`--output html`),不走 artifact 机制。

---

## Phase 6 — IM 渠道

**目标**:飞书等 IM 触发工作流并实时看结果。

### 交付
- [ ] `channels/feishu/` adapter(webhook 入站、卡片出站、签名校验)
- [ ] `ProgressEvent` → 飞书互动卡片(可更新消息)的翻译层
- [ ] 飞书长文本结果折叠 / 多卡片拆分策略
- [ ] 渠道无关的助手接口对外暴露给 adapter
- [ ] (后续)企业微信 / Slack / Discord adapter

### 复用
复用 Phase 2(provider)、Phase 3(助手)、Phase 5(实时事件)的全部能力。adapter 只做翻译。

---

## Phase 7 — 容错

**目标**:失败时 AI 介入给建议/决定重试。

### 交付
- [ ] step 级 `on_error: ai` 配置
- [ ] 失败上下文(命令、输出、错误)喂给 AI,产出建议 / 重试决策
- [ ] 重试策略与围栏(不让 AI 无限重试)

---

## Phase 8 — 更多 provider

**目标**:覆盖更多模型生态。

### 交付
- [ ] `OpenAIProvider`(`/chat/completions`,`base_url` 可配 → OpenAI / Moonshot / 智谱 / Groq / Ollama-OpenAI 模式 / LM Studio)
- [ ] `OllamaProvider`(native,本地零配置)
- [ ] provider 选择可在 CLI flag / 配置文件切换

---

## Phase 9 — 执行沙箱(sandbox) 📋 计划

**目标**:隔离执行环境,让 shell action 不污染宿主。和多用户/安全强相关。

### 交付
- [ ] 容器化执行(docker/podman 后端,每个执行一个临时容器)
- [ ] 或轻量隔离(seccomp/chroot/namespace,不依赖 docker)
- [ ] 资源限制(CPU/内存/时间/磁盘)
- [ ] 网络策略(允许/禁止出站)
- [ ] 文件系统隔离(只允许工作目录读写)

### 为什么放后期
sandbox 是基础设施级工程,和 AI 主线正交。在单机/可信内网场景下非必需;多用户/公网暴露时是 P0。

---

## Phase 10 — Playbook(可分享可执行文档) 📋 计划

**目标**:workflow + 说明 = playbook,可分享给他人照着跑。

### 交付
- [ ] playbook 格式:workflow YAML + 文档段(README + 步骤说明 + 前置条件)
- [ ] playbook 仓库(可分享链接,接收方一键运行)
- [ ] 和渠道(Phase 6)集成:飞书/Web 推送 playbook 卡片,点击执行
- [ ] playbook 版本化

### 定位
playbook 是渠道(Phase 6)的延伸:不只是"执行结果推到飞书",而是"可执行的文档推到飞书"。

---

## Phase 11 — 项目文档站点(VitePress + asciinema) 📋 计划

**目标**:项目文档站点化 + 终端演示录制。

### 交付
- [ ] VitePress 站点(文档、教程、API 参考)
- [ ] asciinema 录制(TUI/chat/replay 演示)
- [ ] 部署(GitHub Pages / 自托管)
- [ ] docs/ 现有文档迁移到 VitePress

### 定位
项目治理,和引擎核心能力正交。提升项目形象和上手体验,但不打断 AI 主线。

---

## Phase 12 — Hook 与通知 📋 计划

**目标**:执行生命周期的扩展点(hook)+ 开箱即用的通知能力。

### Hook(底层机制)
- [ ] workflow/step 级 hook:`before_step` / `after_step` / `on_success` / `on_failure`
- [ ] hook 可调外部 webhook(URL + payload 模板),或执行一段 shell
- [ ] hook 拿到 `StepResult` / `WorkflowResult` 上下文
- [ ] `on_error: ai`(Phase 7)作为 hook 的一个内置实例

### 通知(hook 的预设应用)
- [ ] step 级通知:`on_complete: notify`,推送到配置的渠道
- [ ] 应用级通知配置(`~/.goworkflow/notify.yaml`):webhook URL / Slack / 邮件 / 飞书
- [ ] 前端可配置:Web UI 里选哪些 step 完成时通知、推到哪个渠道
- [ ] 通知模板:step 结果/workflow 摘要/失败详情

### 定位
hook 是"让用户挂钩自定义逻辑"的通用机制;通知是"最常见场景(完成时推消息)"的预设,让用户不写代码就能用。和 Phase 6(IM 渠道)的区别:渠道是"触发+看结果"的双向,hook/通知是"单向推"的。

---

## Phase 13 — 前端架构优化 📋 计划

**目标**:清理前端技术债,提升可维护性。

### 交付
- [ ] **JSON tag 统一**(`stepId`/`step_id` 选一种,后端 + 前端同步,删掉双字段兜底)
- [ ] **Execution.tsx 拆分**(2041 行 → `<StepList>` / `<StepDetail>` / `<LogPanel>`)
- [ ] 全局 toast/error 通知机制(替代散落的 console.error)
- [ ] 状态字符串常量化(前后端同步)
- [ ] 历史管理前端 UI(复用 Phase 4 的 history API)
- [ ] bundle 拆分(当前 766KB 单 chunk,code-split)

---

## Phase 14 — (暂缓)AI 动态编排

**目标**(若做):AI 在执行中动态注入/修改步骤。

**暂缓原因**:
- 摧毁可重复性与可测试性
- 安全风险指数级放大(AI 动态生成 shell 并执行)
- 需要重新定义"执行/重跑/测试"的含义

若未来做,需先解决:执行沙箱、AI 可执行能力白名单、完整审计日志、可观测性大幅增强。

---

## 原则

1. **YAML 是 source of truth**。所有产物(CLI/前端/IM)都是 YAML 的视图。
2. **确定性优先**。不确定的部分(AI)围栏化,不让它污染整条流程的可重放性。
3. **渠道无关**。核心引擎 + AI 助手不知道触达渠道,adapter 只做翻译。
4. **API key 永远不进 YAML**。只走环境变量 / 配置文件。
5. **每个 Phase 自洽可发布**。不依赖未完成的后续 Phase。
6. **不改 YAML schema 破坏兼容**。新字段一律向后兼容(`omitempty`)。
