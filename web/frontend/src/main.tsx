import React, { lazy, Suspense } from 'react'
import ReactDOM from 'react-dom/client'
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import App from './App'
import Dashboard from './components/Dashboard'
import History from './components/History'
import Runbooks from './components/Runbooks'
import EditorLoading from './components/EditorLoading'
import DagRedirect from './components/DagRedirect'
import '@/i18n' // Initialize i18n
import './index.css'

// Lazy load heavy components — keeps the initial bundle small
const Editor = lazy(() => import('./components/Editor'))
const Execution = lazy(() => import('./components/Execution'))
const ChatPanel = lazy(() => import('./components/ChatPanel'))

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
