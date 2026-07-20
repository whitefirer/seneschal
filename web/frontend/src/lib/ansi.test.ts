import { describe, it, expect } from 'vitest'
import { parseAnsiToHtml, highlightInHtml } from './ansi'

describe('parseAnsiToHtml', () => {
  it('passes plain text through', () => {
    expect(parseAnsiToHtml('hello world')).toContain('hello world')
  })

  it('converts ANSI color codes to styled spans', () => {
    const html = parseAnsiToHtml('[31mred text[0m')
    expect(html).not.toContain('[31m')
    expect(html).toContain('red text')
    expect(html).toMatch(/<span[^>]*>/)
  })

  it('escapes XML/HTML special characters', () => {
    const html = parseAnsiToHtml('<script>alert(1)</script>')
    expect(html).not.toContain('<script>')
    expect(html).toContain('&lt;script&gt;')
  })
})

describe('highlightInHtml', () => {
  it('returns html unchanged when pattern is null/undefined', () => {
    const html = '<span>hello</span>'
    expect(highlightInHtml(html, null)).toBe(html)
    expect(highlightInHtml(html, undefined)).toBe(html)
  })

  it('wraps matches in <mark> inside text nodes', () => {
    // 注意：pattern 必须带 g 标志（库的所有调用方都传 g 标志正则）
    const out = highlightInHtml('hello world', /hello/gi)
    expect(out).toContain('<mark')
    expect(out).toContain('>hello</mark>')
    expect(out).toContain('world')
  })

  it('highlights all case-insensitive matches without breaking tags', () => {
    const out = highlightInHtml('<span class="x">Error and error</span>', /error/gi)
    const marks = out.match(/<mark/g) || []
    expect(marks).toHaveLength(2)
    // 标签属性里的 "error"（如果有）不应被破坏：原始 class 保留
    expect(out).toContain('class="x"')
  })
})
