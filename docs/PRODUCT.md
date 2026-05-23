# 产品设计文档

本文档描述 goworkflow 的产品定位、设计哲学与长期方向。技术实现细节见 [ARCHITECTURE.md](../ARCHITECTURE.md),实施节奏见 [ROADMAP.md](ROADMAP.md)。

## 一、产品定位

goworkflow 是一个 **YAML 驱动的工作流引擎**。两个核心差异化:

1. **AI 可选介入** —— 没配 AI 时是一个确定性的、可重放的工作流引擎;配了 AI 后,可在显式标定的步骤里引入智能,且不破坏其余流程的确定性。
2. **多渠道触达** —— 同一套引擎与 AI 助手能力,本机 CLI、Web UI、IM Bot(飞书等)、编程 API 都能触发并实时查看执行。

一句话:**让确定性的部分保持确定,让不确定的部分围栏化,让触达方式随场景而变。**

## 二、设计哲学:确定性优先

工作流引擎的价值在于**可重复编排**——同一种输入、同一种拓扑,期望同一种结果。AI 的本质是**概率性推理**,与可重复性天然冲突。

所以我们的核心设计问题是:**把不确定性放在哪一层,如何围栏化。**

原则:

- **workflow 是蓝图,不是活体**。YAML 文件是可审、可版控、可重放的确定性描述。
- **AI 只在显式标定的步骤里作为"函数"介入**。它的产出是一个字符串或布尔值,进入变量系统,其余步骤依然按 DAG 确定地推进。
- **用户应能一眼看出哪些部分"重跑结果可能变"**。这是引擎自动推导出来的,不需要用户手动声明(见下文"双确定性模型")。

> 路线取舍:我们选择 **A 路线**(workflow 永远是可重放蓝图,AI 只当函数用),而非 **E 路线**(AI 动态修改执行图,走向 agentic)。E 路线更激进但会摧毁可重复性、测试性与安全性,目前列为暂缓。

## 三、AI 介入的 6 种模式

从浅到深:

| 模式 | AI 做什么 | 阶段 |
|---|---|---|
| **A. ai action** | 作为一个执行单元(像 shell/http),产出文本 | 近期(Phase 2) |
| **B. ai_decide action** | 做语义判断,产出 true/false 进入分支 | 近期(Phase 2) |
| **F. AI 写/改/解释 workflow** | 编辑期辅助,不参与执行 | 近期(Phase 3) |
| **D. 自然语言触发** | "把 staging 更到最新" → 选 workflow + 填变量 | 近期(Phase 3) |
| **C. on_error: ai** | 失败时 AI 给建议/决定重试 | 中期(Phase 7) |
| **E. AI 动态编排** | 执行中动态注入/修改步骤 | **暂缓**(Phase 9) |

**近期聚焦 A + B + F + D**:

- A/B 完全契合现有 `action` 抽象,改动最小、围栏最牢。
- F 打掉 YAML 学习曲线,是降低门槛的关键。
- D 是产品亮点。D 和 F 合在一起就是一个 `goworkflow chat` 助手:用户说"部署 staging",AI 先判断有没有现成的;有就走 D(选 + 填参 + 跑),没有就走 F(生成草稿 → 用户确认 → 存 → 跑)。

## 四、双确定性模型

"用 AI 的不确定,没用 AI 的确定"——这个直觉对,但引擎层不能这么粗,否则会丢掉很多能力。考虑:

```yaml
- shell: go test          # 确定
- ai: 总结测试报告        # 不确定
- shell: 把总结发邮件      # 输入依赖 ai → 被污染
- shell: git push         # 不依赖 ai → 仍然确定
```

10 步里有 1 步用了 AI,不该把整条 workflow 一律打成"不可重放"。

### 引擎层:per-step 标记 + 依赖传播

- 每个 step 算一个 `Nondeterministic` 位。
- `ai` / `ai_decide` step 天生为 `true`。
- **依赖传播(taint)**:B 吃了 A 的输出,A 非确定 → B 也非确定。类似污点分析,沿 `depends_on` 反向传播。
- workflow 级 `Nondeterministic` = 所有 step 的 OR,**由引擎自动推导**,不需用户声明。

### 用户认知层:"用了 AI" / "没用 AI"

用户在编辑器或前端能一眼看到:这条 workflow 整体重跑结果会不会变、哪些分支是 AI 影响过的、需要人工复核。

### 三种确定性层级

| 层级 | 同输入同输出? | 举例 | 标记 |
|---|---|---|---|
| 纯函数 | ✅ | `set` / `log` / `sleep` | — |
| 有副作用但可复现-ish | ⚠️ 受外部状态影响 | `shell` / `http` / `template`(写盘) | `SideEffecting` |
| 概率性 | ❌ 每次都可能变 | `ai` / `ai_decide` 及其下游 | `Nondeterministic` |

两个维度分开标:`SideEffecting`(有副作用)和 `Nondeterministic`(概率性)。大多数场景用户只关心后者。

### 智能重放

重跑历史执行时:

- deterministic step → 直接复用记录的输出(省钱、省时、可复现调试);
- nondeterministic step → 默认重新调用,或可选复用历史输出。

这直接决定了执行历史持久化的数据结构——存的时候要带上每个 step 的输入输出和确定性标记,否则没法重放。

## 五、Provider 架构

