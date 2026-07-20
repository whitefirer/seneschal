// @ts-nocheck - DAG 重构，类型转换中
import { useState, useCallback, useMemo, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import {
  ReactFlow,
  Background,
  Controls,
  Node,
  Edge,
  Position,
  Handle,
  BackgroundVariant,
  MarkerType,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { 
  Plus, Save, Play, Globe, Code, Terminal, MessageSquare, 
  GitBranch, RotateCcw, Zap, FileText, Trash2, Moon,
  AlertCircle, CheckCircle, Layers, Repeat
} from 'lucide-react'

// 导入 DAG 转换工具
import { yamlToDAG, dagToYaml } from '../utils/yaml-converter'

// 导入 DAG 转换工具
import { yamlToDAG, dagToYaml } from '../utils/yaml-converter'

// ==================== 类型定义 ====================

interface GraphNodeData {
  name: string
  action: 'http' | 'script' | 'shell' | 'log' | 'condition' | 'loop' | 'foreach' | 'parallel' | 'sleep' | ''
  
  // 父级关系（用于嵌套结构）
  parentId?: string
  branchType?: 'then' | 'else' | 'parallel' | 'do'
  branchIndex?: number
  
  // DAG 核心字段（统一内部表示）
  next?: string[]       // 后继节点 ID 列表
  depends_on?: string[] // 前驱节点 ID 列表
  join_mode?: 'all' | 'any'
  
  // 节点配置
  if?: string
  loop?: string
  run?: string
  message?: string
  level?: string
  url?: string
  method?: string
  body?: string
  script?: string
  shell?: string
  duration?: string
  description?: string
  
  // 子节点数据（用于 YAML 导出）
  childSteps?: any[]
  doSteps?: any[]
  
  // UI 状态
  isCollapsed?: boolean
  bgColor?: string
  borderColor?: string
  
  // 索引签名（满足 Record<string, unknown> 约束）
  [key: string]: any
}

interface GraphNode extends Node {
  data: GraphNodeData
  positionAbsolute?: { x: number; y: number }
  measured?: {
    width: number
    height: number
  }
}

interface WorkflowGraphEditorProps {
  initialSteps?: any[]
  onSave?: (steps: any[]) => void
  onRun?: (steps: any[]) => void
}

// ==================== 常量配置 ====================

// 节点尺寸
const NODE_WIDTH = 220
const BASE_NODE_HEIGHT = 150  // 基础高度（header + 最小内容区）

// 间距
const H_SPACING = 80        // 水平间距
const V_SPACING = 40        // 垂直间距（子节点间）- 减小间距
const PARENT_CHILD_GAP = 60  // 父子节点间距（给容器标签留空间）

// 字段高度配置
const FIELD_HEIGHTS = {
  input: 32,      // 单行输入
  textarea: 80,   // 多行文本（固定高度）
  select: 32,     // 下拉选择
  button: 36,     // 按钮
}

// 不同 action 的字段配置
const ACTION_FIELDS: Record<string, Array<{type: keyof typeof FIELD_HEIGHTS; field: string}>> = {
  'log': [
    {type: 'textarea', field: 'message'},
    {type: 'select', field: 'level'},
  ],
  'http': [
    {type: 'input', field: 'url'},
    {type: 'select', field: 'method'},
  ],
  'shell': [{type: 'textarea', field: 'shell'}],
  'script': [{type: 'textarea', field: 'script'}],
  'sleep': [{type: 'input', field: 'duration'}],
  'condition': [{type: 'input', field: 'if'}],
  'parallel': [],  // 只有 header 和 Add Task 按钮
  'foreach': [{type: 'input', field: 'item_var'}, {type: 'textarea', field: 'items_text'}],
  'loop': [{type: 'input', field: 'item_var'}, {type: 'textarea', field: 'items_text'}],
}

const actionConfigs: Record<string, { icon: any; color: string; bgColor: string; borderColor: string; label: string }> = {
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

function getActionConfig(action: string) {
  if (action === 'loop') return actionConfigs['foreach']
  return actionConfigs[action] || actionConfigs['']
}

// ==================== 工具函数 ====================

// 生成唯一 ID
const generateId = () => `node-${Date.now()}-${Math.random().toString(36).substr(2, 9)}`

// 根据节点数据智能计算高度
const calculateNodeHeight = (data: GraphNodeData): number => {
  let height = BASE_NODE_HEIGHT  // 基础高度
  
  const action = data.action || ''
  const fields = ACTION_FIELDS[action]
  
  if (fields) {
    fields.forEach(({type}) => {
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

// ==================== 容器节点组件 ====================

// Parallel 容器节点
function ParallelGroupNode({ data }: { id: string; data: { width: number; height: number; taskCount?: number } }) {
  return (
    <div
      className="rounded-xl border-2 border-dashed border-pink-400 dark:border-pink-500 bg-pink-50/30 dark:bg-pink-900/10"
      style={{
        width: data.width,
        height: data.height,
      }}
    >
      {/* 并行标记 */}
      <div className="absolute -top-4 left-4 px-2 py-1 bg-pink-100 dark:bg-pink-900 rounded text-xs font-medium text-pink-700 dark:text-pink-300 flex items-center gap-1 z-20 shadow-sm">
        <Layers className="w-3 h-3" />
        <span>Parallel</span>
        {data.taskCount && (
          <span className="text-pink-500">({data.taskCount} tasks)</span>
        )}
      </div>
    </div>
  )
}

// Foreach 容器节点
function ForeachGroupNode({ data }: { id: string; data: { width: number; height: number; iterationCount?: number } }) {
  return (
    <div
      className="rounded-xl border-2 border-dashed border-cyan-400 dark:border-cyan-500 bg-cyan-50/30 dark:bg-cyan-900/10"
      style={{
        width: data.width,
        height: data.height,
      }}
    >
      {/* 循环标记 */}
      <div className="absolute -top-4 left-4 px-2 py-1 bg-cyan-100 dark:bg-cyan-900 rounded text-xs font-medium text-cyan-700 dark:text-cyan-300 flex items-center gap-1 z-20 shadow-sm">
        <Repeat className="w-3 h-3" />
        <span>Foreach</span>
        {data.iterationCount && (
          <span className="text-cyan-500">({data.iterationCount} items)</span>
        )}
      </div>
    </div>
  )
}

// ==================== 自定义节点组件 ====================

function EditorNode({ 
  id, 
  data, 
  selected,
  onAddChild,
  onDelete,
  onDataChange
}: { 
  id: string
  data: GraphNodeData
  selected?: boolean
  onAddChild: (parentId: string, branchType: 'then' | 'else' | 'parallel' | 'do') => void
  onDelete: (nodeId: string) => void
  onDataChange: (nodeId: string, newData: Partial<GraphNodeData>) => void
}) {
  const { t } = useTranslation()
  const config = getActionConfig(data.action)
  const Icon = config.icon

  const handleFieldChange = (field: string, value: any) => {
    // 节点名需要转换为合法格式（小写 + 连字符）
    if (field === 'name') {
      const sanitizedName = String(value)
        .toLowerCase()
        .replace(/[^a-z0-9-]/g, '-')  // 非字母数字字符替换为连字符
        .replace(/-+/g, '-')          // 多个连字符合并为一个
        .replace(/^-|-$/g, '')        // 移除首尾连字符
      onDataChange(id, { [field]: sanitizedName })
    } else {
      // 其他字段变化时，重新计算高度
      const newData = { [field]: value }
      const newHeight = calculateNodeHeight({ ...data, ...newData })
      onDataChange(id, { ...newData, _calculatedHeight: newHeight })
    }
  }

  const handleActionChange = (action: string) => {
    // 切换 action 时，计算新高度并一起更新
    const newData = { action: action as GraphNodeData['action'] }
    const newHeight = calculateNodeHeight({ ...data, ...newData })
    onDataChange(id, { ...newData, _calculatedHeight: newHeight })
  }

  const hasChildren = (data.childSteps && data.childSteps.length > 0) || 
                      (data.doSteps && data.doSteps.length > 0) ||
                      (data.branchType && data.parentId)

  return (
    <div
      className={`group rounded-xl shadow-lg border-2 min-w-[${NODE_WIDTH}px] max-w-[${NODE_WIDTH}px] transition-all flex flex-col ${
        data.bgColor || config.bgColor
      } ${data.borderColor || config.borderColor} ${
        selected ? 'ring-2 ring-blue-500 ring-offset-2' : ''
      }`}
      style={{ 
        width: NODE_WIDTH,
        height: data._calculatedHeight || calculateNodeHeight(data),  // 使用计算高度
        zIndex: selected ? 100 : 1,  // 选中时在最上层
      }}
    >
      {/* 主流程连线 Handle（左右） */}
      <Handle type="target" position={Position.Left} className="!bg-gray-400 !w-3 !h-3" id="left" />
      <Handle type="source" position={Position.Right} className="!bg-gray-400 !w-3 !h-3" id="right" />
      
      {/* 父子节点连线 Handle（上/下） - 仅当有子节点或父节点时显示 */}
      {(data.action === 'parallel' || data.action === 'foreach' || data.action === 'loop' || data.parentId) && (
        <>
          <Handle type="target" position={Position.Top} className="!bg-purple-400 !w-2 !h-2" id="top" />
          <Handle type="source" position={Position.Bottom} className="!bg-purple-400 !w-2 !h-2" id="bottom" />
        </>
      )}
      
      {/* 拖动区域 - 左侧拖动手柄（hover 显示，长条形） */}
      <div 
        className="absolute left-0 top-0 bottom-0 w-6 cursor-grab active:cursor-grabbing flex items-center justify-center hover:bg-gray-100 dark:hover:bg-gray-800 rounded-l-xl transition-colors z-50 group-hover:opacity-100 opacity-0"
        title={t('common.drag')}
      >
        <div className="w-1 h-8 bg-gray-300 dark:bg-gray-600 rounded-full"></div>
      </div>
      
      {/* Header */}
      <div 
        className="flex items-center gap-2 p-3 border-b border-gray-200 dark:border-gray-700 overflow-hidden"
      >
        <div className={`p-1.5 rounded bg-white dark:bg-gray-800 shadow-sm flex-shrink-0 ${config.color}`}>
          <Icon className="w-4 h-4" />
        </div>
        <input
          type="text"
          value={data.name}
          onChange={(e) => handleFieldChange('name', e.target.value)}
          className="flex-1 min-w-0 bg-transparent border-none outline-none text-sm font-semibold text-gray-800 dark:text-gray-100 nodrag"
          placeholder="Node name"
        />
        <button
          onClick={(e) => {
            e.stopPropagation()
            onDelete(id)
          }}
          className="p-1 hover:bg-red-100 dark:hover:bg-red-900/30 rounded transition-colors flex-shrink-0 nodrag"
          title="Delete node"
        >
          <Trash2 className="w-4 h-4 text-red-500" />
        </button>
      </div>

      {/* Body - 内容区 */}
      <div className="p-3 space-y-2 flex-1">  {/* 空白区域可拖动 */}
        {/* Action selector */}
        <select
          value={data.action}
          onChange={(e) => handleActionChange(e.target.value)}
          className="w-full px-2 py-1.5 text-xs border border-gray-300 dark:border-gray-600 rounded bg-white dark:bg-gray-700 focus:ring-2 focus:ring-blue-500 nodrag"
        >
          <option value="log">💬 Log Message</option>
          <option value="http">🌐 HTTP Request</option>
          <option value="script">💻 JavaScript</option>
          <option value="shell">🖥 Shell Command</option>
          <option value="condition">🔀 Condition</option>
          <option value="foreach">🔄 Foreach</option>
          <option value="parallel">⚡ Parallel</option>
          <option value="sleep">💤 Sleep</option>
        </select>

        {/* Description */}
        <input
          type="text"
          value={data.description || ''}
          onChange={(e) => handleFieldChange('description', e.target.value)}
          placeholder="Description (optional)"
          className="w-full px-2 py-1.5 text-xs border border-gray-300 dark:border-gray-600 rounded bg-white dark:bg-gray-700 focus:ring-2 focus:ring-blue-500 nodrag"
        />

        {/* Action-specific fields */}
        {data.action === 'http' && (
          <>
            <input
              type="text"
              value={data.url || ''}
              onChange={(e) => handleFieldChange('url', e.target.value)}
              placeholder="URL"
              className="w-full px-2 py-1.5 text-xs border border-gray-300 dark:border-gray-600 rounded bg-white dark:bg-gray-700 nodrag"
            />
            <select
              value={data.method || 'GET'}
              onChange={(e) => handleFieldChange('method', e.target.value)}
              className="w-full px-2 py-1.5 text-xs border border-gray-300 dark:border-gray-600 rounded bg-white dark:bg-gray-700 nodrag"
            >
              <option value="GET">GET</option>
              <option value="POST">POST</option>
              <option value="PUT">PUT</option>
              <option value="DELETE">DELETE</option>
              <option value="PATCH">PATCH</option>
            </select>
          </>
        )}

        {data.action === 'shell' && (
          <textarea
            value={data.shell || ''}
            onChange={(e) => handleFieldChange('shell', e.target.value)}
            placeholder="Shell command"
            rows={3}
            className="w-full px-2 py-1.5 text-xs border border-gray-300 dark:border-gray-600 rounded bg-white dark:bg-gray-700 font-mono resize-none overflow-auto nodrag"
            style={{ height: '80px' }}  // 固定高度
          />
        )}

        {data.action === 'log' && (
          <>
            <textarea
              value={data.message || ''}
              onChange={(e) => handleFieldChange('message', e.target.value)}
              placeholder="Log message"
              rows={3}
              className="w-full px-2 py-1.5 text-xs border border-gray-300 dark:border-gray-600 rounded bg-white dark:bg-gray-700 resize-none overflow-auto nodrag"
              style={{ height: '80px' }}  // 固定高度
            />
            <select
              value={data.level || 'info'}
              onChange={(e) => handleFieldChange('level', e.target.value)}
              className="w-full px-2 py-1.5 text-xs border border-gray-300 dark:border-gray-600 rounded bg-white dark:bg-gray-700 nodrag"
            >
              <option value="info">INFO</option>
              <option value="warn">WARN</option>
              <option value="error">ERROR</option>
            </select>
          </>
        )}

        {data.action === 'sleep' && (
          <input
            type="text"
            value={data.duration || ''}
            onChange={(e) => handleFieldChange('duration', e.target.value)}
            placeholder="Duration (e.g., 5s, 1m)"
            className="w-full px-2 py-1.5 text-xs border border-gray-300 dark:border-gray-600 rounded bg-white dark:bg-gray-700 nodrag"
          />
        )}

        {/* Add child buttons for container nodes */}
        {data.action === 'condition' && (
          <div className="flex gap-2 mt-3 pt-3 border-t border-gray-200 dark:border-gray-700">
            <button
              onClick={() => onAddChild(id, 'then')}
              className="flex-1 px-2 py-1.5 text-xs bg-green-100 dark:bg-green-900/30 hover:bg-green-200 dark:hover:bg-green-900/50 text-green-700 dark:text-green-300 rounded transition-colors flex items-center justify-center gap-1 nodrag"
            >
              <Plus className="w-3 h-3" />
              Add Then
            </button>
            <button
              onClick={() => onAddChild(id, 'else')}
              className="flex-1 px-2 py-1.5 text-xs bg-red-100 dark:bg-red-900/30 hover:bg-red-200 dark:hover:bg-red-900/50 text-red-700 dark:text-red-300 rounded transition-colors flex items-center justify-center gap-1 nodrag"
            >
              <Plus className="w-3 h-3" />
              Add Else
            </button>
          </div>
        )}

        {data.action === 'parallel' && (
          <button
            onClick={() => onAddChild(id, 'parallel')}
            className="w-full mt-3 px-2 py-1.5 text-xs bg-pink-100 dark:bg-pink-900/30 hover:bg-pink-200 dark:hover:bg-pink-900/50 text-pink-700 dark:text-pink-300 rounded transition-colors flex items-center justify-center gap-1 nodrag"
          >
            <Plus className="w-3 h-3" />
            Add Task
          </button>
        )}

        {data.action === 'foreach' && (
          <>
            <input
              type="text"
              value={data.item_var || 'item'}
              onChange={(e) => handleFieldChange('item_var', e.target.value)}
              placeholder="Item variable name (e.g., item)"
              className="w-full px-2 py-1.5 text-xs border border-gray-300 dark:border-gray-600 rounded bg-white dark:bg-gray-700 nodrag"
            />
            <textarea
              value={data.items_text || (data.items ? JSON.stringify(data.items) : '')}
              onChange={(e) => {
                const text = e.target.value
                // 始终更新文本，方便用户输入
                handleFieldChange('items_text', text)
                // 尝试解析为 JSON，如果有效则更新 items
                try {
                  const items = JSON.parse(text)
                  if (Array.isArray(items)) {
                    handleFieldChange('items', items)
                  }
                } catch {
                  // 不是有效 JSON，只保留文本
                }
              }}
              placeholder='JSON array, e.g. ["a", "b", "c"]'
              rows={3}
              className="w-full px-2 py-1.5 text-xs border border-gray-300 dark:border-gray-600 rounded bg-white dark:bg-gray-700 font-mono resize-none overflow-auto nodrag"
              style={{ height: '80px' }}
            />
            <button
              onClick={() => onAddChild(id, 'do')}
              className="w-full mt-3 px-2 py-1.5 text-xs bg-cyan-100 dark:bg-cyan-900/30 hover:bg-cyan-200 dark:hover:bg-cyan-900/50 text-cyan-700 dark:text-cyan-300 rounded transition-colors flex items-center justify-center gap-1 nodrag"
            >
              <Plus className="w-3 h-3" />
              Add Step
            </button>
          </>
        )}

        {/* Show children count */}
        {hasChildren && !data.parentId && (
          <div className="mt-2 pt-2 border-t border-gray-200 dark:border-gray-700 text-xs text-gray-500">
            {(data.childSteps?.length || 0) + (data.doSteps?.length || 0)} child steps
          </div>
        )}
      </div>
    </div>
  )
}

// ==================== 主组件 ====================

export default function WorkflowGraphEditor({ 
  initialSteps = [], 
  onSave, 
  onRun 
}: WorkflowGraphEditorProps) {
  // 节点和边状态
  const [nodes, setNodes] = useState<GraphNode[]>([])
  const [edges, setEdges] = useState<Edge[]>([])
  
  // 未保存状态
  const [hasUnsavedChanges, setHasUnsavedChanges] = useState(false)
  const [originalStepsHash, setOriginalStepsHash] = useState<string>('')

  // ==================== 工具函数 ====================

  // 计算步骤的哈希值
  const calculateStepsHash = useCallback((steps: any[]): string => {
    return JSON.stringify(steps, Object.keys(steps).sort())
  }, [])

  // ==================== YAML 转换 ====================

  // YAML steps → 节点树
  const stepsToNodes = useCallback((steps: any[], parentId?: string, branchType?: 'then' | 'else' | 'parallel' | 'do', startIndex: number = 0): GraphNode[] => {
    const result: GraphNode[] = []
    
    steps.forEach((step, index) => {
      const nodeId = parentId 
        ? `${parentId}-${branchType}-${startIndex + index}`
        : `step-${startIndex + index}`
      
      const node: GraphNode = {
        id: nodeId,
        type: 'editorNode',
        position: { x: 0, y: 0 },
        data: {
          name: step.name || `Step ${startIndex + index + 1}`,
          action: step.action || '',
          parentId,
          branchType,
          branchIndex: index,
          description: step.description,
          if: step.if,
          loop: step.loop,
          run: step.run,
          message: step.message,
          level: step.level,
          url: step.url,
          method: step.method,
          body: step.body,
          script: step.script,
          shell: step.shell,
          duration: step.duration,
          items: step.items,  // Foreach items
          item_var: step.item_var,  // Foreach item variable
          items_text: step.items ? JSON.stringify(step.items) : '',  // 用于 textarea 显示
          // 保存子节点数据
          childSteps: step.then || step.else || step.steps,
          doSteps: step.do,
          // 计算初始高度
          _calculatedHeight: calculateNodeHeight({
            name: step.name || `Step ${startIndex + index + 1}`,
            action: step.action || '',
            description: step.description,
            if: step.if,
            message: step.message,
            level: step.level,
            url: step.url,
            method: step.method,
            script: step.script,
            shell: step.shell,
            duration: step.duration,
            items_text: step.items ? JSON.stringify(step.items) : '',  // Foreach items text
            item_var: step.item_var,  // Foreach item variable
            childSteps: step.then || step.else || step.steps,
            doSteps: step.do,
          }),
        },
      }
      
      result.push(node)
      
      // 递归处理子节点
      if (step.action === 'condition') {
        if (step.then?.length) {
          result.push(...stepsToNodes(step.then, nodeId, 'then', 0))
        }
        if (step.else?.length) {
          result.push(...stepsToNodes(step.else, nodeId, 'else', 0))
        }
      } else if (step.action === 'parallel' && step.steps?.length) {
        result.push(...stepsToNodes(step.steps, nodeId, 'parallel', 0))
      } else if ((step.action === 'foreach' || step.action === 'loop') && step.do?.length) {
        result.push(...stepsToNodes(step.do, nodeId, 'do', 0))
      }
    })
    
    return result
  }, [])

  // 节点树 → YAML steps（已废弃，使用 dagToYaml 替代）
  // const nodesToSteps = useCallback((allNodes: GraphNode[]): any[] => {
  //   ... 已注释 ...
  // }, [])

  // 检查当前节点状态是否与初始状态相同（比较节点顺序 + data 内容）
  const checkIfHasChanges = useCallback((currentNodes: GraphNode[]) => {
    if (!originalStepsHash) {
      return false
    }
    
    // 比较关键数据：节点顺序（通过排序后的 ID 列表）和业务数据
    const currentSnapshot = JSON.stringify({
      // 节点顺序：按 parentId + branchType + position 排序后的 ID 列表（与初始化一致）
      order: currentNodes
        .filter(n => !n.id.includes('-group-'))
        .sort((a, b) => {
          if (a.data.parentId !== b.data.parentId) return (a.data.parentId || '').localeCompare(b.data.parentId || '')
          if (a.data.branchType !== b.data.branchType) return (a.data.branchType || '').localeCompare(b.data.branchType || '')
          if (!a.data.parentId && !b.data.parentId) return a.position.x - b.position.x
          return a.position.y - b.position.y
        })
        .map(n => n.id),
      // 业务数据：按 ID 排序
      data: currentNodes
        .filter(n => !n.id.includes('-group-'))
        .map(n => ({
          id: n.id,
          data: {
            name: n.data.name,
            action: n.data.action,
            description: n.data.description,
            if: n.data.if,
            loop: n.data.loop,
            run: n.data.run,
            message: n.data.message,
            level: n.data.level,
            url: n.data.url,
            method: n.data.method,
            body: n.data.body,
            script: n.data.script,
            shell: n.data.shell,
            duration: n.data.duration,
            items: n.data.items,
            item_var: n.data.item_var,
          },
        }))
        .sort((a, b) => a.id.localeCompare(b.id))
    })
    
    const hasChanges = currentSnapshot !== originalStepsHash
    if (hasChanges) {
      // 详细比较找出差异
      try {
        const orig = JSON.parse(originalStepsHash)
        const curr = JSON.parse(currentSnapshot)
        
        
        // 比较顺序
        if (JSON.stringify(orig.order) !== JSON.stringify(curr.order)) {
          // 顺序差异当前无需处理（保留比较占位）
        }

        // 比较数据
        if (JSON.stringify(orig.data) !== JSON.stringify(curr.data)) {
          // 找出具体哪个字段变化
          for (let i = 0; i < Math.max(orig.data.length, curr.data.length); i++) {
            const o = orig.data[i]
            const c = curr.data[i]
            if (!o || !c || o.id !== c.id) {
              continue
            }
            Object.keys(o.data).forEach(key => {
              if (JSON.stringify(o.data[key]) !== JSON.stringify(c.data[key])) {
                // 字段级差异定位暂未使用（保留比较占位）
              }
            })
          }
        }
      } catch (e) {
        console.error('[checkIfHasChanges] Error parsing snapshots:', e)
      }
    }
    
    return hasChanges
  }, [originalStepsHash])

  // ==================== 布局系统 ====================

  // 获取节点高度（根据实际数据智能计算）
  const getNodeHeight = useCallback((node: GraphNode): number => {
    // 优先使用 data 中缓存的计算高度（如果有）
    if (node.data._calculatedHeight) {
      return node.data._calculatedHeight
    }
    // 否则实时计算
    return calculateNodeHeight(node.data)
  }, [])

  const calculateLayout = useCallback((inputNodes: GraphNode[]): { nodes: GraphNode[], edges: Edge[] } => {
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
        } as any,
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
        } as any,
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
  }, [])

  // ==================== 节点操作 ====================

  // 添加主流程节点 - 默认添加到末尾
  const addRootNode = useCallback(() => {
    const rootNodes = nodes.filter(n => !n.data.parentId)
    const nextIndex = rootNodes.length
    
    // 计算新节点的位置（在最后一个主流程节点后面）
    let newX = H_SPACING
    let newY = V_SPACING
    
    if (rootNodes.length > 0) {
      const lastNode = rootNodes[rootNodes.length - 1]
      newX = lastNode.position.x + NODE_WIDTH + H_SPACING
      newY = lastNode.position.y  // 同一水平线
    }
    
    const nodeName = `step-${nextIndex + 1}`
    const defaultAction = 'log'  // 默认为 log action
    const defaultHeight = calculateNodeHeight({ name: nodeName, action: defaultAction })
    
    const newNode: GraphNode = {
      id: generateId(),
      type: 'editorNode',
      position: { x: newX, y: newY },
      data: {
        name: nodeName,
        action: defaultAction,
        branchIndex: nextIndex,
        _calculatedHeight: defaultHeight,
      },
    }
    
    setNodes(prev => {
      const newNodes = [...prev, newNode]
      const { nodes: laidOutNodes, edges: newEdges } = calculateLayout(newNodes)
      setEdges(newEdges)
      return laidOutNodes
    })
    setHasUnsavedChanges(true)
  }, [nodes, calculateLayout])

  // 添加子节点
  const addChildNode = useCallback((parentId: string, branchType: 'then' | 'else' | 'parallel' | 'do') => {
    setNodes(prev => {
      const parent = prev.find(n => n.id === parentId)
      if (!parent) return prev
      
      // 获取该分支现有的子节点
      const existingChildren = prev.filter(
        n => n.data.parentId === parentId && n.data.branchType === branchType
      )
      
      const nodeNamePrefix = branchType === 'then' ? 'then' : branchType === 'else' ? 'else' : branchType === 'parallel' ? 'task' : 'step'
      const nodeName = `${nodeNamePrefix}-${existingChildren.length + 1}`
      const defaultAction = 'log'  // 默认为 log action
      
      const newNode: GraphNode = {
        id: generateId(),
        type: 'editorNode',
        position: { x: 0, y: 0 }, // 位置会在布局时计算
        data: {
          name: nodeName,
          action: defaultAction,
          parentId,
          branchType,
          branchIndex: existingChildren.length,
          _calculatedHeight: calculateNodeHeight({ name: nodeName, action: defaultAction }),
        },
      }
      
      const newNodes = [...prev, newNode]
      const { nodes: laidOutNodes, edges: newEdges } = calculateLayout(newNodes)
      setEdges(newEdges)
      return laidOutNodes
    })
    setHasUnsavedChanges(true)
  }, [calculateLayout])

  // 删除节点（递归删除子节点和容器）
  const deleteNode = useCallback((nodeId: string) => {
    setNodes(prev => {
      // 找到所有后代节点
      const findDescendants = (id: string): string[] => {
        const children = prev.filter(n => n.data.parentId === id)
        let ids = children.map(c => c.id)
        children.forEach(child => {
          ids = [...ids, ...findDescendants(child.id)]
        })
        return ids
      }
      
      const descendantIds = findDescendants(nodeId)
      const idsToDelete = [nodeId, ...descendantIds]
      
      // 如果是 Parallel/Foreach 节点，还要删除对应的容器节点
      const nodeToDelete = prev.find(n => n.id === nodeId)
      if (nodeToDelete && (nodeToDelete.data.action === 'parallel' || nodeToDelete.data.action === 'foreach' || nodeToDelete.data.action === 'loop')) {
        const containerId = nodeToDelete.data.action === 'parallel' 
          ? `parallel-group-${nodeId}`
          : `foreach-group-${nodeId}`
        idsToDelete.push(containerId)
      }
      
      const newNodes = prev.filter(n => !idsToDelete.includes(n.id))
      const { nodes: laidOutNodes, edges: newEdges } = calculateLayout(newNodes)
      setEdges(newEdges)
      return laidOutNodes
    })
    setHasUnsavedChanges(true)
  }, [calculateLayout])

  // 更新节点数据
  const updateNodeData = useCallback((nodeId: string, newData: Partial<GraphNodeData>) => {
    setNodes(prev => {
      const node = prev.find(n => n.id === nodeId)
      if (!node) return prev
      
      // 计算新高度
      const mergedData = { ...node.data, ...newData }
      const newHeight = calculateNodeHeight(mergedData)
      
      // 更新节点数据，包含计算的高度
      const updatedNodes = prev.map(n => {
        if (n.id === nodeId) {
          return { 
            ...n, 
            data: { 
              ...n.data, 
              ...newData,
              _calculatedHeight: newHeight,
            } 
          }
        }
        return n
      })
      
      // 重新布局以应用新高度
      const { nodes: laidOutNodes, edges: newEdges } = calculateLayout(updatedNodes)
      setEdges(newEdges)
      
      // 智能检测：只有真正有变化时才设置 hasUnsavedChanges
      const hasChanges = checkIfHasChanges(laidOutNodes)
      setHasUnsavedChanges(hasChanges)
      
      return laidOutNodes
    })
  }, [calculateLayout, checkIfHasChanges])

  // 处理节点变化（拖动、选择等）
  const onNodesChange = useCallback((changes: any[]) => {
    setNodes((prev) => {
      // 过滤掉容器节点的变化（容器节点不可拖动）
      const validChanges = changes.filter(change => {
        return !change.id.startsWith('parallel-group-') && !change.id.startsWith('foreach-group-')
      })
      
      if (validChanges.length === 0) return prev
      
      // 创建全新的节点数组，确保 React 能检测到变化
      return prev.map((node) => {
        const change = validChanges.find(c => c.id === node.id)
        if (change && change.type === 'position' && change.position) {
          return {
            ...node,
            position: {
              x: change.position.x,
              y: change.position.y,
            },
            positionAbsolute: {
              x: change.position.x,
              y: change.position.y,
            },
          }
        }
        return node
      })
    })
    // 注意：不在这里设置 hasUnsavedChanges，因为 React Flow 初始化时也会触发 onNodesChange
    // 只有在用户明确操作（拖动结束、修改数据）时才设置
  }, [])

  // 处理节点拖动结束 - 根据坐标重新计算 branchIndex
  const onNodeDragStop = useCallback((_event: any, node: any) => {
    
    // 先恢复所有节点层级
    setNodes(prev => prev.map(n => ({ ...n, style: { ...n.style, zIndex: 1 } })))
    
    // 然后根据拖动重新排序
    setNodes(prev => {
      const draggedNode = prev.find(n => n.id === node.id)
      if (!draggedNode) {
        return prev
      }
      
      const newNodes = [...prev]
      
      if (!draggedNode.data.parentId) {
        // 主流程节点：按 x 坐标排序（水平布局）
        const rootNodes = newNodes.filter(n => !n.data.parentId)
        
        // 保存拖动前的节点 ID 顺序（按 position.x 排序）
        const beforeOrder = [...rootNodes].sort((a, b) => a.position.x - b.position.x).map(n => n.id)
        
        // 根据拖动后的 x 坐标重新排序
        rootNodes.sort((a, b) => a.position.x - b.position.x)
        const afterOrder = rootNodes.map(n => n.id)
        
        // 检查顺序是否真的改变
        const orderChanged = JSON.stringify(beforeOrder) !== JSON.stringify(afterOrder)
        
        // 总是更新 branchIndex（用于 nodesToSteps 排序）
        rootNodes.forEach((node, index) => {
          node.data.branchIndex = index
        })
        
        // 重新布局，让节点按新顺序整齐排列
        const { nodes: laidOutNodes } = calculateLayout(newNodes)
        
        // 智能检测：只有顺序真正改变时才设置 hasUnsavedChanges
        setTimeout(() => {
          const hasChanges = orderChanged ? true : checkIfHasChanges(laidOutNodes)
          setHasUnsavedChanges(hasChanges)
        }, 0)
        
        return laidOutNodes
      } else {
        // 子节点：按 y 坐标排序（垂直布局）
        const { parentId, branchType } = draggedNode.data
        const siblings = newNodes.filter(
          n => n.data.parentId === parentId && n.data.branchType === branchType
        )
        
        // 保存拖动前的节点 ID 顺序（按 position.y 排序）
        const beforeOrder = [...siblings].sort((a, b) => a.position.y - b.position.y).map(n => n.id)
        
        // 根据拖动后的 y 坐标重新排序
        siblings.sort((a, b) => a.position.y - b.position.y)
        const afterOrder = siblings.map(n => n.id)
        
        // 检查顺序是否真的改变
        const orderChanged = JSON.stringify(beforeOrder) !== JSON.stringify(afterOrder)
        
        // 总是更新 branchIndex（用于 nodesToSteps 排序）
        siblings.forEach((node, index) => {
          node.data.branchIndex = index
        })
        
        // 重新布局，让子节点按新顺序整齐排列
        const { nodes: laidOutNodes } = calculateLayout(newNodes)
        
        // 智能检测：只有顺序真正改变时才设置 hasUnsavedChanges
        setTimeout(() => {
          const hasChanges = orderChanged ? true : checkIfHasChanges(laidOutNodes)
          setHasUnsavedChanges(hasChanges)
        }, 0)
        
        return laidOutNodes
      }
    })
    
    // 延迟更新边（等待节点更新完成）
    setTimeout(() => {
      setNodes(currentNodes => {
        const { edges: newEdges } = calculateLayout(currentNodes)
        setEdges(newEdges)
        return currentNodes
      })
    }, 50)
  }, [calculateLayout, checkIfHasChanges])

  // 处理节点拖动开始 - 提升节点层级
  const onNodeDragStart = useCallback((_event: any, node: any) => {
    // 提升拖动节点的层级，避免被遮挡
    setNodes(prev => prev.map(n => {
      if (n.id === node.id) {
        return {
          ...n,
          style: { ...n.style, zIndex: 100 },
        }
      }
      return n
    }))
  }, [])

  // 处理连接 - 禁用手动连线，连线由布局自动生成
  const onConnect = useCallback(() => {
    // 不执行任何操作，连线由 calculateLayout 中的 buildEdges 自动生成
  }, [])

  // ==================== 保存/加载 ====================

  // 验证节点数据
  const validateNodes = useCallback((): { valid: boolean; errors: string[] } => {
    const errors: string[] = []
    
    nodes.forEach(node => {
      // 跳过容器节点
      if (node.id.includes('-group-')) {
        return
      }
      
      if (!node.data.name || node.data.name.trim() === '') {
        errors.push(`节点 ${node.id} 缺少名称`)
      }
      if (!node.data.action || node.data.action.trim() === '') {
        errors.push(`节点 "${node.data.name || node.id}" 缺少 action 类型`)
      }
      
      // 验证必要字段
      if (node.data.action === 'http' && !node.data.url) {
        errors.push(`节点 "${node.data.name}" (HTTP) 缺少 URL`)
      }
      if (node.data.action === 'shell' && !node.data.shell) {
        errors.push(`节点 "${node.data.name}" (Shell) 缺少命令`)
      }
      if (node.data.action === 'log' && !node.data.message) {
        errors.push(`节点 "${node.data.name}" (Log) 缺少消息内容`)
      }
      if (node.data.action === 'sleep' && !node.data.duration) {
        errors.push(`节点 "${node.data.name}" (Sleep) 缺少时长`)
      }
    })
    
    return { valid: errors.length === 0, errors }
  }, [nodes])

  // 保存
  const handleSave = useCallback(() => {
    // 先验证
    const validation = validateNodes()
    if (!validation.valid) {
      alert('验证失败，请修复以下错误后再保存：\n\n' + validation.errors.join('\n'))
      return
    }
    
    // 使用新的 DAG 转换器
    const steps = dagToYaml(nodes)
    const yamlObj = { steps }
    onSave?.(yamlObj)
    
    // 保存后更新状态
    setHasUnsavedChanges(false)
    setOriginalStepsHash(calculateStepsHash(steps))
  }, [nodes, onSave, calculateStepsHash, validateNodes])

  // 运行
  const handleRun = useCallback(() => {
    // 先验证
    const validation = validateNodes()
    if (!validation.valid) {
      alert('验证失败，请修复以下错误后再运行：\n\n' + validation.errors.join('\n'))
      return
    }
    
    // 使用新的 DAG 转换器
    const steps = dagToYaml(nodes)
    const yamlObj = { steps }
    onRun?.(yamlObj)
  }, [nodes, onRun, validateNodes])

  // ==================== 初始化 ====================

  useEffect(() => {
    if (initialSteps && initialSteps.length > 0) {
      // 使用新的 DAG 转换器（自动推断依赖）
      const dagNodes = yamlToDAG({ steps: initialSteps } as any)
      const { nodes: laidOutNodes, edges: newEdges } = calculateLayout(dagNodes)
      setNodes(laidOutNodes)
      setEdges(newEdges)
      
      // 保存初始状态快照：只比较节点顺序（通过排序后的 ID 列表）和业务数据
      const snapshot = JSON.stringify({
        // 节点顺序：按 parentId + branchType + position 排序后的 ID 列表
        order: laidOutNodes
          .filter(n => !n.id.includes('-group-'))
          .sort((a, b) => {
            if (a.data.parentId !== b.data.parentId) return (a.data.parentId || '').localeCompare(b.data.parentId || '')
            if (a.data.branchType !== b.data.branchType) return (a.data.branchType || '').localeCompare(b.data.branchType || '')
            if (!a.data.parentId && !b.data.parentId) return a.position.x - b.position.x
            return a.position.y - b.position.y
          })
          .map(n => n.id),
        // 业务数据：按 ID 排序
        data: laidOutNodes
          .filter(n => !n.id.includes('-group-'))
          .map(n => ({
            id: n.id,
            data: {
              name: n.data.name,
              action: n.data.action,
              description: n.data.description,
              if: n.data.if,
              loop: n.data.loop,
              run: n.data.run,
              message: n.data.message,
              level: n.data.level,
              url: n.data.url,
              method: n.data.method,
              body: n.data.body,
              script: n.data.script,
              shell: n.data.shell,
              duration: n.data.duration,
              items: n.data.items,
              item_var: n.data.item_var,
            },
          }))
          .sort((a, b) => a.id.localeCompare(b.id))
      })
      setOriginalStepsHash(snapshot)
      
      setHasUnsavedChanges(false)  // 明确设置为未修改状态
    } else {
      // 无初始步骤：保持空画布
    }
  }, [initialSteps, yamlToDAG, calculateLayout])

  // 节点类型映射
  const nodeTypes = useMemo(() => ({
    editorNode: (props: any) => (
      <EditorNode
        id={props.id}
        data={props.data}
        selected={props.selected}
        onAddChild={addChildNode}
        onDelete={deleteNode}
        onDataChange={updateNodeData}
      />
    ),
    parallelGroup: (props: any) => (
      <ParallelGroupNode
        id={props.id}
        data={props.data}
      />
    ),
    foreachGroup: (props: any) => (
      <ForeachGroupNode
        id={props.id}
        data={props.data}
      />
    ),
  }), [addChildNode, deleteNode, updateNodeData, calculateLayout])

  // ==================== 渲染 ====================

  return (
    <div className="w-full h-full bg-gray-50 dark:bg-gray-900 relative">
      {/* 顶部工具栏 */}
      <div className="absolute top-4 left-4 right-4 z-10 flex items-center justify-between">
        <div className="flex items-center gap-2">
          <button
            onClick={addRootNode}
            className="flex items-center gap-2 px-4 py-2 bg-blue-500 hover:bg-blue-600 text-white rounded-lg shadow-lg transition-colors"
          >
            <Plus className="w-4 h-4" />
            Add Step
          </button>
          
          {/* 未保存提示 */}
          {hasUnsavedChanges && (
            <div className="flex items-center gap-2 px-3 py-1.5 bg-yellow-100 dark:bg-yellow-900/30 text-yellow-700 dark:text-yellow-300 rounded-lg text-sm">
              <AlertCircle className="w-4 h-4" />
              <span>Unsaved changes</span>
            </div>
          )}
          {!hasUnsavedChanges && originalStepsHash !== '' && (
            <div className="flex items-center gap-2 px-3 py-1.5 bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-300 rounded-lg text-sm">
              <CheckCircle className="w-4 h-4" />
              <span>Saved</span>
            </div>
          )}
        </div>
        
        <div className="flex items-center gap-2">
          <button
            onClick={handleRun}
            className="flex items-center gap-2 px-4 py-2 bg-green-500 hover:bg-green-600 text-white rounded-lg shadow-lg transition-colors"
          >
            <Play className="w-4 h-4" />
            Run
          </button>
          <button
            onClick={handleSave}
            disabled={!hasUnsavedChanges}
            className={`flex items-center gap-2 px-4 py-2 rounded-lg shadow-lg transition-colors ${
              hasUnsavedChanges
                ? 'bg-blue-500 hover:bg-blue-600 text-white'
                : 'bg-gray-300 dark:bg-gray-700 text-gray-500 dark:text-gray-400 cursor-not-allowed'
            }`}
          >
            <Save className="w-4 h-4" />
            Save
          </button>
        </div>
      </div>

      {/* ReactFlow 画布 */}
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        onNodesChange={onNodesChange}
        onConnect={onConnect}
        onNodeDragStart={onNodeDragStart}
        onNodeDragStop={onNodeDragStop}
        fitView
        snapToGrid
        snapGrid={[15, 15]}
        nodesDraggable={true}
        elementsSelectable={true}
        nodesConnectable={false}  // 禁用连线功能，连线由布局自动生成
        defaultEdgeOptions={{
          type: 'bezier',
          style: { stroke: '#9ca3af', strokeWidth: 2 },
          markerEnd: { type: MarkerType.ArrowClosed, color: '#9ca3af' },
        }}
        className="bg-transparent"
      >
        <Background variant={BackgroundVariant.Dots} gap={20} size={1} />
        <Controls />
      </ReactFlow>
    </div>
  )
}
