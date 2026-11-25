import React, {
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent,
  type ReactNode,
} from 'react'
import { intlLocale, useLocale, useT } from '../i18n'
import type { Currency } from '../money'
import { Icon, useEscape } from './ui'

/* Dashboard form controls that follow the theme everywhere (native
 * <select> and date inputs render with OS chrome). All are controlled:
 * value + onChange, keyboard accessible, close on Escape / outside click. */

/** Closes a popover on Escape (when it is the top overlay) or a click outside the returned ref. */
export function useDismiss(open: boolean, close: () => void) {
  const ref = useRef<HTMLDivElement>(null)
  useEscape(open, close)
  useEffect(() => {
    if (!open) return
    const onDown = (event: MouseEvent) => {
      if (ref.current && !ref.current.contains(event.target as Node)) close()
    }
    document.addEventListener('mousedown', onDown)
    return () => document.removeEventListener('mousedown', onDown)
  }, [open, close])
  return ref
}

// A popover inside a <label className="field"> would otherwise re-click the
// trigger (label activation) after every pick.
const stopLabel = (event: { preventDefault: () => void }) => event.preventDefault()

/**
 * Keeps a popover on screen: right-aligned when it would overflow the right
 * edge, opened upwards when there is not room for it below the trigger (a
 * per-page picker at the bottom of a list).
 */
function useAlign(open: boolean, ref: React.RefObject<HTMLDivElement | null>, width: number, height = 320) {
  const [placement, setPlacement] = useState('')
  useLayoutEffect(() => {
    if (!open || !ref.current) return
    const rect = ref.current.getBoundingClientRect()
    const right = rect.left + width > window.innerWidth - 12
    const below = window.innerHeight - rect.bottom
    const up = below < height + 12 && rect.top > below
    setPlacement(`${right ? ' align-right' : ''}${up ? ' align-up' : ''}`)
  }, [open, ref, width, height])
  return placement
}

// ── Select ────────────────────────────────────────────────────────────────

export interface SelectOption<T extends string = string> {
  value: T
  label: ReactNode
  /** Text used for type-to-search (defaults to the label when it is a string). */
  text?: string
  hint?: ReactNode
  disabled?: boolean
}

