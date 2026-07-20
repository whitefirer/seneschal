import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { ExecutionHeader } from './ExecutionHeader'

function renderHeader(overrides: Partial<Parameters<typeof ExecutionHeader>[0]> = {}) {
  const props = {
    workflowName: 'deploy-flow',
    workflowFile: 'deploy.yaml',
    executionId: 'exec-123',
    status: 'success' as const,
    connected: true,
    viewMode: 'graph' as const,
    onToggleViewMode: vi.fn(),
    onRefresh: vi.fn(),
    ...overrides,
  }
  render(<ExecutionHeader {...props} />)
  return props
}

describe('ExecutionHeader', () => {
  it('renders workflow name, execution id, status badge and live indicator', () => {
    renderHeader()
    expect(screen.getByText('deploy-flow')).toBeInTheDocument()
    expect(screen.getByText('(exec-123)')).toBeInTheDocument()
    expect(screen.getByText('Success')).toBeInTheDocument()
    expect(screen.getByText('🟢 Live')).toBeInTheDocument()
  })

  it('shows Loading... and Disconnected when name is empty and ws is down', () => {
    renderHeader({ workflowName: '', workflowFile: '', connected: false, status: 'running' })
    expect(screen.getByText('Loading...')).toBeInTheDocument()
    expect(screen.getByText('⚪ Disconnected')).toBeInTheDocument()
    expect(screen.getByText('Running')).toBeInTheDocument()
  })

  it('invokes onToggleViewMode and onRefresh from the action buttons', () => {
    const props = renderHeader()
    fireEvent.click(screen.getByText('List')) // graph 模式下显示 List 切换文案
    expect(props.onToggleViewMode).toHaveBeenCalledTimes(1)
    fireEvent.click(screen.getByTitle('Refresh'))
    expect(props.onRefresh).toHaveBeenCalledTimes(1)
  })
})
