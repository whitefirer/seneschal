import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { ArrowLeft, RefreshCw } from 'lucide-react'
import { executionsApi, type ExecutionRecord } from '@/api/client'
import { formatTimestamp } from '@/lib/utils'

export default function History() {
  const { t } = useTranslation()
  const [executions, setExecutions] = useState<ExecutionRecord[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    loadExecutions()
  }, [])

  async function loadExecutions() {
    try {
      const data = await executionsApi.list()
      setExecutions(data)
    } catch (error) {
      console.error('Failed to load executions:', error)
    } finally {
      setLoading(false)
    }
  }

  const getStatusIcon = (status: string) => {
    switch (status) {
      case 'success': return '✅'
      case 'failed': return '❌'
      case 'running': return '🔄'
      default: return '⏳'
    }
  }

  const getStatusColor = (status: string) => {
    switch (status) {
      case 'success': return 'text-green-500 bg-green-500/10'
      case 'failed': return 'text-red-500 bg-red-500/10'
      case 'running': return 'text-blue-500 bg-blue-500/10'
      default: return 'text-muted-foreground bg-muted'
    }
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64">
        <RefreshCw className="h-8 w-8 animate-spin text-primary" />
        <span className="ml-2 text-muted-foreground">{t('common.loading')}</span>
      </div>
    )
  }

  return (
    <div className="h-full overflow-auto">
      <div className="px-4 py-6 space-y-4">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-4">
          <Link
            to="/"
            className="p-2 hover:bg-accent rounded-md transition-colors"
          >
            <ArrowLeft className="h-5 w-5" />
          </Link>
          <h1 className="text-2xl font-bold">{t('history.title')}</h1>
        </div>
        <button
          onClick={loadExecutions}
          className="flex items-center gap-2 px-4 py-2 border rounded-md hover:bg-accent transition-colors"
        >
          <RefreshCw className="h-4 w-4" />
          {t('history.refresh')}
        </button>
      </div>

      {/* Executions List */}
      {executions.length === 0 ? (
        <div className="text-center py-12 border border-dashed rounded-lg">
          <p className="text-muted-foreground">{t('history.noExecutions')}</p>
          <Link to="/" className="text-primary hover:underline mt-2 inline-block">
            {t('history.runWorkflow')}
          </Link>
        </div>
      ) : (
        <div className="border rounded-lg overflow-hidden">
          <table className="w-full">
            <thead className="bg-muted">
              <tr>
                <th className="px-4 py-3 text-left text-sm font-medium">{t('execution.status')}</th>
                <th className="px-4 py-3 text-left text-sm font-medium">{t('history.workflow')}</th>
                <th className="px-4 py-3 text-left text-sm font-medium">{t('history.executionId')}</th>
                <th className="px-4 py-3 text-left text-sm font-medium">{t('history.startedAt')}</th>
                <th className="px-4 py-3 text-left text-sm font-medium">{t('history.completedAt')}</th>
                <th className="px-4 py-3 text-left text-sm font-medium">{t('history.duration')}</th>
                <th className="px-4 py-3 text-left text-sm font-medium">{t('history.steps')}</th>
                <th className="px-4 py-3 text-left text-sm font-medium">{t('history.actions')}</th>
              </tr>
            </thead>
            <tbody>
              {executions.map((exec) => (
                <tr
                  key={exec.id}
                  className="border-t hover:bg-muted/50 transition-colors"
                >
                  <td className="px-4 py-3">
                    <Link to={`/execution/${exec.id}`}>
                      <span className={`inline-flex items-center gap-2 px-3 py-1 rounded-full text-sm font-medium ${getStatusColor(exec.status)}`}>
                        {getStatusIcon(exec.status)}
                        {exec.status}
                      </span>
                    </Link>
                  </td>
                  <td className="px-4 py-3">
                    <Link
                      to={`/editor/${exec.workflowFile}`}
                      className="font-medium hover:underline text-blue-600 dark:text-blue-400"
                    >
                      {exec.workflowName}
                    </Link>
                  </td>
                  <td className="px-4 py-3 text-sm text-muted-foreground">
                    <Link
                      to={`/execution/${exec.id}`}
                      className="hover:underline"
                    >
                      {exec.id}
                    </Link>
                  </td>
                  <td className="px-4 py-3 text-sm text-muted-foreground">
                    {formatTimestamp(exec.startTime)}
                  </td>
                  <td className="px-4 py-3 text-sm text-muted-foreground">
                    {exec.endTime ? formatTimestamp(exec.endTime) : '-'}
                  </td>
                  <td className="px-4 py-3 text-sm text-muted-foreground">
                    {exec.duration}
                  </td>
                  <td className="px-4 py-3 text-sm text-muted-foreground">
                    {exec.stepsCount}
                  </td>
                  <td className="px-4 py-3">
                    <Link
                      to={`/execution/${exec.id}`}
                      className="text-primary hover:underline text-sm"
                    >
                      {t('history.viewDetails')}
                    </Link>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      </div>
    </div>
  )
}
