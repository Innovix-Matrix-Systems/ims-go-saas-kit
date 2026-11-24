import { useQueryClient } from '@tanstack/react-query'
import { useRef, useState } from 'react'
import { api, ApiError, type Schemas } from '../api/client'
import { useT } from '../i18n'
import { useToast } from './Toasts'
import { Button, CopyField, Icon, Spinner } from './ui'

/**
 * Uploading the organization's logo, beside the setting that uses it: the
 * logo is stored as a public image (listed in Files); its link can be copied
 * or set as the logo in one click.
 */
export function LogoUploader({ current, onUse }: { current: string; onUse: (url: string) => Promise<void> }) {
  const t = useT()
  const client = useQueryClient()
  const { push } = useToast()
  const input = useRef<HTMLInputElement>(null)
  const [busy, setBusy] = useState(false)
  const [uploaded, setUploaded] = useState<string | null>(null)
  const preview = uploaded ?? (current || null)

  const upload = async (file: File | undefined) => {
    if (!file) return
    setBusy(true)
    try {
      const form = new FormData()
      form.append('logo', file)
      const stored = await api<Schemas['File']>('/organization/logo', { method: 'POST', body: form })
      setUploaded(stored.url)
      await client.invalidateQueries({ queryKey: ['files'] })
      push(t('logo.uploaded'), 'success')
    } catch (cause) {
      push(cause instanceof ApiError ? (cause.field('logo') ?? cause.message) : String(cause), 'error')
    } finally {
      setBusy(false)
      if (input.current) input.current.value = ''
    }
  }

  return (
    <div className="logo-uploader">
      <div className="logo-preview" aria-hidden={!preview}>
        {preview ? <img src={preview} alt={t('logo.preview')} /> : <Icon name="files" />}
      </div>
      <div className="logo-body">
        {uploaded ? (
          <>
            <span className="small muted">{t('logo.link')}</span>
            <CopyField value={uploaded} />
            <div className="flex g-2 wrap">
              {uploaded !== current ? (
                <Button size="sm" variant="primary" icon="check" disabled={busy} onClick={() => void onUse(uploaded)}>
                  {t('logo.use')}
                </Button>
              ) : (
                <span className="small logo-in-use">
                  <Icon name="check" />
                  {t('logo.inUse')}
                </span>
              )}
              <Button size="sm" icon="edit" disabled={busy} onClick={() => input.current?.click()}>
                {t('logo.replace')}
              </Button>
            </div>
          </>
        ) : (
          <>
            <span className="small muted">{t('logo.hint')}</span>
            <div>
              <Button size="sm" icon="plus" disabled={busy} onClick={() => input.current?.click()}>
                {busy ? <Spinner /> : null} {t('logo.upload')}
              </Button>
            </div>
          </>
        )}
      </div>
      <input ref={input} type="file" accept="image/png,image/jpeg,image/webp,image/gif" hidden onChange={(e) => void upload(e.target.files?.[0])} />
    </div>
  )
}
