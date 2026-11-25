import { useCallback, useSyncExternalStore } from 'react'
import { builtinLanguages, formatTag, referenceLanguage, type Language } from './languages'
import type { MessageKey, MessageVars } from './messages.gen'

// A small typed i18n layer.
//
// - Catalogs are JSON per namespace and language (catalogs/<namespace>/<code>.json). English
//   is bundled as the reference and the fallback; every other language is loaded when it is
//   chosen, in one chunk, and shown only once it has arrived (never raw keys).
// - A project changes wording in overrides/<namespace>/<code>.json, merged last.
// - A value is a string or CLDR plural forms selected by {count} (Intl.PluralRules).
// - The current language is stored per browser and mirrored on <html lang dir>; the API is
//   told it too (Accept-Language), so server messages match.
// - `npm run i18n:types` generates the key and placeholder types (messages.gen.ts) and
//   `npm run i18n:check` fails while a language is incomplete.

type Plural = Partial<Record<Intl.LDMLPluralRule, string>> & { other: string }
type Value = string | Plural
type Catalog = Record<string, Value>
type Vars = Record<string, string | number>

/** The variables a key needs: none, or one value per {placeholder}. */
export type VarsArgs<K extends MessageKey> = MessageKey extends K
  ? [vars?: Vars] // a key built at run time (`billing.status.${s}` as MessageKey)
  : [MessageVars[K]] extends [never]
    ? [vars?: undefined]
    : [vars: Record<MessageVars[K], string | number>]

export type Translate = <K extends MessageKey>(key: K, ...args: VarsArgs<K>) => string

const english = mergeModules(
  import.meta.glob<Catalog>('./catalogs/*/en.json', { eager: true, import: 'default' }),
  import.meta.glob<Catalog>('./overrides/*/en.json', { eager: true, import: 'default' }),
)
const loaders = import.meta.glob<Catalog>(['./catalogs/*/*.json', '!./catalogs/*/en.json'], { import: 'default' })
const overrideLoaders = import.meta.glob<Catalog>(['./overrides/*/*.json', '!./overrides/*/en.json'], { import: 'default' })

function mergeModules(...groups: Record<string, Catalog>[]): Catalog {
  const out: Catalog = {}
  for (const group of groups) for (const catalog of Object.values(group)) Object.assign(out, catalog)
  return out
}

const catalogs: Record<string, Catalog> = { [referenceLanguage]: english }
const loading: Record<string, Promise<void>> = {}

/** Loads a language's catalogs (and overrides) once. */
function loadCatalog(code: string): Promise<void> {
  if (catalogs[code]) return Promise.resolve()
  loading[code] ??= (async () => {
    const pick = (group: Record<string, () => Promise<Catalog>>) =>
      Object.entries(group).filter(([path]) => path.endsWith(`/${code}.json`)).map(([, load]) => load())
    const [own, overrides] = await Promise.all([Promise.all(pick(loaders)), Promise.all(pick(overrideLoaders))])
    catalogs[code] = Object.assign({}, ...own, ...overrides)
  })()
  return loading[code]
}

let languages: Language[] = builtinLanguages
let defaultCode = referenceLanguage
const LOCALE_KEY = 'ims.app.locale'

function find(code: string | null | undefined): Language | undefined {
  return code ? languages.find((language) => language.code === code.toLowerCase()) : undefined
}

/** The language to start with: this browser's pick, then the browser's languages, then the default. */
function detect(): string {
  try {
    const stored = find(localStorage.getItem(LOCALE_KEY))
    if (stored) return stored.code
  } catch {
    /* storage disabled */
  }
  const preferred = typeof navigator === 'undefined' ? [] : (navigator.languages ?? [navigator.language])
  for (const tag of preferred) {
    const match = find(tag.split('-')[0])
    if (match) return match.code
  }
  return defaultCode
}

let current = referenceLanguage
let pending = detect()
const listeners = new Set<() => void>()

function notify(): void {
  for (const listener of listeners) listener()
}

