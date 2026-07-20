// WorkflowGraph 共享类型与纯逻辑（布局常数、LOD 分层渲染、foreach 迭代过滤）

// FlowStep 接口
export interface FlowStep {
  id: string
  name: string
  action: 'http' | 'script' | 'shell' | 'log' | 'condition' | 'loop' | 'foreach' | 'parallel' | 'sleep' | ''
  description?: string
  status?: 'pending' | 'running' | 'success' | 'failed' | 'skipped'
  output?: string
  error?: string
  duration?: string
  if?: string
  loop?: string
  parallel?: boolean
  children?: FlowStep[]
  url?: string
  method?: string
  script?: string
  shell?: string
  message?: string
  level?: string
  run?: string
  body?: string
  collapsed?: boolean
  position?: { x: number; y: number }
  // foreach 特有字段
  items?: any[]
  itemVar?: string
  _originalChildrenCount?: number  // 原始子节点数量（用于判断是否有省略）
  _skippedCount?: number           // 省略的子节点数量
  // DAG 特有字段
  next?: string[]         // 指定下一节点列表（DAG模式）
  depends_on?: string[]   // 依赖的节点列表（DAG模式）
  join_mode?: string      // 汇合模式: "all" 或 "any"
  // Condition 特有字段
  expression?: string           // 条件表达式
  then_children?: FlowStep[]    // then 分支子步骤
  else_children?: FlowStep[]    // else 分支子步骤
  then?: FlowStep[]              // YAML 中的 then 属性（兼容）
  else?: FlowStep[]              // YAML 中的 else 属性（兼容）
  condition_result?: boolean | null    // 条件求值结果
  // Sleep 特有字段
  sleepDuration?: string        // Sleep 休眠时长参数
  // Shell 特有字段
  shellCommand?: string         // Shell 命令
  // HTTP 特有字段
  httpUrl?: string              // HTTP URL
  httpMethod?: string           // HTTP Method
  // Log 特有字段
  logMessage?: string           // Log 消息内容
}

export interface NodeData {
  id: string
  name: string
  action: string
  description?: string
  status?: string
  duration?: string
  hasChildren: boolean
  childrenCount: number
  children?: FlowStep[]  // 子节点列表，用于状态聚合
  isCollapsed: boolean
  onToggleCollapse?: (nodeId: string) => void
  // foreach 特有
  items?: any[]
  itemVar?: string
  // condition 特有（FlowStep 透传字段）
  expression?: string
  conditionResult?: boolean | null
  condition_result?: boolean | null
  // sleep 特有（FlowStep 透传字段）
  sleepDuration?: string
  // 分层渲染特有
  zoomLevel?: number
  totalNodeCount?: number
}

export interface FlatFlowStep extends FlowStep {
  parentId?: string
  position: { x: number; y: number }
}

// Layout 常数
export const NODE_WIDTH = 240
export const NODE_HEIGHT = 100
export const H_SPACING = 60
export const V_SPACING = 50  // 垂直间距
export const PARENT_CHILD_GAP = 60
export const FOREACH_PADDING = 30 // foreach 容器内边距
export const PARALLEL_PADDING = 30 // parallel 容器内边距
export const FOREACH_ITERATION_THRESHOLD = 4 // foreach 迭代次数阈值，超过则聚合显示
export const PARALLEL_TASK_THRESHOLD = 5 // parallel 子节点数量阈值，超过则默认折叠

// 分层渲染（Level of Detail）配置 - 动态阈值
export const LOD_CONFIG = {
  // 节点数量阈值
  ALWAYS_DETAILED: 50,      // < 50 节点，始终显示详细视图
  MODERATE_THRESHOLD: 200,  // 50-200 节点，适度分层
  AGGRESSIVE_THRESHOLD: 500,// > 200 节点，激进分层

  // 缩放级别阈值
  ZOOM_LEVELS: {
    HIDE: 0.15,             // < 15% 缩放：隐藏节点（只画点）
    SIMPLIFIED: 0.3,        // < 30% 缩放：简化视图（色块）
    NORMAL: 0.6,            // < 60% 缩放：标准视图
    DETAILED: 1.0,          // >= 60% 缩放：详细视图
  }
}

// 详细程度级别
export enum DetailLevel {
  HIDE = 0,         // 隐藏（只渲染一个点）
  SIMPLIFIED = 1,   // 简化（色块 + 状态）
  NORMAL = 2,       // 标准（名称 + 类型 + 状态）
  DETAILED = 3      // 详细（所有信息 + 日志预览）
}

