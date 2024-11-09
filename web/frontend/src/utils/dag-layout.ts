// DAG 布局算法（支持真 DAG 和容器嵌套两种模式）

// 间距配置（与原编辑器一致）
const H_SPACING = 80
const V_SPACING = 40
const PARENT_CHILD_GAP = 60
const NODE_WIDTH = 220
const BASE_NODE_HEIGHT = 150

const FIELD_HEIGHTS: Record<string, number> = {
  input: 32,
  textarea: 80,
  select: 32,
  button: 36,
}

const ACTION_FIELDS: Record<string, Array<{type: string; field: string}>> = {
  'log': [{type: 'textarea', field: 'message'}, {type: 'select', field: 'level'}],
  'http': [{type: 'input', field: 'url'}, {type: 'select', field: 'method'}, {type: 'textarea', field: 'body'}],
  'shell': [{type: 'textarea', field: 'shell'}],
  'script': [{type: 'textarea', field: 'script'}],
  'sleep': [{type: 'input', field: 'duration'}],
  'condition': [{type: 'input', field: 'if'}],
  'parallel': [],
  'foreach': [{type: 'input', field: 'item_var'}, {type: 'textarea', field: 'items_text'}],
  'loop': [{type: 'input', field: 'item_var'}, {type: 'textarea', field: 'items_text'}],
}

const COMMON_FIELDS = [{type: 'input', field: 'description'}]

const calculateNodeHeight = (data: any): number => {
  let height = BASE_NODE_HEIGHT
  const action = data?.action || ''
  const fields = ACTION_FIELDS[action]
  if (fields) {
    fields.forEach(({ type }) => { height += FIELD_HEIGHTS[type] || 0 })
  }
  COMMON_FIELDS.forEach(({ type }) => { height += FIELD_HEIGHTS[type] || 0 })
  return height
}

/**
 * 布局主函数
 * 
 * 布局策略：
 * - 真 DAG 模式（有 depends_on）：根据依赖关系分层，同层垂直排列，跨层水平排列
 * - 容器子节点模式（有 parentId）：Parallel/Foreach 子节点在父节点下方垂直排列
 */
