// YAML 与 DAG 内部表示的转换工具
import { GraphNode, GraphNodeData, WorkflowDefinition } from '../types/graph'

export const hasContainerStructure = (steps: any[]): boolean => {
  return steps.some((step: any) => step.then || step.else || step.steps || step.do)
}

export const isContainerNode = (data: GraphNodeData): boolean => {
  return ['condition', 'parallel', 'foreach', 'loop'].includes(data.action)
}

/**
 * Workflow YAML → DAG 内部表示
 * 参照原 WorkflowGraphEditor：所有子节点都是独立节点，通过 parentId 关联
 */
export const yamlToDAG = (yaml: WorkflowDefinition): GraphNode[] => {
  const nodes: GraphNode[] = []
  let nodeIndex = 0
  
  // 递归转换步骤（所有节点都是独立的）
  const convertStep = (step: any, parentId?: string, branchType?: string, branchIndex?: number): string[] => {
    const nodeId = step.name || step.id || `node-${nodeIndex++}`
    
    // 先递归处理子节点（创建为独立节点）
    const childIds: string[] = []
    
    if (step.action === 'condition') {
      if (step.then?.length > 0) {
        step.then.forEach((child: any, i: number) => {
          const ids = convertStep(child, nodeId, 'then', i)
          childIds.push(...ids)
        })
      }
      if (step.else?.length > 0) {
        step.else.forEach((child: any, i: number) => {
          const ids = convertStep(child, nodeId, 'else', i)
          childIds.push(...ids)
        })
      }
    } else if (step.action === 'parallel') {
      if (step.steps?.length > 0) {
        step.steps.forEach((child: any, i: number) => {
          const ids = convertStep(child, nodeId, 'parallel', i)
          childIds.push(...ids)
        })
      }
    } else if (step.action === 'foreach' || step.action === 'loop') {
      if (step.do?.length > 0) {
        step.do.forEach((child: any, i: number) => {
          const ids = convertStep(child, nodeId, 'do', i)
          childIds.push(...ids)
        })
      }
    }
    
    // 创建当前节点
    const nodeData: GraphNodeData = {
      name: step.name || nodeId,
      action: step.action || 'log',
      description: step.description,
      parentId,
      branchType,
      branchIndex,
      // 保存子节点数据用于 YAML 导出
      childSteps: step.then || step.else || step.steps,
      doSteps: step.do,
      // DAG 依赖
      next: step.next && Array.isArray(step.next) 
        ? step.next.filter((id: any) => typeof id === 'string')
        : undefined,
      depends_on: step.depends_on && Array.isArray(step.depends_on)
        ? step.depends_on.filter((id: any) => typeof id === 'string')
        : undefined,
      join_mode: step.join_mode,
      if: step.if,
      loop: step.loop,
      run: step.run,
      message: step.message,
      level: step.level,
      url: step.url,
      method: step.method,
      body: step.body,
      script: step.script,
      shell: step.shell || step.command,
      duration: step.duration,
      items: step.items,
      items_text: step.items ? JSON.stringify(step.items) : '',
      item_var: step.item_var,
      variable: step.variable,
      value: step.value,
      template: step.template,
      env: step.env,
      vars: step.vars,
      output: step.output,
      retry: step.retry,
      timeout: step.timeout,
    }
    
    const node: GraphNode = {
      id: nodeId,
      type: 'dag',
      position: { x: 0, y: 0 },
      data: nodeData,
    }
    
    nodes.push(node)
    
    return [nodeId, ...childIds]
  }
  
  // 转换顶层步骤
  yaml.steps?.forEach((step: any) => {
    convertStep(step)
  })
  
  return nodes
}

/**
 * 推断依赖关系（用于没有显式 depends_on/next 的 YAML）
 */