// 根据节点数量和缩放级别计算详细程度（动态阈值）
export function calculateDetailLevel(nodeCount: number, zoom: number): DetailLevel {
  // 节点少时，始终显示详细视图
  if (nodeCount < LOD_CONFIG.ALWAYS_DETAILED) {
    return DetailLevel.DETAILED
  }

  // 节点中等时，适度分层
  if (nodeCount < LOD_CONFIG.MODERATE_THRESHOLD) {
    if (zoom < LOD_CONFIG.ZOOM_LEVELS.SIMPLIFIED) return DetailLevel.SIMPLIFIED
    if (zoom < LOD_CONFIG.ZOOM_LEVELS.NORMAL) return DetailLevel.NORMAL
    return DetailLevel.DETAILED
  }

  // 节点多时，激进分层
  if (zoom < LOD_CONFIG.ZOOM_LEVELS.HIDE) return DetailLevel.HIDE
  if (zoom < LOD_CONFIG.ZOOM_LEVELS.SIMPLIFIED) return DetailLevel.SIMPLIFIED
  if (zoom < LOD_CONFIG.ZOOM_LEVELS.NORMAL) return DetailLevel.NORMAL
  return DetailLevel.DETAILED
}

// 从节点 ID 中解析迭代索引
export function extractIterationIndex(nodeId: string): number | null {
  // ID 格式: "xxx-iter-0", "xxx-item-0-step-0", "step-xxx-iter-0"
  const iterMatch = nodeId.match(/-iter-(\d+)/)
  if (iterMatch) return parseInt(iterMatch[1], 10)

  const itemMatch = nodeId.match(/-item-(\d+)-step-/)
  if (itemMatch) return parseInt(itemMatch[1], 10)

  return null
}

// 过滤 foreach 子节点：按迭代分组，只保留首、尾、失败的迭代
export function filterForeachChildren(children: FlowStep[], itemsCount?: number): { filtered: FlowStep[], originalIterations: number, skippedIterations: number } {
  if (!children || children.length === 0) {
    return { filtered: [], originalIterations: 0, skippedIterations: 0 }
  }

  // 按迭代索引分组
  const iterationMap = new Map<number, FlowStep[]>()
  let maxIterationIndex = -1

  for (const child of children) {
    const iterIndex = extractIterationIndex(child.id)
    if (iterIndex !== null) {
      if (!iterationMap.has(iterIndex)) {
        iterationMap.set(iterIndex, [])
      }
      iterationMap.get(iterIndex)!.push(child)
      maxIterationIndex = Math.max(maxIterationIndex, iterIndex)
    } else {
      // 无法解析迭代索引的节点（可能是模板节点），直接保留
      // 不分组，后面单独处理
    }
  }

  // 迭代次数：使用 itemsCount（如果有）或从分组推断
  const originalIterations = itemsCount || (maxIterationIndex + 1)

  // 少于阈值，全部显示
  if (originalIterations <= FOREACH_ITERATION_THRESHOLD) {
    return { filtered: children, originalIterations, skippedIterations: 0 }
  }

  const filtered: FlowStep[] = []

  // 找出失败的迭代索引
  const failedIterations = new Set<number>()
  for (const [iterIndex, iterChildren] of iterationMap) {
    if (iterChildren.some(c => c.status === 'failed')) {
      failedIterations.add(iterIndex)
    }
  }

  // 第一次迭代（始终显示）
  if (iterationMap.has(0)) {
    filtered.push(...iterationMap.get(0)!)
  }

  // 失败的迭代（排除第一次和最后一次）
  for (const iterIndex of failedIterations) {
    if (iterIndex > 0 && iterIndex < maxIterationIndex) {
      filtered.push(...iterationMap.get(iterIndex)!)
    }
  }

  // 最后一次迭代（始终显示）
  if (iterationMap.has(maxIterationIndex) && maxIterationIndex > 0) {
    filtered.push(...iterationMap.get(maxIterationIndex)!)
  }

  // 无法解析迭代索引的节点（如模板节点），直接追加
  for (const child of children) {
    if (extractIterationIndex(child.id) === null) {
      filtered.push(child)
    }
  }

  const skippedIterations = originalIterations - (failedIterations.size > 0 ? failedIterations.size + 2 : 2)

  return { filtered, originalIterations, skippedIterations }
}
