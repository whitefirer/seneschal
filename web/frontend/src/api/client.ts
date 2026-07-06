import axios from 'axios'

const API_BASE = '/api'

export interface WorkflowInfo {
  name: string
  fileName: string
  version?: string
  description?: string
  steps: number
  variables: number
  modifiedAt: string
  size: number
}

export interface WorkflowContent {
  name: string
  fileName: string
  content: string
}

export interface RunRequest {
  variables?: Record<string, string>
  dryRun?: boolean
}

export interface RunResponse {
  executionId: string
  status: string
}

export interface ExecutionRecord {
  id: string
  workflowName: string
  workflowFile: string
  status: string
  startTime: string
  endTime: string
  duration: string
  error?: string
  stepsCount: number
}

export interface APIResponse<T> {
  success: boolean
  message?: string
  data?: T
  error?: string
}

const api = axios.create({
  baseURL: API_BASE,
  headers: {
    'Content-Type': 'application/json',
  },
})

export const workflowsApi = {
  list: async (): Promise<WorkflowInfo[]> => {
    const res = await api.get<APIResponse<WorkflowInfo[]>>('/workflows')
    if (!res.data.success) throw new Error(res.data.error)
    return res.data.data || []
  },

  get: async (name: string): Promise<WorkflowContent> => {
    const res = await api.get<APIResponse<WorkflowContent>>(`/workflows/${name}`)
    if (!res.data.success) throw new Error(res.data.error)
    return res.data.data!
  },

  save: async (name: string, content: string): Promise<void> => {
    const res = await api.put<APIResponse<null>>(`/workflows/${name}`, content, {
      headers: {
        'Content-Type': 'text/plain; charset=utf-8',
      },
    })
    if (!res.data.success) throw new Error(res.data.error)
  },

  delete: async (name: string): Promise<void> => {
    const res = await api.delete<APIResponse<null>>(`/workflows/${name}`)
    if (!res.data.success) throw new Error(res.data.error)
  },

  validate: async (name: string): Promise<{ valid: boolean; steps: number; variables: number }> => {
    const res = await api.post<APIResponse<{ valid: boolean; steps: number; variables: number }>>(
      `/workflows/${name}/validate`
    )
    if (!res.data.success) throw new Error(res.data.error)
    return res.data.data!
  },

  run: async (name: string, req?: RunRequest): Promise<RunResponse> => {
    const res = await api.post<APIResponse<RunResponse>>(
      `/workflows/${name}/run`,
      req || {}
    )
    if (!res.data.success) throw new Error(res.data.error)
    return res.data.data!
  },
}

export const executionsApi = {
  list: async (): Promise<ExecutionRecord[]> => {
    const res = await api.get<APIResponse<ExecutionRecord[]>>('/executions')
    if (!res.data.success) throw new Error(res.data.error)
    return res.data.data || []
  },

  get: async (id: string): Promise<ExecutionRecord & { logs: any[]; steps: any[] }> => {
    const res = await api.get<APIResponse<any>>(`/executions/${id}`)
    if (!res.data.success) throw new Error(res.data.error)
    return res.data.data!
  },
}

// ── Chat (AI assistant) ────────────────────────────────────────────────────

export interface ChatSelection {
  workflow: string
  variables: Record<string, string>
  confidence: number
  steps?: { name: string; action: string }[]
  available?: string[]
}

export interface ChatSSEEvent {
  // SSE event types from /api/chat (thinking/selection/done/error) and
  // /api/executions/{id}/ask (thinking/token/done/error). Kept as string so
  // new event types don't require a type change.
  type: string
  [key: string]: any
}

export const chatApi = {
  // sendMessage POSTs to /api/chat and streams SSE events back. EventSource
  // only supports GET, so we use fetch + ReadableStream to parse the SSE
  // stream manually. onEvent is called for each parsed event; returns when
  // the stream ends (done/error) or the AbortSignal aborts.
  sendMessage: async (
    message: string,
    onEvent: (event: ChatSSEEvent) => void,
    signal?: AbortSignal,
  ): Promise<void> => {
    const res = await fetch('/api/chat', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ message }),
      signal,
    })
    if (!res.ok || !res.body) {
      throw new Error(`chat request failed: ${res.status}`)
    }

    const reader = res.body.getReader()
    const decoder = new TextDecoder()
    let buffer = ''

    for (;;) {
      const { done, value } = await reader.read()
      if (done) break
      buffer += decoder.decode(value, { stream: true })

      // SSE events are separated by a blank line (\n\n). Parse complete ones.
      let idx: number
      while ((idx = buffer.indexOf('\n\n')) >= 0) {
        const raw = buffer.slice(0, idx)
        buffer = buffer.slice(idx + 2)
        const event = parseSSE(raw)
        if (event) onEvent(event)
      }
    }
    // Flush any trailing event.
    if (buffer.trim()) {
      const event = parseSSE(buffer)
      if (event) onEvent(event)
    }
  },
}

// parseSSE parses one raw SSE block (may contain `event:` and `data:` lines).
function parseSSE(raw: string): ChatSSEEvent | null {
  let eventType = 'message'
  let dataStr = ''
  for (const line of raw.split('\n')) {
    if (line.startsWith('event:')) eventType = line.slice(6).trim()
    else if (line.startsWith('data:')) dataStr += line.slice(5).trim()
  }
  if (!dataStr) return null
  try {
    return { type: eventType, ...JSON.parse(dataStr) }
  } catch {
    return { type: eventType, raw: dataStr }
  }
}

export const askApi = {
  // askExecution POSTs a question about a specific execution and streams the
  // AI answer back. Same SSE parsing as chatApi.
  askExecution: async (
    executionId: string,
    question: string,
    onEvent: (event: ChatSSEEvent) => void,
    signal?: AbortSignal,
  ): Promise<void> => {
    const res = await fetch(`/api/executions/${executionId}/ask`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ question }),
      signal,
    })
    if (!res.ok || !res.body) {
      throw new Error(`ask request failed: ${res.status}`)
    }
    const reader = res.body.getReader()
    const decoder = new TextDecoder()
    let buffer = ''
    for (;;) {
      const { done, value } = await reader.read()
      if (done) break
      buffer += decoder.decode(value, { stream: true })
      let idx: number
      while ((idx = buffer.indexOf('\n\n')) >= 0) {
        const raw = buffer.slice(0, idx)
        buffer = buffer.slice(idx + 2)
        const event = parseSSE(raw)
        if (event) onEvent(event)
      }
    }
    if (buffer.trim()) {
      const event = parseSSE(buffer)
      if (event) onEvent(event)
    }
  },
}