export const inferDependencies = (nodes: GraphNode[]): void => {
  // 1. 处理 Condition 分支
  const conditionNodes = nodes.filter(n => n.data.action === 'condition')
  conditionNodes.forEach(condition => {
    const thenChildren = nodes.filter(n => n.data.parentId === condition.id && n.data.branchType === 'then')
    const elseChildren = nodes.filter(n => n.data.parentId === condition.id && n.data.branchType === 'else')
    
    // Then 分支
    if (thenChildren.length > 0) {
      if (!thenChildren[0].data.depends_on) {
        thenChildren[0].data.depends_on = [condition.id]
      }
      for (let i = 0; i < thenChildren.length - 1; i++) {
        if (!thenChildren[i].data.next) {
          thenChildren[i].data.next = [thenChildren[i + 1].id]
        }
      }
    }
    
    // Else 分支
    if (elseChildren.length > 0) {
      if (!elseChildren[0].data.depends_on) {
        elseChildren[0].data.depends_on = [condition.id]
      }
      for (let i = 0; i < elseChildren.length - 1; i++) {
        if (!elseChildren[i].data.next) {
          elseChildren[i].data.next = [elseChildren[i + 1].id]
        }
      }
    }
    
    // 父节点 next 指向第一个 then 子节点（如果有）
    if (!condition.data.next) {
      const firstChild = thenChildren[0] || elseChildren[0]
      if (firstChild) {
        condition.data.next = [firstChild.id]
      }
    }
  })
  
  // 2. 处理 Parallel 分支
  const parallelNodes = nodes.filter(n => n.data.action === 'parallel')
  parallelNodes.forEach(parallel => {
    const children = nodes.filter(n => n.data.parentId === parallel.id && n.data.branchType === 'parallel')
    if (children.length > 0) {
      // 子节点依赖于父节点
      children.forEach(child => {
        if (!child.data.depends_on) {
          child.data.depends_on = [parallel.id]
        }
      })
      // 父节点的 next 指向所有子节点
      if (!parallel.data.next) {
        parallel.data.next = children.map(c => c.id)
      }
    }
  })
  
  // 3. 处理 Foreach/Loop 分支
  const foreachNodes = nodes.filter(n => n.data.action === 'foreach' || n.data.action === 'loop')
  foreachNodes.forEach(foreach => {
    const children = nodes.filter(n => n.data.parentId === foreach.id && n.data.branchType === 'do')
    if (children.length > 0) {
      // 子节点依赖于父节点
      if (!children[0].data.depends_on) {
        children[0].data.depends_on = [foreach.id]
      }
      // 子节点之间按顺序连接
      for (let i = 0; i < children.length - 1; i++) {
        if (!children[i].data.next) {
          children[i].data.next = [children[i + 1].id]
        }
      }
      // 父节点的 next 指向第一个子节点
      if (!foreach.data.next) {
        foreach.data.next = [children[0].id]
      }
    }
  })
  
  // 4. 主流程节点按顺序添加依赖（同时设置 next 和 depends_on）
  const rootNodes = nodes.filter(n => !n.data.parentId)
    .sort((a, b) => (a.data.branchIndex || 0) - (b.data.branchIndex || 0))
  
  for (let i = 0; i < rootNodes.length - 1; i++) {
    const node = rootNodes[i]
    const nextNode = rootNodes[i + 1]
    // 跳过已有依赖的节点
    if (node.data.next || node.data.depends_on || nextNode.data.depends_on) {
      continue
    }
    // 设置双向依赖
    node.data.next = [nextNode.id]
    nextNode.data.depends_on = [node.id]
  }
}

/**
 * DAG 内部表示 → YAML（自动选择格式）
 * 参照原编辑器：根据 parentId 重新组装嵌套结构
 */
export const dagToYaml = (nodes: GraphNode[]): any[] => {
  return dagToWorkflowYaml(nodes)
}

const dagToWorkflowYaml = (nodes: GraphNode[]): any[] => {
  const rootNodes = nodes.filter(n => !n.data.parentId && !n.id.includes('-group-'))
    .sort((a, b) => (a.data.branchIndex || 0) - (b.data.branchIndex || 0))
  
  const convertNode = (node: GraphNode): any => {
    const step: any = {
      name: node.data.name,
      action: node.data.action,
    }
    
    // 复制业务字段
    if (node.data.description) step.description = node.data.description
    if (node.data.if) step.if = node.data.if
    if (node.data.loop) step.loop = node.data.loop
    if (node.data.run) step.run = node.data.run
    if (node.data.message) step.message = node.data.message
    if (node.data.level) step.level = node.data.level
    if (node.data.url) step.url = node.data.url
    if (node.data.method) step.method = node.data.method
    if (node.data.body) step.body = node.data.body
    if (node.data.script) step.script = node.data.script
    if (node.data.shell) step.shell = node.data.shell
    if (node.data.duration) step.duration = node.data.duration
    if (node.data.items) step.items = node.data.items
    if (node.data.item_var) step.item_var = node.data.item_var
    if (node.data.variable) step.variable = node.data.variable
    if (node.data.value) step.value = node.data.value
    if (node.data.template) step.template = node.data.template
    
    // 保存 DAG 依赖关系（用于真 DAG 模式）
    if (node.data.next && node.data.next.length > 0) {
      step.next = node.data.next
    }
    if (node.data.depends_on && node.data.depends_on.length > 0) {
      step.depends_on = node.data.depends_on
    }
    
    // 根据 action 类型收集子节点
    if (node.data.action === 'condition') {
      const thenChildren = collectChildren(nodes, node.id, 'then')
      const elseChildren = collectChildren(nodes, node.id, 'else')
      if (thenChildren.length > 0) step.then = thenChildren
      if (elseChildren.length > 0) step.else = elseChildren
    } else if (node.data.action === 'parallel') {
      const parallelChildren = collectChildren(nodes, node.id, 'parallel')
      if (parallelChildren.length > 0) step.steps = parallelChildren
    } else if (node.data.action === 'foreach' || node.data.action === 'loop') {
      const doChildren = collectChildren(nodes, node.id, 'do')
      if (doChildren.length > 0) step.do = doChildren
    }
    
    return step
  }
  
  const collectChildren = (allNodes: GraphNode[], parentId: string, branchType: string): any[] => {
    return allNodes
      .filter(n => n.data.parentId === parentId && n.data.branchType === branchType)
      .sort((a, b) => (a.data.branchIndex || 0) - (b.data.branchIndex || 0))
      .map(n => convertNode(n))
  }
  
  return rootNodes.map(n => convertNode(n))
}
