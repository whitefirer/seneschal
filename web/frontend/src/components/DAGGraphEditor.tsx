// DAG 图形编辑器（纯图形，无 YAML）
import { useState, useCallback, useEffect, useRef, useMemo } from 'react'
import {
  ReactFlow,
  Background,
  Controls,
  MiniMap,
  BackgroundVariant,
  Connection,
  Edge,
  useReactFlow,
  Node,
  Position,
  getBezierPath,
  MarkerType,
  NodeChange,
  applyNodeChanges,
} from '@xyflow/react'
import { Plus, Save, Play, AlertCircle, GitGraph, Layers, Repeat } from 'lucide-react'
import { DAGNodeData, WorkflowYAML } from '../types/dag'
import { DAGNode, calculateNodeHeight } from './DAGNode'
import { calculateDAGLayout, detectCycles } from '../utils/dag-layout'
import { yamlToDAG, dagToYaml, inferDependencies } from '../utils/yaml-converter'

// ==================== 容器节点组件 ====================

// Parallel 容器节点（精确对齐执行页面 WorkflowGraph.tsx）
function ParallelGroup({ data }: { data: { width: number; height: number; taskCount: number } }) {
  return (
    <div
      className="rounded-xl border-2 border-dashed border-purple-400 dark:border-purple-500 bg-purple-50/30 dark:bg-purple-900/10"
      style={{
        width: data.width,
        height: data.height,
        zIndex: -1,
      }}
    >
      {/* 并行标记 */}
      <div className="absolute -top-3 left-3 px-2 py-0.5 bg-purple-100 dark:bg-purple-900 rounded text-xs font-medium text-purple-700 dark:text-purple-300 flex items-center gap-1 z-10">
        <Layers className="w-3 h-3" />
        <span>并行</span>
        {data.taskCount && (
          <span className="text-purple-500">({data.taskCount} 并行任务)</span>
        )}
      </div>
    </div>
  )
}

// Foreach 容器节点（精确对齐执行页面 WorkflowGraph.tsx）
function ForeachGroup({ data }: { data: { width: number; height: number; iterationCount: number } }) {
  return (
    <div
      className="rounded-xl border-2 border-dashed border-cyan-400 dark:border-cyan-500 bg-cyan-50/30 dark:bg-cyan-900/10"
      style={{
        width: data.width,
        height: data.height,
        zIndex: -1,
      }}
    >
      {/* 循环标记 */}
      <div className="absolute -top-3 left-3 px-2 py-0.5 bg-cyan-100 dark:bg-cyan-900 rounded text-xs font-medium text-cyan-700 dark:text-cyan-300 flex items-center gap-1 z-10">
        <Repeat className="w-3 h-3" />
        <span>循环体</span>
        {data.iterationCount && (
          <span className="text-cyan-500">({data.iterationCount} 次)</span>
        )}
      </div>
    </div>
  )
}

// ==================== 自定义边（与原编辑器风格一致） ====================

function HollowEdge({ id, sourceX, sourceY, targetX, targetY, sourcePosition, targetPosition, style, selected, onClick }: {
  id: string
  sourceX: number
  sourceY: number
  targetX: number
  targetY: number
  sourcePosition: Position
  targetPosition: Position
  style?: { stroke?: string }
  selected?: boolean
  onClick?: (e: React.MouseEvent) => void
}) {
  const [edgePath] = getBezierPath({
    sourceX,
    sourceY,
    sourcePosition,
    targetX,
    targetY,
    targetPosition,
  })

  // 计算边的中点（用于显示删除按钮）
  const midX = (sourceX + targetX) / 2
  const midY = (sourceY + targetY) / 2

  return (
    <g onClick={onClick} style={{ cursor: 'pointer' }}>
      <path
        id={`${id}-outer`}
        className="hollow-edge-outer"
        d={edgePath}
        strokeWidth={selected ? 6 : 4}
        stroke={selected ? '#ef4444' : (style?.stroke || '#3b82f6')}
        fill="none"
      />
      <path
        id={`${id}-inner`}
        className="hollow-edge-inner"
        d={edgePath}
        strokeWidth={2}
        stroke="white"
        fill="none"
      />
      {/* 选中时显示删除提示 */}
      {selected && (
        <g transform={`translate(${midX}, ${midY})`}>
          <rect x="-12" y="-12" width="24" height="24" fill="#ef4444" rx="12" />
          <text x="0" y="4" textAnchor="middle" fill="white" fontSize="16" fontWeight="bold">×</text>
        </g>
      )}
    </g>
  )
}

