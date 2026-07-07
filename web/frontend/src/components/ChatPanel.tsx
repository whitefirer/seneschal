import { useState, useRef, useEffect, lazy, Suspense } from 'react'
import { useNavigate } from 'react-router-dom'
import { Send, Bot, User, Loader2, Play, Sparkles, GitBranch, ChevronDown, ChevronRight, ExternalLink, X, CheckCircle, XCircle, Clock, RotateCw, FileCode } from 'lucide-react'
import { chatApi, workflowsApi, executionsApi, type ChatSelection, type ChatStep } from '@/api/client'
import { MarkdownView } from './MarkdownView'

const Mermaid = lazy(() => import('./Mermaid'))

interface ExecState {
  id: string
  status: string
  steps?: any[]
}

interface ToolStep {
  tool: string
  input?: string
  output?: string
  selection?: ChatSelection
  yaml?: string
}

interface Message {
  role: 'user' | 'assistant'
  content: string
  thinking?: boolean
  selection?: ChatSelection
  toolSteps?: ToolStep[]   // agent tool use process
}

const STORAGE_KEY = 'goworkflow-chat-messages'

export default function ChatPanel() {
  const navigate = useNavigate()
  const [messages, setMessages] = useState<Message[]>(() => {
    // Restore from localStorage on mount; fall back to the welcome message.
    try {
      const saved = localStorage.getItem(STORAGE_KEY)
      if (saved) {
        const parsed = JSON.parse(saved)
        if (Array.isArray(parsed) && parsed.length > 0) return parsed
      }
    } catch { /* ignore corrupt storage */ }
    return [{
      role: 'assistant' as const,
      content: '你好！描述你想做什么，我会帮你找到合适的工作流并执行。比如："部署到 staging 环境"或"运行测试"。',
    }]
  })

  // Persist messages to localStorage so the conversation survives refresh.
  useEffect(() => {
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(messages))
    } catch { /* storage full or unavailable — non-fatal */ }
  }, [messages])
  const [input, setInput] = useState('')
  const [loading, setLoading] = useState(false)
  const abortRef = useRef<AbortController | null>(null)
  const scrollRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    scrollRef.current?.scrollTo({ top: scrollRef.current.scrollHeight, behavior: 'smooth' })
  }, [messages])

  const send = async () => {
    const msg = input.trim()
    if (!msg || loading) return
    setInput('')

    // Build conversation history from existing messages (excluding the
    // placeholder we're about to add). Only include messages with content.
    const history = messages
      .filter(m => m.content && m.content.trim())
      .map(m => ({ role: m.role, content: m.content }))

    setMessages((prev) => [...prev, { role: 'user', content: msg }])
    setLoading(true)
    setMessages((prev) => [...prev, { role: 'assistant', content: '', thinking: true }])

    const ac = new AbortController()
    abortRef.current = ac

    try {
      await chatApi.sendMessage(msg, (event) => {
        const etype = event.type
        if (etype === 'thinking') {
          setMessages((prev) => {
            const next = [...prev]
            next[next.length - 1] = { role: 'assistant', content: '', thinking: true, toolSteps: [] }
            return next
          })
        } else if (etype === 'tool_call') {
          // AI is calling a tool — add a step to the current message.
          setMessages((prev) => {
            const next = [...prev]
            const last = next[next.length - 1]
            if (!last.toolSteps) last.toolSteps = []
            last.toolSteps.push({ tool: event.tool, input: event.input })
            last.thinking = false
            next[next.length - 1] = { ...last }
            return next
          })
        } else if (etype === 'tool_result') {
          // Tool finished — update the last step with output.
          setMessages((prev) => {
            const next = [...prev]
            const last = next[next.length - 1]
            if (last.toolSteps && last.toolSteps.length > 0) {
              const step = last.toolSteps[last.toolSteps.length - 1]
              step.output = event.output
              // Rich data: selection (for select_workflow) or yaml (for generate)
              if (event.selection) {
                step.selection = event.selection as unknown as ChatSelection
                last.selection = step.selection
              }
              if (event.yaml) step.yaml = event.yaml
            }
            // run_workflow returns executionId — set on message for ExecProgress
            if (event.executionId) {
              ;(last as any).executionId = event.executionId
            }
            next[next.length - 1] = { ...last }
            return next
          })
        } else if (etype === 'text') {
          // Final text response from AI.
          setMessages((prev) => {
            const next = [...prev]
            next[next.length - 1] = { ...next[next.length - 1], content: event.content || '', thinking: false }
            return next
          })
        } else if (etype === 'error') {
          setMessages((prev) => {
            const next = [...prev]
            next[next.length - 1] = { role: 'assistant', content: `出错了：${event.error}`, thinking: false }
            return next
          })
        }
      }, ac.signal, history)
    } catch (err: any) {
      if (err.name !== 'AbortError') {
        setMessages((prev) => {
          const next = [...prev]
          next[next.length - 1] = { role: 'assistant', content: `请求失败：${err.message}` }
          return next
        })
      }
    } finally {
      setLoading(false)
      abortRef.current = null
    }
  }

  const runSelection = async (msgIndex: number, sel: ChatSelection) => {
    setLoading(true)
    try {
      const runName = sel.fileName || sel.workflow
      const res = await workflowsApi.run(runName, { variables: sel.variables })
      // Store the executionId on the message so the card can poll progress.
      // We don't navigate away — the card shows inline progress.
      setMessages((prev) => {
        const next = [...prev]
        if (next[msgIndex]?.selection) {
          ;(next[msgIndex].selection as any).executionId = res.executionId
        }
        return [...next] // trigger re-render
      })
    } catch (err: any) {
      setMessages((prev) => [
        ...prev,
        { role: 'assistant', content: `执行失败：${err.message}` },
      ])
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="flex flex-col h-full max-w-3xl mx-auto w-full">
      {/* Messages */}
      <div ref={scrollRef} className="flex-1 overflow-y-auto p-4 space-y-4">
        {messages.map((m, i) => (
          <div key={i} className={`flex gap-3 ${m.role === 'user' ? 'justify-end' : ''}`}>
            {m.role === 'assistant' && (
              <div className="flex-shrink-0 w-8 h-8 rounded-full bg-primary/10 flex items-center justify-center">
                {m.thinking ? <Loader2 className="w-4 h-4 animate-spin text-primary" /> : <Bot className="w-4 h-4 text-primary" />}
              </div>
            )}
            <div className={`max-w-[85%] rounded-2xl px-4 py-2.5 ${m.role === 'user' ? 'bg-primary text-primary-foreground' : 'bg-muted text-foreground'}`}>
              {/* Tool use steps (agent process) */}
              {m.toolSteps && m.toolSteps.length > 0 && (
                <div className="mb-2 space-y-1">
                  {m.toolSteps.map((ts, j) => (
                    <ToolStepView key={j} step={ts} />
                  ))}
                </div>
              )}
              {m.content && <MarkdownView content={m.content} />}
              {m.thinking && !m.content && (!m.toolSteps || m.toolSteps.length === 0) && (
                <span className="text-sm text-muted-foreground animate-pulse">思考中…</span>
              )}
              {m.selection && m.selection.workflow && !(m as any).executionId && (
                <SelectionCard
                  selection={m.selection}
                  onRun={() => runSelection(i, m.selection!)}
                  loading={loading}
                  onNavigate={(id) => navigate(`/execution/${id}`)}
                />
              )}
              {(m as any).executionId && (
                <ExecProgress
                  execId={(m as any).executionId}
                  onNavigate={(id) => navigate(`/execution/${id}`)}
                  onRerun={() => runSelection(i, m.selection!)}
                />
              )}
            </div>
            {m.role === 'user' && (
              <div className="flex-shrink-0 w-8 h-8 rounded-full bg-muted flex items-center justify-center">
                <User className="w-4 h-4 text-muted-foreground" />
              </div>
            )}
          </div>
        ))}
      </div>

      {/* Input */}
      <div className="border-t border-border p-4">
        <div className="flex gap-2 items-end">
          <textarea
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={(e) => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); send() } }}
            placeholder="描述你想做什么…"
            rows={1}
            className="flex-1 resize-none rounded-xl border border-border bg-background px-4 py-2.5 text-sm focus:outline-none focus:ring-2 focus:ring-primary max-h-32"
            disabled={loading}
          />
          <button onClick={send} disabled={!input.trim() || loading}
            className="flex-shrink-0 w-10 h-10 rounded-xl bg-primary text-primary-foreground flex items-center justify-center hover:bg-primary/90 disabled:opacity-50">
            <Send className="w-4 h-4" />
          </button>
        </div>
        <p className="text-xs text-muted-foreground mt-1.5">Enter 发送，Shift+Enter 换行</p>
      </div>
    </div>
  )
}

