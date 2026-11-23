import { createFileRoute } from '@tanstack/react-router'
import { keepPreviousData, useQuery, useQueryClient } from '@tanstack/react-query'
import { useRef, useState } from 'react'
import { api, apiBlob, ApiError, saveBlob, type Schemas } from '../api/client'
import { can, useSession } from '../auth/session'
import { formatBytes } from '../format'
import { useToast } from '../components/Toasts'
import { Badge, Button, Card, ConfirmDialog, EmptyState, PageHeader, Spinner } from '../components/ui'
import { intlLocale, useLocale, useT, type MessageKey } from '../i18n'

export const Route = createFileRoute('/_app/org/files')({
  component: FilesPage,
})

type StoredFile = Schemas['File']
interface Page {
  data: StoredFile[]
  meta: Schemas['PaginationMeta']
}
type FileType = '' | StoredFile['type']

const typeLabels: Record<StoredFile['type'], MessageKey> = {
  image: 'files.image',
  video: 'files.video',
  document: 'files.document',
  other: 'files.other',
}

function FilesPage() {
  const t = useT()
  const locale = useLocale()
  const session = useSession()
  const client = useQueryClient()
  const { push } = useToast()
  const input = useRef<HTMLInputElement>(null)
  const [type, setType] = useState<FileType>('')
  const [page, setPage] = useState(1)
  const [uploading, setUploading] = useState(false)
  const [deleting, setDeleting] = useState<StoredFile | null>(null)
  const [busy, setBusy] = useState(false)

  const files = useQuery({
    queryKey: ['files', type, page],
    queryFn: () => api<Page>('/files', { envelope: true, query: { type: type || undefined, page, per_page: 20 } }),
    placeholderData: keepPreviousData,
  })

  const upload = async (list: FileList | null) => {
    if (!list || list.length === 0) return
    setUploading(true)
    try {
      for (const file of Array.from(list)) {
        const form = new FormData()
        form.append('file', file)
        await api('/files', { method: 'POST', body: form })
        push(t('files.uploaded', { name: file.name }), 'success')
      }
    } catch (cause) {
      // A refused file says why on the `file` field (type or size).
      push(cause instanceof ApiError ? (cause.field('file') ?? cause.message) : String(cause), 'error')
    } finally {
      await client.invalidateQueries({ queryKey: ['files'] })
      setUploading(false)
      if (input.current) input.current.value = ''
    }
  }

  const download = async (file: StoredFile) => {
    try {
      saveBlob(await apiBlob(`/files/${file.id}/download`), file.file_name || file.name)
    } catch {
      // apiBlob reports the error.
    }
  }

  const remove = async () => {
    if (!deleting) return
    setBusy(true)
    try {
      await api(`/files/${deleting.id}`, { method: 'DELETE' })
      await client.invalidateQueries({ queryKey: ['files'] })
      push(t('members.done'), 'success')
    } catch (cause) {
      push(cause instanceof Error ? cause.message : String(cause), 'error')
    } finally {
      setBusy(false)
      setDeleting(null)
    }
  }

  const when = (value: string) => new Intl.DateTimeFormat(intlLocale(locale), { dateStyle: 'medium' }).format(new Date(value))

  return (
    <>
      <PageHeader
        title={t('org.files')}
        description={t('files.subtitle')}
        actions={
          can(session, 'file.create') ? (
            <>
              <input ref={input} type="file" multiple hidden onChange={(e) => void upload(e.target.files)} />
              <Button variant="primary" icon="plus" disabled={uploading} onClick={() => input.current?.click()}>
                {uploading ? <Spinner /> : null} {t('files.upload')}
              </Button>
            </>
          ) : null
        }
      />
      <div className="toolbar">
        <div className="segmented">
          {(['', 'image', 'video', 'document', 'other'] as const).map((value) => (
            <button
              key={value}
              type="button"
              className={type === value ? 'active' : ''}
              onClick={() => {
                setType(value)
                setPage(1)
              }}
            >
              {value ? t(typeLabels[value]) : t('files.all')}
            </button>
          ))}
        </div>
      </div>
      <Card flush>
        {files.isLoading ? (
          <div className="p-4">
            <Spinner />
          </div>
        ) : (files.data?.data ?? []).length === 0 ? (
          <EmptyState icon="files" title={t('files.empty')} />
        ) : (
          <div className="table-wrap">
            <table className="table">
              <thead>
                <tr>
                  <th>{t('files.colName')}</th>
                  <th>{t('files.colType')}</th>
                  <th>{t('files.colSize')}</th>
                  <th>{t('files.colUploaded')}</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {files.data!.data.map((file) => (
                  <tr key={file.id}>
                    <td>
                      <span className="member-name">
                        <span className="file-name" title={file.file_name}>
                          {file.name}
                        </span>
                        {file.attached ? <Badge tone="neutral">{t('files.attached')}</Badge> : null}
                      </span>
                      <span className="small muted">{file.mime_type}</span>
                    </td>
                    <td className="small">{t(typeLabels[file.type])}</td>
                    <td className="small muted">{formatBytes(file.size)}</td>
                    <td className="small muted">{when(file.created_at)}</td>
                    <td className="right">
                      <div className="flex justify-end">
                        <Button size="sm" variant="ghost" icon="download" onClick={() => void download(file)}>
                          {t('files.download')}
                        </Button>
                        {can(session, 'file.delete') ? (
                          <Button size="sm" variant="ghost" icon="trash" onClick={() => setDeleting(file)}>
                            {t('files.delete')}
                          </Button>
                        ) : null}
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        {files.data && files.data.meta.last_page > 1 ? (
          <div className="pager">
            <Button size="sm" disabled={page <= 1} onClick={() => setPage((p) => p - 1)}>
              {t('members.previous')}
            </Button>
            <span className="small muted">{t('members.page', { page, pages: files.data.meta.last_page })}</span>
            <Button size="sm" disabled={page >= files.data.meta.last_page} onClick={() => setPage((p) => p + 1)}>
              {t('members.next')}
            </Button>
          </div>
        ) : null}
      </Card>
      <ConfirmDialog
        open={deleting != null}
        title={t('files.delete')}
        description={t('files.deleteConfirm', { name: deleting?.name ?? '' })}
        confirmLabel={t('files.delete')}
        danger
        busy={busy}
        onConfirm={() => void remove()}
        onCancel={() => setDeleting(null)}
      />
    </>
  )
}