// 节点类型映射
const NODE_WIDTH = 220

const nodeTypes = {
  dag: DAGNode,
  parallelGroup: ParallelGroup,
  foreachGroup: ForeachGroup,
}

// 使用 nodeTypes 防止 TypeScript 报错
void nodeTypes

// 边类型映射
const edgeTypes = {
  hollow: HollowEdge,
}

// 默认节点数据
const createDefaultNode = (position: { x: number; y: number }, name?: string): Node<DAGNodeData> => {
  const data: DAGNodeData = {
    name: name || `node-${Date.now().toString(36).substr(2, 6)}`,
    action: 'log',
  }
  const height = calculateNodeHeight(data)
  
  return {
    id: `node-${Date.now()}-${Math.random().toString(36).substr(2, 9)}`,
    type: 'dag',
    position,
    width: 220,
    height,
    data: {
      ...data,
      _calculatedHeight: height,
    },
  }
}

interface DAGGraphEditorProps {
  initialSteps?: any[]
  onSave?: (yaml: WorkflowYAML) => void
  onRun?: (yaml: WorkflowYAML) => void
  readOnly?: boolean
}

const DAGGraphEditor = ({ initialSteps, onSave, onRun, readOnly = false }: DAGGraphEditorProps) => {
  const reactFlowWrapper = useRef<HTMLDivElement>(null)
  const [nodes, setNodes] = useState<Node<DAGNodeData>[]>([])
  const [edges, setEdges] = useState<Edge[]>([])
  const [selectedNodeIds, setSelectedNodeIds] = useState<string[]>([])
  const [selectedEdgeIds, setSelectedEdgeIds] = useState<string[]>([])
  const [hasUnsavedChanges, setHasUnsavedChanges] = useState(false)
  const [cycleError, setCycleError] = useState<string | null>(null)
  
  const { screenToFlowPosition } = useReactFlow()
  
  // 处理节点变化（拖动、选择等）
  const onNodesChange = useCallback((changes: NodeChange[]) => {
    setNodes((nds) => applyNodeChanges(changes, nds) as Node<DAGNodeData>[])
    // 检查是否有位置变化
    const hasPositionChange = changes.some(c => c.type === 'position' && c.position)
    if (hasPositionChange) {
      setHasUnsavedChanges(true)
    }
  }, [])
  
  // ==================== 初始化 ====================
  
  useEffect(() => {
    if (initialSteps && initialSteps.length > 0) {
      // 1. YAML → DAG 转换（所有子节点都是独立节点）
      const dagNodes = yamlToDAG({ steps: initialSteps } as any)
      
      // 2. 推断依赖关系（为子节点设置 depends_on）
      inferDependencies(dagNodes as any)
      
      // 3. 计算布局（包含容器节点和所有边）
      const { nodes: laidOutNodes, edges: laidOutEdges } = calculateDAGLayout(dagNodes as any)
      
      // 设置节点类型（保留布局算法设置的 type）
      const typedNodes = laidOutNodes.map(node => {
        const calculatedHeight = node.data?._calculatedHeight || calculateNodeHeight(node.data || {})
        
        return {
          ...node,
          type: node.type || 'dag',
          position: node.position || { x: 0, y: 0 },
          draggable: node.type !== 'parallelGroup' && node.type !== 'foreachGroup',
          selectable: node.type !== 'parallelGroup' && node.type !== 'foreachGroup',
          width: node.width || (node.type === 'dag' ? NODE_WIDTH : node.data?.width),
          height: node.height || (node.type === 'dag' ? calculatedHeight : node.data?.height),
          data: node.data || {},
        }
      })
      
      setNodes(typedNodes as Node<DAGNodeData>[])
      
      // 使用布局算法生成的边
      const finalEdges = laidOutEdges.map((edge: any) => ({
        ...edge,
        markerEnd: edge.markerEnd || { type: MarkerType.ArrowClosed, color: edge.style?.stroke || '#3b82f6' },
      }))
      
      setEdges(finalEdges)
      setHasUnsavedChanges(false)
    }
  }, [initialSteps])
  
  // ==================== 循环检测 ====================
  
  const checkForCycles = useCallback((currentNodes: Node<DAGNodeData>[]): boolean => {
    const cycles = detectCycles(currentNodes as any)
    if (cycles.length > 0) {
      const cycleStr = cycles.map(c => c.join(' → ')).join(', ')
      setCycleError(`Circular dependency detected: ${cycleStr}`)
      return true
    }
    setCycleError(null)
    return false
  }, [])
  
  // ==================== 节点操作 ====================
  
  // 添加节点（全局）
  const handleAddNode = useCallback(() => {
    if (!reactFlowWrapper.current) return
    
    const rect = reactFlowWrapper.current.getBoundingClientRect()
    
    // 计算新节点位置：如果有节点，放在最右边节点的右侧；否则放在中心
    let position = screenToFlowPosition({
      x: rect.width / 2,
      y: rect.height / 2,
    })
    
    if (nodes.length > 0) {
      // 找到最右边的节点
      const rightmostNode = nodes.reduce((max, node) => 
        (node.position?.x || 0) > (max.position?.x || 0) ? node : max
      , nodes[0])
      
      position = {
        x: (rightmostNode.position?.x || 0) + 300,  // H_SPACING
        y: rightmostNode.position?.y || rect.height / 2,
      }
    }
    
    const newNode = createDefaultNode(position)
    setNodes(nodes => [...nodes, newNode])
    setHasUnsavedChanges(true)
  }, [screenToFlowPosition, nodes, createDefaultNode])
  
  // 添加下一个节点（从节点上的 + 按钮调用）
  const handleAddNextNode = useCallback((sourceNodeId: string) => {
    const newNodeId = `node-${Date.now()}-${Math.random().toString(36).substr(2, 9)}`
    const newTimestamp = Date.now()
    
    // 获取源节点
    const sourceNode = nodes.find(n => n.id === sourceNodeId)
    if (!sourceNode) return
    
    // 创建新节点数据并计算高度
    const newNodeData: DAGNodeData = {
      name: `node-${newTimestamp.toString(36).substr(2, 6)}`,
      action: 'log',
      depends_on: [sourceNodeId],
    }
    const newNodeHeight = calculateNodeHeight(newNodeData)
    
    const newNode: Node<DAGNodeData> = {
      id: newNodeId,
      type: 'dag',
      position: { x: 0, y: 0 },
      width: 220,
      height: newNodeHeight,
      data: {
        ...newNodeData,
        _calculatedHeight: newNodeHeight,
      },
    }
    
    const newEdge: Edge = {
      id: `${sourceNodeId}-${newNodeId}`,
      source: sourceNodeId,
      target: newNodeId,
      type: 'default',
      markerEnd: {
        type: MarkerType.ArrowClosed,
        color: '#3b82f6',
      },
      style: { stroke: '#3b82f6', strokeWidth: 2 },
    }
    
    // 添加新节点并重新计算布局
    setNodes(currentNodes => {
      // 先添加新节点并更新源节点的 next
      const nodesWithNew = currentNodes.map(node => {
        if (node.id === sourceNodeId) {
          return {
            ...node,
            data: {
              ...node.data,
              next: [...(node.data.next || []), newNodeId],
            },
          }
        }
        return node
      })
      nodesWithNew.push(newNode)
      
      // 使用完整的 DAG 布局算法重新计算所有节点位置
      const layoutResult = calculateDAGLayout(nodesWithNew as any)
      const laidOutNodes = layoutResult.nodes || nodesWithNew
      
      // 应用布局结果，保留节点原有的 type（dag / parallelGroup / foreachGroup）
      const finalNodes = laidOutNodes.map(node => {
        const calculatedHeight = node.data?._calculatedHeight || calculateNodeHeight(node.data || {})
        return {
          ...node,
          type: node.type || 'dag',  // 保留布局算法设置的 type
          draggable: node.type !== 'parallelGroup' && node.type !== 'foreachGroup',
          selectable: node.type !== 'parallelGroup' && node.type !== 'foreachGroup',
          width: node.width || (node.type === 'dag' ? 220 : node.data?.width),
          height: node.height || (node.type === 'dag' ? calculatedHeight : node.data?.height),
          data: {
            ...node.data,
            _calculatedHeight: calculatedHeight,
          },
        }
      })
      
      return finalNodes as Node<DAGNodeData>[]
    })
    
    setEdges(currentEdges => [...currentEdges, newEdge])
    setHasUnsavedChanges(true)
  }, [nodes])
  
  // 添加子节点（用于 Condition/Parallel/Foreach）
  // 参照原编辑器：所有子节点都是独立节点，通过 parentId 关联
  const handleAddChildNode = useCallback((parentId: string, branchType: 'then' | 'else' | 'parallel' | 'do') => {
    // 获取父节点
    const parentNode = nodes.find(n => n.id === parentId)
    if (!parentNode) return
    
    const childNodeId = `node-${Date.now()}-${Math.random().toString(36).substr(2, 9)}`
    
    // 获取该分支现有的子节点数量
    const existingChildren = nodes.filter(
      n => n.data.parentId === parentId && n.data.branchType === branchType
    )
    
    // 生成节点名
    const nodeNamePrefix = branchType === 'then' ? 'then' : branchType === 'else' ? 'else' : branchType === 'parallel' ? 'task' : 'step'
    const nodeName = `${nodeNamePrefix}-${existingChildren.length + 1}`
    
    // 创建子节点数据（独立节点）
    const childNodeData: DAGNodeData = {
      name: nodeName,
      action: 'log',
      depends_on: branchType === 'then' || branchType === 'else' ? [parentId] : undefined,
      parentId: parentId,
      branchType: branchType,
      branchIndex: existingChildren.length,
    }
    const childNodeHeight = calculateNodeHeight(childNodeData)
    
    const childNode: Node<DAGNodeData> = {
      id: childNodeId,
      type: 'dag',
      position: { x: 0, y: 0 },
      width: 220,
      height: childNodeHeight,
      data: {
        ...childNodeData,
        _calculatedHeight: childNodeHeight,
      },
    }
    
    // Condition 分支需要连线，Parallel/Foreach 子节点由布局算法处理
    const needsEdge = branchType === 'then' || branchType === 'else'
    
    // 添加子节点并重新计算布局
    setNodes(currentNodes => {
      const nodesWithNew = currentNodes.map(node => {
        if (node.id === parentId) {
          return {
            ...node,
            data: {
              ...node.data,
              next: needsEdge ? [...(node.data.next || []).filter(id => id !== childNodeId), childNodeId] : node.data.next,
            },
          }
        }
        return node
      })
      nodesWithNew.push(childNode)
      
      // 重新布局
      const layoutResult = calculateDAGLayout(nodesWithNew as any)
      const laidOutNodes = layoutResult.nodes || nodesWithNew
      
      // 应用布局结果，保留节点原有的 type（dag / parallelGroup / foreachGroup）
      const finalNodes = laidOutNodes.map(node => {
        const calculatedHeight = node.data?._calculatedHeight || calculateNodeHeight(node.data || {})
        return {
          ...node,
          type: node.type || 'dag',
          draggable: node.type !== 'parallelGroup' && node.type !== 'foreachGroup',
          selectable: node.type !== 'parallelGroup' && node.type !== 'foreachGroup',
          width: node.width || (node.type === 'dag' ? 220 : node.data?.width),
          height: node.height || (node.type === 'dag' ? calculatedHeight : node.data?.height),
          data: {
            ...node.data,
            _calculatedHeight: calculatedHeight,
          },
        }
      })
      
      return finalNodes as Node<DAGNodeData>[]
    })
    
    if (needsEdge) {
      const newEdge: Edge = {
        id: `${parentId}-${childNodeId}`,
        source: parentId,
        target: childNodeId,
        type: 'hollow',
        data: {
          branchType: branchType,
        },
        markerEnd: {
          type: MarkerType.ArrowClosed,
          color: '#3b82f6',
        },
        style: { stroke: '#3b82f6', strokeWidth: 2 },
      }
      setEdges(currentEdges => [...currentEdges, newEdge])
    }
    
    setHasUnsavedChanges(true)
  }, [nodes])
  
  // 使用 useRef 存储回调函数和 setters，避免 nodeTypes 频繁重新创建
  const onAddNextRefCurrent = useRef<(sourceNodeId: string) => void>()
  const onAddChildRefCurrent = useRef<(parentId: string, branchType: 'then' | 'else' | 'parallel' | 'do') => void>()
  onAddNextRefCurrent.current = handleAddNextNode
  onAddChildRefCurrent.current = handleAddChildNode
  
  const setNodesRef = useRef(setNodes)
  const setEdgesRef = useRef(setEdges)
  setNodesRef.current = setNodes
  setEdgesRef.current = setEdges
  
  // 使用 useMemo 缓存 nodeTypes，避免每次渲染都创建新对象
  const nodeTypes = useMemo(() => ({
    dag: (props: any) => (
      <DAGNode 
        {...props} 
        onAddNext={(sourceNodeId: string) => onAddNextRefCurrent.current?.(sourceNodeId)} 
        onAddChild={(parentId: string, branchType: 'then' | 'else' | 'parallel' | 'do') => onAddChildRefCurrent.current?.(parentId, branchType)}
        setNodes={setNodesRef.current} 
        setEdges={setEdgesRef.current} 
      />
    ),
    parallelGroup: ParallelGroup,
    foreachGroup: ForeachGroup,
  }), [])  // ✅ 空依赖，完全稳定
  
  // 删除节点
  const handleDeleteNode = useCallback((nodeId: string) => {
    setNodes(nodes => nodes.filter(n => n.id !== nodeId))
    setEdges(edges => edges.filter(e => e.source !== nodeId && e.target !== nodeId))
    
    // 清理其他节点的依赖
    setNodes(nodes => nodes.map(node => ({
      ...node,
      data: {
        ...node.data,
        next: (node.data.next || []).filter(id => id !== nodeId),
        depends_on: (node.data.depends_on || []).filter(id => id !== nodeId),
      },
    })))
    
    setHasUnsavedChanges(true)
  }, [])
  
  // ==================== 连线操作 ====================
  
  // 处理连线
  const handleConnect = useCallback((connection: Connection) => {
    const { source, target } = connection
    
    if (!source || !target) return
    
    // 检查是否已存在
    const exists = edges.some(e => e.source === source && e.target === target)
    if (exists) return
    
    // 添加依赖关系到节点并重新布局
    setNodes(nodes => {
      const updatedNodes = nodes.map(node => {
        if (node.id === source) {
          return {
            ...node,
            data: {
              ...node.data,
              next: [...(node.data.next || []).filter(id => id !== target), target],
            },
          }
        }
        if (node.id === target) {
          return {
            ...node,
            data: {
              ...node.data,
              depends_on: [...(node.data.depends_on || []).filter(id => id !== source), source],
            },
          }
        }
        return node
      })
      
      // 检查循环
      checkForCycles(updatedNodes)
      
      // 使用完整的 DAG 布局算法重新计算所有节点位置
      const layoutResult = calculateDAGLayout(updatedNodes as any)
      const laidOutNodes = layoutResult.nodes || updatedNodes
      
      // 应用布局结果，保留节点类型和高度信息
      const finalNodes = laidOutNodes.map(node => {
        const calculatedHeight = node.data._calculatedHeight || calculateNodeHeight(node.data)
        return {
          ...node,
          type: node.type === 'dag' || node.type === 'parallelGroup' || node.type === 'foreachGroup' ? node.type : 'dag',
          draggable: node.type === 'dag',
          selectable: node.type === 'dag',
          width: node.width || (node.type === 'dag' ? 220 : node.data?.width),
          height: node.height || (node.type === 'dag' ? calculatedHeight : node.data?.height),
          data: {
            ...node.data,
            _calculatedHeight: calculatedHeight,
          },
        }
      })
      
      return finalNodes as Node<DAGNodeData>[]
    })
    
    // 添加可视化边
    const newEdge: Edge = {
      id: `${source}-${target}`,
      source,
      target,
      type: 'default',
      markerEnd: {
        type: MarkerType.ArrowClosed,
        color: '#3b82f6',
      },
      style: { stroke: '#3b82f6', strokeWidth: 2 },
    }
    setEdges(edges => [...edges, newEdge])
    
    setHasUnsavedChanges(true)
  }, [edges, checkForCycles])
  
  // 处理删除边
  const handleDeleteEdge = useCallback((edgeId: string) => {
    const edge = edges.find(e => e.id === edgeId)
    if (!edge) return
    
    const { source, target } = edge
    
    setNodes(nodes => nodes.map(node => {
      if (node.id === source) {
        return {
          ...node,
          data: {
            ...node.data,
            next: (node.data.next || []).filter(id => id !== target),
          },
        }
      }
      if (node.id === target) {
        return {
          ...node,
          data: {
            ...node.data,
            depends_on: (node.data.depends_on || []).filter(id => id !== source),
          },
        }
      }
      return node
    }))
    
    setEdges(edges => edges.filter(e => e.id !== edgeId))
    setHasUnsavedChanges(true)
  }, [edges])
  
  // 处理边选择（支持按 Delete 键删除）
  const handleEdgeClick = useCallback((event: React.MouseEvent, edge: Edge) => {
    event.stopPropagation()
    setSelectedEdgeIds([edge.id])
    setSelectedNodeIds([])
  }, [])
  
  const handleSave = useCallback(() => {
    if (cycleError) {
      alert('Cannot save: Circular dependency detected. Please fix it first.')
      return
    }
    
    const steps = dagToYaml(nodes as any)
    onSave?.({ steps })
    setHasUnsavedChanges(false)
  }, [nodes, onSave, cycleError])
  
  const handleRun = useCallback(() => {
    if (cycleError) {
      alert('Cannot run: Circular dependency detected. Please fix it first.')
      return
    }
    
    const steps = dagToYaml(nodes as any)
    onRun?.({ steps })
  }, [nodes, onRun, cycleError])
  
  // ==================== 键盘事件 ====================
  
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Delete' || e.key === 'Backspace') {
        // 删除选中的边
        selectedEdgeIds.forEach(edgeId => {
          handleDeleteEdge(edgeId)
        })
        
        // 删除选中的节点
        selectedNodeIds.forEach(nodeId => {
          handleDeleteNode(nodeId)
        })
        
        setSelectedEdgeIds([])
        setSelectedNodeIds([])
      }
    }
    
    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [selectedEdgeIds, selectedNodeIds, handleDeleteEdge, handleDeleteNode])
  
  // ==================== 点击空白处 ====================
  
  const handlePaneClick = useCallback(() => {
    setSelectedNodeIds([])
    setSelectedEdgeIds([])
  }, [])
  
  // ==================== 渲染 ====================
  
  return (
    <div className="w-full h-full flex flex-col">
      {/* 工具栏 */}
      <div className="h-14 bg-white border-b px-4 flex items-center justify-between">
        <div className="flex items-center gap-4">
          <h2 className="text-lg font-semibold flex items-center gap-2">
            <GitGraph className="w-5 h-5 text-purple-500" />
            DAG Graph Editor
          </h2>
          
          {/* 节点计数 */}
          <div className="px-3 py-1 bg-gray-100 rounded text-xs font-mono">
            Nodes: {nodes.length} | Edges: {edges.length}
          </div>
          
          {/* 循环错误提示 */}
          {cycleError && (
            <div className="flex items-center gap-2 text-red-600 bg-red-50 px-3 py-1 rounded">
              <AlertCircle size={16} />
              <span className="text-sm">{cycleError}</span>
            </div>
          )}
          
          {/* 未保存提示 */}
          {hasUnsavedChanges && !cycleError && (
            <div className="flex items-center gap-2 text-orange-600 bg-orange-50 px-3 py-1 rounded">
              <span className="text-sm">Unsaved changes</span>
            </div>
          )}
        </div>
        
        <div className="flex items-center gap-2">
          {/* 添加节点按钮 */}
          <button
            onClick={handleAddNode}
            disabled={readOnly}
            className="flex items-center gap-2 px-4 py-2 bg-blue-500 hover:bg-blue-600 text-white rounded disabled:opacity-50 disabled:cursor-not-allowed"
          >
            <Plus size={18} />
            <span>Add Node</span>
          </button>
          
          {/* 保存按钮 */}
          <button
            onClick={handleSave}
            disabled={readOnly || !!cycleError}
            className="flex items-center gap-2 px-4 py-2 bg-green-500 hover:bg-green-600 text-white rounded disabled:opacity-50 disabled:cursor-not-allowed"
          >
            <Save size={18} />
            <span>Save</span>
          </button>
          
          {/* 运行按钮 */}
          <button
            onClick={handleRun}
            disabled={readOnly || !!cycleError}
            className="flex items-center gap-2 px-4 py-2 bg-purple-500 hover:bg-purple-600 text-white rounded disabled:opacity-50 disabled:cursor-not-allowed"
          >
            <Play size={18} />
            <span>Run</span>
          </button>
        </div>
      </div>
      
      {/* React Flow 画布 */}
      <div className="flex-1 relative" ref={reactFlowWrapper} style={{ height: 'calc(100vh - 200px)' }}>
        <ReactFlow
          nodes={nodes}
          edges={edges}
          nodeTypes={nodeTypes}
          edgeTypes={edgeTypes}
          onNodesChange={onNodesChange}
          defaultEdgeOptions={{
            type: 'hollow',
            markerEnd: {
              type: MarkerType.ArrowClosed,
              color: '#3b82f6',
            },
            style: { stroke: '#3b82f6', strokeWidth: 2 },
          }}
          nodesDraggable={true}
          nodesConnectable={true}
          elementsSelectable={true}
          selectNodesOnDrag={true}
          onNodeClick={(_, node) => {
            setSelectedNodeIds([node.id])
            setSelectedEdgeIds([])
          }}
          onEdgeClick={handleEdgeClick}
          onConnect={handleConnect}
          onPaneClick={handlePaneClick}
          onNodeDragStart={() => {
          }}
          onNodeDragStop={() => {
            setHasUnsavedChanges(true)
          }}
          onNodesDelete={(deleted) => {
            deleted.forEach(node => handleDeleteNode(node.id))
          }}
          onEdgesDelete={(deleted) => {
            deleted.forEach(edge => handleDeleteEdge(edge.id))
          }}
          fitView
          snapToGrid
          snapGrid={[15, 15]}
          deleteKeyCode={['Delete', 'Backspace']}
          className="bg-gray-50"
          style={{ width: '100%', height: '100%' }}
        >
          <Background variant={BackgroundVariant.Dots} gap={20} size={1} />
          <Controls />
          <MiniMap
            nodeColor={(node) => {
              const action = node.data?.action
              if (action === 'log') return '#fde047'
              if (action === 'shell') return '#4ade80'
              if (action === 'http') return '#60a5fa'
              return '#9ca3af'
            }}
            nodeStrokeColor="#4b5563"
            nodeBorderRadius={8}
            nodeStrokeWidth={2}
            maskColor="rgba(0, 0, 0, 0.15)"
            pannable
            zoomable
            style={{
              background: '#ffffff',
              borderRadius: '8px',
              boxShadow: '0 4px 6px -1px rgba(0, 0, 0, 0.1)',
            }}
            className="!border !border-gray-200"
          />
        </ReactFlow>
      </div>
    </div>
  )
}

export default DAGGraphEditor
