// 图形编辑器节点：action 下拉 + 通用字段表单（数据驱动，字段定义见 lib/stepSchema.ts）
import { useState, memo } from 'react'
import { Handle, Position } from '@xyflow/react'
import { Trash2, Plus, ChevronDown, ChevronRight, GitBranch, Layers, Repeat, Terminal, type LucideIcon } from 'lucide-react'
import { ACTION_DEFS, COMMON_FIELDS, actionDef, type FieldDef } from '@/lib/stepSchema'

export type StepFieldValue = string | number | boolean | string[] | number[] | Record<string, string> | null | undefined

export interface StepNodeHandlers {
  onChange: (id: string, patch: Record<string, unknown>) => void
  onAddChild: (id: string, branchType: 'then' | 'else' | 'parallel' | 'do') => void
  onAddNext: (id: string) => void
  onDelete: (id: string) => void
}

export interface StepNodeData extends Record<string, unknown> {
  name: string
  action: string
  __handlers?: StepNodeHandlers
  __upstream?: { name: string; outputVar?: string }[]
}

const actionColor: Record<string, string> = {
  shell: '#4ade80', http: '#60a5fa', condition: '#f59e0b', parallel: '#a78bfa',
  foreach: '#22d3ee', loop: '#22d3ee', ai: '#f472b6', ai_decide: '#f472b6',
  workflow: '#38bdf8', script: '#fbbf24', template: '#34d399', set: '#94a3b8',
  sleep: '#a3e635', log: '#fde047',
}

function containerBadge(action: string): { icon: LucideIcon; label: string } | null {
  if (action === 'condition') return { icon: GitBranch, label: 'condition' }
  if (action === 'parallel') return { icon: Layers, label: 'parallel' }
  if (action === 'foreach' || action === 'loop') return { icon: Repeat, label: action }
  return null
}

// ── 单字段编辑器 ──────────────────────────────────────────────────────────
function FieldEditor({ field, value, onChange }: {
  field: FieldDef
  value: StepFieldValue
  onChange: (v: StepFieldValue) => void
}) {
  if (field.type === 'textarea') {
    return (
      <textarea
        value={typeof value === 'string' || typeof value === 'number' ? value : String(value ?? '')}
        onChange={(e) => onChange(e.target.value)}
        placeholder={field.placeholder}
        rows={2}
        className="w-full px-2 py-1 text-xs border rounded bg-background font-mono resize-y"
      />
    )
  }
  if (field.type === 'select') {
    return (
      <select value={typeof value === 'string' ? value : ''} onChange={(e) => onChange(e.target.value)} className="w-full px-2 py-1 text-xs border rounded bg-background">
        <option value="">—</option>
        {(field.options || []).map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}
      </select>
    )
  }
  if (field.type === 'number') {
    return (
      <input type="number" value={typeof value === 'number' ? value : 0} onChange={(e) => onChange(Number(e.target.value))}
        className="w-full px-2 py-1 text-xs border rounded bg-background" />
    )
  }
  if (field.type === 'bool') {
    return (
      <input type="checkbox" checked={!!value} onChange={(e) => onChange(e.target.checked)} className="h-4 w-4" />
    )
  }
  if (field.type === 'kv') {
    return <KVEditor value={(value as Record<string, string> | null | undefined) || {}} onChange={onChange} />
  }
  if (field.type === 'list' || field.key === 'items') {
    return <ListEditor value={value} onChange={onChange} allowString={field.key === 'items'} />
  }
  // text 默认
  return (
    <input type="text" value={typeof value === 'string' || typeof value === 'number' ? value : String(value ?? '')} onChange={(e) => onChange(e.target.value)}
      placeholder={field.placeholder} className="w-full px-2 py-1 text-xs border rounded bg-background font-mono" />
  )
}

