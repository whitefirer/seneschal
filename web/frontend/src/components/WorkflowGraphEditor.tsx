import { useState, useCallback, useMemo, useEffect, type MouseEvent as ReactMouseEvent } from 'react'
import {
  ReactFlow,
  Background,
  Controls,
  Edge,
  BackgroundVariant,
  MarkerType,
  type NodeChange,
  type NodeProps,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { Plus, Save, Play, AlertCircle, CheckCircle } from 'lucide-react'

import { yamlToDAG, dagToYaml } from '../utils/yaml-converter'
import type { GraphNode, GraphNodeData } from '../types/graph'
import { NODE_WIDTH, V_SPACING, H_SPACING, calculateNodeHeight, generateId } from './graphEditor/constants'
import { calculateLayout } from './graphEditor/layout'
import { EditorNode } from './graphEditor/EditorNode'
import { ParallelGroupNode, ForeachGroupNode, type ParallelGroupNodeData, type ForeachGroupNodeData } from './graphEditor/GroupNodes'

// ==================== 类型定义 ====================

interface WorkflowGraphEditorProps {
  initialSteps?: any[]
  // 保存/运行回调的载荷：主组件向 onSave/onRun 传 { steps } 对象
  // （注意：Editor.tsx 的处理器目前直接消费该形状——行为保持原样）
  onSave?: (payload: { steps: any[] }) => void
  onRun?: (payload: { steps: any[] }) => void
}

// ==================== 主组件 ====================

export default function WorkflowGraphEditor({
  initialSteps = [],
  onSave,
  onRun
}: WorkflowGraphEditorProps) {
  // 节点和边状态
  const [nodes, setNodes] = useState<GraphNode[]>([])
  const [edges, setEdges] = useState<Edge[]>([])

  // 未保存状态
  const [hasUnsavedChanges, setHasUnsavedChanges] = useState(false)
  const [originalStepsHash, setOriginalStepsHash] = useState<string>('')

  // ==================== 工具函数 ====================

  // 计算步骤的哈希值
  const calculateStepsHash = useCallback((steps: any[]): string => {
    return JSON.stringify(steps, Object.keys(steps).sort())
  }, [])

  // 检查当前节点状态是否与初始状态相同（比较节点顺序 + data 内容）
  const checkIfHasChanges = useCallback((currentNodes: GraphNode[]) => {
    if (!originalStepsHash) {
      return false
    }

    // 比较关键数据：节点顺序（通过排序后的 ID 列表）和业务数据
    const currentSnapshot = JSON.stringify({
      // 节点顺序：按 parentId + branchType + position 排序后的 ID 列表（与初始化一致）
      order: currentNodes
        .filter(n => !n.id.includes('-group-'))
        .sort((a, b) => {
          if (a.data.parentId !== b.data.parentId) return (a.data.parentId || '').localeCompare(b.data.parentId || '')
          if (a.data.branchType !== b.data.branchType) return (a.data.branchType || '').localeCompare(b.data.branchType || '')
          if (!a.data.parentId && !b.data.parentId) return a.position.x - b.position.x
          return a.position.y - b.position.y
        })
        .map(n => n.id),
      // 业务数据：按 ID 排序
      data: currentNodes
        .filter(n => !n.id.includes('-group-'))
        .map(n => ({
          id: n.id,
          data: {
            name: n.data.name,
            action: n.data.action,
            description: n.data.description,
            if: n.data.if,
            loop: n.data.loop,
            run: n.data.run,
            message: n.data.message,
            level: n.data.level,
            url: n.data.url,
            method: n.data.method,
            body: n.data.body,
            script: n.data.script,
            shell: n.data.shell,
            duration: n.data.duration,
            items: n.data.items,
            item_var: n.data.item_var,
          },
        }))
        .sort((a, b) => a.id.localeCompare(b.id))
    })

    const hasChanges = currentSnapshot !== originalStepsHash
    if (hasChanges) {
      // 详细比较找出差异
      try {
        const orig = JSON.parse(originalStepsHash)
        const curr = JSON.parse(currentSnapshot)


        // 比较顺序
        if (JSON.stringify(orig.order) !== JSON.stringify(curr.order)) {
          // 顺序差异当前无需处理（保留比较占位）
        }

        // 比较数据
        if (JSON.stringify(orig.data) !== JSON.stringify(curr.data)) {
          // 找出具体哪个字段变化
          for (let i = 0; i < Math.max(orig.data.length, curr.data.length); i++) {
            const o = orig.data[i]
            const c = curr.data[i]
            if (!o || !c || o.id !== c.id) {
              continue
            }
            Object.keys(o.data).forEach(key => {
              if (JSON.stringify(o.data[key]) !== JSON.stringify(c.data[key])) {
                // 字段级差异定位暂未使用（保留比较占位）
              }
            })
          }
        }
      } catch (e) {
        console.error('[checkIfHasChanges] Error parsing snapshots:', e)
      }
    }

    return hasChanges
  }, [originalStepsHash])

  // ==================== 节点操作 ====================

  // 添加主流程节点 - 默认添加到末尾
  const addRootNode = useCallback(() => {
    const rootNodes = nodes.filter(n => !n.data.parentId)
    const nextIndex = rootNodes.length

    // 计算新节点的位置（在最后一个主流程节点后面）
    let newX = H_SPACING
    let newY = V_SPACING

    if (rootNodes.length > 0) {
      const lastNode = rootNodes[rootNodes.length - 1]
      newX = lastNode.position.x + NODE_WIDTH + H_SPACING
      newY = lastNode.position.y  // 同一水平线
    }

    const nodeName = `step-${nextIndex + 1}`
    const defaultAction = 'log'  // 默认为 log action
    const defaultHeight = calculateNodeHeight({ name: nodeName, action: defaultAction })

    const newNode: GraphNode = {
      id: generateId(),
      type: 'editorNode',
      position: { x: newX, y: newY },
      data: {
        name: nodeName,
        action: defaultAction,
        branchIndex: nextIndex,
        _calculatedHeight: defaultHeight,
      },
    }

    setNodes(prev => {
      const newNodes = [...prev, newNode]
      const { nodes: laidOutNodes, edges: newEdges } = calculateLayout(newNodes)
      setEdges(newEdges)
      return laidOutNodes
    })
    setHasUnsavedChanges(true)
  }, [nodes])

  // 添加子节点
  const addChildNode = useCallback((parentId: string, branchType: 'then' | 'else' | 'parallel' | 'do') => {
    setNodes(prev => {
      const parent = prev.find(n => n.id === parentId)
      if (!parent) return prev

      // 获取该分支现有的子节点
      const existingChildren = prev.filter(
        n => n.data.parentId === parentId && n.data.branchType === branchType
      )

      const nodeNamePrefix = branchType === 'then' ? 'then' : branchType === 'else' ? 'else' : branchType === 'parallel' ? 'task' : 'step'
      const nodeName = `${nodeNamePrefix}-${existingChildren.length + 1}`
      const defaultAction = 'log'  // 默认为 log action

      const newNode: GraphNode = {
        id: generateId(),
        type: 'editorNode',
        position: { x: 0, y: 0 }, // 位置会在布局时计算
        data: {
          name: nodeName,
          action: defaultAction,
          parentId,
          branchType,
          branchIndex: existingChildren.length,
          _calculatedHeight: calculateNodeHeight({ name: nodeName, action: defaultAction }),
        },
      }

      const newNodes = [...prev, newNode]
      const { nodes: laidOutNodes, edges: newEdges } = calculateLayout(newNodes)
      setEdges(newEdges)
      return laidOutNodes
    })
    setHasUnsavedChanges(true)
  }, [])

  // 删除节点（递归删除子节点和容器）
  const deleteNode = useCallback((nodeId: string) => {
    setNodes(prev => {
      // 找到所有后代节点
      const findDescendants = (id: string): string[] => {
        const children = prev.filter(n => n.data.parentId === id)
        let ids = children.map(c => c.id)
        children.forEach(child => {
          ids = [...ids, ...findDescendants(child.id)]
        })
        return ids
      }

      const descendantIds = findDescendants(nodeId)
      const idsToDelete = [nodeId, ...descendantIds]

      // 如果是 Parallel/Foreach 节点，还要删除对应的容器节点
      const nodeToDelete = prev.find(n => n.id === nodeId)
      if (nodeToDelete && (nodeToDelete.data.action === 'parallel' || nodeToDelete.data.action === 'foreach' || nodeToDelete.data.action === 'loop')) {
        const containerId = nodeToDelete.data.action === 'parallel'
          ? `parallel-group-${nodeId}`
          : `foreach-group-${nodeId}`
        idsToDelete.push(containerId)
      }

      const newNodes = prev.filter(n => !idsToDelete.includes(n.id))
      const { nodes: laidOutNodes, edges: newEdges } = calculateLayout(newNodes)
      setEdges(newEdges)
      return laidOutNodes
    })
    setHasUnsavedChanges(true)
  }, [])

  // 更新节点数据
  const updateNodeData = useCallback((nodeId: string, newData: Partial<GraphNodeData>) => {
    setNodes(prev => {
      const node = prev.find(n => n.id === nodeId)
      if (!node) return prev

      // 计算新高度
      const mergedData = { ...node.data, ...newData }
      const newHeight = calculateNodeHeight(mergedData)

      // 更新节点数据，包含计算的高度
      const updatedNodes = prev.map(n => {
        if (n.id === nodeId) {
          return {
            ...n,
            data: {
              ...n.data,
              ...newData,
              _calculatedHeight: newHeight,
            }
          }
        }
        return n
      })

      // 重新布局以应用新高度
      const { nodes: laidOutNodes, edges: newEdges } = calculateLayout(updatedNodes)
      setEdges(newEdges)

      // 智能检测：只有真正有变化时才设置 hasUnsavedChanges
      const hasChanges = checkIfHasChanges(laidOutNodes)
      setHasUnsavedChanges(hasChanges)

      return laidOutNodes
    })
  }, [checkIfHasChanges])

  // 处理节点变化（拖动、选择等）
  const onNodesChange = useCallback((changes: NodeChange<GraphNode>[]) => {
    setNodes((prev) => {
      // 过滤掉容器节点的变化（容器节点不可拖动）
      // （'add' 类型的 change 没有 id 字段，用类型谓词收窄出必带 id 的 change）
      const validChanges = changes.filter(
        (change): change is NodeChange<GraphNode> & { id: string } =>
          'id' in change && !change.id.startsWith('parallel-group-') && !change.id.startsWith('foreach-group-')
      )

      if (validChanges.length === 0) return prev

      // 创建全新的节点数组，确保 React 能检测到变化
      return prev.map((node) => {
        const change = validChanges.find(c => c.id === node.id)
        if (change && change.type === 'position' && change.position) {
          return {
            ...node,
            position: {
              x: change.position.x,
              y: change.position.y,
            },
            positionAbsolute: {
              x: change.position.x,
              y: change.position.y,
            },
          }
        }
        return node
      })
    })
    // 注意：不在这里设置 hasUnsavedChanges，因为 React Flow 初始化时也会触发 onNodesChange
    // 只有在用户明确操作（拖动结束、修改数据）时才设置
  }, [])

  // 处理节点拖动结束 - 根据坐标重新计算 branchIndex
  const onNodeDragStop = useCallback((_event: ReactMouseEvent, node: GraphNode) => {

    // 先恢复所有节点层级
    setNodes(prev => prev.map(n => ({ ...n, style: { ...n.style, zIndex: 1 } })))

    // 然后根据拖动重新排序
    setNodes(prev => {
      const draggedNode = prev.find(n => n.id === node.id)
      if (!draggedNode) {
        return prev
      }

      const newNodes = [...prev]

      if (!draggedNode.data.parentId) {
        // 主流程节点：按 x 坐标排序（水平布局）
        const rootNodes = newNodes.filter(n => !n.data.parentId)

        // 保存拖动前的节点 ID 顺序（按 position.x 排序）
        const beforeOrder = [...rootNodes].sort((a, b) => a.position.x - b.position.x).map(n => n.id)

        // 根据拖动后的 x 坐标重新排序
        rootNodes.sort((a, b) => a.position.x - b.position.x)
        const afterOrder = rootNodes.map(n => n.id)

        // 检查顺序是否真的改变
        const orderChanged = JSON.stringify(beforeOrder) !== JSON.stringify(afterOrder)

        // 总是更新 branchIndex（用于 nodesToSteps 排序）
        rootNodes.forEach((node, index) => {
          node.data.branchIndex = index
        })

        // 重新布局，让节点按新顺序整齐排列
        const { nodes: laidOutNodes } = calculateLayout(newNodes)

        // 智能检测：只有顺序真正改变时才设置 hasUnsavedChanges
        setTimeout(() => {
          const hasChanges = orderChanged ? true : checkIfHasChanges(laidOutNodes)
          setHasUnsavedChanges(hasChanges)
        }, 0)

        return laidOutNodes
      } else {
        // 子节点：按 y 坐标排序（垂直布局）
        const { parentId, branchType } = draggedNode.data
        const siblings = newNodes.filter(
          n => n.data.parentId === parentId && n.data.branchType === branchType
        )

        // 保存拖动前的节点 ID 顺序（按 position.y 排序）
        const beforeOrder = [...siblings].sort((a, b) => a.position.y - b.position.y).map(n => n.id)

        // 根据拖动后的 y 坐标重新排序
        siblings.sort((a, b) => a.position.y - b.position.y)
        const afterOrder = siblings.map(n => n.id)

        // 检查顺序是否真的改变
        const orderChanged = JSON.stringify(beforeOrder) !== JSON.stringify(afterOrder)

        // 总是更新 branchIndex（用于 nodesToSteps 排序）
        siblings.forEach((node, index) => {
          node.data.branchIndex = index
        })

        // 重新布局，让子节点按新顺序整齐排列
        const { nodes: laidOutNodes } = calculateLayout(newNodes)

        // 智能检测：只有顺序真正改变时才设置 hasUnsavedChanges
        setTimeout(() => {
          const hasChanges = orderChanged ? true : checkIfHasChanges(laidOutNodes)
          setHasUnsavedChanges(hasChanges)
        }, 0)

        return laidOutNodes
      }
    })

    // 延迟更新边（等待节点更新完成）
    setTimeout(() => {
      setNodes(currentNodes => {
        const { edges: newEdges } = calculateLayout(currentNodes)
        setEdges(newEdges)
        return currentNodes
      })
    }, 50)
  }, [checkIfHasChanges])

  // 处理节点拖动开始 - 提升节点层级
  const onNodeDragStart = useCallback((_event: ReactMouseEvent, node: GraphNode) => {
    // 提升拖动节点的层级，避免被遮挡
    setNodes(prev => prev.map(n => {
      if (n.id === node.id) {
        return {
          ...n,
          style: { ...n.style, zIndex: 100 },
        }
      }
      return n
    }))
  }, [])

  // 处理连接 - 禁用手动连线，连线由布局自动生成
  const onConnect = useCallback(() => {
    // 不执行任何操作，连线由 calculateLayout 中的 buildEdges 自动生成
  }, [])

  // ==================== 保存/加载 ====================

  // 验证节点数据
  const validateNodes = useCallback((): { valid: boolean; errors: string[] } => {
    const errors: string[] = []

    nodes.forEach(node => {
      // 跳过容器节点
      if (node.id.includes('-group-')) {
        return
      }

      if (!node.data.name || node.data.name.trim() === '') {
        errors.push(`节点 ${node.id} 缺少名称`)
      }
      if (!node.data.action || node.data.action.trim() === '') {
        errors.push(`节点 "${node.data.name || node.id}" 缺少 action 类型`)
      }

      // 验证必要字段
      if (node.data.action === 'http' && !node.data.url) {
        errors.push(`节点 "${node.data.name}" (HTTP) 缺少 URL`)
      }
      if (node.data.action === 'shell' && !node.data.shell) {
        errors.push(`节点 "${node.data.name}" (Shell) 缺少命令`)
      }
      if (node.data.action === 'log' && !node.data.message) {
        errors.push(`节点 "${node.data.name}" (Log) 缺少消息内容`)
      }
      if (node.data.action === 'sleep' && !node.data.duration) {
        errors.push(`节点 "${node.data.name}" (Sleep) 缺少时长`)
      }
    })

    return { valid: errors.length === 0, errors }
  }, [nodes])

  // 保存
  const handleSave = useCallback(() => {
    // 先验证
    const validation = validateNodes()
    if (!validation.valid) {
      alert('验证失败，请修复以下错误后再保存：\n\n' + validation.errors.join('\n'))
      return
    }

    // 使用新的 DAG 转换器
    const steps = dagToYaml(nodes)
    const yamlObj = { steps }
    onSave?.(yamlObj)

    // 保存后更新状态
    setHasUnsavedChanges(false)
    setOriginalStepsHash(calculateStepsHash(steps))
  }, [nodes, onSave, calculateStepsHash, validateNodes])

  // 运行
  const handleRun = useCallback(() => {
    // 先验证
    const validation = validateNodes()
    if (!validation.valid) {
      alert('验证失败，请修复以下错误后再运行：\n\n' + validation.errors.join('\n'))
      return
    }

    // 使用新的 DAG 转换器
    const steps = dagToYaml(nodes)
    const yamlObj = { steps }
    onRun?.(yamlObj)
  }, [nodes, onRun, validateNodes])

  // ==================== 初始化 ====================

  useEffect(() => {
    if (initialSteps && initialSteps.length > 0) {
      // 使用新的 DAG 转换器（自动推断依赖）
      const dagNodes = yamlToDAG({ steps: initialSteps })
      const { nodes: laidOutNodes, edges: newEdges } = calculateLayout(dagNodes)
      setNodes(laidOutNodes)
      setEdges(newEdges)

      // 保存初始状态快照：只比较节点顺序（通过排序后的 ID 列表）和业务数据
      const snapshot = JSON.stringify({
        // 节点顺序：按 parentId + branchType + position 排序后的 ID 列表
        order: laidOutNodes
          .filter(n => !n.id.includes('-group-'))
          .sort((a, b) => {
            if (a.data.parentId !== b.data.parentId) return (a.data.parentId || '').localeCompare(b.data.parentId || '')
            if (a.data.branchType !== b.data.branchType) return (a.data.branchType || '').localeCompare(b.data.branchType || '')
            if (!a.data.parentId && !b.data.parentId) return a.position.x - b.position.x
            return a.position.y - b.position.y
          })
          .map(n => n.id),
        // 业务数据：按 ID 排序
        data: laidOutNodes
          .filter(n => !n.id.includes('-group-'))
          .map(n => ({
            id: n.id,
            data: {
              name: n.data.name,
              action: n.data.action,
              description: n.data.description,
              if: n.data.if,
              loop: n.data.loop,
              run: n.data.run,
              message: n.data.message,
              level: n.data.level,
              url: n.data.url,
              method: n.data.method,
              body: n.data.body,
              script: n.data.script,
              shell: n.data.shell,
              duration: n.data.duration,
              items: n.data.items,
              item_var: n.data.item_var,
            },
          }))
          .sort((a, b) => a.id.localeCompare(b.id))
      })
      setOriginalStepsHash(snapshot)

      setHasUnsavedChanges(false)  // 明确设置为未修改状态
    } else {
      // 无初始步骤：保持空画布
    }
  }, [initialSteps])

  // 节点类型映射
  const nodeTypes = useMemo(() => ({
    editorNode: (props: NodeProps) => (
      <EditorNode
        id={props.id}
        data={props.data as unknown as GraphNodeData}
        selected={props.selected}
        onAddChild={addChildNode}
        onDelete={deleteNode}
        onDataChange={updateNodeData}
      />
    ),
    parallelGroup: (props: NodeProps) => (
      <ParallelGroupNode
        id={props.id}
        data={props.data as unknown as ParallelGroupNodeData}
      />
    ),
    foreachGroup: (props: NodeProps) => (
      <ForeachGroupNode
        id={props.id}
        data={props.data as unknown as ForeachGroupNodeData}
      />
    ),
  }), [addChildNode, deleteNode, updateNodeData])

  // ==================== 渲染 ====================

  return (
    <div className="w-full h-full bg-gray-50 dark:bg-gray-900 relative">
      {/* 顶部工具栏 */}
      <div className="absolute top-4 left-4 right-4 z-10 flex items-center justify-between">
        <div className="flex items-center gap-2">
          <button
            onClick={addRootNode}
            className="flex items-center gap-2 px-4 py-2 bg-blue-500 hover:bg-blue-600 text-white rounded-lg shadow-lg transition-colors"
          >
            <Plus className="w-4 h-4" />
            Add Step
          </button>

          {/* 未保存提示 */}
          {hasUnsavedChanges && (
            <div className="flex items-center gap-2 px-3 py-1.5 bg-yellow-100 dark:bg-yellow-900/30 text-yellow-700 dark:text-yellow-300 rounded-lg text-sm">
              <AlertCircle className="w-4 h-4" />
              <span>Unsaved changes</span>
            </div>
          )}
          {!hasUnsavedChanges && originalStepsHash !== '' && (
            <div className="flex items-center gap-2 px-3 py-1.5 bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-300 rounded-lg text-sm">
              <CheckCircle className="w-4 h-4" />
              <span>Saved</span>
            </div>
          )}
        </div>

        <div className="flex items-center gap-2">
          <button
            onClick={handleRun}
            className="flex items-center gap-2 px-4 py-2 bg-green-500 hover:bg-green-600 text-white rounded-lg shadow-lg transition-colors"
          >
            <Play className="w-4 h-4" />
            Run
          </button>
          <button
            onClick={handleSave}
            disabled={!hasUnsavedChanges}
            className={`flex items-center gap-2 px-4 py-2 rounded-lg shadow-lg transition-colors ${
              hasUnsavedChanges
                ? 'bg-blue-500 hover:bg-blue-600 text-white'
                : 'bg-gray-300 dark:bg-gray-700 text-gray-500 dark:text-gray-400 cursor-not-allowed'
            }`}
          >
            <Save className="w-4 h-4" />
            Save
          </button>
        </div>
      </div>

      {/* ReactFlow 画布 */}
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        onNodesChange={onNodesChange}
        onConnect={onConnect}
        onNodeDragStart={onNodeDragStart}
        onNodeDragStop={onNodeDragStop}
        fitView
        snapToGrid
        snapGrid={[15, 15]}
        nodesDraggable={true}
        elementsSelectable={true}
        nodesConnectable={false}  // 禁用连线功能，连线由布局自动生成
        defaultEdgeOptions={{
          type: 'bezier',
          style: { stroke: '#9ca3af', strokeWidth: 2 },
          markerEnd: { type: MarkerType.ArrowClosed, color: '#9ca3af' },
        }}
        className="bg-transparent"
      >
        <Background variant={BackgroundVariant.Dots} gap={20} size={1} />
        <Controls />
      </ReactFlow>
    </div>
  )
}
