import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import { useDeferredValue, useEffect, useMemo, useState, type MouseEvent } from 'react'
import type { ColumnDef } from '@tanstack/react-table'
import { api, apiBlob } from '../api/client'
import type { DevFile, DevFilePage, DevFileStats, DevTarget, DevTargetPage } from '../api/types'
import { DataTable } from '../components/DataTable'
import { DatePicker, Select } from '../components/inputs'
import { useToast } from '../components/Toasts'
import {
  Button,
  Card,
  CardHeader,
  Badge,
  ConfirmDialog,
  Drawer,
  EmptyState,
  ErrorBox,
  Icon,
  LoadingBlock,
  PageHeader,
  StatTile,
  type IconName,
  type BadgeTone,
} from '../components/ui'
import { formatBytes, formatDateTime, localDayEnd, localDayStart, relativeTime } from '../format'

export const Route = createFileRoute('/_app/files')({
  component: FilesPage,
})

const VIEW_KEY = 'ims.dev.files.view'

const typeTones: Record<string, BadgeTone> = {
  image: 'info',
  video: 'violet',
  document: 'neutral',
  other: 'neutral',
}

const sortOptions = [
  { value: 'created_at:desc', label: 'Newest first' },
  { value: 'created_at:asc', label: 'Oldest first' },
  { value: 'name:asc', label: 'Name A→Z' },
  { value: 'name:desc', label: 'Name Z→A' },
  { value: 'size:desc', label: 'Largest first' },
  { value: 'size:asc', label: 'Smallest first' },
]

interface ConfirmState {
  title: string
  description: string
  confirmLabel: string
  file: DevFile
}

