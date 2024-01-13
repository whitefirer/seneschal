// 统一 DAG 节点类型定义
import type { Node } from '@xyflow/react'

export interface NodePosition {
  x: number
  y: number
}

export interface NodeMeta {
  is_container?: boolean
  container_type?: 'condition' | 'parallel' | 'foreach' | 'loop'
  branch_type?: 'then' | 'else' | 'parallel' | 'do'
  view_mode?: 'workflow' | 'dag'
}

// 统一节点数据结构
export interface GraphNodeData {
  // 基础字段
  name: string
  action: string
  description?: string
  
  // DAG 核心字段
  next?: string[]
  depends_on?: string[]
  join_mode?: 'all' | 'any'
  
  // Workflow 容器字段
  parentId?: string
  branchIndex?: number
  branchType?: string
  childSteps?: any[]
  doSteps?: any[]
  // Parallel/Foreach 嵌套子节点数组
  steps?: any[]
  do?: any[]
  collapsed?: boolean
  
  // 业务字段
  if?: string
  loop?: string
  run?: string
  message?: string
  level?: string
  url?: string
  method?: string
  body?: string
  script?: string
  shell?: string
  command?: string
  duration?: string
  items?: any
  item_var?: string
  items_text?: string
  env?: Record<string, string>
  vars?: Record<string, any>
  output?: string
  retry?: any
  timeout?: string
  condition?: string
  
  // UI 内部字段
  _calculatedHeight?: number
  
  // 元数据
  meta?: NodeMeta
  
  // 执行状态
  status?: 'pending' | 'running' | 'success' | 'failed' | 'skipped'
  error?: string
  startTime?: string
  endTime?: string
  output_vars?: Record<string, any>
  
  // 索引签名（满足 Node<T> 的约束）
  [key: string]: any
}

// React Flow 节点类型
export interface GraphNode extends Node<GraphNodeData> {
  id: string
  type: string
  position: NodePosition
  data: GraphNodeData
  selected?: boolean
  style?: React.CSSProperties
  zIndex?: number
  hidden?: boolean
  positionAbsolute?: NodePosition
  measured?: {
    width: number
    height: number
  }
  draggable?: boolean
}

// React Flow 边类型
export interface GraphEdge {
  id: string
  source: string
  target: string
  type?: string
  style?: React.CSSProperties
  animated?: boolean
  markerEnd?: any
}

// 工作流定义
export interface WorkflowDefinition {
  name?: string
  version?: string
  description?: string
  variables?: Record<string, any>
  mode?: 'linear' | 'dag'
  steps: any[]
}
