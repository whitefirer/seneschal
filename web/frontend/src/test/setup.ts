import '@testing-library/jest-dom/vitest'
import { afterEach, vi } from 'vitest'
import { cleanup } from '@testing-library/react'

// vitest 未启用 globals，RTL 的自动 cleanup 不生效；手动在每个用例后卸载，
// 避免多次 render 在同一个 document.body 里累积。
afterEach(() => {
  cleanup()
})

// 全局 i18n 实例（组件测试直接依赖默认 i18next 实例）
import '@/i18n'

// jsdom does not implement window.matchMedia; themeStore (and future
// component tests) need it. Controllable via __setPreferredDark.
let preferredDark = false

export function __setPreferredDark(dark: boolean) {
  preferredDark = dark
}

if (typeof window !== 'undefined' && !window.matchMedia) {
  window.matchMedia = ((query: string) => ({
    matches: preferredDark && query.includes('prefers-color-scheme: dark'),
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  })) as typeof window.matchMedia
}

// jsdom does not implement scrollIntoView; log panels auto-scroll on updates.
if (typeof Element !== 'undefined' && !Element.prototype.scrollIntoView) {
  Element.prototype.scrollIntoView = vi.fn()
}

// jsdom does not implement the async clipboard API; copy buttons use it.
if (typeof navigator !== 'undefined' && !navigator.clipboard) {
  Object.assign(navigator, {
    clipboard: {
      writeText: vi.fn().mockResolvedValue(undefined),
      readText: vi.fn().mockResolvedValue(''),
    },
  })
}
