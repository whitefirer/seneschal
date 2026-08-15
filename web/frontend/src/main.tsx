import React, { lazy, Suspense } from 'react'
import ReactDOM from 'react-dom/client'
import { BrowserRouter, Routes, Route, Navigate, useParams } from 'react-router-dom'
import App from './App'
import Dashboard from './components/Dashboard'
import History from './components/History'
import Runbooks from './components/Runbooks'
import '@/i18n' // Initialize i18n
import './index.css'

// Lazy load heavy components — keeps the initial bundle small
const Editor = lazy(() => import('./components/Editor'))
const Execution = lazy(() => import('./components/Execution'))
const ChatPanel = lazy(() => import('./components/ChatPanel'))

// 旧 /dag 路由重定向到统一编辑器
function DagRedirect() {
  const { name } = useParams<{ name: string }>()
  return <Navigate to={name ? `/editor/${name}` : '/editor/new'} replace />
}

// Loading fallback for lazy components
function EditorLoading() {
  return (
    <div className="flex items-center justify-center h-screen bg-gray-50 dark:bg-gray-900">
      <div className="flex flex-col items-center gap-4">
        <div className="animate-spin rounded-full h-12 w-12 border-4 border-blue-500 border-t-transparent"></div>
        <span className="text-gray-500 dark:text-gray-400">Loading...</span>
      </div>
    </div>
  )
}

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <BrowserRouter>
      <Routes>
        <Route path="/" element={<App />}>
          <Route index element={<Dashboard />} />
          <Route path="editor/:name" element={<Suspense fallback={<EditorLoading />}><Editor /></Suspense>} />
          <Route path="dag/:name" element={<DagRedirect />} />
          <Route path="dag-new" element={<Navigate to="/editor/new" replace />} />
          <Route path="execution/:id" element={<Suspense fallback={<EditorLoading />}><Execution /></Suspense>} />
          <Route path="history" element={<History />} />
          <Route path="runbooks" element={<Runbooks />} />
          <Route path="chat" element={<Suspense fallback={<EditorLoading />}><ChatPanel /></Suspense>} />
        </Route>
      </Routes>
    </BrowserRouter>
  </React.StrictMode>,
)
