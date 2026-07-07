import React, { lazy, Suspense } from 'react'
import ReactDOM from 'react-dom/client'
import { BrowserRouter, Routes, Route } from 'react-router-dom'
import App from './App'
import Dashboard from './components/Dashboard'
import History from './components/History'
import '@/i18n' // Initialize i18n
import './index.css'

// Lazy load heavy components — keeps the initial bundle small
const Editor = lazy(() => import('./components/Editor'))
const DAGEditor = lazy(() => import('./components/DAGEditor'))
const Execution = lazy(() => import('./components/Execution'))
const ChatPanel = lazy(() => import('./components/ChatPanel'))

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
          <Route path="dag/:name" element={<Suspense fallback={<EditorLoading />}><DAGEditor /></Suspense>} />
          <Route path="dag-new" element={<Suspense fallback={<EditorLoading />}><DAGEditor /></Suspense>} />
          <Route path="execution/:id" element={<Suspense fallback={<EditorLoading />}><Execution /></Suspense>} />
          <Route path="history" element={<History />} />
          <Route path="chat" element={<Suspense fallback={<EditorLoading />}><ChatPanel /></Suspense>} />
        </Route>
      </Routes>
    </BrowserRouter>
  </React.StrictMode>,
)
