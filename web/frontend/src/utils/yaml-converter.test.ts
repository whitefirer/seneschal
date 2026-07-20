import { describe, it, expect } from 'vitest'
import { hasContainerStructure, isContainerNode, yamlToDAG } from './yaml-converter'
import type { GraphNodeData } from '../types/graph'

describe('hasContainerStructure', () => {
  it('is false for flat step lists', () => {
    expect(hasContainerStructure([{ name: 'a', action: 'shell' }])).toBe(false)
  })

  it('is true when a step carries branch or children fields', () => {
    expect(hasContainerStructure([{ name: 'c', action: 'condition', then: [] }])).toBe(true)
  })
})

describe('isContainerNode', () => {
  it('recognizes container actions only', () => {
    const data = (action: string): GraphNodeData => ({ name: 'x', action })
    for (const action of ['condition', 'parallel', 'foreach', 'loop']) {
      expect(isContainerNode(data(action))).toBe(true)
    }
    expect(isContainerNode(data('shell'))).toBe(false)
  })
})

describe('yamlToDAG', () => {
  it('flattens condition branches into child nodes with parent metadata', () => {
    const nodes = yamlToDAG({
      steps: [
        {
          name: 'check',
          action: 'condition',
          then: [{ name: 'yes', action: 'log' }],
          else: [{ name: 'no', action: 'log' }],
        },
      ],
    })
    // Children are created before their parent (recursive post-order).
    expect(nodes.map((n) => n.id)).toEqual(['yes', 'no', 'check'])
    const yes = nodes.find((n) => n.id === 'yes')!
    expect(yes.data.parentId).toBe('check')
    expect(yes.data.branchType).toBe('then')
    expect(yes.data.branchIndex).toBe(0)
    const no = nodes.find((n) => n.id === 'no')!
    expect(no.data.parentId).toBe('check')
    expect(no.data.branchType).toBe('else')
  })
})