// ── Tool step view (shows agent's tool call + result inline) ─────────────────

const TOOL_LABELS: Record<string, string> = {
  list_workflows: '📋 列出工作流',
  select_workflow: '🔍 选择工作流',
  generate_workflow: '✨ 生成工作流',
  modify_workflow: '✏️ 修改工作流',
  explain_workflow: '📖 解释工作流',
  validate_workflow: '✅ 校验工作流',
  run_workflow: '🚀 执行工作流',
}

function ToolStepView({ step }: { step: ToolStep }) {
  const [expanded, setExpanded] = useState(false)
  const hasOutput = step.output && step.output !== 'generate: model returned empty YAML'
  const label = TOOL_LABELS[step.tool] || step.tool

  return (
    <div className="text-xs border border-border/50 rounded px-2 py-1 bg-background/50">
      <div className="flex items-center gap-1.5">
        {hasOutput ? <CheckCircle className="w-3 h-3 text-green-500 flex-shrink-0" />
         : <Loader2 className="w-3 h-3 animate-spin text-primary flex-shrink-0" />}
        <span className="font-medium">{label}</span>
        {hasOutput && (
          <button onClick={() => setExpanded(!expanded)} className="text-muted-foreground hover:text-foreground ml-auto text-[10px]">
            {expanded ? '收起' : '详情'}
          </button>
        )}
      </div>
      {expanded && hasOutput && (
        <pre className="mt-1 p-1.5 rounded bg-muted text-foreground whitespace-pre-wrap break-all max-h-32 overflow-y-auto text-[10px]">
          {step.yaml && step.tool === 'generate_workflow' ? step.yaml : step.output}
        </pre>
      )}
    </div>
  )
}

