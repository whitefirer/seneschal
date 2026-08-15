// 图形编辑器画布：React Flow + 工具栏 + 保存/运行。
// 数据转换用 lib/stepGraph.ts（透传），节点 UI 用 StepNode.tsx。
import { useState, useEffect, useCallback, useMemo } from 'react'
import {
  ReactFlow, Background, Controls, MiniMap, BackgroundVariant,
  Connection, Edge, Node, MarkerType, useEdgesState,
} from '@xyflow/react'
import { Plus, Save, Play, GitGraph } from 'lucide-react'
import StepNode, { type StepNodeHandlers } from './StepNode'
import { stepsToGraph, graphToSteps, type StepGraphNode, type StepGraphEdge } from '@/lib/stepGraph'

const nodeTypes = { step: StepNode }

// 稳定节点 id 计数器
let nodeSeq = 0
const nextId = () => 'g' + nodeSeq++

interface GraphEditorProps {
  initialSteps: any[]
  onSave: (steps: any[]) => void
  onRun: (steps: any[]) => void
}

function makeNode(data: Record<string, any>, parentId?: string, branchType?: string, branchIndex?: number): StepGraphNode {
  return {
    id: nextId(),
    data: {
      name: data.name || 'step',
      action: data.action || 'log',
      ...data,
      __parentId: parentId,
      __branchType: branchType,
      __branchIndex: branchIndex ?? 0,
    },
  }
}

// 简单左→右布局（深度 = 到根的最长路径）
function layout(nodes: StepGraphNode[], edges: StepGraphEdge[]): Record<string, { x: number; y: number }> {
  const preds = new Map<string, string[]>()
  nodes.forEach((n) => preds.set(n.id, []))
  for (const e of edges) if (preds.has(e.target)) preds.get(e.target)!.push(e.source)
  nodes.forEach((n) => {
    const p = n.data.__parentId
    if (p && preds.has(n.id)) preds.get(n.id)!.push(p)
  })
  const depth = new Map<string, number>(nodes.map((n) => [n.id, 0]))
  for (let k = 0; k < 200; k++) {
    let changed = false
    for (const n of nodes) {
      let d = 0
      for (const p of preds.get(n.id) || []) d = Math.max(d, (depth.get(p) ?? 0) + 1)
      if (d !== depth.get(n.id)) { depth.set(n.id, d); changed = true }
    }
    if (!changed) break
  }
  const yCursor = new Map<number, number>()
  const pos: Record<string, { x: number; y: number }> = {}
  nodes.forEach((n) => {
    const d = depth.get(n.id) ?? 0
    const y = yCursor.get(d) ?? 0
    pos[n.id] = { x: d * 300, y: y * 160 }
    yCursor.set(d, y + 1)
  })
  return pos
}