export const calculateDAGLayout = (nodes: any[]): { nodes: any[]; edges: any[] } => {
  // 初始化所有节点
  const allNodes = nodes.map(n => ({
    ...n,
    position: { x: 0, y: 0 },
    height: calculateNodeHeight(n.data),
    width: NODE_WIDTH,
  }))
  
  const edges: any[] = []
  const containerNodes: any[] = []
  
  // 检查是否是真正的 DAG（有 depends_on 关系）
  const hasDagRelations = allNodes.some(n => n.data?.depends_on && n.data.depends_on.length > 0)
  
  if (hasDagRelations) {
    // ============ 真 DAG 布局模式 ============
    // 根据 depends_on 计算每个节点的层级
    const levels = new Map<string, number>()
    const visited = new Set<string>()
    
    const calculateLevel = (nodeId: string): number => {
      if (levels.has(nodeId)) return levels.get(nodeId)!
      if (visited.has(nodeId)) return 0 // 防止循环依赖
      
      visited.add(nodeId)
      const node = allNodes.find(n => n.id === nodeId)
      const dependsOn = node?.data?.depends_on || []
      
      if (dependsOn.length === 0) {
        levels.set(nodeId, 0)
        return 0
      }
      
      const maxParentLevel = Math.max(...dependsOn.map((depId: string) => {
        const depNode = allNodes.find(n => n.id === depId)
        if (!depNode) return -1
        return calculateLevel(depId)
      }))
      
      const level = maxParentLevel + 1
      levels.set(nodeId, level)
      return level
    }
    
    // 计算所有节点的层级
    allNodes.forEach(node => calculateLevel(node.id))
    
    // 按层级分组
    const nodesByLevel = new Map<number, any[]>()
    allNodes.forEach(node => {
      const level = levels.get(node.id) || 0
      if (!nodesByLevel.has(level)) nodesByLevel.set(level, [])
      nodesByLevel.get(level)!.push(node)
    })
    
    // 计算每列的宽度和位置
    const levelX = new Map<number, number>()
    let maxX = H_SPACING
    
    // 按层级排序
    const sortedLevels = Array.from(nodesByLevel.keys()).sort((a, b) => a - b)
    
    sortedLevels.forEach((level) => {
      const levelNodes = nodesByLevel.get(level)!
      levelX.set(level, maxX)
      
      // 同层节点垂直排列
      let maxY = 100
      levelNodes.forEach(node => {
        node.position = { x: maxX, y: maxY }
        maxY += node.height + V_SPACING
        
        // 处理容器节点（Parallel/Foreach）
        if (node.data.action === 'parallel' || node.data.action === 'foreach' || node.data.action === 'loop') {
          const branchType = node.data.action === 'parallel' ? 'parallel' : 'do'
          const children = allNodes.filter(
            n => n.data.parentId === node.id && n.data.branchType === branchType
          )
          if (children.length > 0) {
            const parentBottom = node.position.y + node.height + PARENT_CHILD_GAP
            let childY = parentBottom
            children.forEach((child) => {
              child.position = { x: node.position.x, y: childY }
              childY += child.height + V_SPACING
            })
            
            // 容器高度 = 所有子节点高度 + 间距 + 上下边框 padding(10*2)
            const totalChildrenHeight = children.reduce((sum, c) => sum + c.height, 0)
            const totalSpacing = (children.length - 1) * V_SPACING
            const containerHeight = totalChildrenHeight + totalSpacing + 20
            containerNodes.push({
              id: `${node.data.action === 'parallel' ? 'parallel' : 'foreach'}-group-${node.id}`,
              type: node.data.action === 'parallel' ? 'parallelGroup' : 'foreachGroup',
              position: { x: node.position.x - 10, y: parentBottom - 10 },
              draggable: false,
              selectable: false,
              data: {
                width: NODE_WIDTH + 20,
                height: containerHeight,
                taskCount: node.data.action === 'parallel' ? children.length : undefined,
                iterationCount: node.data.action !== 'parallel' ? children.length : undefined,
              },
              style: { zIndex: -1 },
            })
          }
        }
      })
      
      maxX += NODE_WIDTH + H_SPACING
    })
    
    // 生成边（根据 data.next）
    allNodes.forEach(node => {
      const nextIds = node.data?.next || []
      nextIds.forEach((targetId: string) => {
        edges.push({
          id: `edge-${node.id}-${targetId}`,
          source: node.id,
          target: targetId,
          sourceHandle: 'right',
          targetHandle: 'left',
          type: 'default',
          style: { stroke: '#3b82f6', strokeWidth: 2 },
          markerEnd: { type: 'arrowclosed', color: '#3b82f6' },
        })
      })
    })
    
  } else {
    // ============ 线性/容器模式 ============
    // 获取主流程节点（没有 parentId 的节点）
    const rootNodes = allNodes.filter(n => !n.data?.parentId)
    
    // 主流程节点水平排列
    const mainFlowY = 100
    let currentX = H_SPACING
    
    rootNodes.forEach((node, nodeIndex) => {
      node.position = { x: currentX, y: mainFlowY }
      
      // 处理不同类型的容器节点
      if (node.data.action === 'parallel') {
        const children = allNodes.filter(
          n => n.data.parentId === node.id && n.data.branchType === 'parallel'
        ).sort((a, b) => (a.data.branchIndex || 0) - (b.data.branchIndex || 0))
        
        if (children.length > 0) {
          const parentBottom = node.position.y + node.height + PARENT_CHILD_GAP
          let childY = parentBottom
          
          children.forEach((child) => {
            child.position = { x: node.position.x, y: childY }
            childY += child.height + V_SPACING
          })
          
          // 容器节点
          // 容器高度 = 所有子节点高度 + 间距 + 上下边框 padding(10*2)
          const totalChildrenHeight = children.reduce((sum, c) => sum + c.height, 0)
          const totalSpacing = (children.length - 1) * V_SPACING
          const containerHeight = totalChildrenHeight + totalSpacing + 20
          
          containerNodes.push({
            id: `parallel-group-${node.id}`,
            type: 'parallelGroup',
            position: { x: node.position.x - 10, y: parentBottom - 10 },
            draggable: false,
            selectable: false,
            data: {
              width: NODE_WIDTH + 20,
              height: containerHeight,
              taskCount: children.length,
            },
            style: { zIndex: -1 },
          })
          
          // 边：父 → 所有子节点
          children.forEach((child) => {
            edges.push({
              id: `edge-${node.id}-${child.id}`,
              source: node.id,
              target: child.id,
              sourceHandle: 'bottom',
              targetHandle: 'top',
              type: 'bezier',
              style: { stroke: '#a855f7', strokeWidth: 2 },
              markerEnd: { type: 'arrowclosed', color: '#a855f7' },
            })
          })
          
          // 边：所有子节点 → 下一主流程节点
          const nextNode = nodeIndex < rootNodes.length - 1 ? rootNodes[nodeIndex + 1] : null
          if (nextNode) {
            children.forEach((child) => {
              edges.push({
                id: `edge-${child.id}-${nextNode.id}`,
                source: child.id,
                target: nextNode.id,
                sourceHandle: 'right',
                targetHandle: 'left',
                type: 'bezier',
                style: { stroke: '#9ca3af', strokeWidth: 2 },
                markerEnd: { type: 'arrowclosed', color: '#9ca3af' },
              })
            })
          }
        }
      } else if (node.data.action === 'foreach' || node.data.action === 'loop') {
        const children = allNodes.filter(
          n => n.data.parentId === node.id && n.data.branchType === 'do'
        ).sort((a, b) => (a.data.branchIndex || 0) - (b.data.branchIndex || 0))
        
        if (children.length > 0) {
          const parentBottom = node.position.y + node.height + PARENT_CHILD_GAP
          let childY = parentBottom
          
          children.forEach((child) => {
            child.position = { x: node.position.x, y: childY }
            childY += child.height + V_SPACING
          })
          
          // 容器节点
          // 容器高度 = 所有子节点高度 + 间距 + 上下边框 padding(10*2)
          const totalChildrenHeight = children.reduce((sum, c) => sum + c.height, 0)
          const totalSpacing = (children.length - 1) * V_SPACING
          const containerHeight = totalChildrenHeight + totalSpacing + 20
          
          containerNodes.push({
            id: `foreach-group-${node.id}`,
            type: 'foreachGroup',
            position: { x: node.position.x - 10, y: parentBottom - 10 },
            draggable: false,
            selectable: false,
            data: {
              width: NODE_WIDTH + 20,
              height: containerHeight,
              iterationCount: children.length,
            },
            style: { zIndex: -1 },
          })
          
          // 边：父 → 第一个子节点
          edges.push({
            id: `edge-${node.id}-${children[0].id}`,
            source: node.id,
            target: children[0].id,
            sourceHandle: 'bottom',
            targetHandle: 'top',
            type: 'bezier',
            style: { stroke: '#06b6d4', strokeWidth: 2, strokeDasharray: '4,4' },
            markerEnd: { type: 'arrowclosed', color: '#06b6d4' },
          })
          
          // 边：子节点链式连接
          for (let i = 0; i < children.length - 1; i++) {
            edges.push({
              id: `edge-${children[i].id}-${children[i + 1].id}`,
              source: children[i].id,
              target: children[i + 1].id,
              sourceHandle: 'bottom',
              targetHandle: 'top',
              type: 'bezier',
              style: { stroke: '#06b6d4', strokeWidth: 2, strokeDasharray: '4,4' },
              markerEnd: { type: 'arrowclosed', color: '#06b6d4' },
            })
          }
          
          // 边：最后子节点 → 下一主流程节点
          const nextNode = nodeIndex < rootNodes.length - 1 ? rootNodes[nodeIndex + 1] : null
          if (nextNode) {
            const lastChild = children[children.length - 1]
            edges.push({
              id: `edge-${lastChild.id}-${nextNode.id}`,
              source: lastChild.id,
              target: nextNode.id,
              sourceHandle: 'right',
              targetHandle: 'left',
              type: 'bezier',
              style: { stroke: '#9ca3af', strokeWidth: 2 },
              markerEnd: { type: 'arrowclosed', color: '#9ca3af' },
            })
          }
        }
      } else if (node.data.action === 'condition') {
        const thenChildren = allNodes.filter(
          n => n.data.parentId === node.id && n.data.branchType === 'then'
        ).sort((a, b) => (a.data.branchIndex || 0) - (b.data.branchIndex || 0))
        
        const elseChildren = allNodes.filter(
          n => n.data.parentId === node.id && n.data.branchType === 'else'
        ).sort((a, b) => (a.data.branchIndex || 0) - (b.data.branchIndex || 0))
        
        // Then 分支
        if (thenChildren.length > 0) {
          const parentBottom = node.position.y + node.height + PARENT_CHILD_GAP
          let childY = parentBottom
          
          thenChildren.forEach((child) => {
            child.position = { x: node.position.x, y: childY }
            childY += child.height + V_SPACING
          })
          
          // 边：父 → 第一个 then
          edges.push({
            id: `edge-${node.id}-${thenChildren[0].id}`,
            source: node.id,
            target: thenChildren[0].id,
            sourceHandle: 'bottom',
            targetHandle: 'top',
            type: 'bezier',
            style: { stroke: '#22c55e', strokeWidth: 2, strokeDasharray: '4,4' },
            markerEnd: { type: 'arrowclosed', color: '#22c55e' },
            label: 'then',
            labelStyle: { fill: '#22c55e', fontSize: 11, fontWeight: 'bold' },
            labelBgStyle: { fill: 'white', fillOpacity: 0.9 },
          })
          
          // 边：then 子节点链式连接
          for (let i = 0; i < thenChildren.length - 1; i++) {
            edges.push({
              id: `edge-${thenChildren[i].id}-${thenChildren[i + 1].id}`,
              source: thenChildren[i].id,
              target: thenChildren[i + 1].id,
              sourceHandle: 'bottom',
              targetHandle: 'top',
              type: 'bezier',
              style: { stroke: '#22c55e', strokeWidth: 2, strokeDasharray: '4,4' },
              markerEnd: { type: 'arrowclosed', color: '#22c55e' },
            })
          }
          
          // 边：最后 then → 下一主流程节点
          const nextNode = nodeIndex < rootNodes.length - 1 ? rootNodes[nodeIndex + 1] : null
          if (nextNode) {
            edges.push({
              id: `edge-${thenChildren[thenChildren.length - 1].id}-${nextNode.id}`,
              source: thenChildren[thenChildren.length - 1].id,
              target: nextNode.id,
              sourceHandle: 'right',
              targetHandle: 'left',
              type: 'bezier',
              style: { stroke: '#22c55e', strokeWidth: 2 },
              markerEnd: { type: 'arrowclosed', color: '#22c55e' },
            })
          }
        }
        
        // Else 分支
        if (elseChildren.length > 0) {
          const parentBottom = node.position.y + node.height + PARENT_CHILD_GAP
          let childY = parentBottom
          
          elseChildren.forEach((child) => {
            child.position = { x: node.position.x, y: childY }
            childY += child.height + V_SPACING
          })
          
          // 边：父 → 第一个 else
          edges.push({
            id: `edge-${node.id}-${elseChildren[0].id}`,
            source: node.id,
            target: elseChildren[0].id,
            sourceHandle: 'bottom',
            targetHandle: 'top',
            type: 'bezier',
            style: { stroke: '#ef4444', strokeWidth: 2, strokeDasharray: '4,4' },
            markerEnd: { type: 'arrowclosed', color: '#ef4444' },
            label: 'else',
            labelStyle: { fill: '#ef4444', fontSize: 11, fontWeight: 'bold' },
            labelBgStyle: { fill: 'white', fillOpacity: 0.9 },
          })
          
          // 边：else 子节点链式连接
          for (let i = 0; i < elseChildren.length - 1; i++) {
            edges.push({
              id: `edge-${elseChildren[i].id}-${elseChildren[i + 1].id}`,
              source: elseChildren[i].id,
              target: elseChildren[i + 1].id,
              sourceHandle: 'bottom',
              targetHandle: 'top',
              type: 'bezier',
              style: { stroke: '#ef4444', strokeWidth: 2, strokeDasharray: '4,4' },
              markerEnd: { type: 'arrowclosed', color: '#ef4444' },
            })
          }
          
          // 边：最后 else → 下一主流程节点
          const nextNode = nodeIndex < rootNodes.length - 1 ? rootNodes[nodeIndex + 1] : null
          if (nextNode) {
            edges.push({
              id: `edge-${elseChildren[elseChildren.length - 1].id}-${nextNode.id}`,
              source: elseChildren[elseChildren.length - 1].id,
              target: nextNode.id,
              sourceHandle: 'right',
              targetHandle: 'left',
              type: 'bezier',
              style: { stroke: '#ef4444', strokeWidth: 2 },
              markerEnd: { type: 'arrowclosed', color: '#ef4444' },
            })
          }
        }
      }
      
      // 主流程节点之间的连接
      if (nodeIndex < rootNodes.length - 1) {
        const nextNode = rootNodes[nodeIndex + 1]
        edges.push({
          id: `edge-${node.id}-${nextNode.id}`,
          source: node.id,
          target: nextNode.id,
          sourceHandle: 'right',
          targetHandle: 'left',
          type: 'bezier',
          style: { stroke: '#9ca3af', strokeWidth: 2 },
          markerEnd: { type: 'arrowclosed', color: '#9ca3af' },
        })
      }
      
      // 移动到下一个主流程节点位置
      currentX += NODE_WIDTH + H_SPACING
    })
  }
  
  // 构建最终节点列表
  const normalNodes = allNodes.map(node => ({
    ...node,
    type: node.type || 'dag',
    draggable: true,
    style: { ...node.style, zIndex: 1 },
  }))
  
  return { nodes: [...containerNodes, ...normalNodes], edges }
}

/**
 * 循环检测
 */
export const detectCycles = (nodes: any[]): string[][] => {
  const graph = new Map<string, string[]>()
  
  nodes.forEach(node => {
    graph.set(node.id, [])
    if (node.data?.next) {
      (node.data.next as string[]).forEach((targetId: string) => {
        graph.get(node.id)?.push(targetId)
      })
    }
  })
  
  const cycles: string[][] = []
  const visited = new Set<string>()
  const path: string[] = []
  
  const dfs = (nodeId: string) => {
    if (path.includes(nodeId)) {
      const cycleStart = path.indexOf(nodeId)
      cycles.push([...path.slice(cycleStart), nodeId])
      return
    }
    if (visited.has(nodeId)) return
    
    path.push(nodeId)
    const neighbors = graph.get(nodeId) || []
    neighbors.forEach(neighbor => dfs(neighbor))
    path.pop()
    visited.add(nodeId)
  }
  
  nodes.forEach(node => dfs(node.id))
  
  return cycles
}
