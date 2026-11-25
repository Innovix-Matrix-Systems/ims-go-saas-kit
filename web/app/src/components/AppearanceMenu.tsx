import { useCallback, useState } from 'react'
import { getLanguages, setLocale, useLocale, useT, type MessageKey } from '../i18n'
import { accents, densities, setAppearance, themes, useAppearance, type Density, type ThemePreference } from './appearance'
import { useDismiss } from './inputs'
import { Icon, type IconName } from './ui'

const themeIcons: Record<ThemePreference, IconName> = { system: 'monitor', light: 'sun', dark: 'moon' }
const themeLabels: Record<ThemePreference, MessageKey> = {
  system: 'appearance.system',
  light: 'appearance.light',
  dark: 'appearance.dark',
}
const densityLabels: Record<Density, MessageKey> = {
  comfortable: 'appearance.comfortable',
  compact: 'appearance.compact',
}
const capitalize = (value: string) => value.charAt(0).toUpperCase() + value.slice(1)

/** The top bar's theme, accent, density and language picker. */
export function AppearanceMenu() {
  const appearance = useAppearance()
  const locale = useLocale()
  const t = useT()
  const [open, setOpen] = useState(false)
  const close = useCallback(() => setOpen(false), [])
  const ref = useDismiss(open, close)

  return (
    <div className="appearance" ref={ref}>
      <button
        type="button"
        className="icon-btn"
        title={`${t('appearance.theme')}: ${t(themeLabels[appearance.theme])}`}
        aria-label={t('common.appearance')}
        aria-expanded={open}
        onClick={() => setOpen((value) => !value)}
      >
        <Icon name={themeIcons[appearance.theme]} />
      </button>
      {open ? (
        <div className="ui-popover align-right appearance-menu" role="dialog" aria-label={t('common.appearance')}>
          <div className="field-label">{t('appearance.theme')}</div>
          <div className="segmented">
            {themes.map((theme) => (
              <button
                key={theme}
                type="button"
                className={appearance.theme === theme ? 'active' : ''}
                aria-pressed={appearance.theme === theme}
                onClick={() => setAppearance({ theme })}
              >
                <Icon name={themeIcons[theme]} /> {t(themeLabels[theme])}
              </button>
            ))}
          </div>
          <div className="field-label">{t('appearance.accent')}</div>
          <div className="accent-swatches">
            {accents.map((accent) => (
              <button
                key={accent}
                type="button"
                data-accent={accent}
                className={`accent-swatch ${appearance.accent === accent ? 'active' : ''}`}
                title={capitalize(accent)}
                aria-label={`${capitalize(accent)} ${t('appearance.accent')}`}
                aria-pressed={appearance.accent === accent}
                onClick={() => setAppearance({ accent })}
              />
            ))}
          </div>
          <div className="field-label">{t('appearance.density')}</div>
          <div className="segmented">
            {densities.map((density) => (
              <button
                key={density}
                type="button"
                className={appearance.density === density ? 'active' : ''}
                aria-pressed={appearance.density === density}
                onClick={() => setAppearance({ density })}
              >
                {t(densityLabels[density])}
              </button>
            ))}
          </div>
          <div className="field-label">{t('common.language')}</div>
          <div className="segmented">
            {getLanguages().map((item) => (
              <button
                key={item.code}
                type="button"
                lang={item.code}
                className={locale === item.code ? 'active' : ''}
                aria-pressed={locale === item.code}
                onClick={() => setLocale(item.code)}
              >
                {item.label}
              </button>
            ))}
          </div>
        </div>
      ) : null}
    </div>
  )
}