function applyDocument(): void {
  if (typeof document === 'undefined') return
  const language = find(current)
  document.documentElement.lang = current
  document.documentElement.dir = language?.dir ?? 'ltr'
}

/** Switches to a language once its catalogs have loaded. */
async function activate(code: string): Promise<void> {
  pending = code
  await loadCatalog(code).catch(() => undefined)
  if (pending !== code || !catalogs[code]) return // another choice came first, or loading failed
  current = code
  applyDocument()
  notify()
}

/**
 * Starts i18n: reads the server's languages (GET /api/v1/languages; the built-in list when
 * offline) and loads the starting language. The app renders after this resolves.
 */
export async function startI18n(): Promise<void> {
  try {
    // A plain fetch, not the API client: the client imports getLocale from here (it sends
    // Accept-Language), and it must not run before the language is known. The server caches the
    // answer for five minutes (Cache-Control), so a language added or switched off reaches open
    // tabs on their next load after that.
    const response = await fetch('/api/v1/languages', { headers: { Accept: 'application/json' } })
    if (response.ok) {
      const body = (await response.json()) as { data: { default: string; languages: Language[] } }
      if (body.data.languages.length > 0) {
        languages = body.data.languages
        defaultCode = body.data.default
        pending = detect()
      }
    }
  } catch {
    /* offline: the built-in list */
  }
  await activate(find(pending)?.code ?? defaultCode)
}

/** Every language the app offers, the reference first. */
export function getLanguages(): Language[] {
  return languages
}

/** A language's own name ("English", "বাংলা"), or the code when unknown. */
export function localeLabel(code: string): string {
  return find(code)?.label ?? code
}

/** Whether code is a language the app offers. */
export function isLocale(code: string | null | undefined): boolean {
  return !!find(code)
}

export function getLocale(): string {
  return current
}

/** The Intl tag for dates and numbers: the language's tag and digits. */
export function intlLocale(locale: string = current): string {
  const language = find(locale)
  return language ? formatTag(language) : referenceLanguage
}

/** Chooses a language for this browser; the text changes once its catalogs arrive. */
export function setLocale(locale: string): Promise<void> {
  const language = find(locale)
  if (!language) return Promise.resolve()
  try {
    localStorage.setItem(LOCALE_KEY, language.code)
  } catch {
    /* storage disabled */
  }
  if (language.code === current) return Promise.resolve()
  return activate(language.code)
}

const pluralRules = new Map<string, Intl.PluralRules>()

function select(value: Value, locale: string, count: unknown): string {
  if (typeof value === 'string') return value
  if (typeof count !== 'number') return value.other
  let rules = pluralRules.get(locale)
  if (!rules) {
    rules = new Intl.PluralRules(intlLocale(locale))
    pluralRules.set(locale, rules)
  }
  return value[rules.select(count)] ?? value.other
}

/** Whether a key exists (for keys built at run time, like activity.label.<action>). */
export function hasMessage(key: string): boolean {
  return key in english
}

/** Translates a key for a language, replacing {name} placeholders. */
export function translate(key: MessageKey, vars?: Vars, locale: string = current): string {
  const value = catalogs[locale]?.[key] ?? english[key]
  if (value === undefined) return key
  const template = select(value, catalogs[locale]?.[key] !== undefined ? locale : referenceLanguage, vars?.count)
  if (!vars) return template
  return template.replace(/\{(\w+)\}/g, (match, name: string) => {
    if (!(name in vars)) return match
    const v = vars[name]
    // {count} is shown in the language's digits ("৩ দিন").
    return name === 'count' && typeof v === 'number' ? new Intl.NumberFormat(intlLocale(locale)).format(v) : String(v)
  })
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

export function useLocale(): string {
  return useSyncExternalStore(subscribe, getLocale, () => referenceLanguage)
}

/** The translate function bound to the current language (re-renders on change). */
export function useT(): Translate {
  const locale = useLocale()
  return useCallback(((key: MessageKey, vars?: Vars) => translate(key, vars, locale)) as Translate, [locale])
}

export type { Language, MessageKey }
