// step 数组 ↔ 图节点 的透传转换。
//
// 核心原则：节点的 data 原样保存 step 的「除容器子节点外的全部字段」，
// 转换层只负责：容器子节点（then/else/steps/do）与 next/depends_on 的扁平化/重组。
// 因此任意新增字段天然 round-trip，不会丢数据。
import { CONTAINER_CHILDREN } from './stepSchema'

export interface StepGraphNode {
  id: string
  data: Record<string, any>
  position?: { x: number; y: number }
}

export interface StepGraphEdge {
  id: string
  source: string
  target: string
}

// 内部元数据字段（前缀 __，避免与真实 step 字段冲突）
const META_PARENT = '__parentId'
const META_BRANCH = '__branchType'
const META_INDEX = '__branchIndex'

// 容器子字段 → 分支类型
const CONTAINER_KEY_TO_BRANCH: Record<string, string> = {
  then: 'then',
  else: 'else',
  steps: 'parallel',
  do: 'do',
}

function nodeIdOf(counter: { n: number }): string {
  return 'n' + counter.n++
}

/**
 * step 数组 → 图（扁平化：容器子节点成为独立节点，next/depends_on 成为边）
 */
export function stepsToGraph(steps: any[]): { nodes: StepGraphNode[]; edges: StepGraphEdge[] } {
  const nodes: StepGraphNode[] = []
  const edges: StepGraphEdge[] = []
  const nameToId = new Map<string, string>()
  const counter = { n: 0 }
  const pendingEdges: { nodeId: string; nexts: string[]; deps: string[] }[] = []

  const walk = (list: any[], parentId?: string, branchType?: string) => {
    ;(list || []).forEach((step, i) => {
      if (!step || typeof step !== 'object') return
      const name = step.name || 'step-' + (i + 1)
      const id = nodeIdOf(counter)
      if (name && !nameToId.has(name)) nameToId.set(name, id)
      if (step.id && !nameToId.has(step.id)) nameToId.set(step.id, id)

      // 透传：复制 step 全部字段，删掉容器子节点与 DAG 字段（由边/子节点表达）
      const data: Record<string, any> = { ...step }
      delete data.then
      delete data.else
      delete data.steps
      delete data.do
      const nexts = Array.isArray(data.next) ? data.next.filter((x: any) => typeof x === 'string') : []
      const deps = Array.isArray(data.depends_on) ? data.depends_on.filter((x: any) => typeof x === 'string') : []
      delete data.next
      delete data.depends_on

      data.name = name
      data.action = step.action || 'log'
      data[META_PARENT] = parentId
      data[META_BRANCH] = branchType
      data[META_INDEX] = i

      nodes.push({ id, data })
      pendingEdges.push({ nodeId: id, nexts, deps })

      // 递归容器子节点
      if (step.action === 'condition') {
        walk(step.then, id, 'then')
        walk(step.else, id, 'else')
      } else if (step.action === 'parallel') {
        walk(step.steps, id, 'parallel')
      } else if (step.action === 'foreach' || step.action === 'loop') {
        walk(step.do, id, 'do')
      }
    })
  }

  walk(steps)

  // 第二遍：next/depends_on → 边（去重）
  const addEdge = (source: string, target: string) => {
    if (!source || !target || source === target) return
    const id = source + '->' + target
    if (!edges.some((e) => e.id === id)) edges.push({ id, source, target })
  }
  for (const p of pendingEdges) {
    for (const dep of p.deps) {
      const src = nameToId.get(dep)
      if (src) addEdge(src, p.nodeId)
    }
    for (const nxt of p.nexts) {
      const tgt = nameToId.get(nxt)
      if (tgt) addEdge(p.nodeId, tgt)
    }
  }

  return { nodes, edges }
}

/**
 * 图 → step 数组（重组容器嵌套 + 拓扑排序顶层 + 写回 next/depends_on）
 */
