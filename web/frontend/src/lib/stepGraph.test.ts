import { describe, it, expect } from 'vitest'
import { stepsToGraph, graphToSteps, inferLinearEdges } from './stepGraph'

describe('stepGraph 透传转换', () => {
  it('全字段 + 容器嵌套 round-trip 不丢字段', () => {
    const steps = [
      {
        name: 'build',
        action: 'shell',
        command: 'echo hi',
        shell: 'bash',
        dir: './src',
        env: { GOOS: 'linux', CGO_ENABLED: '0' },
        output_var: 'build_out',
        retry: 3,
        retry_delay: '5s',
        continue_on_error: true,
        description: '构建',
      },
      {
        name: 'check',
        action: 'condition',
        expression: '{{.env}} == "prod"',
        then: [
          { name: 'prod', action: 'log', message: 'prod', level: 'info' },
          { name: 'prod2', action: 'sleep', duration: '1s' },
        ],
        else: [{ name: 'dev', action: 'log', message: 'dev' }],
      },
      {
        name: 'lint',
        action: 'script',
        lang: 'python',
        code: 'print("lint")',
        save_output: 'lint_out',
      },
      {
        name: 'fan',
        action: 'parallel',
        steps: [
          { name: 'a', action: 'shell', command: 'echo a' },
          {
            name: 'b',
            action: 'http',
            url: 'https://example.com',
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: '{"x":1}',
            timeout: '30s',
            save_output: 'resp',
          },
        ],
      },
      {
        name: 'loop',
        action: 'foreach',
        items: ['auth', 'api', 'worker'],
        item_var: 'service',
        do: [{ name: 'deploy-svc', action: 'log', message: 'Deploy {{.service}}' }],
      },
      {
        name: 'summarize',
        action: 'ai',
        prompt: '总结 {{.build_out}}',
        system: '你是助手',
        model: 'deepseek-v4-flash',
        inputs: ['build_out'],
        save_output: 'summary',
        save_output_format: 'json',
      },
      {
        name: 'decide',
        action: 'ai_decide',
        question: '是否通过? {{.summary}}',
        save_output: 'passed',
      },
      {
        name: 'sub',
        action: 'workflow',
        source: 'deploy.yaml',
        env: { region: 'cn' },
        save_output: 'deploy_result',
      },
      {
        name: 'render',
        action: 'template',
        source: 'config.template',
        output: 'config.yaml',
      },
      {
        name: 'assign',
        action: 'set',
        value: 'computed {{.x}}',
      },
    ]

    const { nodes, edges } = stepsToGraph(steps)
    const out = graphToSteps(nodes, edges)
    expect(out).toEqual(steps)
  })

  it('DAG next/depends_on round-trip（规范形式）', () => {
    const steps = [
      { name: 'build', action: 'shell', command: 'b', next: ['test', 'lint'] },
      { name: 'test', action: 'shell', command: 't', depends_on: ['build'] },
      { name: 'lint', action: 'shell', command: 'l', depends_on: ['build'] },
    ]
    const { nodes, edges } = stepsToGraph(steps)
    expect(graphToSteps(nodes, edges)).toEqual(steps)
  })

  it('空 steps 和空容器不报错', () => {
    expect(graphToSteps(stepsToGraph([]).nodes, stepsToGraph([]).edges)).toEqual([])
    const { nodes, edges } = stepsToGraph([{ name: 'p', action: 'parallel', steps: [] }])
    expect(graphToSteps(nodes, edges)).toEqual([{ name: 'p', action: 'parallel' }])
  })

  it('线性工作流（无显式依赖）推断出顺序边', () => {
    const steps = [
      { name: 'a', action: 'log', message: '1' },
      { name: 'b', action: 'log', message: '2' },
      { name: 'c', action: 'log', message: '3' },
    ]
    const { nodes, edges } = stepsToGraph(steps)
    const all = inferLinearEdges(nodes, edges)
    const nameOf = new Map(nodes.map((n) => [n.id, n.data.name]))
    expect(all.length).toBe(2)
    expect(all.some((e) => nameOf.get(e.source) === 'a' && nameOf.get(e.target) === 'b')).toBe(true)
    expect(all.some((e) => nameOf.get(e.source) === 'b' && nameOf.get(e.target) === 'c')).toBe(true)
  })
})