**Provider 接口与具体厂商解耦**,按**协议族**划分,而非按品牌罗列。

### 优先级与覆盖范围

| Provider | 协议 | 覆盖范围 | 阶段 |
|---|---|---|---|
| **Anthropic** | `/v1/messages`,`x-api-key`,顶层 `system`,`content` 数组 | Claude 原生 + **DeepSeek**(`api.deepseek.com/anthropic`)+ 未来兼容此协议的厂商 | **Phase 2 首先实现** |
| **OpenAI-compatible** | `/chat/completions`,`Bearer`,`messages` 数组,`base_url` 可配 | OpenAI / Moonshot / 智谱 / Groq / Ollama(OpenAI 模式)/ LM Studio | Phase 8 |
| **Ollama native** | `/api/chat` | 本地零配置模型 | Phase 8(可选) |

> **关键事实**:DeepSeek 同时提供 OpenAI 格式(`api.deepseek.com`)和 **Anthropic 格式**(`api.deepseek.com/anthropic`)。所以"默认 Anthropic、先接 DeepSeek"在 Anthropic 协议下是同一套代码——一个 Anthropic provider 实现,Claude 和 DeepSeek 立刻都通,只需切 `base_url`。

### 配置原则

- **API key 只走环境变量**(如 `ANTHROPIC_API_KEY`、`DEEPSEEK_API_KEY`),**绝不写进 workflow YAML**(否则会被 `SaveWorkflow` 落盘)。
- `base_url` 可配,便于切厂商/自托管网关。
- Provider 选择与模型参数(workflow 级或 step 级)放配置文件 / 环境变量,不放 YAML。

### 上下文注入策略

不要默认把所有变量喂给 AI(成本 + 信息泄漏)。让用户在 YAML 里显式:

```yaml
- name: summarize
  action: ai
  prompt: "总结这封邮件:{{.email_body}}"
  inputs: [email_body]      # 显式声明喂给 AI 的变量
  # 或省略 inputs,默认只解析 prompt 里的 {{.var}}
```

**默认只传 prompt 模板里出现的变量**——一个安全的 conservative default。

### 围栏原则

- AI 不接触执行编排本身(路线 A,非 E)。AI 不能动态生成要执行的 shell 命令并执行。
- 上下文注入显式可控(默认只传 prompt 出现的变量)。
- AI step 的产出经变量系统流入下游,可被 deterministic step 消费(如发邮件),但整条链的确定性由 taint 传播正确反映。

## 六、渠道架构

goworkflow-server 定位为**渠道无关的工作流助手后端**。核心引擎 + AI 助手是渠道无关的,各种渠道只是"翻译层 adapter"。

### 渠道

| 渠道 | 形态 | 状态 |
|---|---|---|
| **CLI** | 本机交互 TUI,`--output-mode tui` | ✅ 已有 |
| **Web UI** | 自建聊天框 + DAG 可视化编辑器 + 实时执行视图 | ⚠️ 已有 React+WS 基础设施,聊天框待加(Phase 5) |
| **REST + WebSocket API** | 编程式触发与订阅 | ✅ 已有 |
| **IM Bot** | 飞书 / 企业微信 / Slack / Discord | 📋 计划(Phase 6) |

### 典型业务场景

> 用户在飞书发"统计本周提交" → bot webhook → 后端 AI 助手(D:选 workflow + 填参)→ 执行工作流(可能含 AI 摘要 step)→ 结果以飞书卡片消息推回,执行过程的关键节点也实时推卡片。

这个场景的关键在于:**核心引擎与 AI 助手完全不知道消息来自飞书还是 Web**。飞书 adapter 只做两件事:

1. **入站**:把飞书消息翻译成内部"助手请求";
2. **出站**:把内部 `ProgressEvent` 翻译成飞书卡片/消息格式。

### Web 自建 chat 先做

Web 自建 chat 应该**先于** IM bot,因为:

- 基础设施已就绪(React + WebSocket + 实时执行视图);
- 不需要处理 IM 平台的鉴权、webhook 注册、消息格式差异;
- 验证"渠道无关"架构的最小成本路径。

Web chat 跑通后,IM bot 作为新渠道 adapter 接入,复用 Phase 2-5 的全部能力。

## 七、执行可观测性

无论哪个渠道,用户都应能实时看到:

- 当前执行到哪一步(高亮 DAG 节点);
- 每步的输出、耗时、状态;
- **AI step 的流式输出**(token 边产生边推);
- 哪些分支是 AI 影响过的(`Nondeterministic` 标记);
- 失败原因与(未来)`on_error: ai` 的建议。

底层统一通过 `ProgressEvent` 流(含新增的 `ai_token` 事件类型),各渠道 adapter 各自渲染。

## 八、不做 / 暂缓

明确不在近期范围:

- **E 路线(AI 动态编排)**:摧毁可重复性与安全性,暂缓。
- **多用户 / 鉴权**:近期定位本机/可信内网工具。多用户需要的不只是鉴权,还有执行隔离、密钥管理、配额——这是一整套,放后期。
- **图形化拖拽编排替代 YAML**:YAML 是 source of truth,图形编辑器只是 YAML 的视图,不会取代 YAML。

---

技术实现细节见 [ARCHITECTURE.md](../ARCHITECTURE.md),实施节奏见 [ROADMAP.md](ROADMAP.md)。
