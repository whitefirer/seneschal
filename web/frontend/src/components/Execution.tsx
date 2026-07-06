import { useState, useEffect, useRef, useCallback, useMemo } from 'react'
import { useParams } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { 
  ArrowLeft, RefreshCw, CheckCircle, XCircle, Clock, AlertCircle, X, GripVertical, 
  Map, FileText, PanelBottomClose, PanelRightClose, Square, ArrowUpToLine, ArrowDownToLine, 
  Timer, Search, Type, ChevronUp, ChevronDown, CaseSensitive, Regex, ChevronsDownUp, ChevronsUpDown,
  ChevronRight, Copy
} from 'lucide-react'
import { useWebSocket, type ProgressEvent } from '@/hooks/useWebSocket'
import { executionsApi } from '@/api/client'
import { MarkdownView } from './MarkdownView'
import { WorkflowGraph, workflowToFlowSteps, type FlowStep } from '@/components/WorkflowGraph'
// @ts-ignore - ansi-to-html 没有完整的 ESM 类型定义
import AnsiToHtmlModule from 'ansi-to-html'

// 创建 ANSI 转 HTML 的实例（使用正确的导入方式）
const AnsiToHtml = AnsiToHtmlModule as any
const ansiConverter = new AnsiToHtml({
  fg: '#333',
  bg: 'transparent',
  newline: true,
  escapeXML: true,
  stream: false
})

// 解析 ANSI 颜色代码为 HTML
function parseAnsiToHtml(text: string): string {
  return ansiConverter.toHtml(text)
}

// 在 HTML 中高亮匹配文本（在文本节点上操作，避免破坏 HTML 结构）
function highlightInHtml(html: string, pattern: RegExp | null | undefined): string {
  if (!pattern) return html
  
  // 克隆正则表达式，避免修改原始 lastIndex
  const clonedPattern = new RegExp(pattern.source, pattern.flags)
  
  const parser = new DOMParser()
  const doc = parser.parseFromString(html, 'text/html')
  
  const walk = (node: Node) => {
    if (node.nodeType === Node.TEXT_NODE) {
      const text = node.textContent || ''
      clonedPattern.lastIndex = 0
      if (clonedPattern.test(text)) {
        clonedPattern.lastIndex = 0
        const span = document.createElement('span')
        let lastIndex = 0
        let match
        while ((match = clonedPattern.exec(text)) !== null) {
          // 添加匹配前的文本
          if (match.index > lastIndex) {
            span.appendChild(document.createTextNode(text.slice(lastIndex, match.index)))
          }
          // 添加高亮的匹配文本
          const mark = document.createElement('mark')
          mark.className = 'bg-yellow-300 dark:bg-yellow-600 rounded px-0.5'
          mark.textContent = match[0]
          span.appendChild(mark)
          lastIndex = clonedPattern.lastIndex
        }
        // 添加剩余的文本
        if (lastIndex < text.length) {
          span.appendChild(document.createTextNode(text.slice(lastIndex)))
        }
        node.parentNode?.replaceChild(span, node)
      }
    } else {
      for (const child of Array.from(node.childNodes)) {
        walk(child)
      }
    }
  }
  
  walk(doc.body)
  return doc.body.innerHTML
}

// 渲染带有 ANSI 颜色的文本
function AnsiText({ text, highlightPattern }: { text: string; highlightPattern?: RegExp | null }) {
  const html = useMemo(() => {
    // 先进行 ANSI 转义
    const ansiHtml = parseAnsiToHtml(text)
    // 然后在 HTML 的文本节点上高亮匹配内容
    return highlightInHtml(ansiHtml, highlightPattern)
  }, [text, highlightPattern])
  return <span className="ansi-output" dangerouslySetInnerHTML={{ __html: html }} />
}

interface Step {
  id: string
  name: string
  description?: string
  status: 'pending' | 'running' | 'success' | 'failed' | 'skipped'
  output?: string
  error?: string
  startTime?: string
  endTime?: string
  duration?: string
  action?: string
  parentId?: string
  children?: Step[]
  // DAG 字段
  next?: string[]
  depends_on?: string[]
  join_mode?: string
  // Condition 字段
  expression?: string
  condition_result?: boolean | null
  then_children?: Step[]
  else_children?: Step[]
  // Sleep 字段
  sleepDuration?: string
  // Shell 字段
  shellCommand?: string
  // HTTP 字段
  httpUrl?: string
  httpMethod?: string
  // Log 字段
  logMessage?: string
  // AI streaming output: incremental tokens accumulate here as they arrive,
  // separate from `output` (which is set on step_complete with the final
  // annotated text). The detail panel renders this as markdown.
  aiOutput?: string
}

