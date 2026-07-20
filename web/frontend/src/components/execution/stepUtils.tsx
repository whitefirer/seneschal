import { RefreshCw, CheckCircle, XCircle, Clock, AlertCircle } from 'lucide-react'
import type { Step } from '@/types/execution'

// 步骤状态图标
export function getStatusIcon(stepStatus: string) {
  switch (stepStatus) {
    case 'success':
      return <CheckCircle className="w-5 h-5 text-green-500" />
    case 'running':
      return <RefreshCw className="w-5 h-5 text-blue-500 animate-spin" />
    case 'failed':
      return <XCircle className="w-5 h-5 text-red-500" />
    case 'skipped':
      return <AlertCircle className="w-5 h-5 text-yellow-500" />
    default:
      return <Clock className="w-5 h-5 text-gray-400" />
  }
}

// 步骤状态对应的卡片底色/边框色
export function getStatusColor(stepStatus: string) {
  switch (stepStatus) {
    case 'success':
      return 'bg-green-50 dark:bg-green-900/20 border-green-200 dark:border-green-800'
    case 'running':
      return 'bg-blue-50 dark:bg-blue-900/20 border-blue-200 dark:border-blue-800'
    case 'failed':
      return 'bg-red-50 dark:bg-red-900/20 border-red-200 dark:border-red-800'
    case 'skipped':
      return 'bg-yellow-50 dark:bg-yellow-900/20 border-yellow-200 dark:border-yellow-800'
    default:
      return 'bg-gray-50 dark:bg-gray-900/20 border-gray-200 dark:border-gray-800'
  }
}

// 计算步骤耗时（无开始时间显示占位符）
export function calculateDuration(startTime?: string, endTime?: string): string {
  if (!startTime) return '-'
  try {
    const start = new Date(startTime)
    const end = endTime ? new Date(endTime) : new Date()
    const diff = end.getTime() - start.getTime()
    const seconds = Math.floor(diff / 1000)
    if (seconds < 60) return `${seconds}s`
    const minutes = Math.floor(seconds / 60)
    const secs = seconds % 60
    return `${minutes}m ${secs}s`
  } catch {
    return '-'
  }
}

// 在步骤树中按 ID 查找（含 condition 的 then/else 分支）
export function findStepInTree(steps: Step[], stepId: string): Step | null {
  for (const step of steps) {
    if (step.id === stepId) {
      return step
    }
    if (step.children && step.children.length > 0) {
      const found = findStepInTree(step.children, stepId)
      if (found) return found
    }
    if (step.then_children && step.then_children.length > 0) {
      const found = findStepInTree(step.then_children, stepId)
      if (found) return found
    }
    if (step.else_children && step.else_children.length > 0) {
      const found = findStepInTree(step.else_children, stepId)
      if (found) return found
    }
  }
  return null
}

// 按 ID 更新步骤树节点，并在所有子节点完成时回写父节点状态
export function updateStepInTree(steps: Step[], stepId: string, updates: Partial<Step>): Step[] {
  const updateWithParent = (stepList: Step[]): Step[] => {
    return stepList.map((step) => {
      if (step.id === stepId) {
        return { ...step, ...updates }
      }
      // 处理 children
      if (step.children && step.children.length > 0) {
        const updatedChildren = updateWithParent(step.children)
        // Check if all children are complete
        const allChildrenComplete = updatedChildren.every(c => c.status === 'success' || c.status === 'failed' || c.status === 'skipped')
        const anyChildFailed = updatedChildren.some(c => c.status === 'failed')

        let parentUpdates = {}
        if (allChildrenComplete) {
          parentUpdates = { status: anyChildFailed ? 'failed' : 'success' as Step['status'] }
        }

        return {
          ...step,
          children: updatedChildren,
          ...parentUpdates
        }
      }
      // 处理 condition 的 then_children 和 else_children
      if (step.then_children && step.then_children.length > 0) {
        const updatedThenChildren = updateWithParent(step.then_children)
        step = { ...step, then_children: updatedThenChildren }
      }
      if (step.else_children && step.else_children.length > 0) {
        const updatedElseChildren = updateWithParent(step.else_children)
        step = { ...step, else_children: updatedElseChildren }
      }
      return step
    })
  }
  const result = updateWithParent(steps)
  return result
}

// step_output 事件：把输出片段追加到匹配步骤的 output 后（带换行）
export function appendStepOutput(steps: Step[], stepId: string, stepName: string, output: string): Step[] {
  const updateOutput = (stepList: Step[]): Step[] => {
    return stepList.map((step) => {
      // 检查当前节点是否匹配
      if (step.id === stepId || step.name === stepName) {
        return { ...step, output: (step.output || '') + output + '\n' }
      }

      // 检查并更新 children
      if (step.children && step.children.length > 0) {
        const updatedChildren = updateOutput(step.children)
        // 如果 children 有变化，返回更新后的 step
        if (updatedChildren.some((c, i) => c.output !== step.children?.[i]?.output || c !== step.children?.[i])) {
          return { ...step, children: updatedChildren }
        }
      }

      // 检查并更新 condition 的 then_children 和 else_children
      if (step.then_children && step.then_children.length > 0) {
        const updatedThen = updateOutput(step.then_children)
        if (updatedThen.some((c, i) => c.output !== step.then_children?.[i]?.output || c !== step.then_children?.[i])) {
          step = { ...step, then_children: updatedThen }
        }
      }
      if (step.else_children && step.else_children.length > 0) {
        const updatedElse = updateOutput(step.else_children)
        if (updatedElse.some((c, i) => c.output !== step.else_children?.[i]?.output || c !== step.else_children?.[i])) {
          step = { ...step, else_children: updatedElse }
        }
      }

      return step
    })
  }
  const result = updateOutput(steps)
  return result
}

// ai_token 事件：把增量 token 追加到匹配步骤的 aiOutput（不加换行）
export function appendStepAiToken(steps: Step[], stepId: string, stepName: string, token: string): Step[] {
  const appendToken = (stepList: Step[]): Step[] => {
    return stepList.map((step) => {
      if (step.id === stepId || step.name === stepName) {
        return { ...step, aiOutput: (step.aiOutput || '') + token }
      }
      if (step.children?.length) {
        const next = appendToken(step.children)
        if (next !== step.children) return { ...step, children: next }
      }
      if (step.then_children?.length) {
        const next = appendToken(step.then_children)
        if (next !== step.then_children) return { ...step, then_children: next }
      }
      if (step.else_children?.length) {
        const next = appendToken(step.else_children)
        if (next !== step.else_children) return { ...step, else_children: next }
      }
      return step
    })
  }
  return appendToken(steps)
}
