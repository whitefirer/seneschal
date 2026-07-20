import { useState, useEffect, useRef, useCallback, useMemo } from 'react'
import { useParams } from 'react-router-dom'
import { useWebSocket, type ProgressEvent } from '@/hooks/useWebSocket'
import { executionsApi } from '@/api/client'
import { ExecutionChatPanel } from './ExecutionChatPanel'
import { WorkflowGraph, workflowToFlowSteps, type FlowStep } from '@/components/WorkflowGraph'
import type { RawExecutionStep, Step } from '@/types/execution'
import {
  appendStepAiToken,
  appendStepOutput,
  findStepInTree,
  updateStepInTree,
} from './execution/stepUtils'
import { ExecutionHeader } from './execution/ExecutionHeader'
import { StepListView } from './execution/StepListView'
import { StepDetailPanel } from './execution/StepDetailPanel'
import { LogPanel, type LogLayout } from './execution/LogPanel'
import { FloatingToolbar } from './execution/FloatingToolbar'

export default function Execution() {
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

  // 折叠节点状态
  const [collapsedNodes, setCollapsedNodes] = useState<Set<string>>(new Set())

  const [logLayout, setLogLayout] = useState<LogLayout>('float')
  const [detailPanelPosition, setDetailPanelPosition] = useState({ x: 0, y: 0 })
  const stepsRef = useRef<Step[]>([])

  // Handle progress events
  const handleProgress = useCallback((event: ProgressEvent) => {
    if (!id || event.executionId !== id) return

    if (event.workflowName && !workflowName) {
      setWorkflowName(event.workflowName)
    }
    if (event.workflowFile && !workflowFile) {
      setWorkflowFile(event.workflowFile)
    }

    // 获取实际的 stepId 和 name
    const actualStepId = event.stepId || ''
    const actualName = event.stepName || event.name || ''
    const actualTimestamp = event.timestamp || event.time || ''
    const actualConditionResult = event.conditionResult

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
        setSteps((prev) => appendStepOutput(prev, actualStepId, actualName, event.output || ''))
        break

      case 'ai_token': {
        // Incremental AI token: append to the step's aiOutput (streaming text).
        // Unlike step_output, no trailing newline — these are partial tokens.
        const targetId = actualStepId || actualName || ''
        setSteps((prev) => appendStepAiToken(prev, targetId, actualName, event.output || ''))
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
  }, [id, workflowName])

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

  async function loadExecution(execId: string) {
    try {
      const data = await executionsApi.get(execId)
      setWorkflowName(data.workflowName)
      if (data.workflowFile) {
        setWorkflowFile(data.workflowFile)
      }
      setStatus(data.status as 'running' | 'success' | 'failed')

      // Convert steps with children structure (preserve tree)
      const convertSteps = (steps: RawExecutionStep[], parentId?: string, childIndex = 0): Step[] => {
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
            status: (s.status as Step['status']) || 'pending',
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
        setSteps(newSteps)
        stepsRef.current = newSteps
      }

      if (data.logs) {
        setLogs(data.logs.map((l) => `[${l.timestamp}] ${l.level.toUpperCase()}: ${l.message}`))
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

  return (
    <div className="flex flex-col h-[calc(100vh-3.5rem)] bg-gray-50 dark:bg-gray-900 overflow-hidden">
      {/* Header */}
      <ExecutionHeader
        workflowName={workflowName}
        workflowFile={workflowFile}
        executionId={id}
        status={status}
        connected={connected}
        viewMode={viewMode}
        onToggleViewMode={() => setViewMode(viewMode === 'graph' ? 'list' : 'graph')}
        onRefresh={() => id && loadExecution(id)}
      />

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
                const found = findStepInTree(steps, step.id)
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
            <StepListView
              steps={steps}
              selectedStep={selectedStep}
              onSelectStep={setSelectedStep}
            />
          )}
        </div>
      </div>

      {/* Selected Step Detail Panel */}
      {selectedStep && (
        <StepDetailPanel
          step={selectedStep}
          position={detailPanelPosition}
          onPositionChange={setDetailPanelPosition}
          onClose={() => setSelectedStep(null)}
        />
      )}

      {/* Log Panel - with three layouts */}
      {showLogs && (
        <LogPanel
          logs={logs}
          layout={logLayout}
          onLayoutChange={setLogLayout}
          onClose={() => setShowLogs(false)}
        />
      )}

      {/* Floating Navigation Bar - Draggable */}
      <FloatingToolbar
        showCollapseToggle={collapsibleNodes.length > 0}
        isAllCollapsed={isAllCollapsed}
        onToggleCollapseAll={() => (isAllCollapsed ? handleExpandAll() : handleCollapseAll())}
        showMiniMap={showMiniMap}
        onToggleMiniMap={() => setShowMiniMap(!showMiniMap)}
        showLogs={showLogs}
        onToggleLogs={() => setShowLogs(!showLogs)}
      />

      {/* Floating AI assistant for this execution */}
      {id && <ExecutionChatPanel executionId={id} />}
    </div>
  )
}
