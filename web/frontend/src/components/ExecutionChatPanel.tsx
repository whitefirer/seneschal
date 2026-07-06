import { useState, useRef, useEffect } from 'react'
import { Bot, User, Loader2, Send, X, MessageCircle } from 'lucide-react'
import { askApi, type ChatSSEEvent } from '@/api/client'
import { MarkdownView } from './MarkdownView'

interface Msg {
  role: 'user' | 'assistant'
  content: string
  thinking?: boolean
}

/**
 * ExecutionChatPanel is a floating assistant panel embedded in the Execution
 * view. It lets the user ask questions about the *current* execution —
 * "why did this step fail?", "explain what this workflow did", etc.
 *
 * Unlike the /chat page (entry-point chat), this panel's context is the
 * execution itself (step tree, variables, outputs), fed to the AI server-side
 * via /api/executions/{id}/ask.
 */
export function ExecutionChatPanel({ executionId }: { executionId: string }) {
  const [open, setOpen] = useState(false)
  const [messages, setMessages] = useState<Msg[]>([])
  const [input, setInput] = useState('')
  const [loading, setLoading] = useState(false)
  const abortRef = useRef<AbortController | null>(null)
  const scrollRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    scrollRef.current?.scrollTo({ top: scrollRef.current.scrollHeight, behavior: 'smooth' })
  }, [messages])

  const ask = async (question?: string) => {
    const q = (question ?? input).trim()
    if (!q || loading) return
    setInput('')
    setMessages((prev) => [...prev, { role: 'user', content: q }, { role: 'assistant', content: '', thinking: true }])
    setLoading(true)

    const ac = new AbortController()
    abortRef.current = ac

    try {
      await askApi.askExecution(executionId, q, (event: ChatSSEEvent) => {
        if (event.type === 'token') {
          setMessages((prev) => {
            const next = [...prev]
            next[next.length - 1] = { role: 'assistant', content: (event as any).text || '', thinking: false }
            return next
          })
        } else if (event.type === 'error') {
          setMessages((prev) => {
            const next = [...prev]
            next[next.length - 1] = { role: 'assistant', content: `出错了：${(event as any).error}` }
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

  if (!open) {
    return (
      <button
        onClick={() => setOpen(true)}
        className="fixed bottom-6 right-6 z-50 w-12 h-12 rounded-full bg-primary text-primary-foreground shadow-lg flex items-center justify-center hover:bg-primary/90 transition-colors"
        title="Ask AI about this execution"
      >
        <MessageCircle className="w-5 h-5" />
      </button>
    )
  }

  return (
    <div className="fixed bottom-6 right-6 z-50 w-96 max-h-[60vh] flex flex-col rounded-2xl border border-border bg-background shadow-2xl">
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-2.5 border-b border-border">
        <div className="flex items-center gap-2">
          <Bot className="w-4 h-4 text-primary" />
          <span className="text-sm font-medium">执行助手</span>
        </div>
        <button onClick={() => setOpen(false)} className="p-1 rounded hover:bg-muted">
          <X className="w-4 h-4 text-muted-foreground" />
        </button>
      </div>

      {/* Messages */}
      <div ref={scrollRef} className="flex-1 overflow-y-auto p-3 space-y-3 min-h-[200px]">
        {messages.length === 0 && (
          <div className="text-center text-sm text-muted-foreground py-8 space-y-3">
            <p>问任何关于这次执行的问题</p>
            <div className="flex flex-col gap-1.5 items-center">
              <button onClick={() => ask('解释这次执行的结果')} className="text-xs px-3 py-1 rounded-full border border-border hover:bg-muted">解释这次执行的结果</button>
              <button onClick={() => ask('有哪些步骤失败了？为什么？')} className="text-xs px-3 py-1 rounded-full border border-border hover:bg-muted">有哪些步骤失败了？</button>
            </div>
          </div>
        )}
        {messages.map((m, i) => (
          <div key={i} className={`flex gap-2 ${m.role === 'user' ? 'justify-end' : ''}`}>
            {m.role === 'assistant' && (
              <div className="flex-shrink-0 w-6 h-6 rounded-full bg-primary/10 flex items-center justify-center">
                {m.thinking ? <Loader2 className="w-3 h-3 animate-spin text-primary" /> : <Bot className="w-3 h-3 text-primary" />}
              </div>
            )}
            <div className={`max-w-[80%] rounded-xl px-3 py-1.5 text-sm ${m.role === 'user' ? 'bg-primary text-primary-foreground' : 'bg-muted text-foreground'}`}>
              {m.thinking && !m.content ? (
                <span className="animate-pulse">思考中…</span>
              ) : (
                m.role === 'assistant' ? <MarkdownView content={m.content} /> : m.content
              )}
            </div>
            {m.role === 'user' && (
              <div className="flex-shrink-0 w-6 h-6 rounded-full bg-muted flex items-center justify-center">
                <User className="w-3 h-3 text-muted-foreground" />
              </div>
            )}
          </div>
        ))}
      </div>

      {/* Input */}
      <div className="border-t border-border p-2">
        <div className="flex gap-1.5 items-end">
          <input
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={(e) => { if (e.key === 'Enter') ask() }}
            placeholder="问一个问题…"
            className="flex-1 rounded-lg border border-border bg-background px-3 py-1.5 text-sm focus:outline-none focus:ring-1 focus:ring-primary"
            disabled={loading}
          />
          <button onClick={() => ask()} disabled={!input.trim() || loading} className="flex-shrink-0 w-8 h-8 rounded-lg bg-primary text-primary-foreground flex items-center justify-center hover:bg-primary/90 disabled:opacity-50">
            <Send className="w-3.5 h-3.5" />
          </button>
        </div>
      </div>
    </div>
  )
}
