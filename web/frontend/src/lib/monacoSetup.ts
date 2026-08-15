// 本地打包 Monaco（离线可用，不依赖 CDN）
import * as monaco from 'monaco-editor'
import editorWorker from 'monaco-editor/esm/vs/editor/editor.worker?worker'
import { loader } from '@monaco-editor/react'

// 使用本地打包的 editor worker（避免运行时从 CDN 拉取）
self.MonacoEnvironment = {
  getWorker() {
    return new editorWorker()
  },
}

// 让 @monaco-editor/react 使用本地 monaco 实例
loader.config({ monaco })
