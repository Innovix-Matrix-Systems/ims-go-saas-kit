import { useQueryClient } from '@tanstack/react-query'
import { useRef, useState } from 'react'
import { api, ApiError, type Schemas } from '../api/client'
import { useT } from '../i18n'
import { useToast } from './Toasts'
import { Button, CopyField, Icon, Spinner } from './ui'

/**
 * Uploading the organization's logo. The upload becomes the logo at once
 * (the API sets the branding.logo_url setting), so everything that shows it
 * is refreshed: the organization, settings and files. `input` is the hidden
 * file input to render; `choose` opens the picker.
 */
export function useLogoUpload() {
  const t = useT()
  const client = useQueryClient()
  const { push } = useToast()
  const ref = useRef<HTMLInputElement>(null)
  const [busy, setBusy] = useState(false)

  const upload = async (file: File | undefined) => {
    if (!file) return
    setBusy(true)
    try {
      const form = new FormData()
      form.append('logo', file)
      await api<Schemas['File']>('/organization/logo', { method: 'POST', body: form })
      await Promise.all([
        client.invalidateQueries({ queryKey: ['organization'] }),
        client.invalidateQueries({ queryKey: ['settings'] }),
        client.invalidateQueries({ queryKey: ['files'] }),
      ])
      push(t('logo.uploaded'), 'success')
    } catch (cause) {
      push(cause instanceof ApiError ? (cause.field('logo') ?? cause.message) : String(cause), 'error')
    } finally {
      setBusy(false)
      if (ref.current) ref.current.value = ''
    }
  }

  return {
    busy,
    choose: () => ref.current?.click(),
    input: <input ref={ref} type="file" accept="image/png,image/jpeg,image/webp,image/gif" hidden onChange={(e) => void upload(e.target.files?.[0])} />,
  }
}

/** The logo's preview, its link to copy, and the button to upload a new one; under the Logo setting. */
export function LogoUploader({ current }: { current: string }) {
  const t = useT()
  const logo = useLogoUpload()
  return (
    <div className="logo-uploader">
      <div className="logo-preview">{current ? <img src={current} alt={t('logo.preview')} /> : <Icon name="files" />}</div>
      <div className="logo-body">
        {current ? (
          <>
            <span className="small muted">{t('logo.link')}</span>
            <CopyField value={current} />
          </>
        ) : (
          <span className="small muted">{t('logo.hint')}</span>
        )}
        <div>
          <Button size="sm" icon={current ? 'edit' : 'plus'} disabled={logo.busy} onClick={logo.choose}>
            {logo.busy ? <Spinner /> : null} {current ? t('logo.replace') : t('logo.upload')}
          </Button>
        </div>
      </div>
      {logo.input}
    </div>
  )
}
