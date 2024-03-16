import { useState, useRef, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { languages, changeLanguage } from '@/i18n'
import { Globe } from 'lucide-react'

export function LanguageSwitcher() {
  const { t, i18n } = useTranslation()
  const [isOpen, setIsOpen] = useState(false)
  const dropdownRef = useRef<HTMLDivElement>(null)

  // Close dropdown when clicking outside
  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (dropdownRef.current && !dropdownRef.current.contains(event.target as Node)) {
        setIsOpen(false)
      }
    }

    document.addEventListener('mousedown', handleClickOutside)
    return () => document.removeEventListener('mousedown', handleClickOutside)
  }, [])

  const handleLanguageChange = (langCode: string) => {
    changeLanguage(langCode)
    setIsOpen(false)
  }

  const currentLang = languages.find(l => l.code === i18n.language) || languages[1]

  return (
    <div className="relative" ref={dropdownRef}>
      <button
        onClick={() => setIsOpen(!isOpen)}
        className="flex items-center gap-2 px-3 py-2 rounded-lg 
                   text-gray-600 dark:text-gray-300 
                   hover:bg-gray-100 dark:hover:bg-gray-800 
                   transition-colors"
        title={t('common.language')}
      >
        <Globe className="w-4 h-4" />
        <span className="text-sm">{currentLang.flag}</span>
      </button>

      {isOpen && (
        <div className="absolute right-0 mt-2 w-40 
                        bg-white dark:bg-gray-800 
                        border border-gray-200 dark:border-gray-700 
                        rounded-lg shadow-lg 
                        overflow-hidden z-50">
          {languages.map((lang) => (
            <button
              key={lang.code}
              onClick={() => handleLanguageChange(lang.code)}
              className={`w-full flex items-center gap-3 px-4 py-2.5 
                          text-left text-sm
                          hover:bg-gray-100 dark:hover:bg-gray-700 
                          transition-colors
                          ${i18n.language === lang.code 
                            ? 'bg-blue-50 dark:bg-blue-900/20 text-blue-600 dark:text-blue-400' 
                            : 'text-gray-700 dark:text-gray-300'}`}
            >
              <span>{lang.flag}</span>
              <span>{lang.name}</span>
              {i18n.language === lang.code && (
                <span className="ml-auto text-blue-500">✓</span>
              )}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}