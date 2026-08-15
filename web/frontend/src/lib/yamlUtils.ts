import yaml from 'js-yaml'

export interface WorkflowStep {
  name?: string
  id?: string
  action?: string
  then?: WorkflowStep[]
  else?: WorkflowStep[]
  steps?: WorkflowStep[]
  do?: WorkflowStep[]
  [key: string]: unknown
}

export interface Workflow {
  name: string
  version?: string
  description?: string
  variables?: Record<string, string>
  steps?: WorkflowStep[]
}

/**
 * Parse YAML string to Workflow object
 */
export function yamlToWorkflow(yamlStr: string): Workflow {
  try {
    const doc = (yaml.load(yamlStr) ?? {}) as Record<string, unknown>
    return {
      name: typeof doc.name === 'string' ? doc.name : 'unnamed',
      version: typeof doc.version === 'string' ? doc.version : undefined,
      description: typeof doc.description === 'string' ? doc.description : undefined,
      variables:
        doc.variables && typeof doc.variables === 'object'
          ? (doc.variables as Record<string, string>)
          : {},
      steps: Array.isArray(doc.steps) ? (doc.steps as WorkflowStep[]) : [],
    }
  } catch {
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
  const obj: Record<string, unknown> = {
    name: workflow.name,
    version: workflow.version || '1.0',
    description: workflow.description,
  }

  if (workflow.variables && Object.keys(workflow.variables).length > 0) {
    obj.variables = workflow.variables
  }

  if (workflow.steps && workflow.steps.length > 0) {
    // Recursively convert steps, handling 'do' field for foreach
    const convertSteps = (steps: WorkflowStep[]): WorkflowStep[] => {
      return steps.map((step) => {
        const converted: WorkflowStep = { ...step }
        // Handle nested steps - 'do' for foreach, 'steps' for parallel/condition
        if (Array.isArray(step.do)) {
          converted.do = convertSteps(step.do)
        }
        if (Array.isArray(step.steps)) {
          converted.steps = convertSteps(step.steps)
        }
        if (Array.isArray(step.then)) {
          converted.then = convertSteps(step.then)
        }
        if (Array.isArray(step.else)) {
          converted.else = convertSteps(step.else)
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
export function parseYaml<T = unknown>(yamlStr: string): T {
  return yaml.load(yamlStr) as T
}

/**
 * Convert object to YAML string
 */
export function toYaml(obj: unknown): string {
  return yaml.dump(obj, {
    indent: 2,
    lineWidth: -1,
    noRefs: true,
  })
}
