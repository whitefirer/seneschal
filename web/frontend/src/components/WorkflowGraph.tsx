import { useState, useMemo, useCallback, useRef, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import {
  ReactFlow,
  Node,
  Edge,
  Position,
  Handle,
  getBezierPath,
  Controls,
  MiniMap,
  MarkerType,
  ControlButton,
  applyNodeChanges,
  NodeChange,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { useThemeStore } from '@/store/themeStore'
import {
  Globe,
  Terminal,
  MessageSquare,
  GitBranch,
  Repeat,
  Layers,
  Zap,
  Clock,
  CheckCircle,
  XCircle,
  AlertCircle,
  ChevronDown,
  ChevronUp,
} from 'lucide-react'

// 移除 ReactFlow 节点容器的阴影（让阴影只应用于节点自身，跟随节点大小变化）
const GLOBAL_STYLES = `
  .react-flow__node {
    box-shadow: none !important;
    background: transparent !important;
  }
  .react-flow__node-wrapper {
    box-shadow: none !important;
    background: transparent !important;
  }
`

// ReactFlow 原生锁定/解锁图标
function ReactFlowLockIcon() {
  return (
    <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 25 32" className="react-flow__controls-interactive-icon">
      <path d="M21.333 10.667H19.81V7.619C19.81 3.429 16.38 0 12.19 0 8 0 4.571 3.429 4.571 7.619v3.048H3.048A3.056 3.056 0 000 13.714v15.238A3.056 3.056 0 003.048 32h18.285a3.056 3.056 0 003.048-3.048V13.714a3.056 3.056 0 00-3.048-3.047zM12.19 24.533a3.056 3.056 0 01-3.047-3.047 3.056 3.056 0 013.047-3.048 3.056 3.056 0 013.048 3.048 3.056 3.056 0 01-3.048 3.047zm4.724-13.866H7.467V7.619c0-2.59 2.133-4.724 4.723-4.724 2.591 0 4.724 2.133 4.724 4.724v3.048z" fill="currentColor" />
    </svg>
  )
}

function ReactFlowUnlockIcon() {
  return (
    <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 25 32" className="react-flow__controls-interactive-icon">
      <path d="M21.333 10.667H19.81V7.619C19.81 3.429 16.38 0 12.19 0c-4.114 1.828-1.37 2.133.305 2.438 1.676.305 4.42 2.59 4.42 5.181v3.048H3.047A3.056 3.056 0 000 13.714v15.238A3.056 3.056 0 003.048 32h18.285a3.056 3.056 0 003.048-3.048V13.714a3.056 3.056 0 00-3.048-3.047zM12.19 24.533a3.056 3.056 0 01-3.047-3.047 3.056 3.056 0 013.047-3.048 3.056 3.056 0 013.048 3.048 3.056 3.056 0 01-3.048 3.047z" fill="currentColor" />
    </svg>
  )
}

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

interface NodeData {
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
  // condition 特有
  expression?: string
  conditionResult?: boolean | null
  // 分层渲染特有
  zoomLevel?: number
  totalNodeCount?: number
}

interface FlatFlowStep extends FlowStep {
  parentId?: string
  position: { x: number; y: number }
}

// 中空边组件
function HollowEdge({ id, sourceX, sourceY, targetX, targetY, sourcePosition, targetPosition, style }: {
  id: string
  sourceX: number
  sourceY: number
  targetX: number
  targetY: number
  sourcePosition: Position
  targetPosition: Position
  style?: { stroke?: string }
}) {
  const [edgePath] = getBezierPath({
    sourceX,
    sourceY,
    sourcePosition,
    targetX,
    targetY,
    targetPosition,
  })

  return (
    <>
      <path
        id={`${id}-outer`}
        className="hollow-edge-outer"
        d={edgePath}
        strokeWidth={4}
        stroke={style?.stroke || '#ec4899'}
        fill="none"
      />
      <path
        id={`${id}-inner`}
        className="hollow-edge-inner"
        d={edgePath}
        strokeWidth={2}
        fill="none"
      />
    </>
  )
}

// Action 样式
function getActionStyle(action: string) {
  const styles: Record<string, { icon: any; color: string; borderColor: string; bgColor: string }> = {
    http: { icon: Globe, color: 'text-blue-600 dark:text-blue-400', borderColor: 'border-blue-500', bgColor: 'bg-blue-50 dark:bg-blue-900/90' },
    shell: { icon: Terminal, color: 'text-green-600 dark:text-green-400', borderColor: 'border-green-500', bgColor: 'bg-green-50 dark:bg-green-900/90' },
    script: { icon: Terminal, color: 'text-purple-600 dark:text-purple-400', borderColor: 'border-purple-500', bgColor: 'bg-purple-50 dark:bg-purple-900/90' },
    log: { icon: MessageSquare, color: 'text-yellow-600 dark:text-yellow-400', borderColor: 'border-yellow-500', bgColor: 'bg-yellow-50 dark:bg-yellow-900/90' },
    condition: { icon: GitBranch, color: 'text-orange-600 dark:text-orange-400', borderColor: 'border-orange-500', bgColor: 'bg-orange-50 dark:bg-orange-900/90' },
    loop: { icon: Repeat, color: 'text-cyan-600 dark:text-cyan-400', borderColor: 'border-cyan-500', bgColor: 'bg-cyan-50 dark:bg-cyan-900/90' },
    foreach: { icon: Repeat, color: 'text-cyan-600 dark:text-cyan-400', borderColor: 'border-cyan-500', bgColor: 'bg-cyan-50 dark:bg-cyan-900/90' },
    parallel: { icon: Zap, color: 'text-pink-600 dark:text-pink-400', borderColor: 'border-pink-500', bgColor: 'bg-pink-50 dark:bg-pink-900/90' },
    sleep: { icon: Clock, color: 'text-gray-600 dark:text-gray-400', borderColor: 'border-gray-500', bgColor: 'bg-gray-50 dark:bg-gray-900/90' },
  }
  return styles[action] || styles['log']
}

function getStatusConfig(status?: string) {
  const configs: Record<string, { icon: any; color: string }> = {
    success: { icon: CheckCircle, color: 'text-green-500' },
    running: { icon: Clock, color: 'text-blue-500 animate-spin' },
    failed: { icon: XCircle, color: 'text-red-500' },
    skipped: { icon: AlertCircle, color: 'text-yellow-500' },
    pending: { icon: Clock, color: 'text-gray-400' },
  }
  return configs[status || 'pending'] || configs['pending']
}

function getActionLabel(action: string): string {
  const labels: Record<string, string> = {
    http: 'HTTP',
    shell: 'Shell',
    script: 'Script',
    log: 'Log',
    condition: 'Condition',
    loop: 'Loop',
    foreach: 'Foreach',
    parallel: 'Parallel',
    sleep: 'Sleep',
  }
  return labels[action] || action
}

// Layout 常数
const NODE_WIDTH = 240
const NODE_HEIGHT = 100
const H_SPACING = 60
const V_SPACING = 50  // 垂直间距
const PARENT_CHILD_GAP = 60
const FOREACH_PADDING = 30 // foreach 容器内边距
const PARALLEL_PADDING = 30 // parallel 容器内边距
const FOREACH_ITERATION_THRESHOLD = 4 // foreach 迭代次数阈值，超过则聚合显示
const PARALLEL_TASK_THRESHOLD = 5 // parallel 子节点数量阈值，超过则默认折叠

// 分层渲染（Level of Detail）配置 - 动态阈值
const LOD_CONFIG = {
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
enum DetailLevel {
  HIDE = 0,         // 隐藏（只渲染一个点）
  SIMPLIFIED = 1,   // 简化（色块 + 状态）
  NORMAL = 2,       // 标准（名称 + 类型 + 状态）
  DETAILED = 3      // 详细（所有信息 + 日志预览）
}

// 根据节点数量和缩放级别计算详细程度（动态阈值）
function calculateDetailLevel(nodeCount: number, zoom: number): DetailLevel {
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
function extractIterationIndex(nodeId: string): number | null {
  // ID 格式: "xxx-iter-0", "xxx-item-0-step-0", "step-xxx-iter-0"
  const iterMatch = nodeId.match(/-iter-(\d+)/)
  if (iterMatch) return parseInt(iterMatch[1], 10)
  
  const itemMatch = nodeId.match(/-item-(\d+)-step-/)
  if (itemMatch) return parseInt(itemMatch[1], 10)
  
  return null
}

// 过滤 foreach 子节点：按迭代分组，只保留首、尾、失败的迭代
function filterForeachChildren(children: FlowStep[], itemsCount?: number): { filtered: FlowStep[], originalIterations: number, skippedIterations: number } {
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

// 递归布局 condition 分支的子节点
/* [UNUSED] 
interface BranchLayoutResult {
  nodes: FlatFlowStep[]
  maxX: number
  maxY: number
}

function layoutBranch(
  children: FlowStep[],
  startX: number,
  startY: number,
  branchType: 'then' | 'else',
  parentId: string,
  forceElseHorizontal: boolean = false
): BranchLayoutResult {
  const nodes: FlatFlowStep[] = []
  let currentX = startX
  let currentY = startY
  let branchMaxY = startY
  
  for (const child of children) {
    const isCollapsed = child.collapsed
    const childId = child.id || `${parentId}-${branchType}-${nodes.length}`
    nodes.push({ ...child, id: childId, parentId, position: { x: currentX, y: currentY } })
    let shouldAdvance = true
    if (child.action === 'condition') {
      const thenChildren = child.then_children || child.then || []
      const elseChildren = child.else_children || child.else || []
      if (thenChildren.length > 0 || elseChildren.length > 0) {
        const thenExecuted = child.condition_result === true
        const elseExecuted = child.condition_result === false
        shouldAdvance = false
        const childStartX = currentX + NODE_WIDTH + H_SPACING
        if (isCollapsed) {
          const childrenToLayout = thenExecuted ? thenChildren : (elseExecuted ? elseChildren : [])
          if (childrenToLayout.length > 0) {
            const result = layoutBranch(childrenToLayout, childStartX, currentY, 'then', childId, forceElseHorizontal)
            const nodesWithoutParent = result.nodes.map(n => ({ ...n, parentId: undefined }))
            nodes.push(...nodesWithoutParent)
            currentX = result.maxX
            branchMaxY = Math.max(branchMaxY, result.maxY)
          } else {
            currentX = childStartX
          }
        } else {
          const thenResult = layoutBranch(thenChildren, childStartX, currentY, 'then', childId, forceElseHorizontal)
          nodes.push(...thenResult.nodes)
          const elseStartY = forceElseHorizontal ? currentY : thenResult.maxY + V_SPACING
          const elseResult = layoutBranch(elseChildren, childStartX, elseStartY, 'then', childId, forceElseHorizontal)
          nodes.push(...elseResult.nodes)
          currentX = Math.max(thenResult.maxX, elseResult.maxX)
          branchMaxY = Math.max(branchMaxY, elseResult.maxY)
        }
      }
    }
    if (shouldAdvance) {
      currentX += NODE_WIDTH + H_SPACING
      branchMaxY = Math.max(branchMaxY, currentY + NODE_HEIGHT)
    }
  }
  return { nodes, maxX: currentX, maxY: branchMaxY }
}
*/

// 计算布局 - 改进版，支持 foreach/parallel 容器，递归处理 condition 嵌套
// DAG 布局：按层级排列节点
// 计算 DAG 布局
// isParallelContainer: 是否为并行容器的直接子节点（并行子节点之间不搞隐式依赖）
function calculateDAGLayout(steps: FlowStep[], startX: number, startY: number, isParallelContainer: boolean = false): FlatFlowStep[] {
  // 构建节点 ID 到步骤的映射
  // 建立 ID 和 Name 的双重映射，兼容后端数据（next/depends_on 存的是 name）
  const stepMap = new Map<string, FlowStep>()
  const nameToIdMap = new Map<string, string>()
  steps.forEach(s => {
    stepMap.set(s.id, s)
    if (s.name) nameToIdMap.set(s.name, s.id)
  })

  const resolveRef = (ref: string): string | null => {
    if (stepMap.has(ref)) return ref
    if (nameToIdMap.has(ref)) return nameToIdMap.get(ref)!
    if (stepMap.has(`step-${ref}`)) return `step-${ref}`
    return null
  }

  // 构建依赖图：记录每个节点的前驱和后继
  const predecessors = new Map<string, Set<string>>()
  const successors = new Map<string, Set<string>>()
  let explicitEdgeCount = 0

  steps.forEach(s => {
    predecessors.set(s.id, new Set())
    successors.set(s.id, new Set())
  })

  // 1. 处理 depends_on (反向依赖)
  steps.forEach(s => {
    if (s.depends_on && s.depends_on.length > 0) {
      s.depends_on.forEach(depId => {
        const targetId = resolveRef(depId)
        if (targetId) {
          successors.get(targetId)?.add(s.id)
          predecessors.get(s.id)?.add(targetId)
          explicitEdgeCount++
        } else {
          console.warn(`[DAGLayout] Step '${s.id}' depends_on '${depId}', but target not found!`)
        }
      })
    }
  })

  // 2. 处理 next (正向依赖)
  steps.forEach(s => {
    if (s.next && s.next.length > 0) {
      s.next.forEach(nextId => {
        const targetId = resolveRef(nextId)
        if (targetId) {
          successors.get(s.id)?.add(targetId)
          predecessors.get(targetId)?.add(s.id)
          explicitEdgeCount++
        } else {
          console.warn(`[DAGLayout] Step '${s.id}' next is '${nextId}', but target not found!`)
        }
      })
    }
  })


  // 3. 隐式依赖回退 (仅限非并行容器)
  // 修改策略：不再依赖全局 explicitEdgeCount，而是为每个无 depends_on 的节点（且非首个）添加前驱依赖
  // 这保证了线性流程（包括 Condition 后、Parallel 后）的节点不会重叠
  if (!isParallelContainer && steps.length > 1) {
    for (let i = 1; i < steps.length; i++) {
      const curr = steps[i]
      const prev = steps[i - 1]
      // 如果当前节点没有显式依赖，且不是并行容器子节点，则依赖前一个节点
      if (!curr.depends_on || curr.depends_on.length === 0) {
        successors.get(prev.id)?.add(curr.id)
        predecessors.get(curr.id)?.add(prev.id)
      }
    }
  }

  // 计算层级：使用拓扑排序 (BFS)
  const levels = new Map<string, number>()
  const inDegree = new Map<string, number>()
  
  // 计算入度
  steps.forEach(s => {
    inDegree.set(s.id, predecessors.get(s.id)?.size || 0)
  })

  // 初始队列：入度为 0 的节点
  const queue: string[] = []
  steps.forEach(s => {
    if (inDegree.get(s.id) === 0) {
      queue.push(s.id)
    }
  })

  // BFS
  while (queue.length > 0) {
    // 注意：这里我们简单地使用队列长度来控制层级不太准确，应该记录层级
    // 重新设计：BFS 时携带层级
  }
  
  // 重新计算层级
  const computeLevel = (nodeId: string, visited: Set<string>): number => {
    if (levels.has(nodeId)) return levels.get(nodeId)!
    
    const preds = predecessors.get(nodeId) || new Set()
    if (preds.size === 0) {
      levels.set(nodeId, 0)
      return 0
    }
    
    let maxPredLevel = -1
    for (const predId of preds) {
      const predLevel = computeLevel(predId, visited)
      maxPredLevel = Math.max(maxPredLevel, predLevel)
    }
    
    const currentLevel = maxPredLevel + 1
    levels.set(nodeId, currentLevel)
    return currentLevel
  }

  const visited = new Set<string>()
  steps.forEach(s => computeLevel(s.id, visited))


  // 按层级分组
  const levelGroups = new Map<number, FlowStep[]>()
  steps.forEach(s => {
    const level = levels.get(s.id) || 0
    if (!levelGroups.has(level)) levelGroups.set(level, [])
    levelGroups.get(level)?.push(s)
  })

  // 生成节点位置
  const nodes: FlatFlowStep[] = []
  const maxLevel = Math.max(...Array.from(levels.values()))

  for (let level = 0; level <= maxLevel; level++) {
    const group = levelGroups.get(level) || []
    const levelX = startX + level * (NODE_WIDTH + H_SPACING)
    const groupHeight = group.length * (NODE_HEIGHT + V_SPACING) - V_SPACING
    // 垂直居中
    const levelStartY = startY + (group.length > 1 ? -groupHeight / 2 + NODE_HEIGHT / 2 : 0)

    group.forEach((step, idx) => {
      nodes.push({
        ...step,
        parentId: undefined,
        position: {
          x: levelX,
          y: levelStartY + idx * (NODE_HEIGHT + V_SPACING)
        }
      })
    })
  }

  return nodes
}

/* [UNUSED]
function calculateContainerDAGLayout(steps: FlowStep[], startX: number, startY: number, parentId: string, action: string): FlatFlowStep[] {
  const isParallel = action === 'parallel'
  const dagNodes = calculateDAGLayout(steps, startX, startY, isParallel)
  return dagNodes.map(n => ({ ...n, parentId }))
}
*/

function calculateLayout(
  steps: FlowStep[],
  startX: number,
  startY: number
): { nodes: FlatFlowStep[], nextX: number, maxY: number, foreachGroups: Map<string, any>, parallelGroups: Map<string, any> } {
  // 1. 使用 DAG 布局计算当前层级节点
  const dagNodes = calculateDAGLayout(steps, startX, startY)
  
  const allNodes: FlatFlowStep[] = [...dagNodes]
  const foreachGroups = new Map<string, any>()
  const parallelGroups = new Map<string, any>()
  let globalMaxX = startX
  let globalMaxY = startY

  // 2. 处理容器节点的子节点
  for (const node of dagNodes) {
    const step = steps.find(s => s.id === node.id)
    if (!step) continue

    // 更新边界
    globalMaxX = Math.max(globalMaxX, node.position.x + NODE_WIDTH)
    globalMaxY = Math.max(globalMaxY, node.position.y + NODE_HEIGHT)

    if (step.collapsed) {
      // 折叠状态处理
      if (step.action === 'condition') {
        const thenChildren = step.then_children || step.then || []
        const elseChildren = step.else_children || step.else || []
        const conditionResult = step.condition_result
        const thenExecuted = conditionResult === true
        const elseExecuted = conditionResult === false
        
        const childrenToLayout = thenExecuted ? thenChildren : (elseExecuted ? elseChildren : [])
        
        if (childrenToLayout.length > 0) {
          const childStartX = node.position.x + NODE_WIDTH + H_SPACING
          const childResult = calculateLayout(childrenToLayout, childStartX, node.position.y)
          // 折叠时子节点无 parentId
          allNodes.push(...childResult.nodes.map(n => ({ ...n, parentId: undefined })))
          globalMaxX = Math.max(globalMaxX, childResult.nextX)
          globalMaxY = Math.max(globalMaxY, childResult.maxY)
        }
      }
      // 其他容器折叠时不显示子节点
      continue
    }

    // 展开状态处理
    if (step.action === 'parallel' && step.children?.length) {
      const childrenStartX = node.position.x
      const childrenStartY = node.position.y + NODE_HEIGHT + PARENT_CHILD_GAP
      
      // Parallel 子节点应垂直排列，不走 DAG 算法（与 Foreach 一致）
      let currentY = childrenStartY
      const childNodes: FlatFlowStep[] = []
      
      step.children.forEach(child => {
        childNodes.push({
          ...child,
          parentId: node.id,
          position: { x: childrenStartX, y: currentY },
        })
        currentY += NODE_HEIGHT + V_SPACING
      })
      
      let childMaxX = childrenStartX + NODE_WIDTH
      let childMaxY = currentY - V_SPACING
      
      childNodes.forEach(cn => {
        cn.parentId = node.id
        allNodes.push(cn)
        childMaxX = Math.max(childMaxX, cn.position.x + NODE_WIDTH)
        childMaxY = Math.max(childMaxY, cn.position.y + NODE_HEIGHT)
      })
      
      parallelGroups.set(node.id, {
        x: childrenStartX - PARALLEL_PADDING,
        y: childrenStartY - PARALLEL_PADDING,
        width: NODE_WIDTH + PARALLEL_PADDING * 2,
        height: childMaxY - childrenStartY + PARALLEL_PADDING * 2
      })
      globalMaxX = Math.max(globalMaxX, childMaxX)
      globalMaxY = Math.max(globalMaxY, childMaxY)
    } 
    else if ((step.action === 'foreach' || step.action === 'loop') && step.children?.length) {
      const { filtered, originalIterations, skippedIterations } = filterForeachChildren(step.children, step.items?.length)
      if (filtered.length > 0) {
        // DEBUG: 检查 Foreach 子节点是否包含不该有的节点（如 complete）
        
        const childrenStartX = node.position.x
        const childrenStartY = node.position.y + NODE_HEIGHT + PARENT_CHILD_GAP
        
        // Foreach 子节点应垂直排列，不走 DAG 算法
        let currentY = childrenStartY
        const childNodes: FlatFlowStep[] = []
        
        filtered.forEach((child, idx) => {
          childNodes.push({
            ...child,
            parentId: node.id,
            position: { x: childrenStartX, y: currentY },
            ...(idx === 0 ? { _originalChildrenCount: originalIterations, _skippedCount: skippedIterations } : {})
          })
          currentY += NODE_HEIGHT + V_SPACING
        })
        
        let childMaxX = childrenStartX + NODE_WIDTH
        let childMaxY = currentY - V_SPACING
        
        allNodes.push(...childNodes)
        
        foreachGroups.set(node.id, {
          x: childrenStartX - FOREACH_PADDING,
          y: childrenStartY - FOREACH_PADDING,
          width: NODE_WIDTH + FOREACH_PADDING * 2,
          height: childMaxY - childrenStartY + FOREACH_PADDING * 2,
          skippedIterations,
          originalIterations
        })
        globalMaxX = Math.max(globalMaxX, childMaxX)
        globalMaxY = Math.max(globalMaxY, childMaxY)
      }
    } 
    else if (step.action === 'condition') {
      const thenChildren = step.then_children || step.then || []
      const elseChildren = step.else_children || step.else || []
      
      if (thenChildren.length > 0 || elseChildren.length > 0) {
        const childStartX = node.position.x + NODE_WIDTH + H_SPACING
        
        // Then 分支
        let thenMaxY = node.position.y
        let thenNextX = childStartX
        if (thenChildren.length > 0) {
          const thenResult = calculateLayout(thenChildren, childStartX, node.position.y)
          allNodes.push(...thenResult.nodes.map(n => ({ ...n, parentId: step.id })))
          thenMaxY = thenResult.maxY
          thenNextX = thenResult.nextX
          globalMaxX = Math.max(globalMaxX, thenNextX)
          globalMaxY = Math.max(globalMaxY, thenResult.maxY)
        }
        
        // Else 分支
        if (elseChildren.length > 0) {
          // Else 放在 Then 下方
          const elseStartY = thenChildren.length > 0 ? thenMaxY + V_SPACING : node.position.y
          const elseResult = calculateLayout(elseChildren, childStartX, elseStartY)
          allNodes.push(...elseResult.nodes.map(n => ({ ...n, parentId: step.id })))
          globalMaxX = Math.max(globalMaxX, elseResult.nextX)
          globalMaxY = Math.max(globalMaxY, elseResult.maxY)
        }

        // 关键修复：Condition 子节点撑开了宽度，需要把后面的兄弟节点（dagNodes 中 node 之后的节点）向右推
        // 计算需要推移的距离：Condition 块的最右端 - (Condition 节点本身的最右端)
        const conditionBlockEndX = Math.max(thenNextX, globalMaxX) // 取 Then 或 Else 的最右端
        const nodeEndX = node.position.x + NODE_WIDTH
        if (conditionBlockEndX > nodeEndX + H_SPACING) {
          const offset = conditionBlockEndX - nodeEndX
          // 在 dagNodes 中找到当前 node 的索引
          const nodeIndex = dagNodes.findIndex(n => n.id === node.id)
          if (nodeIndex !== -1) {
            // 推移后续所有节点
            for (let i = nodeIndex + 1; i < dagNodes.length; i++) {
              dagNodes[i].position.x += offset
            }
          }
        }
      }
    }
  }

  // 去重
  const nodeMap = new Map<string, FlatFlowStep>()
  for (const n of allNodes) {
    nodeMap.set(n.id, n)
  }

  return {
    nodes: Array.from(nodeMap.values()),
    nextX: globalMaxX + H_SPACING,
    maxY: globalMaxY,
    foreachGroups,
    parallelGroups
  }
}

// 递归构建 condition 分支连线（支持任意层级嵌套）
function buildBranchEdges(
  children: FlowStep[],
  parentId: string,
  branchType: 'then' | 'else',
  branchExecuted: boolean,
  isDark: boolean,
  edges: any[]
): void {
  for (let i = 0; i < children.length; i++) {
    const child = children[i]
    const childId = child.id || `${parentId}-${branchType}-${i}`
    const isCollapsed = child.collapsed
    
    // 链式连线（到下一个兄弟节点）- 所有分支都水平连线
    if (i < children.length - 1) {
      const nextChild = children[i + 1]
      const nextChildId = nextChild.id || `${parentId}-${branchType}-${i + 1}`
      edges.push({
        id: `edge-${childId}-${nextChildId}`,
        source: childId,
        sourceHandle: 'right',
        target: nextChildId,
        targetHandle: 'left',
        type: 'default',
        style: { 
          stroke: branchExecuted ? '#22c55e' : '#d1d5db', 
          strokeWidth: 2,
          strokeDasharray: branchExecuted ? undefined : '4,4'
        },
        markerEnd: { type: MarkerType.ArrowClosed, color: branchExecuted ? '#22c55e' : '#d1d5db' },
      })
    }
    
    // 如果是 condition 且未折叠，递归处理其子节点连线
    if (child.action === 'condition' && !isCollapsed) {
      const nestedThenChildrenRaw = child.then_children || child.then || []
      const nestedElseChildrenRaw = child.else_children || child.else || []
      
      if (nestedThenChildrenRaw.length > 0 || nestedElseChildrenRaw.length > 0) {
        const nestedConditionResult = child.condition_result
        const nestedThenExecuted = nestedConditionResult === true
        const nestedElseExecuted = nestedConditionResult === false
        
        // 根据嵌套 condition 的折叠状态过滤子节点
        const nestedIsCollapsed = child.collapsed
        const nestedThenChildren = nestedIsCollapsed ? (nestedThenExecuted ? nestedThenChildrenRaw : []) : nestedThenChildrenRaw
        const nestedElseChildren = nestedIsCollapsed ? (nestedElseExecuted ? nestedElseChildrenRaw : []) : nestedElseChildrenRaw
        
        // condition → 第一个 then 子节点
        if (nestedThenChildren.length > 0) {
          const firstNestedThen = nestedThenChildren[0]
          const firstNestedThenId = firstNestedThen.id || `${childId}-then-0`
          edges.push({
            id: `edge-${childId}-${firstNestedThenId}`,
            source: childId,
            sourceHandle: 'right',
            target: firstNestedThenId,
            targetHandle: 'left',
            type: 'default',
            style: { 
              stroke: nestedThenExecuted ? '#22c55e' : '#d1d5db', 
              strokeWidth: 2,
              strokeDasharray: nestedThenExecuted ? undefined : '4,4'
            },
            markerEnd: { type: MarkerType.ArrowClosed, color: nestedThenExecuted ? '#22c55e' : '#d1d5db' },
            label: 'true',
            labelStyle: { fill: nestedThenExecuted ? '#22c55e' : '#9ca3af', fontSize: 11, fontWeight: 'bold' },
            labelBgStyle: { fill: isDark ? '#1f2937' : 'white', fillOpacity: 0.9 },
          })
          
          // 递归处理 then 分支连线
          buildBranchEdges(nestedThenChildren, childId, 'then', nestedThenExecuted, isDark, edges)
        }
        
        // condition → 第一个 else 子节点
        if (nestedElseChildren.length > 0) {
          const firstNestedElse = nestedElseChildren[0]
          const firstNestedElseId = firstNestedElse.id || `${childId}-else-0`
          edges.push({
            id: `edge-${childId}-${firstNestedElseId}`,
            source: childId,
            sourceHandle: 'bottom',
            target: firstNestedElseId,
            targetHandle: 'left',
            type: 'default',
            style: { 
              stroke: nestedElseExecuted ? '#22c55e' : '#d1d5db', 
              strokeWidth: 2,
              strokeDasharray: nestedElseExecuted ? undefined : '4,4'
            },
            markerEnd: { type: MarkerType.ArrowClosed, color: nestedElseExecuted ? '#22c55e' : '#d1d5db' },
            label: 'false',
            labelStyle: { fill: nestedElseExecuted ? '#22c55e' : '#9ca3af', fontSize: 11, fontWeight: 'bold' },
            labelBgStyle: { fill: isDark ? '#1f2937' : 'white', fillOpacity: 0.9 },
          })
          
          // 递归处理 else 分支连线
          buildBranchEdges(nestedElseChildren, childId, 'else', nestedElseExecuted, isDark, edges)
        }
      }
    }
  }
}

// 构建连线
function buildEdges(steps: FlowStep[], isDark: boolean): any[] {
  const edges: any[] = []
  
  // 建立 ID 和 Name 的双重映射，兼容后端数据
  const stepMap = new Map<string, FlowStep>()
  const nameToIdMap = new Map<string, string>()
  steps.forEach(s => {
    stepMap.set(s.id, s)
    if (s.name) nameToIdMap.set(s.name, s.id)
  })
  const resolveRef = (ref: string): string => {
    if (stepMap.has(ref)) return ref
    if (nameToIdMap.has(ref)) return nameToIdMap.get(ref)!
    if (stepMap.has(`step-${ref}`)) return `step-${ref}`
    return ref // fallback to original
  }

  // 检测是否是 DAG 模式
  const isDAG = steps.some(s => s.next && s.next.length > 0)

  if (isDAG) {
    for (const step of steps) {
      if (step.next && step.next.length > 0) {
        for (const nextRef of step.next) {
          const targetId = resolveRef(nextRef)
          edges.push({
            id: `edge-${step.id}-${targetId}`,
            source: step.id,
            target: targetId,
            type: 'default',
            style: { stroke: '#9ca3af', strokeWidth: 2 },
            markerEnd: { type: MarkerType.ArrowClosed, color: '#9ca3af' },
          })
        }
      }
    }
    return edges
  }

  // 线性模式：原有逻辑
  const connectSteps = (stepList: FlowStep[], parent?: FlowStep) => {
    for (let i = 0; i < stepList.length; i++) {
      const current = stepList[i]
      const next = stepList[i + 1]
      const isCollapsed = current.collapsed

      // Parallel: 扇出扇入连线
      if (current.action === 'parallel' && current.children?.length) {
        // 父节点连接到当前 parallel 节点（如果有父节点且是第一个子节点）
        if (parent && i === 0) {
          edges.push({
            id: `edge-${parent.id}-${current.id}`,
            source: parent.id,
            target: current.id,
            type: 'default',
            style: { stroke: '#9ca3af', strokeWidth: 2 },
            markerEnd: { type: MarkerType.ArrowClosed, color: '#9ca3af' },
          })
        }
        
        if (!isCollapsed) {
          // 展开：扇出扇入
          // 扇出：父节点 → 所有子节点（根据子节点状态决定是否流动）
          for (let j = 0; j < current.children.length; j++) {
            const child = current.children[j]
            const isRunning = child.status === 'running'
            
            edges.push({
              id: `edge-${current.id}-${child.id}`,
              source: current.id,
              sourceHandle: 'bottom',
              target: child.id,
              targetHandle: 'top',
              type: 'hollow',
              animated: isRunning,
              style: { stroke: '#a855f7' },  // 紫色
            })
          }
          // 扇入：所有子节点 → 下一节点（根据下一节点状态决定是否流动）
          if (next) {
            const nextIsRunning = next.status === 'running'
            
            for (let j = 0; j < current.children.length; j++) {
              edges.push({
                id: `edge-${current.children[j].id}-${next.id}`,
                source: current.children[j].id,
                sourceHandle: Position.Right,  // 从右侧发出
                target: next.id,
                type: 'default',  // 普通线
                animated: nextIsRunning,
                style: { stroke: '#9ca3af', strokeWidth: 2 },
                markerEnd: { type: MarkerType.ArrowClosed, color: '#9ca3af' },
              })
            }
          }
        } else {
          // 折叠时：智能分组显示（前 2 个子节点 + 省略标记）
          const displayCount = Math.min(2, current.children.length)
          
          // 显示前 N 个子节点
          for (let j = 0; j < displayCount; j++) {
            const child = current.children[j]
            const isRunning = child.status === 'running'
            
            // 父节点 → 子节点
            edges.push({
              id: `edge-${current.id}-${child.id}-collapsed`,
              source: current.id,
              sourceHandle: 'bottom',
              target: child.id,
              targetHandle: 'top',
              type: 'hollow',
              animated: isRunning,
              style: { stroke: '#a855f7', strokeDasharray: '4 4' },  // 虚线表示折叠
            })
            
            // 子节点 → 下一节点
            if (next) {
              const nextIsRunning = next.status === 'running'
              
              edges.push({
                id: `edge-${child.id}-${next.id}-collapsed`,
                source: child.id,
                sourceHandle: Position.Right,
                target: next.id,
                type: 'default',
                animated: nextIsRunning,
                style: { stroke: '#9ca3af', strokeWidth: 2, strokeDasharray: '4 4' },
                markerEnd: { type: MarkerType.ArrowClosed, color: '#9ca3af' },
              })
            }
          }
          
          // 如果有省略的子节点，添加省略标记连线
          if (current.children.length > displayCount && next) {
            edges.push({
              id: `edge-${current.id}-${next.id}-omitted`,
              source: current.id,
              sourceHandle: 'right',
              target: next.id,
              type: 'default',
              label: '······',  // 省略标记
              labelStyle: { fill: '#9ca3af', fontWeight: 500, fontSize: '12px' },
              style: { stroke: '#9ca3af', strokeWidth: 2, strokeDasharray: '4 4' },
              markerEnd: { type: MarkerType.ArrowClosed, color: '#9ca3af' },
            })
          } else if (next) {
            // 没有省略，直接连接
            const nextIsRunning = next.status === 'running'
            
            edges.push({
              id: `edge-${current.id}-${next.id}`,
              source: current.id,
              sourceHandle: 'right',
              target: next.id,
              type: 'default',
              animated: nextIsRunning,
              style: { stroke: '#9ca3af', strokeWidth: 2 },
              markerEnd: { type: MarkerType.ArrowClosed, color: '#9ca3af' },
            })
          }
        }
        continue
      }

      // Foreach: 链式连接，最后子节点连接到下一个步骤
      if ((current.action === 'foreach' || current.action === 'loop') && current.children?.length) {
        // 父节点连接到当前 foreach 节点（如果有父节点且是第一个子节点）
        if (parent && i === 0) {
          edges.push({
            id: `edge-${parent.id}-${current.id}`,
            source: parent.id,
            target: current.id,
            type: 'default',
            style: { stroke: '#9ca3af', strokeWidth: 2 },
            markerEnd: { type: MarkerType.ArrowClosed, color: '#9ca3af' },
          })
        }
        
        if (!isCollapsed) {
          // 过滤子节点，与布局保持一致
          const { filtered, skippedIterations } = filterForeachChildren(current.children, current.items?.length)
          
          if (filtered.length === 0) continue
          
          // 展开时：父节点连接到第一个子节点（入口）
          const firstChild = filtered[0]
          edges.push({
            id: `edge-${current.id}-${firstChild.id}`,
            source: current.id,
            sourceHandle: 'bottom',
            target: firstChild.id,
            targetHandle: 'top',
            type: 'default',
            style: { stroke: '#06b6d4', strokeWidth: 2, strokeDasharray: '4,4' },
            markerEnd: { type: MarkerType.ArrowClosed, color: '#06b6d4' },
          })
          
          // 子节点链式连接（顺序执行）
          // 检测是否跨迭代，在跨迭代的连线上添加省略标记
          for (let j = 0; j < filtered.length - 1; j++) {
            const currentIter = extractIterationIndex(filtered[j].id)
            const nextIter = extractIterationIndex(filtered[j + 1].id)
            
            const edge: Edge = {
              id: `edge-${filtered[j].id}-${filtered[j + 1].id}`,
              source: filtered[j].id,
              sourceHandle: 'bottom',
              target: filtered[j + 1].id,
              targetHandle: 'top',
              type: 'default',
              style: { stroke: '#06b6d4', strokeWidth: 2, strokeDasharray: '4,4' },
              markerEnd: { type: MarkerType.ArrowClosed, color: '#06b6d4' },
            }
            
            // 如果跨迭代（当前迭代 != 下一个迭代），且有省略的迭代，添加省略标记
            // 标记添加在第一次迭代结束后的第一条跨迭代连线
            if (skippedIterations > 0 && currentIter !== null && nextIter !== null && currentIter !== nextIter) {
              edge.label = '······'
              edge.labelStyle = { fill: '#9ca3af', fontWeight: 'bold', fontSize: 12 }
              edge.labelBgStyle = { fill: isDark ? '#1f2937' : '#ffffff', fillOpacity: 0.9 }
              edge.labelBgPadding = [8, 4] as [number, number]
              edge.labelBgBorderRadius = 4
            }
            
            edges.push(edge)
          }
          
          // 最后子节点连接到下一个步骤（出口）
          if (next) {
            const lastChild = filtered[filtered.length - 1]
            edges.push({
              id: `edge-${lastChild.id}-${next.id}`,
              source: lastChild.id,
              sourceHandle: Position.Right,
              target: next.id,
              type: 'default',
              style: { stroke: '#9ca3af', strokeWidth: 2 },
              markerEnd: { type: MarkerType.ArrowClosed, color: '#9ca3af' },
            })
          }
        } else {
          // 折叠时：foreach 父节点直接连到下一个节点
          if (next) {
            edges.push({
              id: `edge-${current.id}-${next.id}`,
              source: current.id,
              sourceHandle: 'right',
              target: next.id,
              targetHandle: 'left',
              type: 'default',
              style: { stroke: '#9ca3af', strokeWidth: 2 },
              markerEnd: { type: MarkerType.ArrowClosed, color: '#9ca3af' },
            })
          }
        }
        continue
      }

      // Condition: Y型分叉连线（从右侧连到左侧）
      if (current.action === 'condition') {
        // 兼容 then_children 和 then 属性
        const thenChildren = current.then_children || current.then || []
        const elseChildren = current.else_children || current.else || []
        const conditionResult = current.condition_result
        const isCollapsed = current.collapsed
        
        // 确定 condition 分支结束后的目标节点
        // 优先使用显式指定的 next，否则使用 steps 中的下一节点
        let branchTarget: FlowStep | undefined = next
        if (current.next && current.next.length > 0) {
          // 找到指定的目标节点
          const targetId = current.next[0]
          const targetStep = steps.find(s => s.id === targetId || s.name === targetId)
          if (targetStep) {
            branchTarget = targetStep
          }
        }

        // 父节点连接到当前 condition 节点
        if (parent && i === 0) {
          edges.push({
            id: `edge-${parent.id}-${current.id}`,
            source: parent.id,
            sourceHandle: 'right',
            target: current.id,
            targetHandle: 'left',
            type: 'default',
            style: { stroke: '#9ca3af', strokeWidth: 2 },
            markerEnd: { type: MarkerType.ArrowClosed, color: '#9ca3af' },
          })
        }

        // 折叠状态：显示执行分支的节点，隐藏未执行分支
        // 展开状态：显示所有分支节点
        const thenExecuted = conditionResult === true
        const elseExecuted = conditionResult === false
        
        let visibleThenChildren = isCollapsed ? (thenExecuted ? thenChildren : []) : thenChildren
        let visibleElseChildren = isCollapsed ? (elseExecuted ? elseChildren : []) : elseChildren

        // 折叠时：如果有执行分支节点，连线到执行分支，而不是直接跳过
        // 注意：布局函数 layoutCollapsedBranch 已经将执行分支节点放在同一水平线
        
        // then 分支连线（执行分支或展开状态）
        if (visibleThenChildren.length > 0) {
          const firstThen = visibleThenChildren[0]
          // condition → then 子节点：label "true"
          edges.push({
            id: `edge-${current.id}-${firstThen.id || `${current.id}-then-0`}`,
            source: current.id,
            sourceHandle: 'right',
            target: firstThen.id || `${current.id}-then-0`,
            targetHandle: 'left',
            type: 'default',
            style: { 
              stroke: thenExecuted ? '#22c55e' : '#d1d5db', 
              strokeWidth: 2,
              strokeDasharray: thenExecuted ? undefined : '4,4'
            },
            markerEnd: { type: MarkerType.ArrowClosed, color: thenExecuted ? '#22c55e' : '#d1d5db' },
            label: 'true',
            labelStyle: { fill: thenExecuted ? '#22c55e' : '#9ca3af', fontSize: 11, fontWeight: 'bold' },
            labelBgStyle: { fill: isDark ? '#1f2937' : 'white', fillOpacity: 0.9 },
          })
          // then 子节点链式连接（水平排布）
          for (let j = 0; j < visibleThenChildren.length - 1; j++) {
            edges.push({
              id: `edge-${visibleThenChildren[j].id || `${current.id}-then-${j}`}-${visibleThenChildren[j + 1].id || `${current.id}-then-${j + 1}`}`,
              source: visibleThenChildren[j].id || `${current.id}-then-${j}`,
              sourceHandle: 'right',
              target: visibleThenChildren[j + 1].id || `${current.id}-then-${j + 1}`,
              targetHandle: 'left',
              type: 'default',
              style: { stroke: thenExecuted ? '#22c55e' : '#d1d5db', strokeWidth: 2, strokeDasharray: thenExecuted ? undefined : '4,4' },
              markerEnd: { type: MarkerType.ArrowClosed, color: thenExecuted ? '#22c55e' : '#d1d5db' },
            })
          }
          
          // 递归处理 then 子节点内部的连线（使用递归函数处理任意层级嵌套）
          for (const child of visibleThenChildren) {
            // 父级折叠时，嵌套 condition 正常展开，不跟随父级折叠
            // 所以这里不检查 child.collapsed
            if (child.action === 'condition' && ((child.then_children || child.then)?.length || (child.else_children || child.else)?.length)) {
              // 获取嵌套 condition 的执行结果
              const nestedConditionResult = child.condition_result
              const nestedThenExecuted = nestedConditionResult === true
              const nestedElseExecuted = nestedConditionResult === false
              
              // 嵌套 condition 正常展开，不跟随父级折叠
              // 只检查嵌套 condition 自身的折叠状态
              const nestedThenChildrenRaw = child.then_children || child.then || []
              const nestedElseChildrenRaw = child.else_children || child.else || []
              // 嵌套 condition 的连线：只看其自身折叠状态，不看父级
              const nestedThenChildren = child.collapsed ? (nestedThenExecuted ? nestedThenChildrenRaw : []) : nestedThenChildrenRaw
              const nestedElseChildren = child.collapsed ? (nestedElseExecuted ? nestedElseChildrenRaw : []) : nestedElseChildrenRaw
              
              // condition → 第一个 then 子节点
              if (nestedThenChildren.length > 0) {
                const firstNestedThen = nestedThenChildren[0]
                const firstNestedThenId = firstNestedThen.id || `${child.id}-then-0`
                edges.push({
                  id: `edge-${child.id}-${firstNestedThenId}`,
                  source: child.id,
                  sourceHandle: 'right',
                  target: firstNestedThenId,
                  targetHandle: 'left',
                  type: 'default',
                  style: { 
                    stroke: nestedThenExecuted ? '#22c55e' : '#d1d5db', 
                    strokeWidth: 2,
                    strokeDasharray: nestedThenExecuted ? undefined : '4,4'
                  },
                  markerEnd: { type: MarkerType.ArrowClosed, color: nestedThenExecuted ? '#22c55e' : '#d1d5db' },
                  label: 'true',
                  labelStyle: { fill: nestedThenExecuted ? '#22c55e' : '#9ca3af', fontSize: 11, fontWeight: 'bold' },
                  labelBgStyle: { fill: isDark ? '#1f2937' : 'white', fillOpacity: 0.9 },
                })
                
                // 递归处理 then 分支内部连线
                buildBranchEdges(nestedThenChildren, child.id, 'then', nestedThenExecuted, isDark, edges)
              }
              
              // condition → 第一个 else 子节点
              if (nestedElseChildren.length > 0) {
                const firstNestedElse = nestedElseChildren[0]
                const firstNestedElseId = firstNestedElse.id || `${child.id}-else-0`
                edges.push({
                  id: `edge-${child.id}-${firstNestedElseId}`,
                  source: child.id,
                  sourceHandle: 'bottom',
                  target: firstNestedElseId,
                  targetHandle: 'left',
                  type: 'default',
                  style: { 
                    stroke: nestedElseExecuted ? '#22c55e' : '#d1d5db', 
                    strokeWidth: 2,
                    strokeDasharray: nestedElseExecuted ? undefined : '4,4'
                  },
                  markerEnd: { type: MarkerType.ArrowClosed, color: nestedElseExecuted ? '#22c55e' : '#d1d5db' },
                  label: 'false',
                  labelStyle: { fill: nestedElseExecuted ? '#22c55e' : '#9ca3af', fontSize: 11, fontWeight: 'bold' },
                  labelBgStyle: { fill: isDark ? '#1f2937' : 'white', fillOpacity: 0.9 },
                })
                
                // 递归处理 else 分支内部连线
                buildBranchEdges(nestedElseChildren, child.id, 'else', nestedElseExecuted, isDark, edges)
              }
            } else if (child.collapsed && child.action === 'condition') {
              // 嵌套 condition 折叠时：直接连接到目标节点
              const nestedConditionResult = child.condition_result
              const nestedThenExecuted = nestedConditionResult === true
              const nestedElseExecuted = nestedConditionResult === false
              let nestedBranchTarget = branchTarget
              if (child.next && child.next.length > 0) {
                const targetId = child.next[0]
                const targetStep = steps.find(s => s.id === targetId || s.name === targetId)
                if (targetStep) nestedBranchTarget = targetStep
              }
              if (nestedBranchTarget) {
                edges.push({
                  id: `edge-${child.id}-${nestedBranchTarget.id}`,
                  source: child.id,
                  sourceHandle: 'right',
                  target: nestedBranchTarget.id,
                  targetHandle: 'left',
                  type: 'default',
                  style: { 
                    stroke: nestedThenExecuted || nestedElseExecuted ? '#22c55e' : '#d1d5db', 
                    strokeWidth: 2,
                    strokeDasharray: nestedThenExecuted || nestedElseExecuted ? undefined : '4,4'
                  },
                  markerEnd: { type: MarkerType.ArrowClosed, color: nestedThenExecuted || nestedElseExecuted ? '#22c55e' : '#d1d5db' },
                  label: nestedThenExecuted ? '✓' : nestedElseExecuted ? '✓' : '✗',
                  labelStyle: { fill: nestedThenExecuted || nestedElseExecuted ? '#22c55e' : '#9ca3af', fontSize: 14, fontWeight: 'bold' },
                  labelBgStyle: { fill: isDark ? '#1f2937' : 'white', fillOpacity: 0.9 },
                })
              }
            } else if (child.children && child.children.length > 0) {
              connectSteps(child.children, child)
            }
          }
          
          // then 最后节点连接到目标节点：label "✓" 或 "✗"
          if (branchTarget && visibleThenChildren.length > 0) {
            const lastThen = visibleThenChildren[visibleThenChildren.length - 1]
            // 如果最后一个 then 子节点是 condition，连线应该从其子节点发出
            if (lastThen.action === 'condition' && !lastThen.collapsed) {
              // 嵌套 condition：从子节点发出连线
              const lastThenChildrenRaw = lastThen.then_children || lastThen.then || []
              const lastElseChildrenRaw = lastThen.else_children || lastThen.else || []
              const lastConditionResult = lastThen.condition_result
              const lastThenExecuted = lastConditionResult === true
              const lastElseExecuted = lastConditionResult === false
              // 嵌套 condition 正常展开，只看其自身折叠状态
              const lastThenChildren = lastThen.collapsed ? (lastThenExecuted ? lastThenChildrenRaw : []) : lastThenChildrenRaw
              const lastElseChildren = lastThen.collapsed ? (lastElseExecuted ? lastElseChildrenRaw : []) : lastElseChildrenRaw
              
              // then 子节点结束连线
              if (lastThenChildren.length > 0) {
                const lastNestedThen = lastThenChildren[lastThenChildren.length - 1]
                edges.push({
                  id: `edge-${lastNestedThen.id}-${branchTarget.id}`,
                  source: lastNestedThen.id,
                  sourceHandle: 'right',
                  target: branchTarget.id,
                  targetHandle: 'left',
                  type: 'default',
                  style: { 
                    stroke: lastThenExecuted ? '#22c55e' : '#d1d5db', 
                    strokeWidth: 2,
                    strokeDasharray: lastThenExecuted ? undefined : '5,5'
                  },
                  markerEnd: { type: MarkerType.ArrowClosed, color: lastThenExecuted ? '#22c55e' : '#d1d5db' },
                  label: lastThenExecuted ? '✓' : '✗',
                  labelStyle: { fill: lastThenExecuted ? '#22c55e' : '#9ca3af', fontSize: 14, fontWeight: 'bold' },
                  labelBgStyle: { fill: isDark ? '#1f2937' : 'white', fillOpacity: 0.9 },
                })
              }
              
              // else 子节点结束连线（展开时显示）
              if (lastElseChildren.length > 0) {
                const lastNestedElse = lastElseChildren[lastElseChildren.length - 1]
                // 如果最后一个 else 子节点也是 condition 且展开，递归获取其真正的最后一个节点
                if (lastNestedElse.action === 'condition' && !lastNestedElse.collapsed) {
                  const nestedElseThenRaw = lastNestedElse.then_children || lastNestedElse.then || []
                  const nestedElseElseRaw = lastNestedElse.else_children || lastNestedElse.else || []
                  const nestedElseResult = lastNestedElse.condition_result
                  const nestedElseThenExecuted = nestedElseResult === true
                  const nestedElseElseExecuted = nestedElseResult === false
                  // 嵌套 condition 正常展开，只看其自身折叠状态
                  const nestedElseThen = lastNestedElse.collapsed ? (nestedElseThenExecuted ? nestedElseThenRaw : []) : nestedElseThenRaw
                  const nestedElseElse = lastNestedElse.collapsed ? (nestedElseElseExecuted ? nestedElseElseRaw : []) : nestedElseElseRaw
                  
                  if (nestedElseThen.length > 0) {
                    const finalNode = nestedElseThen[nestedElseThen.length - 1]
                    edges.push({
                      id: `edge-${finalNode.id}-${branchTarget.id}`,
                      source: finalNode.id,
                      sourceHandle: 'right',
                      target: branchTarget.id,
                      targetHandle: 'left',
                      type: 'default',
                      style: { 
                        stroke: nestedElseThenExecuted ? '#22c55e' : '#d1d5db', 
                        strokeWidth: 2,
                        strokeDasharray: nestedElseThenExecuted ? undefined : '5,5'
                      },
                      markerEnd: { type: MarkerType.ArrowClosed, color: nestedElseThenExecuted ? '#22c55e' : '#d1d5db' },
                      label: nestedElseThenExecuted ? '✓' : '✗',
                      labelStyle: { fill: nestedElseThenExecuted ? '#22c55e' : '#9ca3af', fontSize: 14, fontWeight: 'bold' },
                      labelBgStyle: { fill: isDark ? '#1f2937' : 'white', fillOpacity: 0.9 },
                    })
                  }
                  if (nestedElseElse.length > 0) {
                    const finalNode = nestedElseElse[nestedElseElse.length - 1]
                    edges.push({
                      id: `edge-${finalNode.id}-${branchTarget.id}`,
                      source: finalNode.id,
                      sourceHandle: 'right',
                      target: branchTarget.id,
                      targetHandle: 'left',
                      type: 'default',
                      style: { 
                        stroke: nestedElseElseExecuted ? '#22c55e' : '#d1d5db', 
                        strokeWidth: 2,
                        strokeDasharray: nestedElseElseExecuted ? undefined : '5,5'
                      },
                      markerEnd: { type: MarkerType.ArrowClosed, color: nestedElseElseExecuted ? '#22c55e' : '#d1d5db' },
                      label: nestedElseElseExecuted ? '✓' : '✗',
                      labelStyle: { fill: nestedElseElseExecuted ? '#22c55e' : '#9ca3af', fontSize: 14, fontWeight: 'bold' },
                      labelBgStyle: { fill: isDark ? '#1f2937' : 'white', fillOpacity: 0.9 },
                    })
                  }
                } else {
                  edges.push({
                    id: `edge-${lastNestedElse.id}-${branchTarget.id}`,
                    source: lastNestedElse.id,
                    sourceHandle: 'right',
                    target: branchTarget.id,
                    targetHandle: 'left',
                    type: 'default',
                    style: { 
                      stroke: lastElseExecuted ? '#22c55e' : '#d1d5db', 
                      strokeWidth: 2,
                      strokeDasharray: lastElseExecuted ? undefined : '5,5'
                    },
                    markerEnd: { type: MarkerType.ArrowClosed, color: lastElseExecuted ? '#22c55e' : '#d1d5db' },
                    label: lastElseExecuted ? '✓' : '✗',
                    labelStyle: { fill: lastElseExecuted ? '#22c55e' : '#9ca3af', fontSize: 14, fontWeight: 'bold' },
                    labelBgStyle: { fill: isDark ? '#1f2937' : 'white', fillOpacity: 0.9 },
                  })
                }
              }
            } else if (lastThen.action === 'condition' && lastThen.collapsed) {
              // 折叠时：condition 本身直接连接到目标节点
              const lastConditionResult = lastThen.condition_result
              const lastThenExecuted = lastConditionResult === true
              const lastElseExecuted = lastConditionResult === false
              edges.push({
                id: `edge-${lastThen.id}-${branchTarget.id}`,
                source: lastThen.id,
                sourceHandle: 'right',
                target: branchTarget.id,
                targetHandle: 'left',
                type: 'default',
                style: { 
                  stroke: lastThenExecuted || lastElseExecuted ? '#22c55e' : '#d1d5db', 
                  strokeWidth: 2,
                  strokeDasharray: lastThenExecuted || lastElseExecuted ? undefined : '5,5'
                },
                markerEnd: { type: MarkerType.ArrowClosed, color: lastThenExecuted || lastElseExecuted ? '#22c55e' : '#d1d5db' },
                label: lastThenExecuted || lastElseExecuted ? '✓' : '✗',
                labelStyle: { fill: lastThenExecuted || lastElseExecuted ? '#22c55e' : '#9ca3af', fontSize: 14, fontWeight: 'bold' },
                labelBgStyle: { fill: isDark ? '#1f2937' : 'white', fillOpacity: 0.9 },
              })
            } else {
              // 非 condition 节点：直接连线
              edges.push({
                id: `edge-${lastThen.id}-${branchTarget.id}`,
                source: lastThen.id,
                sourceHandle: 'right',
                target: branchTarget.id,
                targetHandle: 'left',
                type: 'default',
                style: { 
                  stroke: thenExecuted ? '#22c55e' : '#d1d5db', 
                  strokeWidth: 2,
                  strokeDasharray: thenExecuted ? undefined : '5,5'
                },
                markerEnd: { type: MarkerType.ArrowClosed, color: thenExecuted ? '#22c55e' : '#d1d5db' },
                label: thenExecuted ? '✓' : '✗',
                labelStyle: { fill: thenExecuted ? '#22c55e' : '#9ca3af', fontSize: 14, fontWeight: 'bold' },
                labelBgStyle: { fill: isDark ? '#1f2937' : 'white', fillOpacity: 0.9 },
              })
            }
          }
        }

        // else 分支连线（从右侧连线）
        if (visibleElseChildren.length > 0) {
          const firstElse = visibleElseChildren[0]
          // condition → else 子节点：label "false"（从右侧连线）
          edges.push({
            id: `edge-${current.id}-${firstElse.id || `${current.id}-else-0`}`,
            source: current.id,
            sourceHandle: 'right',
            target: firstElse.id || `${current.id}-else-0`,
            targetHandle: 'left',
            type: 'default',
            style: { 
              stroke: elseExecuted ? '#22c55e' : '#d1d5db', 
              strokeWidth: 2,
              strokeDasharray: elseExecuted ? undefined : '4,4'
            },
            markerEnd: { type: MarkerType.ArrowClosed, color: elseExecuted ? '#22c55e' : '#d1d5db' },
            label: 'false',
            labelStyle: { fill: elseExecuted ? '#22c55e' : '#9ca3af', fontSize: 11, fontWeight: 'bold' },
            labelBgStyle: { fill: isDark ? '#1f2937' : 'white', fillOpacity: 0.9 },
          })
          // else 子节点链式连接（水平排布）
          for (let j = 0; j < visibleElseChildren.length - 1; j++) {
            edges.push({
              id: `edge-${visibleElseChildren[j].id || `${current.id}-else-${j}`}-${visibleElseChildren[j + 1].id || `${current.id}-else-${j + 1}`}`,
              source: visibleElseChildren[j].id || `${current.id}-else-${j}`,
              sourceHandle: 'right',
              target: visibleElseChildren[j + 1].id || `${current.id}-else-${j + 1}`,
              targetHandle: 'left',
              type: 'default',
              style: { stroke: elseExecuted ? '#22c55e' : '#d1d5db', strokeWidth: 2, strokeDasharray: elseExecuted ? undefined : '4,4' },
              markerEnd: { type: MarkerType.ArrowClosed, color: elseExecuted ? '#22c55e' : '#d1d5db' },
            })
          }
          
          // 递归处理 else 子节点内部的连线（使用递归函数处理任意层级嵌套）
          for (const child of visibleElseChildren) {
            // 父级折叠时，嵌套 condition 正常展开，不跟随父级折叠
            // 所以这里不检查 child.collapsed
            if (child.action === 'condition' && ((child.then_children || child.then)?.length || (child.else_children || child.else)?.length)) {
              // 获取嵌套 condition 的执行结果
              const nestedConditionResult = child.condition_result
              const nestedThenExecuted = nestedConditionResult === true
              const nestedElseExecuted = nestedConditionResult === false
              
              // 嵌套 condition 正常展开，不跟随父级折叠
              const nestedThenChildrenRaw = child.then_children || child.then || []
              const nestedElseChildrenRaw = child.else_children || child.else || []
              // 嵌套 condition 的连线：只看其自身折叠状态，不看父级
              const nestedThenChildren = child.collapsed ? (nestedThenExecuted ? nestedThenChildrenRaw : []) : nestedThenChildrenRaw
              const nestedElseChildren = child.collapsed ? (nestedElseExecuted ? nestedElseChildrenRaw : []) : nestedElseChildrenRaw
              
              // condition → 第一个 then 子节点
              if (nestedThenChildren.length > 0) {
                const firstNestedThen = nestedThenChildren[0]
                const firstNestedThenId = firstNestedThen.id || `${child.id}-then-0`
                edges.push({
                  id: `edge-${child.id}-${firstNestedThenId}`,
                  source: child.id,
                  sourceHandle: 'right',
                  target: firstNestedThenId,
                  targetHandle: 'left',
                  type: 'default',
                  style: { 
                    stroke: nestedThenExecuted ? '#22c55e' : '#d1d5db', 
                    strokeWidth: 2,
                    strokeDasharray: nestedThenExecuted ? undefined : '4,4'
                  },
                  markerEnd: { type: MarkerType.ArrowClosed, color: nestedThenExecuted ? '#22c55e' : '#d1d5db' },
                  label: 'true',
                  labelStyle: { fill: nestedThenExecuted ? '#22c55e' : '#9ca3af', fontSize: 11, fontWeight: 'bold' },
                  labelBgStyle: { fill: isDark ? '#1f2937' : 'white', fillOpacity: 0.9 },
                })
                
                // 递归处理 then 分支内部连线
                buildBranchEdges(nestedThenChildren, child.id, 'then', nestedThenExecuted, isDark, edges)
              }
              
              // condition → 第一个 else 子节点
              if (nestedElseChildren.length > 0) {
                const firstNestedElse = nestedElseChildren[0]
                const firstNestedElseId = firstNestedElse.id || `${child.id}-else-0`
                edges.push({
                  id: `edge-${child.id}-${firstNestedElseId}`,
                  source: child.id,
                  sourceHandle: 'bottom',
                  target: firstNestedElseId,
                  targetHandle: 'left',
                  type: 'default',
                  style: { 
                    stroke: nestedElseExecuted ? '#22c55e' : '#d1d5db', 
                    strokeWidth: 2,
                    strokeDasharray: nestedElseExecuted ? undefined : '4,4'
                  },
                  markerEnd: { type: MarkerType.ArrowClosed, color: nestedElseExecuted ? '#22c55e' : '#d1d5db' },
                  label: 'false',
                  labelStyle: { fill: nestedElseExecuted ? '#22c55e' : '#9ca3af', fontSize: 11, fontWeight: 'bold' },
                  labelBgStyle: { fill: isDark ? '#1f2937' : 'white', fillOpacity: 0.9 },
                })
                
                // 递归处理 else 分支内部连线
                buildBranchEdges(nestedElseChildren, child.id, 'else', nestedElseExecuted, isDark, edges)
              }
            } else if (child.collapsed && child.action === 'condition') {
              // 嵌套 condition 折叠时：直接连接到目标节点
              const nestedConditionResult = child.condition_result
              const nestedThenExecuted = nestedConditionResult === true
              const nestedElseExecuted = nestedConditionResult === false
              let nestedBranchTarget = branchTarget
              if (child.next && child.next.length > 0) {
                const targetId = child.next[0]
                const targetStep = steps.find(s => s.id === targetId || s.name === targetId)
                if (targetStep) nestedBranchTarget = targetStep
              }
              if (nestedBranchTarget) {
                edges.push({
                  id: `edge-${child.id}-${nestedBranchTarget.id}`,
                  source: child.id,
                  sourceHandle: 'right',
                  target: nestedBranchTarget.id,
                  targetHandle: 'left',
                  type: 'default',
                  style: { 
                    stroke: nestedThenExecuted || nestedElseExecuted ? '#22c55e' : '#d1d5db', 
                    strokeWidth: 2,
                    strokeDasharray: nestedThenExecuted || nestedElseExecuted ? undefined : '4,4'
                  },
                  markerEnd: { type: MarkerType.ArrowClosed, color: nestedThenExecuted || nestedElseExecuted ? '#22c55e' : '#d1d5db' },
                  label: nestedThenExecuted ? '✓' : nestedElseExecuted ? '✓' : '✗',
                  labelStyle: { fill: nestedThenExecuted || nestedElseExecuted ? '#22c55e' : '#9ca3af', fontSize: 14, fontWeight: 'bold' },
                  labelBgStyle: { fill: isDark ? '#1f2937' : 'white', fillOpacity: 0.9 },
                })
              }
            } else if (child.children && child.children.length > 0) {
              connectSteps(child.children, child)
            }
          }
          
          // else 最后节点连接到目标节点：label "✓" 或 "✗"
          if (branchTarget && visibleElseChildren.length > 0) {
            const lastElse = visibleElseChildren[visibleElseChildren.length - 1]
            // 如果最后一个 else 子节点是 condition，连线应该从其子节点发出
            if (lastElse.action === 'condition') {
              // 嵌套 condition：根据折叠状态和执行状态决定显示哪些子节点
              const lastThenChildrenRaw = lastElse.then_children || lastElse.then || []
              const lastElseChildrenRaw = lastElse.else_children || lastElse.else || []
              const lastConditionResult = lastElse.condition_result
              const nestedThenExecuted = lastConditionResult === true
              const nestedElseExecuted = lastConditionResult === false
              
              // 嵌套 condition 正常展开，只看其自身折叠状态
              const visibleNestedThenChildren = lastElse.collapsed ? (nestedThenExecuted ? lastThenChildrenRaw : []) : lastThenChildrenRaw
              const visibleNestedElseChildren = lastElse.collapsed ? (nestedElseExecuted ? lastElseChildrenRaw : []) : lastElseChildrenRaw
              
              // then 子节点结束连线
              if (visibleNestedThenChildren.length > 0) {
                const lastNestedThen = visibleNestedThenChildren[visibleNestedThenChildren.length - 1]
                edges.push({
                  id: `edge-${lastNestedThen.id}-${branchTarget.id}`,
                  source: lastNestedThen.id,
                  sourceHandle: 'right',
                  target: branchTarget.id,
                  targetHandle: 'left',
                  type: 'default',
                  style: { 
                    stroke: nestedThenExecuted ? '#22c55e' : '#d1d5db', 
                    strokeWidth: 2,
                    strokeDasharray: nestedThenExecuted ? undefined : '5,5'
                  },
                  markerEnd: { type: MarkerType.ArrowClosed, color: nestedThenExecuted ? '#22c55e' : '#d1d5db' },
                  label: nestedThenExecuted ? '✓' : '✗',
                  labelStyle: { fill: nestedThenExecuted ? '#22c55e' : '#9ca3af', fontSize: 14, fontWeight: 'bold' },
                  labelBgStyle: { fill: isDark ? '#1f2937' : 'white', fillOpacity: 0.9 },
                })
              }
              
              // else 子节点结束连线（展开时显示）
              if (visibleNestedElseChildren.length > 0) {
                const lastNestedElse = visibleNestedElseChildren[visibleNestedElseChildren.length - 1]
                edges.push({
                  id: `edge-${lastNestedElse.id}-${branchTarget.id}`,
                  source: lastNestedElse.id,
                  sourceHandle: 'right',
                  target: branchTarget.id,
                  targetHandle: 'left',
                  type: 'default',
                  style: { 
                    stroke: nestedElseExecuted ? '#22c55e' : '#d1d5db', 
                    strokeWidth: 2,
                    strokeDasharray: nestedElseExecuted ? undefined : '5,5'
                  },
                  markerEnd: { type: MarkerType.ArrowClosed, color: nestedElseExecuted ? '#22c55e' : '#d1d5db' },
                  label: nestedElseExecuted ? '✓' : '✗',
                  labelStyle: { fill: nestedElseExecuted ? '#22c55e' : '#9ca3af', fontSize: 14, fontWeight: 'bold' },
                  labelBgStyle: { fill: isDark ? '#1f2937' : 'white', fillOpacity: 0.9 },
                })
              }
            } else {
              // 非 condition 节点：直接连线
              edges.push({
                id: `edge-${lastElse.id || `${current.id}-else-${visibleElseChildren.length - 1}`}-${branchTarget.id}`,
                source: lastElse.id || `${current.id}-else-${visibleElseChildren.length - 1}`,
                sourceHandle: 'right',
                target: branchTarget.id,
                targetHandle: 'left',
                type: 'default',
                style: { 
                  stroke: elseExecuted ? '#22c55e' : '#d1d5db', 
                  strokeWidth: 2,
                  strokeDasharray: elseExecuted ? undefined : '5,5'
                },
                markerEnd: { type: MarkerType.ArrowClosed, color: elseExecuted ? '#22c55e' : '#d1d5db' },
                label: elseExecuted ? '✓' : '✗',
                labelStyle: { fill: elseExecuted ? '#22c55e' : '#9ca3af', fontSize: 14, fontWeight: 'bold' },
                labelBgStyle: { fill: isDark ? '#1f2937' : 'white', fillOpacity: 0.9 },
              })
            }
          }
        }

        // 如果两个分支都没有子节点，直接连接到目标节点
        if (thenChildren.length === 0 && elseChildren.length === 0 && branchTarget) {
          edges.push({
            id: `edge-${current.id}-${branchTarget.id}`,
            source: current.id,
            sourceHandle: 'right',
            target: branchTarget.id,
            targetHandle: 'left',
            type: 'default',
            style: { stroke: '#9ca3af', strokeWidth: 2 },
            markerEnd: { type: MarkerType.ArrowClosed, color: '#9ca3af' },
          })
        }

        continue
      }

      // 其他节点
      if (parent && i === 0) {
        const isRunning = current.status === 'running'
        
        edges.push({
          id: `edge-${parent.id}-${current.id}`,
          source: parent.id,
          target: current.id,
          type: 'default',
          animated: isRunning,
          style: { stroke: '#9ca3af', strokeWidth: 2 },
          markerEnd: { type: MarkerType.ArrowClosed, color: '#9ca3af' },
        })
      }

      if (next) {
        const nextIsRunning = next.status === 'running'
        
        edges.push({
          id: `edge-${current.id}-${next.id}`,
          source: current.id,
          target: next.id,
          type: 'default',
          animated: nextIsRunning,
          style: { stroke: '#9ca3af', strokeWidth: 2 },
          markerEnd: { type: MarkerType.ArrowClosed, color: '#9ca3af' },
        })
      }

      // 未折叠时递归处理子节点
      if (current.children?.length && !isCollapsed) {
        connectSteps(current.children, current)
      }
    }
  }

  connectSteps(steps)
  return edges
}

// 自定义节点
function FlowNode({ id, data }: { id: string; data: Record<string, unknown> }) {
  const { t } = useTranslation()
  const d = data as unknown as NodeData
  const style = getActionStyle(d?.action || '')
  const statusConf = getStatusConfig(d?.status)
  const Icon = style.icon
  const StatusIcon = statusConf.icon
  const actionLabel = getActionLabel(d?.action || '')
  const isForeach = d?.action === 'foreach' || d?.action === 'loop'
  
  // 提取迭代序号（从 ID 中解析 iter-N）
  const iterMatch = d?.id?.match(/-iter-(\d+)$/)
  const iterIndex = iterMatch ? parseInt(iterMatch[1]) : null
  
  // 计算详细程度（分层渲染）
  const zoomLevel = (d?.zoomLevel as number) || 1
  const totalNodeCount = (d?.totalNodeCount as number) || 0
  const detailLevel = calculateDetailLevel(totalNodeCount, zoomLevel)
  
  // 详细视图（节点 < 50 或缩放 > 60%）
  if (detailLevel === DetailLevel.DETAILED) {
    return (
      <div
        className={`relative rounded-xl border-2 ${style.borderColor} ${style.bgColor} shadow-sm hover:shadow-md transition-all ${d?.status === 'running' ? 'animate-pulse border-blue-400' : ''}`}
        style={{ width: NODE_WIDTH, minHeight: NODE_HEIGHT }}
      >
        <Handle type="target" position={Position.Left} id="left" className="!bg-gray-400 !w-2.5 !h-2.5" />
        <Handle type="source" position={Position.Right} id="right" className="!bg-gray-400 !w-2.5 !h-2.5" />
        <Handle type="target" position={Position.Top} id="top" className="!bg-gray-400 !w-2.5 !h-2.5" />
        <Handle type="source" position={Position.Bottom} id="bottom" className="!bg-gray-400 !w-2.5 !h-2.5" />

        <div className="p-3">
          <div className="flex items-center gap-2.5 mb-1.5">
            <div className={`p-1.5 rounded-lg ${style.bgColor} ${style.color} flex-shrink-0`}>
              <Icon className="w-4 h-4" />
            </div>
            <span className="font-semibold text-gray-800 dark:text-gray-100 text-sm truncate flex-1" title={d?.name === '__pending_loop__' ? t('execution.pendingLoop') : (d?.name || 'Unknown')}>
              {iterIndex !== null 
                ? `#${iterIndex + 1} ${d?.name || 'Unknown'}` 
                : d?.name === '__pending_loop__' 
                  ? t('execution.pendingLoop') 
                  : (d?.name || 'Unknown')}
            </span>
            <StatusIcon className={`w-4 h-4 flex-shrink-0 ${statusConf.color}`} />
          </div>

          <div className="flex items-center justify-between">
            <span className={`text-xs font-medium ${style.color} px-2 py-0.5 rounded-full bg-white/60 dark:bg-black/20`}>
              {actionLabel}
            </span>
            {d?.action === 'sleep' && (d as any)?.sleepDuration && (
              <span className="text-xs text-purple-600 dark:text-purple-400 font-medium">
                ⏸️ {(d as any).sleepDuration}
              </span>
            )}
            {d?.hasChildren && d?.action !== 'condition' && (
              <span className="text-xs text-gray-500 dark:text-gray-400 font-medium">
                {(isForeach || d?.action === 'parallel') && d?.children ? (
                  (() => {
                    const success = d.children.filter((c: any) => c.status === 'success').length
                    const total = d.children.length
                    const failed = d.children.filter((c: any) => c.status === 'failed').length
                    if (failed > 0) return `❌ ${failed}/${total} ${t('execution.failed')}`
                    return `✅ ${success}/${total} ${t('execution.success')}`
                  })()
                ) : isForeach && d?.items ? `${d.items.length} items` : d?.childrenCount}
              </span>
            )}
          </div>

          {d?.duration && (
            <div className="mt-1 text-xs text-gray-500 dark:text-gray-400 font-mono">⏱ {d.duration}</div>
          )}
        </div>

        {d?.hasChildren && d?.onToggleCollapse && (
          <button
            onClick={(e) => { e.stopPropagation(); e.preventDefault(); d?.onToggleCollapse?.(id) }}
            onDoubleClick={(e) => { e.stopPropagation(); e.preventDefault() }}
            className={`absolute -bottom-3 -right-3 w-8 h-8 rounded-full flex items-center justify-center shadow-lg border-2 border-white dark:border-gray-800 ${d?.isCollapsed ? 'bg-blue-500 hover:bg-blue-600' : 'bg-gray-100 hover:bg-gray-200 dark:bg-gray-700 dark:hover:bg-gray-600'} transition-colors`}
          >
            {d?.isCollapsed ? <ChevronDown className="w-5 h-5 text-white" /> : <ChevronUp className="w-5 h-5 text-gray-600 dark:text-gray-300" />}
          </button>
        )}
      </div>
    )
  }
  
  // 标准视图（节点 50-200，缩放 30%-60%）
  if (detailLevel === DetailLevel.NORMAL) {
    return (
      <div
        className={`relative rounded-lg border-2 ${style.borderColor} ${style.bgColor} transition-all ${d?.status === 'running' ? 'animate-pulse border-blue-400' : ''}`}
        style={{ width: 180, minHeight: 50 }}
      >
        <Handle type="target" position={Position.Left} id="left" className="!bg-gray-400 !w-2 !h-2" />
        <Handle type="source" position={Position.Right} id="right" className="!bg-gray-400 !w-2 !h-2" />
        <Handle type="target" position={Position.Top} id="top" className="!bg-gray-400 !w-2 !h-2" />
        <Handle type="source" position={Position.Bottom} id="bottom" className="!bg-gray-400 !w-2 !h-2" />

        <div className="p-2">
          <div className="flex items-center gap-2">
            <div className={`p-1 rounded ${style.color} flex-shrink-0`}>
              <Icon className="w-3.5 h-3.5" />
            </div>
            <span className="font-medium text-gray-800 dark:text-gray-100 text-xs truncate flex-1">
              {d?.name || 'Unknown'}
            </span>
            <StatusIcon className={`w-3.5 h-3.5 flex-shrink-0 ${statusConf.color}`} />
          </div>
        </div>
      </div>
    )
  }
  
  // 简化视图（节点 200-500，缩放 < 30%）
  if (detailLevel === DetailLevel.SIMPLIFIED) {
    return (
      <div
        className={`relative rounded-md border-2 ${style.borderColor} ${style.bgColor} transition-all`}
        style={{ width: 40, height: 40 }}
      >
        <Handle type="target" position={Position.Left} id="left" className="!bg-gray-400 !w-1.5 !h-1.5" />
        <Handle type="source" position={Position.Right} id="right" className="!bg-gray-400 !w-1.5 !h-1.5" />
        <Handle type="target" position={Position.Top} id="top" className="!bg-gray-400 !w-1.5 !h-1.5" />
        <Handle type="source" position={Position.Bottom} id="bottom" className="!bg-gray-400 !w-1.5 !h-1.5" />

        <div className="flex items-center justify-center h-full">
          <StatusIcon className={`w-5 h-5 ${statusConf.color}`} />
        </div>
      </div>
    )
  }
  
  // 隐藏视图（节点 > 500，缩放 < 15%）- 只渲染一个小点
  return (
    <div
      className={`relative rounded-full ${style.bgColor}`}
      style={{ width: 8, height: 8, background: statusConf.color === 'text-green-500' ? '#22c55e' : statusConf.color === 'text-red-500' ? '#ef4444' : '#9ca3af' }}
    >
      <Handle type="target" position={Position.Left} id="left" className="!bg-transparent !w-1 !h-1" />
      <Handle type="source" position={Position.Right} id="right" className="!bg-transparent !w-1 !h-1" />
    </div>
  )
}

// Foreach 容器节点
function ForeachGroupNode({ data }: { data: Record<string, unknown> }) {
  const { t } = useTranslation()
  const d = data as { width: number; height: number; iterationCount?: number; foreachId?: string; labelLoop?: string; labelTimes?: string }
  
  return (
    <div
      className="rounded-xl border-2 border-dashed border-cyan-400 dark:border-cyan-500 bg-cyan-50/30 dark:bg-cyan-900/10"
      style={{
        width: d.width,
        height: d.height,
      }}
    >
      {/* 循环标记 */}
      <div className="absolute -top-3 left-3 px-2 py-0.5 bg-cyan-100 dark:bg-cyan-900 rounded text-xs font-medium text-cyan-700 dark:text-cyan-300 flex items-center gap-1 z-10">
        <Repeat className="w-3 h-3" />
        <span>{d.labelLoop || t('execution.loopBody')}</span>
        {d.iterationCount && <span className="text-cyan-500">({d.iterationCount}{d.labelTimes || t('execution.loopTimes')})</span>}
      </div>
    </div>
  )
}

// Parallel 容器节点
function ParallelGroupNode({ data }: { data: Record<string, unknown> }) {
  const { t } = useTranslation()
  const d = data as { width: number; height: number; taskCount?: number; parallelId?: string; labelParallel?: string; labelTasks?: string; isCollapsed?: boolean }
  
  // 智能分组显示：折叠时显示前 2 个 + 省略数
  const displayCount = d.isCollapsed ? 2 : d.taskCount
  const omittedCount = d.isCollapsed && d.taskCount ? Math.max(0, d.taskCount - 2) : 0
  
  return (
    <div
      className="rounded-xl border-2 border-dashed border-purple-400 dark:border-purple-500 bg-purple-50/30 dark:bg-purple-900/10"
      style={{
        width: d.width,
        height: d.height,
      }}
    >
      {/* 并行标记 */}
      <div className="absolute -top-3 left-3 px-2 py-0.5 bg-purple-100 dark:bg-purple-900 rounded text-xs font-medium text-purple-700 dark:text-purple-300 flex items-center gap-1 z-10">
        <Layers className="w-3 h-3" />
        <span>{d.labelParallel || t('execution.parallel')}</span>
        {d.taskCount && (
          <span className="text-purple-500">
            ({d.isCollapsed && omittedCount > 0 
              ? `${displayCount}/${d.taskCount}` 
              : d.taskCount}
            {d.labelTasks || t('execution.parallelTasks')})
            {omittedCount > 0 && <span className="ml-1 text-purple-400">(+{omittedCount} more)</span>}
          </span>
        )}
      </div>
    </div>
  )
}

// nodeTypes 必须在组件外部定义，避免每次渲染重新创建
const nodeTypes = { flowNode: FlowNode, foreachGroup: ForeachGroupNode, parallelGroup: ParallelGroupNode }
const edgeTypes = { hollow: HollowEdge }

// WorkflowGraph 主组件
export interface WorkflowGraphProps {
  steps: FlowStep[]
  executionSteps?: FlowStep[]
  onNodeClick?: (step: FlowStep, viewportPosition?: { x: number; y: number; width: number; height: number; right: number; left: number }) => void
  showMiniMap?: boolean
  logLayout?: 'bottom' | 'right' | 'float' | 'none'
  collapsedNodes?: Set<string>
  onCollapseChange?: (collapsedNodes: Set<string>) => void
  locked?: boolean
  onLockChange?: (locked: boolean) => void
}

export function WorkflowGraph({ steps, executionSteps = [], onNodeClick, showMiniMap = true, logLayout = 'none', collapsedNodes: externalCollapsedNodes, onCollapseChange, locked: externalLocked, onLockChange }: WorkflowGraphProps) {
  const { t } = useTranslation()
  const { isDark } = useThemeStore()
  const [internalCollapsedNodes, setInternalCollapsedNodes] = useState<Set<string>>(new Set())
  const [internalLocked, setInternalLocked] = useState<boolean>(true) // 默认锁定
  const fitViewRef = useRef<any>(null)
  
  // 使用外部或内部的折叠状态
  const collapsedNodes = externalCollapsedNodes ?? internalCollapsedNodes
  const setCollapsedNodes = onCollapseChange ?? setInternalCollapsedNodes
  
  // 自动折叠 parallel 子节点超过阈值的节点（只在初始化时执行一次）
  const [autoCollapseInitialized, setAutoCollapseInitialized] = useState(false)
  useEffect(() => {
    if (!steps || steps.length === 0) return
    if (autoCollapseInitialized) return
    
    // 检查所有 parallel 步骤，子节点超过阈值则自动折叠
    const autoCollapseIds = new Set<string>()
    const checkParallel = (stepList: FlowStep[]) => {
      for (const step of stepList) {
        if (step.action === 'parallel' && step.children && step.children.length > PARALLEL_TASK_THRESHOLD) {
          autoCollapseIds.add(step.id)
        }
        if (step.children) checkParallel(step.children)
        if (step.then_children) checkParallel(step.then_children)
        if (step.else_children) checkParallel(step.else_children)
      }
    }
    checkParallel(steps)
    
    // 只在第一次初始化时设置，不覆盖用户后续操作
    if (autoCollapseIds.size > 0) {
      setCollapsedNodes(autoCollapseIds)
    }
    setAutoCollapseInitialized(true)
  }, [steps]) // 依赖 steps，但通过 autoCollapseInitialized 确保只执行一次
  
  // 使用外部或内部的锁定状态
  const locked = externalLocked ?? internalLocked
  const setLocked = onLockChange ?? setInternalLocked
  
  // 节点状态（包含用户拖动后的位置）
  const [nodesState, setNodesState] = useState<Node[]>([])
  
  // 跟踪被用户拖动过的节点 ID，只有这些节点才保留位置
  const [draggedNodes, setDraggedNodes] = useState<Set<string>>(new Set())
  
  // 跟踪当前缩放级别（用于分层渲染）
  const [zoomLevel, setZoomLevel] = useState<number>(1)
  
  // 折叠/展开节点
  const handleToggleCollapse = useCallback((nodeId: string) => {
    const newSet = new Set(collapsedNodes)
    if (newSet.has(nodeId)) {
      newSet.delete(nodeId)
    } else {
      newSet.add(nodeId)
    }
    setCollapsedNodes(newSet)
  }, [collapsedNodes, setCollapsedNodes])

  // 合并 executionSteps 的状态到 steps
  const enrichedSteps = useMemo(() => {
    if (!steps || steps.length === 0) return []
    const execMap = new Map(executionSteps.map((s) => [s.id, s]))
    const enrich = (stepList: FlowStep[]): FlowStep[] => {
      return stepList.map(step => {
        const exec = execMap.get(step.id)
        const merged = {
          ...step,
          ...(exec || {}),
          collapsed: collapsedNodes.has(step.id),
        }
        
        // 递归处理 children（使用合并后的值）
        if (merged.children && merged.children.length > 0) {
          merged.children = enrich(merged.children)
        }
        
        // Condition 特殊处理：递归 enrich then/else children（使用合并后的值）
        if (merged.then_children && merged.then_children.length > 0) {
          merged.then_children = enrich(merged.then_children)
        }
        if (merged.else_children && merged.else_children.length > 0) {
          merged.else_children = enrich(merged.else_children)
        }
        
        return merged
      })
    }
    return enrich(steps)
  }, [steps, executionSteps, collapsedNodes])

  // 所有 hooks 必须在早期返回之前
  const { nodes: positionedNodes, foreachGroups, parallelGroups } = useMemo(() => {
    if (enrichedSteps?.length > 0) {
    }
    if (!enrichedSteps || enrichedSteps.length === 0) return { nodes: [], nextX: 0, maxY: 0, foreachGroups: new Map(), parallelGroups: new Map() }
    return calculateLayout(enrichedSteps, 50, 50)
  }, [enrichedSteps])

  const edges = useMemo(() => {
    if (!enrichedSteps || enrichedSteps.length === 0) return []
    return buildEdges(enrichedSteps, isDark)
  }, [enrichedSteps, collapsedNodes, isDark])

  // 基础节点（不含用户拖动后的位置）
  const baseNodes: Node[] = useMemo(() => {
    if (!positionedNodes || positionedNodes.length === 0) return []
    
    // 流程节点
    const flowNodes = positionedNodes.map((step) => ({
      id: step.id,
      type: 'flowNode',
      position: step.position,
      width: NODE_WIDTH,
      height: NODE_HEIGHT,
      data: {
        ...step,
        hasChildren: (!!step.children && step.children.length > 0) || 
                     (step.action === 'condition' && ((step.then_children && step.then_children.length > 0) || (step.else_children && step.else_children.length > 0))),
        childrenCount: step.children?.length || 0,
        children: step.children,
        isCollapsed: collapsedNodes.has(step.id),
        onToggleCollapse: handleToggleCollapse,
        // 传递缩放级别和总节点数，用于分层渲染
        zoomLevel,
        totalNodeCount: positionedNodes.length,
      } as unknown as Record<string, unknown>,
    }))
    
    // foreach 容器节点（不可拖动）
    const foreachContainerNodes = Array.from(foreachGroups.entries()).map(([foreachId, bounds]) => {
      const foreachStep = positionedNodes.find(s => s.id === foreachId)
      // 使用 originalIterations（原始迭代次数）或 items.length
      const displayCount = bounds.originalIterations || foreachStep?.items?.length || 0
      return {
        id: `foreach-group-${foreachId}`,
        type: 'foreachGroup' as any,
        position: { x: bounds.x, y: bounds.y },
        draggable: false,
        selectable: false,
        data: {
          width: bounds.width,
          height: bounds.height,
          iterationCount: displayCount,
          foreachId,
        } as unknown as Record<string, unknown>,
        zIndex: -1,
      } as any
    })
    
    // parallel 容器节点（不可拖动）
    const parallelContainerNodes = Array.from(parallelGroups.entries()).map(([parallelId, bounds]) => {
      const parallelStep = positionedNodes.find(s => s.id === parallelId)
      const isCollapsed = collapsedNodes.has(parallelId)
      return {
        id: `parallel-group-${parallelId}`,
        type: 'parallelGroup' as any,
        position: { x: bounds.x, y: bounds.y },
        draggable: false,
        selectable: false,
        data: {
          width: bounds.width,
          height: bounds.height,
          taskCount: parallelStep?.children?.length,
          parallelId,
          isCollapsed,
        } as unknown as Record<string, unknown>,
        zIndex: -1,
      } as any
    })
    
    return [...flowNodes, ...foreachContainerNodes, ...parallelContainerNodes]
  }, [positionedNodes, collapsedNodes, handleToggleCollapse, foreachGroups, parallelGroups, zoomLevel])
  
  // 当 baseNodes 变化时，合并到 nodesState（保留用户拖动的位置）
  useEffect(() => {
    if (baseNodes.length === 0) {
      setNodesState([])
      setDraggedNodes(new Set())
      return
    }
    
    // 只保留被用户拖动过的节点的位置
    const prevPositions = new Map(
      nodesState
        .filter(n => draggedNodes.has(n.id))
        .map(n => [n.id, n.position])
    )
    
    const newNodes = baseNodes.map(node => {
      const prevPos = prevPositions.get(node.id)
      // 只有 flowNode 类型且被拖动过才保留拖动位置
      if (prevPos && node.type === 'flowNode') {
        return { ...node, position: prevPos }
      }
      return node
    })
    
    setNodesState(newNodes)
  }, [baseNodes, draggedNodes])
  
  // 处理节点位置变化（拖动）
  const onNodesChange = useCallback((changes: NodeChange[]) => {
    if (locked) return
    setNodesState((nds) => {
      const newDraggedIds = new Set(draggedNodes)
      // 记录被拖动的节点 ID
      changes.forEach(change => {
        if (change.type === 'position' && change.id) {
          newDraggedIds.add(change.id)
        }
      })
      setDraggedNodes(newDraggedIds)
      return applyNodeChanges(changes, nds)
    })
  }, [locked, draggedNodes])
  
  // 最终节点数组
  const nodes = nodesState

  const finalEdges: Edge[] = useMemo(() => {
    return edges.map((edge) => ({
      ...edge,
      // 保留边原有的 animated 属性（如果有的话）
      animated: edge.animated,
    }))
  }, [edges])

  // 初始 fitView
  useEffect(() => {
    if (fitViewRef.current && positionedNodes.length > 0) {
      setTimeout(() => {
        fitViewRef.current?.fitView({ padding: 0.2 })
      }, 100)
    }
  }, [])

  // 早期返回放在所有 hooks 之后
  if (!steps || steps.length === 0) {
    return (
      <div className="w-full h-full flex items-center justify-center bg-gray-50 dark:bg-gray-900">
        <div className="text-center text-gray-500 dark:text-gray-400">
          <AlertCircle className="w-8 h-8 mx-auto mb-4 text-gray-400" />
          <p>{t('execution.noSteps')}</p>
        </div>
      </div>
    )
  }

  return (
    <>
      {/* 全局样式：移除 ReactFlow 节点容器的阴影 */}
      <style dangerouslySetInnerHTML={{ __html: GLOBAL_STYLES }} />
      <div className="w-full h-full bg-gray-50 dark:bg-gray-900 relative">
      <ReactFlow
        nodes={nodes}
        edges={finalEdges}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        fitView={false}
        nodesDraggable={!locked}
        elementsSelectable={!locked}
        onNodesChange={onNodesChange}
        minZoom={0.1}
        maxZoom={2}
        className="bg-transparent"
        style={{ background: 'transparent' }}
        onInit={(instance) => {
          fitViewRef.current = instance
        }}
        onMove={(_event, transform) => {
          // 跟踪缩放级别变化
          setZoomLevel(transform.zoom)
        }}
        onNodeClick={(event, node) => {
          if (onNodeClick && node.data) {
            // 获取节点在视口中的位置
            const rect = (event.target as HTMLElement).closest('.react-flow__node')?.getBoundingClientRect()
            if (rect) {
              const viewportPosition = {
                x: rect.left + rect.width / 2, // 节点中心 x
                y: rect.top + rect.height / 2, // 节点中心 y
                width: rect.width,  // 节点宽度
                height: rect.height, // 节点高度
                right: rect.right,  // 节点右边界
                left: rect.left,    // 节点左边界
              }
              onNodeClick(node.data as unknown as FlowStep, viewportPosition)
            } else {
              onNodeClick(node.data as unknown as FlowStep)
            }
          }
        }}
      >
        <Controls showInteractive={false}>
          <ControlButton
            onClick={() => setLocked(!locked)}
            title={locked ? t('execution.unlockCanvas') : t('execution.lockCanvas')}
            aria-label={locked ? t('execution.unlockCanvas') : t('execution.lockCanvas')}
          >
            {locked ? <ReactFlowLockIcon /> : <ReactFlowUnlockIcon />}
          </ControlButton>
        </Controls>
        {showMiniMap && (
          <MiniMap
            nodeColor={(node) => {
              const d = node.data as unknown as NodeData
              if (d?.status === 'failed') return '#ef4444'
              if (d?.status === 'success') return '#22c55e'
              if (d?.status === 'running') return '#3b82f6'
              return '#9ca3af'
            }}
            maskColor={isDark ? "rgba(255, 255, 255, 0.1)" : "rgba(0, 0, 0, 0.15)"}
            pannable
            zoomable
            style={{
              background: isDark ? 'rgba(255, 255, 255, 0.1)' : 'rgba(0, 0, 0, 0.05)',
              backdropFilter: 'blur(4px)',
              borderRadius: '8px',
              bottom: logLayout === 'right' ? 10 : 50,
              right: logLayout === 'right' ? 420 : 10,
            }}
            className="!border !border-gray-200 dark:!border-gray-700"
          />
        )}
      </ReactFlow>
      </div>
    </>
  )
}

// 工具函数
export function workflowToFlowSteps(workflowSteps: any[]): FlowStep[] {
  const result = workflowSteps.map((step, index) => {
    const converted = {
      id: step.id || `step-${index}`,
      name: step.name || `Step ${index + 1}`,
      action: step.action || step.type || '',
      description: step.description,
      status: step.status,
      output: step.output,
      error: step.error,
      duration: step.duration,
      if: step.if,
      loop: step.loop,
      parallel: step.parallel,
      children: step.children ? workflowToFlowSteps(step.children) :
               (step.action === 'parallel' && step.steps ? workflowToFlowSteps(step.steps) :
               ((step.action === 'foreach' || step.action === 'loop') && step.do ? workflowToFlowSteps(step.do) : undefined)),
      url: step.url,
      method: step.method,
      script: step.script,
      shell: step.shell,
      message: step.message,
      level: step.level,
      run: step.run,
      body: step.body,
      items: step.items,
      itemVar: step.item_var,
      // DAG 字段
      next: step.next,
      depends_on: step.depends_on,
      join_mode: step.join_mode,
      // Condition 字段
      expression: step.expression,
      then_children: step.then_children ? workflowToFlowSteps(step.then_children) : undefined,
      else_children: step.else_children ? workflowToFlowSteps(step.else_children) : undefined,
      condition_result: step.condition_result,
      // Sleep 字段
      sleepDuration: step.sleepDuration || step.sleep_duration || step.duration,
      // Shell 命令
      shellCommand: step.shellCommand || step.command || step.shell,
      // HTTP 信息
      httpUrl: step.httpUrl || step.url,
      httpMethod: step.httpMethod || step.method,
      // Log 消息
      logMessage: step.logMessage || step.message,
    }
    if (step.action === 'condition') {
    }
    return converted
  })
  return result
}