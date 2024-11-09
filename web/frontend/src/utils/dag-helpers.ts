// DAG 相关的边生成和布局工具
import { Edge, MarkerType } from '@xyflow/react'
import { GraphNode } from '../types/graph'

/**
 * 构建 DAG 依赖边（next/depends_on）
 */
export const buildDAGEdges = (nodes: GraphNode[], existingEdges: Edge[] = []): Edge[] => {
  const dagEdges: Edge[] = []
  
  nodes.forEach(node => {
    // 处理 next（后继）
    if (node.data.next && node.data.next.length > 0) {
      node.data.next.forEach(targetId => {
        const exists = existingEdges.some(e => e.source === node.id && e.target === targetId)
        if (!exists) {
          dagEdges.push({
            id: `${node.id}-${targetId}`,
            source: node.id,
            target: targetId,
            type: 'smoothstep',
            style: { stroke: '#3b82f6', strokeWidth: 2 },
            markerEnd: { type: MarkerType.ArrowClosed, color: '#3b82f6' },
          })
        }
      })
    }
    
    // 处理 depends_on（前驱）
    if (node.data.depends_on && node.data.depends_on.length > 0) {
      node.data.depends_on.forEach(sourceId => {
        const exists = dagEdges.some(e => e.source === sourceId && e.target === node.id) ||
                      existingEdges.some(e => e.source === sourceId && e.target === node.id)
        if (!exists) {
          dagEdges.push({
            id: `${sourceId}-${node.id}`,
            source: sourceId,
            target: node.id,
            type: 'smoothstep',
            style: { stroke: '#3b82f6', strokeWidth: 2 },
            markerEnd: { type: MarkerType.ArrowClosed, color: '#3b82f6' },
          })
        }
      })
    }
  })
  
  return dagEdges
}

/**
 * 循环依赖检测
 */
export const detectCycles = (nodes: GraphNode[]): string[][] => {
  const cycles: string[][] = []
  const visited = new Set<string>()
  const recursionStack = new Set<string>()
  const path: string[] = []
  
  const dfs = (nodeId: string) => {
    if (recursionStack.has(nodeId)) {
      const cycleStart = path.indexOf(nodeId)
      if (cycleStart !== -1) {
        cycles.push([...path.slice(cycleStart), nodeId])
      }
      return
    }
    
    if (visited.has(nodeId)) return
    
    visited.add(nodeId)
    recursionStack.add(nodeId)
    path.push(nodeId)
    
    const node = nodes.find(n => n.id === nodeId)
    if (node?.data.next) {
      node.data.next.forEach(nextId => dfs(nextId))
    }
    
    path.pop()
    recursionStack.delete(nodeId)
  }
  
  nodes.forEach(node => {
    if (!visited.has(node.id)) {
      dfs(node.id)
    }
  })
  
  return cycles
}
