import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { GripVertical, Map, FileText, ChevronsDownUp, ChevronsUpDown } from 'lucide-react'

export interface FloatingToolbarProps {
  showCollapseToggle: boolean
  isAllCollapsed: boolean
  onToggleCollapseAll: () => void
  showMiniMap: boolean
  onToggleMiniMap: () => void
  showLogs: boolean
  onToggleLogs: () => void
}

// 右下角浮动工具栏（可拖动）：全部折叠/展开、MiniMap 开关、日志开关
export function FloatingToolbar({
  showCollapseToggle,
  isAllCollapsed,
  onToggleCollapseAll,
  showMiniMap,
  onToggleMiniMap,
  showLogs,
  onToggleLogs,
}: FloatingToolbarProps) {
  const { t } = useTranslation()
  // 可拖动按钮位置
  const [buttonPosition, setButtonPosition] = useState({ x: 0, y: 0 })
  const [isDraggingButton, setIsDraggingButton] = useState(false)
  const buttonDragRef = useRef<{ startX: number; startY: number; startPos: { x: number; y: number } } | null>(null)

  // 按钮拖动处理
  const handleButtonDragStart = useCallback((e: React.MouseEvent) => {
    setIsDraggingButton(true)
    buttonDragRef.current = {
      startX: e.clientX,
      startY: e.clientY,
      startPos: { x: buttonPosition.x, y: buttonPosition.y }
    }
  }, [buttonPosition])

  const handleButtonDragMove = useCallback((e: MouseEvent) => {
    if (!isDraggingButton || !buttonDragRef.current) return
    // CSS用right/bottom定位，方向和鼠标移动相反
    const deltaX = e.clientX - buttonDragRef.current.startX
    const deltaY = e.clientY - buttonDragRef.current.startY
    setButtonPosition({
      x: buttonDragRef.current.startPos.x - deltaX,
      y: buttonDragRef.current.startPos.y - deltaY
    })
  }, [isDraggingButton])

  const handleButtonDragEnd = useCallback(() => {
    setIsDraggingButton(false)
    buttonDragRef.current = null
  }, [])

  // 添加拖动事件监听
  useEffect(() => {
    if (isDraggingButton) {
      window.addEventListener('mousemove', handleButtonDragMove)
      window.addEventListener('mouseup', handleButtonDragEnd)
      return () => {
        window.removeEventListener('mousemove', handleButtonDragMove)
        window.removeEventListener('mouseup', handleButtonDragEnd)
      }
    }
  }, [isDraggingButton, handleButtonDragMove, handleButtonDragEnd])

  return (
    <div
      className="fixed z-40 flex flex-row gap-2 bg-white/90 dark:bg-gray-800/90 rounded-xl px-2 py-1.5 shadow-sm border border-gray-200 dark:border-gray-600 cursor-move"
      style={{
        bottom: `${4 + buttonPosition.y}px`,
        right: `${4 + buttonPosition.x}px`,
      }}
      onMouseDown={handleButtonDragStart}
    >
      {/* Drag Handle Indicator */}
      <div className="flex items-center justify-center text-gray-400 dark:text-gray-500 cursor-grab active:cursor-grabbing">
        <GripVertical className="w-4 h-4" />
      </div>

      {/* Collapse/Expand All Toggle */}
      {showCollapseToggle && (
        <button
          onClick={(e) => {
            e.stopPropagation()
            onToggleCollapseAll()
          }}
          className={`p-2 rounded-lg transition-all ${
            isAllCollapsed
              ? 'bg-orange-500 text-white'
              : 'text-gray-500 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-700'
          }`}
          title={isAllCollapsed ? t('execution.expandAll') : t('execution.collapseAll')}
        >
          {isAllCollapsed ? <ChevronsDownUp className="w-4 h-4" /> : <ChevronsUpDown className="w-4 h-4" />}
        </button>
      )}

      {/* MiniMap Toggle */}
      <button
        onClick={(e) => {
          e.stopPropagation()
          onToggleMiniMap()
        }}
        className={`p-2 rounded-lg transition-all ${
          showMiniMap
            ? 'bg-blue-500 text-white'
            : 'text-gray-500 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-700'
        }`}
        title={showMiniMap ? t('execution.hideMiniMap') : t('execution.showMiniMap')}
      >
        <Map className="w-4 h-4" />
      </button>

      {/* Logs Toggle */}
      <button
        onClick={(e) => {
          e.stopPropagation()
          onToggleLogs()
        }}
        className={`p-2 rounded-lg transition-all ${
          showLogs
            ? 'bg-blue-500 text-white'
            : 'text-gray-500 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-700'
        }`}
        title={showLogs ? t('execution.hideLogs') : t('execution.showLogs')}
      >
        <FileText className="w-4 h-4" />
      </button>
    </div>
  )
}
