import { Command } from 'cmdk'
import { useNavigate } from '@tanstack/react-router'
import { useCallback } from 'react'
import { signOut, useSession } from '../auth/session'
import { getLanguages, getLocale, setLocale, useT } from '../i18n'
import { nextTheme, setAppearance, useAppearance } from './appearance'
import { visiblePages } from './navigation'
import { Icon, Kbd, useEscape } from './ui'

/** Ctrl+K: jump to any page the user can open, or run a quick action. */
export function CommandPalette({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const t = useT()
  const navigate = useNavigate()
  const session = useSession()
  const appearance = useAppearance()
  const close = useCallback(() => onOpenChange(false), [onOpenChange])
  useEscape(open, close)

  if (!open) return null

  const select = (action: () => void) => {
    close()
    action()
  }

  return (
    <div className="command-overlay" role="presentation" onClick={close}>
      <div className="command-dialog" onClick={(event) => event.stopPropagation()}>
        <Command label={t('nav.searchOrJump')} shouldFilter loop>
          <div className="command-input-wrap">
            <Icon name="search" />
            <Command.Input autoFocus className="command-input" placeholder={t('palette.placeholder')} />
            <Kbd>Esc</Kbd>
          </div>
          <Command.List className="command-list">
            <Command.Empty className="command-empty">{t('palette.empty')}</Command.Empty>

            <Command.Group heading={t('palette.pages')}>
              {visiblePages(session).map((page) => (
                <Command.Item
                  key={page.to}
                  value={`${t(page.label)} ${page.to} ${page.keywords ?? ''}`}
                  className="command-item"
                  onSelect={() => select(() => void navigate({ to: page.to }))}
                >
                  <Icon name={page.icon} />
                  {t(page.label)}
                  {page.key ? (
                    <span className="command-hint keys">
                      <Kbd>g</Kbd>
                      <Kbd>{page.key}</Kbd>
                    </span>
                  ) : null}
                </Command.Item>
              ))}
            </Command.Group>

            <Command.Group heading={t('palette.actions')}>
              <Command.Item
                value="theme dark light"
                className="command-item"
                onSelect={() => select(() => setAppearance({ theme: nextTheme(appearance.theme) }))}
              >
                <Icon name={appearance.theme === 'dark' ? 'moon' : appearance.theme === 'light' ? 'sun' : 'monitor'} />
                {t('palette.toggleTheme')}
              </Command.Item>
              <Command.Item
                value={`language ${getLanguages().map((item) => `${item.english} ${item.label}`).join(' ')}`}
                className="command-item"
                onSelect={() => select(() => void setLocale(nextLanguage()))}
              >
                <Icon name="globe" />
                {t('palette.switchLanguage')}
              </Command.Item>
              <Command.Item
                value="sign out logout"
                className="command-item"
                onSelect={() =>
                  select(() => {
                    void signOut().then(() => navigate({ to: '/login' }))
                  })
                }
              >
                <Icon name="logout" />
                {t('common.signOut')}
              </Command.Item>
            </Command.Group>
          </Command.List>
        </Command>
      </div>
    </div>
  )
}

/** The language after the current one, for the palette's switch entry. */
function nextLanguage(): string {
  const list = getLanguages()
  const index = list.findIndex((item) => item.code === getLocale())
  return list[(index + 1) % list.length].code
}
