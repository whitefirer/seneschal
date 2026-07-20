import { useState, useEffect, useCallback, useMemo } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import MonacoEditor, { OnMount } from '@monaco-editor/react'
import { Save, Play, Trash2, ArrowLeft, Check, Eye, Code2 } from 'lucide-react'
import { workflowsApi } from '@/api/client'
import { useThemeStore } from '@/store/themeStore'
import WorkflowGraphEditor from '@/components/WorkflowGraphEditor'
import { workflowToYaml, yamlToWorkflow } from '@/lib/yamlUtils'
import { registerMonacoThemes } from '@/lib/monacoThemes'
import { configureMonacoLoader, logCDNSelection } from '@/lib/cdnSelector'

// Configure Monaco to load from optimal CDN with auto-fallback
configureMonacoLoader()
logCDNSelection()

export default function Editor() {
  const { t } = useTranslation()
  const { name } = useParams<{ name: string }>()
  const navigate = useNavigate()
  const isNew = name === 'new'
  const isDark = useThemeStore((state) => state.isDark)

  const [content, setContent] = useState('')
  const [fileName, setFileName] = useState('')
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [running, setRunning] = useState(false)
  const [mode, setMode] = useState<'yaml' | 'graph'>('yaml')
  const [validationError, setValidationError] = useState<string | null>(null)
  const [editorTheme, setEditorTheme] = useState<'dark' | 'light'>(
    isDark ? 'dark' : 'light'
  )

  // Sync editor theme with app theme in real-time
  useEffect(() => {
    setEditorTheme(isDark ? 'dark' : 'light')
  }, [isDark])

  useEffect(() => {
    if (!isNew) {
      loadWorkflow()
    } else {
      setContent(`name: new-workflow
version: "1.0"
description: "My new workflow"

variables:
  env: development

steps:
  - name: greet
    action: log
    message: "Hello, World!"
    level: info
`)
      setFileName('new-workflow.yaml')
      setLoading(false)
    }
  }, [name])

  const loadWorkflow = async () => {
    try {
      const wf = await workflowsApi.get(name!)
      setContent(wf.content)
      setFileName(wf.fileName)
    } catch (error: any) {
      alert(`${t('editor.loadFailed')}: ${error.message}`)
      navigate('/')
    } finally {
      setLoading(false)
    }
  }

  const saveWorkflow = async () => {
    setSaving(true)
    try {
      let saveName = fileName.replace('.yaml', '').replace('.yml', '')
      if (!saveName) saveName = 'workflow'
      
      await workflowsApi.save(saveName, content)
      setValidationError(null)
      
      // If new file, redirect to the new name
      if (isNew) {
        navigate(`/editor/${saveName}`)
      }
    } catch (error: any) {
      // 显示详细错误信息
      let errorMsg = error.message || 'Save failed'
      
      // 尝试解析后端返回的详细错误（YAML 解析错误等）
      try {
        const errData = JSON.parse(errorMsg)
        if (errData.error) {
          errorMsg = errData.error
        }
        if (errData.line) {
          errorMsg += `\n行号：${errData.line}`
        }
        if (errData.column) {
          errorMsg += `\n列：${errData.column}`
        }
      } catch {
        // 不是 JSON，直接使用原始消息
      }
      
      setValidationError(errorMsg)
      alert(`保存失败:\n\n${errorMsg}`)
    } finally {
      setSaving(false)
    }
  }

  const runWorkflow = async () => {
    // First save
    await saveWorkflow()
    if (validationError) return

    setRunning(true)
    try {
      const saveName = fileName.replace('.yaml', '').replace('.yml', '')
      const res = await workflowsApi.run(saveName)
      navigate(`/execution/${res.executionId}`)
    } catch (error: any) {
      // 显示详细错误信息
      let errorMsg = error.message || t('editor.runFailedError')
      
      // 尝试解析后端返回的详细错误
      try {
        const errData = JSON.parse(errorMsg)
        if (errData.error) {
          errorMsg = errData.error
        }
        if (errData.node) {
          errorMsg += `\n\n出错节点：${errData.node}`
        }
        if (errData.step) {
          errorMsg += `\n步骤索引：${errData.step}`
        }
      } catch {
        // 不是 JSON，直接使用原始消息
      }
      
      alert(`${t('editor.runFailedError')}:\n\n${errorMsg}`)
    } finally {
      setRunning(false)
    }
  }

  const validateWorkflow = async () => {
    try {
      const saveName = fileName.replace('.yaml', '').replace('.yml', '')
      await workflowsApi.save(saveName, content)
      const result = await workflowsApi.validate(saveName)
      alert(`✅ ${t('editor.validWorkflow')}\n\n${t('editor.steps')}: ${result.steps}\n${t('editor.variables')}: ${result.variables}`)
      setValidationError(null)
    } catch (error: any) {
      // 显示详细验证错误
      let errorMsg = error.message || t('editor.invalidWorkflow')
      
      // 尝试解析后端返回的详细错误
      try {
        const errData = JSON.parse(errorMsg)
        if (errData.error) {
          errorMsg = errData.error
        }
        if (errData.line) {
          errorMsg += `\n行号：${errData.line}`
        }
        if (errData.field) {
          errorMsg += `\n字段：${errData.field}`
        }
      } catch {
        // 不是 JSON，直接使用原始消息
      }
      
      alert(`❌ ${t('editor.invalidWorkflow')}:\n\n${errorMsg}`)
      setValidationError(errorMsg)
    }
  }

  const deleteWorkflow = async () => {
    if (isNew || !confirm(`Delete "${fileName}"?`)) return
    try {
      await workflowsApi.delete(fileName)
      navigate('/')
    } catch (error: any) {
      alert(`Failed to delete: ${error.message}`)
    }
  }

  const handleEditorMount: OnMount = useCallback((editor, monaco) => {
    // Register custom themes
    registerMonacoThemes(monaco)
    
    editor.addCommand(
      // Ctrl/Cmd + S to save
      monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyS,
      () => {
        saveWorkflow()
      }
    )
  }, [saveWorkflow])

  // Handle content change - normalize line endings
  const handleContentChange = useCallback((value: string | undefined) => {
    if (value === undefined) {
      setContent('')
    } else {
      // Normalize line endings to Unix style (\n)
      setContent(value.replace(/\r\n/g, '\n'))
    }
  }, [])

  // Graph editor functions
  // 注意：WorkflowGraphEditor 传入的是 { steps } 对象而非步骤数组（既有调用形状）。
  // 下方把它赋给 workflow.steps 后，workflowToYaml 只处理数组、对象会被静默跳过——
  // 本次为行为保持的重构，仅按实际形状修正类型，不修复该问题（见任务报告）。
  const handleGraphSave = useCallback((payload: { steps: any[] }) => {
    try {
      const workflow = yamlToWorkflow(content)
      workflow.steps = payload as unknown as any[]
      const newYaml = workflowToYaml(workflow)
      setContent(newYaml)
      setMode('yaml')
      setTimeout(() => saveWorkflow(), 100)
    } catch (error: any) {
      // 显示详细错误信息
      let errorMsg = error.message || 'Failed to save graph'
      
      // 尝试解析错误中的节点信息
      try {
        const errData = JSON.parse(errorMsg)
        if (errData.error) {
          errorMsg = errData.error
        }
        if (errData.node) {
          errorMsg += `\n\n节点：${errData.node}`
        }
      } catch {
        // 不是 JSON，直接使用原始消息
      }
      
      alert(`Failed to save graph:\n\n${errorMsg}`)
    }
  }, [content, saveWorkflow])

  const handleGraphRun = useCallback((payload: { steps: any[] }) => {
    try {
      const workflow = yamlToWorkflow(content)
      workflow.steps = payload as unknown as any[]
      const newYaml = workflowToYaml(workflow)
      setContent(newYaml)
      setMode('yaml')
      setTimeout(() => runWorkflow(), 100)
    } catch (error: any) {
      // 显示详细错误信息
      let errorMsg = error.message || 'Failed to run from graph'
      
      // 尝试解析错误中的节点信息
      try {
        const errData = JSON.parse(errorMsg)
        if (errData.error) {
          errorMsg = errData.error
        }
        if (errData.node) {
          errorMsg += `\n\n节点：${errData.node}`
        }
      } catch {
        // 不是 JSON，直接使用原始消息
      }
      
      alert(`Failed to run from graph:\n\n${errorMsg}`)
    }
  }, [content, runWorkflow])

  // Parse steps from YAML for graph editor/viewer
  const graphSteps = useMemo(() => {
    try {
      const workflow = yamlToWorkflow(content)
      return workflow.steps || []
    } catch {
      return []
    }
  }, [content])

  if (loading) {
    return <div className="flex items-center justify-center h-64">Loading...</div>
  }

  return (
    <div className="h-full overflow-hidden flex flex-col">
      {/* Toolbar */}
      <div className="flex-shrink-0 px-4 py-3 border-b dark:border-gray-700">
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
          {/* Mode Toggle */}
          <div className="flex items-center border rounded-md p-1">
            <button
              onClick={() => setMode('yaml')}
              className={`flex items-center gap-2 px-3 py-1.5 rounded ${
                mode === 'yaml' ? 'bg-accent' : ''
              }`}
              title={t('editor.switchToYaml')}
            >
              <Code2 className="h-4 w-4" />
              {t('editor.yamlMode')}
            </button>
            <button
              onClick={() => setMode('graph')}
              className={`flex items-center gap-2 px-3 py-1.5 rounded ${
                mode === 'graph' ? 'bg-accent' : ''
              }`}
              title={t('editor.switchToGraph')}
            >
              <Eye className="h-4 w-4" />
              {t('editor.graphMode')}
            </button>
          </div>

          <button
            onClick={validateWorkflow}
            className="flex items-center gap-2 px-4 py-2 border rounded-md hover:bg-accent transition-colors"
          >
            <Check className="h-4 w-4" />
            {t('common.validate')}
          </button>

          <button
            onClick={saveWorkflow}
            disabled={saving}
            className="flex items-center gap-2 px-4 py-2 bg-primary text-primary-foreground rounded-md hover:bg-primary/90 transition-colors disabled:opacity-50"
          >
            <Save className="h-4 w-4" />
            {saving ? t('common.loading') : t('common.save')}
          </button>

          <button
            onClick={runWorkflow}
            disabled={running || isNew}
            className="flex items-center gap-2 px-4 py-2 bg-green-600 text-white rounded-md hover:bg-green-700 transition-colors disabled:opacity-50"
          >
            <Play className="h-4 w-4" />
            {running ? t('execution.running') : t('common.run')}
          </button>

          {!isNew && (
            <button
              onClick={deleteWorkflow}
              className="p-2 border rounded-md hover:bg-destructive hover:text-destructive-foreground transition-colors"
              title={t('common.delete')}
            >
              <Trash2 className="h-4 w-4" />
            </button>
          )}
        </div>
      </div>
      </div>

      {/* Validation Error */}
      {validationError && (
        <div className="p-4 bg-destructive/10 border border-destructive rounded-md text-destructive">
          {validationError}
        </div>
      )}

      {/* Editor */}
      <div className="flex-1 overflow-hidden">
        {mode === 'yaml' ? (
          <MonacoEditor
            key={`editor-${editorTheme}`}
            height="100%"
            language="yaml"
            value={content}
            onChange={handleContentChange}
            onMount={handleEditorMount}
            theme={editorTheme}
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
          <div className="w-full h-full">
            <WorkflowGraphEditor
              initialSteps={graphSteps}
              onSave={handleGraphSave}
              onRun={handleGraphRun}
            />
          </div>
        )}
      </div>
    </div>
  )
}
