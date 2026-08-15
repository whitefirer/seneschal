// 容器分组框：condition 的 then/else、parallel 的 steps、foreach 的 do
import { memo } from 'react'
import { GitBranch, Layers, Repeat } from 'lucide-react'

const kindStyle: Record<string, { color: string; Icon: any; label: string }> = {
  condition: { color: '#f59e0b', Icon: GitBranch, label: 'condition' },
  parallel: { color: '#a78bfa', Icon: Layers, label: 'parallel' },
  foreach: { color: '#22d3ee', Icon: Repeat, label: 'loop' },
}

const GroupNode = memo(function GroupNode({ data }: {
  data: { branch: string; kind: 'condition' | 'parallel' | 'foreach'; width?: number; height?: number }
}) {
  const s = kindStyle[data.kind] || kindStyle.condition
  const Icon = s.Icon
  return (
    <div
      className="rounded-xl border-2 border-dashed bg-muted/10"
      style={{ width: data.width, height: data.height, borderColor: s.color }}
    >
      <div
        className="absolute -top-3 left-3 px-2 py-0.5 rounded text-xs font-medium flex items-center gap-1"
        style={{ color: s.color, background: 'var(--card, #fff)' }}
      >
        <Icon className="w-3 h-3" />
        {data.branch}
      </div>
    </div>
  )
})

export default GroupNode
