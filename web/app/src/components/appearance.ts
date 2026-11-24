import { useSyncExternalStore } from 'react'

// Appearance preferences: theme, accent preset and density. Each is stored
// per browser and applied as a data attribute on <html>, which the tokens in
// styles/tokens.css read. The same rules run before first paint from
// public/appearance.js (keep the keys and defaults in step), so a reload
// shows no flash.

export type ThemePreference = 'system' | 'light' | 'dark'
export type Accent = 'indigo' | 'blue' | 'teal' | 'violet' | 'slate'
export type Density = 'comfortable' | 'compact'

export interface Appearance {
  theme: ThemePreference
  accent: Accent
  density: Density
}

export const themes: ThemePreference[] = ['system', 'light', 'dark']
export const accents: Accent[] = ['indigo', 'blue', 'teal', 'violet', 'slate']
export const densities: Density[] = ['comfortable', 'compact']

const THEME_KEY = 'ims.app.theme'
const ACCENT_KEY = 'ims.app.accent'
const DENSITY_KEY = 'ims.app.density'
const defaults: Appearance = { theme: 'system', accent: 'indigo', density: 'comfortable' }

function read<T extends string>(key: string, allowed: readonly T[], fallback: T): T {
  try {
    const value = localStorage.getItem(key)
    return allowed.includes(value as T) ? (value as T) : fallback
  } catch {
    return fallback
  }
}

function write(key: string, value: string): void {
  try {
    localStorage.setItem(key, value)
  } catch {
    /* storage disabled */
  }
}

const darkQuery = typeof window === 'undefined' ? null : window.matchMedia('(prefers-color-scheme: dark)')

let current: Appearance = {
  theme: read(THEME_KEY, themes, defaults.theme),
  accent: read(ACCENT_KEY, accents, defaults.accent),
  density: read(DENSITY_KEY, densities, defaults.density),
}
const listeners = new Set<() => void>()

function apply(): void {
  const root = document.documentElement
  const dark = current.theme === 'dark' || (current.theme === 'system' && Boolean(darkQuery?.matches))
  root.dataset.theme = dark ? 'dark' : 'light'
  root.dataset.accent = current.accent
  root.dataset.density = current.density
}

darkQuery?.addEventListener('change', () => {
  if (current.theme === 'system') apply()
})

export function setAppearance(patch: Partial<Appearance>): void {
  current = { ...current, ...patch }
  if (patch.theme) write(THEME_KEY, patch.theme)
  if (patch.accent) write(ACCENT_KEY, patch.accent)
  if (patch.density) write(DENSITY_KEY, patch.density)
  apply()
  for (const listener of listeners) listener()
}

/** The next theme in the system → light → dark cycle. */
export function nextTheme(theme: ThemePreference): ThemePreference {
  return themes[(themes.indexOf(theme) + 1) % themes.length]
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

export function useAppearance(): Appearance {
  return useSyncExternalStore(
    subscribe,
    () => current,
    () => defaults,
  )
}
