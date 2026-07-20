import { ArrowLeft, RefreshCw, CheckCircle, XCircle } from 'lucide-react'
import { useTranslation } from 'react-i18next'

export interface ExecutionHeaderProps {
  workflowName: string
  workflowFile: string
  executionId?: string
  status: 'running' | 'success' | 'failed'
  connected: boolean
  viewMode: 'graph' | 'list'
  onToggleViewMode: () => void
  onRefresh: () => void
}

// Execution 页头部：返回链接、工作流名/执行 ID、状态徽章、连接状态、视图切换与刷新
export function ExecutionHeader({
  workflowName,
  workflowFile,
  executionId,
  status,
  connected,
  viewMode,
  onToggleViewMode,
  onRefresh,
}: ExecutionHeaderProps) {
  const { t } = useTranslation()
  return (
    <div className="bg-white dark:bg-gray-800 border-b dark:border-gray-700 flex-shrink-0">
      <div className="px-4 sm:px-6 py-3">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-4">
            <a
              href="/history"
              className="flex items-center gap-2 text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200"
            >
              <ArrowLeft className="w-5 h-5" />
            </a>
            <div>
              <div className="flex items-center gap-2">
                <a
                  href={`/editor/${workflowFile || workflowName}`}
                  className="text-xl font-semibold text-gray-900 dark:text-white hover:text-blue-600 dark:hover:text-blue-400 transition-colors"
                >
                  {workflowName || 'Loading...'}
                </a>
                <span className="text-sm text-gray-500 dark:text-gray-400">
                  {executionId ? `(${executionId})` : ''}
                </span>
              </div>
              <div className="flex items-center gap-3 mt-1">
                <span className={`inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-medium ${
                  status === 'running' ? 'bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-400' :
                  status === 'success' ? 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400' :
                  'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400'
                }`}>
                  {status === 'running' && <RefreshCw className="w-3 h-3 animate-spin" />}
                  {status === 'success' && <CheckCircle className="w-3 h-3" />}
                  {status === 'failed' && <XCircle className="w-3 h-3" />}
                  {status.charAt(0).toUpperCase() + status.slice(1)}
                </span>
                <span className="text-sm text-gray-500 dark:text-gray-400">
                  {connected ? '🟢 Live' : '⚪ Disconnected'}
                </span>
              </div>
            </div>
          </div>

          <div className="flex items-center gap-2">
            <button
              onClick={onToggleViewMode}
              className="px-3 py-1.5 text-sm bg-gray-100 dark:bg-gray-700 hover:bg-gray-200 dark:hover:bg-gray-600 rounded-lg transition-colors"
            >
              {viewMode === 'graph' ? t('execution.listView') : t('execution.graphView')}
            </button>
            <button
              onClick={onRefresh}
              className="p-2 bg-gray-100 dark:bg-gray-700 hover:bg-gray-200 dark:hover:bg-gray-600 rounded-lg transition-colors"
              title={t('common.refresh')}
            >
              <RefreshCw className="w-4 h-4" />
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}
