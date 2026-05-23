# 实施路线

本文档是 goworkflow 的分阶段实施路线。每个 Phase 尽量自洽可发布、可回滚。依赖关系单向向后。

产品背景见 [PRODUCT.md](PRODUCT.md),技术细节见 [ARCHITECTURE.md](../ARCHITECTURE.md)。

## 状态总览

| Phase | 名称 | 状态 | 依赖 |
|---|---|---|---|
| 0 | 文档 | ✅ 进行中 | — |
| 1 | 地基(并发/安全/确定性字段) | ✅ 进行中 | 0 |
| 2 | AI 内核(provider + ai action) | 📋 计划 | 1 |
| 3 | 助手(chat / generate / explain / fix) | 📋 计划 | 2 |
| 4 | 持久化与智能重放 | 📋 计划 | 2 |
| 5 | 前端 AI 集成(Web chat + ai_token 流式) | 📋 计划 | 2, 3 |
| 6 | IM 渠道(飞书等) | 📋 计划 | 2, 3, 5 |
| 7 | 容错(on_error: ai) | 📋 计划 | 2 |
| 8 | 更多 provider(OpenAI 兼容 / Ollama) | 📋 计划 | 2 |
| 9 | (暂缓)AI 动态编排 | 🅿️ 暂缓 | 全部 |

---

## Phase 0 — 文档 ✅

**目标**:确立产品方向、技术架构、实施节奏的共识。

**交付**:
- [x] `README.md` 重写(用户向,AI 愿景 + 多渠道 + 安全告示)
- [x] `docs/PRODUCT.md`(产品定位、AI 6 模式、双确定性模型、Provider、渠道)
- [x] `ARCHITECTURE.md` 重写(技术架构、AI 集成、渠道适配层)
- [x] `docs/ROADMAP.md`(本文档)

---

## Phase 1 — 地基 ✅ 进行中

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

## Phase 2 — AI 内核

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

## Phase 3 — 助手(D + F)

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

## Phase 4 — 持久化与智能重放

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

## Phase 5 — 前端 AI 集成

**目标**:Web UI 能聊天触发 + 实时看 AI 流式输出。

### 交付
- [ ] WebSocket 协议加 `ai_token` 事件
- [ ] `useWebSocket.ts` 处理 `ai_token`(去重兼容字段、删掉双 tag 兜底)
- [ ] Web 自建聊天框组件(选/填/跑/取消)
- [ ] 执行视图支持 AI 流式 token 显示
- [ ] AI step 在 DAG 图上标 `Nondeterministic` 视觉提示

### 顺手
- [ ] **JSON tag 统一**(`stepId`/`step_id` 选一种,前端去掉双字段兼容)
- [ ] 巨型组件拆分(`Execution.tsx` 等)

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

## Phase 9 — (暂缓)AI 动态编排

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
