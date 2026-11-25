import { createFileRoute, Link } from '@tanstack/react-router'
import { keepPreviousData, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef, useState, type MouseEvent } from 'react'
import { api, apiBlob, ApiError, saveBlob, type Schemas } from '../api/client'
import { can, useSession } from '../auth/session'
import { formatBytes } from '../format'
import { Pagination } from '../components/Pagination'
import { useToast } from '../components/Toasts'
import { Badge, Button, Card, ConfirmDialog, CopyField, Drawer, EmptyState, Icon, PageHeader, Spinner, type BadgeTone, type IconName } from '../components/ui'
import { intlLocale, useLocale, useT, type MessageKey } from '../i18n'
import { TableSkeleton } from '../components/Skeleton'

export const Route = createFileRoute('/_app/org/files')({
  component: FilesPage,
})

type StoredFile = Schemas['File']
interface Page {
  data: StoredFile[]
  meta: Schemas['PaginationMeta']
}
type FileType = '' | StoredFile['type']
type View = 'grid' | 'list'

const VIEW_KEY = 'ims.app.files.view'

const typeLabels: Record<StoredFile['type'], MessageKey> = {
  image: 'files.image',
  video: 'files.video',
  document: 'files.document',
  other: 'files.other',
}

const typeTones: Record<StoredFile['type'], BadgeTone> = {
  image: 'info',
  video: 'violet',
  document: 'neutral',
  other: 'neutral',
}

/**
 * The organization's files, uploads and generated exports alike, as a grid
 * of previews or a list. Opening one shows its preview and details.
 */
