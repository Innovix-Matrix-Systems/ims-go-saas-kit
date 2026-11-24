import { useCallback, useSyncExternalStore } from 'react'
import { bn } from './bn'
import { en, type MessageKey, type Messages } from './en'

// A small typed i18n layer: one catalog per language, the current language
// stored per browser and mirrored on <html lang>, and t(key, vars) for text.
// The API is told the language too (Accept-Language), so server messages match.

export type Locale = 'en' | 'bn'

export const locales: { code: Locale; label: string; intl: string }[] = [
  { code: 'en', label: 'English', intl: 'en' },
  { code: 'bn', label: 'বাংলা', intl: 'bn-BD' },
]

const catalogs: Record<Locale, Messages> = { en, bn }
const LOCALE_KEY = 'ims.app.locale'

function detect(): Locale {
  try {
    const stored = localStorage.getItem(LOCALE_KEY)
    if (stored === 'en' || stored === 'bn') return stored
  } catch {
    /* storage disabled */
  }
  const browser = typeof navigator === 'undefined' ? '' : navigator.language.toLowerCase()
  return browser.startsWith('bn') ? 'bn' : 'en'
}

let current: Locale = detect()
const listeners = new Set<() => void>()

function applyDocument(): void {
  if (typeof document !== 'undefined') document.documentElement.lang = current
}
applyDocument()

/** A language's own name ("English", "বাংলা"), or the code when unknown. */
export function localeLabel(code: string): string {
  return locales.find((item) => item.code === code)?.label ?? code
}

export function getLocale(): Locale {
  return current
}

export function intlLocale(locale: Locale = current): string {
  return locales.find((item) => item.code === locale)?.intl ?? 'en'
}

export function setLocale(locale: Locale): void {
  if (locale === current) return
  current = locale
  try {
    localStorage.setItem(LOCALE_KEY, locale)
  } catch {
    /* storage disabled */
  }
  applyDocument()
  for (const listener of listeners) listener()
}

/** Translates a key for the current language, replacing {name} placeholders. */
export function translate(key: MessageKey, vars?: Record<string, string | number>, locale: Locale = current): string {
  const template = catalogs[locale][key] ?? en[key] ?? key
  if (!vars) return template
  return template.replace(/\{(\w+)\}/g, (match, name: string) => (name in vars ? String(vars[name]) : match))
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

export function useLocale(): Locale {
  return useSyncExternalStore(subscribe, getLocale, () => 'en')
}

/** The translate function bound to the current language (re-renders on change). */
export function useT(): (key: MessageKey, vars?: Record<string, string | number>) => string {
  const locale = useLocale()
  return useCallback((key, vars) => translate(key, vars, locale), [locale])
}

export type { MessageKey }
