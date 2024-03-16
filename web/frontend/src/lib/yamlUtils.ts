import yaml from 'js-yaml'

export interface Workflow {
  name: string
  version?: string
  description?: string
  variables?: Record<string, string>
  steps?: any[]
}

/**
 * Parse YAML string to Workflow object
 */
export function yamlToWorkflow(yamlStr: string): Workflow {
  try {
    const doc = yaml.load(yamlStr) as any
    return {
      name: doc?.name || 'unnamed',
      version: doc?.version,
      description: doc?.description,
      variables: doc?.variables || {},
      steps: doc?.steps || [],
    }
  } catch (error) {
    // Return empty workflow on parse error
    return {
      name: 'unnamed',
      version: '1.0',
      description: '',
      variables: {},
      steps: [],
    }
  }
}

/**
 * Convert Workflow object to YAML string
 */
export function workflowToYaml(workflow: Workflow): string {
  const obj: any = {
    name: workflow.name,
    version: workflow.version || '1.0',
    description: workflow.description,
  }

  if (workflow.variables && Object.keys(workflow.variables).length > 0) {
    obj.variables = workflow.variables
  }

  if (workflow.steps && workflow.steps.length > 0) {
    // Recursively convert steps, handling 'do' field for foreach
    const convertSteps = (steps: any[]): any[] => {
      return steps.map((step) => {
        const converted: any = { ...step }
        // Handle nested steps - 'do' for foreach, 'steps' for parallel/condition
        if (step.do && Array.isArray(step.do)) {
          converted.do = convertSteps(step.do)
        }
        if (step.steps && Array.isArray(step.steps)) {
          converted.steps = convertSteps(step.steps)
        }
        return converted
      })
    }
    obj.steps = convertSteps(workflow.steps)
  }

  return yaml.dump(obj, {
    indent: 2,
    lineWidth: -1, // No line wrapping
    noRefs: true, // No YAML references
    quotingType: '"',
    forceQuotes: false,
  })
}

/**
 * Parse YAML string to generic object
 */
export function parseYaml<T = any>(yamlStr: string): T {
  return yaml.load(yamlStr) as T
}

/**
 * Convert object to YAML string
 */
export function toYaml(obj: any): string {
  return yaml.dump(obj, {
    indent: 2,
    lineWidth: -1,
    noRefs: true,
  })
}