export function Select<T extends string = string>({
  value,
  onChange,
  options,
  placeholder,
  searchable,
  disabled,
  className,
  ariaLabel,
  size,
}: {
  value: T
  onChange: (value: T) => void
  options: SelectOption<T>[]
  placeholder?: string
  /** Show a filter box (defaults to on for more than 8 options). */
  searchable?: boolean
  disabled?: boolean
  className?: string
  ariaLabel?: string
  size?: 'sm'
}) {
  const t = useT()
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const [active, setActive] = useState(0)
  const close = useCallback(() => {
    setOpen(false)
    setQuery('')
  }, [])
  const ref = useDismiss(open, close)
  const listId = useId()
  const listRef = useRef<HTMLDivElement>(null)
  const showSearch = searchable ?? options.length > 8
  const align = useAlign(open, ref, 240)

  const filtered = useMemo(() => {
    const needle = query.trim().toLowerCase()
    if (!needle) return options
    return options.filter((option) =>
      (option.text ?? (typeof option.label === 'string' ? option.label : option.value))
        .toLowerCase()
        .includes(needle),
    )
  }, [options, query])

  const selected = options.find((option) => option.value === value)

  useEffect(() => {
    if (!open) return
    const index = filtered.findIndex((option) => option.value === value)
    setActive(index >= 0 ? index : 0)
  }, [open, filtered, value])

  useEffect(() => {
    listRef.current?.querySelector<HTMLElement>(`[data-index="${active}"]`)?.scrollIntoView({ block: 'nearest' })
  }, [active])

  const choose = (option: SelectOption<T> | undefined) => {
    if (!option || option.disabled) return
    onChange(option.value)
    close()
  }

  const onKeyDown = (event: KeyboardEvent) => {
    if (!open) {
      if (['ArrowDown', 'ArrowUp', 'Enter', ' '].includes(event.key)) {
        event.preventDefault()
        setOpen(true)
      }
      return
    }
    if (event.key === 'ArrowDown') {
      event.preventDefault()
      setActive((index) => Math.min(filtered.length - 1, index + 1))
    } else if (event.key === 'ArrowUp') {
      event.preventDefault()
      setActive((index) => Math.max(0, index - 1))
    } else if (event.key === 'Enter') {
      event.preventDefault()
      choose(filtered[active])
    } else if (event.key === 'Tab') {
      close()
    }
  }

  return (
    <div ref={ref} className={`ui-select ${size ?? ''} ${className ?? ''}`} onKeyDown={onKeyDown}>
      <button
        type="button"
        className={`ui-select-trigger ${open ? 'open' : ''}`}
        disabled={disabled}
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-controls={listId}
        aria-label={ariaLabel}
        onClick={() => (open ? close() : setOpen(true))}
      >
        <span className={`ui-select-value ${selected ? '' : 'placeholder'}`}>
          {selected ? selected.label : (placeholder ?? t('inputs.select'))}
        </span>
        <Icon name="chevron" className="ui-select-caret" />
      </button>
      {open ? (
        <div className={`ui-popover ui-select-menu${align}`} onClick={stopLabel}>
          {showSearch ? (
            <div className="ui-select-search">
              <Icon name="search" />
              <input
                autoFocus
                value={query}
                placeholder={t('inputs.filter')}
                onChange={(event) => {
                  setQuery(event.target.value)
                  setActive(0)
                }}
              />
            </div>
          ) : null}
          <div ref={listRef} id={listId} role="listbox" className="ui-select-list" tabIndex={-1}>
            {filtered.length === 0 ? <div className="ui-select-empty">{t('inputs.noMatches')}</div> : null}
            {filtered.map((option, index) => (
              <div
                key={option.value}
                role="option"
                data-index={index}
                aria-selected={option.value === value}
                aria-disabled={option.disabled}
                className={`ui-select-option ${index === active ? 'active' : ''} ${
                  option.value === value ? 'selected' : ''
                } ${option.disabled ? 'disabled' : ''}`}
                onMouseEnter={() => setActive(index)}
                onMouseDown={(event) => event.preventDefault()}
                onClick={() => choose(option)}
              >
                <span className="ui-select-option-label">{option.label}</span>
                {option.hint ? <span className="ui-select-option-hint">{option.hint}</span> : null}
                {option.value === value ? <Icon name="check" className="ui-select-check" /> : null}
              </div>
            ))}
          </div>
        </div>
      ) : null}
    </div>
  )
}

// ── Dates ─────────────────────────────────────────────────────────────────

const pad = (value: number) => String(value).padStart(2, '0')
const toISODate = (date: Date) => `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`

function parseISODate(value: string): Date | null {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value)
  if (!match) return null
  const date = new Date(Number(match[1]), Number(match[2]) - 1, Number(match[3]))
  return Number.isNaN(date.getTime()) ? null : date
}

/** Short weekday names, Monday first, in the app's language. */
function weekdayNames(locale: string): string[] {
  const format = new Intl.DateTimeFormat(locale, { weekday: 'short' })
  return Array.from({ length: 7 }, (_, i) => format.format(new Date(2024, 0, 1 + i)))
}

function monthGrid(year: number, month: number): Date[] {
  const first = new Date(year, month, 1)
  const offset = (first.getDay() + 6) % 7 // Monday first
  const start = new Date(year, month, 1 - offset)
  return Array.from({ length: 42 }, (_, index) => new Date(start.getFullYear(), start.getMonth(), start.getDate() + index))
}

