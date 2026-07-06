import { useState, useRef, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { Send, Bot, User, Loader2, Play, Sparkles } from 'lucide-react'
import { chatApi, workflowsApi, type ChatSelection } from '@/api/client'
import { MarkdownView } from './MarkdownView'

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

    // Placeholder assistant message that we update as events arrive.
    setMessages((prev) => [...prev, { role: 'assistant', content: '', thinking: true }])

    const ac = new AbortController()
    abortRef.current = ac

    try {
      await chatApi.sendMessage(msg, (event) => {
        if (event.type === 'thinking') {
          setMessages((prev) => {
            const next = [...prev]
            next[next.length - 1] = {
              role: 'assistant',
              content: '正在思考…',
              thinking: true,
            }
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
                content: '没有找到匹配的工作流。请尝试更具体的描述，或用 `generate` 命令创建一个新的。',
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
      const res = await workflowsApi.run(sel.workflow, { variables: sel.variables })
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
                <div className="mt-3 border border-border rounded-lg p-3 bg-background">
                  <div className="flex items-center gap-2 mb-2">
                    <Sparkles className="w-4 h-4 text-primary" />
                    <span className="font-semibold text-sm">{m.selection.workflow}</span>
                  </div>
                  {m.selection.steps && m.selection.steps.length > 0 && (
                    <div className="mb-2">
                      <p className="text-xs text-muted-foreground mb-1">步骤预览:</p>
                      <div className="flex flex-wrap gap-1">
                        {m.selection.steps.map((s, j) => (
                          <span key={j} className="text-xs px-2 py-0.5 rounded bg-muted">
                            {s.name} ({s.action})
                          </span>
                        ))}
                      </div>
                    </div>
                  )}
                  {Object.keys(m.selection.variables).length > 0 && (
                    <div className="mb-2">
                      <p className="text-xs text-muted-foreground mb-1">变量:</p>
                      <div className="space-y-0.5">
                        {Object.entries(m.selection.variables).map(([k, v]) => (
                          <div key={k} className="text-xs font-mono">
                            <span className="text-muted-foreground">{k}</span> = <span>{v}</span>
                          </div>
                        ))}
                      </div>
                    </div>
                  )}
                  <button
                    onClick={() => runSelection(m.selection!)}
                    disabled={loading}
                    className="w-full mt-2 flex items-center justify-center gap-2 px-3 py-1.5 rounded-lg bg-primary text-primary-foreground text-sm font-medium hover:bg-primary/90 disabled:opacity-50"
                  >
                    <Play className="w-3.5 h-3.5" />
                    执行工作流
                  </button>
                </div>
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
