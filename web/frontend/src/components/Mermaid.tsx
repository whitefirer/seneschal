import { useEffect, useRef, useState } from 'react'
import mermaid from 'mermaid'

// Mermaid is configured once on first import, then render is called per-chart.
let initialized = false

function initMermaid() {
  if (initialized) return
  mermaid.initialize({
    startOnLoad: false,
    theme: 'default',
    securityLevel: 'loose', // allow <br/> in node labels
    flowchart: { htmlLabels: true, curve: 'basis' },
  })
  initialized = true
}

/**
 * Mermaid renders a mermaid chart definition string into an SVG. Lazy-loaded
 * by ChatPanel so the ~500KB mermaid library only loads when the user clicks
 * "查看结构图".
 */
export default function Mermaid({ chart }: { chart: string }) {
  const containerRef = useRef<HTMLDivElement>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    initMermaid()
    const id = 'mermaid-' + Math.random().toString(36).slice(2, 9)
    let cancelled = false

    mermaid.render(id, chart).then(({ svg }) => {
      if (!cancelled && containerRef.current) {
        containerRef.current.innerHTML = svg
      }
    }).catch((err) => {
      if (!cancelled) setError(String(err))
    })

    return () => { cancelled = true }
  }, [chart])

  if (error) {
    return <div className="text-xs text-red-500 p-2">图表渲染失败: {error}</div>
  }
  return <div ref={containerRef} className="mermaid-container flex justify-center" />
}
