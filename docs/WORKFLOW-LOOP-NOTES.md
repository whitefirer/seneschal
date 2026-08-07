# 工作流回退（Fallback/Loop）调研与建议

> 2026-08-06 · 来自 cenacle 集成调研（cenacle 的"Tester 失败 → 回退 Coder"需求）。
> 性质：设计建议，非当前承诺。决策权在 seneschal 产品方向。

## 背景：DAG 无环 vs 修复循环

cenacle 的多角色工作流（Coder → Reviewer → Tester）需要**失败回退**：Tester 测试失败 → 回到 Coder 修复 → 再走 Reviewer → 再 Tester，直到通过或轮数耗尽。

seneschal 当前是**纯 DAG**（Kahn 拓扑排序 + 环检测，有环直接报错）——**无法表达循环**。现有可用的近似手段：

| 现有能力 | 能否表达回退循环 |
|----------|------------------|
| `condition` then/else | ❌ 分支是一次性的，不能"重跑上游节点" |
| `foreach` | ❌ 静态 items（固定轮数），且不能动态决定是否继续 |
| `on_error: ai/ai_auto` | ⚠️ 只重试**当前步**（retry 上限 3），不能"回到上游重新执行" |

## 当前方案（分层，cenacle 侧实现）

**seneschal 零改动**：seneschal 执行"一轮 DAG"（Coder→Reviewer→Tester），**轮间循环由 cenacle 调度器控制**：

```
seneschal（一轮 DAG，确定性可重放）        cenacle 调度器（轮间状态机）
Coder → Reviewer → Tester                 round 1: Tester 失败
        (condition 门控)                    → 记录失败原因 + 最大轮数 N
                                          → round 2: 重新触发一轮
                                            （上轮失败上下文注入 variables）
                                          → 直到通过或 N 轮耗尽
```

- 每轮是独立 DAG 执行（DAG 内无环）
- 轮间上下文：上轮结果（失败原因、测试输出）注入下一轮 `variables`
- 优点：不动执行器；缺点：**工作流 YAML 不自包含**（循环语义在 cenacle 层，重放不完整）

## 长期建议：seneschal 支持受限回退

作为**独立通用引擎**，回退是通用需求（部署失败回滚、测试失败重跑），且与 `on_error: ai/ai_auto` 的 AI 错误恢复体系天然衔接。建议的**受限回退**（非全量重调度）：

### YAML 形态草案

```yaml
steps:
  - name: coder
    action: shell
    command: "write code"
    on_fail:
      goto: coder          # 失败时回到哪个 step（自己 = 重试当前步）
      max_retries: 3       # 本回退路径最多执行次数（防死循环）
      reset: [reviewer]    # 可选：回退时重置哪些下游（默认全部下游）
  - name: reviewer
    action: shell
    command: "review"
  - name: tester
    action: shell
    command: "run tests"
    on_fail:
      goto: coder          # 测试失败 → 回 Coder 修复
      max_retries: 2
```

### 执行语义要点

- **回退 = 重新调度 goto 目标及其下游**（DAG 中 goto 目标之后的所有依赖链重跑）
- **环防护**：`max_retries` 是硬上限（计数按"回退路径整体"而非单步）；超限走 `on_error` 兜底（fail/abort）
- **确定性保持**：重放时记录回退路径（每次 goto 的决策点 + 次数），重放按记录路径执行——不破坏"可审、可重放"
- **AI 接入**：`on_error: ai` 的 AI 可以**建议回退目标**（"错误像是测试环境问题，回退到 tester 而不是 coder"）——回退决策交给 AI，`max_retries` 仍是硬约束
- **可视化**：回退路径在 DAG 图上画成"回头箭头"（工作流历史里可展开每一轮）

### 建议的升级时机

1. **现在不做**：cenacle v1.0 先用分层方案跑通，验证真实回退需求（轮数、上下文、降级策略）
2. **触发条件**：cenacle 工作流真实使用后，抽象出"分层方案的痛点"（跨轮变量传递繁琐、状态可视化断裂、YAML 不自包含）——若痛点成立，则升级
3. **范围**：大概率是上述**受限回退**（`on_fail: goto` + `max_retries`），不是全量重调度（避免执行器核心大改）

## 一句话

**短期分层（cenacle 侧轮次循环）、长期受限回退（`on_fail: goto` + 上限）**——受限回退是通用引擎的自然能力，与 AI 错误恢复体系衔接，且保留确定性/可重放卖点。
