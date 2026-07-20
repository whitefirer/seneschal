import { Layers, Repeat } from 'lucide-react'

// ==================== 容器节点组件 ====================

export interface ParallelGroupNodeData {
  width: number
  height: number
  taskCount?: number
}

// Parallel 容器节点
export function ParallelGroupNode({ data }: { id: string; data: ParallelGroupNodeData }) {
  return (
    <div
      className="rounded-xl border-2 border-dashed border-pink-400 dark:border-pink-500 bg-pink-50/30 dark:bg-pink-900/10"
      style={{
        width: data.width,
        height: data.height,
      }}
    >
      {/* 并行标记 */}
      <div className="absolute -top-4 left-4 px-2 py-1 bg-pink-100 dark:bg-pink-900 rounded text-xs font-medium text-pink-700 dark:text-pink-300 flex items-center gap-1 z-20 shadow-sm">
        <Layers className="w-3 h-3" />
        <span>Parallel</span>
        {data.taskCount && (
          <span className="text-pink-500">({data.taskCount} tasks)</span>
        )}
      </div>
    </div>
  )
}

export interface ForeachGroupNodeData {
  width: number
  height: number
  iterationCount?: number
}

// Foreach 容器节点
export function ForeachGroupNode({ data }: { id: string; data: ForeachGroupNodeData }) {
  return (
    <div
      className="rounded-xl border-2 border-dashed border-cyan-400 dark:border-cyan-500 bg-cyan-50/30 dark:bg-cyan-900/10"
      style={{
        width: data.width,
        height: data.height,
      }}
    >
      {/* 循环标记 */}
      <div className="absolute -top-4 left-4 px-2 py-1 bg-cyan-100 dark:bg-cyan-900 rounded text-xs font-medium text-cyan-700 dark:text-cyan-300 flex items-center gap-1 z-20 shadow-sm">
        <Repeat className="w-3 h-3" />
        <span>Foreach</span>
        {data.iterationCount && (
          <span className="text-cyan-500">({data.iterationCount} items)</span>
        )}
      </div>
    </div>
  )
}