// ── Selection card: collapsed by default, expandable, inline exec progress ──

function SelectionCard({ selection, onRun, loading, onNavigate }: {
  selection: ChatSelection
  onRun: () => void
  loading: boolean
  onNavigate: (id: string) => void
}) {
  const [expanded, setExpanded] = useState(false)
  const execId = (selection as any).executionId as string | undefined
  // Lightweight status poll for the collapsed view (only when collapsed +
  // has execId). When expanded, ExecProgress does its own 2s polling.
  const [collapsedStatus, setCollapsedStatus] = useState<string>('')

  useEffect(() => {
    if (!execId || expanded) return
    let active = true
    let timer: ReturnType<typeof setTimeout>
    const poll = async () => {
      try {
        const data = await executionsApi.get(execId)
        if (!active) return
        setCollapsedStatus(data.status)
        if (data.status === 'running') timer = setTimeout(poll, 3000)
      } catch { if (active) timer = setTimeout(poll, 3000) }
    }
    poll()
    return () => { active = false; clearTimeout(timer) }
  }, [execId, expanded])

  // Collapsed view: name + status icon + action button.
  // When no execution yet: show Run. When running: spinner. When done: result + rerun.
  if (!expanded) {
    const statusEl = collapsedStatus === 'running' ? <Loader2 className="w-3.5 h-3.5 animate-spin text-primary" />
      : collapsedStatus === 'success' ? <CheckCircle className="w-3.5 h-3.5 text-green-500" />
      : collapsedStatus === 'failed' ? <XCircle className="w-3.5 h-3.5 text-red-500" />
      : null

    return (
      <div className="mt-3 border border-border rounded-lg p-3 bg-background">
        <div className="flex items-center gap-2">
          <button onClick={() => setExpanded(true)} className="flex items-center gap-1 flex-1 text-left min-w-0">
            <ChevronRight className="w-4 h-4 text-muted-foreground flex-shrink-0" />
            <Sparkles className="w-4 h-4 text-primary flex-shrink-0" />
            <span className="font-semibold text-sm truncate">{selection.workflow}</span>
            {statusEl && <span className="flex-shrink-0">{statusEl}</span>}
            {collapsedStatus === 'running' && <span className="text-xs text-muted-foreground">执行中…</span>}
          </button>
          {/* Action button depends on state */}
          {!execId ? (
            <button onClick={onRun} disabled={loading}
              className="flex items-center gap-1 px-3 py-1 rounded-lg bg-primary text-primary-foreground text-xs font-medium hover:bg-primary/90 disabled:opacity-50">
              <Play className="w-3 h-3" /> 执行
            </button>
          ) : collapsedStatus !== 'running' ? (
            <button onClick={onRun} disabled={loading}
              className="flex items-center gap-1 px-2 py-1 rounded-lg border border-border text-muted-foreground text-xs hover:bg-muted">
              <RotateCw className="w-3 h-3" /> 重跑
            </button>
          ) : null}
        </div>
      </div>
    )
  }

  // Expanded view OR execution in progress.
  return (
    <div className="mt-3 border border-border rounded-lg p-3 bg-background space-y-2">
      {/* Header with collapse toggle */}
      <div className="flex items-center gap-2">
        <button onClick={() => setExpanded(!expanded)} className="flex items-center gap-1">
          {expanded ? <ChevronDown className="w-4 h-4 text-muted-foreground" /> : <ChevronRight className="w-4 h-4 text-muted-foreground" />}
          <Sparkles className="w-4 h-4 text-primary" />
          <span className="font-semibold text-sm">{selection.workflow}</span>
        </button>
        <span className="text-xs text-muted-foreground">置信度 {(selection.confidence * 100).toFixed(0)}%</span>
      </div>

      {/* Step preview + mermaid toggle — hidden once execution starts;
          the ExecProgress section shows live step status instead. */}
      {expanded && !execId && selection.steps && selection.steps.length > 0 && (
        <StepPreviewSection steps={selection.steps} />
      )}

      {/* Variables — also hidden after execution to reduce noise. */}
      {expanded && !execId && Object.keys(selection.variables).length > 0 && (
        <div>
          <p className="text-xs text-muted-foreground mb-1">变量:</p>
          <div className="space-y-0.5">
            {Object.entries(selection.variables).map(([k, v]) => (
              <div key={k} className="text-xs font-mono">
                <span className="text-muted-foreground">{k}</span> = <span>{v}</span>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* View YAML link (before execution) */}
      {expanded && !execId && (selection.fileName || selection.workflow) && (
        <a
          href={`/editor/${selection.fileName || selection.workflow}`}
          className="flex items-center gap-1 text-xs text-primary hover:underline"
        >
          <FileCode className="w-3 h-3" /> 查看 YAML
        </a>
      )}

      {/* Run button or inline execution progress */}
      {execId ? (
        <ExecProgress execId={execId} onNavigate={onNavigate} onRerun={onRun} />
      ) : (
        <button onClick={onRun} disabled={loading}
          className="w-full flex items-center justify-center gap-2 px-3 py-1.5 rounded-lg bg-primary text-primary-foreground text-sm font-medium hover:bg-primary/90 disabled:opacity-50">
          <Play className="w-3.5 h-3.5" /> 执行工作流
        </button>
      )}
    </div>
  )
}

// ── Step preview: text tree + mermaid modal ──────────────────────────────────

function StepPreviewSection({ steps }: { steps: ChatStep[] }) {
  const [mermaidModal, setMermaidModal] = useState(false)

  return (
    <div>
      <div className="flex items-center justify-between mb-1">
        <p className="text-xs text-muted-foreground">步骤预览:</p>
        <button onClick={() => setMermaidModal(true)}
          className="flex items-center gap-1 text-xs px-2 py-0.5 rounded border border-border hover:bg-muted text-muted-foreground">
          <GitBranch className="w-3 h-3" /> 结构图
        </button>
      </div>
      <StepTreeText steps={steps} />

      {/* Mermaid modal (large graph) */}
      {mermaidModal && (
        <div className="fixed inset-0 z-50 bg-black/50 flex items-center justify-center p-8" onClick={() => setMermaidModal(false)}>
          <div className="bg-background rounded-xl border border-border max-w-5xl max-h-[85vh] overflow-auto p-6 relative" onClick={(e) => e.stopPropagation()}>
            <button onClick={() => setMermaidModal(false)}
              className="absolute top-3 right-3 p-1 rounded hover:bg-muted">
              <X className="w-5 h-5 text-muted-foreground" />
            </button>
            <h3 className="text-sm font-semibold mb-4 text-muted-foreground">工作流结构图</h3>
            <Suspense fallback={<div className="text-sm text-muted-foreground py-8 text-center">加载图表…</div>}>
              <Mermaid chart={stepsToMermaid(steps)} />
            </Suspense>
          </div>
        </div>
      )}
    </div>
  )
}

// ── Inline execution progress (polls /api/executions/{id}) ──────────────────

function ExecProgress({ execId, onNavigate, onRerun }: { execId: string; onNavigate: (id: string) => void; onRerun: () => void }) {
  const [exec, setExec] = useState<ExecState | null>(null)
  // Steps section: expanded while running, auto-collapse when done.
  const [stepsCollapsed, setStepsCollapsed] = useState(false)
  // Output section: collapsed while running, auto-expand when done.
  const [outputCollapsed, setOutputCollapsed] = useState(true)
  const prevStatusRef = useRef<string | undefined>()

  useEffect(() => {
    let active = true
    let timer: ReturnType<typeof setTimeout>

    const poll = async () => {
      try {
        const data = await executionsApi.get(execId)
        if (!active) return
        setExec({ id: execId, status: data.status, steps: data.steps })
        // Determine if we should switch to the "done" layout.
        // This fires on running→done transition AND on first-load-if-already-done
        // (e.g. page refresh when execution already completed).
        const wasFirstLoad = prevStatusRef.current === undefined
        const transitionedFromRunning = prevStatusRef.current === 'running'
        if (data.status !== 'running' && (transitionedFromRunning || wasFirstLoad)) {
          setStepsCollapsed(true)
          setOutputCollapsed(false)
        }
        prevStatusRef.current = data.status
        if (data.status === 'running') {
          timer = setTimeout(poll, 2000)
        }
      } catch {
        if (active) timer = setTimeout(poll, 2000)
      }
    }
    poll()
    return () => { active = false; clearTimeout(timer) }
  }, [execId])

  const isDone = exec && exec.status !== 'running' && exec.status !== ''
  const statusIcon = exec?.status === 'success' ? <CheckCircle className="w-4 h-4 text-green-500" />
    : exec?.status === 'failed' ? <XCircle className="w-4 h-4 text-red-500" />
    : <Loader2 className="w-4 h-4 animate-spin text-primary" />

  const outputSteps = exec?.steps ? collectOutputs(exec.steps) : []

  return (
    <div className="border-t border-border pt-2 mt-2 space-y-1">
      {/* Status header */}
      <div className="flex items-center gap-2">
        {statusIcon}
        <span className="text-xs font-medium">
          {exec?.status === 'running' ? '执行中…' : exec?.status === 'success' ? '执行完成' : exec?.status === 'failed' ? '执行失败' : '准备中…'}
        </span>
        {isDone && (
          <button onClick={onRerun} className="flex items-center gap-1 text-xs px-2 py-0.5 rounded border border-border hover:bg-muted text-muted-foreground">
            <RotateCw className="w-3 h-3" /> 重跑
          </button>
        )}
        <button onClick={() => onNavigate(execId)}
          className="ml-auto flex items-center gap-1 text-xs text-primary hover:underline">
          查看完整详情 <ExternalLink className="w-3 h-3" />
        </button>
      </div>

      {/* Steps section first (auto-collapse when done) */}
      <div className="ml-1">
        <button onClick={() => setStepsCollapsed(!stepsCollapsed)}
          className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground">
          {stepsCollapsed ? <ChevronRight className="w-3 h-3" /> : <ChevronDown className="w-3 h-3" />}
          执行步骤
        </button>
        {!stepsCollapsed && exec?.steps && (
          <div className="ml-4 mt-0.5">
            <ExecStepTree steps={exec.steps} depth={0} />
          </div>
        )}
      </div>

      {/* Output results section second (auto-expand when done) */}
      {isDone && outputSteps.length > 0 && (
        <div className="ml-1">
          <button onClick={() => setOutputCollapsed(!outputCollapsed)}
            className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground">
            {outputCollapsed ? <ChevronRight className="w-3 h-3" /> : <ChevronDown className="w-3 h-3" />}
            输出结果 ({outputSteps.length})
          </button>
          {!outputCollapsed && (
            <div className="ml-4 mt-1 space-y-1.5">
              {outputSteps.map((s, i) => (
                <div key={i} className="text-xs">
                  <span className="text-muted-foreground font-medium">{s.name}</span>
                  <pre className="mt-0.5 p-1.5 rounded bg-muted text-foreground whitespace-pre-wrap break-all max-h-40 overflow-y-auto text-[11px]">{s.output}</pre>
                </div>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  )
}

// collectOutputs extracts steps that have meaningful output text (for the
// output-results section). Skips steps with empty or trivial output.
function collectOutputs(steps: any[]): { name: string; output: string }[] {
  const out: { name: string; output: string }[] = []
  for (const s of steps) {
    if (s.output && s.output.trim() && s.output !== '(dry run)') {
      out.push({ name: s.name, output: s.output })
    }
    if (s.children) out.push(...collectOutputs(s.children))
    if (s.then_children) out.push(...collectOutputs(s.then_children))
    if (s.else_children) out.push(...collectOutputs(s.else_children))
  }
  return out
}

// ExecStepTree renders execution steps with indentation. Container steps
// (parallel/foreach) auto-collapse their children and show N/M completion.
function ExecStepTree({ steps, depth }: { steps: any[]; depth: number }) {
  return (
    <div className={depth > 0 ? 'ml-3 border-l border-border pl-2 space-y-0.5' : 'space-y-0.5'}>
      {steps.map((s: any, i: number) => (
        <ExecStepLine key={i} step={s} depth={depth} />
      ))}
    </div>
  )
}

function ExecStepLine({ step: s, depth }: { step: any; depth: number }) {
  // Container steps (parallel/foreach/loop) with children: show as collapsible
  // with completion count.
  const childSteps = s.children || []
  const isContainer = (s.action === 'parallel' || s.action === 'foreach' || s.action === 'loop') && childSteps.length > 0
  const [containerCollapsed, setContainerCollapsed] = useState(isContainer && childSteps.length > 3)
  const [showOutput, setShowOutput] = useState(false)

  const completed = childSteps.filter((c: any) => c.status === 'success' || c.status === 'failed').length
  const total = childSteps.length

  return (
    <div>
      <div className={`flex items-center gap-1.5 text-xs py-0.5 ${s.status === 'skipped' ? 'opacity-50' : ''}`}>
        {/* Container collapse toggle */}
        {isContainer ? (
          <button onClick={() => setContainerCollapsed(!containerCollapsed)}>
            {containerCollapsed ? <ChevronRight className="w-3 h-3 text-muted-foreground" /> : <ChevronDown className="w-3 h-3 text-muted-foreground" />}
          </button>
        ) : null}
        {s.status === 'success' ? <CheckCircle className="w-3 h-3 text-green-500 flex-shrink-0" />
         : s.status === 'failed' ? <XCircle className="w-3 h-3 text-red-500 flex-shrink-0" />
         : s.status === 'running' ? <Loader2 className="w-3 h-3 animate-spin text-primary flex-shrink-0" />
         : <Clock className="w-3 h-3 text-muted-foreground flex-shrink-0" />}
        <span className={s.status === 'skipped' ? 'text-muted-foreground line-through' : ''}>{s.name}</span>
        {s.action && <span className="text-muted-foreground text-[10px]">({s.action})</span>}
        {/* Container completion count */}
        {isContainer && <span className="text-muted-foreground text-[10px]">{completed}/{total}</span>}
        {s.duration && s.duration !== '0s' && <span className="text-muted-foreground">{s.duration}</span>}
        {/* Output toggle for steps with output */}
        {s.output && s.output.trim() && s.output !== '(dry run)' && (
          <button onClick={() => setShowOutput(!showOutput)} className="text-muted-foreground hover:text-foreground text-[10px]">
            {showOutput ? '收起' : '详情'}
          </button>
        )}
      </div>
      {/* Inline output */}
      {showOutput && s.output && (
        <pre className="ml-5 mt-0.5 p-1.5 rounded bg-muted text-foreground whitespace-pre-wrap break-all max-h-32 overflow-y-auto text-[11px]">{s.output}</pre>
      )}
      {/* Container children (collapsible) */}
      {isContainer && !containerCollapsed && childSteps.length > 0 && (
        <div className="mt-0.5"><ExecStepTree steps={childSteps} depth={depth + 1} /></div>
      )}
      {/* Non-container nested children */}
      {(!isContainer || !containerCollapsed) && s.then_children && s.then_children.length > 0 && (
        <div className="mt-0.5"><span className="text-[10px] text-green-600 dark:text-green-400">✓ then:</span><ExecStepTree steps={s.then_children} depth={depth + 1} /></div>
      )}
      {(!isContainer || !containerCollapsed) && s.else_children && s.else_children.length > 0 && (
        <div className="mt-0.5"><span className="text-[10px] text-orange-600 dark:text-orange-400">✗ else:</span><ExecStepTree steps={s.else_children} depth={depth + 1} /></div>
      )}
    </div>
  )
}

// ── Step text tree (with collapse for long lists) ────────────────────────────

const COLLAPSE_THRESHOLD = 5

function StepTreeText({ steps, depth }: { steps: ChatStep[]; depth?: number }) {
  const d = depth || 0
  const [collapsed, setCollapsed] = useState(d === 0 && steps.length > COLLAPSE_THRESHOLD)

  if (collapsed && d === 0) {
    return (
      <div>
        {steps.slice(0, COLLAPSE_THRESHOLD).map((s, i) => <StepLine key={i} s={s} depth={0} />)}
        <button onClick={() => setCollapsed(false)} className="text-xs text-primary hover:underline mt-1">
          展开剩余 {steps.length - COLLAPSE_THRESHOLD} 步 ▾
        </button>
      </div>
    )
  }
  return (
    <div className={d > 0 ? 'ml-3 border-l border-border pl-2' : ''}>
      {steps.map((s, i) => <StepLine key={i} s={s} depth={d} />)}
      {d === 0 && steps.length > COLLAPSE_THRESHOLD && (
        <button onClick={() => setCollapsed(true)} className="text-xs text-primary hover:underline mt-1">收起 ▴</button>
      )}
    </div>
  )
}

function StepLine({ s, depth: d }: { s: ChatStep; depth: number }) {
  return (
    <div className="text-xs py-0.5">
      <span className="inline-flex items-center gap-1">
        {d > 0 && <span className="text-muted-foreground">↳</span>}
        <span className="px-1.5 py-0 rounded bg-muted text-muted-foreground">{s.action}</span>
        <span>{s.name}</span>
        {s.next && s.next.length > 0 && <span className="text-muted-foreground"> → {s.next.join(', ')}</span>}
      </span>
      {s.then && s.then.length > 0 && <div className="mt-0.5"><span className="text-xs text-green-600 dark:text-green-400">✓ then:</span><StepTreeText steps={s.then} depth={d + 1} /></div>}
      {s.else && s.else.length > 0 && <div className="mt-0.5"><span className="text-xs text-orange-600 dark:text-orange-400">✗ else:</span><StepTreeText steps={s.else} depth={d + 1} /></div>}
      {s.do && s.do.length > 0 && <div className="mt-0.5"><span className="text-xs text-blue-600 dark:text-blue-400">↻ do:</span><StepTreeText steps={s.do} depth={d + 1} /></div>}
      {s.steps && s.steps.length > 0 && <div className="mt-0.5"><span className="text-xs text-purple-600 dark:text-purple-400">∥ parallel:</span><StepTreeText steps={s.steps} depth={d + 1} /></div>}
    </div>
  )
}

// ── Mermaid graph generation (safe node IDs) ─────────────────────────────────

function stepsToMermaid(steps: ChatStep[]): string {
  const lines: string[] = ['graph TD']
  const nameToId = new Map<string, string>()
  let counter = 0
  const collectNames = (ss: ChatStep[]) => {
    for (const s of ss) {
      if (!nameToId.has(s.name)) nameToId.set(s.name, `s${counter++}`)
      if (s.then) collectNames(s.then)
      if (s.else) collectNames(s.else)
      if (s.do) collectNames(s.do)
      if (s.steps) collectNames(s.steps)
    }
  }
  collectNames(steps)

  const declare = (s: ChatStep) => {
    const id = nameToId.get(s.name)!
    lines.push(`  ${id}["${s.name} (${s.action})"]`)
  }
  const edge = (from: string, to: string, label?: string) => {
    const fromId = nameToId.get(from), toId = nameToId.get(to)
    if (fromId && toId) lines.push(label ? `  ${fromId} -->|${label}| ${toId}` : `  ${fromId} --> ${toId}`)
  }
  const renderBranch = (branchSteps: ChatStep[], parentName: string, label: string) => {
    if (!branchSteps.length) return
    edge(parentName, branchSteps[0].name, label)
    for (let i = 0; i < branchSteps.length; i++) {
      declare(branchSteps[i])
      if (i > 0) edge(branchSteps[i - 1].name, branchSteps[i].name)
      if (branchSteps[i].then) renderBranch(branchSteps[i].then!, branchSteps[i].name, 'then')
      if (branchSteps[i].else) renderBranch(branchSteps[i].else!, branchSteps[i].name, 'else')
    }
  }

  for (const s of steps) {
    declare(s)
    if (s.next) for (const n of s.next) edge(s.name, n)
    if (s.then) renderBranch(s.then, s.name, 'then')
    if (s.else) renderBranch(s.else, s.name, 'else')
    if (s.do) {
      edge(s.name, s.do[0].name, 'do')
      for (let i = 0; i < s.do.length; i++) { declare(s.do[i]); if (i > 0) edge(s.do[i - 1].name, s.do[i].name) }
    }
    if (s.steps) for (const child of s.steps) { edge(s.name, child.name, 'par'); declare(child) }
  }
  return lines.join('\n')
}
