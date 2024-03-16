import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { Play, Edit, Trash2, FileText, Plus, RefreshCw } from 'lucide-react'
import { workflowsApi, executionsApi, type WorkflowInfo, type ExecutionRecord } from '@/api/client'
import { formatTimestamp } from '@/lib/utils'

export default function Dashboard() {
  const { t } = useTranslation()
  const [workflows, setWorkflows] = useState<WorkflowInfo[]>([])
  const [executions, setExecutions] = useState<ExecutionRecord[]>([])
  const [loading, setLoading] = useState(true)
  const [running, setRunning] = useState<Record<string, boolean>>({})

  const loadData = async () => {
    try {
      const [wfList, exList] = await Promise.all([
        workflowsApi.list(),
        executionsApi.list(),
      ])
      setWorkflows(wfList)
      setExecutions(exList.slice(0, 5)) // Show last 5 executions
    } catch (error) {
      console.error('Failed to load data:', error)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    loadData()
  }, [])

  const runWorkflow = async (name: string) => {
    setRunning((prev) => ({ ...prev, [name]: true }))
    try {
      const res = await workflowsApi.run(name)
      window.location.href = `/execution/${res.executionId}`
    } catch (error: any) {
      alert(`${t('editor.runFailed')}: ${error.message}`)
    } finally {
      setRunning((prev) => ({ ...prev, [name]: false }))
    }
  }

  const deleteWorkflow = async (name: string) => {
    if (!confirm(t('editor.deleteConfirm'))) return
    try {
      await workflowsApi.delete(name)
      loadData()
    } catch (error: any) {
      alert(`${t('common.delete')} ${t('common.failed')}: ${error.message}`)
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
      case 'success': return 'text-green-500'
      case 'failed': return 'text-red-500'
      case 'running': return 'text-blue-500'
      default: return 'text-muted-foreground'
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
      <div className="container mx-auto px-4 py-6 space-y-8">
        {/* Workflows Section */}
        <section>
        <div className="flex items-center justify-between mb-4">
          <h2 className="text-2xl font-bold">{t('dashboard.workflows')}</h2>
          <div className="flex items-center gap-2">
            <Link
              to="/editor/new"
              className="flex items-center gap-2 px-4 py-2 bg-primary text-primary-foreground rounded-md hover:bg-primary/90 transition-colors"
            >
              <Plus className="h-4 w-4" />
              {t('dashboard.createWorkflow')}
            </Link>
            <Link
              to="/dag-new"
              className="flex items-center gap-2 px-4 py-2 bg-purple-500 text-white rounded-md hover:bg-purple-600 transition-colors"
            >
              <Plus className="h-4 w-4" />
              DAG Editor
            </Link>
          </div>
        </div>

        {workflows.length === 0 ? (
          <div className="text-center py-12 border border-dashed rounded-lg">
            <FileText className="h-12 w-12 mx-auto text-muted-foreground mb-4" />
            <p className="text-muted-foreground">{t('dashboard.noWorkflows')}</p>
            <Link
              to="/editor/new"
              className="text-primary hover:underline mt-2 inline-block"
            >
              {t('dashboard.createFirst')}
            </Link>
          </div>
        ) : (
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
            {workflows.map((wf) => (
              <div
                key={wf.fileName}
                className="border rounded-lg p-4 bg-card hover:shadow-md transition-shadow"
              >
                <div className="flex items-start justify-between mb-2">
                  <div className="flex items-center gap-2">
                    <FileText className="h-5 w-5 text-primary" />
                    <h3 className="font-semibold">{wf.name}</h3>
                  </div>
                  {wf.version && (
                    <span className="text-xs px-2 py-1 bg-muted rounded-full">
                      v{wf.version}
                    </span>
                  )}
                </div>

                {wf.description && (
                  <p className="text-sm text-muted-foreground mb-3 line-clamp-2">
                    {wf.description}
                  </p>
                )}

                <div className="flex items-center gap-4 text-xs text-muted-foreground mb-4">
                  <span>{wf.steps} steps</span>
                  <span>{wf.variables} variables</span>
                </div>

                <div className="flex items-center gap-2">
                  <button
                    onClick={() => runWorkflow(wf.fileName)}
                    disabled={running[wf.fileName]}
                    className="flex-1 flex items-center justify-center gap-2 px-3 py-2 bg-primary text-primary-foreground rounded-md hover:bg-primary/90 transition-colors disabled:opacity-50"
                  >
                    <Play className="h-4 w-4" />
                    {running[wf.fileName] ? t('execution.running') : t('common.run')}
                  </button>
                  <Link
                    to={`/editor/${wf.fileName}`}
                    className="p-2 border rounded-md hover:bg-accent transition-colors"
                    title={t('common.edit')}
                  >
                    <Edit className="h-4 w-4" />
                  </Link>
                  <button
                    onClick={() => deleteWorkflow(wf.fileName)}
                    className="p-2 border rounded-md hover:bg-destructive hover:text-destructive-foreground transition-colors"
                    title={t('common.delete')}
                  >
                    <Trash2 className="h-4 w-4" />
                  </button>
                </div>
              </div>
            ))}
          </div>
        )}
      </section>

      {/* Recent Executions Section */}
      <section>
        <div className="flex items-center justify-between mb-4">
          <h2 className="text-2xl font-bold">{t('dashboard.recentExecutions')}</h2>
          <Link
            to="/history"
            className="text-sm text-primary hover:underline"
          >
            {t('nav.history')}
          </Link>
        </div>

        {executions.length === 0 ? (
          <div className="text-center py-8 border border-dashed rounded-lg text-muted-foreground">
            {t('history.noExecutions')}
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
                  <th className="px-4 py-3 text-left text-sm font-medium">{t('execution.duration')}</th>
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
                        <span className={`text-lg ${getStatusColor(exec.status)}`}>
                          {getStatusIcon(exec.status)}
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
                      {exec.duration}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
      </div>
    </div>
  )
}
