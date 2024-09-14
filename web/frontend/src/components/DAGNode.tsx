// DAG 节点组件（完全参照原 EditorNode 样式）
import { memo, useCallback } from 'react'
import { Handle, Position, NodeProps } from '@xyflow/react'
import { 
  MessageSquare, Terminal, Globe, Code, GitBranch, 
  Zap, RotateCcw, FileText, Plus, Trash2, Moon
} from 'lucide-react'
import { DAGNodeData } from '../types/dag'

// ==================== 常量 ====================

const NODE_WIDTH = 220
const BASE_NODE_HEIGHT = 150

// 字段高度配置
const FIELD_HEIGHTS = {
  input: 32,
  textarea: 80,
  select: 32,
  button: 36,
  buttonRow: 44,
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
    {type: 'textarea', field: 'body'},
  ],
  'shell': [{type: 'textarea', field: 'shell'}],
  'script': [{type: 'textarea', field: 'script'}],
  'sleep': [{type: 'input', field: 'duration'}],
  'condition': [
    {type: 'input', field: 'if'},
    {type: 'buttonRow', field: '_buttons'},
  ],
  'parallel': [
    {type: 'button', field: '_addTask'},
  ],
  'foreach': [
    {type: 'input', field: 'item_var'}, 
    {type: 'textarea', field: 'items_text'},
    {type: 'button', field: '_addStep'},
  ],
  'loop': [
    {type: 'input', field: 'item_var'}, 
    {type: 'textarea', field: 'items_text'},
  ],
  'set': [
    {type: 'input', field: 'variable'}, 
    {type: 'input', field: 'value'}
  ],
  'template': [{type: 'input', field: 'template'}],
}

// 通用字段
const COMMON_FIELDS = [
  {type: 'input' as const, field: 'description'},
]

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
  set: { icon: FileText, color: 'text-teal-500', bgColor: 'bg-teal-50 dark:bg-teal-900/20', borderColor: 'border-teal-500', label: 'Set' },
  template: { icon: FileText, color: 'text-orange-500', bgColor: 'bg-orange-50 dark:bg-orange-900/20', borderColor: 'border-orange-500', label: 'Template' },
  '': { icon: FileText, color: 'text-gray-500', bgColor: 'bg-gray-50 dark:bg-gray-900/20', borderColor: 'border-gray-400', label: 'Action' },
}

function getActionConfig(action: string) {
  return actionConfigs[action] || actionConfigs['']
}

// 根据节点数据智能计算高度（与原编辑器一致）
const calculateNodeHeight = (data: DAGNodeData): number => {
  let height = BASE_NODE_HEIGHT
  
  const action = data.action || ''
  const fields = ACTION_FIELDS[action]
  
  if (fields) {
    fields.forEach(({ type }) => {
      height += FIELD_HEIGHTS[type]
    })
  }
  
  COMMON_FIELDS.forEach(({ type }) => {
    height += FIELD_HEIGHTS[type]
  })
  
  return height
}

export { calculateNodeHeight, getActionConfig }

// ==================== DAG 节点组件 ====================

interface DAGNodeProps extends NodeProps {
  data: DAGNodeData
  setNodes?: any
  setEdges?: any
  onAddNext?: (sourceNodeId: string) => void
  onAddChild?: (parentId: string, branchType: 'then' | 'else' | 'parallel' | 'do') => void
}

