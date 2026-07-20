import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { X, Copy } from 'lucide-react'
import type { Step } from '@/types/execution'
import { MarkdownView } from '../MarkdownView'
import { AnsiText } from './AnsiText'
import { getStatusIcon } from './stepUtils'

export interface StepDetailPanelProps {
  step: Step
  position: { x: number; y: number }
  onPositionChange: (position: { x: number; y: number }) => void
  onClose: () => void
}

// 选中步骤的浮动详情面板（可拖动标题栏，字段可复制）
export function StepDetailPanel({ step, position, onPositionChange, onClose }: StepDetailPanelProps) {
  const { t } = useTranslation()
  const [copiedField, setCopiedField] = useState<string | null>(null) // 记录哪个字段被复制
  const [isDragging, setIsDragging] = useState(false)
  const dragStart = useRef({ x: 0, y: 0, panelX: 0, panelY: 0 })

  // 复制到剪贴板
  const copyToClipboard = useCallback((text: string, field: string) => {
    navigator.clipboard.writeText(text)
    setCopiedField(field)
    setTimeout(() => setCopiedField(null), 2000)
  }, [])

  // 面板拖动处理
  useEffect(() => {
    if (!isDragging) return

    const handleMouseMove = (e: MouseEvent) => {
      const deltaX = e.clientX - dragStart.current.x
      const deltaY = e.clientY - dragStart.current.y

      const newX = Math.max(0, Math.min(window.innerWidth - 400, dragStart.current.panelX + deltaX))
      const newY = Math.max(56, Math.min(window.innerHeight - 100, dragStart.current.panelY + deltaY))

      onPositionChange({ x: newX, y: newY })
    }

    const handleMouseUp = () => {
      setIsDragging(false)
      document.body.style.cursor = 'default'
      document.body.style.userSelect = 'auto'
    }

    document.addEventListener('mousemove', handleMouseMove)
    document.addEventListener('mouseup', handleMouseUp)

    return () => {
      document.removeEventListener('mousemove', handleMouseMove)
      document.removeEventListener('mouseup', handleMouseUp)
    }
  }, [isDragging, onPositionChange])

  const startDrag = (e: React.MouseEvent) => {
    e.preventDefault()
    dragStart.current = {
      x: e.clientX,
      y: e.clientY,
      panelX: position.x,
      panelY: position.y
    }
    setIsDragging(true)
    document.body.style.cursor = 'move'
    document.body.style.userSelect = 'none'
  }

  return (
    <div
      className="fixed w-96 bg-white dark:bg-gray-800 rounded-xl shadow-2xl border dark:border-gray-700 z-[60] max-h-[calc(100vh-180px)] overflow-hidden"
      style={{
        left: `${position.x}px`,
        top: `${position.y}px`,
      }}
    >
      <div
      className="px-4 py-3 border-b dark:border-gray-700 flex items-center justify-between bg-gray-50 dark:bg-gray-900 cursor-move select-none relative"
      onMouseDown={startDrag}
      >
        {/* 复制成功提示 */}
        {copiedField && (
          <div className="absolute left-1/2 top-full transform -translate-x-1/2 mt-1 px-2 py-1 bg-black text-white text-xs rounded shadow-lg z-50">
            {t('execution.copied')}
          </div>
        )}
        <h3 className="font-semibold text-gray-900 dark:text-white truncate">{step.name}</h3>
        <button
          onClick={onClose}
          className="p-1 hover:bg-gray-200 dark:hover:bg-gray-700 rounded transition-colors"
        >
          <X className="w-4 h-4 text-gray-500" />
        </button>
      </div>
      <div className="p-4 overflow-y-auto max-h-[calc(100vh-240px)]">
        <div className="space-y-3">
          <div className="flex items-center gap-2">
            {getStatusIcon(step.status)}
            <span className={`text-sm font-medium ${
              step.status === 'success' ? 'text-green-600 dark:text-green-400' :
              step.status === 'running' ? 'text-blue-600 dark:text-blue-400' :
              step.status === 'failed' ? 'text-red-600 dark:text-red-400' :
              'text-gray-600 dark:text-gray-400'
            }`}>
              {t(`execution.${step.status}`)}
            </span>
          </div>

          {step.action && (
            <div>
              <p className="text-xs text-gray-500 dark:text-gray-400 uppercase tracking-wide">{t('execution.action')}</p>
              <p className="text-sm text-gray-900 dark:text-white font-mono">{step.action}</p>
            </div>
          )}

          {step.description && (
            <div>
              <p className="text-xs text-gray-500 dark:text-gray-400 uppercase tracking-wide">{t('execution.description')}</p>
              <p className="text-sm text-gray-900 dark:text-white">{step.description}</p>
            </div>
          )}

          {step.startTime && (
            <div>
              <p className="text-xs text-gray-500 dark:text-gray-400 uppercase tracking-wide">{t('execution.startTime')}</p>
              <p className="text-sm text-gray-900 dark:text-white">{step.startTime}</p>
            </div>
          )}

          {step.endTime && (
            <div>
              <p className="text-xs text-gray-500 dark:text-gray-400 uppercase tracking-wide">{t('execution.endTime')}</p>
              <p className="text-sm text-gray-900 dark:text-white">{step.endTime}</p>
            </div>
          )}

          {step.duration && (
            <div>
              <p className="text-xs text-gray-500 dark:text-gray-400 uppercase tracking-wide">{t('execution.duration')}</p>
              <p className="text-sm text-gray-900 dark:text-white">{step.duration}</p>
            </div>
          )}

          {/* Sleep 参数 */}
          {step.action === 'sleep' && step.sleepDuration && (
            <div>
              <p className="text-xs text-gray-500 dark:text-gray-400 uppercase tracking-wide">{t('execution.sleepDuration')}</p>
              <p className="text-sm text-purple-600 dark:text-purple-400 font-mono">{step.sleepDuration}</p>
            </div>
          )}

          {/* Shell 命令 */}
          {step.action === 'shell' && step.shellCommand && (
            <div>
              <div className="flex items-center justify-between mb-1">
                <p className="text-xs text-gray-500 dark:text-gray-400 uppercase tracking-wide">{t('execution.shellCommand')}</p>
                <button
                  onClick={() => copyToClipboard(step.shellCommand!, 'shellCommand')}
                  className={`p-1 rounded transition-colors ${
                    copiedField === 'shellCommand'
                      ? 'bg-green-500 text-white'
                      : 'text-gray-400 hover:text-gray-600 hover:bg-gray-200 dark:hover:bg-gray-700'
                  }`}
                  title={t('execution.copy')}
                >
                  <Copy className="w-3 h-3" />
                </button>
              </div>
              <pre className="text-sm text-green-700 dark:text-green-400 bg-gray-50 dark:bg-gray-900 p-3 rounded-lg overflow-x-auto font-mono whitespace-pre-wrap">{step.shellCommand}</pre>
            </div>
          )}

          {/* HTTP 参数 */}
          {step.action === 'http' && (step.httpUrl || step.httpMethod) && (
            <div>
              <p className="text-xs text-gray-500 dark:text-gray-400 uppercase tracking-wide">{t('execution.httpParams')}</p>
              <div className="text-sm text-blue-600 dark:text-blue-400 font-mono space-y-1">
                {step.httpMethod && <p>{step.httpMethod}</p>}
                {step.httpUrl && <p className="truncate">{step.httpUrl}</p>}
              </div>
            </div>
          )}

          {/* Log 消息 */}
          {step.action === 'log' && step.logMessage && (
            <div>
              <div className="flex items-center justify-between mb-1">
                <p className="text-xs text-gray-500 dark:text-gray-400 uppercase tracking-wide">{t('execution.logMessage')}</p>
                <button
                  onClick={() => copyToClipboard(step.logMessage!, 'logMessage')}
                  className={`p-1 rounded transition-colors ${
                    copiedField === 'logMessage'
                      ? 'bg-green-500 text-white'
                      : 'text-gray-400 hover:text-gray-600 hover:bg-gray-200 dark:hover:bg-gray-700'
                  }`}
                  title={t('execution.copy')}
                >
                  <Copy className="w-3 h-3" />
                </button>
              </div>
              <pre className="text-sm text-gray-900 dark:text-white bg-gray-50 dark:bg-gray-900 p-3 rounded-lg overflow-x-auto font-mono whitespace-pre-wrap">{step.logMessage}</pre>
            </div>
          )}

          {/* Condition 表达式 */}
          {step.action === 'condition' && step.expression && (
            <div>
              <p className="text-xs text-gray-500 dark:text-gray-400 uppercase tracking-wide">{t('execution.expression')}</p>
              <p className="text-sm text-orange-600 dark:text-orange-400 font-mono">{step.expression}</p>
            </div>
          )}

          {/* Condition 结果 */}
          {step.action === 'condition' && step.condition_result !== undefined && step.condition_result !== null && (
            <div>
              <p className="text-xs text-gray-500 dark:text-gray-400 uppercase tracking-wide">{t('execution.conditionResult')}</p>
              <p className="text-sm font-mono">
                {step.condition_result === true ? (
                  <span className="text-green-600 dark:text-green-400">✓ true</span>
                ) : (
                  <span className="text-gray-500 dark:text-gray-400">✗ false</span>
                )}
              </p>
            </div>
          )}

          {step.aiOutput && (
            <div>
              <div className="flex items-center justify-between mb-1">
                <p className="text-xs text-gray-500 dark:text-gray-400 uppercase tracking-wide">
                  AI Output
                  {step.status === 'running' && (
                    <span className="ml-1 animate-pulse">●</span>
                  )}
                </p>
              </div>
              <div className="text-sm text-gray-900 dark:text-white bg-gray-50 dark:bg-gray-900 p-3 rounded-lg">
                <MarkdownView content={step.aiOutput} />
              </div>
            </div>
          )}

          {step.output && (
            <div>
              <div className="flex items-center justify-between mb-1">
                <p className="text-xs text-gray-500 dark:text-gray-400 uppercase tracking-wide">{t('execution.output')}</p>
                <button
                  onClick={() => copyToClipboard(step.output!, 'output')}
                  className={`p-1 rounded transition-colors ${
                    copiedField === 'output'
                      ? 'bg-green-500 text-white'
                      : 'text-gray-400 hover:text-gray-600 hover:bg-gray-200 dark:hover:bg-gray-700'
                  }`}
                  title={t('execution.copy')}
                >
                  <Copy className="w-3 h-3" />
                </button>
              </div>
              <pre className="text-sm text-gray-900 dark:text-white bg-gray-50 dark:bg-gray-900 p-3 rounded-lg overflow-x-auto font-mono whitespace-pre-wrap">
                <AnsiText text={step.output} />
              </pre>
            </div>
          )}

          {step.error && (
            <div>
              <div className="flex items-center justify-between mb-1">
                <p className="text-xs text-gray-500 dark:text-gray-400 uppercase tracking-wide">{t('execution.error')}</p>
                <button
                  onClick={() => copyToClipboard(step.error!, 'error')}
                  className={`p-1 rounded transition-colors ${
                    copiedField === 'error'
                      ? 'bg-green-500 text-white'
                      : 'text-gray-400 hover:text-gray-600 hover:bg-gray-200 dark:hover:bg-gray-700'
                  }`}
                  title={t('execution.copy')}
                >
                  <Copy className="w-3 h-3" />
                </button>
              </div>
              <pre className="text-sm text-red-600 dark:text-red-400 bg-red-50 dark:bg-red-900/20 p-3 rounded-lg overflow-x-auto font-mono whitespace-pre-wrap">
                {step.error}
              </pre>
            </div>
          )}

          {/* Parallel/Foreach 子任务 */}
          {step.children && step.children.length > 0 && (
            <div>
              <p className="text-xs text-gray-500 dark:text-gray-400 uppercase tracking-wide mb-2">
                {step.action === 'parallel' ? t('execution.parallelTasks') :
                 (step.action === 'foreach' || step.action === 'loop') ? t('execution.loopIterations') :
                 t('execution.subSteps')}
              </p>
              <div className="space-y-2">
                {step.children.map((child, idx) => (
                  <div
                    key={child.id || idx}
                    className="bg-gray-50 dark:bg-gray-900 rounded text-sm"
                  >
                    <div className="flex items-center gap-2 p-2">
                      {getStatusIcon(child.status)}
                      <span className="font-medium text-gray-900 dark:text-white truncate flex-1">{child.name}</span>
                      {child.duration && (
                        <span className="text-xs text-gray-500 dark:text-gray-400 font-mono">{child.duration}</span>
                      )}
                    </div>
                    {/* 子任务输出 */}
                    {child.output && (
                      <div className="px-2 pb-2 pt-0">
                        <pre className="text-xs text-gray-700 dark:text-gray-300 bg-gray-100 dark:bg-gray-800 p-2 rounded overflow-x-auto font-mono whitespace-pre-wrap max-h-32 overflow-y-auto">
                          <AnsiText text={child.output} />
                        </pre>
                      </div>
                    )}
                    {/* 子任务错误 */}
                    {child.error && (
                      <div className="px-2 pb-2 pt-0">
                        <pre className="text-xs text-red-600 dark:text-red-400 bg-red-50 dark:bg-red-900/20 p-2 rounded overflow-x-auto font-mono whitespace-pre-wrap">
                          {child.error}
                        </pre>
                      </div>
                    )}
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* Condition 分支 */}
          {(step.then_children && step.then_children.length > 0) || (step.else_children && step.else_children.length > 0) ? (
            <div>
              <p className="text-xs text-gray-500 dark:text-gray-400 uppercase tracking-wide mb-2">{t('execution.branches')}</p>
              <div className="space-y-2">
                {step.then_children && step.then_children.length > 0 && (
                  <div>
                    <p className="text-xs font-medium text-green-600 dark:text-green-400 mb-1">
                      ✓ {t('execution.thenBranch')} {step.condition_result === true ? '(执行)' : '(跳过)'}
                    </p>
                    <div className="space-y-1 pl-2">
                      {step.then_children.map((child, idx) => (
                        <div
                          key={child.id || idx}
                          className="flex items-center gap-2 p-1.5 bg-gray-50 dark:bg-gray-900 rounded text-xs"
                        >
                          {getStatusIcon(child.status)}
                          <span className="text-gray-900 dark:text-white truncate">{child.name}</span>
                        </div>
                      ))}
                    </div>
                  </div>
                )}
                {step.else_children && step.else_children.length > 0 && (
                  <div>
                    <p className="text-xs font-medium text-gray-500 dark:text-gray-400 mb-1">
                      ✗ {t('execution.elseBranch')} {step.condition_result === false ? '(执行)' : '(跳过)'}
                    </p>
                    <div className="space-y-1 pl-2">
                      {step.else_children.map((child, idx) => (
                        <div
                          key={child.id || idx}
                          className="flex items-center gap-2 p-1.5 bg-gray-50 dark:bg-gray-900 rounded text-xs"
                        >
                          {getStatusIcon(child.status)}
                          <span className="text-gray-900 dark:text-white truncate">{child.name}</span>
                        </div>
                      ))}
                    </div>
                  </div>
                )}
              </div>
            </div>
          ) : null}
        </div>
      </div>
    </div>
  )
}
