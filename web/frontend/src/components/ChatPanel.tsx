import { useState, useRef, useEffect, lazy, Suspense } from 'react'
import { useNavigate } from 'react-router-dom'
import { Send, Bot, User, Loader2, Play, Sparkles, GitBranch } from 'lucide-react'
import { chatApi, workflowsApi, type ChatSelection, type ChatStep } from '@/api/client'
import { MarkdownView } from './MarkdownView'

// Lazy-load mermaid only when the user clicks "view structure" — keeps the
// main bundle small (~500KB savings).
const Mermaid = lazy(() => import('./Mermaid'))

interface Message {
  role: 'user' | 'assistant'
  content: string
  thinking?: boolean
  selection?: ChatSelection
}

export default function ChatPanel() {
  const navigate = useNavigate()
  const [messages, setMessages] = useState<Message[]>([
    {
      role: 'assistant',
      content: '你好！描述你想做什么，我会帮你找到合适的工作流并执行。比如："部署到 staging 环境"或"运行测试"。',
    },
  ])
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
              // No match — offer to create one.
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

  const runSelection = async (sel: ChatSelection) => {
    setLoading(true)
    try {
      // Use fileName (falls back to workflow name) — the /run API matches by
      // file name, not the YAML `name:` field. This was the cause of 404s.
      const runName = sel.fileName || sel.workflow
      const res = await workflowsApi.run(runName, { variables: sel.variables })
      navigate(`/execution/${res.executionId}`)
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
            <div
              className={`max-w-[80%] rounded-2xl px-4 py-2.5 ${
                m.role === 'user'
                  ? 'bg-primary text-primary-foreground'
                  : 'bg-muted text-foreground'
              }`}
            >
              {m.content && <MarkdownView content={m.content} />}
              {m.thinking && !m.content && (
                <span className="text-sm text-muted-foreground animate-pulse">思考中…</span>
              )}

              {/* Selection confirmation card */}
              {m.selection && m.selection.workflow && (
                <SelectionCard selection={m.selection} onRun={() => runSelection(m.selection!)} loading={loading} />
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
            onKeyDown={(e) => {
              if (e.key === 'Enter' && !e.shiftKey) {
                e.preventDefault()
                send()
              }
            }}
            placeholder="描述你想做什么…"
            rows={1}
            className="flex-1 resize-none rounded-xl border border-border bg-background px-4 py-2.5 text-sm focus:outline-none focus:ring-2 focus:ring-primary max-h-32"
            disabled={loading}
          />
          <button
            onClick={send}
            disabled={!input.trim() || loading}
            className="flex-shrink-0 w-10 h-10 rounded-xl bg-primary text-primary-foreground flex items-center justify-center hover:bg-primary/90 disabled:opacity-50"
          >
            <Send className="w-4 h-4" />
          </button>
        </div>
        <p className="text-xs text-muted-foreground mt-1.5">Enter 发送，Shift+Enter 换行</p>
      </div>
    </div>
  )
}

// ── Selection card with step preview + mermaid toggle ───────────────────────

function SelectionCard({ selection, onRun, loading }: { selection: ChatSelection; onRun: () => void; loading: boolean }) {
  const [showGraph, setShowGraph] = useState(false)

  return (
    <div className="mt-3 border border-border rounded-lg p-3 bg-background">
      <div className="flex items-center gap-2 mb-2">
        <Sparkles className="w-4 h-4 text-primary" />
        <span className="font-semibold text-sm">{selection.workflow}</span>
      </div>

      {/* Step preview — text with indentation + arrows */}
      {selection.steps && selection.steps.length > 0 && (
        <div className="mb-2">
          <div className="flex items-center justify-between mb-1">
            <p className="text-xs text-muted-foreground">步骤预览:</p>
            <button
              onClick={() => setShowGraph(!showGraph)}
              className="flex items-center gap-1 text-xs px-2 py-0.5 rounded border border-border hover:bg-muted text-muted-foreground"
            >
              <GitBranch className="w-3 h-3" />
              {showGraph ? '收起图' : '查看结构图'}
            </button>
          </div>

          {showGraph ? (
            <Suspense fallback={<div className="text-xs text-muted-foreground py-2">加载图表…</div>}>
              <div className="border border-border rounded p-2 bg-background overflow-x-auto">
                <Mermaid chart={stepsToMermaid(selection.steps)} />
              </div>
            </Suspense>
          ) : (
            <StepTreeText steps={selection.steps} />
          )}
        </div>
      )}

      {/* Variables */}
      {Object.keys(selection.variables).length > 0 && (
        <div className="mb-2">
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

      <button
        onClick={onRun}
        disabled={loading}
        className="w-full mt-2 flex items-center justify-center gap-2 px-3 py-1.5 rounded-lg bg-primary text-primary-foreground text-sm font-medium hover:bg-primary/90 disabled:opacity-50"
      >
        <Play className="w-3.5 h-3.5" />
        执行工作流
      </button>
    </div>
  )
}

// StepTreeText renders the step list with indentation to show nesting
// (condition branches, foreach body, parallel children) and arrows for
// next/depends_on at the top level.
function StepTreeText({ steps, depth }: { steps: ChatStep[]; depth?: number }) {
  const d = depth || 0
  return (
    <div className={d > 0 ? 'ml-3 border-l border-border pl-2' : ''}>
      {steps.map((s, i) => (
        <div key={i} className="text-xs py-0.5">
          <span className="inline-flex items-center gap-1">
            {d > 0 && <span className="text-muted-foreground">↳</span>}
            <span className="px-1.5 py-0 rounded bg-muted text-muted-foreground">{s.action}</span>
            <span>{s.name}</span>
            {s.next && s.next.length > 0 && (
              <span className="text-muted-foreground"> → {s.next.join(', ')}</span>
            )}
          </span>
          {s.then && s.then.length > 0 && (
            <div className="mt-0.5"><span className="text-xs text-green-600 dark:text-green-400">✓ then:</span><StepTreeText steps={s.then} depth={d + 1} /></div>
          )}
          {s.else && s.else.length > 0 && (
            <div className="mt-0.5"><span className="text-xs text-orange-600 dark:text-orange-400">✗ else:</span><StepTreeText steps={s.else} depth={d + 1} /></div>
          )}
          {s.do && s.do.length > 0 && (
            <div className="mt-0.5"><span className="text-xs text-blue-600 dark:text-blue-400">↻ do:</span><StepTreeText steps={s.do} depth={d + 1} /></div>
          )}
          {s.steps && s.steps.length > 0 && (
            <div className="mt-0.5"><span className="text-xs text-purple-600 dark:text-purple-400">∥ parallel:</span><StepTreeText steps={s.steps} depth={d + 1} /></div>
          )}
        </div>
      ))}
    </div>
  )
}

// stepsToMermaid converts the step tree into a mermaid graph definition string.
// Top-level steps are nodes; next/depends_on create edges; branches recurse.
function stepsToMermaid(steps: ChatStep[]): string {
  const lines: string[] = ['graph TD']
  const nodeLabel = (s: ChatStep) => `${s.name}['${s.name}<br/>(${s.action})']`

  for (const s of steps) {
    // Edges from next
    if (s.next) {
      for (const n of s.next) {
        lines.push(`  ${s.name} --> ${n}`)
      }
    }
    // Sub-graph for branches (simplified — just note the branch exists)
    if (s.then && s.then.length > 0) {
      lines.push(`  ${s.name} -.->|then| ${s.then[0].name}`)
      lines.push(...subStepsToMermaid(s.then, s.name + '_then'))
    }
    if (s.else && s.else.length > 0) {
      lines.push(`  ${s.name} -.->|else| ${s.else[0].name}`)
      lines.push(...subStepsToMermaid(s.else, s.name + '_else'))
    }
    if (s.do && s.do.length > 0) {
      lines.push(...subStepsToMermaid(s.do, s.name + '_do'))
    }
    if (s.steps && s.steps.length > 0) {
      lines.push(...subStepsToMermaid(s.steps, s.name + '_par'))
    }
    // Ensure node is declared (in case no edges reference it)
    lines.push(`  ${nodeLabel(s)}`)
  }
  return lines.join('\n')
}

function subStepsToMermaid(steps: ChatStep[], _prefix: string): string[] {
  const lines: string[] = []
  for (let i = 0; i < steps.length; i++) {
    const s = steps[i]
    lines.push(`  ${s.name}['${s.name}<br/>(${s.action})']`)
    if (i > 0) {
      lines.push(`  ${steps[i - 1].name} --> ${s.name}`)
    }
  }
  return lines
}