const DAGNodeInner = (props: DAGNodeProps) => {
  const { id, data, selected, setNodes, setEdges, onAddNext, onAddChild } = props
  
  const handleFieldChange = useCallback((field: string, value: any) => {
    if (!setNodes) return
    
    setNodes((nds: any[]) =>
      nds.map((node) => {
        if (node.id === id) {
          const newData = { ...node.data, [field]: value }
          const newHeight = calculateNodeHeight(newData)
          return {
            ...node,
            height: newHeight,
            data: { ...newData, _calculatedHeight: newHeight },
          }
        }
        return node
      })
    )
    if (setEdges) setEdges((eds: any[]) => [...eds])
  }, [id, setNodes, setEdges])
  
  const handleActionChange = useCallback((action: string) => {
    if (!setNodes) return
    
    setNodes((nds: any[]) =>
      nds.map((node) => {
        if (node.id === id) {
          const newNodeData = { ...node.data, action }
          const newHeight = calculateNodeHeight(newNodeData)
          return {
            ...node,
            height: newHeight,
            data: { ...newNodeData, _calculatedHeight: newHeight },
          }
        }
        return node
      })
    )
    if (setEdges) setEdges((eds: any[]) => [...eds])
  }, [id, setNodes, setEdges])
  
  const config = getActionConfig(data.action)
  const Icon = config.icon
  
  const handleAddNext = useCallback(() => {
    if (onAddNext) onAddNext(id)
  }, [id, onAddNext])
  
  const handleAddThen = useCallback(() => {
    if (onAddChild) onAddChild(id, 'then')
  }, [id, onAddChild])
  
  const handleAddElse = useCallback(() => {
    if (onAddChild) onAddChild(id, 'else')
  }, [id, onAddChild])
  
  const handleAddParallelTask = useCallback(() => {
    if (onAddChild) onAddChild(id, 'parallel')
  }, [id, onAddChild])
  
  const handleAddDoStep = useCallback(() => {
    if (onAddChild) onAddChild(id, 'do')
  }, [id, onAddChild])
  
  return (
    <div
      className={`group rounded-xl shadow-lg border-2 min-w-[${NODE_WIDTH}px] transition-all flex flex-col overflow-hidden ${
        config.bgColor
      } ${config.borderColor} ${
        selected ? 'ring-2 ring-blue-500 ring-offset-2' : ''
      }`}
      style={{ 
        width: NODE_WIDTH,
        height: data._calculatedHeight || calculateNodeHeight(data),
        zIndex: selected ? 100 : 1,
      }}
    >
      {/* 主流程连线 Handle（左右） */}
      <Handle type="target" position={Position.Left} className="!bg-gray-400 !w-3 !h-3" id="left" />
      <Handle type="source" position={Position.Right} className="!bg-gray-400 !w-3 !h-3" id="right" />
      
      {/* 父子节点连线 Handle（上/下） */}
      {(data.action === 'parallel' || data.action === 'foreach' || data.action === 'loop' || data.parentId) && (
        <>
          <Handle type="target" position={Position.Top} className="!bg-purple-400 !w-2 !h-2" id="top" />
          <Handle type="source" position={Position.Bottom} className="!bg-purple-400 !w-2 !h-2" id="bottom" />
        </>
      )}
      
      {/* Header */}
      <div className={`flex items-center gap-2 p-3 border-b ${config.borderColor}`}>
        <div className={`p-1.5 rounded ${config.color} ${config.bgColor}`}>
          <Icon className="w-4 h-4" />
        </div>
        <input
          type="text"
          value={data.name || ''}
          onChange={(e) => handleFieldChange('name', e.target.value)}
          className="flex-1 bg-transparent border-none outline-none text-sm font-semibold text-gray-800 dark:text-gray-100 nodrag"
          placeholder="Node name"
        />
        <button
          onClick={handleAddNext}
          className="p-1.5 hover:bg-white/50 dark:hover:bg-gray-700/50 rounded transition-colors nodrag"
          title="Add next node"
        >
          <Plus className="w-4 h-4 text-gray-600 dark:text-gray-300" />
        </button>
        <button
          onClick={() => {
            if (setNodes) {
              setNodes((nds: any[]) => nds.filter((n: any) => n.id !== id))
              if (setEdges) {
                setEdges((eds: any[]) => eds.filter((e: any) => e.source !== id && e.target !== id))
              }
            }
          }}
          className="p-1.5 hover:bg-red-100 dark:hover:bg-red-900/30 rounded transition-colors nodrag"
          title="Delete node"
        >
          <Trash2 className="w-4 h-4 text-red-500" />
        </button>
      </div>
      
      {/* Body */}
      <div className="p-3 space-y-3 flex-1">
        {/* Action 选择框 */}
        <select
          value={data.action || ''}
          onChange={(e) => handleActionChange(e.target.value)}
          className="w-full px-2 py-1.5 text-xs border border-gray-300 dark:border-gray-600 rounded bg-white dark:bg-gray-700 nodrag"
        >
          <option value="">Select action</option>
          <option value="log">💬 Log Message</option>
          <option value="http">🌐 HTTP Request</option>
          <option value="script">💻 JavaScript</option>
          <option value="shell">🖥 Shell Command</option>
          <option value="condition">🔀 Condition</option>
          <option value="foreach">🔄 Foreach</option>
          <option value="parallel">⚡ Parallel</option>
          <option value="sleep">💤 Sleep</option>
          <option value="set">📝 Set Variable</option>
          <option value="template">📄 Template</option>
          <option value="loop">🔁 Loop</option>
        </select>
        
        {/* Description */}
        <input
          type="text"
          value={data.description || ''}
          onChange={(e) => handleFieldChange('description', e.target.value)}
          placeholder="Description (optional)"
          className="w-full px-2 py-1.5 text-xs border border-gray-300 dark:border-gray-600 rounded bg-white dark:bg-gray-700 nodrag"
        />
        
        {/* Log fields */}
        {data.action === 'log' && (
          <>
            <textarea
              value={data.message || ''}
              onChange={(e) => handleFieldChange('message', e.target.value)}
              placeholder="Message"
              rows={3}
              className="w-full px-2 py-1.5 text-xs border border-gray-300 dark:border-gray-600 rounded bg-white dark:bg-gray-700 resize-none overflow-auto nodrag"
              style={{ height: '80px' }}
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
        
        {/* HTTP fields */}
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
            <textarea
              value={data.body || ''}
              onChange={(e) => handleFieldChange('body', e.target.value)}
              placeholder="Request body"
              rows={3}
              className="w-full px-2 py-1.5 text-xs border border-gray-300 dark:border-gray-600 rounded bg-white dark:bg-gray-700 resize-none overflow-auto nodrag"
              style={{ height: '80px' }}
            />
          </>
        )}
        
        {/* Shell field */}
        {data.action === 'shell' && (
          <textarea
            value={data.shell || ''}
            onChange={(e) => handleFieldChange('shell', e.target.value)}
            placeholder="Shell command"
            rows={3}
            className="w-full px-2 py-1.5 text-xs border border-gray-300 dark:border-gray-600 rounded bg-white dark:bg-gray-700 resize-none overflow-auto nodrag"
            style={{ height: '80px' }}
          />
        )}
        
        {/* Script field */}
        {data.action === 'script' && (
          <textarea
            value={data.script || ''}
            onChange={(e) => handleFieldChange('script', e.target.value)}
            placeholder="JavaScript code"
            rows={3}
            className="w-full px-2 py-1.5 text-xs border border-gray-300 dark:border-gray-600 rounded bg-white dark:bg-gray-700 resize-none overflow-auto nodrag"
            style={{ height: '80px' }}
          />
        )}
        
        {/* Condition fields */}
        {data.action === 'condition' && (
          <>
            <input
              type="text"
              value={data.if || ''}
              onChange={(e) => handleFieldChange('if', e.target.value)}
              placeholder="Condition (e.g., status == 'success')"
              className="w-full px-2 py-1.5 text-xs border border-gray-300 dark:border-gray-600 rounded bg-white dark:bg-gray-700 nodrag"
            />
            <div className="flex gap-2">
              <button
                onClick={handleAddThen}
                className="flex-1 px-2 py-1.5 text-xs bg-green-100 dark:bg-green-900/30 hover:bg-green-200 dark:hover:bg-green-900/50 text-green-700 dark:text-green-300 rounded transition-colors flex items-center justify-center gap-1 nodrag"
              >
                <Plus className="w-3 h-3" />
                Add Then
              </button>
              <button
                onClick={handleAddElse}
                className="flex-1 px-2 py-1.5 text-xs bg-red-100 dark:bg-red-900/30 hover:bg-red-200 dark:hover:bg-red-900/50 text-red-700 dark:text-red-300 rounded transition-colors flex items-center justify-center gap-1 nodrag"
              >
                <Plus className="w-3 h-3" />
                Add Else
              </button>
            </div>
          </>
        )}
        
        {/* Parallel fields */}
        {data.action === 'parallel' && (
          <button
            onClick={handleAddParallelTask}
            className="w-full px-2 py-1.5 text-xs bg-pink-100 dark:bg-pink-900/30 hover:bg-pink-200 dark:hover:bg-pink-900/50 text-pink-700 dark:text-pink-300 rounded transition-colors flex items-center justify-center gap-1 nodrag"
          >
            <Plus className="w-3 h-3" />
            Add Task
          </button>
        )}
        
        {/* Foreach fields */}
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
                handleFieldChange('items_text', text)
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
              onClick={handleAddDoStep}
              className="w-full px-2 py-1.5 text-xs bg-cyan-100 dark:bg-cyan-900/30 hover:bg-cyan-200 dark:hover:bg-cyan-900/50 text-cyan-700 dark:text-cyan-300 rounded transition-colors flex items-center justify-center gap-1 nodrag"
            >
              <Plus className="w-3 h-3" />
              Add Do Step
            </button>
          </>
        )}
        
        {/* Sleep field */}
        {data.action === 'sleep' && (
          <input
            type="text"
            value={data.duration || ''}
            onChange={(e) => handleFieldChange('duration', e.target.value)}
            placeholder="Duration (e.g., 5s, 1m)"
            className="w-full px-2 py-1.5 text-xs border border-gray-300 dark:border-gray-600 rounded bg-white dark:bg-gray-700 nodrag"
          />
        )}
        
        {/* Set fields */}
        {data.action === 'set' && (
          <>
            <input
              type="text"
              value={data.variable || ''}
              onChange={(e) => handleFieldChange('variable', e.target.value)}
              placeholder="Variable name"
              className="w-full px-2 py-1.5 text-xs border border-gray-300 dark:border-gray-600 rounded bg-white dark:bg-gray-700 nodrag"
            />
            <input
              type="text"
              value={data.value || ''}
              onChange={(e) => handleFieldChange('value', e.target.value)}
              placeholder="Value"
              className="w-full px-2 py-1.5 text-xs border border-gray-300 dark:border-gray-600 rounded bg-white dark:bg-gray-700 nodrag"
            />
          </>
        )}
        
        {/* Template field */}
        {data.action === 'template' && (
          <input
            type="text"
            value={data.template || ''}
            onChange={(e) => handleFieldChange('template', e.target.value)}
            placeholder="Template name"
            className="w-full px-2 py-1.5 text-xs border border-gray-300 dark:border-gray-600 rounded bg-white dark:bg-gray-700 nodrag"
          />
        )}
      </div>
    </div>
  )
}

export const DAGNode = memo(DAGNodeInner) as any