function FilesPage() {
  const { push } = useToast()
  const queryClient = useQueryClient()
  const [view, setView] = useState<'grid' | 'list'>(() => {
    try {
      return localStorage.getItem(VIEW_KEY) === 'list' ? 'list' : 'grid'
    } catch {
      return 'grid'
    }
  })
  const [searchInput, setSearchInput] = useState('')
  const search = useDeferredValue(searchInput.trim())
  const [type, setType] = useState('')
  const [disk, setDisk] = useState('')
  const [attached, setAttached] = useState('')
  const [createdFrom, setCreatedFrom] = useState('')
  const [createdTo, setCreatedTo] = useState('')
  const [sort, setSort] = useState('created_at:desc')
  const [page, setPage] = useState(1)
  const [tenantQuery, setTenantQuery] = useState('')
  const deferredTenantQuery = useDeferredValue(tenantQuery.trim())
  const [selectedTenant, setSelectedTenant] = useState<DevTarget | null>(null)
  const [selected, setSelected] = useState<DevFile | null>(null)
  const [confirm, setConfirm] = useState<ConfirmState | null>(null)

  const [orderBy, orderDir] = sort.split(':')
  const tenantId = selectedTenant?.id ?? 0

  useEffect(() => {
    try {
      localStorage.setItem(VIEW_KEY, view)
    } catch {
      /* storage disabled */
    }
  }, [view])

  useEffect(() => {
    setPage(1)
  }, [search, type, disk, attached, tenantId, createdFrom, createdTo, sort])

  const tenants = useQuery({
    queryKey: ['files', 'tenants', deferredTenantQuery],
    queryFn: ({ signal }) =>
      api<DevTargetPage>('/dev/api/targets', {
        query: { type: 'tenant', search: deferredTenantQuery || undefined, per_page: 100 },
        signal,
        unwrap: false,
      }),
    staleTime: 60_000,
  })

  const tenantOptions = useMemo(() => {
    const items = tenants.data?.data ?? []
    if (selectedTenant && !items.some((item) => item.id === selectedTenant.id)) {
      return [selectedTenant, ...items]
    }
    return items
  }, [tenants.data, selectedTenant])

  const files = useQuery({
    queryKey: [
      'files',
      'list',
      { search, type, disk, attached, tenantId, createdFrom, createdTo, orderBy, orderDir, page },
    ],
    queryFn: ({ signal }) =>
      api<DevFilePage>('/dev/api/files', {
        query: {
          search: search || undefined,
          type: type || undefined,
          disk: disk || undefined,
          attached: attached === '' ? undefined : attached,
          tenant_id: tenantId || undefined,
          created_from: localDayStart(createdFrom),
          created_to: localDayEnd(createdTo),
          order_by: orderBy,
          order_direction: orderDir,
          page,
          per_page: 24,
        },
        signal,
        unwrap: false,
      }),
    placeholderData: keepPreviousData,
  })

  const stats = useQuery({
    queryKey: ['files', 'stats', tenantId],
    queryFn: ({ signal }) =>
      api<DevFileStats>('/dev/api/files/stats', {
        query: { tenant_id: tenantId || undefined },
        signal,
      }),
  })

  const rows = files.data?.data ?? []
  const meta = files.data?.meta
  const tenantLabel = selectedTenant?.name ?? 'All tenants'

  const invalidate = (fileID?: number) => {
    void queryClient.invalidateQueries({ queryKey: ['files', 'list'] })
    void queryClient.invalidateQueries({ queryKey: ['files', 'stats'] })
    void queryClient.invalidateQueries({ queryKey: ['actions'] })
    void queryClient.invalidateQueries({ queryKey: ['overview'] })
    if (fileID) {
      queryClient.removeQueries({ queryKey: ['files', 'blob', fileID] })
    }
  }

  const download = useMutation({
    mutationFn: async (file: DevFile) => {
      const blob = await apiBlob(`/dev/api/files/${file.id}/download`)
      const url = URL.createObjectURL(blob)
      const anchor = document.createElement('a')
      anchor.href = url
      anchor.download = file.file_name || file.name
      document.body.appendChild(anchor)
      anchor.click()
      anchor.remove()
      window.setTimeout(() => URL.revokeObjectURL(url), 1_000)
    },
    onError: (error) => push(error instanceof Error ? error.message : 'Unable to download the file', 'error'),
  })

  const remove = useMutation({
    mutationFn: (fileID: number) => api(`/dev/api/files/${fileID}`, { method: 'DELETE' }),
    onSuccess: (_result, fileID) => {
      push('File deleted', 'success')
      setConfirm(null)
      if (selected?.id === fileID) setSelected(null)
      invalidate(fileID)
    },
    onError: (error) => push(error instanceof Error ? error.message : 'Unable to delete the file', 'error'),
  })

  const resetFilters = () => {
    setSearchInput('')
    setType('')
    setDisk('')
    setAttached('')
    setCreatedFrom('')
    setCreatedTo('')
    setTenantQuery('')
    setSelectedTenant(null)
    setSort('created_at:desc')
    setPage(1)
  }

  const askDelete = (event: MouseEvent, file: DevFile) => {
    event.stopPropagation()
    setConfirm({
      title: `Delete ${file.name}?`,
      description:
        'The file row and its stored object are removed from every disk destination. This action is audited and cannot be undone.',
      confirmLabel: 'Delete file',
      file,
    })
  }

  const columns = useMemo<ColumnDef<DevFile, any>[]>(
    () => [
      {
        id: 'file',
        header: 'File',
        accessorFn: (row) => row.name,
        enableSorting: false,
        cell: ({ row }) => (
          <div className="file-cell">
            <FileThumb file={row.original} variant="row" />
            <div className="min-0">
              <div className="strong">{row.original.name}</div>
              <div className="small dim mono" title={row.original.file_name}>
                {row.original.file_name}
              </div>
            </div>
          </div>
        ),
      },
      {
        id: 'type',
        header: 'Type',
        accessorFn: (row) => row.type,
        enableSorting: false,
        cell: ({ row }) => (
          <Badge tone={typeTones[row.original.type] ?? 'neutral'}>{row.original.type}</Badge>
        ),
      },
      {
        id: 'size',
        header: 'Size',
        accessorFn: (row) => row.size,
        enableSorting: false,
        cell: ({ row }) => <span className="num small">{formatBytes(row.original.size)}</span>,
      },
      {
        id: 'tenant',
        header: 'Tenant / uploader',
        accessorFn: (row) => row.tenant_name ?? row.uploader_email ?? '',
        enableSorting: false,
        cell: ({ row }) => (
          <div>
            <div className="small">{row.original.tenant_name ?? 'Internal / global'}</div>
            <div className="small dim">{row.original.uploader_email ?? 'system'}</div>
          </div>
        ),
      },
      {
        id: 'attachment',
        header: 'Attachment',
        accessorFn: (row) => (row.attached ? 1 : 0),
        enableSorting: false,
        cell: ({ row }) =>
          row.original.attached ? (
            <span className="small">
              {row.original.owner_type}#{row.original.owner_id}
              {row.original.collection ? <span className="dim"> · {row.original.collection}</span> : null}
            </span>
          ) : (
            <span className="small dim">library</span>
          ),
      },
      {
        id: 'created',
        header: 'Uploaded',
        accessorFn: (row) => row.created_at,
        enableSorting: false,
        cell: ({ row }) => (
          <span className="small dim nowrap" title={formatDateTime(row.original.created_at)}>
            {relativeTime(row.original.created_at)}
          </span>
        ),
      },
      {
        id: 'actions',
        header: '',
        enableSorting: false,
        cell: ({ row }) => (
          <div className="flex justify-end g-1">
            <Button
              size="sm"
              variant="ghost"
              icon="download"
              title="Download"
              onClick={(event) => {
                event.stopPropagation()
                download.mutate(row.original)
              }}
            />
            <Button
              size="sm"
              variant="ghost"
              icon="trash"
              title="Delete"
              onClick={(event) => askDelete(event, row.original)}
            />
          </div>
        ),
      },
    ],
    [],
  )

  return (
    <>
      <PageHeader
        title="Files"
        description="File-manager browser over every upload: preview, inspect storage metadata, download and delete with an audit trail."
        actions={
          <>
            <div className="segmented" role="group" aria-label="Layout">
              <button
                type="button"
                className={view === 'grid' ? 'active' : ''}
                onClick={() => setView('grid')}
                title="Grid view"
              >
                <Icon name="grid" />
                Grid
              </button>
              <button
                type="button"
                className={view === 'list' ? 'active' : ''}
                onClick={() => setView('list')}
                title="List view"
              >
                <Icon name="list" />
                List
              </button>
            </div>
            <Button
              icon="refresh"
              onClick={() => {
                void queryClient.invalidateQueries({ queryKey: ['files'] })
              }}
            >
              Refresh
            </Button>
          </>
        }
      />

      <div className="grid cols-5">
        <StatTile label="Files" value={stats.data?.total_files ?? 0} hint={tenantLabel} />
        <StatTile label="Stored" value={formatBytes(stats.data?.total_size ?? 0)} hint="all disks" />
        <StatTile
          label="Images"
          value={byType(stats.data, 'image')}
          tone="accent"
          hint={formatBytes(byTypeSize(stats.data, 'image'))}
        />
        <StatTile
          label="Documents"
          value={byType(stats.data, 'document')}
          hint={formatBytes(byTypeSize(stats.data, 'document'))}
        />
        <StatTile
          label="Attachments"
          value={stats.data?.attached_files ?? 0}
          hint={`${stats.data?.unattached_files ?? 0} library files`}
        />
      </div>

      <Card className="mt-24">
        <CardHeader
          title="Filters"
          sub={meta ? `${meta.total} file(s) · page ${meta.current_page} of ${meta.last_page}` : undefined}
          actions={
            <Button size="sm" variant="ghost" icon="x" onClick={resetFilters}>
              Reset
            </Button>
          }
        />
        <div className="filters">
          <label className="field grow">
            <span className="field-label">Search</span>
            <input
              className="input"
              placeholder="Name or file name…"
              value={searchInput}
              onChange={(event) => setSearchInput(event.target.value)}
            />
          </label>
          <label className="field">
            <span className="field-label">Type</span>
            <Select
              value={type}
              onChange={setType}
              options={[
                { value: '', label: 'All types' },
                { value: 'image', label: 'Image' },
                { value: 'video', label: 'Video' },
                { value: 'document', label: 'Document' },
                { value: 'other', label: 'Other' },
              ]}
            />
          </label>
          <label className="field">
            <span className="field-label">Disk</span>
            <Select
              value={disk}
              onChange={setDisk}
              options={[
                { value: '', label: 'All disks' },
                ...(stats.data?.by_disk ?? []).map((item) => ({
                  value: item.disk,
                  label: item.disk,
                  hint: `${item.files}`,
                })),
              ]}
            />
          </label>
          <label className="field">
            <span className="field-label">Attachment</span>
            <Select
              value={attached}
              onChange={setAttached}
              options={[
                { value: '', label: 'All files' },
                { value: 'true', label: 'Attached' },
                { value: 'false', label: 'Library' },
              ]}
            />
          </label>
          <label className="field">
            <span className="field-label">Tenant</span>
            <Select
              value={selectedTenant ? String(selectedTenant.id) : ''}
              onChange={(value) => {
                const id = Number.parseInt(value, 10)
                setSelectedTenant(tenantOptions.find((item) => item.id === id) ?? null)
              }}
              options={[
                { value: '', label: 'All tenants' },
                ...tenantOptions.map((tenant) => ({
                  value: String(tenant.id),
                  label: tenant.name,
                  text: `${tenant.name} ${tenant.code ?? ''}`,
                  hint: tenant.code ?? undefined,
                })),
              ]}
            />
          </label>
          {tenants.data && tenants.data.meta.total > tenantOptions.length ? (
            <label className="field">
              <span className="field-label">Find tenant</span>
              <input
                className="input w-sm"
                placeholder="Search tenants…"
                value={tenantQuery}
                onChange={(event) => setTenantQuery(event.target.value)}
              />
            </label>
          ) : null}
          <label className="field">
            <span className="field-label">From</span>
            <DatePicker value={createdFrom} onChange={setCreatedFrom} max={createdTo || undefined} placeholder="Any date" />
          </label>
          <label className="field">
            <span className="field-label">To</span>
            <DatePicker value={createdTo} onChange={setCreatedTo} min={createdFrom || undefined} placeholder="Any date" />
          </label>
          <label className="field">
            <span className="field-label">Sort</span>
            <Select value={sort} onChange={setSort} options={sortOptions} />
          </label>
        </div>

        {files.isLoading ? (
          <LoadingBlock label="Loading files…" />
        ) : files.isError ? (
          <ErrorBox message={files.error instanceof Error ? files.error.message : 'Unable to load files.'} />
        ) : rows.length === 0 ? (
          <EmptyState
            title="No files match these filters"
            description="Uploads from the API and profile avatars appear here. Try a different search, tenant or type."
            icon="files"
            action={
              <Button variant="ghost" icon="x" onClick={resetFilters}>
                Clear filters
              </Button>
            }
          />
        ) : view === 'grid' ? (
          <div className="file-grid">
            {rows.map((file) => (
              <div
                key={file.id}
                className="file-card"
                role="button"
                tabIndex={0}
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
                  <span className="file-card-type">
                    <Badge tone={typeTones[file.type] ?? 'neutral'}>{file.type}</Badge>
                  </span>
                  <div className="file-card-actions">
                    <Button
                      size="sm"
                      variant="ghost"
                      icon="download"
                      title="Download"
                      onClick={(event) => {
                        event.stopPropagation()
                        download.mutate(file)
                      }}
                    />
                    <Button
                      size="sm"
                      variant="ghost"
                      icon="trash"
                      title="Delete"
                      onClick={(event) => askDelete(event, file)}
                    />
                  </div>
                </div>
                <div className="file-card-body">
                  <div className="file-card-name" title={file.name}>
                    {file.name}
                  </div>
                  <div className="file-card-meta">
                    <span>{formatBytes(file.size)}</span>
                    <span className="dot-sep">·</span>
                    <span>{file.tenant_name ?? 'Internal'}</span>
                  </div>
                  <div className="file-card-sub">
                    {file.uploader_email ?? 'system'} · {relativeTime(file.created_at)}
                  </div>
                </div>
              </div>
            ))}
          </div>
        ) : (
          <DataTable data={rows} columns={columns} onRowClick={(row) => setSelected(row)} />
        )}

        <div className="flex mt-16">
          <Button
            icon="chevron"
            disabled={!meta || meta.current_page <= 1 || files.isFetching}
            onClick={() => setPage((value) => Math.max(1, value - 1))}
          >
            Previous
          </Button>
          <Button
            icon="chevron"
            disabled={!meta || meta.current_page >= meta.last_page || files.isFetching}
            onClick={() => setPage((value) => value + 1)}
          >
            Next
          </Button>
          <span className="small dim">
            {meta ? `page ${meta.current_page} of ${meta.last_page} · ${meta.total} files` : ''}
          </span>
          {files.isFetching ? (
            <span className="small dim flex">
              <span className="spinner" /> refreshing
            </span>
          ) : null}
        </div>
      </Card>

      <Drawer
        open={selected !== null}
        title={selected?.name ?? 'File'}
        subtitle={
          selected
            ? `${selected.mime_type} · ${formatBytes(selected.size)} · ${selected.disk} disk`
            : undefined
        }
        onClose={() => setSelected(null)}
        actions={
          selected ? (
            <>
              <Button
                size="sm"
                icon="download"
                onClick={() => download.mutate(selected)}
                disabled={download.isPending}
              >
                Download
              </Button>
              <Button
                size="sm"
                variant="danger"
                icon="trash"
                onClick={(event) => askDelete(event, selected)}
              >
                Delete
              </Button>
            </>
          ) : undefined
        }
      >
        {selected ? (
          <div className="stack">
            <FilePreview file={selected} />
            <dl className="kv">
              <dt>File</dt>
              <dd>{selected.name}</dd>
              <dt>Stored as</dt>
              <dd className="mono small">{selected.file_name}</dd>
              <dt>MIME type</dt>
              <dd>{selected.mime_type}</dd>
              <dt>Type</dt>
              <dd>
                <Badge tone={typeTones[selected.type] ?? 'neutral'}>{selected.type}</Badge>
              </dd>
              <dt>Size</dt>
              <dd>{formatBytes(selected.size)}</dd>
              <dt>Disk</dt>
              <dd className="mono small">{selected.disk}</dd>
              <dt>Path</dt>
              <dd className="mono small break">{selected.path}</dd>
              <dt>Attachment</dt>
              <dd>
                {selected.attached
                  ? `${selected.owner_type}#${selected.owner_id}${selected.collection ? ` · ${selected.collection}` : ''}`
                  : 'Library file'}
              </dd>
              {selected.owner_name || selected.owner_email ? (
                <>
                  <dt>Owner</dt>
                  <dd>
                    {selected.owner_name ?? '—'}
                    {selected.owner_email ? <span className="dim"> · {selected.owner_email}</span> : null}
                  </dd>
                </>
              ) : null}
              <dt>Uploader</dt>
              <dd>
                {selected.uploader_name ?? 'System'}
                {selected.uploader_email ? <span className="dim"> · {selected.uploader_email}</span> : null}
              </dd>
              <dt>Tenant</dt>
              <dd>{selected.tenant_name ?? 'Internal / global'}</dd>
              <dt>Uploaded</dt>
              <dd>{formatDateTime(selected.created_at)}</dd>
              <dt>Updated</dt>
              <dd>{formatDateTime(selected.updated_at)}</dd>
              <dt>UUID</dt>
              <dd className="mono small break">{selected.uuid}</dd>
              <dt>Public URL</dt>
              <dd className="break">
                {selected.url ? (
                  <a href={selected.url} target="_blank" rel="noreferrer">
                    {selected.url}
                  </a>
                ) : (
                  <span className="dim">Private disk — use Download</span>
                )}
              </dd>
            </dl>
          </div>
        ) : null}
      </Drawer>

      <ConfirmDialog
        open={confirm !== null}
        title={confirm?.title ?? 'Delete file'}
        description={confirm?.description}
        confirmLabel={confirm?.confirmLabel ?? 'Delete'}
        danger
        busy={remove.isPending}
        onCancel={() => setConfirm(null)}
        onConfirm={() => {
          if (confirm) remove.mutate(confirm.file.id)
        }}
      />
    </>
  )
}

