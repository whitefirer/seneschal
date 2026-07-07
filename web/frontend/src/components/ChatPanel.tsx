import { useState, useRef, useEffect, lazy, Suspense } from 'react'
import { useNavigate } from 'react-router-dom'
import { Send, Bot, User, Loader2, Play, Sparkles, GitBranch, ChevronDown, ChevronRight, ExternalLink, X, CheckCircle, XCircle, Clock } from 'lucide-react'
import { chatApi, workflowsApi, executionsApi, type ChatSelection, type ChatStep } from '@/api/client'
import { MarkdownView } from './MarkdownView'

const Mermaid = lazy(() => import('./Mermaid'))

interface ExecState {
  id: string
  status: string
  steps?: any[]
}

interface Message {
  role: 'user' | 'assistant'
  content: string
  thinking?: boolean
  selection?: ChatSelection
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
    setMessages((prev) => [...prev, { role: 'user', content: msg }])
    setLoading(true)
    setMessages((prev) => [...prev, { role: 'assistant', content: '', thinking: true }])

    const ac = new AbortController()
    abortRef.current = ac

    try {
      await chatApi.sendMessage(msg, (event) => {
        if (event.type === 'thinking') {
          setMessages((prev) => {
            const next = [...prev]
            next[next.length - 1] = { role: 'assistant', content: '正在思考…', thinking: true }
            return next
          })
        } else if (event.type === 'selection') {
          const sel = event as unknown as ChatSelection
          setMessages((prev) => {
            const next = [...prev]
            if (sel.workflow) {
              next[next.length - 1] = {
                role: 'assistant',
                content: `我找到了工作流 **${sel.workflow}**，置信度 ${(sel.confidence * 100).toFixed(0)}%。`,
                selection: sel,
              }
            } else {
              next[next.length - 1] = {
                role: 'assistant',
                content: sel.suggestCreate
                  ? '没有找到匹配的工作流。你可以用 `goworkflow generate` 命令创建一个新的，或者换个描述再试试。'
                  : '没有找到匹配的工作流。请尝试更具体的描述。',
              }
            }
            return next
          })
        } else if (event.type === 'error') {
          setMessages((prev) => {
            const next = [...prev]
            next[next.length - 1] = { role: 'assistant', content: `出错了：${event.error}` }
            return next
          })
        }
      }, ac.signal)
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
              {m.content && <MarkdownView content={m.content} />}
              {m.thinking && !m.content && (
                <span className="text-sm text-muted-foreground animate-pulse">思考中…</span>
              )}
              {m.selection && m.selection.workflow && (
                <SelectionCard
                  selection={m.selection}
                  onRun={() => runSelection(i, m.selection!)}
                  loading={loading}
                  onNavigate={(id) => navigate(`/execution/${id}`)}
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

// ── Selection card: collapsed by default, expandable, inline exec progress ──

function SelectionCard({ selection, onRun, loading, onNavigate }: {
  selection: ChatSelection
  onRun: () => void
  loading: boolean
  onNavigate: (id: string) => void
}) {
  const [expanded, setExpanded] = useState(false)
  const execId = (selection as any).executionId as string | undefined

  // Collapsed view: name + description + run button (or progress).
  if (!expanded && !execId) {
    return (
      <div className="mt-3 border border-border rounded-lg p-3 bg-background">
        <div className="flex items-center gap-2">
          <button onClick={() => setExpanded(true)} className="flex items-center gap-1 flex-1 text-left">
            <ChevronRight className="w-4 h-4 text-muted-foreground flex-shrink-0" />
            <Sparkles className="w-4 h-4 text-primary flex-shrink-0" />
            <span className="font-semibold text-sm">{selection.workflow}</span>
          </button>
          <button onClick={onRun} disabled={loading}
            className="flex items-center gap-1 px-3 py-1 rounded-lg bg-primary text-primary-foreground text-xs font-medium hover:bg-primary/90 disabled:opacity-50">
            <Play className="w-3 h-3" /> 执行
          </button>
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

      {/* Step preview + mermaid toggle */}
      {expanded && selection.steps && selection.steps.length > 0 && (
        <StepPreviewSection steps={selection.steps} />
      )}

      {/* Variables */}
      {expanded && Object.keys(selection.variables).length > 0 && (
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

      {/* Run button or inline execution progress */}
      {execId ? (
        <ExecProgress execId={execId} onNavigate={onNavigate} />
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

function ExecProgress({ execId, onNavigate }: { execId: string; onNavigate: (id: string) => void }) {
  const [exec, setExec] = useState<ExecState | null>(null)
  const [collapsed, setCollapsed] = useState(false)

  useEffect(() => {
    let active = true
    let timer: ReturnType<typeof setTimeout>

    const poll = async () => {
      try {
        const data = await executionsApi.get(execId)
        if (!active) return
        setExec({ id: execId, status: data.status, steps: data.steps })
        // Continue polling if still running.
        if (data.status === 'running') {
          timer = setTimeout(poll, 2000)
        }
      } catch {
        // Execution not found (may not be registered yet) — retry shortly.
        if (active) timer = setTimeout(poll, 2000)
      }
    }
    poll()
    return () => { active = false; clearTimeout(timer) }
  }, [execId])

  const statusIcon = exec?.status === 'success' ? <CheckCircle className="w-4 h-4 text-green-500" />
    : exec?.status === 'failed' ? <XCircle className="w-4 h-4 text-red-500" />
    : <Loader2 className="w-4 h-4 animate-spin text-primary" />

  return (
    <div className="border-t border-border pt-2 mt-2">
      {/* Status header */}
      <div className="flex items-center gap-2 mb-1">
        {statusIcon}
        <span className="text-xs font-medium">
          {exec?.status === 'running' ? '执行中…' : exec?.status === 'success' ? '执行完成' : exec?.status === 'failed' ? '执行失败' : '准备中…'}
        </span>
        <button onClick={() => setCollapsed(!collapsed)} className="text-xs text-muted-foreground hover:text-foreground ml-1">
          {collapsed ? '展开' : '收起'}
        </button>
        <button onClick={() => onNavigate(execId)}
          className="ml-auto flex items-center gap-1 text-xs text-primary hover:underline">
          查看完整详情 <ExternalLink className="w-3 h-3" />
        </button>
      </div>

      {/* Step list (collapsible, with hierarchy) */}
      {!collapsed && exec?.steps && (
        <div className="ml-6">
          <ExecStepTree steps={exec.steps} depth={0} />
        </div>
      )}
    </div>
  )
}

// ExecStepTree renders execution steps with indentation preserving hierarchy
// (children, then_children, else_children get progressively indented).
function ExecStepTree({ steps, depth }: { steps: any[]; depth: number }) {
  return (
    <div className={depth > 0 ? 'ml-3 border-l border-border pl-2' : 'space-y-0.5'}>
      {steps.map((s: any, i: number) => (
        <div key={i}>
          <div className={`flex items-center gap-1.5 text-xs py-0.5 ${s.status === 'skipped' ? 'opacity-50' : ''}`}>
            {s.status === 'success' ? <CheckCircle className="w-3 h-3 text-green-500 flex-shrink-0" />
             : s.status === 'failed' ? <XCircle className="w-3 h-3 text-red-500 flex-shrink-0" />
             : s.status === 'running' ? <Loader2 className="w-3 h-3 animate-spin text-primary flex-shrink-0" />
             : <Clock className="w-3 h-3 text-muted-foreground flex-shrink-0" />}
            <span className={s.status === 'skipped' ? 'text-muted-foreground line-through' : ''}>{s.name}</span>
            {s.action && <span className="text-muted-foreground text-[10px]">({s.action})</span>}
            {s.duration && s.duration !== '0s' && <span className="text-muted-foreground">{s.duration}</span>}
          </div>
          {/* Recurse into nested children with indentation */}
          {s.children && s.children.length > 0 && (
            <div className="mt-0.5"><ExecStepTree steps={s.children} depth={depth + 1} /></div>
          )}
          {s.then_children && s.then_children.length > 0 && (
            <div className="mt-0.5"><span className="text-[10px] text-green-600 dark:text-green-400">✓ then:</span><ExecStepTree steps={s.then_children} depth={depth + 1} /></div>
          )}
          {s.else_children && s.else_children.length > 0 && (
            <div className="mt-0.5"><span className="text-[10px] text-orange-600 dark:text-orange-400">✗ else:</span><ExecStepTree steps={s.else_children} depth={depth + 1} /></div>
          )}
        </div>
      ))}
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