export default function GraphEditor({ initialSteps, onSave, onRun }: GraphEditorProps) {
  const [rawNodes, setRawNodes] = useState<StepGraphNode[]>([])
  const [edges, setEdges, onEdgesChange] = useEdgesState<Edge>([])

  // 初始化
  useEffect(() => {
    const g = stepsToGraph(initialSteps || [])
    const pos = layout(g.nodes, g.edges)
    const positioned = g.nodes.map((n) => ({ ...n, position: pos[n.id] || { x: 0, y: 0 } }))
    setRawNodes(positioned)
    setEdges(g.edges.map((e) => ({
      id: e.id, source: e.source, target: e.target, type: 'smoothstep',
      markerEnd: { type: MarkerType.ArrowClosed, color: '#94a3b8' },
      style: { stroke: '#94a3b8', strokeWidth: 2 },
    })))
  }, [initialSteps])

  const relayout = useCallback((nodes: StepGraphNode[], es: StepGraphEdge[]) => {
    const pos = layout(nodes, es)
    return nodes.map((n) => ({ ...n, position: pos[n.id] || n.position || { x: 0, y: 0 } }))
  }, [])

  // ── 操作（全部用函数式更新，handlers 稳定） ─────────────────────────
  const handlers: StepNodeHandlers = {
    onChange: (id, patch) => {
      setRawNodes((prev) => prev.map((n) => (n.id === id ? { ...n, data: { ...n.data, ...patch } } : n)))
    },
    onAddChild: (id, branchType) => {
      setRawNodes((prev) => {
        const siblings = prev.filter((n) => n.data.__parentId === id && n.data.__branchType === branchType)
        const child = makeNode({}, id, branchType, siblings.length)
        return relayout([...prev, child], edges)
      })
    },
    onAddNext: (id) => {
      const childId = nextId()
      setRawNodes((prev) => {
        const siblings = prev.filter((n) => n.data.__parentId === (prev.find((x) => x.id === id)?.data.__parentId))
        const child = makeNode({}, undefined, undefined, siblings.length)
        // 用固定 id 保证边引用一致
        child.id = childId
        return relayout([...prev, child], edges)
      })
      setEdges((prev) => [...prev, {
        id: id + '->' + childId, source: id, target: childId, type: 'smoothstep',
        markerEnd: { type: MarkerType.ArrowClosed, color: '#94a3b8' },
        style: { stroke: '#94a3b8', strokeWidth: 2 },
      }])
    },
    onDelete: (id) => {
      // 删除节点及其后代
      const descendants = (root: string): string[] => {
        const kids = rawNodes.filter((n) => n.data.__parentId === root).map((n) => n.id)
        return kids.flatMap((k) => [k, ...descendants(k)])
      }
      const doomed = new Set([id, ...descendants(id)])
      setRawNodes((prev) => prev.filter((n) => !doomed.has(n.id)))
      setEdges((prev) => prev.filter((e) => !doomed.has(e.source) && !doomed.has(e.target)))
    },
  }

  // React Flow 节点：rawNodes + __handlers（稳定引用）
  const rfNodes: Node[] = useMemo(() => rawNodes.map((n) => ({
    id: n.id,
    type: 'step',
    position: n.position || { x: 0, y: 0 },
    data: { ...n.data, __handlers: handlers },
  })), [rawNodes]) // eslint-disable-line react-hooks/exhaustive-deps

  const onConnect = useCallback((c: Connection) => {
    if (!c.source || !c.target || c.source === c.target) return
    setEdges((prev) => {
      if (prev.some((e) => e.source === c.source && e.target === c.target)) return prev
      return [...prev, {
        id: c.source + '->' + c.target, source: c.source, target: c.target, type: 'smoothstep',
        markerEnd: { type: MarkerType.ArrowClosed, color: '#94a3b8' },
        style: { stroke: '#94a3b8', strokeWidth: 2 },
      }]
    })
  }, [])

  const handleSave = () => {
    const steps = graphToSteps(
      rawNodes.map((n) => ({ id: n.id, data: n.data })),
      edges.map((e) => ({ id: e.id, source: e.source, target: e.target }))
    )
    onSave(steps)
  }
  const handleRun = () => {
    const steps = graphToSteps(
      rawNodes.map((n) => ({ id: n.id, data: n.data })),
      edges.map((e) => ({ id: e.id, source: e.source, target: e.target }))
    )
    onRun(steps)
  }

  return (
    <div className="w-full h-full flex flex-col">
      <div className="h-12 border-b px-4 flex items-center justify-between bg-background">
        <div className="flex items-center gap-3 text-sm text-muted-foreground">
          <GitGraph className="w-4 h-4" />
          <span>Nodes: {rawNodes.length} · Edges: {edges.length}</span>
        </div>
        <div className="flex items-center gap-2">
          <button onClick={() => setRawNodes((prev) => relayout([...prev, makeNode({}, undefined, undefined, prev.length)], edges))}
            className="flex items-center gap-1 px-3 py-1.5 bg-blue-500 hover:bg-blue-600 text-white rounded text-sm">
            <Plus className="w-4 h-4" /> 节点
          </button>
          <button onClick={handleSave} className="flex items-center gap-1 px-3 py-1.5 bg-green-500 hover:bg-green-600 text-white rounded text-sm">
            <Save className="w-4 h-4" /> 保存
          </button>
          <button onClick={handleRun} className="flex items-center gap-1 px-3 py-1.5 bg-purple-500 hover:bg-purple-600 text-white rounded text-sm">
            <Play className="w-4 h-4" /> 运行
          </button>
        </div>
      </div>
      <div className="flex-1">
        <ReactFlow
          nodes={rfNodes}
          edges={edges}
          nodeTypes={nodeTypes}
          onEdgesChange={onEdgesChange}
          onConnect={onConnect}
          fitView
          fitViewOptions={{ padding: 0.2 }}
          deleteKeyCode={['Delete', 'Backspace']}
          className="bg-muted/30"
        >
          <Background variant={BackgroundVariant.Dots} gap={20} size={1} />
          <Controls />
          <MiniMap pannable zoomable />
        </ReactFlow>
      </div>
    </div>
  )
}
