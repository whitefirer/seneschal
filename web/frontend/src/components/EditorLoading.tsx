// Loading fallback for lazy-loaded editor/execution/chat routes.
export default function EditorLoading() {
  return (
    <div className="flex items-center justify-center h-screen bg-gray-50 dark:bg-gray-900">
      <div className="flex flex-col items-center gap-4">
        <div className="animate-spin rounded-full h-12 w-12 border-4 border-blue-500 border-t-transparent"></div>
        <span className="text-gray-500 dark:text-gray-400">Loading...</span>
      </div>
    </div>
  )
}