/** Calendar date picker. value/onChange use YYYY-MM-DD ('' = empty). */
export function DatePicker({
  value,
  onChange,
  placeholder,
  min,
  max,
  clearable = true,
  ariaLabel,
}: {
  value: string
  onChange: (value: string) => void
  placeholder?: string
  min?: string
  max?: string
  clearable?: boolean
  ariaLabel?: string
}) {
  const t = useT()
  const locale = intlLocale(useLocale())
  const digits = new Intl.NumberFormat(locale)
  const [open, setOpen] = useState(false)
  const close = useCallback(() => setOpen(false), [])
  const ref = useDismiss(open, close)
  const selected = parseISODate(value)
  const align = useAlign(open, ref, 264, 340)
  const [view, setView] = useState(() => selected ?? new Date())

  useEffect(() => {
    if (open) setView(parseISODate(value) ?? new Date())
  }, [open, value])

  const today = toISODate(new Date())
  const days = monthGrid(view.getFullYear(), view.getMonth())
  const shift = (months: number) => setView(new Date(view.getFullYear(), view.getMonth() + months, 1))
  const outOfRange = (iso: string) => (min !== undefined && iso < min) || (max !== undefined && iso > max)
  const pick = (iso: string) => {
    if (outOfRange(iso)) return
    onChange(iso)
    close()
  }

  return (
    <div ref={ref} className="ui-select ui-date">
      <button
        type="button"
        className={`ui-select-trigger ${open ? 'open' : ''}`}
        aria-label={ariaLabel}
        aria-haspopup="dialog"
        aria-expanded={open}
        onClick={() => setOpen(!open)}
      >
        <Icon name="calendar" className="ui-date-icon" />
        <span className={`ui-select-value ${value ? '' : 'placeholder'}`}>
          {selected
            ? selected.toLocaleDateString(locale, { year: 'numeric', month: 'short', day: 'numeric' })
            : (placeholder ?? t('inputs.pickDate'))}
        </span>
        {clearable && value ? (
          <span
            role="button"
            tabIndex={-1}
            className="ui-clear"
            aria-label={t('inputs.clearDate')}
            onClick={(event) => {
              event.stopPropagation()
              onChange('')
            }}
          >
            <Icon name="x" />
          </span>
        ) : null}
      </button>
      {open ? (
        <div className={`ui-popover ui-calendar${align}`} role="dialog" aria-label={t('inputs.chooseDate')} onClick={stopLabel}>
          <div className="ui-calendar-head">
            <button type="button" className="icon-btn flip" aria-label={t('inputs.previousMonth')} onClick={() => shift(-1)}>
              <Icon name="chevron" />
            </button>
            <span>{view.toLocaleDateString(locale, { month: 'long', year: 'numeric' })}</span>
            <button type="button" className="icon-btn" aria-label={t('inputs.nextMonth')} onClick={() => shift(1)}>
              <Icon name="chevron" />
            </button>
          </div>
          <div className="ui-calendar-grid">
            {weekdayNames(locale).map((day) => (
              <span key={day} className="ui-calendar-weekday">
                {day}
              </span>
            ))}
            {days.map((day) => {
              const iso = toISODate(day)
              const classes = [
                'ui-calendar-day',
                day.getMonth() !== view.getMonth() ? 'outside' : '',
                iso === value ? 'selected' : '',
                iso === today ? 'today' : '',
                outOfRange(iso) ? 'disabled' : '',
              ].join(' ')
              return (
                <button key={iso} type="button" className={classes} onClick={() => pick(iso)}>
                  {digits.format(day.getDate())}
                </button>
              )
            })}
          </div>
          <div className="ui-calendar-foot">
            <button type="button" className="link-btn" onClick={() => pick(today)}>
              {t('inputs.today')}
            </button>
            {clearable ? (
              <button
                type="button"
                className="link-btn"
                onClick={() => {
                  onChange('')
                  close()
                }}
              >
                {t('inputs.clear')}
              </button>
            ) : null}
          </div>
        </div>
      ) : null}
    </div>
  )
}

