import { MarkerType, type Edge } from '@xyflow/react'
import type { GraphNode, GraphNodeData } from '@/types/graph'
import { NODE_WIDTH, H_SPACING, V_SPACING, PARENT_CHILD_GAP, calculateNodeHeight } from './constants'

// 获取节点高度（根据实际数据智能计算）
const getNodeHeight = (node: GraphNode): number => {
  // 优先使用 data 中缓存的计算高度（如果有）
  if (node.data._calculatedHeight) {
    return node.data._calculatedHeight
  }
  // 否则实时计算
  return calculateNodeHeight(node.data)
}

// 布局系统：根据节点树计算位置并生成连线（主流程水平、子分支垂直、容器边界）
export function calculateLayout(inputNodes: GraphNode[]): { nodes: GraphNode[], edges: Edge[] } {
  // 过滤掉容器节点，保留 measured 和 _calculatedHeight 数据（不使用 deepClone）
  const nodes = inputNodes
    .filter(n => !n.id.includes('-group-'))
    .map(n => ({
      ...n,
      data: {
        ...n.data,
        // 确保 branchIndex 被保留
        branchIndex: n.data.branchIndex,
        parentId: n.data.parentId,
        branchType: n.data.branchType,
      },
      position: { ...n.position },
      measured: n.measured ? { ...n.measured } : undefined,
    }))
  const edges: Edge[] = []

  // 容器边界收集
  const parallelGroups = new Map<string, { x: number; y: number; width: number; height: number; taskCount: number }>()
  const foreachGroups = new Map<string, { x: number; y: number; width: number; height: number; iterationCount: number }>()

  // 找到根节点并按 branchIndex 排序（主流程水平布局）
  const rootNodes = nodes.filter(n => !n.data.parentId).sort((a, b) => (a.data.branchIndex || 0) - (b.data.branchIndex || 0))

  // 主流程 Y 起始位置
  const startY = V_SPACING

  // 递归布局函数 - 返回该分支占用的最大 X 和 Y
  const layoutBranch = (
    branchNodes: GraphNode[],
    startX: number,
    startY: number,
    _parentId?: string,
    branchType?: 'then' | 'else' | 'parallel' | 'do'
  ): { maxX: number, maxY: number } => {
    let currentX = startX
    let currentY = startY
    let maxY = startY
    let maxX = startX

    // 主流程水平布局，子分支根据类型布局
    const isMainFlow = !branchType

    for (const node of branchNodes) {
      // 设置节点位置
      if (isMainFlow) {
        // 主流程节点保持在同一水平线
        node.position = { x: currentX, y: startY }
      } else {
        // 子分支节点垂直排列（使用动态高度）
        node.position = { x: currentX, y: currentY }
      }

      // 先更新 maxX
      maxX = Math.max(maxX, currentX + NODE_WIDTH)

      // 先更新 maxY 为当前节点的底部（使用动态高度）
      const nodeHeight = getNodeHeight(node)
      const nodeBottom = node.position.y + nodeHeight
      if (nodeBottom > maxY) {
        maxY = nodeBottom
      }

      // 处理子节点
      if (node.data.action === 'condition') {
        const thenChildren = nodes.filter(
          n => n.data.parentId === node.id && n.data.branchType === 'then'
        ).sort((a, b) => (a.data.branchIndex || 0) - (b.data.branchIndex || 0))

        const elseChildren = nodes.filter(
          n => n.data.parentId === node.id && n.data.branchType === 'else'
        ).sort((a, b) => (a.data.branchIndex || 0) - (b.data.branchIndex || 0))

        // 布局 then 分支（向右水平展开）
        if (thenChildren.length > 0) {
          const thenResult = layoutBranch(
            thenChildren,
            currentX + NODE_WIDTH + H_SPACING,
            node.position.y,  // then 分支与父节点同一行
            node.id,
            'then'
          )
          maxY = Math.max(maxY, thenResult.maxY)
          maxX = Math.max(maxX, thenResult.maxX)
        }

        // 布局 else 分支（向下垂直展开）
        if (elseChildren.length > 0) {
          const nodeHeight = getNodeHeight(node)
          const elseResult = layoutBranch(
            elseChildren,
            currentX,
            node.position.y + nodeHeight + PARENT_CHILD_GAP,  // 从父节点底部开始
            node.id,
            'else'
          )
          maxY = Math.max(maxY, elseResult.maxY)
          maxX = Math.max(maxX, elseResult.maxX)
        }
      } else if (node.data.action === 'parallel') {
        const parallelChildren = nodes.filter(
          n => n.data.parentId === node.id && n.data.branchType === 'parallel'
        ).sort((a, b) => (a.data.branchIndex || 0) - (b.data.branchIndex || 0))

        // 垂直排列 parallel 子节点（从父节点底部 + PARENT_CHILD_GAP 开始）
        const parentHeight = getNodeHeight(node)
        const childrenStartY = node.position.y + parentHeight + PARENT_CHILD_GAP
        const containerX = node.position.x

        // 使用累积高度计算位置
        let currentY = childrenStartY
        parallelChildren.forEach((child) => {
          const childHeight = getNodeHeight(child)
          child.position = { x: containerX, y: currentY }
          currentY += childHeight + V_SPACING
        })

        if (parallelChildren.length > 0) {
          // maxY 为最后一个子节点的底部
          const lastChild = parallelChildren[parallelChildren.length - 1]
          const lastChildBottom = parallelChildren[parallelChildren.length - 1].position.y + getNodeHeight(lastChild)
          maxY = lastChildBottom
          maxX = Math.max(maxX, containerX + NODE_WIDTH)

          // 记录 Parallel 容器边界
          parallelGroups.set(node.id, {
            x: containerX - 10,
            y: childrenStartY - 10,
            width: NODE_WIDTH + 20,
            height: maxY - childrenStartY + 20,
            taskCount: parallelChildren.length,
          })
        }
      } else if (node.data.action === 'foreach' || node.data.action === 'loop') {
        const doChildren = nodes.filter(
          n => n.data.parentId === node.id && n.data.branchType === 'do'
        ).sort((a, b) => (a.data.branchIndex || 0) - (b.data.branchIndex || 0))

        // 垂直排列 foreach 子节点（从父节点底部 + PARENT_CHILD_GAP 开始）
        const parentHeight = getNodeHeight(node)
        const childrenStartY = node.position.y + parentHeight + PARENT_CHILD_GAP
        const containerX = node.position.x

        // 使用累积高度计算位置，避免间隙
        let currentY = childrenStartY
        doChildren.forEach((child) => {
          child.position = { x: containerX, y: currentY }
          const childHeight = getNodeHeight(child)
          currentY += childHeight + V_SPACING
        })

        if (doChildren.length > 0) {
          // maxY 为最后一个子节点的底部
          const lastChild = doChildren[doChildren.length - 1]
          const lastChildBottom = doChildren[doChildren.length - 1].position.y + getNodeHeight(lastChild)
          maxY = lastChildBottom
          maxX = Math.max(maxX, containerX + NODE_WIDTH)

          // 记录 Foreach 容器边界
          foreachGroups.set(node.id, {
            x: containerX - 10,
            y: childrenStartY - 10,
            width: NODE_WIDTH + 20,
            height: maxY - childrenStartY + 20,
            iterationCount: doChildren.length,
          })
        }
      }

      // 主流程：水平移动到下一个位置
      if (isMainFlow) {
        currentX += NODE_WIDTH + H_SPACING
      } else {
        // 子分支：垂直移动（使用动态高度）
        const nodeHeight = getNodeHeight(node)
        currentY += nodeHeight + V_SPACING
      }
    }

    return { maxX, maxY }
  }

  // 布局主流程（水平排列）
  layoutBranch(rootNodes, H_SPACING, startY)


  // 生成边
  const buildEdges = (nodeList: GraphNode[], parentId?: string) => {

    nodeList.forEach((node, index) => {

      // 父节点 → 当前节点
      if (parentId && index === 0) {
        const branchType = node.data.branchType

        const edgeColor = branchType === 'then' ? '#22c55e' :
                         branchType === 'else' ? '#ef4444' :
                         branchType === 'parallel' ? '#a855f7' : '#06b6d4'
        const edgeLabel = branchType === 'then' ? 'then' :
                         branchType === 'else' ? 'else' : undefined

        edges.push({
          id: `edge-${parentId}-${node.id}`,
          source: parentId,
          target: node.id,
          sourceHandle: 'bottom',  // 从父节点底部发出
          targetHandle: 'top',     // 连接到子节点顶部
          type: 'bezier',
          style: {
            stroke: edgeColor,
            strokeWidth: 2,
            strokeDasharray: branchType ? '4,4' : undefined
          },
          markerEnd: { type: MarkerType.ArrowClosed, color: edgeColor },
          label: edgeLabel,
          labelStyle: { fill: edgeColor, fontSize: 11, fontWeight: 'bold' },
          labelBgStyle: { fill: 'white', fillOpacity: 0.9 },
        })
      }

      // 当前节点 → 下一个节点（主流程）
      if (index < nodeList.length - 1) {
        const nextNode = nodeList[index + 1]
        edges.push({
          id: `edge-${node.id}-${nextNode.id}`,
          source: node.id,
          target: nextNode.id,
          sourceHandle: 'right',   // 从右侧发出
          targetHandle: 'left',    // 连接到左侧
          type: 'bezier',
          style: { stroke: '#9ca3af', strokeWidth: 2 },
          markerEnd: { type: MarkerType.ArrowClosed, color: '#9ca3af' },
        })
      }

      // 递归处理子节点（从完整的 nodes 数组中查找）
      if (node.data.action === 'condition') {
        const thenChildren = nodes.filter(
          n => n.data.parentId === node.id && n.data.branchType === 'then'
        ).sort((a, b) => (a.data.branchIndex || 0) - (b.data.branchIndex || 0))

        const elseChildren = nodes.filter(
          n => n.data.parentId === node.id && n.data.branchType === 'else'
        ).sort((a, b) => (a.data.branchIndex || 0) - (b.data.branchIndex || 0))


        // 找到 condition 后的下一个节点
        const nodeIndex = nodeList.findIndex(n => n.id === node.id)
        const nextNode = nodeIndex >= 0 && nodeIndex < nodeList.length - 1 ? nodeList[nodeIndex + 1] : null

        // 布局 then 分支
        if (thenChildren.length > 0) {
          buildEdges(thenChildren, node.id)

          // then 分支最后一个节点 → 下一节点
          if (nextNode && thenChildren.length > 0) {
            const lastThenChild = thenChildren[thenChildren.length - 1]
            edges.push({
              id: `edge-${lastThenChild.id}-${nextNode.id}`,
              source: lastThenChild.id,
              target: nextNode.id,
              sourceHandle: 'right',
              targetHandle: 'left',
              type: 'bezier',
              style: { stroke: '#22c55e', strokeWidth: 2 },
              markerEnd: { type: MarkerType.ArrowClosed, color: '#22c55e' },
            })
          }
        }

        // 布局 else 分支
        if (elseChildren.length > 0) {
          buildEdges(elseChildren, node.id)

          // else 分支最后一个节点 → 下一节点
          if (nextNode && elseChildren.length > 0) {
            const lastElseChild = elseChildren[elseChildren.length - 1]
            edges.push({
              id: `edge-${lastElseChild.id}-${nextNode.id}`,
              source: lastElseChild.id,
              target: nextNode.id,
              sourceHandle: 'right',
              targetHandle: 'left',
              type: 'bezier',
              style: { stroke: '#ef4444', strokeWidth: 2 },
              markerEnd: { type: MarkerType.ArrowClosed, color: '#ef4444' },
            })
          }
        }
      } else if (node.data.action === 'parallel') {
        // Parallel: 扇出扇入连线
        const children = nodes.filter(
          n => n.data.parentId === node.id && n.data.branchType === 'parallel'
        ).sort((a, b) => (a.data.branchIndex || 0) - (b.data.branchIndex || 0))

        if (children.length === 0) return

        // 扇出：父节点 → 所有子节点（紫色实线）
        children.forEach((child) => {
          edges.push({
            id: `edge-${node.id}-${child.id}`,
            source: node.id,
            target: child.id,
            sourceHandle: 'bottom',
            targetHandle: 'top',
            type: 'bezier',
            style: {
              stroke: '#a855f7',  // 紫色
              strokeWidth: 2,
            },
            markerEnd: { type: MarkerType.ArrowClosed, color: '#a855f7' },
          })
        })

        // 扇入：所有子节点 → 下一节点（灰色实线）
        const nextNode = nodeList.find(n => {
          const nodeIndex = nodeList.findIndex(x => x.id === node.id)
          return nodeIndex >= 0 && nodeList[nodeIndex + 1]?.id === n.id
        })

        if (nextNode) {
          children.forEach((child) => {
            edges.push({
              id: `edge-${child.id}-${nextNode.id}`,
              source: child.id,
              target: nextNode.id,
              sourceHandle: 'right',  // 从子节点右侧发出
              targetHandle: 'left',
              type: 'bezier',
              style: {
                stroke: '#9ca3af',  // 灰色
                strokeWidth: 2,
              },
              markerEnd: { type: MarkerType.ArrowClosed, color: '#9ca3af' },
            })
          })
        }
      } else if (node.data.action === 'foreach' || node.data.action === 'loop') {
        // Foreach: 链式连接
        const children = nodes.filter(
          n => n.data.parentId === node.id && n.data.branchType === 'do'
        ).sort((a, b) => (a.data.branchIndex || 0) - (b.data.branchIndex || 0))

        if (children.length === 0) return

        // 入口：父节点 → 第一个子节点（青色虚线）
        const firstChild = children[0]
        edges.push({
          id: `edge-${node.id}-${firstChild.id}`,
          source: node.id,
          target: firstChild.id,
          sourceHandle: 'bottom',
          targetHandle: 'top',
          type: 'bezier',
          style: {
            stroke: '#06b6d4',  // 青色
            strokeWidth: 2,
            strokeDasharray: '4,4',  // 虚线
          },
          markerEnd: { type: MarkerType.ArrowClosed, color: '#06b6d4' },
        })

        // 链式：子节点依次连接（青色虚线）
        for (let i = 0; i < children.length - 1; i++) {
          edges.push({
            id: `edge-${children[i].id}-${children[i + 1].id}`,
            source: children[i].id,
            target: children[i + 1].id,
            sourceHandle: 'bottom',
            targetHandle: 'top',
            type: 'bezier',
            style: {
              stroke: '#06b6d4',  // 青色
              strokeWidth: 2,
              strokeDasharray: '4,4',  // 虚线
            },
            markerEnd: { type: MarkerType.ArrowClosed, color: '#06b6d4' },
          })
        }

        // 出口：最后子节点 → 下一节点（灰色实线）
        const nextNode = nodeList.find(n => {
          const nodeIndex = nodeList.findIndex(x => x.id === node.id)
          return nodeIndex >= 0 && nodeList[nodeIndex + 1]?.id === n.id
        })

        if (nextNode) {
          const lastChild = children[children.length - 1]
          edges.push({
            id: `edge-${lastChild.id}-${nextNode.id}`,
            source: lastChild.id,
            target: nextNode.id,
            sourceHandle: 'right',  // 从子节点右侧发出
            targetHandle: 'left',
            type: 'bezier',
            style: {
              stroke: '#9ca3af',  // 灰色
              strokeWidth: 2,
            },
            markerEnd: { type: MarkerType.ArrowClosed, color: '#9ca3af' },
          })
        }
      }
    })
  }

  buildEdges(rootNodes)


  // 添加容器节点（zIndex: -1 让它们在普通节点下方）
  const containerNodes: GraphNode[] = []

  // Parallel 容器节点
  parallelGroups.forEach((bounds, parallelId) => {
    containerNodes.push({
      id: `parallel-group-${parallelId}`,
      type: 'parallelGroup',
      position: { x: bounds.x, y: bounds.y },
      positionAbsolute: { x: bounds.x, y: bounds.y },
      draggable: false,
      selectable: false,
      data: {
        width: bounds.width,
        height: bounds.height,
        taskCount: bounds.taskCount,
      } as unknown as GraphNodeData,
      style: {
        zIndex: -1,  // 容器节点在底层
        pointerEvents: 'none' as const,  // 防止容器拦截鼠标事件
      },
    })
  })

  // Foreach 容器节点
  foreachGroups.forEach((bounds, foreachId) => {
    containerNodes.push({
      id: `foreach-group-${foreachId}`,
      type: 'foreachGroup',
      position: { x: bounds.x, y: bounds.y },
      positionAbsolute: { x: bounds.x, y: bounds.y },
      draggable: false,
      selectable: false,
      data: {
        width: bounds.width,
        height: bounds.height,
        iterationCount: bounds.iterationCount,
      } as unknown as GraphNodeData,
      style: {
        zIndex: -1,  // 容器节点在底层
        pointerEvents: 'none' as const,  // 防止容器拦截鼠标事件
      },
    })
  })

  // 给普通节点设置更高的 zIndex
  const normalNodes = nodes.map(node => ({
    ...node,
    positionAbsolute: node.position,
    style: {
      ...node.style,
      zIndex: 1,
    },
  }))

  // 容器节点在前（底层），普通节点在后（上层）
  return { nodes: [...containerNodes, ...normalNodes], edges }
}
