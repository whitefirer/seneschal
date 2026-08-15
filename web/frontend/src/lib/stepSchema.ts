// 统一 step 字段 schema，对齐后端 workflow.Step（workflow/workflow.go）。
// 数据驱动：节点 UI 和转换层都从这份定义派生，新增 action 只需加一条。

export type FieldType = 'text' | 'textarea' | 'select' | 'number' | 'bool' | 'kv' | 'list'

export interface FieldOption {
  value: string
  label: string
}

export interface FieldDef {
  key: string // step 对象的字段名（即 YAML key）
  label: string
  type: FieldType
  placeholder?: string
  options?: FieldOption[] // 仅 select
}

export interface ActionDef {
  value: string // action 名
  label: string
  container?: 'condition' | 'parallel' | 'foreach' // 容器：子节点用图形节点表达
  fields: FieldDef[]
}

const METHOD_OPTIONS: FieldOption[] = [
  { value: 'GET', label: 'GET' },
  { value: 'POST', label: 'POST' },
  { value: 'PUT', label: 'PUT' },
  { value: 'DELETE', label: 'DELETE' },
]

const SHELL_OPTIONS: FieldOption[] = [
  { value: 'sh', label: 'sh' },
  { value: 'bash', label: 'bash' },
  { value: 'cmd', label: 'cmd' },
  { value: 'powershell', label: 'powershell' },
]

const LEVEL_OPTIONS: FieldOption[] = [
  { value: 'info', label: 'info' },
  { value: 'warn', label: 'warn' },
  { value: 'error', label: 'error' },
]

const LANG_OPTIONS: FieldOption[] = [
  { value: 'python', label: 'python' },
  { value: 'python3', label: 'python3' },
  { value: 'node', label: 'node' },
  { value: 'ruby', label: 'ruby' },
  { value: 'lua', label: 'lua' },
  { value: 'perl', label: 'perl' },
]

const ON_ERROR_OPTIONS: FieldOption[] = [
  { value: '', label: 'off' },
  { value: 'ai', label: 'ai (suggest)' },
  { value: 'ai_auto', label: 'ai_auto' },
]

export const ACTION_DEFS: ActionDef[] = [
  {
    value: 'log', label: 'Log',
    fields: [
      { key: 'message', label: 'Message', type: 'textarea', placeholder: '支持 {{.var}} 模板' },
      { key: 'level', label: 'Level', type: 'select', options: LEVEL_OPTIONS },
    ],
  },
  {
    value: 'shell', label: 'Shell',
    fields: [
      { key: 'command', label: 'Command', type: 'textarea', placeholder: 'echo hello' },
      { key: 'shell', label: 'Shell', type: 'select', options: SHELL_OPTIONS },
      { key: 'dir', label: 'Working dir', type: 'text', placeholder: './src' },
      { key: 'env', label: 'Env', type: 'kv' },
      { key: 'output_var', label: 'Output var', type: 'text' },
    ],
  },
  {
    value: 'http', label: 'HTTP',
    fields: [
      { key: 'url', label: 'URL', type: 'text', placeholder: 'https://...' },
      { key: 'method', label: 'Method', type: 'select', options: METHOD_OPTIONS },
      { key: 'headers', label: 'Headers', type: 'kv' },
      { key: 'body', label: 'Body', type: 'textarea' },
      { key: 'timeout', label: 'Timeout', type: 'text', placeholder: '30s' },
      { key: 'save_output', label: 'Save output to', type: 'text' },
    ],
  },
  {
    value: 'condition', label: 'Condition', container: 'condition',
    fields: [
      { key: 'expression', label: 'Expression', type: 'textarea', placeholder: '{{.env}} == "prod"' },
    ],
  },
  {
    value: 'parallel', label: 'Parallel', container: 'parallel',
    fields: [],
  },
  {
    value: 'foreach', label: 'Foreach', container: 'foreach',
    fields: [
      { key: 'items', label: 'Items', type: 'list', placeholder: '每行一个；或填变量名' },
      { key: 'item_var', label: 'Item var', type: 'text', placeholder: 'item' },
    ],
  },
  {
    value: 'loop', label: 'Loop', container: 'foreach',
    fields: [
      { key: 'items', label: 'Items', type: 'list' },
      { key: 'item_var', label: 'Item var', type: 'text' },
    ],
  },
  {
    value: 'set', label: 'Set',
    fields: [
      { key: 'value', label: 'Value', type: 'text', placeholder: '{{.other_var}}' },
    ],
  },
  {
    value: 'sleep', label: 'Sleep',
    fields: [
      { key: 'duration', label: 'Duration', type: 'text', placeholder: '5s' },
    ],
  },
  {
    value: 'template', label: 'Template',
    fields: [
      { key: 'source', label: 'Source', type: 'text', placeholder: 'config.template' },
      { key: 'output', label: 'Output', type: 'text', placeholder: 'config.yaml' },
    ],
  },
  {
    value: 'script', label: 'Script',
    fields: [
      { key: 'lang', label: 'Lang', type: 'select', options: LANG_OPTIONS },
      { key: 'code', label: 'Code', type: 'textarea' },
      { key: 'save_output', label: 'Save output to', type: 'text' },
    ],
  },
  {
    value: 'ai', label: 'AI',
    fields: [
      { key: 'prompt', label: 'Prompt', type: 'textarea' },
      { key: 'system', label: 'System', type: 'textarea' },
      { key: 'model', label: 'Model', type: 'text' },
      { key: 'inputs', label: 'Inputs', type: 'list', placeholder: '每行一个变量名' },
      { key: 'save_output', label: 'Save output to', type: 'text' },
      { key: 'save_output_format', label: 'Output format', type: 'select', options: [{ value: 'json', label: 'json' }] },
    ],
  },
  {
    value: 'ai_decide', label: 'AI Decide',
    fields: [
      { key: 'question', label: 'Question', type: 'textarea' },
      { key: 'system', label: 'System', type: 'textarea' },
      { key: 'model', label: 'Model', type: 'text' },
      { key: 'save_output', label: 'Save output to', type: 'text' },
    ],
  },
  {
    value: 'workflow', label: 'Sub-workflow',
    fields: [
      { key: 'source', label: 'Path', type: 'text', placeholder: 'deploy.yaml' },
      { key: 'env', label: 'Variables', type: 'kv' },
      { key: 'save_output', label: 'Save output to', type: 'text' },
    ],
  },
]

// 所有 action 共有的字段（节点 UI 的「高级」折叠区）
export const COMMON_FIELDS: FieldDef[] = [
  { key: 'description', label: 'Description', type: 'text' },
  { key: 'continue_on_error', label: 'Continue on error', type: 'bool' },
  { key: 'retry', label: 'Retry', type: 'number' },
  { key: 'retry_delay', label: 'Retry delay', type: 'text', placeholder: '5s' },
  { key: 'on_error', label: 'On error', type: 'select', options: ON_ERROR_OPTIONS },
]

export function actionDef(action: string): ActionDef | undefined {
  return ACTION_DEFS.find((a) => a.value === action)
}

// 容器子字段名（用于 stepsToGraph / graphToSteps 递归）
export const CONTAINER_CHILDREN: Record<string, string[]> = {
  condition: ['then', 'else'],
  parallel: ['steps'],
  foreach: ['do'],
  loop: ['do'],
}
