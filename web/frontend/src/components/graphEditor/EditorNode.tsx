import { useTranslation } from 'react-i18next'
import { Position, Handle } from '@xyflow/react'
import { Plus, Trash2 } from 'lucide-react'
import type { GraphNodeData } from '@/types/graph'
import { NODE_WIDTH, calculateNodeHeight, getActionConfig } from './constants'

export interface EditorNodeProps {
  id: string
  data: GraphNodeData
  selected?: boolean
  onAddChild: (parentId: string, branchType: 'then' | 'else' | 'parallel' | 'do') => void
  onDelete: (nodeId: string) => void
  onDataChange: (nodeId: string, newData: Partial<GraphNodeData>) => void
}

// 可编辑节点：名称/action/字段编辑，容器节点的 Add 子节点按钮
export function EditorNode({
  id,
  data,
  selected,
  onAddChild,
  onDelete,
  onDataChange
}: EditorNodeProps) {
  const { t } = useTranslation()
  const config = getActionConfig(data.action)
  const Icon = config.icon

  const handleFieldChange = (field: string, value: unknown) => {
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
    const newData = { action }
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
