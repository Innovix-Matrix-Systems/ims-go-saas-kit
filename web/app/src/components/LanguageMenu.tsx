import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useCallback, useRef, useState, type KeyboardEvent } from 'react'
import { api } from '../api/client'
import { isSignedIn, ownProfile, sessionQuery } from '../auth/session'
import { getLanguages, setLocale, useLocale, useT } from '../i18n'
import { useDismiss } from './inputs'
import { Icon } from './ui'

/**
 * Saves a language choice to the signed-in account, so e-mails and other
 * devices follow it. Never while acting as someone else: their choice is theirs.
 */
export function useChooseLanguage(): (code: string) => Promise<void> {
  // Signed-out screens use this menu too: no session to read there.
  const session = useQuery({ ...sessionQuery, enabled: isSignedIn() }).data
  const queryClient = useQueryClient()
  return useCallback(
    async (code: string) => {
      await setLocale(code)
      if (!session?.user || session.acting) return
      try {
        const { path, method } = ownProfile(session).update
        await api(path, { method, body: { locale: code } })
        await queryClient.invalidateQueries({ queryKey: ['session'] })
      } catch {
        /* the screen already changed; the account keeps its old choice */
      }
    },
    [session, queryClient],
  )
}

/** The top bar's language picker: the current code, opening to every language in its own name. */
export function LanguageMenu() {
  const t = useT()
  const locale = useLocale()
  const choose = useChooseLanguage()
  const [open, setOpen] = useState(false)
  const close = useCallback(() => setOpen(false), [])
  const ref = useDismiss(open, close)
  const list = useRef<HTMLDivElement>(null)
  const languages = getLanguages()
  if (languages.length < 2) return null

  const focusItem = (index: number) => {
    const items = list.current?.querySelectorAll<HTMLButtonElement>('[role="menuitemradio"]')
    if (!items?.length) return
    items[(index + items.length) % items.length]?.focus()
  }
  const openMenu = () => {
    setOpen(true)
    requestAnimationFrame(() => focusItem(Math.max(0, languages.findIndex((item) => item.code === locale))))
  }
  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    const items = Array.from(list.current?.querySelectorAll<HTMLButtonElement>('[role="menuitemradio"]') ?? [])
    const index = items.indexOf(document.activeElement as HTMLButtonElement)
    if (event.key === 'ArrowDown') focusItem(index + 1)
    else if (event.key === 'ArrowUp') focusItem(index - 1)
    else if (event.key === 'Home') focusItem(0)
    else if (event.key === 'End') focusItem(items.length - 1)
    else return
    event.preventDefault()
  }
  const pick = (code: string) => {
    close()
    void choose(code)
  }

  return (
    <div className="language" ref={ref}>
      <button
        type="button"
        className="icon-btn language-trigger"
        title={t('common.language')}
        aria-label={`${t('common.language')}: ${languages.find((item) => item.code === locale)?.label ?? locale}`}
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => (open ? close() : openMenu())}
        onKeyDown={(event) => {
          if (event.key === 'ArrowDown' && !open) {
            event.preventDefault()
            openMenu()
          }
        }}
      >
        <Icon name="globe" />
        <span className="language-code">{locale.toUpperCase()}</span>
      </button>
      {open ? (
        <div className="ui-popover align-right language-menu" role="menu" aria-label={t('common.language')} ref={list} onKeyDown={onKeyDown}>
          {languages.map((item) => (
            <button
              key={item.code}
              type="button"
              role="menuitemradio"
              aria-checked={item.code === locale}
              className="menu-item"
              lang={item.code}
              dir={item.dir}
              onClick={() => pick(item.code)}
            >
              <span className="language-name">{item.label}</span>
              {item.reviewed ? null : <span className="language-beta">{t('language.beta')}</span>}
              {item.code === locale ? <Icon name="check" className="language-check" /> : null}
            </button>
          ))}
        </div>
      ) : null}
    </div>
  )
}