export function graphToSteps(nodes: StepGraphNode[], edges: StepGraphEdge[]): any[] {
  const idToNode = new Map(nodes.map((n) => [n.id, n]))
  const nextMap = new Map<string, string[]>() // source -> targets
  const depMap = new Map<string, string[]>() // target -> sources

  for (const e of edges) {
    if (!nextMap.has(e.source)) nextMap.set(e.source, [])
    if (!nextMap.get(e.source)!.includes(e.target)) nextMap.get(e.source)!.push(e.target)
    if (!depMap.has(e.target)) depMap.set(e.target, [])
    if (!depMap.get(e.target)!.includes(e.source)) depMap.get(e.target)!.push(e.source)
  }

  const build = (nodeId: string): any => {
    const node = idToNode.get(nodeId)
    if (!node) return { name: 'step', action: 'log' }
    const step: Record<string, any> = {}
    for (const [k, v] of Object.entries(node.data)) {
      if (k.startsWith('__')) continue // 跳过元数据
      step[k] = v
    }
    if (!step.name) step.name = 'step'
    if (!step.action) step.action = 'log'

    const nexts = nextMap.get(nodeId)
    const deps = depMap.get(nodeId)
    if (nexts && nexts.length) step.next = nexts.map((id) => idToNode.get(id)?.data?.name || id)
    if (deps && deps.length) step.depends_on = deps.map((id) => idToNode.get(id)?.data?.name || id)

    const action = step.action
    if (action === 'condition') {
      const then = childrenOf(nodeId, 'then')
      const els = childrenOf(nodeId, 'else')
      if (then.length) step.then = then
      if (els.length) step.else = els
    } else if (action === 'parallel') {
      const cs = childrenOf(nodeId, 'parallel')
      if (cs.length) step.steps = cs
    } else if (action === 'foreach' || action === 'loop') {
      const cs = childrenOf(nodeId, 'do')
      if (cs.length) step.do = cs
    }
    return step
  }

  const childrenOf = (parentId: string, branchType: string): any[] =>
    nodes
      .filter((n) => n.data[META_PARENT] === parentId && n.data[META_BRANCH] === branchType)
      .sort((a, b) => (a.data[META_INDEX] ?? 0) - (b.data[META_INDEX] ?? 0))
      .map((n) => build(n.id))

  const roots = nodes.filter((n) => !n.data[META_PARENT])
  const ordered = topologicalSort(roots, depMap)
  return ordered.map((id) => build(id))
}

/**
 * 顶层节点的拓扑排序（依赖优先，__branchIndex 作稳定 tiebreak）
 */
function topologicalSort(roots: StepGraphNode[], depMap: Map<string, string[]>): string[] {
  const rootIds = new Set(roots.map((n) => n.id))
  const inDegree = new Map<string, number>()
  const dependents = new Map<string, string[]>() // node -> 依赖它的（反向）

  for (const n of roots) {
    inDegree.set(n.id, 0)
    dependents.set(n.id, [])
  }
  for (const [target, sources] of depMap) {
    if (!rootIds.has(target)) continue
    for (const src of sources) {
      if (!rootIds.has(src)) continue
      inDegree.set(target, (inDegree.get(target) || 0) + 1)
      dependents.get(src)!.push(target)
    }
  }

  const byIndex = (a: StepGraphNode, b: StepGraphNode) =>
    (a.data[META_INDEX] ?? 0) - (b.data[META_INDEX] ?? 0)

  // 初始 ready = 入度 0 的节点，按 index 排序
  const ready = roots.filter((n) => (inDegree.get(n.id) || 0) === 0).sort(byIndex).map((n) => n.id)
  const result: string[] = []
  const idx = new Map<string, number>(roots.map((n) => [n.id, n.data[META_INDEX] ?? 0]))

  while (ready.length > 0) {
    // 稳定地取 index 最小的 ready 节点
    ready.sort((a, b) => idx.get(a)! - idx.get(b)!)
    const id = ready.shift()!
    result.push(id)
    for (const dep of dependents.get(id) || []) {
      const d = (inDegree.get(dep) || 1) - 1
      inDegree.set(dep, d)
      if (d === 0) ready.push(dep)
    }
  }

  // 有环或未覆盖的节点（理论上不该发生），按 index 补上
  const rest = roots.filter((n) => !result.includes(n.id)).sort(byIndex).map((n) => n.id)
  return [...result, ...rest]
}

// 推断线性依赖：相邻步骤无显式依赖时按顺序连接（与引擎 inferLinearDependencies 一致）。
// 跳过 parallel 子节点（默认并行，不做相邻链化）。
export function inferLinearEdges(nodes: StepGraphNode[], edges: StepGraphEdge[]): StepGraphEdge[] {
  const result: StepGraphEdge[] = [...edges]
  const groups = new Map<string, StepGraphNode[]>()
  for (const n of nodes) {
    const key = (n.data[META_PARENT] ?? '') + '::' + (n.data[META_BRANCH] ?? '')
    if (!groups.has(key)) groups.set(key, [])
    groups.get(key)!.push(n)
  }
  for (const group of groups.values()) {
    if (group[0]?.data[META_BRANCH] === 'parallel') continue
    group.sort((a, b) => (a.data[META_INDEX] ?? 0) - (b.data[META_INDEX] ?? 0))
    for (let i = 0; i < group.length - 1; i++) {
      const cur = group[i]
      const nxt = group[i + 1]
      const hasEdge = result.some(
        (e) => (e.source === cur.id && e.target === nxt.id) || (e.source === nxt.id && e.target === cur.id)
      )
      if (hasEdge) continue
      result.push({ id: cur.id + '->' + nxt.id, source: cur.id, target: nxt.id })
    }
  }
  return result
}

export { CONTAINER_CHILDREN, CONTAINER_KEY_TO_BRANCH }
