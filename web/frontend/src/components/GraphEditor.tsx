// 图形编辑器画布：React Flow + 工具栏 + 保存/运行。
// 数据转换用 lib/stepGraph.ts（透传），节点 UI 用 StepNode.tsx，容器分组用 GroupNode.tsx。
import { useState, useEffect, useCallback, useMemo } from 'react'
import {
  ReactFlow, Background, Controls, MiniMap, BackgroundVariant,
  Connection, Edge, Node, MarkerType, useEdgesState,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { Plus, Save, Play, GitGraph } from 'lucide-react'
import StepNode, { type StepNodeHandlers } from './StepNode'
import GroupNode from './GroupNode'
import { stepsToGraph, graphToSteps, type StepGraphNode, type StepGraphEdge } from '@/lib/stepGraph'

const nodeTypes = { step: StepNode, group: GroupNode }

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

// 容器 action → 分支列表
const CONTAINER_BRANCHES: Record<string, string[]> = {
  condition: ['then', 'else'],
  parallel: ['parallel'],
  foreach: ['do'],
  loop: ['do'],
}

// 由 rawNodes + edges 计算 React Flow 节点（含容器分组 + 布局）
function computeGraph(rawNodes: StepGraphNode[], edges: StepGraphEdge[], handlers: StepNodeHandlers): Node[] {
  const nodes: Node[] = []
  const tops = rawNodes.filter((n) => !n.data.__parentId)
  const topIds = new Set(tops.map((n) => n.id))

  // 上游引用：每条边 source 的 name/save_output 供下游 AI 节点 @ 引用
  const upstreamMap = new Map<string, { name: string; outputVar?: string }[]>()
  rawNodes.forEach((n) => upstreamMap.set(n.id, []))
  for (const e of edges) {
    const src = rawNodes.find((n) => n.id === e.source)
    const tgt = rawNodes.find((n) => n.id === e.target)
    if (src && tgt) {
      const outputVar = src.data.save_output || src.data.output_var
      upstreamMap.get(tgt.id)!.push({ name: src.data.name, outputVar })
    }
  }

  // 顶层深度（只考虑顶层节点之间的边）
  const preds = new Map<string, string[]>(tops.map((n) => [n.id, []]))
  for (const e of edges) if (topIds.has(e.target)) preds.get(e.target)!.push(e.source)
  const depth = new Map<string, number>(tops.map((n) => [n.id, 0]))
  for (let k = 0; k < 200; k++) {
    let changed = false
    for (const n of tops) {
      let d = 0
      for (const p of preds.get(n.id) || []) d = Math.max(d, (depth.get(p) ?? 0) + 1)
      if (d !== depth.get(n.id)) { depth.set(n.id, d); changed = true }
    }
    if (!changed) break
  }

  // 顶层节点 x/y（按深度分列）
  const yCursor = new Map<number, number>()
  const topPos = new Map<string, { x: number; y: number }>()
  tops.forEach((n) => {
    const d = depth.get(n.id) ?? 0
    const y = yCursor.get(d) ?? 0
    topPos.set(n.id, { x: d * 380, y: y * 200 })
    yCursor.set(d, y + 1)
  })

  const handled = new Set<string>()
  for (const top of tops) {
    const branches = CONTAINER_BRANCHES[top.data.action]
    if (!branches) continue
    const base = topPos.get(top.id)!
    let gy = base.y
    for (const branch of branches) {
      const children = rawNodes
        .filter((n) => n.data.__parentId === top.id && n.data.__branchType === branch)
        .sort((a, b) => (a.data.__branchIndex ?? 0) - (b.data.__branchIndex ?? 0))
      if (!children.length) continue
      const groupId = top.id + '::' + branch
      const pad = 16, headerH = 30, gap = 16, cw = 240, ch = 130
      const gw = cw + pad * 2
      const gh = headerH + children.length * ch + (children.length - 1) * gap + pad * 2
      nodes.push({
        id: groupId, type: 'group', position: { x: base.x + 340, y: gy },
        width: gw, height: gh,
        data: { branch, kind: top.data.action === 'condition' ? 'condition' : top.data.action === 'parallel' ? 'parallel' : 'foreach' },
      })
      children.forEach((c, i) => {
        nodes.push({
          id: c.id, type: 'step', parentId: groupId, extent: 'parent' as const,
          position: { x: pad, y: headerH + pad + i * (ch + gap) },
          data: { ...c.data, __handlers: handlers, __upstream: upstreamMap.get(c.id) || [] },
        })
      })
      gy += gh + 28
    }
    nodes.push({ id: top.id, type: 'step', position: base, data: { ...top.data, __handlers: handlers, __upstream: upstreamMap.get(top.id) || [] } })
    handled.add(top.id)
  }

  // 非容器顶层节点
  for (const top of tops) {
    if (handled.has(top.id)) continue
    nodes.push({ id: top.id, type: 'step', position: topPos.get(top.id)!, data: { ...top.data, __handlers: handlers, __upstream: upstreamMap.get(top.id) || [] } })
  }

  return nodes
}

export default function GraphEditor({ initialSteps, onSave, onRun }: GraphEditorProps) {
  const [rawNodes, setRawNodes] = useState<StepGraphNode[]>([])
  const [edges, setEdges, onEdgesChange] = useEdgesState<Edge>([])

  // 初始化
  useEffect(() => {
    const g = stepsToGraph(initialSteps || [])
    setRawNodes(g.nodes)
    setEdges(g.edges.map((e) => ({
      id: e.id, source: e.source, target: e.target, type: 'smoothstep',
      markerEnd: { type: MarkerType.ArrowClosed, color: '#94a3b8' },
      style: { stroke: '#94a3b8', strokeWidth: 2 },
    })))
  }, [initialSteps])

  // ── 操作（函数式更新，handlers 稳定） ──────────────────────────────
  const handlers: StepNodeHandlers = {
    onChange: (id, patch) => {
      setRawNodes((prev) => prev.map((n) => (n.id === id ? { ...n, data: { ...n.data, ...patch } } : n)))
    },
    onAddChild: (id, branchType) => {
      setRawNodes((prev) => {
        const siblings = prev.filter((n) => n.data.__parentId === id && n.data.__branchType === branchType)
        const child = makeNode({}, id, branchType, siblings.length)
        return [...prev, child]
      })
    },
    onAddNext: (id) => {
      const childId = nextId()
      setRawNodes((prev) => [...prev, makeNode({}, undefined, undefined, prev.length)])
      setEdges((prev) => [...prev, {
        id: id + '->' + childId, source: id, target: childId, type: 'smoothstep',
        markerEnd: { type: MarkerType.ArrowClosed, color: '#94a3b8' },
        style: { stroke: '#94a3b8', strokeWidth: 2 },
      }])
    },
    onDelete: (id) => {
      const descendants = (root: string): string[] => {
        const kids = rawNodes.filter((n) => n.data.__parentId === root).map((n) => n.id)
        return kids.flatMap((k) => [k, ...descendants(k)])
      }
      const doomed = new Set([id, ...descendants(id)])
      setRawNodes((prev) => prev.filter((n) => !doomed.has(n.id)))
      setEdges((prev) => prev.filter((e) => !doomed.has(e.source) && !doomed.has(e.target)))
    },
  }

  const rfNodes: Node[] = useMemo(
    () => computeGraph(rawNodes, edges, handlers),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [rawNodes, edges]
  )

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
          <button onClick={() => setRawNodes((prev) => [...prev, makeNode({}, undefined, undefined, prev.length)])}
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
          <MiniMap
            position="bottom-right"
            nodeColor={(node) => {
              const a = node.data?.action as string
              if (a === 'shell') return '#4ade80'
              if (a === 'http') return '#60a5fa'
              if (a === 'condition') return '#f59e0b'
              if (a === 'parallel') return '#a78bfa'
              if (a === 'foreach' || a === 'loop') return '#22d3ee'
              if (a === 'ai' || a === 'ai_decide') return '#f472b6'
              if (a === 'log') return '#fde047'
              return '#9ca3af'
            }}
            maskColor="rgba(0, 0, 0, 0.15)"
            pannable
            zoomable
            style={{
              width: 160,
              height: 120,
              background: 'rgba(255, 255, 255, 0.6)',
              backdropFilter: 'blur(4px)',
              borderRadius: '8px',
            }}
            className="!border !border-gray-200 dark:!border-gray-700"
          />
        </ReactFlow>
      </div>
    </div>
  )
}
