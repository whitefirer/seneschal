// DAG 编辑器类型定义

import { Node, Edge } from '@xyflow/react'

// 节点数据类型
export interface DAGNodeData {
  [key: string]: any  // 索引签名，满足 React Flow 类型要求
  
  name: string
  action: string
  
  // 父级关系（用于嵌套结构，与原编辑器一致）
  parentId?: string
  branchType?: 'then' | 'else' | 'parallel' | 'do'
  branchIndex?: number
  
  // 子节点数据（用于 YAML 导出时重新组装）
  childSteps?: any[]  // condition 的 then/else 或 parallel 的 steps
  doSteps?: any[]     // foreach/loop 的 do
  
  // DAG 依赖
  next?: string[]
  depends_on?: string[]
  
  // 业务字段（所有 action 的字段）
  description?: string
  if?: string
  loop?: number
  run?: string
  message?: string
  level?: string
  url?: string
  method?: string
  body?: string
  script?: string
  shell?: string
  duration?: number
  items?: any[] | string
  items_text?: string  // 用于 UI 输入的文本格式
  item_var?: string
  variable?: string
  value?: string
  template?: string
  
  // UI 状态
  _calculatedHeight?: number
  
  // 运行时状态（可选）
  status?: 'pending' | 'running' | 'success' | 'failed' | 'skipped'
  output?: string
  error?: string
}

// 节点类型
export type DAGNode = Node<DAGNodeData>

// 边类型
export type DAGEdge = Edge

// 动作类型列表（用于 UI 选择）
export const ACTION_TYPES = [
  { value: 'log', label: 'Log', icon: 'MessageSquare' },
  { value: 'shell', label: 'Shell', icon: 'Terminal' },
  { value: 'http', label: 'HTTP', icon: 'Globe' },
  { value: 'script', label: 'Script', icon: 'Code' },
  { value: 'condition', label: 'Condition', icon: 'GitBranch' },
  { value: 'parallel', label: 'Parallel', icon: 'Layers' },
  { value: 'foreach', label: 'Foreach', icon: 'Repeat' },
  { value: 'sleep', label: 'Sleep', icon: 'RotateCcw' },
  { value: 'set', label: 'Set', icon: 'FileText' },
  { value: 'template', label: 'Template', icon: 'FileText' },
] as const

export type ActionType = typeof ACTION_TYPES[number]['value']

// YAML 结构
export interface WorkflowYAML {
  steps: any[]
}
