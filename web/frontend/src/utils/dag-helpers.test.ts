import { describe, it, expect } from 'vitest'
import type { Edge } from '@xyflow/react'
import { buildDAGEdges, detectCycles } from './dag-helpers'
import type { GraphNode, GraphNodeData } from '../types/graph'

const node = (id: string, data: Partial<GraphNodeData> = {}): GraphNode => ({
  id,
  type: 'dag',
  position: { x: 0, y: 0 },
  data: { name: id, action: 'shell', ...data },
})

describe('buildDAGEdges', () => {
  it('creates an edge for each next reference', () => {
    const edges = buildDAGEdges([node('a', { next: ['b'] }), node('b')])
    expect(edges).toHaveLength(1)
    expect(edges[0]).toMatchObject({ id: 'a-b', source: 'a', target: 'b' })
  })

  it('creates an edge for each depends_on reference', () => {
    const edges = buildDAGEdges([node('a'), node('b', { depends_on: ['a'] })])
    expect(edges).toHaveLength(1)
    expect(edges[0]).toMatchObject({ source: 'a', target: 'b' })
  })

  it('does not duplicate edges already present', () => {
    const existing: Edge[] = [{ id: 'a-b', source: 'a', target: 'b' }]
    const edges = buildDAGEdges(
      [node('a', { next: ['b'] }), node('b', { depends_on: ['a'] })],
      existing
    )
    expect(edges).toHaveLength(0)
  })
})

describe('detectCycles', () => {
  it('returns no cycles for a linear chain', () => {
    const nodes = [node('a', { next: ['b'] }), node('b', { next: ['c'] }), node('c')]
    expect(detectCycles(nodes)).toEqual([])
  })

  it('detects a two-node cycle', () => {
    const cycles = detectCycles([node('a', { next: ['b'] }), node('b', { next: ['a'] })])
    expect(cycles).toHaveLength(1)
    expect(cycles[0]).toContain('a')
    expect(cycles[0]).toContain('b')
  })
})
