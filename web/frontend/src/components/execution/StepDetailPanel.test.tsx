import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { StepDetailPanel } from './StepDetailPanel'
import type { Step } from '@/types/execution'

function renderPanel(step: Step, onClose = vi.fn()) {
  const props = {
    step,
    position: { x: 100, y: 100 },
    onPositionChange: vi.fn(),
    onClose,
  }
  render(<StepDetailPanel {...props} />)
  return props
}

describe('StepDetailPanel', () => {
  beforeEach(() => {
    vi.mocked(navigator.clipboard.writeText).mockClear()
  })

  it('renders name, status and shell command for a shell step', () => {
    const step: Step = {
      id: 's1', name: 'build', action: 'shell', status: 'success',
      shellCommand: 'echo hello', output: 'hello\n', duration: '1s',
    }
    renderPanel(step)
    expect(screen.getByText('build')).toBeInTheDocument()
    expect(screen.getByText('Success')).toBeInTheDocument()
    expect(screen.getByText('Shell Command')).toBeInTheDocument()
    expect(screen.getByText('echo hello')).toBeInTheDocument()
    expect(screen.getByText('Output')).toBeInTheDocument()
  })

  it('renders condition result and then/else branches for a condition step', () => {
    const step: Step = {
      id: 'c1', name: 'check-env', action: 'condition', status: 'success',
      expression: '{{.env}} == prod', condition_result: true,
      then_children: [{ id: 't1', name: 'is-prod', status: 'success' }],
      else_children: [{ id: 'e1', name: 'not-prod', status: 'skipped' }],
    }
    renderPanel(step)
    expect(screen.getByText('✓ true')).toBeInTheDocument()
    expect(screen.getByText('Branches')).toBeInTheDocument()
    expect(screen.getByText('is-prod')).toBeInTheDocument()
    expect(screen.getByText('not-prod')).toBeInTheDocument()
  })

  it('copies the shell command to the clipboard', () => {
    const step: Step = { id: 's1', name: 'build', action: 'shell', status: 'success', shellCommand: 'make all' }
    renderPanel(step)
    // 注意：i18n 缺少 execution.copy 键，title 回退为键名（与线上行为一致）
    fireEvent.click(screen.getAllByTitle('execution.copy')[0])
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith('make all')
  })

  it('calls onClose from the close button', () => {
    const onClose = vi.fn()
    renderPanel({ id: 's1', name: 'build', action: 'log', status: 'failed', error: 'boom' }, onClose)
    expect(screen.getByText('boom')).toBeInTheDocument()
    const header = screen.getByText('build').closest('div')!
    fireEvent.click(header.querySelector('button')!)
    expect(onClose).toHaveBeenCalledTimes(1)
  })
})
