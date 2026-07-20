import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { LogPanel } from './LogPanel'

const logs = [
  '[2026-01-01T00:00:00Z] INFO: build started',
  '[2026-01-01T00:00:01Z] ERROR: compile failed',
  '[2026-01-01T00:00:02Z] INFO: build done',
]

function renderPanel(overrides: Partial<Parameters<typeof LogPanel>[0]> = {}) {
  const props = {
    logs,
    layout: 'bottom' as const,
    onLayoutChange: vi.fn(),
    onClose: vi.fn(),
    ...overrides,
  }
  render(<LogPanel {...props} />)
  return props
}

describe('LogPanel', () => {
  it('renders the empty state when there are no logs', () => {
    renderPanel({ logs: [] })
    expect(screen.getByText('No logs yet')).toBeInTheDocument()
  })

  it('renders leveled log entries', () => {
    renderPanel()
    expect(screen.getByText('build started')).toBeInTheDocument()
    expect(screen.getByText('compile failed')).toBeInTheDocument()
    expect(screen.getByText('build done')).toBeInTheDocument()
    // 级别徽标
    expect(screen.getAllByText('INFO:')).toHaveLength(2)
    expect(screen.getByText('ERROR:')).toBeInTheDocument()
  })

  it('filters logs by level via the level dropdown', () => {
    renderPanel()
    // 级别下拉在搜索工具栏内：先打开搜索栏
    fireEvent.click(screen.getByTitle('Search logs'))
    // 打开级别下拉，取消勾选 ERROR
    fireEvent.click(screen.getByTitle('Log levels'))
    const errorCheckbox = screen.getByRole('checkbox', { name: /ERROR/ })
    fireEvent.click(errorCheckbox)
    expect(screen.queryByText('compile failed')).not.toBeInTheDocument()
    expect(screen.getByText('build started')).toBeInTheDocument()
  })

  it('searches and shows match count', () => {
    renderPanel()
    fireEvent.click(screen.getByTitle('Search logs'))
    const input = screen.getByPlaceholderText('Search...')
    fireEvent.change(input, { target: { value: 'build' } })
    // 两条 INFO 匹配 → 1/2
    expect(screen.getByText('1/2')).toBeInTheDocument()
    fireEvent.change(input, { target: { value: 'zzz-no-match' } })
    expect(screen.getByText('No results')).toBeInTheDocument()
  })

  it('invokes onLayoutChange and onClose from toolbar buttons', () => {
    const props = renderPanel()
    fireEvent.click(screen.getByTitle('Hide Logs'))
    expect(props.onClose).toHaveBeenCalledTimes(1)
  })
})
