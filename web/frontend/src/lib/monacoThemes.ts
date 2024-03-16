import type * as Monaco from 'monaco-editor'

// Custom dark theme for Monaco Editor
const customDarkTheme: Monaco.editor.IStandaloneThemeData = {
  base: 'vs-dark' as const,
  inherit: true,
  rules: [
    { token: 'comment', foreground: '6b7280', fontStyle: 'italic' },
    { token: 'string', foreground: '86efac' },
    { token: 'number', foreground: 'fca5a5' },
    { token: 'keyword', foreground: '93c5fd' },
    { token: 'operator', foreground: 'f0abfc' },
    { token: 'delimiter', foreground: 'f0abfc' },
    { token: 'type', foreground: 'fcd34d' },
    { token: 'annotation', foreground: 'fcd34d' },
    { token: 'tag', foreground: '93c5fd' },
    { token: 'attribute.name', foreground: 'fca5a5' },
    { token: 'attribute.value', foreground: '86efac' },
  ],
  colors: {
    'editor.background': '#1f2937',
    'editor.foreground': '#f3f4f6',
    'editor.lineHighlightBackground': '#374151',
    'editorCursor.foreground': '#60a5fa',
    'editor.selectionBackground': '#374151',
    'editor.inactiveSelectionBackground': '#1f2937',
    'editorLineNumber.foreground': '#6b7280',
    'editorLineNumber.activeForeground': '#9ca3af',
    'editorIndentGuide.background': '#374151',
    'editorIndentGuide.activeBackground': '#4b5563',
    'editorHoverWidget.background': '#1f2937',
    'editorHoverWidget.border': '#374151',
    'editorWidget.background': '#1f2937',
    'editorWidget.border': '#374151',
    'input.background': '#111827',
    'input.border': '#374151',
    'input.foreground': '#f3f4f6',
    'scrollbar.shadow': '#000000',
    'scrollbarSlider.background': '#374151',
    'scrollbarSlider.hoverBackground': '#4b5563',
    'scrollbarSlider.activeBackground': '#6b7280',
  },
}

// Custom light theme for Monaco Editor
const customLightTheme: Monaco.editor.IStandaloneThemeData = {
  base: 'vs' as const,
  inherit: true,
  rules: [
    { token: 'comment', foreground: '6b7280', fontStyle: 'italic' },
    { token: 'string', foreground: '16a34a' },
    { token: 'number', foreground: 'dc2626' },
    { token: 'keyword', foreground: '2563eb' },
    { token: 'operator', foreground: '9333ea' },
    { token: 'delimiter', foreground: '9333ea' },
    { token: 'type', foreground: 'b45309' },
    { token: 'annotation', foreground: 'b45309' },
    { token: 'tag', foreground: '2563eb' },
    { token: 'attribute.name', foreground: 'dc2626' },
    { token: 'attribute.value', foreground: '16a34a' },
  ],
  colors: {
    'editor.background': '#ffffff',
    'editor.foreground': '#1f2937',
    'editor.lineHighlightBackground': '#f3f4f6',
    'editorCursor.foreground': '#2563eb',
    'editor.selectionBackground': '#e5e7eb',
    'editor.inactiveSelectionBackground': '#f9fafb',
    'editorLineNumber.foreground': '#9ca3af',
    'editorLineNumber.activeForeground': '#4b5563',
    'editorIndentGuide.background': '#e5e7eb',
    'editorIndentGuide.activeBackground': '#d1d5db',
    'editorHoverWidget.background': '#ffffff',
    'editorHoverWidget.border': '#e5e7eb',
    'editorWidget.background': '#ffffff',
    'editorWidget.border': '#e5e7eb',
    'input.background': '#f9fafb',
    'input.border': '#e5e7eb',
    'input.foreground': '#1f2937',
    'scrollbar.shadow': '#00000033',
    'scrollbarSlider.background': '#e5e7eb',
    'scrollbarSlider.hoverBackground': '#d1d5db',
    'scrollbarSlider.activeBackground': '#9ca3af',
  },
}

// Register custom themes - call this in Monaco's onMount callback
export function registerMonacoThemes(monaco: typeof Monaco) {
  monaco.editor.defineTheme('dark', customDarkTheme)
  monaco.editor.defineTheme('light', customLightTheme)
}