// key-value 编辑器
function KVEditor({ value, onChange }: { value: Record<string, string>; onChange: (v: Record<string, string>) => void }) {
  const entries = Object.entries(value || {})
  const update = (i: number, k: string, v: string) => {
    const next: Record<string, string> = {}
    entries.forEach(([ek, ev], j) => {
      const nk = j === i ? k : ek
      const nv = j === i ? v : ev
      if (nk.trim()) next[nk.trim()] = nv
    })
    if (i === entries.length) next[k.trim()] = v // 新增行
    onChange(next)
  }
  const remove = (i: number) => {
    const next: Record<string, string> = {}
    entries.forEach(([ek, ev], j) => { if (j !== i) next[ek] = ev })
    onChange(next)
  }
  return (
    <div className="space-y-1">
      {entries.map(([k, v], i) => (
        <div key={i} className="flex gap-1">
          <input value={k} onChange={(e) => update(i, e.target.value, v)} placeholder="key"
            className="flex-1 px-2 py-1 text-xs border rounded bg-background font-mono" />
          <input value={v} onChange={(e) => update(i, k, e.target.value)} placeholder="value"
            className="flex-1 px-2 py-1 text-xs border rounded bg-background font-mono" />
          <button onClick={() => remove(i)} className="px-1 text-xs text-red-500 hover:text-red-700">×</button>
        </div>
      ))}
      <button onClick={() => update(entries.length, '', '')} className="text-xs text-primary hover:underline">+ 添加</button>
    </div>
  )
}

// list 编辑器（每行一项）；allowString 时若含 {{ 则按字符串存
function ListEditor({ value, onChange, allowString }: { value: StepFieldValue; onChange: (v: StepFieldValue) => void; allowString?: boolean }) {
  const text = Array.isArray(value) ? value.map(String).join('\n') : (typeof value === 'string' ? value : String(value ?? ''))
  const handle = (s: string) => {
    if (allowString && s.includes('{{')) { onChange(s); return }
    onChange(s.split('\n').map((x) => x.trim()).filter(Boolean))
  }
  return (
    <textarea value={text} onChange={(e) => handle(e.target.value)} rows={2} placeholder="每行一项"
      className="w-full px-2 py-1 text-xs border rounded bg-background font-mono resize-y" />
  )
}

