/**
 * Smart CDN Selector for Monaco Editor
 * 
 * Detects user location and selects the optimal CDN:
 * - China: use domestic mirrors (faster access)
 * - Other regions: use jsdelivr CDN (global coverage)
 * 
 * Speed test results (from China, 300KB file):
 * - BootCDN: 1.27s (FASTEST - domestic CDN)
 * - cdn.jsdelivr: 3.02s (moderate)
 * - fastly.jsdelivr: 3.87s (moderate)
 * - gcore.jsdelivr: 6.95s (slowest)
 * 
 * Auto-fallback: When primary CDN fails, automatically try next CDN in list
 */

import { loader } from '@monaco-editor/react'

// CDN configurations
const CDN_CONFIGS = {
  // Domestic (China) mirrors - ordered by speed (tested from China)
  domestic: [
    'https://cdn.bootcdn.net/ajax/libs', // BootCDN - fastest in China (1.27s for 300KB)
    'https://cdn.jsdelivr.net/npm', // jsdelivr global - moderate (3.02s)
    'https://fastly.jsdelivr.net/npm', // Fastly - moderate (3.87s)
    'https://gcore.jsdelivr.net/npm', // Gcore - slowest (6.95s)
  ],
  // Global CDN
  global: [
    'https://cdn.jsdelivr.net/npm', // jsdelivr global
    'https://unpkg.com', // unpkg
  ],
}

// Monaco version to use
const MONACO_VERSION = '0.45.0'

// Track current CDN for fallback
let currentCDNList: string[] = []
let currentCDNIndex = 0

/**
 * Detect if user is likely in China
 * Uses multiple heuristics for better accuracy
 */
export function isInChina(): boolean {
  // Check 1: Browser language contains 'zh' or 'cn'
  const language = navigator.language || ''
  const languages = navigator.languages || []
  
  const hasChineseLang = language.toLowerCase().includes('zh') ||
    language.toLowerCase().includes('cn') ||
    languages.some(l => l.toLowerCase().includes('zh') || l.toLowerCase().includes('cn'))
  
  // Check 2: Timezone offset matches China (UTC+8)
  const timezoneOffset = new Date().getTimezoneOffset()
  const isChinaTimezone = timezoneOffset === -480 // UTC+8 = -480 minutes offset
  
  // Check 3: Try to detect China timezone name
  try {
    const timezone = Intl.DateTimeFormat().resolvedOptions().timeZone
    const isChinaTimezoneName = timezone === 'Asia/Shanghai' || 
      timezone === 'Asia/Beijing' ||
      timezone === 'Asia/Chongqing' ||
      timezone === 'Asia/Hong_Kong' ||
      timezone === 'Asia/Macau'
    
    if (isChinaTimezoneName) return true
  } catch {
    // Timezone detection not supported
  }
  
  // Combine heuristics - if both language and timezone match, definitely in China
  // If only one matches, still likely in China
  return hasChineseLang && isChinaTimezone
}

/**
 * Build Monaco CDN path from base URL
 */
function buildMonacoPath(baseCDN: string): string {
  // Different CDN have different path formats
  if (baseCDN.includes('bootcdn')) {
    // BootCDN format: /ajax/libs/monaco-editor/VERSION/min/vs
    return `${baseCDN}/monaco-editor/${MONACO_VERSION}/min/vs`
  } else {
    // jsdelivr/unpkg format: /monaco-editor@VERSION/min/vs
    return `${baseCDN}/monaco-editor@${MONACO_VERSION}/min/vs`
  }
}

/**
 * Get Monaco Editor CDN path (first CDN in list)
 */
export function getMonacoCDNPath(): string {
  const inChina = isInChina()
  currentCDNList = inChina ? CDN_CONFIGS.domestic : CDN_CONFIGS.global
  currentCDNIndex = 0
  
  return buildMonacoPath(currentCDNList[currentCDNIndex])
}

/**
 * Get next fallback CDN path
 * Returns null if no more fallbacks available
 */
export function getNextFallbackCDN(): string | null {
  currentCDNIndex++
  
  if (currentCDNIndex >= currentCDNList.length) {
    console.error('[CDN Selector] All CDN fallbacks exhausted')
    return null
  }
  
  const nextCDN = buildMonacoPath(currentCDNList[currentCDNIndex])
  console.warn(`[CDN Selector] Falling back to CDN #${currentCDNIndex + 1}: ${nextCDN}`)
  
  return nextCDN
}

/**
 * Configure Monaco loader with fallback support
 */
export function configureMonacoLoader(): void {
  const initialPath = getMonacoCDNPath()
  loader.config({ paths: { vs: initialPath } })
  
  // Handle loader errors and try fallback
  loader.init().catch((error) => {
    console.error('[CDN Selector] Monaco loader failed:', error)
    
    const fallbackPath = getNextFallbackCDN()
    if (fallbackPath) {
      loader.config({ paths: { vs: fallbackPath } })
      return loader.init()
    }
    
    throw error
  })
}

/**
 * Log CDN selection for debugging
 */
export function logCDNSelection(): void {
  const inChina = isInChina()
  const cdn = getMonacoCDNPath()
  const allCDNs = inChina ? CDN_CONFIGS.domestic : CDN_CONFIGS.global
  
  console.log(`[CDN Selector] User region: ${inChina ? 'China' : 'Global'}`)
  console.log(`[CDN Selector] Primary CDN: ${cdn}`)
  console.log(`[CDN Selector] Fallback chain: ${allCDNs.map(buildMonacoPath).join(' → ')}`)
  console.log(`[CDN Selector] Language: ${navigator.language}, Timezone offset: ${new Date().getTimezoneOffset()}`)
}