function FilesPage() {
  const t = useT()
  const session = useSession()
  const client = useQueryClient()
  const { push } = useToast()
  const input = useRef<HTMLInputElement>(null)
  const [view, setView] = useState<View>(() => {
    try {
      return localStorage.getItem(VIEW_KEY) === 'list' ? 'list' : 'grid'
    } catch {
      return 'grid'
    }
  })
  const [type, setType] = useState<FileType>('')
  const [page, setPage] = useState(1)
  const [perPage, setPerPage] = useState(20)
  const [uploading, setUploading] = useState(false)
  const [selected, setSelected] = useState<StoredFile | null>(null)
  const [deleting, setDeleting] = useState<StoredFile | null>(null)
  const [busy, setBusy] = useState(false)
  const when = useFileDate()

  useEffect(() => {
    try {
      localStorage.setItem(VIEW_KEY, view)
    } catch {
      // Storage is off; the view is just not remembered.
    }
  }, [view])

  const files = useQuery({
    queryKey: ['files', type, page, perPage],
    queryFn: () => api<Page>('/files', { envelope: true, query: { type: type || undefined, page, per_page: perPage } }),
    placeholderData: keepPreviousData,
  })
  const rows = files.data?.data ?? []
  const canDelete = can(session, 'file.delete')

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
      if (selected?.id === deleting.id) setSelected(null)
      push(t('members.done'), 'success')
    } catch (cause) {
      push(cause instanceof Error ? cause.message : String(cause), 'error')
    } finally {
      setBusy(false)
      setDeleting(null)
    }
  }

  // Card and row buttons act on the file without opening it.
  const act = (event: MouseEvent, run: () => void) => {
    event.stopPropagation()
    run()
  }

  const fileActions = (file: StoredFile) => (
    <>
      <Button size="sm" variant="ghost" icon="download" title={t('files.download')} aria-label={t('files.download')} onClick={(event) => act(event, () => void download(file))} />
      {canDelete && !file.generated ? (
        <Button size="sm" variant="ghost" icon="trash" title={t('files.delete')} aria-label={t('files.delete')} onClick={(event) => act(event, () => setDeleting(file))} />
      ) : null}
    </>
  )

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
      <div className="files-toolbar">
        <div className="segmented" role="group" aria-label={t('files.colType')}>
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
        <span className="spacer" />
        <div className="segmented" role="group" aria-label={t('files.layout')}>
          <button type="button" className={view === 'grid' ? 'active' : ''} aria-pressed={view === 'grid'} onClick={() => setView('grid')}>
            <Icon name="grid" />
            {t('files.grid')}
          </button>
          <button type="button" className={view === 'list' ? 'active' : ''} aria-pressed={view === 'list'} onClick={() => setView('list')}>
            <Icon name="list" />
            {t('files.list')}
          </button>
        </div>
      </div>
      <Card flush>
        {files.isLoading ? (
          <TableSkeleton columns={5} avatar={false} />
        ) : rows.length === 0 ? (
          <EmptyState icon="files" title={t('files.empty')} />
        ) : view === 'grid' ? (
          <div className="files-body">
            <div className="file-grid">
              {rows.map((file) => (
                <div
                  key={file.id}
                  className="file-card"
                  role="button"
                  tabIndex={0}
                  aria-label={file.name}
                  onClick={() => setSelected(file)}
                  onKeyDown={(event) => {
                    if (event.target !== event.currentTarget) return
                    if (event.key === 'Enter' || event.key === ' ') {
                      event.preventDefault()
                      setSelected(file)
                    }
                  }}
                >
                  <div className="file-card-preview">
                    <FileThumb file={file} />
                    <span className="file-card-badges">
                      <Badge tone={typeTones[file.type]}>{t(typeLabels[file.type])}</Badge>
                      <UsageBadge file={file} />
                    </span>
                    <div className="file-card-actions">{fileActions(file)}</div>
                  </div>
                  <div className="file-card-body">
                    <div className="file-card-name" title={file.name}>
                      {file.name}
                    </div>
                    <div className="file-card-meta">
                      <span>{formatBytes(file.size)}</span>

                    </div>
                    <div className="file-card-sub">{when(file.created_at)}</div>
                  </div>
                </div>
              ))}
            </div>
          </div>
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
                {rows.map((file) => (
                  <tr key={file.id} className="clickable" onClick={() => setSelected(file)}>
                    <td>
                      <div className="file-cell">
                        <FileThumb file={file} variant="row" />
                        <span className="member-meta">
                          <span className="member-name">
                            <span className="file-name" title={file.file_name}>
                              {file.name}
                            </span>
                            <UsageBadge file={file} />
                          </span>
                          <span className="small muted file-mime" title={file.mime_type}>
                            {file.mime_type}
                          </span>
                        </span>
                      </div>
                    </td>
                    <td>
                      <Badge tone={typeTones[file.type]}>{t(typeLabels[file.type])}</Badge>
                    </td>
                    <td className="small muted nowrap">{formatBytes(file.size)}</td>
                    <td className="small muted nowrap">{when(file.created_at)}</td>
                    <td className="end">
                      <div className="flex justify-end">{fileActions(file)}</div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        {files.data ? (
          <Pagination
            page={page}
            perPage={perPage}
            total={files.data.meta.total}
            onPage={setPage}
            onPerPage={(size) => {
              setPerPage(size)
              setPage(1)
            }}
          />
        ) : null}
      </Card>
      <Drawer
        open={selected != null}
        title={selected?.name ?? ''}
        subtitle={selected ? `${selected.mime_type} · ${formatBytes(selected.size)}` : undefined}
        size="md"
        onClose={() => setSelected(null)}
        actions={
          selected ? (
            <>
              <Button size="sm" icon="download" onClick={() => void download(selected)}>
                {t('files.download')}
              </Button>
              {canDelete && !selected.generated ? (
                <Button size="sm" variant="danger" icon="trash" onClick={() => setDeleting(selected)}>
                  {t('files.delete')}
                </Button>
              ) : null}
            </>
          ) : null
        }
      >
        {selected ? (
          <div className="stack">
            <FilePreview file={selected} />
            {selected.generated ? (
              <div className="banner info">
                <span className="file-generated-note">
                  <Icon name="queue" />
                  {t('files.generatedNote')}
                </span>
                <Link to="/jobs" className="link-btn">
                  {t('jobs.manage')}
                </Link>
              </div>
            ) : null}
            <dl className="kv">
              <dt>{t('files.colName')}</dt>
              <dd>{selected.name}</dd>
              <dt>{t('files.storedAs')}</dt>
              <dd>{selected.file_name}</dd>
              <dt>{t('files.colType')}</dt>
              <dd>
                <Badge tone={typeTones[selected.type]}>{t(typeLabels[selected.type])}</Badge>
              </dd>
              <dt>{t('files.mime')}</dt>
              <dd>{selected.mime_type}</dd>
              <dt>{t('files.colSize')}</dt>
              <dd>{formatBytes(selected.size)}</dd>
              <dt>{t('files.colUploaded')}</dt>
              <dd>{when(selected.created_at)}</dd>
              <dt>{t('files.usage')}</dt>
              <dd>{usageText(t, selected)}</dd>
              {isPublic(selected) ? (
                <>
                  <dt>{t('files.publicLink')}</dt>
                  <dd>
                    <CopyField value={selected.url} />
                  </dd>
                </>
              ) : null}
            </dl>
          </div>
        ) : null}
      </Drawer>
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

/** A public file's url is its own address; a private one points at the download route. */
const isPublic = (file: StoredFile) => !!file.url && !file.url.includes('/api/v1/')

function usageText(t: ReturnType<typeof useT>, file: StoredFile): string {
  if (file.generated) return t('jobs.generated')
  if (file.collection === 'logo') return t('files.logo')
  if (file.collection === 'profile') return t('files.photo')
  return file.attached ? t('files.attached') : t('files.library')
}

/** What a file is for, when it is more than a library file. */
function UsageBadge({ file }: { file: StoredFile }) {
  const t = useT()
  if (file.generated) return <Badge tone="accent">{t('jobs.generated')}</Badge>
  if (file.collection === 'logo') return <Badge tone="violet">{t('files.logo')}</Badge>
  if (file.collection === 'profile') return <Badge tone="info">{t('files.photo')}</Badge>
  return file.attached ? <Badge tone="neutral">{t('files.attached')}</Badge> : null
}

function useFileDate() {
  const locale = intlLocale(useLocale())
  return (value: string) => new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
}

type PreviewKind = 'image' | 'video' | 'audio' | 'document'

/** What the browser can show inline; everything else is download-only. */
function previewKind(file: StoredFile): PreviewKind | null {
  const mime = file.mime_type.toLowerCase()
  if (mime.startsWith('image/') && mime !== 'image/svg+xml') return 'image'
  if (mime.startsWith('video/')) return 'video'
  if (mime.startsWith('audio/')) return 'audio'
  if (mime === 'application/pdf' || mime === 'text/plain') return 'document'
  return null
}

function extension(name: string): string {
  const dot = name.lastIndexOf('.')
  return dot < 0 || dot === name.length - 1 ? 'FILE' : name.slice(dot + 1).toUpperCase()
}

const fileIcons: Record<StoredFile['type'], IconName> = {
  image: 'eye',
  video: 'play',
  document: 'logs',
  other: 'files',
}

/** The file's content, fetched once through the authorized download route. */
function useFileBlob(file: StoredFile, enabled: boolean) {
  return useQuery({
    queryKey: ['files', 'blob', file.id],
    queryFn: ({ signal }) => apiBlob(`/files/${file.id}/download`, signal),
    enabled,
    staleTime: Number.POSITIVE_INFINITY,
    gcTime: 10 * 60_000,
    retry: false,
  })
}

function useBlobURL(blob: Blob | undefined): string | undefined {
  const [url, setUrl] = useState<string>()
  useEffect(() => {
    if (!blob) {
      setUrl(undefined)
      return
    }
    const next = URL.createObjectURL(blob)
    setUrl(next)
    return () => URL.revokeObjectURL(next)
  }, [blob])
  return url
}

function Fallback({ file, large }: { file: StoredFile; large?: boolean }) {
  return (
    <div className={`file-thumb-fallback ${large ? 'large' : ''}`}>
      <Icon name={fileIcons[file.type]} />
      <span>{extension(file.file_name || file.name)}</span>
    </div>
  )
}

/** A card or row thumbnail: images show themselves, the rest an icon. */
function FileThumb({ file, variant = 'card' }: { file: StoredFile; variant?: 'card' | 'row' }) {
  const image = previewKind(file) === 'image'
  const blob = useFileBlob(file, image)
  const url = useBlobURL(image ? blob.data : undefined)
  return <div className={`file-thumb ${variant}`}>{url ? <img src={url} alt="" loading="lazy" /> : <Fallback file={file} />}</div>
}

function FilePreview({ file }: { file: StoredFile }) {
  const t = useT()
  const kind = previewKind(file)
  const blob = useFileBlob(file, kind !== null)
  const url = useBlobURL(blob.data)
  if (!kind) {
    return (
      <div className="file-preview empty">
        <Fallback file={file} large />
        <div className="small muted">{t('files.noPreview')}</div>
      </div>
    )
  }
  if (blob.isLoading) {
    return (
      <div className="file-preview">
        <Spinner large />
      </div>
    )
  }
  if (blob.isError || !url) {
    return (
      <div className="file-preview empty">
        <Fallback file={file} large />
        <div className="small muted">{t('files.previewFailed')}</div>
      </div>
    )
  }
  return (
    <div className="file-preview">
      {kind === 'image' ? <img src={url} alt={file.name} /> : null}
      {kind === 'video' ? <video src={url} controls /> : null}
      {kind === 'audio' ? <audio src={url} controls /> : null}
      {kind === 'document' ? <iframe src={url} title={file.name}/> : null}
    </div>
  )
}