/** 24-hour time picker. value/onChange use HH:MM ('' = empty). */
export function TimePicker({
  value,
  onChange,
  placeholder,
  minuteStep = 5,
  clearable = true,
  ariaLabel,
}: {
  value: string
  onChange: (value: string) => void
  placeholder?: string
  minuteStep?: number
  clearable?: boolean
  ariaLabel?: string
}) {
  const t = useT()
  const [open, setOpen] = useState(false)
  const close = useCallback(() => setOpen(false), [])
  const ref = useDismiss(open, close)
  const match = /^(\d{2}):(\d{2})$/.exec(value)
  const hour = match ? Number(match[1]) : null
  const minute = match ? Number(match[2]) : null
  const minutes = Array.from({ length: Math.ceil(60 / minuteStep) }, (_, index) => index * minuteStep)
  const align = useAlign(open, ref, 130, 260)
  const set = (h: number, m: number) => onChange(`${pad(h)}:${pad(m)}`)

  return (
    <div ref={ref} className="ui-select ui-time">
      <button
        type="button"
        className={`ui-select-trigger ${open ? 'open' : ''}`}
        aria-label={ariaLabel}
        aria-haspopup="dialog"
        aria-expanded={open}
        onClick={() => setOpen(!open)}
      >
        <Icon name="clock" className="ui-date-icon" />
        <span className={`ui-select-value mono ${value ? '' : 'placeholder'}`}>{value || placeholder || t('inputs.pickTime')}</span>
        {clearable && value ? (
          <span
            role="button"
            tabIndex={-1}
            className="ui-clear"
            aria-label="Clear time"
            onClick={(event) => {
              event.stopPropagation()
              onChange('')
            }}
          >
            <Icon name="x" />
          </span>
        ) : null}
      </button>
      {open ? (
        <div className={`ui-popover ui-timegrid${align}`} role="dialog" aria-label="Choose a time" onClick={stopLabel}>
          <div className="ui-time-col">
            <div className="ui-time-head">{t('inputs.hour')}</div>
            {Array.from({ length: 24 }, (_, h) => (
              <button
                key={h}
                type="button"
                className={`ui-time-cell ${h === hour ? 'selected' : ''}`}
                onClick={() => set(h, minute ?? 0)}
              >
                {pad(h)}
              </button>
            ))}
          </div>
          <div className="ui-time-col">
            <div className="ui-time-head">{t('inputs.minute')}</div>
            {minutes.map((m) => (
              <button
                key={m}
                type="button"
                className={`ui-time-cell ${m === minute ? 'selected' : ''}`}
                onClick={() => {
                  set(hour ?? 0, m)
                  close()
                }}
              >
                {pad(m)}
              </button>
            ))}
          </div>
        </div>
      ) : null}
    </div>
  )
}

/**
 * An amount in a currency: the currency's symbol on its side, its decimals, typed as text (the
 * currency's decimal mark or "."). The value stays a string; convert it with toMinor from
 * ../money when submitting, so a typo is an error, never a rounded number. Numbers are left to
 * right in every language.
 */
export function MoneyInput({
  value,
  onChange,
  currency,
  id,
  disabled,
  required,
  placeholder,
  ariaLabel,
}: {
  value: string
  onChange: (value: string) => void
  currency: Currency
  id?: string
  disabled?: boolean
  required?: boolean
  placeholder?: string
  ariaLabel?: string
}) {
  const symbol = <span className="money-input-symbol" aria-hidden="true">{currency.symbol}</span>
  return (
    <span className={`money-input ${disabled ? 'is-disabled' : ''}`} dir="ltr">
      {currency.symbol_first ? symbol : null}
      <input
        id={id}
        className="input"
        type="text"
        inputMode={currency.precision > 0 ? 'decimal' : 'numeric'}
        autoComplete="off"
        disabled={disabled}
        required={required}
        placeholder={placeholder ?? (currency.precision > 0 ? `0${currency.decimal_mark}${'0'.repeat(currency.precision)}` : '0')}
        aria-label={ariaLabel ? `${ariaLabel} (${currency.code})` : currency.code}
        value={value}
        onChange={(event) => onChange(event.target.value)}
      />
      {currency.symbol_first ? null : symbol}
    </span>
  )
}
