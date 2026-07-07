# AI Provider API 规范参考

本文档记录三种 provider 协议的关键差异，供实现/调试参考。

## 1. Anthropic Messages API

**文档**：
- Messages API: https://docs.anthropic.com/en/api/messages
- Tool Use: https://docs.anthropic.com/en/docs/build-with-claude/tool-use
- Streaming: https://docs.anthropic.com/en/api/messages-streaming
- API Reference: https://platform.anthropic.com/docs/api-reference/messages

**端点**: `POST /v1/messages`

**认证**: `x-api-key: <key>` + `anthropic-version: 2023-06-01`

**请求结构**:
```json
{
  "model": "claude-sonnet-4-5-20250929",
  "max_tokens": 1024,
  "system": "system prompt",
  "messages": [
    {"role": "user", "content": [{"type": "text", "text": "hello"}]}
  ],
  "tools": [
    {
      "name": "get_weather",
      "description": "Get weather",
      "input_schema": {"type": "object", "properties": {...}}
    }
  ]
}
```

**响应（tool_use）**:
```json
{
  "stop_reason": "tool_use",
  "content": [
    {"type": "text", "text": "Let me check..."},
    {"type": "tool_use", "id": "toolu_xxx", "name": "get_weather", "input": {"location": "SF"}}
  ]
}
```

**回传 tool_result（关键！）**:
```json
{
  "role": "user",
  "content": [
    {
      "type": "tool_result",
      "tool_use_id": "toolu_xxx",
      "content": "结果文本",           // ← 用 content 不是 text！
      "is_error": false
    }
  ]
}
```

> ⚠️ **踩过的坑**：tool_result 的内容字段是 `content`，不是 `text`。用错会导致模型收到空结果。

**Streaming 事件**:
- `message_start` — 初始 usage
- `content_block_start` — 新 block 开始（含 tool_use 的 id/name）
- `content_block_delta` — 增量内容（text_delta 或 input_json_delta）
- `content_block_stop` — block 完成
- `message_delta` — stop_reason
- `message_stop` — 终止

**SSE 格式**: `event: <type>\ndata: <json>\n\n`

## 2. OpenAI Chat Completions API

**文档**：
- API Reference: https://platform.openai.com/docs/api-reference/chat
- Function Calling: https://platform.openai.com/docs/guides/function-calling

**端点**: `POST /v1/chat/completions`

**认证**: `Authorization: Bearer <key>`

**请求结构**:
```json
{
  "model": "gpt-4o",
  "messages": [
    {"role": "system", "content": "system prompt"},
    {"role": "user", "content": "hello"}
  ],
  "tools": [
    {
      "type": "function",
      "function": {
        "name": "get_weather",
        "description": "Get weather",
        "parameters": {"type": "object", "properties": {...}}
      }
    }
  ]
}
```

**响应（tool_calls）**:
```json
{
  "choices": [{
    "message": {
      "role": "assistant",
      "content": null,
      "tool_calls": [{
        "id": "call_xxx",
        "type": "function",
        "function": {"name": "get_weather", "arguments": "{\"location\":\"SF\"}"}
      }]
    },
    "finish_reason": "tool_calls"
  }]
}
```

**回传 tool result（关键差异！）**:
```json
{
  "role": "tool",                    // ← 独立 role，不是 user 里嵌 block
  "tool_call_id": "call_xxx",
  "content": "结果文本"
}
```

> ⚠️ OpenAI 和 Anthropic 的 tool use 格式完全不同：
> - Anthropic: `role: "user"` + `content: [{type: "tool_result", ...}]`
> - OpenAI: `role: "tool"` + `content: "文本"` (独立消息)

**Streaming**: SSE `data: {chunk}\n\n`，每个 chunk 有 `choices[0].delta.content`

## 3. DeepSeek

**文档**：
- Anthropic 格式: https://api-docs.deepseek.com/zh-cn/guides/anthropic_api
- OpenAI 格式: https://api-docs.deepseek.com/zh-cn
- Function Calling (OpenAI): https://api-docs.deepseek.com/zh-cn/guides/function_calling

**端点**:
- Anthropic: `https://api.deepseek.com/anthropic/v1/messages`
- OpenAI: `https://api.deepseek.com/v1/chat/completions`

**模型名**:
- `deepseek-v4-flash` — 快速模型（原 deepseek-chat）
- `deepseek-v4-pro` — 思考模型（原 deepseek-reasoner）
- 注意：deepseek-chat / deepseek-reasoner 于 2026-07-24 弃用

**Tool Use 支持**:
- Anthropic 格式：支持（通过兼容层）
- OpenAI 格式：原生支持

## 4. Ollama

**文档**: https://github.com/ollama/ollama/blob/main/docs/api.md

**端点**: `POST /api/chat`

**认证**: 无（本地服务）

**请求结构**:
```json
{
  "model": "llama3.2",
  "messages": [{"role": "user", "content": "hello"}],
  "stream": true
}
```

**Streaming**: NDJSON（每行一个 JSON 对象）
```jsonl
{"message":{"role":"assistant","content":"Hel"},"done":false}
{"message":{"role":"assistant","content":"lo"},"done":false}
{"message":{"role":"assistant","content":""},"done":true,"eval_count":12}
```

**Tool Use**: 支持（在 messages 里传 tools，格式接近 OpenAI）

## 5. goworkflow 实现

| Provider | 文件 | Complete | Stream | Tool Use | 协议 |
|---|---|---|---|---|---|
| AnthropicProvider | `ai/anthropic.go` | ✅ | ✅ SSE | ✅ CompleteRaw | Anthropic Messages |
| OpenAIProvider | `ai/openai.go` | ✅ | ✅ SSE | ❌ 待做 | OpenAI Chat |
| OllamaProvider | `ai/ollama.go` | ✅ | ✅ NDJSON | ❌ 待做 | Ollama Chat |

**Tool Use 实现状态**：
- Anthropic: 完整支持（CompleteRaw + tool_result 循环）
- OpenAI: 需实现 `role: "tool"` 消息格式转换
- Ollama: 需实现 OpenAI 格式的 tool calling（Ollama 兼容 OpenAI 格式）
