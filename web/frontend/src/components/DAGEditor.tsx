// DAG 编辑器 - 模式切换（YAML 文本 ↔ 图形）
import { useState, useCallback, useEffect, useMemo } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import Editor from '@monaco-editor/react'
import { Save, Play, Check, Code2, GitGraph, ArrowLeft } from 'lucide-react'
import { ReactFlowProvider } from '@xyflow/react'
import { WorkflowYAML } from '../types/dag'
import DAGGraphEditor from './DAGGraphEditor'
import { workflowsApi } from '@/api/client'
import yaml from 'js-yaml'

export default function DAGEditor() {
  const { name } = useParams<{ name: string }>()
  const navigate = useNavigate()
  const isNew = name === 'new' || name === undefined
  
  const [content, setContent] = useState('')
  const [fileName, setFileName] = useState('')
  const [loading, setLoading] = useState(true)
  const [mode, setMode] = useState<'yaml' | 'graph'>('yaml')
  const [saving, setSaving] = useState(false)
  const [running, setRunning] = useState(false)
  const [validationError, setValidationError] = useState<string | null>(null)
  const [workflowMetadata, setWorkflowMetadata] = useState<any>({})
  
  // 加载工作流
  useEffect(() => {
    const loadWorkflow = async () => {
      if (!isNew) {
        try {
          const workflow = await workflowsApi.get(name!)
          setContent(workflow.content)
          setFileName(workflow.fileName)
          
          // 解析并保存工作流元数据
          try {
            const yamlObj = yaml.load(workflow.content) as any
            setWorkflowMetadata({
              name: yamlObj.name,
              version: yamlObj.version,
              description: yamlObj.description,
              variables: yamlObj.variables,
              env: yamlObj.env,
            })
          } catch {
            // YAML 解析失败时保持编辑器现有内容
          }
        } catch (error: any) {
          alert(`Failed to load: ${error.message}`)
          navigate('/')
        }
      } else {
        // 新建工作流的默认 YAML
        setContent(`name: new-workflow
version: "1.0"
description: "My new DAG workflow"

steps:
  - name: start
    action: log
    message: "Hello, DAG!"
`)
        setFileName('new-workflow.yaml')
        setWorkflowMetadata({
          name: 'new-workflow',
          version: '1.0',
          description: 'My new DAG workflow',
        })
      }
      setLoading(false)
    }
    loadWorkflow()
  }, [name, isNew, navigate])
  
  // 从 YAML 解析 steps（用于图形模式）
  const graphSteps = useMemo(() => {
    if (!content) return []
    try {
      const yamlObj = yaml.load(content) as any
      return yamlObj?.steps || []
    } catch {
      return []
    }
  }, [content])
  
  // 保存工作流
  const saveWorkflow = useCallback(async () => {
    setSaving(true)
    try {
      let saveName = fileName.replace('.yaml', '').replace('.yml', '')
      if (!saveName) saveName = 'workflow'
      
      await workflowsApi.save(saveName, content)
      setValidationError(null)
      
      if (isNew) {
        navigate(`/dag/${saveName}`)
      }
    } catch (error: any) {
      alert(`Failed to save: ${error.message}`)
    } finally {
      setSaving(false)
    }
  }, [fileName, content, isNew, navigate])
  
  // 运行工作流
  const runWorkflow = useCallback(async () => {
    setRunning(true)
    try {
      let saveName = fileName.replace('.yaml', '').replace('.yml', '')
      if (!saveName) saveName = 'workflow'
      
      await workflowsApi.save(saveName, content)
      const res = await workflowsApi.run(saveName)
      navigate(`/execution/${res.executionId}`)
    } catch (error: any) {
      alert(`Failed to run: ${error.message}`)
    } finally {
      setRunning(false)
    }
  }, [fileName, content, navigate])
  
  // 验证工作流
  const validateWorkflow = useCallback(() => {
    try {
      const yamlObj = yaml.load(content) as any
      if (!yamlObj.steps || !Array.isArray(yamlObj.steps)) {
        throw new Error('Missing or invalid "steps" field')
      }
      setValidationError(null)
      alert('Validation passed! ✅')
    } catch (error: any) {
      setValidationError(error.message)
    }
  }, [content])
  
  // 图形模式保存回调
  const handleGraphSave = useCallback((yamlObj: WorkflowYAML) => {
    try {
      // 保留工作流元数据
      const fullYaml = {
        ...workflowMetadata,
        steps: yamlObj.steps,
      }
      // 先生成标准 YAML
      let newYaml = yaml.dump(fullYaml, {
        lineWidth: -1,
        indent: 2,
        quotingType: '"',
        forceQuotes: false,
      })
      
      // 手动将 next/depends_on 转换为单行数组格式
      // 修复：正确识别数组元素，避免收集下一行的 step 定义
      const lines = newYaml.split('\n')
      const resultLines: string[] = []
      let i = 0
      while (i < lines.length) {
        const line = lines[i]
        // 检查是否是 next 或 depends_on 字段（多行数组格式）
        const match = line.match(/^(\s+)(next|depends_on):\s*$/)
        if (match) {
          const indent = match[1]
          const fieldName = match[2]
          // 收集后续的数组元素（只收集纯值，不收集包含冒号的行）
          const items: string[] = []
          i++
          while (i < lines.length) {
            const itemLine = lines[i]
            const itemMatch = itemLine.match(/^\s+-\s+(.+)$/)
            if (itemMatch) {
              const itemValue = itemMatch[1]
              // 只接受不包含冒号的纯值（避免收集 "- name: xxx"）
              if (!itemValue.includes(':')) {
                items.push(itemValue)
              } else {
                // 遇到包含冒号的行，说明是下一个字段，停止收集
                break
              }
            } else {
              // 不是数组元素行，停止收集
              break
            }
            i++
          }
          // 转换为单行格式
          if (items.length > 0) {
            resultLines.push(`${indent}${fieldName}: [${items.join(', ')}]`)
          } else {
            // 没有有效数组元素，保留原行
            resultLines.push(line)
          }
        } else {
          resultLines.push(line)
          i++
        }
      }
      newYaml = resultLines.join('\n')
      
      setContent(newYaml)
      setMode('yaml')
      setTimeout(() => saveWorkflow(), 100)
    } catch (error: any) {
      alert(`Failed to save graph: ${error.message}`)
    }
  }, [workflowMetadata, saveWorkflow])
  
  // 图形模式运行回调
  const handleGraphRun = useCallback((yamlObj: WorkflowYAML) => {
    try {
      // 保留工作流元数据
      const fullYaml = {
        ...workflowMetadata,
        steps: yamlObj.steps,
      }
      let newYaml = yaml.dump(fullYaml, {
        lineWidth: -1,
        indent: 2,
      })
      // 手动将 next/depends_on 转换为单行数组格式
      const lines = newYaml.split('\n')
      const resultLines: string[] = []
      let i = 0
      while (i < lines.length) {
        const line = lines[i]
        const match = line.match(/^(\s+)(next|depends_on):\s*$/)
        if (match) {
          const indent = match[1]
          const fieldName = match[2]
          const items: string[] = []
          i++
          while (i < lines.length && lines[i].match(/^\s+-\s+/)) {
            const itemMatch = lines[i].match(/^\s+-\s+(.+)$/)
            if (itemMatch) {
              items.push(itemMatch[1])
            }
            i++
          }
          if (items.length > 0) {
            resultLines.push(`${indent}${fieldName}: [${items.join(', ')}]`)
          }
        } else {
          resultLines.push(line)
          i++
        }
      }
      newYaml = resultLines.join('\n')
      
      setContent(newYaml)
      setMode('yaml')
      setTimeout(() => runWorkflow(), 100)
    } catch (error: any) {
      alert(`Failed to run graph: ${error.message}`)
    }
  }, [workflowMetadata, runWorkflow])
  
  // Monaco Editor 挂载
  const handleEditorMount = useCallback((editor: any, monaco: any) => {
    // Ctrl/Cmd + S 保存
    editor.addCommand(
      monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyS,
      () => saveWorkflow()
    )
  }, [saveWorkflow])
  
  // 内容变化处理
  const handleContentChange = useCallback((value: string | undefined) => {
    if (value === undefined) {
      setContent('')
    } else {
      setContent(value.replace(/\r\n/g, '\n'))
    }
  }, [])
  
  if (loading) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="text-center">
          <div className="animate-spin rounded-full h-12 w-12 border-4 border-blue-500 border-t-transparent mx-auto mb-4"></div>
          <div className="text-gray-500">Loading DAG Editor...</div>
        </div>
      </div>
    )
  }
  
  return (
    <div className="h-full overflow-hidden flex flex-col">
      {/* 工具栏 */}
      <div className="flex-shrink-0 px-4 py-3 border-b">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-4">
            <button
              onClick={() => navigate('/')}
              className="p-2 hover:bg-accent rounded-md transition-colors"
            >
              <ArrowLeft className="h-5 w-5" />
            </button>
            <div>
              <input
                type="text"
                value={fileName}
                onChange={(e) => setFileName(e.target.value)}
                className="text-lg font-semibold bg-transparent border-none focus:outline-none focus:ring-2 focus:ring-primary/20 rounded px-2 py-1"
                placeholder="workflow.yaml"
              />
            </div>
          </div>
          
          <div className="flex items-center gap-2">
            {/* 模式切换 */}
            <div className="flex items-center border rounded-md p-1">
              <button
                onClick={() => setMode('yaml')}
                className={`flex items-center gap-2 px-3 py-1.5 rounded ${
                  mode === 'yaml' ? 'bg-accent' : ''
                }`}
                title="Switch to YAML mode"
              >
                <Code2 className="h-4 w-4" />
                YAML
              </button>
              <button
                onClick={() => setMode('graph')}
                className={`flex items-center gap-2 px-3 py-1.5 rounded ${
                  mode === 'graph' ? 'bg-accent' : ''
                }`}
                title="Switch to Graph mode"
              >
                <GitGraph className="h-4 w-4" />
                Graph
              </button>
            </div>
            
            {/* 验证按钮 */}
            <button
              onClick={validateWorkflow}
              className="flex items-center gap-2 px-4 py-2 border rounded-md hover:bg-accent transition-colors"
            >
              <Check className="h-4 w-4" />
              Validate
            </button>
            
            {/* 保存按钮 */}
            <button
              onClick={saveWorkflow}
              disabled={saving}
              className="flex items-center gap-2 px-4 py-2 bg-primary text-primary-foreground rounded-md hover:bg-primary/90 transition-colors disabled:opacity-50"
            >
              <Save className="h-4 w-4" />
              {saving ? 'Loading...' : 'Save'}
            </button>
            
            {/* 运行按钮 */}
            <button
              onClick={runWorkflow}
              disabled={running || isNew}
              className="flex items-center gap-2 px-4 py-2 bg-green-600 text-white rounded-md hover:bg-green-700 transition-colors disabled:opacity-50"
            >
              <Play className="h-4 w-4" />
              {running ? 'Running...' : 'Run'}
            </button>
          </div>
        </div>
      </div>
      
      {/* 验证错误提示 */}
      {validationError && (
        <div className="p-4 bg-destructive/10 border border-destructive rounded-md text-destructive">
          {validationError}
        </div>
      )}
      
      {/* 编辑器区域 */}
      <div className="flex-1 overflow-hidden">
        {mode === 'yaml' ? (
          // YAML 文本模式
          <Editor
            height="100%"
            language="yaml"
            value={content}
            onChange={handleContentChange}
            onMount={handleEditorMount}
            theme="vs-light"
            options={{
              minimap: { enabled: false },
              fontSize: 14,
              lineNumbers: 'on',
              scrollBeyondLastLine: false,
              automaticLayout: true,
              tabSize: 2,
              renderLineHighlight: 'all',
              lineHeight: 20,
              formatOnPaste: true,
              detectIndentation: true,
            }}
          />
        ) : (
          // 图形模式
          <div className="w-full h-full">
            <ReactFlowProvider>
              <DAGGraphEditor
                initialSteps={graphSteps}
                onSave={handleGraphSave}
                onRun={handleGraphRun}
              />
            </ReactFlowProvider>
          </div>
        )}
      </div>
    </div>
  )
}
