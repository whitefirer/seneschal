import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { StepListView } from './StepListView'
import type { Step } from '@/types/execution'

const tree: Step[] = [
  {
    id: 's1',
    name: 'build',
    action: 'shell',
    status: 'success',
    startTime: '2026-01-01T00:00:00Z',
    endTime: '2026-01-01T00:00:05Z',
  },
  {
    id: 's2',
    name: 'par',
    action: 'parallel',
    status: 'running',
    children: [
      { id: 's2-1', name: 'child-a', action: 'shell', status: 'success' },
      { id: 's2-2', name: 'child-b', action: 'log', status: 'failed' },
    ],
  },
]

describe('StepListView', () => {
  it('renders the empty state when there are no steps', () => {
    render(<StepListView steps={[]} selectedStep={null} onSelectStep={vi.fn()} />)
    expect(screen.getByText('No steps yet')).toBeInTheDocument()
  })

  it('renders the step tree with nested children indented', () => {
    render(<StepListView steps={tree} selectedStep={null} onSelectStep={vi.fn()} />)
    expect(screen.getByText('build')).toBeInTheDocument()
    expect(screen.getByText('par')).toBeInTheDocument()
    expect(screen.getByText('child-a')).toBeInTheDocument()
    expect(screen.getByText('child-b')).toBeInTheDocument()

    // 子节点缩进 16 + 20 = 36px
    const childRow = screen.getByText('child-a').closest('[style]') as HTMLElement | null
    expect(childRow?.style.paddingLeft).toBe('36px')
    // 顶层节点耗时 5s
    expect(screen.getByText('5s')).toBeInTheDocument()
  })

  it('calls onSelectStep with the clicked step', () => {
    const onSelectStep = vi.fn()
    render(<StepListView steps={tree} selectedStep={null} onSelectStep={onSelectStep} />)
    fireEvent.click(screen.getByText('child-b'))
    expect(onSelectStep).toHaveBeenCalledWith(tree[1].children?.[1])
  })

  it('marks the selected step row with a ring class', () => {
    render(<StepListView steps={tree} selectedStep={tree[0]} onSelectStep={vi.fn()} />)
    const row = screen.getByText('build').closest('.border-l-4')
    expect(row?.className).toContain('ring-2')
  })
})