export default function Execution() {
  const { t } = useTranslation()
  const { id } = useParams<{ id: string }>()
  const [workflowName, setWorkflowName] = useState('')
  const [workflowFile, setWorkflowFile] = useState('')
  const [steps, setSteps] = useState<Step[]>([])
  const [logs, setLogs] = useState<string[]>([])
  const [status, setStatus] = useState<'running' | 'success' | 'failed'>('running')
  const [selectedStep, setSelectedStep] = useState<Step | null>(null)
  const [viewMode, setViewMode] = useState<'graph' | 'list'>('graph')
  const [showLogs, setShowLogs] = useState(true)
  const [showMiniMap, setShowMiniMap] = useState(true)
  const [showTimestamp, setShowTimestamp] = useState(false)
  const [logFontSize, setLogFontSize] = useState(12) // 字体大小（像素值）
  
  // 折叠节点状态
  const [collapsedNodes, setCollapsedNodes] = useState<Set<string>>(new Set())
  
  // 可拖动按钮位置
  const [buttonPosition, setButtonPosition] = useState({ x: 0, y: 0 })
  const [isDraggingButton, setIsDraggingButton] = useState(false)
  const buttonDragRef = useRef<{ startX: number; startY: number; startPos: { x: number; y: number } } | null>(null)
  
  // 搜索相关状态
  const [showSearch, setShowSearch] = useState(false)
  const [searchQuery, setSearchQuery] = useState('')
  const [searchRegex, setSearchRegex] = useState<RegExp | null>(null)
  const [searchError, setSearchError] = useState<string | null>(null)
  const [selectedLogLevels, setSelectedLogLevels] = useState<string[]>(['ERROR', 'WARN', 'INFO', 'DEBUG'])
  const [showLevelDropdown, setShowLevelDropdown] = useState(false) // 下拉列表展开状态
  const [showFontSize, setShowFontSize] = useState(false) // 字体滑块显隐状态
  const [copied, setCopied] = useState(false) // 复制成功提示状态
  const [copiedField, setCopiedField] = useState<string | null>(null) // 记录哪个字段被复制
  
  // 复制到剪贴板
  const copyToClipboard = useCallback((text: string, field: string) => {
    navigator.clipboard.writeText(text)
    setCopiedField(field)
    setTimeout(() => setCopiedField(null), 2000)
  }, [])
  
  // 搜索选项
  const [caseSensitive, setCaseSensitive] = useState(false) // 区分大小写
  const [useRegex, setUseRegex] = useState(true) // 正则模式（默认开启）
  const [currentMatchIndex, setCurrentMatchIndex] = useState(0) // 当前匹配索引
  const [matchPositions, setMatchPositions] = useState<number[]>([]) // 匹配的日志索引数组
  const logContentRef = useRef<HTMLDivElement>(null) // 日志内容区域引用
  const searchInputRef = useRef<HTMLInputElement>(null) // 搜索输入框引用

  const [logLayout, setLogLayout] = useState<'bottom' | 'right' | 'float'>('float')
  const [bottomPanelHeight, setBottomPanelHeight] = useState(250) // 底部布局专用高度
  const [floatPanelHeight, setFloatPanelHeight] = useState(300) // 悬浮窗口专用高度
  const [logPanelWidth, setLogPanelWidth] = useState(480) // 默认隐藏时间戳，初始宽度480（容纳中文标题和所有图标）
  const [logPanelPosition, setLogPanelPosition] = useState({ x: 0, y: 70 })
  const [detailPanelPosition, setDetailPanelPosition] = useState({ x: 0, y: 0 })
  const [isDraggingDetail, setIsDraggingDetail] = useState(false)
  const detailDragStart = useRef({ x: 0, y: 0, panelX: 0, panelY: 0 })
  const logsEndRef = useRef<HTMLDivElement>(null)
  const stepsRef = useRef<Step[]>([])
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

  // Recursively update step status in tree by ID, and update parent status
  const updateStepInTree = useCallback((steps: Step[], stepId: string, updates: Partial<Step>): Step[] => {
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
  }, [])

  // Find step by ID in tree
  const findStepInTree = useCallback((steps: Step[], stepId: string): Step | null => {
    for (const step of steps) {
      if (step.id === stepId) {
        return step
      }
      if (step.children && step.children.length > 0) {
        const found = findStepInTree(step.children, stepId)
        if (found) return found
      }
      // 处理 condition 的 then_children 和 else_children
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
  }, [])

  // Handle progress events
  const handleProgress = useCallback((event: ProgressEvent) => {
    if (!id || event.executionId !== id) return

    if (event.workflowName && !workflowName) {
      setWorkflowName(event.workflowName)
    }
    if (event.workflowFile && !workflowFile) {
      setWorkflowFile(event.workflowFile)
    }

    // 获取实际的 stepId 和 name（支持 snake_case 和 camelCase）
    const actualStepId = event.stepId || event.step_id || ''
    const actualName = event.stepName || event.name || ''
    const actualTimestamp = event.timestamp || event.time || ''
    const actualConditionResult = event.conditionResult ?? event.condition_result

    // 使用后端提供的日志消息
    if (event.logMessage) {
      const level = event.logLevel || 'INFO'
      setLogs((prev) => [...prev, `[${actualTimestamp}] ${level}: ${event.logMessage}`])
    }

    switch (event.type) {
      case 'workflow_start':
        setStatus('running')
        break

      case 'step_start': {
        const targetId = actualStepId || actualName || ''
        setSteps((prev) => updateStepInTree(prev, targetId, {
          status: 'running',
          startTime: actualTimestamp,
          action: event.action || '',
        }))
        break
      }

      case 'step_complete': {
        const targetId = actualStepId || actualName || ''
        const updates: Partial<Step> = {
          status: event.status as Step['status'] || 'success',
          endTime: actualTimestamp,
          duration: event.duration,
        }
        // Condition step: 更新 condition_result
        if (event.action === 'condition' && actualConditionResult !== undefined) {
          updates.condition_result = actualConditionResult
        }
        setSteps((prev) => updateStepInTree(prev, targetId, updates))
        break
      }

      case 'step_output':
        setSteps((prev) => {
          const updateOutput = (steps: Step[]): Step[] => {
            return steps.map((step) => {
              // 检查当前节点是否匹配
              if (step.id === actualStepId || step.name === actualName) {
                return { ...step, output: (step.output || '') + (event.output || '') + '\n' }
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
          const result = updateOutput(prev)
          return result
        })
        break

      case 'ai_token': {
        // Incremental AI token: append to the step's aiOutput (streaming text).
        // Unlike step_output, no trailing newline — these are partial tokens.
        const targetId = actualStepId || actualName || ''
        setSteps((prev) => {
          const appendToken = (steps: Step[]): Step[] => {
            return steps.map((step) => {
              if (step.id === targetId || step.name === actualName) {
                return { ...step, aiOutput: (step.aiOutput || '') + (event.output || '') }
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
          return appendToken(prev)
        })
        break
      }

      case 'workflow_end':
        setStatus((event.status as 'running' | 'success' | 'failed') || 'success')
        // 执行完成后重新获取完整数据（包含 condition_result 等）
        if (id) {
          loadExecution(id)
        }
        break
    }
  }, [id, workflowName, updateStepInTree])

  const { subscribe, unsubscribe, connected, disconnect } = useWebSocket({
    onMessage: handleProgress,
  })

  // Load execution on mount
  useEffect(() => {
    if (id) {
      subscribe(id)
      loadExecution(id)
    }
    return () => {
      if (id) {
        unsubscribe(id)
      }
      disconnect()
    }
  }, [id])

  // Keep steps ref updated
  useEffect(() => {
    stepsRef.current = steps
  }, [steps])

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
        newWidth = Math.max(minWidth, Math.min(maxWidth, startWidth.current - deltaX))
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

  // Detail panel drag handling
  useEffect(() => {
    if (!isDraggingDetail) return

    const handleMouseMove = (e: MouseEvent) => {
      const deltaX = e.clientX - detailDragStart.current.x
      const deltaY = e.clientY - detailDragStart.current.y
      
      const newX = Math.max(0, Math.min(window.innerWidth - 400, detailDragStart.current.panelX + deltaX))
      const newY = Math.max(56, Math.min(window.innerHeight - 100, detailDragStart.current.panelY + deltaY))
      
      setDetailPanelPosition({ x: newX, y: newY })
    }

    const handleMouseUp = () => {
      setIsDraggingDetail(false)
      document.body.style.cursor = 'default'
      document.body.style.userSelect = 'auto'
    }

    document.addEventListener('mousemove', handleMouseMove)
    document.addEventListener('mouseup', handleMouseUp)

    return () => {
      document.removeEventListener('mousemove', handleMouseMove)
      document.removeEventListener('mouseup', handleMouseUp)
    }
  }, [isDraggingDetail])

  const startDetailDrag = (e: React.MouseEvent) => {
    e.preventDefault()
    detailDragStart.current = {
      x: e.clientX,
      y: e.clientY,
      panelX: detailPanelPosition.x,
      panelY: detailPanelPosition.y
    }
    setIsDraggingDetail(true)
    document.body.style.cursor = 'move'
    document.body.style.userSelect = 'none'
  }

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

  async function loadExecution(execId: string) {
    try {
      const data = await executionsApi.get(execId)
      setWorkflowName(data.workflowName)
      if (data.workflowFile) {
        setWorkflowFile(data.workflowFile)
      }
      setStatus(data.status as 'running' | 'success' | 'failed')
      
      // DEBUG: Log raw data
      
      // Convert steps with children structure (preserve tree)
      const convertSteps = (steps: any[], parentId?: string, childIndex = 0): Step[] => {
        return steps.map((s, i) => {
          // Generate unique ID: prefer step.id, otherwise generate from name or parent+index
          const generateId = () => {
            if (s.id) return s.id
            if (s.name) return `step-${s.name.toLowerCase().replace(/\s+/g, '-')}`
            if (parentId) return `${parentId}-child-${childIndex + i}`
            return `step-${childIndex + i}`
          }
          
          const step: Step = {
            id: generateId(),
            name: s.name || `Step ${childIndex + i + 1}`,
            description: s.description,
            status: s.status || 'pending',
            output: s.output,
            error: s.error,
            startTime: s.startTime,
            endTime: s.endTime,
            duration: s.duration,
            action: s.action || '',
            // DAG 字段
            next: s.next,
            depends_on: s.depends_on,
            join_mode: s.join_mode,
            // Condition 字段
            expression: s.expression,
            condition_result: s.condition_result,
            // Sleep 字段
            sleepDuration: s.sleepDuration,
            // Shell 字段
            shellCommand: s.shellCommand,
            // HTTP 字段
            httpUrl: s.httpUrl,
            httpMethod: s.httpMethod,
            // Log 字段
            logMessage: s.logMessage,
          }
          // Recursively convert children
          if (s.children && s.children.length > 0) {
            step.children = convertSteps(s.children, step.id, 0)
          }
          // Foreach: convert do attribute (foreach uses 'do' not 'children')
          if ((s.action === 'foreach' || s.action === 'loop') && s.do && s.do.length > 0) {
            step.children = convertSteps(s.do, step.id, 0)
          }
          // Condition: convert then_children and else_children (兼容 then/else 属性名)
          const thenChildren = s.then_children || s.then || []
          const elseChildren = s.else_children || s.else || []
          if (thenChildren.length > 0) {
            step.then_children = convertSteps(thenChildren, step.id, 0)
          }
          if (elseChildren.length > 0) {
            step.else_children = convertSteps(elseChildren, step.id, 0)
          }
          return step
        })
      }
      
      if (data.steps && data.steps.length > 0) {
        const newSteps = convertSteps(data.steps)
        const printSteps = (steps: Step[], indent = '') => {
          steps.forEach(s => {
            if (s.children && s.children.length > 0) {
              printSteps(s.children, indent + '  ')
            }
          })
        }
        printSteps(newSteps)
        setSteps(newSteps)
        stepsRef.current = newSteps
      }
      
      if (data.logs) {
        setLogs(data.logs.map((l: any) => `[${l.timestamp}] ${l.level.toUpperCase()}: ${l.message}`))
      }
    } catch (error) {
      console.error('Failed to load execution:', error)
    }
  }

  // Convert steps to FlowStep for graph (preserve tree structure)
  const flowSteps: FlowStep[] = workflowToFlowSteps(steps)

  // 获取所有可折叠的节点（condition、foreach、parallel节点）
  const collapsibleNodes = useMemo(() => {
    const nodes: string[] = []
    function findCollapsible(steps: FlowStep[]) {
      steps.forEach(step => {
        if (step.action === 'condition') {
          const thenChildren = step.then_children || step.then || []
          const elseChildren = step.else_children || step.else || []
          if (thenChildren.length > 0 || elseChildren.length > 0) {
            nodes.push(step.id)
            // 递归查找子节点中的condition
            findCollapsible(thenChildren)
            findCollapsible(elseChildren)
          }
        }
        // foreach 和 parallel 节点如果有子节点也可以折叠
        if (step.action === 'foreach' || step.action === 'parallel') {
          if (step.children && step.children.length > 0) {
            nodes.push(step.id)
          }
          findCollapsible(step.children || [])
        }
      })
    }
    findCollapsible(flowSteps)
    return nodes
  }, [flowSteps])

  // 折叠/展开所有节点
  const handleCollapseAll = useCallback(() => {
    setCollapsedNodes(new Set(collapsibleNodes))
  }, [collapsibleNodes])

  const handleExpandAll = useCallback(() => {
    setCollapsedNodes(new Set())
  }, [])

  const isAllCollapsed = collapsibleNodes.length > 0 && collapsibleNodes.every(id => collapsedNodes.has(id))

  // 搜索栏显示时自动获得焦点
  useEffect(() => {
    if (showSearch && searchInputRef.current) {
      setTimeout(() => searchInputRef.current?.focus(), 100)
    }
  }, [showSearch])

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

  function getStatusIcon(stepStatus: string) {
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

  function getStatusColor(stepStatus: string) {
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

  function calculateDuration(startTime?: string, endTime?: string): string {
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
    } catch (e) {
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

  // Navigate to previous match
  const goToPrevMatch = useCallback(() => {
    if (matchPositions.length === 0) return
    const newIndex = currentMatchIndex > 0 ? currentMatchIndex - 1 : matchPositions.length - 1
    setCurrentMatchIndex(newIndex)
    scrollToMatch(matchPositions[newIndex])
  }, [matchPositions, currentMatchIndex])

  // Navigate to next match
  const goToNextMatch = useCallback(() => {
    if (matchPositions.length === 0) return
    const newIndex = currentMatchIndex < matchPositions.length - 1 ? currentMatchIndex + 1 : 0
    setCurrentMatchIndex(newIndex)
    scrollToMatch(matchPositions[newIndex])
  }, [matchPositions, currentMatchIndex])

  // Scroll to specific log entry
  const scrollToMatch = useCallback((logIndex: number) => {
    if (!logContentRef.current) return
    const logEntries = logContentRef.current.querySelectorAll('[data-log-index]')
    if (logEntries[logIndex]) {
      logEntries[logIndex].scrollIntoView({ behavior: 'smooth', block: 'center' })
    }
  }, [])

  // Auto scroll to first match when match positions change
  useEffect(() => {
    if (matchPositions.length > 0 && matchPositions[0] !== undefined) {
      setTimeout(() => scrollToMatch(matchPositions[0]), 100)
    }
  }, [matchPositions, scrollToMatch])

  // Render step tree recursively for list view
  const renderStepTree = (stepList: Step[], indent = 0) => {
    return stepList.map((step) => (
      <div key={step.id}>
        <div
          className={`px-4 py-3 cursor-pointer transition-colors border-l-4 ${getStatusColor(step.status)} ${
            selectedStep?.id === step.id ? 'ring-2 ring-blue-500 ring-inset' : ''
          }`}
          style={{ paddingLeft: `${indent + 16}px` }}
          onClick={() => setSelectedStep(step)}
        >
          <div className="flex items-center gap-3">
            {getStatusIcon(step.status)}
            <div className="flex-1 min-w-0">
              <div className="flex items-center justify-between">
                <p className="font-medium text-gray-900 dark:text-white truncate">
                  {step.name}
                </p>
                <span className="text-xs text-gray-500 dark:text-gray-400">
                  {calculateDuration(step.startTime, step.endTime)}
                </span>
              </div>
              {step.action && (
                <p className="text-sm text-gray-500 dark:text-gray-400 truncate">
                  {step.action}
                </p>
              )}
            </div>
          </div>
        </div>
        {step.children && step.children.length > 0 && renderStepTree(step.children, indent + 20)}
      </div>
    ))
  }

  // Find step by ID recursively
  const findStepById = useCallback((stepList: Step[], stepId: string): Step | null => {
    for (const s of stepList) {
      if (s.id === stepId) return s
      if (s.children && s.children.length > 0) {
        const found = findStepById(s.children, stepId)
        if (found) return found
      }
      // 处理 condition 的 then_children 和 else_children
      if (s.then_children && s.then_children.length > 0) {
        const found = findStepById(s.then_children, stepId)
        if (found) return found
      }
      if (s.else_children && s.else_children.length > 0) {
        const found = findStepById(s.else_children, stepId)
        if (found) return found
      }
    }
    return null
  }, [steps])

  return (
    <div className="flex flex-col h-[calc(100vh-3.5rem)] bg-gray-50 dark:bg-gray-900 overflow-hidden">
      {/* Header */}
      <div className="bg-white dark:bg-gray-800 border-b dark:border-gray-700 flex-shrink-0">
        <div className="px-4 sm:px-6 py-3">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-4">
              <a
                href="/history"
                className="flex items-center gap-2 text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200"
              >
                <ArrowLeft className="w-5 h-5" />
              </a>
              <div>
                <div className="flex items-center gap-2">
                  <a
                    href={`/editor/${workflowFile || workflowName}`}
                    className="text-xl font-semibold text-gray-900 dark:text-white hover:text-blue-600 dark:hover:text-blue-400 transition-colors"
                  >
                    {workflowName || 'Loading...'}
                  </a>
                  <span className="text-sm text-gray-500 dark:text-gray-400">
                    {id ? `(${id})` : ''}
                  </span>
                </div>
                <div className="flex items-center gap-3 mt-1">
                  <span className={`inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-medium ${
                    status === 'running' ? 'bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-400' :
                    status === 'success' ? 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400' :
                    'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400'
                  }`}>
                    {status === 'running' && <RefreshCw className="w-3 h-3 animate-spin" />}
                    {status === 'success' && <CheckCircle className="w-3 h-3" />}
                    {status === 'failed' && <XCircle className="w-3 h-3" />}
                    {status.charAt(0).toUpperCase() + status.slice(1)}
                  </span>
                  <span className="text-sm text-gray-500 dark:text-gray-400">
                    {connected ? '🟢 Live' : '⚪ Disconnected'}
                  </span>
                </div>
              </div>
            </div>
            
            <div className="flex items-center gap-2">
              <button
                onClick={() => setViewMode(viewMode === 'graph' ? 'list' : 'graph')}
                className="px-3 py-1.5 text-sm bg-gray-100 dark:bg-gray-700 hover:bg-gray-200 dark:hover:bg-gray-600 rounded-lg transition-colors"
              >
                {viewMode === 'graph' ? t('execution.listView') : t('execution.graphView')}
              </button>
              <button
                onClick={() => id && loadExecution(id)}
                className="p-2 bg-gray-100 dark:bg-gray-700 hover:bg-gray-200 dark:hover:bg-gray-600 rounded-lg transition-colors"
                title={t('common.refresh')}
              >
                <RefreshCw className="w-4 h-4" />
              </button>
            </div>
          </div>
        </div>
      </div>

      {/* Main Content - Graph/List */}
      <div className="flex-1 overflow-hidden relative">
        <div className="h-full">
          {viewMode === 'graph' ? (
            <WorkflowGraph
              steps={flowSteps}
              executionSteps={flowSteps}
              showMiniMap={showMiniMap}
              logLayout={showLogs ? logLayout : 'none'}
              collapsedNodes={collapsedNodes}
              onCollapseChange={setCollapsedNodes}
              onNodeClick={(step, viewportPosition) => {
                // Find step in the original tree structure by ID
                const found = findStepById(steps, step.id)
                if (found) {
                  // 设置详情面板位置：屏幕中央偏右，但不超出屏幕
                  const panelWidth = 384 // w-96 = 24rem = 384px
                  const panelHeight = 300 // 估计高度
                  const headerHeight = 56 // 全局导航栏
                  const padding = 20
                  
                  let x: number, y: number
                  
                  // 如果有节点视口位置信息，在节点附近显示
                  if (viewportPosition) {
                    // 在节点右侧显示，如果空间不够则在左侧
                    if (viewportPosition.right + panelWidth + padding < window.innerWidth) {
                      // 节点右侧有足够空间
                      x = viewportPosition.right + padding
                    } else if (viewportPosition.left - panelWidth - padding > 0) {
                      // 节点左侧有足够空间
                      x = viewportPosition.left - panelWidth - padding
                    } else {
                      // 左右都没有足够空间，在节点右侧但确保在屏幕内
                      x = Math.max(padding, Math.min(window.innerWidth - panelWidth - padding, viewportPosition.right + padding))
                    }
                    // 在节点垂直居中显示，但保持在屏幕内
                    y = Math.max(headerHeight + padding, Math.min(window.innerHeight - panelHeight - padding, viewportPosition.y - panelHeight / 3))
                  } else {
                    // 默认位置：屏幕中央偏右
                    x = Math.min(window.innerWidth - panelWidth - padding, Math.max(padding, window.innerWidth / 2))
                    y = headerHeight + padding
                  }
                  
                  setDetailPanelPosition({ x, y })
                  setSelectedStep(found)
                }
              }}
            />
          ) : (
            <div className="h-full overflow-y-auto">
              <div className="divide-y dark:divide-gray-700">
                {steps.length === 0 ? (
                  <div className="px-4 py-8 text-center text-gray-500 dark:text-gray-400">
                    <Clock className="w-8 h-8 mx-auto mb-2 opacity-50" />
                    <p>No steps yet</p>
                  </div>
                ) : (
                  renderStepTree(steps)
                )}
              </div>
            </div>
          )}
        </div>
      </div>

      {/* Selected Step Detail Panel */}
      {selectedStep && (
        <div 
          className="fixed w-96 bg-white dark:bg-gray-800 rounded-xl shadow-2xl border dark:border-gray-700 z-[60] max-h-[calc(100vh-180px)] overflow-hidden"
          style={{
            left: `${detailPanelPosition.x}px`,
            top: `${detailPanelPosition.y}px`,
          }}
        >
          <div
          className="px-4 py-3 border-b dark:border-gray-700 flex items-center justify-between bg-gray-50 dark:bg-gray-900 cursor-move select-none relative"
          onMouseDown={startDetailDrag}
          >
            {/* 复制成功提示 */}
            {copiedField && (
              <div className="absolute left-1/2 top-full transform -translate-x-1/2 mt-1 px-2 py-1 bg-black text-white text-xs rounded shadow-lg z-50">
                {t('execution.copied')}
              </div>
            )}
            <h3 className="font-semibold text-gray-900 dark:text-white truncate">{selectedStep.name}</h3>
            <button
              onClick={() => setSelectedStep(null)}
              className="p-1 hover:bg-gray-200 dark:hover:bg-gray-700 rounded transition-colors"
            >
              <X className="w-4 h-4 text-gray-500" />
            </button>
          </div>
          <div className="p-4 overflow-y-auto max-h-[calc(100vh-240px)]">
            <div className="space-y-3">
              <div className="flex items-center gap-2">
                {getStatusIcon(selectedStep.status)}
                <span className={`text-sm font-medium ${
                  selectedStep.status === 'success' ? 'text-green-600 dark:text-green-400' :
                  selectedStep.status === 'running' ? 'text-blue-600 dark:text-blue-400' :
                  selectedStep.status === 'failed' ? 'text-red-600 dark:text-red-400' :
                  'text-gray-600 dark:text-gray-400'
                }`}>
                  {t(`execution.${selectedStep.status}`)}
                </span>
              </div>
              
              {selectedStep.action && (
                <div>
                  <p className="text-xs text-gray-500 dark:text-gray-400 uppercase tracking-wide">{t('execution.action')}</p>
                  <p className="text-sm text-gray-900 dark:text-white font-mono">{selectedStep.action}</p>
                </div>
              )}
              
              {selectedStep.description && (
                <div>
                  <p className="text-xs text-gray-500 dark:text-gray-400 uppercase tracking-wide">{t('execution.description')}</p>
                  <p className="text-sm text-gray-900 dark:text-white">{selectedStep.description}</p>
                </div>
              )}
              
              {selectedStep.startTime && (
                <div>
                  <p className="text-xs text-gray-500 dark:text-gray-400 uppercase tracking-wide">{t('execution.startTime')}</p>
                  <p className="text-sm text-gray-900 dark:text-white">{selectedStep.startTime}</p>
                </div>
              )}
              
              {selectedStep.endTime && (
                <div>
                  <p className="text-xs text-gray-500 dark:text-gray-400 uppercase tracking-wide">{t('execution.endTime')}</p>
                  <p className="text-sm text-gray-900 dark:text-white">{selectedStep.endTime}</p>
                </div>
              )}
              
              {selectedStep.duration && (
                <div>
                  <p className="text-xs text-gray-500 dark:text-gray-400 uppercase tracking-wide">{t('execution.duration')}</p>
                  <p className="text-sm text-gray-900 dark:text-white">{selectedStep.duration}</p>
                </div>
              )}
              
              {/* Sleep 参数 */}
              {selectedStep.action === 'sleep' && selectedStep.sleepDuration && (
                <div>
                  <p className="text-xs text-gray-500 dark:text-gray-400 uppercase tracking-wide">{t('execution.sleepDuration')}</p>
                  <p className="text-sm text-purple-600 dark:text-purple-400 font-mono">{selectedStep.sleepDuration}</p>
                </div>
              )}
              
              {/* Shell 命令 */}
              {selectedStep.action === 'shell' && selectedStep.shellCommand && (
                <div>
                  <div className="flex items-center justify-between mb-1">
                    <p className="text-xs text-gray-500 dark:text-gray-400 uppercase tracking-wide">{t('execution.shellCommand')}</p>
                    <button
                      onClick={() => copyToClipboard(selectedStep.shellCommand!, 'shellCommand')}
                      className={`p-1 rounded transition-colors ${
                        copiedField === 'shellCommand'
                          ? 'bg-green-500 text-white'
                          : 'text-gray-400 hover:text-gray-600 hover:bg-gray-200 dark:hover:bg-gray-700'
                      }`}
                      title={t('execution.copy')}
                    >
                      <Copy className="w-3 h-3" />
                    </button>
                  </div>
                  <pre className="text-sm text-green-700 dark:text-green-400 bg-gray-50 dark:bg-gray-900 p-3 rounded-lg overflow-x-auto font-mono whitespace-pre-wrap">{selectedStep.shellCommand}</pre>
                </div>
              )}
              
              {/* HTTP 参数 */}
              {selectedStep.action === 'http' && (selectedStep.httpUrl || selectedStep.httpMethod) && (
                <div>
                  <p className="text-xs text-gray-500 dark:text-gray-400 uppercase tracking-wide">{t('execution.httpParams')}</p>
                  <div className="text-sm text-blue-600 dark:text-blue-400 font-mono space-y-1">
                    {selectedStep.httpMethod && <p>{selectedStep.httpMethod}</p>}
                    {selectedStep.httpUrl && <p className="truncate">{selectedStep.httpUrl}</p>}
                  </div>
                </div>
              )}
              
              {/* Log 消息 */}
              {selectedStep.action === 'log' && selectedStep.logMessage && (
                <div>
                  <div className="flex items-center justify-between mb-1">
                    <p className="text-xs text-gray-500 dark:text-gray-400 uppercase tracking-wide">{t('execution.logMessage')}</p>
                    <button
                      onClick={() => copyToClipboard(selectedStep.logMessage!, 'logMessage')}
                      className={`p-1 rounded transition-colors ${
                        copiedField === 'logMessage'
                          ? 'bg-green-500 text-white'
                          : 'text-gray-400 hover:text-gray-600 hover:bg-gray-200 dark:hover:bg-gray-700'
                      }`}
                      title={t('execution.copy')}
                    >
                      <Copy className="w-3 h-3" />
                    </button>
                  </div>
                  <pre className="text-sm text-gray-900 dark:text-white bg-gray-50 dark:bg-gray-900 p-3 rounded-lg overflow-x-auto font-mono whitespace-pre-wrap">{selectedStep.logMessage}</pre>
                </div>
              )}
              
              {/* Condition 表达式 */}
              {selectedStep.action === 'condition' && selectedStep.expression && (
                <div>
                  <p className="text-xs text-gray-500 dark:text-gray-400 uppercase tracking-wide">{t('execution.expression')}</p>
                  <p className="text-sm text-orange-600 dark:text-orange-400 font-mono">{selectedStep.expression}</p>
                </div>
              )}
              
              {/* Condition 结果 */}
              {selectedStep.action === 'condition' && selectedStep.condition_result !== undefined && selectedStep.condition_result !== null && (
                <div>
                  <p className="text-xs text-gray-500 dark:text-gray-400 uppercase tracking-wide">{t('execution.conditionResult')}</p>
                  <p className="text-sm font-mono">
                    {selectedStep.condition_result === true ? (
                      <span className="text-green-600 dark:text-green-400">✓ true</span>
                    ) : (
                      <span className="text-gray-500 dark:text-gray-400">✗ false</span>
                    )}
                  </p>
                </div>
              )}
              
              {selectedStep.aiOutput && (
                <div>
                  <div className="flex items-center justify-between mb-1">
                    <p className="text-xs text-gray-500 dark:text-gray-400 uppercase tracking-wide">
                      AI Output
                      {selectedStep.status === 'running' && (
                        <span className="ml-1 animate-pulse">●</span>
                      )}
                    </p>
                  </div>
                  <div className="text-sm text-gray-900 dark:text-white bg-gray-50 dark:bg-gray-900 p-3 rounded-lg">
                    <MarkdownView content={selectedStep.aiOutput} />
                  </div>
                </div>
              )}

              {selectedStep.output && (
                <div>
                  <div className="flex items-center justify-between mb-1">
                    <p className="text-xs text-gray-500 dark:text-gray-400 uppercase tracking-wide">{t('execution.output')}</p>
                    <button
                      onClick={() => copyToClipboard(selectedStep.output!, 'output')}
                      className={`p-1 rounded transition-colors ${
                        copiedField === 'output'
                          ? 'bg-green-500 text-white'
                          : 'text-gray-400 hover:text-gray-600 hover:bg-gray-200 dark:hover:bg-gray-700'
                      }`}
                      title={t('execution.copy')}
                    >
                      <Copy className="w-3 h-3" />
                    </button>
                  </div>
                  <pre className="text-sm text-gray-900 dark:text-white bg-gray-50 dark:bg-gray-900 p-3 rounded-lg overflow-x-auto font-mono whitespace-pre-wrap">
                    <AnsiText text={selectedStep.output} />
                  </pre>
                </div>
              )}
              
              {selectedStep.error && (
                <div>
                  <div className="flex items-center justify-between mb-1">
                    <p className="text-xs text-gray-500 dark:text-gray-400 uppercase tracking-wide">{t('execution.error')}</p>
                    <button
                      onClick={() => copyToClipboard(selectedStep.error!, 'error')}
                      className={`p-1 rounded transition-colors ${
                        copiedField === 'error'
                          ? 'bg-green-500 text-white'
                          : 'text-gray-400 hover:text-gray-600 hover:bg-gray-200 dark:hover:bg-gray-700'
                      }`}
                      title={t('execution.copy')}
                    >
                      <Copy className="w-3 h-3" />
                    </button>
                  </div>
                  <pre className="text-sm text-red-600 dark:text-red-400 bg-red-50 dark:bg-red-900/20 p-3 rounded-lg overflow-x-auto font-mono whitespace-pre-wrap">
                    {selectedStep.error}
                  </pre>
                </div>
              )}
              
              {/* Parallel/Foreach 子任务 */}
              {selectedStep.children && selectedStep.children.length > 0 && (
                <div>
                  <p className="text-xs text-gray-500 dark:text-gray-400 uppercase tracking-wide mb-2">
                    {selectedStep.action === 'parallel' ? t('execution.parallelTasks') : 
                     (selectedStep.action === 'foreach' || selectedStep.action === 'loop') ? t('execution.loopIterations') : 
                     t('execution.subSteps')}
                  </p>
                  <div className="space-y-2">
                    {selectedStep.children.map((child, idx) => (
                      <div 
                        key={child.id || idx}
                        className="bg-gray-50 dark:bg-gray-900 rounded text-sm"
                      >
                        <div className="flex items-center gap-2 p-2">
                          {getStatusIcon(child.status)}
                          <span className="font-medium text-gray-900 dark:text-white truncate flex-1">{child.name}</span>
                          {child.duration && (
                            <span className="text-xs text-gray-500 dark:text-gray-400 font-mono">{child.duration}</span>
                          )}
                        </div>
                        {/* 子任务输出 */}
                        {child.output && (
                          <div className="px-2 pb-2 pt-0">
                            <pre className="text-xs text-gray-700 dark:text-gray-300 bg-gray-100 dark:bg-gray-800 p-2 rounded overflow-x-auto font-mono whitespace-pre-wrap max-h-32 overflow-y-auto">
                              <AnsiText text={child.output} />
                            </pre>
                          </div>
                        )}
                        {/* 子任务错误 */}
                        {child.error && (
                          <div className="px-2 pb-2 pt-0">
                            <pre className="text-xs text-red-600 dark:text-red-400 bg-red-50 dark:bg-red-900/20 p-2 rounded overflow-x-auto font-mono whitespace-pre-wrap">
                              {child.error}
                            </pre>
                          </div>
                        )}
                      </div>
                    ))}
                  </div>
                </div>
              )}
              
              {/* Condition 分支 */}
              {(selectedStep.then_children && selectedStep.then_children.length > 0) || (selectedStep.else_children && selectedStep.else_children.length > 0) ? (
                <div>
                  <p className="text-xs text-gray-500 dark:text-gray-400 uppercase tracking-wide mb-2">{t('execution.branches')}</p>
                  <div className="space-y-2">
                    {selectedStep.then_children && selectedStep.then_children.length > 0 && (
                      <div>
                        <p className="text-xs font-medium text-green-600 dark:text-green-400 mb-1">
                          ✓ {t('execution.thenBranch')} {selectedStep.condition_result === true ? '(执行)' : '(跳过)'}
                        </p>
                        <div className="space-y-1 pl-2">
                          {selectedStep.then_children.map((child, idx) => (
                            <div 
                              key={child.id || idx}
                              className="flex items-center gap-2 p-1.5 bg-gray-50 dark:bg-gray-900 rounded text-xs"
                            >
                              {getStatusIcon(child.status)}
                              <span className="text-gray-900 dark:text-white truncate">{child.name}</span>
                            </div>
                          ))}
                        </div>
                      </div>
                    )}
                    {selectedStep.else_children && selectedStep.else_children.length > 0 && (
                      <div>
                        <p className="text-xs font-medium text-gray-500 dark:text-gray-400 mb-1">
                          ✗ {t('execution.elseBranch')} {selectedStep.condition_result === false ? '(执行)' : '(跳过)'}
                        </p>
                        <div className="space-y-1 pl-2">
                          {selectedStep.else_children.map((child, idx) => (
                            <div 
                              key={child.id || idx}
                              className="flex items-center gap-2 p-1.5 bg-gray-50 dark:bg-gray-900 rounded text-xs"
                            >
                              {getStatusIcon(child.status)}
                              <span className="text-gray-900 dark:text-white truncate">{child.name}</span>
                            </div>
                          ))}
                        </div>
                      </div>
                    )}
                  </div>
                </div>
              ) : null}
            </div>
          </div>
        </div>
      )}

      {/* Log Panel - with three layouts */}
      {showLogs && (
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
                onClick={() => setShowLogs(false)}
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
      )}

      {/* Floating Navigation Bar - Draggable */}
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
        {collapsibleNodes.length > 0 && (
          <button
            onClick={(e) => {
              e.stopPropagation()
              if (isAllCollapsed) {
                handleExpandAll()
              } else {
                handleCollapseAll()
              }
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
            setShowMiniMap(!showMiniMap)
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
            setShowLogs(!showLogs)
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
    </div>
  )
}

// MiniMap component for Execution page

