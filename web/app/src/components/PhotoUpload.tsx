import { useQueryClient } from '@tanstack/react-query'
import { useRef, useState } from 'react'
import { api, ApiError } from '../api/client'
import { useSession } from '../auth/session'
import { useT } from '../i18n'
import { useToast } from './Toasts'
import { Avatar, Button } from './ui'

/**
 * Choosing and uploading the signed-in person's photo. `input` is the hidden
 * file input to render; `choose` opens the picker.
 */
export function usePhotoUpload() {
  const t = useT()
  const session = useSession()
  const client = useQueryClient()
  const { push } = useToast()
  const ref = useRef<HTMLInputElement>(null)
  const [uploading, setUploading] = useState(false)

  const upload = async (file: File | undefined) => {
    if (!file || !session) return
    setUploading(true)
    try {
      const form = new FormData()
      form.append('id', String(session.user.id))
      form.append('avatar', file)
      await api('/user/profile/update-avatar', { method: 'POST', body: form })
      await client.invalidateQueries({ queryKey: ['session'] })
      push(t('profilePage.photoSaved'), 'success')
    } catch (cause) {
      push(cause instanceof ApiError ? (cause.field('avatar') ?? cause.message) : String(cause), 'error')
    } finally {
      setUploading(false)
      if (ref.current) ref.current.value = ''
    }
  }

  return {
    uploading,
    choose: () => ref.current?.click(),
    input: <input ref={ref} type="file" accept="image/png,image/jpeg,image/webp,image/gif" hidden onChange={(e) => void upload(e.target.files?.[0])} />,
  }
}

/** The photo row of account settings: the current photo and a button to change it. */
export function PhotoField() {
  const t = useT()
  const session = useSession()
  const photo = usePhotoUpload()
  if (!session) return null
  return (
    <div className="photo-field">
      <Avatar name={session.user.name} photo={session.user.photo} size="lg" />
      <div className="photo-field-text">
        <span className="strong">{t('profilePage.photo')}</span>
        <span className="small muted">{t('profilePage.photoHint')}</span>
      </div>
      {photo.input}
      <Button icon="edit" disabled={photo.uploading} onClick={photo.choose}>
        {session.user.photo ? t('profilePage.changePhoto') : t('profilePage.addPhoto')}
      </Button>
    </div>
  )
}
