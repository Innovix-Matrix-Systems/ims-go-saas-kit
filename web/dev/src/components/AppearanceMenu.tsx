import { useCallback, useState } from 'react'
import { accents, densities, setAppearance, themes, useAppearance, type ThemePreference } from './appearance'
import { useDismiss } from './inputs'
import { Icon, type IconName } from './ui'

const themeIcons: Record<ThemePreference, IconName> = { system: 'monitor', light: 'sun', dark: 'moon' }
const label = (value: string) => value.charAt(0).toUpperCase() + value.slice(1)

/** The top bar's theme, accent and density picker. */
export function AppearanceMenu() {
  const appearance = useAppearance()
  const [open, setOpen] = useState(false)
  const close = useCallback(() => setOpen(false), [])
  const ref = useDismiss(open, close)

  return (
    <div className="appearance" ref={ref}>
      <button
        type="button"
        className="icon-btn"
        title={`Theme: ${label(appearance.theme)}`}
        aria-label="Appearance"
        aria-expanded={open}
        onClick={() => setOpen((value) => !value)}
      >
        <Icon name={themeIcons[appearance.theme]} />
      </button>
      {open ? (
        <div className="ui-popover align-right appearance-menu" role="dialog" aria-label="Appearance">
          <div className="field-label">Theme</div>
          <div className="segmented">
            {themes.map((theme) => (
              <button
                key={theme}
                type="button"
                className={appearance.theme === theme ? 'active' : ''}
                aria-pressed={appearance.theme === theme}
                onClick={() => setAppearance({ theme })}
              >
                <Icon name={themeIcons[theme]} /> {label(theme)}
              </button>
            ))}
          </div>
          <div className="field-label">Accent</div>
          <div className="accent-swatches">
            {accents.map((accent) => (
              <button
                key={accent}
                type="button"
                data-accent={accent}
                className={`accent-swatch ${appearance.accent === accent ? 'active' : ''}`}
                title={label(accent)}
                aria-label={`${label(accent)} accent`}
                aria-pressed={appearance.accent === accent}
                onClick={() => setAppearance({ accent })}
              />
            ))}
          </div>
          <div className="field-label">Density</div>
          <div className="segmented">
            {densities.map((density) => (
              <button
                key={density}
                type="button"
                className={appearance.density === density ? 'active' : ''}
                aria-pressed={appearance.density === density}
                onClick={() => setAppearance({ density })}
              >
                {label(density)}
              </button>
            ))}
          </div>
        </div>
      ) : null}
    </div>
  )
}
