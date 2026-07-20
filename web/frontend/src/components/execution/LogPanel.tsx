import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import {
  X, GripVertical, PanelBottomClose, PanelRightClose, Square, ArrowUpToLine, ArrowDownToLine,
  Timer, Search, Type, ChevronUp, ChevronDown, CaseSensitive, Regex,
  ChevronRight, Copy, Clock,
} from 'lucide-react'
import { AnsiText } from './AnsiText'

export type LogLayout = 'bottom' | 'right' | 'float'

export interface LogPanelProps {
  logs: string[]
  layout: LogLayout
  onLayoutChange: (layout: LogLayout) => void
  onClose: () => void
}

// 日志面板：三种布局（底部/右侧/悬浮），支持搜索高亮、级别过滤、字号调节、拖动与缩放
export function LogPanel({ logs, layout: logLayout, onLayoutChange: setLogLayout, onClose }: LogPanelProps) {
  const { t } = useTranslation()
  const [showTimestamp, setShowTimestamp] = useState(false)
  const [logFontSize, setLogFontSize] = useState(12) // 字体大小（像素值）

  // 搜索相关状态
  const [showSearch, setShowSearch] = useState(false)
  const [searchQuery, setSearchQuery] = useState('')
  const [searchRegex, setSearchRegex] = useState<RegExp | null>(null)
  const [searchError, setSearchError] = useState<string | null>(null)
  const [selectedLogLevels, setSelectedLogLevels] = useState<string[]>(['ERROR', 'WARN', 'INFO', 'DEBUG'])
  const [showLevelDropdown, setShowLevelDropdown] = useState(false) // 下拉列表展开状态
  const [showFontSize, setShowFontSize] = useState(false) // 字体滑块显隐状态
  const [copied, setCopied] = useState(false) // 复制成功提示状态

  // 搜索选项
  const [caseSensitive, setCaseSensitive] = useState(false) // 区分大小写
  const [useRegex, setUseRegex] = useState(true) // 正则模式（默认开启）
  const [currentMatchIndex, setCurrentMatchIndex] = useState(0) // 当前匹配索引
  const [matchPositions, setMatchPositions] = useState<number[]>([]) // 匹配的日志索引数组
  const logContentRef = useRef<HTMLDivElement>(null) // 日志内容区域引用
  const searchInputRef = useRef<HTMLInputElement>(null) // 搜索输入框引用

  const [bottomPanelHeight, setBottomPanelHeight] = useState(250) // 底部布局专用高度
  const [floatPanelHeight, setFloatPanelHeight] = useState(300) // 悬浮窗口专用高度
  const [logPanelWidth, setLogPanelWidth] = useState(480) // 默认隐藏时间戳，初始宽度480（容纳中文标题和所有图标）
  const [logPanelPosition, setLogPanelPosition] = useState({ x: 0, y: 70 })
  const logsEndRef = useRef<HTMLDivElement>(null)
  const logPanelRef = useRef<HTMLDivElement>(null)
  const isResizing = useRef(false)
  const resizeEdge = useRef<'top' | 'bottom' | 'left' | 'right' | 'topLeft' | 'topRight' | 'bottomLeft' | 'bottomRight'>('bottom')
  const startY = useRef(0)
  const startX = useRef(0)
  const startHeight = useRef(0) // 根据布局类型使用不同的高度
  const startWidth = useRef(0)
  const startPosX = useRef(0)
  const startPosY = useRef(0)
  const isDragging = useRef(false)
  const dragStartX = useRef(0)
  const dragStartY = useRef(0)
  const resizeContext = useRef<'bottomLayout' | 'floatLayout' | 'rightLayout'>('bottomLayout')

  // Initialize float panel position and height
  useEffect(() => {
    if (logLayout === 'float' && logPanelPosition.x === 0) {
      // 全局导航栏高度: 56px (h-14)
      // Execution header 高度: 约 78px（实际测量）
      const headerTotal = 134 // 实际测量值

      // MiniMap 位置：相对于 ReactFlow 容器 bottom: 65px（实际测量）
      // MiniMap 实际高度约 152px
      const minimapBottomRelative = 65
      const minimapHeight = 152
      const reactFlowHeight = window.innerHeight - headerTotal - 10
      const minimapTop = headerTotal + reactFlowHeight - minimapHeight - minimapBottomRelative

      // 初始高度：接近 MiniMap 顶部，留 10px 间距
      const availableHeight = minimapTop - headerTotal - 10
      const floatHeight = Math.max(200, Math.floor(availableHeight))

      setFloatPanelHeight(floatHeight)
      setLogPanelPosition({
        x: window.innerWidth - logPanelWidth - 10, // 贴近右侧，留 10px 边距
        y: headerTotal + 5 // 紧贴 Execution header 下方
      })
    }
  }, [logLayout, logPanelPosition.x, logPanelWidth])

  // Auto-adjust log panel width when toggling timestamp display
  useEffect(() => {
    if (logLayout === 'bottom') return // 底部模式不需要调整宽度

    const timestampWidth = 180 // 时间戳占用的宽度（约 "[2026-04-08T21:12:30+08:00]" 的宽度）
    const minWidth = 400 // 最小宽度需要容纳中文标题和所有工具栏图标
    const maxWidth = window.innerWidth - 100

    if (showTimestamp) {
      // 显示时间戳时增加宽度
      const newWidth = Math.min(maxWidth, logPanelWidth + timestampWidth)
      setLogPanelWidth(newWidth)
      // 悬浮模式下保持右侧边缘固定，面板向左扩展
      if (logLayout === 'float') {
        const newPosX = window.innerWidth - newWidth - 10
        setLogPanelPosition(prev => ({ ...prev, x: newPosX }))
      }
    } else {
      // 隐藏时间戳时减少宽度
      const newWidth = Math.max(minWidth, logPanelWidth - timestampWidth)
      setLogPanelWidth(newWidth)
      // 悬浮模式下保持右侧边缘固定，面板向右收缩
      if (logLayout === 'float') {
        const newPosX = window.innerWidth - newWidth - 10
        setLogPanelPosition(prev => ({ ...prev, x: newPosX }))
      }
    }
  }, [showTimestamp, logLayout])

  // Auto-scroll logs
  useEffect(() => {
    logsEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [logs])

  // Close level dropdown when clicking outside
  useEffect(() => {
    const handleClickOutside = (e: MouseEvent) => {
      if (showLevelDropdown) {
        const target = e.target as HTMLElement
        if (!target.closest('.level-dropdown-container')) {
          setShowLevelDropdown(false)
        }
      }
    }
    document.addEventListener('click', handleClickOutside)
    return () => document.removeEventListener('click', handleClickOutside)
  }, [showLevelDropdown])

  // Adjust float panel position when window resizes
  useEffect(() => {
    const handleResize = () => {
      if (logLayout === 'float') {
        const headerTotal = 134 // header height
        const panelHeight = floatPanelHeight

        // Ensure panel stays within visible area
        const maxX = window.innerWidth - logPanelWidth - 10
        const maxY = window.innerHeight - panelHeight - 10

        setLogPanelPosition(prev => ({
          x: Math.max(10, Math.min(maxX, prev.x)),
          y: Math.max(headerTotal + 5, Math.min(maxY, prev.y))
        }))
      }
    }

    window.addEventListener('resize', handleResize)
    return () => window.removeEventListener('resize', handleResize)
  }, [logLayout, logPanelWidth, floatPanelHeight])

  // Handle log panel resize
  useEffect(() => {
    const handleMouseMove = (e: MouseEvent) => {
      if (!isResizing.current) return

      const deltaX = e.clientX - startX.current
      const deltaY = e.clientY - startY.current

      let newWidth = startWidth.current
      let newHeight = startHeight.current
      let newPosX = startPosX.current
      let newPosY = startPosY.current

      // 底部布局：只调整高度，向上拖变大
      if (resizeContext.current === 'bottomLayout') {
        const minBottomHeight = 100
        const maxBottomHeight = window.innerHeight - 200 // 留给上方内容至少200px
        newHeight = Math.max(minBottomHeight, Math.min(maxBottomHeight, startHeight.current - deltaY))
        setBottomPanelHeight(newHeight)
        return
      }

      // 右侧布局：resize handle 在左边缘，向左拖变大（deltaX < 0），向右拖变小（deltaX > 0）
      if (resizeContext.current === 'rightLayout') {
        const minWidth = 200
        const maxWidth = window.innerWidth - 200 // 留给左侧内容至少200px
        newWidth = Math.max(minWidth, Math.max(maxWidth, startWidth.current - deltaX))
        setLogPanelWidth(newWidth)
        return
      }

      // 悬浮窗口：可以调整宽高和位置，基于窗口边界动态限制
      const minHeight = 100
      const minWidth = 200
      const maxWidth = window.innerWidth - 50
      const maxHeight = window.innerHeight - newPosY - 10

      switch (resizeEdge.current) {
        case 'bottom':
          newHeight = Math.max(minHeight, Math.min(maxHeight, startHeight.current + deltaY))
          break
        case 'right':
          newWidth = Math.max(minWidth, Math.min(maxWidth, startWidth.current + deltaX))
          break
        case 'left':
          newWidth = Math.max(minWidth, Math.min(maxWidth, startWidth.current - deltaX))
          newPosX = Math.max(0, startPosX.current + deltaX)
          break
        case 'top':
          newHeight = Math.max(minHeight, Math.min(maxHeight, startHeight.current - deltaY))
          newPosY = Math.max(56, startPosY.current + deltaY)
          break
        case 'topLeft':
          newWidth = Math.max(minWidth, Math.min(maxWidth, startWidth.current - deltaX))
          newHeight = Math.max(minHeight, Math.min(maxHeight, startHeight.current - deltaY))
          newPosX = Math.max(0, startPosX.current + deltaX)
          newPosY = Math.max(56, startPosY.current + deltaY)
          break
        case 'topRight':
          newWidth = Math.max(minWidth, Math.min(maxWidth, startWidth.current + deltaX))
          newHeight = Math.max(minHeight, Math.min(maxHeight, startHeight.current - deltaY))
          newPosY = Math.max(56, startPosY.current + deltaY)
          break
        case 'bottomLeft':
          newWidth = Math.max(minWidth, Math.min(maxWidth, startWidth.current - deltaX))
          newHeight = Math.max(minHeight, Math.min(maxHeight, startHeight.current + deltaY))
          newPosX = Math.max(0, startPosX.current + deltaX)
          break
        case 'bottomRight':
          newWidth = Math.max(minWidth, Math.min(maxWidth, startWidth.current + deltaX))
          newHeight = Math.max(minHeight, Math.min(maxHeight, startHeight.current + deltaY))
          break
      }

      setLogPanelWidth(newWidth)
      setFloatPanelHeight(newHeight)
      setLogPanelPosition({ x: newPosX, y: newPosY })
    }

    const handleMouseUp = () => {
      isResizing.current = false
      resizeEdge.current = 'bottom'
      resizeContext.current = 'bottomLayout'
      document.body.style.cursor = 'default'
      document.body.style.userSelect = 'auto'
    }

    document.addEventListener('mousemove', handleMouseMove)
    document.addEventListener('mouseup', handleMouseUp)

    return () => {
      document.removeEventListener('mousemove', handleMouseMove)
      document.removeEventListener('mouseup', handleMouseUp)
    }
  }, [])

  // Handle float panel dragging
  useEffect(() => {
    const handleMouseMove = (e: MouseEvent) => {
      if (!isDragging.current) return
      const deltaX = e.clientX - dragStartX.current
      const deltaY = e.clientY - dragStartY.current
      setLogPanelPosition(prev => ({
        x: Math.max(0, prev.x + deltaX),
        y: Math.max(0, prev.y + deltaY)
      }))
      dragStartX.current = e.clientX
      dragStartY.current = e.clientY
    }

    const handleMouseUp = () => {
      isDragging.current = false
      document.body.style.cursor = 'default'
      document.body.style.userSelect = 'auto'
    }

    document.addEventListener('mousemove', handleMouseMove)
    document.addEventListener('mouseup', handleMouseUp)

    return () => {
      document.removeEventListener('mousemove', handleMouseMove)
      document.removeEventListener('mouseup', handleMouseUp)
    }
  }, [])

  const startResize = (e: React.MouseEvent, edge: 'bottom' | 'right' | 'left' | 'top' | 'topLeft' | 'topRight' | 'bottomLeft' | 'bottomRight', layoutType: 'bottom' | 'right' | 'float') => {
    e.preventDefault()
    e.stopPropagation()
    isResizing.current = true
    resizeEdge.current = edge
    resizeContext.current = layoutType === 'bottom' ? 'bottomLayout' : layoutType === 'right' ? 'rightLayout' : 'floatLayout'

    startX.current = e.clientX
    startY.current = e.clientY
    startWidth.current = logPanelWidth
    // 根据布局类型使用对应的高度
    startHeight.current = layoutType === 'bottom' ? bottomPanelHeight : floatPanelHeight
    startPosX.current = logPanelPosition.x
    startPosY.current = logPanelPosition.y

    const cursorMap: Record<string, string> = {
      top: 'row-resize',
      bottom: 'row-resize',
      left: 'col-resize',
      right: 'col-resize',
      topLeft: 'nwse-resize',
      topRight: 'nesw-resize',
      bottomLeft: 'nesw-resize',
      bottomRight: 'nwse-resize',
    }
    document.body.style.cursor = cursorMap[edge]
    document.body.style.userSelect = 'none'
  }

  const startDrag = (e: React.MouseEvent) => {
    e.preventDefault()
    isDragging.current = true
    dragStartX.current = e.clientX
    dragStartY.current = e.clientY
    document.body.style.cursor = 'move'
    document.body.style.userSelect = 'none'
  }

  // 搜索栏显示时自动获得焦点
  useEffect(() => {
    if (showSearch && searchInputRef.current) {
      setTimeout(() => searchInputRef.current?.focus(), 100)
    }
  }, [showSearch])

  // Handle search with regex and case sensitivity support
  const handleSearch = useCallback((query: string) => {
    if (!query.trim()) {
      setSearchRegex(null)
      setSearchError(null)
      setMatchPositions([])
      setCurrentMatchIndex(0)
      return
    }
    try {
      // 构建正则表达式
      let pattern = query
      if (!useRegex) {
        // 普通模式，转义特殊字符
        pattern = query.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
      }
      const flags = caseSensitive ? 'g' : 'gi'
      const regex = new RegExp(pattern, flags)
      setSearchRegex(regex)
      setSearchError(null)
    } catch {
      setSearchError(t('execution.invalidRegex'))
      setSearchRegex(null)
      setMatchPositions([])
    }
  }, [t, useRegex, caseSensitive])

  // Filter logs by level only (search only highlights, doesn't filter)
  const filteredLogs = useMemo(() => {
    let result = logs

    // Filter by log level
    if (selectedLogLevels.length < 4) {
      result = result.filter(log => {
        const match = log.match(/^\[([^\]]+)\]\s+(ERROR|WARN|INFO|DEBUG):/)
        if (match && match[2]) {
          return selectedLogLevels.includes(match[2])
        }
        return true // Keep logs without level
      })
    }

    return result
  }, [logs, selectedLogLevels])

  // Calculate match positions when search regex changes
  useEffect(() => {
    if (!searchRegex || filteredLogs.length === 0) {
      setMatchPositions([])
      setCurrentMatchIndex(0)
      return
    }

    const positions: number[] = []
    filteredLogs.forEach((log, index) => {
      searchRegex.lastIndex = 0
      if (searchRegex.test(log)) {
        positions.push(index)
      }
    })
    setMatchPositions(positions)
    setCurrentMatchIndex(0)
  }, [searchRegex, filteredLogs])

  // Scroll to specific log entry
  const scrollToMatch = useCallback((logIndex: number) => {
    if (!logContentRef.current) return
    const logEntries = logContentRef.current.querySelectorAll('[data-log-index]')
    if (logEntries[logIndex]) {
      logEntries[logIndex].scrollIntoView({ behavior: 'smooth', block: 'center' })
    }
  }, [])

  // Navigate to previous match
  const goToPrevMatch = useCallback(() => {
    if (matchPositions.length === 0) return
    const newIndex = currentMatchIndex > 0 ? currentMatchIndex - 1 : matchPositions.length - 1
    setCurrentMatchIndex(newIndex)
    scrollToMatch(matchPositions[newIndex])
  }, [matchPositions, currentMatchIndex, scrollToMatch])

  // Navigate to next match
  const goToNextMatch = useCallback(() => {
    if (matchPositions.length === 0) return
    const newIndex = currentMatchIndex < matchPositions.length - 1 ? currentMatchIndex + 1 : 0
    setCurrentMatchIndex(newIndex)
    scrollToMatch(matchPositions[newIndex])
  }, [matchPositions, currentMatchIndex, scrollToMatch])

  // Auto scroll to first match when match positions change
  useEffect(() => {
    if (matchPositions.length > 0 && matchPositions[0] !== undefined) {
      setTimeout(() => scrollToMatch(matchPositions[0]), 100)
    }
  }, [matchPositions, scrollToMatch])

  return (
    <div
      ref={logPanelRef}
      className={`bg-white dark:bg-gray-800 shadow-lg ${
        logLayout === 'bottom'
          ? 'flex-shrink-0 border-t dark:border-gray-700'
          : logLayout === 'right'
          ? 'fixed right-0 border-l dark:border-gray-700 z-30'
          : 'fixed border dark:border-gray-700 rounded-lg z-50'
      }`}
      style={
        logLayout === 'bottom'
          ? { height: `${bottomPanelHeight}px` }
          : logLayout === 'right'
          ? {
              width: `${logPanelWidth}px`,
              top: '122px', // 全局导航 56px + Execution header 约 66px
              bottom: '60px' // 底部工具栏高度
            }
          : {
              width: `${logPanelWidth}px`,
              height: `${floatPanelHeight}px`,
              left: `${logPanelPosition.x}px`,
              top: `${logPanelPosition.y}px`,
            }
      }
    >
      {/* Resize handle - different for each layout */}
      {logLayout === 'bottom' && (
        <div
          className="h-3 bg-gray-100 dark:bg-gray-700 cursor-row-resize flex items-center justify-center hover:bg-gray-200 dark:hover:bg-gray-600 transition-colors border-b dark:border-gray-600"
          onMouseDown={(e) => startResize(e, 'bottom', 'bottom')}
        >
          <div className="flex items-center gap-2 text-gray-400 dark:text-gray-500">
            <GripVertical className="w-4 h-4" />
            <span className="text-xs">{t('execution.dragToResize')}</span>
          </div>
        </div>
      )}

      {logLayout === 'right' && (
        <div
          className="absolute left-0 top-0 bottom-0 w-1.5 bg-transparent hover:bg-blue-500/20 dark:hover:bg-blue-400/20 cursor-col-resize transition-colors"
          onMouseDown={(e) => startResize(e, 'left', 'right')}
        />
      )}

      {/* Float resize handles - edges and corners */}
      {logLayout === 'float' && (
        <>
          {/* Top edge */}
          <div
            className="absolute top-0 left-4 right-4 h-1.5 bg-transparent hover:bg-blue-500/20 dark:hover:bg-blue-400/20 cursor-row-resize transition-colors"
            onMouseDown={(e) => startResize(e, 'top', 'float')}
          />
          {/* Bottom edge */}
          <div
            className="absolute bottom-0 left-4 right-4 h-1.5 bg-transparent hover:bg-blue-500/20 dark:hover:bg-blue-400/20 cursor-row-resize transition-colors"
            onMouseDown={(e) => startResize(e, 'bottom', 'float')}
          />
          {/* Left edge */}
          <div
            className="absolute left-0 top-8 bottom-8 w-1.5 bg-transparent hover:bg-blue-500/20 dark:hover:bg-blue-400/20 cursor-col-resize transition-colors"
            onMouseDown={(e) => startResize(e, 'left', 'float')}
          />
          {/* Right edge */}
          <div
            className="absolute right-0 top-8 bottom-8 w-1.5 bg-transparent hover:bg-blue-500/20 dark:hover:bg-blue-400/20 cursor-col-resize transition-colors"
            onMouseDown={(e) => startResize(e, 'right', 'float')}
          />
          {/* Top-left corner */}
          <div
            className="absolute top-0 left-0 w-4 h-4 cursor-nwse-resize"
            onMouseDown={(e) => startResize(e, 'topLeft', 'float')}
          />
          {/* Top-right corner */}
          <div
            className="absolute top-0 right-0 w-4 h-4 cursor-nesw-resize"
            onMouseDown={(e) => startResize(e, 'topRight', 'float')}
          />
          {/* Bottom-left corner */}
          <div
            className="absolute bottom-0 left-0 w-4 h-4 cursor-nesw-resize"
            onMouseDown={(e) => startResize(e, 'bottomLeft', 'float')}
          />
          {/* Bottom-right corner */}
          <div
            className="absolute bottom-0 right-0 w-4 h-4 cursor-nwse-resize"
            onMouseDown={(e) => startResize(e, 'bottomRight', 'float')}
          />
        </>
      )}

      {/* Log header with layout toggle - draggable for float */}
      <div
        className={`flex items-center justify-between px-4 py-2 border-b dark:border-gray-700 bg-gray-50 dark:bg-gray-900 ${
          logLayout === 'float' ? 'cursor-move' : ''
        }`}
        onMouseDown={logLayout === 'float' ? startDrag : undefined}
      >
        {/* 复制成功提示 - 绝对定位，不影响布局 */}
        {copied && (
          <div className="absolute left-1/2 top-2 transform -translate-x-1/2 px-2 py-1 bg-black text-white text-xs rounded shadow-lg z-50">
            {t('execution.copied')}
          </div>
        )}
        <h2 className="font-semibold text-gray-900 dark:text-white text-sm select-none">{t('execution.logs')}</h2>
        <div className="flex items-center gap-1">
          {/* Copy logs button */}
          <button
            onClick={() => {
              navigator.clipboard.writeText(logs.join('\n'))
              setCopied(true)
              setTimeout(() => setCopied(false), 2000)
            }}
            className={`p-1.5 rounded transition-colors ${
              copied
                ? 'bg-green-500 text-white'
                : 'text-gray-500 hover:bg-gray-200 dark:hover:bg-gray-700'
            }`}
            title={t('execution.copyLogs')}
          >
            <Copy className="w-3.5 h-3.5" />
          </button>
          {/* Font size toggle */}
          <button
            onClick={() => setShowFontSize(!showFontSize)}
            className={`p-1.5 rounded transition-colors ${
              showFontSize
                ? 'bg-blue-500 text-white'
                : 'text-gray-500 hover:bg-gray-200 dark:hover:bg-gray-700'
            }`}
            title={t('execution.fontSize')}
          >
            <Type className="w-3.5 h-3.5" />
          </button>

          {/* Timestamp toggle */}
          <button
            onClick={() => setShowTimestamp(!showTimestamp)}
            className={`p-1.5 rounded transition-colors ${
              showTimestamp
                ? 'bg-blue-500 text-white'
                : 'text-gray-500 hover:bg-gray-200 dark:hover:bg-gray-700'
            }`}
            title={showTimestamp ? t('execution.hideTimestamp') : t('execution.showTimestamp')}
          >
            <Timer className="w-3.5 h-3.5" />
          </button>

          {/* Search toggle */}
          <button
            onClick={() => setShowSearch(!showSearch)}
            className={`p-1.5 rounded transition-colors ${
              showSearch
                ? 'bg-blue-500 text-white'
                : 'text-gray-500 hover:bg-gray-200 dark:hover:bg-gray-700'
            }`}
            title={t('execution.search')}
          >
            <Search className="w-3.5 h-3.5" />
          </button>

          {/* Divider */}
          <div className="w-px h-4 bg-gray-300 dark:bg-gray-600 mx-1" />

          {/* Layout toggle buttons */}
          <button
            onClick={() => setLogLayout('bottom')}
            className={`p-1.5 rounded transition-colors ${
              logLayout === 'bottom'
                ? 'bg-blue-500 text-white'
                : 'text-gray-500 hover:bg-gray-200 dark:hover:bg-gray-700'
            }`}
            title={t('execution.layoutBottom')}
          >
            <PanelBottomClose className="w-3.5 h-3.5" />
          </button>
          <button
            onClick={() => setLogLayout('right')}
            className={`p-1.5 rounded transition-colors ${
              logLayout === 'right'
                ? 'bg-blue-500 text-white'
                : 'text-gray-500 hover:bg-gray-200 dark:hover:bg-gray-700'
            }`}
            title={t('execution.layoutRight')}
          >
            <PanelRightClose className="w-3.5 h-3.5" />
          </button>
          <button
            onClick={() => setLogLayout('float')}
            className={`p-1.5 rounded transition-colors ${
              logLayout === 'float'
                ? 'bg-blue-500 text-white'
                : 'text-gray-500 hover:bg-gray-200 dark:hover:bg-gray-700'
            }`}
            title={t('execution.layoutFloat')}
          >
            <Square className="w-3.5 h-3.5" />
          </button>

          {/* Divider */}
          <div className="w-px h-4 bg-gray-300 dark:bg-gray-600 mx-1" />

          {/* Scroll buttons */}
          <button
            onClick={() => {
              const logContent = logPanelRef.current?.querySelector('.log-content')
              if (logContent) logContent.scrollTop = 0
            }}
            className="p-1.5 rounded text-gray-500 hover:bg-gray-200 dark:hover:bg-gray-700 transition-colors"
            title={t('execution.scrollToTop')}
          >
            <ArrowUpToLine className="w-3.5 h-3.5" />
          </button>
          <button
            onClick={() => {
              const logContent = logPanelRef.current?.querySelector('.log-content')
              if (logContent) logContent.scrollTop = logContent.scrollHeight
            }}
            className="p-1.5 rounded text-gray-500 hover:bg-gray-200 dark:hover:bg-gray-700 transition-colors"
            title={t('execution.scrollToBottom')}
          >
            <ArrowDownToLine className="w-3.5 h-3.5" />
          </button>

          {/* Close button */}
          <button
            onClick={onClose}
            className="p-1 hover:bg-gray-200 dark:hover:bg-gray-700 rounded transition-colors ml-1"
            title={t('execution.hideLogs')}
          >
            <X className="w-4 h-4 text-gray-500" />
          </button>
        </div>
      </div>

      {/* Search toolbar - fixed position, below main toolbar */}
      {showSearch && (
        <div className="flex items-center gap-2 px-2 py-1 bg-white dark:bg-gray-800 border-b dark:border-gray-700">
          {/* Log level dropdown */}
          <div className="relative level-dropdown-container">
              <button
                onClick={() => setShowLevelDropdown(!showLevelDropdown)}
                className="flex items-center gap-1 px-2 py-1 bg-gray-100 dark:bg-gray-700 text-xs rounded hover:bg-gray-200 dark:hover:bg-gray-600 transition-colors"
                title={t('execution.logLevels')}
              >
                <span>{t('execution.logLevels')}</span>
                <span className="text-gray-500">{selectedLogLevels.length}/4</span>
              </button>
              {showLevelDropdown && (
                <div className="absolute top-full left-0 mt-1 bg-white dark:bg-gray-800 border dark:border-gray-700 rounded shadow-lg z-50 py-1 min-w-[100px]">
                  {(['ERROR', 'WARN', 'INFO', 'DEBUG'] as const).map(level => (
                    <label
                      key={level}
                      className="flex items-center gap-2 px-3 py-1 hover:bg-gray-100 dark:hover:bg-gray-700 cursor-pointer text-xs"
                    >
                      <input
                        type="checkbox"
                        checked={selectedLogLevels.includes(level)}
                        onChange={() => {
                          setSelectedLogLevels(prev => {
                            if (prev.includes(level)) {
                              if (prev.length === 1) return prev
                              return prev.filter(l => l !== level)
                            }
                            return [...prev, level]
                          })
                        }}
                        className="w-3 h-3 rounded"
                      />
                      <span className={level === 'ERROR' ? 'text-red-500' : level === 'WARN' ? 'text-yellow-500' : level === 'INFO' ? 'text-blue-500' : 'text-gray-500'}>{level}</span>
                    </label>
                  ))}
                </div>
              )}
            </div>

            {/* Search input */}
            <div className="flex-1 relative">
              <input
                ref={searchInputRef}
                type="text"
                value={searchQuery}
                onChange={(e) => {
                  setSearchQuery(e.target.value)
                  // 实时搜索
                  handleSearch(e.target.value)
                }}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') {
                    if (e.shiftKey) {
                      goToPrevMatch()
                    } else {
                      goToNextMatch()
                    }
                  }
                }}
                placeholder={t('execution.searchPlaceholder')}
                className="w-full bg-gray-100 dark:bg-gray-700 text-xs rounded px-2 py-1 focus:outline-none focus:ring-1 focus:ring-blue-500"
              />
            </div>

            {/* Case sensitive toggle */}
            <button
              onClick={() => {
                setCaseSensitive(!caseSensitive)
                handleSearch(searchQuery)
              }}
              className={`p-1 rounded transition-colors ${
                caseSensitive
                  ? 'bg-blue-500 text-white'
                  : 'text-gray-500 hover:bg-gray-200 dark:hover:bg-gray-700'
              }`}
              title={t('execution.caseSensitive')}
            >
              <CaseSensitive className="w-3.5 h-3.5" />
            </button>

            {/* Regex toggle */}
            <button
              onClick={() => {
                setUseRegex(!useRegex)
                handleSearch(searchQuery)
              }}
              className={`p-1 rounded transition-colors ${
                useRegex
                  ? 'bg-blue-500 text-white'
                  : 'text-gray-500 hover:bg-gray-200 dark:hover:bg-gray-700'
              }`}
              title={t('execution.useRegex')}
            >
              <Regex className="w-3.5 h-3.5" />
            </button>

            {/* Match count */}
            {searchRegex && (
              <span className="text-xs text-gray-500 dark:text-gray-400 min-w-[50px] text-center">
                {matchPositions.length > 0 ? `${currentMatchIndex + 1}/${matchPositions.length}` : t('execution.noResults')}
              </span>
            )}

            {/* Navigation buttons - compact */}
            {matchPositions.length > 0 && (
              <div className="flex gap-0.5">
                <button
                  onClick={goToPrevMatch}
                  className="p-0.5 rounded text-gray-500 hover:bg-gray-200 dark:hover:bg-gray-700 transition-colors"
                  title={t('execution.prevMatch')}
                >
                  <ChevronUp className="w-3.5 h-3.5" />
                </button>
                <button
                  onClick={goToNextMatch}
                  className="p-0.5 rounded text-gray-500 hover:bg-gray-200 dark:hover:bg-gray-700 transition-colors"
                  title={t('execution.nextMatch')}
                >
                  <ChevronDown className="w-3.5 h-3.5" />
                </button>
              </div>
            )}

            {/* Search error */}
            {searchError && (
              <span className="text-xs text-red-500">{searchError}</span>
            )}
        </div>
      )}

      {/* Font size slider container - fixed position, below toolbar */}
      {showFontSize && (
        <div className="relative h-0 pointer-events-none">
          <div className="absolute top-1 right-2 pointer-events-auto flex items-center gap-1 px-1 py-0.5 bg-white/60 dark:bg-gray-700/60 rounded transition-all opacity-70 hover:opacity-100 hover:bg-white/90 dark:hover:bg-gray-700/90 hover:shadow-sm hover:border hover:border-gray-200 dark:hover:border-gray-600">
            <span className="text-xs text-gray-400 dark:text-gray-500 w-6 text-center font-mono">{logFontSize}</span>
            <input
              type="range"
              min="10"
              max="28"
              value={logFontSize}
              onChange={(e) => setLogFontSize(Number(e.target.value))}
              className="w-10 h-0.5 bg-gray-300 dark:bg-gray-600 rounded-lg appearance-none cursor-pointer"
              title={t('execution.fontSize')}
            />
          </div>
        </div>
      )}

      {/* Log content */}
      <div
        ref={logContentRef}
        className={`log-content p-3 overflow-y-auto font-mono bg-gray-50 dark:bg-gray-900 ${
          logLayout === 'bottom'
            ? (showSearch ? 'h-[calc(100%-48px-32px)]' : 'h-[calc(100%-48px)]')
            : (showSearch ? 'h-[calc(100%-40px-32px)]' : 'h-[calc(100%-40px)]')
        }`}
        style={{ fontSize: `${logFontSize}px` }}
      >

        {filteredLogs.length === 0 ? (
          <div className="text-center text-gray-500 dark:text-gray-400 py-8">
            <Clock className="w-8 h-8 mx-auto mb-2 opacity-50" />
            <p>{t('execution.noLogs')}</p>
          </div>
        ) : (
          filteredLogs.map((log, index) => {
            // 当前匹配位置的标记
            const isCurrentMatch = matchPositions.length > 0 && matchPositions[currentMatchIndex] === index

            // 解析日志格式: [timestamp] LEVEL: message
            // 注意：message 可能包含 ANSI 转义序列和换行符
            const match = log.match(/^\[([^\]]+)\]\s+(ERROR|WARN|INFO|DEBUG):\s+(.*)/)
            if (!match) {
              // 未匹配格式的日志，直接渲染（支持 ANSI 颜色）
              return (
                <div key={index} data-log-index={index} className={`flex items-start py-1 px-2 border-b dark:border-gray-800 last:border-0 text-gray-600 dark:text-gray-400 whitespace-pre-wrap break-all font-mono ${isCurrentMatch ? 'bg-yellow-100 dark:bg-yellow-900/30' : ''}`}>
                  <span className="flex-shrink-0 w-4 h-4 -ml-4 mr-0 flex items-center">
                    {isCurrentMatch && (
                      <ChevronRight className="w-4 h-4 text-yellow-600 dark:text-yellow-400" />
                    )}
                  </span>
                  <AnsiText text={log} highlightPattern={searchRegex} />
                </div>
              )
            }
            const [, timestamp, level, message] = match

            // 根据级别应用样式
            const levelStyles: Record<string, string> = {
              ERROR: 'text-red-600 dark:text-red-400 bg-red-50/50 dark:bg-red-900/20',
              WARN: 'text-yellow-600 dark:text-yellow-400 bg-yellow-50/50 dark:bg-yellow-900/20',
              INFO: 'text-gray-700 dark:text-gray-300',
              DEBUG: 'text-gray-500 dark:text-gray-500',
            }
            const levelStyle = levelStyles[level] || levelStyles.INFO

            // 处理多行消息，移除末尾换行符后分割
            const trimmedMessage = message.replace(/\n$/, '')
            const lines = trimmedMessage.split('\n')

            return (
              <div key={index} data-log-index={index} className={`flex items-start py-1 px-2 border-b dark:border-gray-800 last:border-0 ${levelStyle} ${isCurrentMatch ? 'bg-yellow-100 dark:bg-yellow-900/30' : ''} font-mono`}>
                <span className="flex-shrink-0 w-4 h-4 -ml-4 mr-0 flex items-center">
                  {isCurrentMatch && (
                    <ChevronRight className="w-4 h-4 text-yellow-600 dark:text-yellow-400" />
                  )}
                </span>
                {lines.map((line, lineIndex) => (
                  <div key={lineIndex} className="flex">
                    {lineIndex === 0 && (
                      <>
                        {showTimestamp && (
                          <span className="text-gray-400 dark:text-gray-500 mr-2 flex-shrink-0">[{timestamp}]</span>
                        )}
                        <span className={`font-semibold mr-1 flex-shrink-0 ${
                          level === 'ERROR' ? 'text-red-500 dark:text-red-400' :
                          level === 'WARN' ? 'text-yellow-500 dark:text-yellow-400' :
                          level === 'INFO' ? 'text-blue-500 dark:text-blue-400' :
                          'text-gray-400 dark:text-gray-500'
                        }`}>{level}:</span>
                      </>
                    )}
                    {lineIndex > 0 && showTimestamp && (
                      <span className="text-gray-400 dark:text-gray-500 mr-2 flex-shrink-0" style={{ visibility: 'hidden' }}>[{timestamp}]</span>
                    )}
                    {lineIndex > 0 && (
                      <span className="font-semibold mr-1 flex-shrink-0" style={{ visibility: 'hidden' }}>{level}:</span>
                    )}
                    <span className="whitespace-pre-wrap break-all flex-1"><AnsiText text={line} highlightPattern={searchRegex} /></span>
                  </div>
                ))}
              </div>
            )
          })
        )}
        <div ref={logsEndRef} />
      </div>
    </div>
  )
}
