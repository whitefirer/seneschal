import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'

import en from './locales/en.json'
import zh from './locales/zh.json'

// Get saved language from localStorage or detect from browser
const getSavedLanguage = (): string => {
  const saved = localStorage.getItem('language')
  if (saved && (saved === 'en' || saved === 'zh')) {
    return saved
  }
  
  // Detect from browser language
  const browserLang = navigator.language.toLowerCase()
  if (browserLang.startsWith('zh')) {
    return 'zh'
  }
  
  return 'en'
}

i18n
  .use(initReactI18next)
  .init({
    resources: {
      en: { translation: en },
      zh: { translation: zh },
    },
    lng: getSavedLanguage(),
    fallbackLng: 'en',
    interpolation: {
      escapeValue: false, // React already escapes values
    },
  })

export default i18n

export const languages = [
  { code: 'zh', name: '中文', flag: '🇨🇳' },
  { code: 'en', name: 'English', flag: '🇺🇸' },
]

export function changeLanguage(lang: string): void {
  i18n.changeLanguage(lang)
  localStorage.setItem('language', lang)
}

export function getCurrentLanguage(): string {
  return i18n.language
}