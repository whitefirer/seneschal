import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { FloatingToolbar } from './FloatingToolbar'

function renderToolbar(overrides: Partial<Parameters<typeof FloatingToolbar>[0]> = {}) {
  const props = {
    showCollapseToggle: true,
    isAllCollapsed: false,
    onToggleCollapseAll: vi.fn(),
    showMiniMap: true,
    onToggleMiniMap: vi.fn(),
    showLogs: true,
    onToggleLogs: vi.fn(),
    ...overrides,
  }
  render(<FloatingToolbar {...props} />)
  return props
}

describe('FloatingToolbar', () => {
  it('renders collapse/minimap/logs toggles', () => {
    renderToolbar()
    expect(screen.getByTitle('Collapse All Nodes')).toBeInTheDocument()
    expect(screen.getByTitle('Hide MiniMap')).toBeInTheDocument()
    expect(screen.getByTitle('Hide Logs')).toBeInTheDocument()
  })

  it('hides the collapse toggle when there is nothing collapsible', () => {
    renderToolbar({ showCollapseToggle: false })
    expect(screen.queryByTitle('Collapse All Nodes')).not.toBeInTheDocument()
    expect(screen.getByTitle('Hide MiniMap')).toBeInTheDocument()
  })

  it('invokes the toggle callbacks', () => {
    const props = renderToolbar()
    fireEvent.click(screen.getByTitle('Collapse All Nodes'))
    expect(props.onToggleCollapseAll).toHaveBeenCalledTimes(1)
    fireEvent.click(screen.getByTitle('Hide MiniMap'))
    expect(props.onToggleMiniMap).toHaveBeenCalledTimes(1)
    fireEvent.click(screen.getByTitle('Hide Logs'))
    expect(props.onToggleLogs).toHaveBeenCalledTimes(1)
  })

  it('shows expand-all affordance when everything is collapsed', () => {
    renderToolbar({ isAllCollapsed: true })
    expect(screen.getByTitle('Expand All Nodes')).toBeInTheDocument()
  })
})
