import { create } from 'zustand'
import { persist } from 'zustand/middleware'

interface ThemeStore {
  theme: 'light' | 'dark' | 'system'
  isDark: boolean
  setTheme: (theme: 'light' | 'dark' | 'system') => void
  updateIsDark: () => void
}

function calculateIsDark(theme: 'light' | 'dark' | 'system'): boolean {
  if (theme === 'dark') return true
  if (theme === 'light') return false
  return window.matchMedia('(prefers-color-scheme: dark)').matches
}

export const useThemeStore = create<ThemeStore>()(
  persist(
    (set, get) => ({
      theme: 'system',
      isDark: calculateIsDark('system'),
      setTheme: (theme) => {
        const isDark = calculateIsDark(theme)
        set({ theme, isDark })
        updateTheme(theme)
      },
      updateIsDark: () => {
        const { theme } = get()
        const isDark = calculateIsDark(theme)
        set({ isDark })
      },
    }),
    {
      name: 'seneschal-theme',
    }
  )
)

function updateTheme(theme: 'light' | 'dark' | 'system') {
  const root = document.documentElement
  const isDark = calculateIsDark(theme)
  
  if (isDark) {
    root.classList.add('dark')
  } else {
    root.classList.remove('dark')
  }
}

// Initialize theme on mount
const storedTheme = localStorage.getItem('seneschal-theme')
if (storedTheme) {
  try {
    const { state } = JSON.parse(storedTheme)
    updateTheme(state.theme)
    useThemeStore.setState({ 
      theme: state.theme,
      isDark: calculateIsDark(state.theme)
    })
  } catch (e) {
    updateTheme('system')
  }
} else {
  updateTheme('system')
}

// Listen for system theme changes
window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
  const { theme } = useThemeStore.getState()
  if (theme === 'system') {
    updateTheme('system')
    useThemeStore.getState().updateIsDark()
  }
})
