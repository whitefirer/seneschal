import { MarkerType, type Edge } from '@xyflow/react'
import {
  NODE_WIDTH, NODE_HEIGHT, H_SPACING, V_SPACING, PARENT_CHILD_GAP,
  FOREACH_PADDING, PARALLEL_PADDING,
  extractIterationIndex, filterForeachChildren,
  type FlowStep, type FlatFlowStep,
} from './flowTypes'

// ===== 统一的 DAG 依赖图构建 =====
// 所有依赖关系（显式 next/depends_on + 隐式顺序）都走同一张依赖图
// edgeReasons 标记每条边来源：'next' | 'depends_on' | 'implicit'

export type EdgeReason = 'next' | 'depends_on' | 'implicit'

export interface DepGraph {
  predecessors: Map<string, Set<string>>
  successors: Map<string, Set<string>>
  stepMap: Map<string, FlowStep>
  nameToIdMap: Map<string, string>
  edgeReasons: Map<string, EdgeReason>  // key: "sourceId->targetId"
  resolveRef: (ref: string) => string | null
}

export function buildDepGraph(steps: FlowStep[], isParallelContainer: boolean = false): DepGraph {
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

  const predecessors = new Map<string, Set<string>>()
  const successors = new Map<string, Set<string>>()
  const edgeReasons = new Map<string, EdgeReason>()
  steps.forEach(s => {
    predecessors.set(s.id, new Set())
    successors.set(s.id, new Set())
  })

  const addEdge = (from: string, to: string, reason: EdgeReason) => {
    successors.get(from)?.add(to)
    predecessors.get(to)?.add(from)
    const key = `${from}->${to}`
    // 显式声明优先级高于隐式
    if (!edgeReasons.has(key) || reason !== 'implicit') {
      edgeReasons.set(key, reason)
    }
  }

  // 1. depends_on
  steps.forEach(s => {
    if (s.depends_on && s.depends_on.length > 0) {
      s.depends_on.forEach(depId => {
        const targetId = resolveRef(depId)
        if (targetId) addEdge(targetId, s.id, 'depends_on')
      })
    }
  })

  // 2. next
  steps.forEach(s => {
    if (s.next && s.next.length > 0) {
      s.next.forEach(nextId => {
        const targetId = resolveRef(nextId)
        if (targetId) addEdge(s.id, targetId, 'next')
      })
    }
  })

  // 3. 隐式顺序依赖（无显式依赖且非首个时，依赖前一个兄弟）
  if (!isParallelContainer && steps.length > 1) {
    for (let i = 1; i < steps.length; i++) {
      const curr = steps[i]
      const prev = steps[i - 1]
      if (!curr.depends_on || curr.depends_on.length === 0) {
        const key = `${prev.id}->${curr.id}`
        if (!edgeReasons.has(key)) {
          addEdge(prev.id, curr.id, 'implicit')
        }
      }
    }
  }

  return { predecessors, successors, stepMap, nameToIdMap, edgeReasons, resolveRef }
}

// 基于依赖图计算层级和位置（只做布局，不做连线）
export function calculateDAGLayout(steps: FlowStep[], startX: number, startY: number, isParallelContainer: boolean = false): FlatFlowStep[] {
  const { predecessors } = buildDepGraph(steps, isParallelContainer)

  // DFS 计算层级（最长前驱路径长度）
  const levels = new Map<string, number>()
  const computeLevel = (nodeId: string): number => {
    if (levels.has(nodeId)) return levels.get(nodeId)!
    const preds = predecessors.get(nodeId) || new Set()
    if (preds.size === 0) { levels.set(nodeId, 0); return 0 }
    let maxPred = -1
    for (const p of preds) maxPred = Math.max(maxPred, computeLevel(p))
    const lvl = maxPred + 1
    levels.set(nodeId, lvl)
    return lvl
  }
  steps.forEach(s => computeLevel(s.id))

  const levelGroups = new Map<number, FlowStep[]>()
  steps.forEach(s => {
    const lvl = levels.get(s.id) || 0
    if (!levelGroups.has(lvl)) levelGroups.set(lvl, [])
    levelGroups.get(lvl)?.push(s)
  })

  const nodes: FlatFlowStep[] = []
  const maxLevel = Math.max(...Array.from(levels.values()), 0)
  for (let lvl = 0; lvl <= maxLevel; lvl++) {
    const group = levelGroups.get(lvl) || []
    const lx = startX + lvl * (NODE_WIDTH + H_SPACING)
    const gh = group.length * (NODE_HEIGHT + V_SPACING) - V_SPACING
    const ly = startY + (group.length > 1 ? -gh / 2 + NODE_HEIGHT / 2 : 0)
    group.forEach((step, idx) => {
      nodes.push({ ...step, parentId: undefined, position: { x: lx, y: ly + idx * (NODE_HEIGHT + V_SPACING) } })
    })
  }
  return nodes
}

