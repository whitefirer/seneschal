import AnsiToHtmlModule from 'ansi-to-html'

// ansi-to-html 没有官方类型声明，这里给出本项目用到的最小接口
interface AnsiToHtmlOptions {
  fg?: string
  bg?: string
  newline?: boolean
  escapeXML?: boolean
  stream?: boolean
}
interface AnsiToHtmlInstance {
  toHtml: (input: string) => string
}

const AnsiToHtml = AnsiToHtmlModule as unknown as new (options?: AnsiToHtmlOptions) => AnsiToHtmlInstance

// 创建 ANSI 转 HTML 的实例（使用正确的导入方式）
const ansiConverter = new AnsiToHtml({
  fg: '#333',
  bg: 'transparent',
  newline: true,
  escapeXML: true,
  stream: false,
})

// 解析 ANSI 颜色代码为 HTML
export function parseAnsiToHtml(text: string): string {
  return ansiConverter.toHtml(text)
}

// 在 HTML 中高亮匹配文本（在文本节点上操作，避免破坏 HTML 结构）
export function highlightInHtml(html: string, pattern: RegExp | null | undefined): string {
  if (!pattern) return html

  // 克隆正则表达式，避免修改原始 lastIndex
  const clonedPattern = new RegExp(pattern.source, pattern.flags)

  const parser = new DOMParser()
  const doc = parser.parseFromString(html, 'text/html')

  const walk = (node: Node) => {
    if (node.nodeType === Node.TEXT_NODE) {
      const text = node.textContent || ''
      clonedPattern.lastIndex = 0
      if (clonedPattern.test(text)) {
        clonedPattern.lastIndex = 0
        const span = document.createElement('span')
        let lastIndex = 0
        let match
        while ((match = clonedPattern.exec(text)) !== null) {
          // 添加匹配前的文本
          if (match.index > lastIndex) {
            span.appendChild(document.createTextNode(text.slice(lastIndex, match.index)))
          }
          // 添加高亮的匹配文本
          const mark = document.createElement('mark')
          mark.className = 'bg-yellow-300 dark:bg-yellow-600 rounded px-0.5'
          mark.textContent = match[0]
          span.appendChild(mark)
          lastIndex = clonedPattern.lastIndex
        }
        // 添加剩余的文本
        if (lastIndex < text.length) {
          span.appendChild(document.createTextNode(text.slice(lastIndex)))
        }
        node.parentNode?.replaceChild(span, node)
      }
    } else {
      for (const child of Array.from(node.childNodes)) {
        walk(child)
      }
    }
  }

  walk(doc.body)
  return doc.body.innerHTML
}
