import builtin from './languages.json'

// The languages the app can show. The built-in list (languages.json, the same
// descriptors as the API's i18n.Builtin(); a Go test keeps them equal) is the
// offline fallback; at start the app reads the server's list from
// GET /api/v1/languages, so APP_SUPPORTED_LOCALES decides what the menu offers.
// Adding a language: a descriptor here and in internal/app/i18n.go, plus
// catalogs/<namespace>/<code>.json (docs/guides/languages.md).

export interface Language {
  /** Two-letter code ("de"): catalog file names, Accept-Language, users.locale. */
  code: string
  /** The language's own name ("Deutsch"). */
  label: string
  /** Its English name ("German"). */
  english: string
  /** BCP 47 tag for dates and numbers ("bn-BD"). */
  intl: string
  dir: 'ltr' | 'rtl'
  /** Numbering system for generated numbers ("latn", "beng", "arab"). */
  digits: string
  /** False until the translation passed review: the menu marks it beta. */
  reviewed: boolean
}

/** The reference language: the text every other language is checked against. */
export const referenceLanguage = 'en'

export const builtinLanguages = builtin as Language[]

/** The tag Intl formats with: the language's tag and its digits. */
export function formatTag(language: Language): string {
  return language.intl.includes('-u-') || !language.digits ? language.intl : `${language.intl}-u-nu-${language.digits}`
}