export interface ForeachGroupBounds {
  x: number
  y: number
  width: number
  height: number
  skippedIterations?: number
  originalIterations?: number
}

export interface ParallelGroupBounds {
  x: number
  y: number
  width: number
  height: number
}

export function calculateLayout(
  steps: FlowStep[],
  startX: number,
  startY: number
): { nodes: FlatFlowStep[], nextX: number, maxY: number, foreachGroups: Map<string, ForeachGroupBounds>, parallelGroups: Map<string, ParallelGroupBounds> } {
  // 1. 使用 DAG 布局计算当前层级节点
  const dagNodes = calculateDAGLayout(steps, startX, startY)

  const allNodes: FlatFlowStep[] = [...dagNodes]
  const foreachGroups = new Map<string, ForeachGroupBounds>()
  const parallelGroups = new Map<string, ParallelGroupBounds>()
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

        const childMaxX = childrenStartX + NODE_WIDTH
        const childMaxY = currentY - V_SPACING

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


// 构建连线（基于统一的 DAG 依赖图）
export function buildEdges(steps: FlowStep[], isDark: boolean): Edge[] {
  const edges: Edge[] = []
  const edgeIdSet = new Set<string>()
  const addEdge = (edge: Edge) => {
    if (!edgeIdSet.has(edge.id)) { edgeIdSet.add(edge.id); edges.push(edge) }
  }

  const processLevel = (stepList: FlowStep[], isParallelContainer = false) => {
    if (stepList.length === 0) return
    const dg = buildDepGraph(stepList, isParallelContainer)

    // 1. 从依赖图 successors 生成水平 DAG 边
    // condition 有子分支时不画直连出口边，由下方容器处理画分支出口
    for (const [srcId, targets] of dg.successors) {
      const srcStep = dg.stepMap.get(srcId)
      const isConditionWithBranches = srcStep?.action === 'condition' &&
        ((srcStep.then_children || srcStep.then)?.length || (srcStep.else_children || srcStep.else)?.length)
      if (isConditionWithBranches) continue // 出口由分支子节点处理
      for (const tgtId of targets) {
        const reason = dg.edgeReasons.get(`${srcId}->${tgtId}`)
        const isExplicit = reason === 'next' || reason === 'depends_on'
        const target = dg.stepMap.get(tgtId)
        addEdge({
          id: `edge-${srcId}-${tgtId}`,
          source: srcId, sourceHandle: 'right',
          target: tgtId, targetHandle: 'left',
          type: 'default',
          animated: target?.status === 'running',
          style: { stroke: isExplicit ? '#3b82f6' : '#9ca3af', strokeWidth: 2 },
          markerEnd: { type: MarkerType.ArrowClosed, color: isExplicit ? '#3b82f6' : '#9ca3af' },
        })
      }
    }

    // 2. 容器节点的垂直（父子）边
    for (const step of stepList) {
      const collapsed = step.collapsed

      // --- parallel ---
      if (step.action === 'parallel' && step.children?.length) {
        if (collapsed) {
          const n = Math.min(2, step.children.length)
          for (let j = 0; j < n; j++) {
            const c = step.children[j]
            addEdge({
              id: `edge-${step.id}-${c.id}-collapsed`,
              source: step.id, sourceHandle: 'bottom',
              target: c.id, targetHandle: 'top',
              type: 'hollow', animated: c.status === 'running',
              style: { stroke: '#a855f7', strokeDasharray: '4 4' },
            })
          }
        } else {
          for (const c of step.children) {
            addEdge({
              id: `edge-${step.id}-${c.id}`,
              source: step.id, sourceHandle: 'bottom',
              target: c.id, targetHandle: 'top',
              type: 'hollow', animated: c.status === 'running',
              style: { stroke: '#a855f7' },
            })
          }
        }
        processLevel(step.children, true)
      }
      // --- foreach / loop ---
      else if ((step.action === 'foreach' || step.action === 'loop') && step.children?.length) {
        if (!collapsed) {
          const { filtered, skippedIterations } = filterForeachChildren(step.children, step.items?.length)
          if (filtered.length > 0) {
            addEdge({
              id: `edge-${step.id}-${filtered[0].id}`,
              source: step.id, sourceHandle: 'bottom',
              target: filtered[0].id, targetHandle: 'top',
              type: 'default',
              style: { stroke: '#06b6d4', strokeWidth: 2, strokeDasharray: '4,4' },
              markerEnd: { type: MarkerType.ArrowClosed, color: '#06b6d4' },
            })
            for (let j = 0; j < filtered.length - 1; j++) {
              const ci = extractIterationIndex(filtered[j].id)
              const ni = extractIterationIndex(filtered[j + 1].id)
              const e: Edge = {
                id: `edge-${filtered[j].id}-${filtered[j + 1].id}`,
                source: filtered[j].id, sourceHandle: 'bottom',
                target: filtered[j + 1].id, targetHandle: 'top',
                type: 'default',
                style: { stroke: '#06b6d4', strokeWidth: 2, strokeDasharray: '4,4' },
                markerEnd: { type: MarkerType.ArrowClosed, color: '#06b6d4' },
              }
              if (skippedIterations > 0 && ci !== null && ni !== null && ci !== ni) {
                e.label = '......'
                e.labelStyle = { fill: '#9ca3af', fontWeight: 'bold', fontSize: 12 }
                e.labelBgStyle = { fill: isDark ? '#1f2937' : '#ffffff', fillOpacity: 0.9 }
                e.labelBgPadding = [8, 4] as [number, number]
                e.labelBgBorderRadius = 4
              }
              addEdge(e)
            }
          }
        }
        processLevel(step.children, false)
      }
      // --- condition ---
      else if (step.action === 'condition') {
        const thenC = step.then_children || step.then || []
        const elseC = step.else_children || step.else || []
        const cr = step.condition_result
        const te = cr === true; const ee = cr === false
        const vThen = collapsed ? (te ? thenC : []) : thenC
        const vElse = collapsed ? (ee ? elseC : []) : elseC

        if (vThen.length > 0) {
          addEdge({
            id: `edge-${step.id}-${vThen[0].id || step.id + '-then-0'}`,
            source: step.id, sourceHandle: 'right',
            target: vThen[0].id || step.id + '-then-0', targetHandle: 'left',
            type: 'default',
            style: { stroke: te ? '#22c55e' : '#d1d5db', strokeWidth: 2, strokeDasharray: te ? undefined : '4,4' },
            markerEnd: { type: MarkerType.ArrowClosed, color: te ? '#22c55e' : '#d1d5db' },
            label: `true(T:${te})`, labelStyle: { fill: te ? '#22c55e' : '#9ca3af', fontSize: 9, fontWeight: 'bold' },
            labelBgStyle: { fill: isDark ? '#1f2937' : 'white', fillOpacity: 0.9 },
          })
          for (let j = 0; j < vThen.length - 1; j++) {
            addEdge({
              id: `edge-${vThen[j].id}-${vThen[j + 1].id}`,
              source: vThen[j].id, sourceHandle: 'right',
              target: vThen[j + 1].id, targetHandle: 'left',
              type: 'default',
              style: { stroke: te ? '#22c55e' : '#d1d5db', strokeWidth: 2, strokeDasharray: te ? undefined : '4,4' },
              markerEnd: { type: MarkerType.ArrowClosed, color: te ? '#22c55e' : '#d1d5db' },
            })
          }
          processLevel(vThen, false)
        }
        if (vElse.length > 0) {
          addEdge({
            id: `edge-${step.id}-${vElse[0].id || step.id + '-else-0'}`,
            source: step.id, sourceHandle: 'right',
            target: vElse[0].id || step.id + '-else-0', targetHandle: 'left',
            type: 'default',
            style: { stroke: ee ? '#22c55e' : '#d1d5db', strokeWidth: 2, strokeDasharray: ee ? undefined : '4,4' },
            markerEnd: { type: MarkerType.ArrowClosed, color: ee ? '#22c55e' : '#d1d5db' },
            label: `false(E:${ee})`, labelStyle: { fill: ee ? '#22c55e' : '#9ca3af', fontSize: 9, fontWeight: 'bold' },
            labelBgStyle: { fill: isDark ? '#1f2937' : 'white', fillOpacity: 0.9 },
          })
          for (let j = 0; j < vElse.length - 1; j++) {
            addEdge({
              id: `edge-${vElse[j].id}-${vElse[j + 1].id}`,
              source: vElse[j].id, sourceHandle: 'right',
              target: vElse[j + 1].id, targetHandle: 'left',
              type: 'default',
              style: { stroke: ee ? '#22c55e' : '#d1d5db', strokeWidth: 2, strokeDasharray: ee ? undefined : '4,4' },
              markerEnd: { type: MarkerType.ArrowClosed, color: ee ? '#22c55e' : '#d1d5db' },
            })
          }
          processLevel(vElse, false)
        }
        // condition 分支出口边：最后一子 → after-target
        const afterTargets = dg.successors.get(step.id)
        if (afterTargets && afterTargets.size > 0) {
          for (const tgt of afterTargets) {
            if (vThen.length > 0) {
              const lastThen = vThen[vThen.length - 1]
              addEdge({
                id: `edge-${lastThen.id}-${tgt}-cond-${cr}`,
                source: lastThen.id, sourceHandle: 'right',
                target: tgt, targetHandle: 'left',
                type: 'default',
                style: { stroke: te ? '#22c55e' : '#d1d5db', strokeWidth: 2, strokeDasharray: te ? undefined : '5,5' },
                markerEnd: { type: MarkerType.ArrowClosed, color: te ? '#22c55e' : '#d1d5db' },
                label: `T:${te} cr:${cr}`, labelStyle: { fill: te ? '#22c55e' : '#9ca3af', fontSize: 10, fontWeight: 'bold' },
                labelBgStyle: { fill: isDark ? '#1f2937' : 'white', fillOpacity: 0.9 },
              })
            }
            if (vElse.length > 0) {
              const lastElse = vElse[vElse.length - 1]
              addEdge({
                id: `edge-${lastElse.id}-${tgt}-cond-${cr}`,
                source: lastElse.id, sourceHandle: 'right',
                target: tgt, targetHandle: 'left',
                type: 'default',
                style: { stroke: ee ? '#22c55e' : '#d1d5db', strokeWidth: 2, strokeDasharray: ee ? undefined : '5,5' },
                markerEnd: { type: MarkerType.ArrowClosed, color: ee ? '#22c55e' : '#d1d5db' },
                label: `E:${ee} cr:${cr}`, labelStyle: { fill: ee ? '#22c55e' : '#9ca3af', fontSize: 10, fontWeight: 'bold' },
                labelBgStyle: { fill: isDark ? '#1f2937' : 'white', fillOpacity: 0.9 },
              })
            }
          }
        }
      }
      // --- 普通节点：递归子节点 ---
      else if (step.children?.length && !collapsed) {
        processLevel(step.children, false)
      }
    }
  }

  processLevel(steps)
  return edges
}
