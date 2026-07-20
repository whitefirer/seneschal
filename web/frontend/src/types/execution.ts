// Execution 页共享类型：UI 步骤树模型 + API 原始数据模型

// UI 步骤树节点（Execution 页及子组件使用）
export interface Step {
  id: string
  name: string
  description?: string
  status: 'pending' | 'running' | 'success' | 'failed' | 'skipped'
  output?: string
  error?: string
  startTime?: string
  endTime?: string
  duration?: string
  action?: string
  parentId?: string
  children?: Step[]
  // DAG 字段
  next?: string[]
  depends_on?: string[]
  join_mode?: string
  // Condition 字段
  expression?: string
  condition_result?: boolean | null
  then_children?: Step[]
  else_children?: Step[]
  // Sleep 字段
  sleepDuration?: string
  // Shell 字段
  shellCommand?: string
  // HTTP 字段
  httpUrl?: string
  httpMethod?: string
  // Log 字段
  logMessage?: string
  // AI streaming output: incremental tokens accumulate here as they arrive,
  // separate from `output` (which is set on step_complete with the final
  // annotated text). The detail panel renders this as markdown.
  aiOutput?: string
}

// API 返回的原始步骤（JSON 字段名），loadExecution 转换为 Step 树
export interface RawExecutionStep {
  id?: string
  name?: string
  description?: string
  status?: string
  output?: string
  error?: string
  startTime?: string
  endTime?: string
  duration?: string
  action?: string
  next?: string[]
  depends_on?: string[]
  join_mode?: string
  expression?: string
  condition_result?: boolean | null
  sleepDuration?: string
  shellCommand?: string
  httpUrl?: string
  httpMethod?: string
  logMessage?: string
  children?: RawExecutionStep[]
  // foreach 使用 do 而非 children
  do?: RawExecutionStep[]
  // condition 兼容 then/else 属性名
  then_children?: RawExecutionStep[]
  else_children?: RawExecutionStep[]
  then?: RawExecutionStep[]
  else?: RawExecutionStep[]
}

// API 返回的原始日志条目
export interface RawExecutionLog {
  timestamp: string
  level: string
  message: string
}