function byType(stats: DevFileStats | undefined, type: string): number {
  return stats?.by_type.find((item) => item.type === type)?.files ?? 0
}

function byTypeSize(stats: DevFileStats | undefined, type: string): number {
  return stats?.by_type.find((item) => item.type === type)?.size ?? 0
}

function extension(name: string): string {
  const dot = name.lastIndexOf('.')
  if (dot < 0 || dot === name.length - 1) return 'FILE'
  return name.slice(dot + 1).toUpperCase()
}

function fileIcon(file: DevFile): IconName {
  switch (file.type) {
    case 'image':
      return 'eye'
    case 'video':
      return 'play'
    case 'document':
      return 'logs'
    default:
      return 'files'
  }
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

function FileThumb({ file, variant = 'card' }: { file: DevFile; variant?: 'card' | 'row' }) {
  const previewable = file.preview_kind === 'image'
  const blob = useQuery({
    queryKey: ['files', 'blob', file.id, 'inline'],
    queryFn: ({ signal }) => apiBlob(`/dev/api/files/${file.id}/download?inline=1`, signal),
    enabled: previewable,
    staleTime: Number.POSITIVE_INFINITY,
    gcTime: 10 * 60_000,
    retry: false,
  })
  // The drawer preview shares this cache entry, so a PDF opened there is
  // cached too: only ever render images as <img>.
  const url = useBlobURL(previewable ? blob.data : undefined)

  return (
    <div className={`file-thumb ${variant}`}>
      {url ? (
        <img src={url} alt="" loading="lazy" />
      ) : (
        <div className="file-thumb-fallback">
          <Icon name={fileIcon(file)} />
          <span>{extension(file.file_name)}</span>
        </div>
      )}
    </div>
  )
}

function FilePreview({ file }: { file: DevFile }) {
  const kind = file.preview_kind ?? null
  const blob = useQuery({
    queryKey: ['files', 'blob', file.id, 'inline'],
    queryFn: ({ signal }) => apiBlob(`/dev/api/files/${file.id}/download?inline=1`, signal),
    enabled: kind !== null,
    staleTime: Number.POSITIVE_INFINITY,
    gcTime: 10 * 60_000,
    retry: false,
  })
  const url = useBlobURL(blob.data)

  if (!kind) {
    return (
      <div className="file-preview empty">
        <div className="file-thumb-fallback large">
          <Icon name={fileIcon(file)} />
          <span>{extension(file.file_name)}</span>
        </div>
        <div className="small dim">This file type has no inline preview. Use Download to open it.</div>
      </div>
    )
  }
  if (blob.isLoading) {
    return (
      <div className="file-preview">
        <LoadingBlock label="Loading preview…" />
      </div>
    )
  }
  if (blob.isError || !url) {
    return (
      <div className="file-preview">
        <ErrorBox message={blob.error instanceof Error ? blob.error.message : 'Unable to load the preview.'} />
      </div>
    )
  }
  if (kind === 'image') {
    return (
      <div className="file-preview">
        <img src={url} alt={file.name} />
      </div>
    )
  }
  if (kind === 'video') {
    return (
      <div className="file-preview">
        <video src={url} controls />
      </div>
    )
  }
  if (kind === 'audio') {
    return (
      <div className="file-preview">
        <audio src={url} controls />
      </div>
    )
  }
  return (
    <div className="file-preview">
      <iframe src={url} title={file.name} />
    </div>
  )
}
