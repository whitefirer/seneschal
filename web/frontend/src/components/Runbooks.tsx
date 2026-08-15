import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import { ArrowLeft, RefreshCw, Play, Edit, Trash2, Plus, X, Clock } from 'lucide-react'
import yaml from 'js-yaml'
import { runbooksApi, workflowsApi, type Runbook, type WorkflowInfo } from '@/api/client'

interface TriggerForm { type: 'manual' | 'cron' | 'webhook'; cron: string; path: string }
interface VarForm { key: string; value: string }
interface FormState { name: string; workflow: string; triggers: TriggerForm[]; variables: VarForm[] }

const emptyForm: FormState = {
  name: '',
  workflow: '',
  triggers: [{ type: 'manual', cron: '', path: '' }],
  variables: [],
}

function triggerLabel(t: { Type: string; Cron?: string; Path?: string }): string {
  if (t.Type === 'cron') return `cron ${t.Cron || ''}`
  if (t.Type === 'webhook') return `webhook ${t.Path || ''}`
  return 'manual'
}

export default function Runbooks() {
  const [runbooks, setRunbooks] = useState<Runbook[]>([])
  const [workflows, setWorkflows] = useState<WorkflowInfo[]>([])
  const [loading, setLoading] = useState(true)
  const [showForm, setShowForm] = useState(false)
  const [form, setForm] = useState<FormState>(emptyForm)
  const [saving, setSaving] = useState(false)
  const [triggering, setTriggering] = useState<string | null>(null)
  const [notice, setNotice] = useState<{ ok: boolean; text: string } | null>(null)

  async function load() {
    try {
      const [rb, wf] = await Promise.all([runbooksApi.list(), workflowsApi.list()])
      setRunbooks(rb)
      setWorkflows(wf)
    } catch (error) {
      console.error('Failed to load runbooks:', error)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load() }, [])

  function openNew() {
    setForm(emptyForm)
    setNotice(null)
    setShowForm(true)
  }

  function openEdit(rb: Runbook) {
    setForm({
      name: rb.Name,
      workflow: rb.Workflow,
      triggers: rb.Triggers?.length
        ? rb.Triggers.map((t) => ({ type: t.Type, cron: t.Cron || '', path: t.Path || '' }))
        : [{ type: 'manual', cron: '', path: '' }],
      variables: Object.entries(rb.Variables || {}).map(([key, value]) => ({ key, value })),
    })
    setNotice(null)
    setShowForm(true)
  }

  function buildYaml(): string {
    const rb: Record<string, unknown> = { name: form.name.trim(), workflow: form.workflow.trim() }
    const triggers = form.triggers
      .filter((t) => t.type)
      .map((t) =>
        t.type === 'cron'
          ? { type: 'cron', cron: t.cron.trim() }
          : t.type === 'webhook'
            ? { type: 'webhook', path: t.path.trim() }
            : { type: 'manual' }
      )
    if (triggers.length) rb.triggers = triggers
    const vars: Record<string, string> = {}
    for (const v of form.variables) {
      if (v.key.trim()) vars[v.key.trim()] = v.value
    }
    if (Object.keys(vars).length) rb.variables = vars
    return yaml.dump(rb)
  }

  async function save() {
    if (!form.name.trim() || !form.workflow.trim()) {
      setNotice({ ok: false, text: 'Name and workflow are required' })
      return
    }
    setSaving(true)
    try {
      await runbooksApi.save(form.name.trim() + '.yaml', buildYaml())
      setShowForm(false)
      setNotice(null)
      await load()
    } catch (error) {
      setNotice({ ok: false, text: `Save failed: ${error instanceof Error ? error.message : String(error)}` })
    } finally {
      setSaving(false)
    }
  }

  async function trigger(rb: Runbook) {
    setTriggering(rb.Name)
    setNotice(null)
    try {
      const res = await runbooksApi.trigger(rb.Name)
      setNotice({ ok: true, text: `Triggered "${rb.Name}" → execution ${res.executionId}` })
    } catch (error) {
      setNotice({ ok: false, text: `Trigger failed: ${error instanceof Error ? error.message : String(error)}` })
    } finally {
      setTriggering(null)
    }
  }

  async function remove(rb: Runbook) {
    if (!confirm(`Delete runbook "${rb.Name}"?`)) return
    try {
      await runbooksApi.delete(rb.FileName || rb.Name + '.yaml')
      await load()
    } catch (error) {
      setNotice({ ok: false, text: `Delete failed: ${error instanceof Error ? error.message : String(error)}` })
    }
  }

  function updateTrigger(i: number, patch: Partial<TriggerForm>) {
    setForm((f) => ({ ...f, triggers: f.triggers.map((t, j) => (j === i ? { ...t, ...patch } : t)) }))
  }
  function updateVar(i: number, patch: Partial<VarForm>) {
    setForm((f) => ({ ...f, variables: f.variables.map((v, j) => (j === i ? { ...v, ...patch } : v)) }))
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64">
        <RefreshCw className="h-8 w-8 animate-spin text-primary" />
        <span className="ml-2 text-muted-foreground">Loading...</span>
      </div>
    )
  }

  return (
    <div className="h-full overflow-auto">
      <div className="px-4 py-6 space-y-4">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-4">
            <Link to="/" className="p-2 hover:bg-accent rounded-md transition-colors">
              <ArrowLeft className="h-5 w-5" />
            </Link>
            <h1 className="text-2xl font-bold">Runbooks</h1>
          </div>
          <div className="flex items-center gap-2">
            <button onClick={load} className="flex items-center gap-2 px-4 py-2 border rounded-md hover:bg-accent transition-colors">
              <RefreshCw className="h-4 w-4" /> Refresh
            </button>
            <button onClick={openNew} className="flex items-center gap-2 px-4 py-2 bg-primary text-primary-foreground rounded-md hover:bg-primary/90 transition-colors">
              <Plus className="h-4 w-4" /> New Runbook
            </button>
          </div>
        </div>

        {notice && (
          <div className={`px-4 py-3 rounded-md border text-sm ${notice.ok ? 'bg-green-500/10 border-green-500/30 text-green-700 dark:text-green-300' : 'bg-red-500/10 border-red-500/30 text-red-700 dark:text-red-300'}`}>
            {notice.text}
          </div>
        )}

        {runbooks.length === 0 ? (
          <div className="text-center py-12 border border-dashed rounded-lg">
            <Clock className="h-12 w-12 mx-auto text-muted-foreground mb-4" />
            <p className="text-muted-foreground">No runbooks yet. Create one to schedule or webhook-trigger a workflow.</p>
          </div>
        ) : (
          <div className="border rounded-lg overflow-hidden">
            <table className="w-full">
              <thead className="bg-muted">
                <tr>
                  <th className="px-4 py-3 text-left text-sm font-medium">Name</th>
                  <th className="px-4 py-3 text-left text-sm font-medium">Workflow</th>
                  <th className="px-4 py-3 text-left text-sm font-medium">Triggers</th>
                  <th className="px-4 py-3 text-left text-sm font-medium">Actions</th>
                </tr>
              </thead>
              <tbody>
                {runbooks.map((rb) => (
                  <tr key={rb.FileName || rb.Name} className="border-t hover:bg-muted/50 transition-colors">
                    <td className="px-4 py-3 font-medium">{rb.Name}</td>
                    <td className="px-4 py-3 text-sm text-muted-foreground">{rb.Workflow}</td>
                    <td className="px-4 py-3">
                      <div className="flex flex-wrap gap-1">
                        {(rb.Triggers || []).map((t, i) => (
                          <span key={i} className="inline-flex items-center px-2 py-0.5 rounded-full text-xs bg-muted text-muted-foreground">
                            {triggerLabel(t)}
                          </span>
                        ))}
                        {!rb.Triggers?.length && <span className="text-xs text-muted-foreground">manual</span>}
                      </div>
                    </td>
                    <td className="px-4 py-3">
                      <div className="flex items-center gap-2">
                        <button
                          onClick={() => trigger(rb)}
                          disabled={triggering === rb.Name}
                          className="flex items-center gap-1 px-3 py-1.5 bg-primary text-primary-foreground rounded-md hover:bg-primary/90 text-sm disabled:opacity-50"
                        >
                          <Play className="h-3.5 w-3.5" />
                          {triggering === rb.Name ? '...' : 'Run'}
                        </button>
                        <button onClick={() => openEdit(rb)} className="p-2 border rounded-md hover:bg-accent transition-colors" title="Edit">
                          <Edit className="h-4 w-4" />
                        </button>
                        <button onClick={() => remove(rb)} className="p-2 border rounded-md hover:bg-destructive hover:text-destructive-foreground transition-colors" title="Delete">
                          <Trash2 className="h-4 w-4" />
                        </button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {showForm && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
          <div className="bg-card border rounded-lg w-full max-w-lg max-h-[90vh] overflow-auto p-6 space-y-4">
            <div className="flex items-center justify-between">
              <h2 className="text-lg font-semibold">{form.name ? `Edit ${form.name}` : 'New Runbook'}</h2>
              <button onClick={() => setShowForm(false)} className="p-1 hover:bg-accent rounded-md">
                <X className="h-5 w-5" />
              </button>
            </div>

            <div>
              <label className="block text-sm font-medium mb-1">Name</label>
              <input
                value={form.name}
                onChange={(e) => setForm({ ...form, name: e.target.value })}
                placeholder="nightly"
                className="w-full px-3 py-2 border rounded-md bg-background"
              />
            </div>

            <div>
              <label className="block text-sm font-medium mb-1">Workflow</label>
              <input
                value={form.workflow}
                onChange={(e) => setForm({ ...form, workflow: e.target.value })}
                list="workflow-options"
                placeholder="deploy.yaml"
                className="w-full px-3 py-2 border rounded-md bg-background"
              />
              <datalist id="workflow-options">
                {workflows.map((wf) => (
                  <option key={wf.fileName} value={wf.fileName} />
                ))}
              </datalist>
            </div>

            <div>
              <label className="block text-sm font-medium mb-1">Triggers</label>
              <div className="space-y-2">
                {form.triggers.map((t, i) => (
                  <div key={i} className="flex items-center gap-2">
                    <select
                      value={t.type}
                      onChange={(e) => updateTrigger(i, { type: e.target.value as TriggerForm['type'] })}
                      className="px-2 py-2 border rounded-md bg-background"
                    >
                      <option value="manual">manual</option>
                      <option value="cron">cron</option>
                      <option value="webhook">webhook</option>
                    </select>
                    {t.type === 'cron' && (
                      <input
                        value={t.cron}
                        onChange={(e) => updateTrigger(i, { cron: e.target.value })}
                        placeholder="*/30 * * * *"
                        className="flex-1 px-3 py-2 border rounded-md bg-background"
                      />
                    )}
                    {t.type === 'webhook' && (
                      <input
                        value={t.path}
                        onChange={(e) => updateTrigger(i, { path: e.target.value })}
                        placeholder="/deploy"
                        className="flex-1 px-3 py-2 border rounded-md bg-background"
                      />
                    )}
                    <button
                      onClick={() => setForm((f) => ({ ...f, triggers: f.triggers.filter((_, j) => j !== i) }))}
                      className="p-1 hover:bg-accent rounded-md"
                    >
                      <X className="h-4 w-4" />
                    </button>
                  </div>
                ))}
                <button
                  onClick={() => setForm((f) => ({ ...f, triggers: [...f.triggers, { type: 'manual', cron: '', path: '' }] }))}
                  className="text-sm text-primary hover:underline"
                >
                  + Add trigger
                </button>
              </div>
            </div>

            <div>
              <label className="block text-sm font-medium mb-1">Variables</label>
              <div className="space-y-2">
                {form.variables.map((v, i) => (
                  <div key={i} className="flex items-center gap-2">
                    <input
                      value={v.key}
                      onChange={(e) => updateVar(i, { key: e.target.value })}
                      placeholder="key"
                      className="flex-1 px-3 py-2 border rounded-md bg-background"
                    />
                    <input
                      value={v.value}
                      onChange={(e) => updateVar(i, { value: e.target.value })}
                      placeholder="value"
                      className="flex-1 px-3 py-2 border rounded-md bg-background"
                    />
                    <button
                      onClick={() => setForm((f) => ({ ...f, variables: f.variables.filter((_, j) => j !== i) }))}
                      className="p-1 hover:bg-accent rounded-md"
                    >
                      <X className="h-4 w-4" />
                    </button>
                  </div>
                ))}
                <button
                  onClick={() => setForm((f) => ({ ...f, variables: [...f.variables, { key: '', value: '' }] }))}
                  className="text-sm text-primary hover:underline"
                >
                  + Add variable
                </button>
              </div>
            </div>

            <div className="flex justify-end gap-2 pt-2">
              <button onClick={() => setShowForm(false)} className="px-4 py-2 border rounded-md hover:bg-accent">
                Cancel
              </button>
              <button
                onClick={save}
                disabled={saving}
                className="px-4 py-2 bg-primary text-primary-foreground rounded-md hover:bg-primary/90 disabled:opacity-50"
              >
                {saving ? 'Saving...' : 'Save'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
