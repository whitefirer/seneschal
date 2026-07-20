import '@testing-library/jest-dom/vitest'

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
