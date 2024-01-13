import React, { lazy, Suspense } from 'react'
import ReactDOM from 'react-dom/client'
import { BrowserRouter, Routes, Route } from 'react-router-dom'
import App from './App'
import Dashboard from './components/Dashboard'
import Execution from './components/Execution'
import History from './components/History'
import '@/i18n' // Initialize i18n
import './index.css'

// Lazy load Editor component (includes Monaco Editor)
const Editor = lazy(() => import('./components/Editor'))

// Lazy load DAG Editor component
const DAGEditor = lazy(() => import('./components/DAGEditor'))

// Loading fallback for lazy components
function EditorLoading() {
  return (
    <div className="flex items-center justify-center h-screen bg-gray-50 dark:bg-gray-900">
      <div className="flex flex-col items-center gap-4">
        <div className="animate-spin rounded-full h-12 w-12 border-4 border-blue-500 border-t-transparent"></div>
        <span className="text-gray-500 dark:text-gray-400">Loading Editor...</span>
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
          <Route path="execution/:id" element={<Execution />} />
          <Route path="history" element={<History />} />
        </Route>
      </Routes>
    </BrowserRouter>
  </React.StrictMode>,
)
