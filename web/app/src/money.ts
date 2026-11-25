import { api, type Schemas } from './api/client'
import { intlLocale } from './i18n'

// Money, Akaunting style: an amount is an integer in its currency's minor units (cents, paisa),
// and the currency's row says how to write it (symbol, where it goes, decimals, separators).
// Only the digits follow the language. Amounts are never converted between currencies.

export type Currency = Schemas['PublicCurrency']
export type CurrencyList = Schemas['PublicCurrenciesEnvelope']['data']

/** The enabled currencies and the default (the currency of plan prices and new payments). */
export const currenciesQuery = {
  queryKey: ['public-currencies'],
  queryFn: () => api<CurrencyList>('/public/currencies', { token: null }),
  staleTime: 300_000,
}

/** A currency without a row: its code as the symbol, two decimals. */
export function fallbackCurrency(code: string): Currency {
  return { code, name: code, symbol: code, symbol_first: true, precision: 2, decimal_mark: '.', thousands_separator: ',' }
}

/** The currency for code, the default without one, the fallback when unknown. */
export function findCurrency(list: CurrencyList | undefined, code?: string | null): Currency {
  const wanted = code || list?.default
  return list?.currencies.find((currency) => currency.code === wanted) ?? fallbackCurrency(wanted || 'USD')
}

const digitCache = new Map<string, string[]>()

/** The language's ten digits ('0'…'9', '০'…'৯'). */
function digitsFor(locale: string): string[] {
  let digits = digitCache.get(locale)
  if (!digits) {
    const format = new Intl.NumberFormat(intlLocale(locale), { useGrouping: false })
    digits = Array.from({ length: 10 }, (_, d) => format.format(d))
    digitCache.set(locale, digits)
  }
  return digits
}

function withSymbol(number: string, currency: Currency): string {
  if (!currency.symbol_first) return `${number} ${currency.symbol}`
  return [...currency.symbol].length > 1 ? `${currency.symbol} ${number}` : `${currency.symbol}${number}`
}

/** "৳১,২৩৪.৫০", "€1.234,50", "1 234,50 kr"; compact for chart axes and KPIs ("$1.2K"). */
export function formatMoney(minor: number, currency: Currency, locale: string, options: { compact?: boolean } = {}): string {
  const negative = minor < 0
  let number: string
  if (options.compact) {
    number = new Intl.NumberFormat(intlLocale(locale), { notation: 'compact', maximumFractionDigits: 1 }).format(Math.abs(minor) / 10 ** currency.precision)
  } else {
    const raw = String(Math.abs(Math.trunc(minor))).padStart(currency.precision + 1, '0')
    const whole = raw.slice(0, raw.length - currency.precision)
    const fraction = currency.precision > 0 ? raw.slice(raw.length - currency.precision) : ''
    const grouped = currency.thousands_separator ? whole.replace(/\B(?=(\d{3})+(?!\d))/g, currency.thousands_separator) : whole
    const digits = digitsFor(locale)
    number = (fraction ? `${grouped}${currency.decimal_mark}${fraction}` : grouped).replace(/[0-9]/g, (d) => digits[Number(d)])
  }
  return `${negative ? '-' : ''}${withSymbol(number, currency)}`
}

/** Minor units → what an input shows ("1234.50", "1234,50" for a comma currency). */
export function fromMinor(minor: number, currency: Currency): string {
  const raw = String(Math.abs(Math.trunc(minor))).padStart(currency.precision + 1, '0')
  if (currency.precision === 0) return (minor < 0 ? '-' : '') + raw
  const mark = currency.decimal_mark || '.'
  return `${minor < 0 ? '-' : ''}${raw.slice(0, -currency.precision)}${mark}${raw.slice(-currency.precision)}`
}

/**
 * What someone typed → minor units, or null when it isn't an amount in this currency. The
 * currency's decimal mark and "." both work; thousands separators and spaces are ignored; more
 * decimals than the currency has is refused (never rounded away).
 */
export function toMinor(text: string, currency: Currency): number | null {
  let value = text.trim().replace(/\s/g, '')
  if (currency.thousands_separator && currency.thousands_separator !== currency.decimal_mark && currency.thousands_separator !== '.') {
    value = value.split(currency.thousands_separator).join('')
  }
  if (currency.decimal_mark && currency.decimal_mark !== '.') value = value.replace(currency.decimal_mark, '.')
  const match = /^(\d+)(?:\.(\d*))?$/.exec(value)
  if (!match) return null
  const fraction = match[2] ?? ''
  if (fraction.length > currency.precision) return null
  return Number(match[1]) * 10 ** currency.precision + Number(fraction.padEnd(currency.precision, '0') || 0)
}
