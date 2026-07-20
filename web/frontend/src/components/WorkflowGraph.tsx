import { useState, useMemo, useCallback, useRef, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import {
  ReactFlow,
  Node,
  Edge,
  Controls,
  MiniMap,
  ControlButton,
  applyNodeChanges,
  NodeChange,
  type ReactFlowInstance,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { useThemeStore } from '@/store/themeStore'
import { AlertCircle } from 'lucide-react'

import {
  NODE_WIDTH, NODE_HEIGHT, PARALLEL_TASK_THRESHOLD,
  type FlowStep, type NodeData,
} from './graph/flowTypes'
import { calculateLayout, buildEdges } from './graph/dagLayout'
import {
  GLOBAL_STYLES, ReactFlowLockIcon, ReactFlowUnlockIcon,
} from './graph/FlowNodes'
import { nodeTypes, edgeTypes } from './graph/nodeTypes'

export type { FlowStep } from './graph/flowTypes'

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

export function WorkflowGraph({ steps, onNodeClick, showMiniMap = true, logLayout = 'none', collapsedNodes: externalCollapsedNodes, onCollapseChange, locked: externalLocked, onLockChange }: WorkflowGraphProps) {
  const { t } = useTranslation()
  const { isDark } = useThemeStore()
  const [internalCollapsedNodes, setInternalCollapsedNodes] = useState<Set<string>>(new Set())
  const [internalLocked, setInternalLocked] = useState<boolean>(true) // 默认锁定
  const fitViewRef = useRef<ReactFlowInstance | null>(null)

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
    const enrich = (stepList: FlowStep[]): FlowStep[] => {
      return stepList.map(step => {
        const merged = {
          ...step,
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
  }, [steps, collapsedNodes])

  // 所有 hooks 必须在早期返回之前
  const { nodes: positionedNodes, foreachGroups, parallelGroups } = useMemo(() => {
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
        type: 'foreachGroup',
        position: { x: bounds.x, y: bounds.y },
        draggable: false,
        selectable: false,
        data: {
          width: bounds.width,
          height: bounds.height,
          iterationCount: displayCount,
          foreachId,
        },
        zIndex: -1,
      }
    })

    // parallel 容器节点（不可拖动）
    const parallelContainerNodes = Array.from(parallelGroups.entries()).map(([parallelId, bounds]) => {
      const parallelStep = positionedNodes.find(s => s.id === parallelId)
      const isCollapsed = collapsedNodes.has(parallelId)
      return {
        id: `parallel-group-${parallelId}`,
        type: 'parallelGroup',
        position: { x: bounds.x, y: bounds.y },
        draggable: false,
        selectable: false,
        data: {
          width: bounds.width,
          height: bounds.height,
          taskCount: parallelStep?.children?.length,
          parallelId,
          isCollapsed,
        },
        zIndex: -1,
      }
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
    return converted
  })
  return result
}
