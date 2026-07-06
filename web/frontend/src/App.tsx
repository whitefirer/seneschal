import { Outlet, Link, useLocation } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { useThemeStore } from '@/store/themeStore'
import { LanguageSwitcher } from '@/components/LanguageSwitcher'
import { Moon, Sun, Monitor, Workflow, History, FolderOpen, MessageSquare } from 'lucide-react'

export default function App() {
  const location = useLocation()
  const { t } = useTranslation()
  const { theme, setTheme } = useThemeStore()

  const navItems = [
    { path: '/', label: t('nav.dashboard'), icon: FolderOpen },
    { path: '/chat', label: 'Chat', icon: MessageSquare },
    { path: '/history', label: t('nav.history'), icon: History },
  ]

  return (
    <div className="min-h-screen bg-background">
      {/* Header */}
      <header className="sticky top-0 z-50 w-full border-b border-border bg-background/95 backdrop-blur supports-[backdrop-filter]:bg-background/60">
        <div className="h-14 px-4 flex items-center justify-between">
          <div className="flex items-center gap-6">
            <Link to="/" className="flex items-center gap-2 font-semibold">
              <Workflow className="h-6 w-6 text-primary" />
              <span>goworkflow</span>
            </Link>
            <nav className="flex items-center gap-4">
              {navItems.map((item) => {
                const Icon = item.icon
                const isActive = location.pathname === item.path
                return (
                  <Link
                    key={item.path}
                    to={item.path}
                    className={`flex items-center gap-2 text-sm font-medium transition-colors hover:text-primary ${
                      isActive ? 'text-primary' : 'text-muted-foreground'
                    }`}
                  >
                    <Icon className="h-4 w-4" />
                    {item.label}
                  </Link>
                )
              })}
            </nav>
          </div>

          {/* Theme Toggle & Language Switcher */}
          <div className="flex items-center gap-2">
            <LanguageSwitcher />
            <div className="flex items-center rounded-md border border-border p-1">
              <button
                onClick={() => setTheme('light')}
                className={`p-1.5 rounded ${theme === 'light' ? 'bg-accent text-accent-foreground' : 'text-muted-foreground hover:text-foreground'}`}
                title={t('theme.switchToLight')}
              >
                <Sun className="h-4 w-4" />
              </button>
              <button
                onClick={() => setTheme('system')}
                className={`p-1.5 rounded ${theme === 'system' ? 'bg-accent text-accent-foreground' : 'text-muted-foreground hover:text-foreground'}`}
                title={t('theme.switchToSystem')}
              >
                <Monitor className="h-4 w-4" />
              </button>
              <button
                onClick={() => setTheme('dark')}
                className={`p-1.5 rounded ${theme === 'dark' ? 'bg-accent text-accent-foreground' : 'text-muted-foreground hover:text-foreground'}`}
                title={t('theme.switchToDark')}
              >
                <Moon className="h-4 w-4" />
              </button>
            </div>
          </div>
        </div>
      </header>

      {/* Main Content */}
      <main className="h-[calc(100vh-3.5rem)]">
        <Outlet />
      </main>
    </div>
  )
}
