import { describe, it, expect } from 'vitest'
import { yamlToWorkflow, workflowToYaml, parseYaml } from './yamlUtils'

describe('yamlToWorkflow', () => {
  it('parses a full workflow document', () => {
    const wf = yamlToWorkflow(
      'name: deploy\nversion: "1.0"\nvariables:\n  env: prod\nsteps:\n  - name: a\n    action: log\n'
    )
    expect(wf.name).toBe('deploy')
    expect(wf.variables).toEqual({ env: 'prod' })
    expect(wf.steps).toHaveLength(1)
  })

  it('fills defaults for a minimal document', () => {
    const wf = yamlToWorkflow('steps: []')
    expect(wf.name).toBe('unnamed')
    expect(wf.variables).toEqual({})
    expect(wf.steps).toEqual([])
  })

  it('returns an empty workflow on invalid YAML', () => {
    const wf = yamlToWorkflow('name: [unclosed')
    expect(wf.name).toBe('unnamed')
    expect(wf.version).toBe('1.0')
    expect(wf.steps).toEqual([])
  })
})

describe('workflowToYaml', () => {
  it('round-trips name, variables and steps', () => {
    const out = workflowToYaml({
      name: 'wf',
      variables: { a: '1' },
      steps: [{ name: 's', action: 'log' }],
    })
    const parsed = parseYaml<{ name: string; variables: Record<string, string>; steps: unknown[] }>(out)
    expect(parsed.name).toBe('wf')
    expect(parsed.variables).toEqual({ a: '1' })
    expect(parsed.steps).toHaveLength(1)
  })

  it('omits the variables key when empty', () => {
    const out = workflowToYaml({ name: 'wf', variables: {}, steps: [] })
    expect(out).not.toContain('variables')
  })
})
