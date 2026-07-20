import { useTranslation } from 'react-i18next'
import { Position, Handle, getBezierPath } from '@xyflow/react'
import {
  Globe, Terminal, MessageSquare, GitBranch, Repeat, Layers, Zap, Clock,
  CheckCircle, XCircle, AlertCircle, ChevronDown, ChevronUp,
  type LucideIcon,
} from 'lucide-react'
import {
  NODE_WIDTH, NODE_HEIGHT, DetailLevel, calculateDetailLevel,
  type NodeData,
} from './flowTypes'

// 移除 ReactFlow 节点容器的阴影（让阴影只应用于节点自身，跟随节点大小变化）
export const GLOBAL_STYLES = `
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
export function ReactFlowLockIcon() {
  return (
    <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 25 32" className="react-flow__controls-interactive-icon">
      <path d="M21.333 10.667H19.81V7.619C19.81 3.429 16.38 0 12.19 0 8 0 4.571 3.429 4.571 7.619v3.048H3.048A3.056 3.056 0 000 13.714v15.238A3.056 3.056 0 003.048 32h18.285a3.056 3.056 0 003.048-3.048V13.714a3.056 3.056 0 00-3.048-3.047zM12.19 24.533a3.056 3.056 0 01-3.047-3.047 3.056 3.056 0 013.047-3.048 3.056 3.056 0 013.048 3.048 3.056 3.056 0 01-3.048 3.047zm4.724-13.866H7.467V7.619c0-2.59 2.133-4.724 4.723-4.724 2.591 0 4.724 2.133 4.724 4.724v3.048z" fill="currentColor" />
    </svg>
  )
}

export function ReactFlowUnlockIcon() {
  return (
    <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 25 32" className="react-flow__controls-interactive-icon">
      <path d="M21.333 10.667H19.81V7.619C19.81 3.429 16.38 0 12.19 0c-4.114 1.828-1.37 2.133.305 2.438 1.676.305 4.42 2.59 4.42 5.181v3.048H3.047A3.056 3.056 0 000 13.714v15.238A3.056 3.056 0 003.047 32h18.285a3.056 3.056 0 003.048-3.048V13.714a3.056 3.056 0 00-3.048-3.047zM12.19 24.533a3.056 3.056 0 01-3.047-3.047 3.056 3.056 0 013.047-3.048 3.056 3.056 0 013.048 3.048 3.056 3.056 0 01-3.048 3.047zm4.724-13.866H7.467V7.619c0-2.59 2.133-4.724 4.723-4.724 2.591 0 4.724 2.133 4.724 4.724v3.048z" fill="currentColor" />
    </svg>
  )
}

// 中空边组件
export function HollowEdge({ id, sourceX, sourceY, targetX, targetY, sourcePosition, targetPosition, style }: {
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
  const styles: Record<string, { icon: LucideIcon; color: string; borderColor: string; bgColor: string }> = {
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
  const configs: Record<string, { icon: LucideIcon; color: string }> = {
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

// 自定义节点
export function FlowNode({ id, data }: { id: string; data: Record<string, unknown> }) {
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
            {d?.action === 'condition' && (
              <span className="text-xs font-mono text-red-500">cr:{String(d?.condition_result)}</span>
            )}
            {d?.action === 'sleep' && d?.sleepDuration && (
              <span className="text-xs text-purple-600 dark:text-purple-400 font-medium">
                ⏸️ {d.sleepDuration}
              </span>
            )}
            {d?.hasChildren && d?.action !== 'condition' && (
              <span className="text-xs text-gray-500 dark:text-gray-400 font-medium">
                {(isForeach || d?.action === 'parallel') && d?.children ? (
                  (() => {
                    const success = d.children!.filter((c) => c.status === 'success').length
                    const total = d.children!.length
                    const failed = d.children!.filter((c) => c.status === 'failed').length
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
export function ForeachGroupNode({ data }: { data: Record<string, unknown> }) {
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
export function ParallelGroupNode({ data }: { data: Record<string, unknown> }) {
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

// nodeTypes/edgeTypes 定义在 ./nodeTypes（独立文件，保持本文件纯组件导出）
