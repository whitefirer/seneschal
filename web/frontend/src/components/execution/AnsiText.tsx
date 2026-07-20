import { useMemo } from 'react'
import { highlightInHtml, parseAnsiToHtml } from '@/lib/ansi'

// 渲染带有 ANSI 颜色的文本（可选搜索高亮）
export function AnsiText({ text, highlightPattern }: { text: string; highlightPattern?: RegExp | null }) {
  const html = useMemo(() => {
    // 先进行 ANSI 转义
    const ansiHtml = parseAnsiToHtml(text)
    // 然后在 HTML 的文本节点上高亮匹配内容
    return highlightInHtml(ansiHtml, highlightPattern)
  }, [text, highlightPattern])
  return <span className="ansi-output" dangerouslySetInnerHTML={{ __html: html }} />
}