// ── 节点主体 ─────────────────────────────────────────────────────────────
const StepNode = memo(function StepNode({ id, data, selected }: { id: string; data: StepNodeData; selected?: boolean }) {
  const [showAdvanced, setShowAdvanced] = useState(false)
  const def = actionDef(data.action)
  const badge = containerBadge(data.action)
  const h = data.__handlers
  const borderColor = actionColor[data.action] || '#9ca3af'

  const patch = (k: string, v: StepFieldValue) => h?.onChange(id, { [k]: v })

  // 引用上游：点击 chip 把 {{.ref}} 追加到 prompt/question
  const insertRef = (u: { name: string; outputVar?: string }) => {
    const ref = u.outputVar || u.name
    const key = data.action === 'ai_decide' ? 'question' : 'prompt'
    patch(key, ((data[key] || '') + ' {{.' + ref + '}}').trim())
  }

  return (
    <div
      className="rounded-lg border-2 bg-card shadow-sm text-left"
      style={{ borderColor: selected ? '#3b82f6' : borderColor, minWidth: 220, maxWidth: 300 }}
    >
      <Handle type="target" position={Position.Left} className="!w-2.5 !h-2.5 !bg-gray-400" />
      <div className="px-2 py-1.5 flex items-center gap-1" style={{ borderBottom: '1px solid rgba(0,0,0,0.1)' }}>
        {badge ? (
          <badge.icon className="w-3.5 h-3.5" style={{ color: borderColor }} />
        ) : (
          <Terminal className="w-3.5 h-3.5" style={{ color: borderColor }} />
        )}
        <input
          value={data.name || ''}
          onChange={(e) => patch('name', e.target.value)}
          placeholder="name"
          className="flex-1 min-w-0 px-1 py-0.5 text-sm font-semibold bg-transparent outline-none"
        />
        <button onClick={() => h?.onDelete(id)} className="p-0.5 text-gray-400 hover:text-red-500" title="删除节点">
          <Trash2 className="w-3.5 h-3.5" />
        </button>
      </div>

      <div className="px-2 py-2 space-y-1.5">
        <select value={data.action} onChange={(e) => patch('action', e.target.value)}
          className="w-full px-1 py-1 text-xs border rounded bg-background">
          {ACTION_DEFS.map((a) => <option key={a.value} value={a.value}>{a.label}</option>)}
        </select>

        {/* 引用上游（AI 节点：点击 chip 插入 {{.ref}}） */}
        {(data.action === 'ai' || data.action === 'ai_decide') && ((data.__upstream?.length ?? 0) > 0) && (
          <div className="flex flex-wrap items-center gap-1">
            <span className="text-[10px] text-muted-foreground">上游:</span>
            {(data.__upstream || []).map((u) => (
              <button key={u.name} onClick={() => insertRef(u)} title={u.name}
                className="px-1.5 py-0.5 text-[10px] border rounded hover:bg-accent font-mono text-muted-foreground">
                {u.outputVar || u.name}
              </button>
            ))}
          </div>
        )}

        {def?.fields.map((f) => (
          <div key={f.key}>
            <label className="block text-[10px] text-muted-foreground mb-0.5">{f.label}</label>
            <FieldEditor field={f} value={data[f.key] as StepFieldValue} onChange={(v) => patch(f.key, v)} />
          </div>
        ))}

        {/* 容器：添加子节点 */}
        {badge && (
          <div className="flex flex-wrap gap-1 pt-1">
            {badge.label === 'condition' && (
              <>
                <button onClick={() => h?.onAddChild(id, 'then')} className="flex items-center gap-1 px-2 py-0.5 text-xs border rounded hover:bg-accent"><Plus className="w-3 h-3" /> then</button>
                <button onClick={() => h?.onAddChild(id, 'else')} className="flex items-center gap-1 px-2 py-0.5 text-xs border rounded hover:bg-accent"><Plus className="w-3 h-3" /> else</button>
              </>
            )}
            {badge.label === 'parallel' && (
              <button onClick={() => h?.onAddChild(id, 'parallel')} className="flex items-center gap-1 px-2 py-0.5 text-xs border rounded hover:bg-accent"><Plus className="w-3 h-3" /> 子任务</button>
            )}
            {(badge.label === 'foreach' || badge.label === 'loop') && (
              <button onClick={() => h?.onAddChild(id, 'do')} className="flex items-center gap-1 px-2 py-0.5 text-xs border rounded hover:bg-accent"><Plus className="w-3 h-3" /> 循环体</button>
            )}
          </div>
        )}

        {/* 添加后续节点 */}
        <button onClick={() => h?.onAddNext(id)} className="flex items-center gap-1 text-xs text-primary hover:underline pt-1">
          <Plus className="w-3 h-3" /> 后续节点
        </button>

        {/* 高级 */}
        <button onClick={() => setShowAdvanced(!showAdvanced)} className="flex items-center gap-1 text-[10px] text-muted-foreground hover:text-foreground">
          {showAdvanced ? <ChevronDown className="w-3 h-3" /> : <ChevronRight className="w-3 h-3" />} 高级
        </button>
        {showAdvanced && (
          <div className="space-y-1.5 border-t pt-1.5">
            {COMMON_FIELDS.map((f) => (
              <div key={f.key}>
                <label className="block text-[10px] text-muted-foreground mb-0.5">{f.label}</label>
                <FieldEditor field={f} value={data[f.key] as StepFieldValue} onChange={(v) => patch(f.key, v)} />
              </div>
            ))}
          </div>
        )}
      </div>

      <Handle type="source" position={Position.Right} className="!w-2.5 !h-2.5 !bg-gray-400" />
    </div>
  )
})

export default StepNode
