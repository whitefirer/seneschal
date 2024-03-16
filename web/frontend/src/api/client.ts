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
