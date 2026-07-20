import { describe, it, expect, beforeEach } from 'vitest'
import { useThemeStore } from './themeStore'
import { __setPreferredDark } from '../test/setup'

describe('themeStore', () => {
  beforeEach(() => {
    __setPreferredDark(false)
    document.documentElement.classList.remove('dark')
  })

  it('setTheme(dark) forces dark mode and applies the root class', () => {
    useThemeStore.getState().setTheme('dark')
    expect(useThemeStore.getState().theme).toBe('dark')
    expect(useThemeStore.getState().isDark).toBe(true)
    expect(document.documentElement.classList.contains('dark')).toBe(true)
  })

  it('setTheme(light) forces light mode and removes the root class', () => {
    useThemeStore.getState().setTheme('dark')
    useThemeStore.getState().setTheme('light')
    expect(useThemeStore.getState().isDark).toBe(false)
    expect(document.documentElement.classList.contains('dark')).toBe(false)
  })

  it('system theme follows the OS preference, updateIsDark re-reads it', () => {
    __setPreferredDark(true)
    useThemeStore.getState().setTheme('system')
    expect(useThemeStore.getState().isDark).toBe(true)

    // updateIsDark only recomputes the flag; the root class is updated by
    // setTheme / the matchMedia change listener (updateTheme).
    __setPreferredDark(false)
    useThemeStore.getState().updateIsDark()
    expect(useThemeStore.getState().isDark).toBe(false)
  })
})
