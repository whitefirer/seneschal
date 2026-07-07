import { useEffect, useRef, useState } from 'react'
import mermaid from 'mermaid'

let initialized = false

function initMermaid() {
  if (initialized) return
  mermaid.initialize({
    startOnLoad: false,
    theme: 'default',
    securityLevel: 'loose',
    flowchart: { htmlLabels: true, curve: 'basis', useMaxWidth: false },
  })
  initialized = true
}

/**
 * Mermaid renders a chart definition into an SVG. The SVG is sized to fill
 * the container width (useMaxWidth: false + explicit CSS) so it doesn't
 * render tiny inside a large modal.
 */
export default function Mermaid({ chart }: { chart: string }) {
  const containerRef = useRef<HTMLDivElement>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    initMermaid()
    const id = 'mermaid-' + Math.random().toString(36).slice(2, 9)
    let cancelled = false

    mermaid.render(id, chart).then(({ svg }) => {
      if (cancelled || !containerRef.current) return
      containerRef.current.innerHTML = svg
      // Force the SVG to be at least as wide as the container and scale up.
      const svgEl = containerRef.current.querySelector('svg')
      if (svgEl) {
        svgEl.style.maxWidth = 'none'
        svgEl.style.width = '100%'
        svgEl.style.height = 'auto'
        // If the natural SVG is wider than container, let it overflow scroll.
        const naturalWidth = svgEl.viewBox?.baseVal?.width || svgEl.clientWidth
        if (naturalWidth > containerRef.current.clientWidth) {
          svgEl.style.width = naturalWidth + 'px'
        }
      }
    }).catch((err) => {
      if (!cancelled) setError(String(err))
    })

    return () => { cancelled = true }
  }, [chart])

  if (error) {
    return <div className="text-xs text-red-500 p-2">图表渲染失败: {error}</div>
  }
  return <div ref={containerRef} className="mermaid-container" style={{ minWidth: '100%', overflow: 'auto' }} />
}
