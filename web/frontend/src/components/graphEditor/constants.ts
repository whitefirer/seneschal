import type { LucideIcon } from 'lucide-react'
import {
  Globe, Code, Terminal, MessageSquare, GitBranch, RotateCcw, Zap, FileText, Moon,
} from 'lucide-react'
import type { GraphNodeData } from '@/types/graph'

// ==================== 常量配置 ====================

// 节点尺寸
export const NODE_WIDTH = 220
export const BASE_NODE_HEIGHT = 150  // 基础高度（header + 最小内容区）

// 间距
export const H_SPACING = 80        // 水平间距
export const V_SPACING = 40        // 垂直间距（子节点间）- 减小间距
export const PARENT_CHILD_GAP = 60  // 父子节点间距（给容器标签留空间）

// 字段高度配置
export const FIELD_HEIGHTS = {
  input: 32,      // 单行输入
  textarea: 80,   // 多行文本（固定高度）
  select: 32,     // 下拉选择
  button: 36,     // 按钮
}

// 不同 action 的字段配置
export const ACTION_FIELDS: Record<string, Array<{ type: keyof typeof FIELD_HEIGHTS; field: string }>> = {
  'log': [
    { type: 'textarea', field: 'message' },
    { type: 'select', field: 'level' },
  ],
  'http': [
    { type: 'input', field: 'url' },
    { type: 'select', field: 'method' },
  ],
  'shell': [{ type: 'textarea', field: 'shell' }],
  'script': [{ type: 'textarea', field: 'script' }],
  'sleep': [{ type: 'input', field: 'duration' }],
  'condition': [{ type: 'input', field: 'if' }],
  'parallel': [],  // 只有 header 和 Add Task 按钮
  'foreach': [{ type: 'input', field: 'item_var' }, { type: 'textarea', field: 'items_text' }],
  'loop': [{ type: 'input', field: 'item_var' }, { type: 'textarea', field: 'items_text' }],
}

const actionConfigs: Record<string, { icon: LucideIcon; color: string; bgColor: string; borderColor: string; label: string }> = {
  http: { icon: Globe, color: 'text-blue-500', bgColor: 'bg-blue-50 dark:bg-blue-900/20', borderColor: 'border-blue-500', label: 'HTTP' },
  script: { icon: Code, color: 'text-purple-500', bgColor: 'bg-purple-50 dark:bg-purple-900/20', borderColor: 'border-purple-500', label: 'Script' },
  shell: { icon: Terminal, color: 'text-green-500', bgColor: 'bg-green-50 dark:bg-green-900/20', borderColor: 'border-green-500', label: 'Shell' },
  log: { icon: MessageSquare, color: 'text-yellow-500', bgColor: 'bg-yellow-50 dark:bg-yellow-900/20', borderColor: 'border-yellow-500', label: 'Log' },
  condition: { icon: GitBranch, color: 'text-orange-500', bgColor: 'bg-orange-50 dark:bg-orange-900/20', borderColor: 'border-orange-500', label: 'Condition' },
  loop: { icon: RotateCcw, color: 'text-cyan-500', bgColor: 'bg-cyan-50 dark:bg-cyan-900/20', borderColor: 'border-cyan-500', label: 'Loop' },
  foreach: { icon: RotateCcw, color: 'text-cyan-500', bgColor: 'bg-cyan-50 dark:bg-cyan-900/20', borderColor: 'border-cyan-500', label: 'Foreach' },
  parallel: { icon: Zap, color: 'text-pink-500', bgColor: 'bg-pink-50 dark:bg-pink-900/20', borderColor: 'border-pink-500', label: 'Parallel' },
  sleep: { icon: Moon, color: 'text-indigo-500', bgColor: 'bg-indigo-50 dark:bg-indigo-900/20', borderColor: 'border-indigo-500', label: 'Sleep' },
  '': { icon: FileText, color: 'text-gray-500', bgColor: 'bg-gray-50 dark:bg-gray-900/20', borderColor: 'border-gray-400', label: 'Action' },
}

export function getActionConfig(action: string) {
  if (action === 'loop') return actionConfigs['foreach']
  return actionConfigs[action] || actionConfigs['']
}

// ==================== 工具函数 ====================

// 生成唯一 ID
export const generateId = () => `node-${Date.now()}-${Math.random().toString(36).substr(2, 9)}`

// 根据节点数据智能计算高度
export const calculateNodeHeight = (data: GraphNodeData): number => {
  let height = BASE_NODE_HEIGHT  // 基础高度

  const action = data.action || ''
  const fields = ACTION_FIELDS[action]

  if (fields) {
    fields.forEach(({ type }) => {
      // 不管字段有没有值，只要 action 有这个字段就计算高度
      // 因为渲染时字段总会显示（即使为空）
      height += FIELD_HEIGHTS[type]
    })
  }

  // 如果是容器节点且有 Add 按钮，增加按钮高度
  if (['parallel', 'foreach', 'loop'].includes(action)) {
    height += FIELD_HEIGHTS.button
  }

  // 如果有子节点，显示 child steps 计数，增加额外高度
  const hasChildren = (data.childSteps && data.childSteps.length > 0) ||
                      (data.doSteps && data.doSteps.length > 0)
  if (hasChildren && !data.parentId) {
    height += 40  // child steps 计数区域的高度（margin + padding + border + text）
  }

  return height
}
