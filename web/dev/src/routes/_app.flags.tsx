import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import { useDeferredValue, useEffect, useMemo, useState } from 'react'
import { api } from '../api/client'
import type { DevFlag, DevTarget, DevTargetStats, DevTenant, Page } from '../api/types'
import { sourceTone } from '../format'
import { useToast } from '../components/Toasts'
import {
  Button,
  Card,
  CardHeader,
  Badge,
  Drawer,
  EmptyState,
  ErrorBox,
  Icon,
  LoadingBlock,
  PageHeader,
  StatTile,
} from '../components/ui'

export const Route = createFileRoute('/_app/flags')({
  validateSearch: (search: Record<string, unknown>): { tenant?: number } => ({
    tenant: typeof search.tenant === 'number' ? search.tenant : undefined,
  }),
  component: FlagsPage,
})

type TargetKind = 'global' | 'tenant' | 'user'

/** Loads the tenant named in ?tenant= (deep link from the Tenants panel). */
function useLinkedTenant(id: number | undefined) {
  return useQuery({
    queryKey: ['tenants', 'detail', id],
    queryFn: ({ signal }) => api<DevTenant>(`/dev/api/tenants/${id}`, { signal }),
    enabled: id !== undefined,
    select: (tenant): DevTarget => ({
      id: tenant.id,
      type: 'tenant',
      name: tenant.name,
      code: tenant.code,
      overrides: tenant.overrides,
    }),
  })
}

interface SelectedTarget {
  type: TargetKind
  id?: number
  name: string
  detail?: string
}

const globalTarget: SelectedTarget = {
  type: 'global',
  name: 'Global',
  detail: 'the fallback for every tenant and user',
}

const pageSize = 20

const targetDetail = (target: DevTarget): string =>
  target.type === 'tenant'
    ? [target.code, `#${target.id}`].filter(Boolean).join(' · ')
    : [target.email, target.tenant_name ?? 'internal user'].filter(Boolean).join(' · ')

const targetFromOption = (target: DevTarget): SelectedTarget => ({
  type: target.type,
  id: target.id,
  name: target.name,
  detail: targetDetail(target),
})

const kindLabel = (kind: TargetKind): string =>
  kind === 'global' ? 'global' : kind === 'tenant' ? 'tenant' : 'user'

function FlagRow({
  flag,
  targetName,
  busy,
  resetBusy,
  onToggle,
  onReset,
  onOpen,
}: {
  flag: DevFlag
  targetName: string
  busy: boolean
  resetBusy: boolean
  onToggle: (flag: DevFlag) => void
  onReset: (key: string) => void
  onOpen: (key: string) => void
}) {
  return (
    <div className="flag-row">
      <div className="min-0 fill">
        <div className="flex g-2 wrap">
          <span className="code-inline">{flag.key}</span>
          <Badge tone={sourceTone(flag.source)}>{flag.source === 'default' ? 'code default' : flag.source}</Badge>
          {flag.overridden ? <Badge tone="ok">set here</Badge> : null}
          {flag.rollout != null ? <Badge tone="accent">{flag.rollout}% rollout</Badge> : null}
          {flag.overridden && flag.value === flag.default ? (
            <span className="small dim">same as default</span>
          ) : null}
          <span className="small dim">default {flag.default ? 'on' : 'off'}</span>
        </div>
        <div className="small dim mt-8">{flag.description}</div>
      </div>
      <div className="flex g-3">
        <span className={`small ${flag.value ? 'green' : 'dim'}`}>
          {flag.value ? 'on' : 'off'}
          {flag.rollout != null ? ` · ${flag.rollout}%` : ''}
        </span>
        <button
          type="button"
          className={`switch ${flag.value ? 'on' : ''}`}
          role="switch"
          aria-checked={flag.value}
          aria-label={`${flag.key} for ${targetName}`}
          disabled={busy}
          onClick={() => onToggle(flag)}
        />
        <Button
          size="sm"
          variant="ghost"
          icon="refresh"
          title={flag.overridden ? 'Clear the stored value at this target' : 'No stored value at this target'}
          disabled={!flag.overridden || resetBusy}
          onClick={() => onReset(flag.key)}
        >
          Reset
        </Button>
        <Button size="sm" variant="ghost" icon="eye" title="Flag details" onClick={() => onOpen(flag.key)} />
      </div>
    </div>
  )
}

const rolloutPresets = [5, 10, 25, 50, 75, 100]

/** Enables the flag for a percentage of users at the selected target. */
function RolloutEditor({
  flag,
  busy,
  onApply,
}: {
  flag: DevFlag
  busy: boolean
  onApply: (rollout: number) => void
}) {
  const current = flag.rollout ?? (flag.value ? 100 : 0)
  const [percent, setPercent] = useState(current)
  return (
    <div className="stack-tight">
      <div className="card-title">Rollout</div>
      <div className="small dim">
        Turn the flag on for a share of users. Each user keeps the same answer, and raising the percentage
        only adds users. 100% means everyone.
      </div>
      <div className="flex g-3">
        <input
          type="range"
          min={0}
          max={100}
          step={1}
          value={percent}
          aria-label={`Rollout percentage for ${flag.key}`}
          onChange={(event) => setPercent(Number(event.target.value))}
          className="fill"
        />
        <input
          className="input w-xs"
          type="number"
          min={0}
          max={100}
          value={percent}
          aria-label="Rollout percentage"
          onChange={(event) => setPercent(Math.max(0, Math.min(100, Number(event.target.value) || 0)))}
        />
        <span className="small">%</span>
      </div>
      <div className="flex g-2 wrap">
        {rolloutPresets.map((preset) => (
          <Button
            key={preset}
            size="sm"
            variant={preset === percent ? 'primary' : 'ghost'}
            onClick={() => setPercent(preset)}
          >
            {preset}%
          </Button>
        ))}
      </div>
      <div className="flex">
        <Button variant="primary" icon="play" disabled={busy || (percent === current && flag.overridden)} onClick={() => onApply(percent)}>
          {percent === 100 ? 'Enable for everyone' : `Roll out to ${percent}%`}
        </Button>
      </div>
    </div>
  )
}

function FlagsPage() {
  const { push } = useToast()
  const queryClient = useQueryClient()
  const [kind, setKind] = useState<TargetKind>('global')
  const [selected, setSelected] = useState<SelectedTarget>(globalTarget)
  const { tenant: linkedTenantID } = Route.useSearch()
  const linkedTenant = useLinkedTenant(linkedTenantID)
  useEffect(() => {
    if (!linkedTenant.data) return
    setKind('tenant')
    setSelected(targetFromOption(linkedTenant.data))
  }, [linkedTenant.data])
  const [search, setSearch] = useState('')
  const [page, setPage] = useState(1)
  const [drawerKey, setDrawerKey] = useState<string | null>(null)
  const deferredSearch = useDeferredValue(search)

  const isGlobal = kind === 'global'
  const hasTarget = selected.type === 'global' || Boolean(selected.id)

  const flags = useQuery({
    queryKey: ['flags', selected.type, selected.id ?? 0],
    queryFn: ({ signal }) =>
      api<DevFlag[]>('/dev/api/flags', {
        query: { target_type: selected.type, target_id: selected.id },
        signal,
      }),
    enabled: hasTarget,
  })

  const globalFlags = useQuery({
    queryKey: ['flags', 'global', 0],
    queryFn: ({ signal }) => api<DevFlag[]>('/dev/api/flags', { signal }),
    enabled: !isGlobal,
    staleTime: 30_000,
  })

  const directory = useQuery({
    queryKey: ['flags', 'targets', kind, deferredSearch, page],
    queryFn: ({ signal }) =>
      api<Page<DevTarget>>('/dev/api/targets', {
        query: { type: kind, search: deferredSearch || undefined, page, per_page: pageSize },
        signal,
        unwrap: false,
      }),
    enabled: !isGlobal,
    placeholderData: keepPreviousData,
  })

  const targetStats = useQuery({
    queryKey: ['flags', 'targets', 'stats', kind],
    queryFn: ({ signal }) => api<DevTargetStats>('/dev/api/targets/stats', { query: { type: kind }, signal }),
    enabled: !isGlobal,
  })

  useEffect(() => {
    if (isGlobal || selected.id || selected.type !== kind) return
    const first = directory.data?.data[0]
    if (first && first.type === kind) setSelected(targetFromOption(first))
  }, [directory.data, isGlobal, kind, selected])

  const invalidateAudit = () => {
    void queryClient.invalidateQueries({ queryKey: ['actions'] })
    void queryClient.invalidateQueries({ queryKey: ['overview'] })
  }

  const invalidateDirectory = () => {
    void queryClient.invalidateQueries({ queryKey: ['flags', 'targets'] })
  }

  const setFlag = useMutation({
    mutationFn: ({ key, value, rollout }: { key: string; value: boolean; rollout?: number }) =>
      api<DevFlag[]>('/dev/api/flags', {
        method: 'PATCH',
        body: {
          target_type: selected.type,
          target_id: selected.id,
          flags: { [key]: value },
          ...(value && rollout !== undefined ? { rollout: { [key]: rollout } } : {}),
        },
      }),
    onSuccess: (items, variables) => {
      queryClient.setQueryData(['flags', selected.type, selected.id ?? 0], items)
      const what =
        variables.value && variables.rollout !== undefined && variables.rollout < 100
          ? `rolled out to ${variables.rollout}%`
          : variables.value
            ? 'enabled for everyone'
            : 'disabled'
      push(`${variables.key} ${what} at ${selected.name}`, 'success')
      invalidateDirectory()
      invalidateAudit()
    },
    onError: (error) => push(error instanceof Error ? error.message : 'Unable to update the flag', 'error'),
  })

  const resetFlag = useMutation({
    mutationFn: (key: string) =>
      api<{ key: string; target: string; reset: boolean }>(
        `/dev/api/flags/${encodeURIComponent(key)}`,
        { method: 'DELETE', query: { target_type: selected.type, target_id: selected.id } },
      ),
    onSuccess: (_result, key) => {
      push(`${key} now inherits again at ${selected.name}`, 'success')
      void flags.refetch()
      invalidateDirectory()
      invalidateAudit()
    },
    onError: (error) => push(error instanceof Error ? error.message : 'Unable to reset the flag', 'error'),
  })

  const sync = useMutation({
    mutationFn: () => api<{ created: number }>('/dev/api/flags/sync', { method: 'POST' }),
    onSuccess: (result) => {
      push(
        result.created > 0
          ? `Seeded ${result.created} missing global default${result.created === 1 ? '' : 's'}`
          : 'Global defaults are already in sync',
        'success',
      )
      void flags.refetch()
      void globalFlags.refetch()
      invalidateAudit()
    },
    onError: (error) => push(error instanceof Error ? error.message : 'Flag sync failed', 'error'),
  })

  const chooseKind = (next: TargetKind) => {
    if (next === kind) return
    setKind(next)
    setSearch('')
    setPage(1)
    setDrawerKey(null)
    if (next === 'global') {
      setSelected(globalTarget)
      return
    }
    setSelected({
      type: next,
      name: next === 'tenant' ? 'Choose a tenant' : 'Choose a user',
      detail: 'select a target from the directory',
    })
  }

  const groups = useMemo(() => {
    const byGroup = new Map<string, DevFlag[]>()
    for (const flag of flags.data ?? []) {
      const list = byGroup.get(flag.group) ?? []
      list.push(flag)
      byGroup.set(flag.group, list)
    }
    return [...byGroup.entries()].sort(([left], [right]) => left.localeCompare(right))
  }, [flags.data])

  const directoryMeta = directory.data?.meta
  const directoryTotal = directoryMeta?.total ?? 0
  const directoryRows = directory.data?.data ?? []
  const targetOverrides = flags.data?.filter((flag) => flag.overridden).length ?? 0
  const drawerFlag = drawerKey ? flags.data?.find((flag) => flag.key === drawerKey) ?? null : null
  const globalEnabled = globalFlags.data?.filter((flag) => flag.value).length ?? 0
  const totalFlags = (isGlobal ? flags.data : globalFlags.data)?.length ?? 0

  const refreshAll = () => {
    void flags.refetch()
    if (!isGlobal) {
      void globalFlags.refetch()
      void directory.refetch()
      void targetStats.refetch()
    }
  }

  return (
    <>
      <PageHeader
        title="Feature flags"
        description="Runtime toggles resolved user → tenant → global → code default. Separate from billing package entitlements."
        actions={
          <>
            <Button
              icon="refresh"
              onClick={refreshAll}
              disabled={!hasTarget || flags.isFetching || directory.isFetching}
            >
              Refresh
            </Button>
            <Button variant="primary" icon="flags" onClick={() => sync.mutate()} disabled={sync.isPending}>
              {sync.isPending ? 'Syncing…' : 'Sync defaults'}
            </Button>
          </>
        }
      />

      <Card tight>
        <div className="flex wrap g-3">
          <div>
            <div className="card-title">Resolution scope</div>
            <div className="card-sub">
              Pick a scope, then open a tenant or user from the directory to inspect and override flags.
            </div>
          </div>
          <span className="spacer" />
          <div className="pill-toggle">
            {(['global', 'tenant', 'user'] as TargetKind[]).map((option) => (
              <button
                key={option}
                type="button"
                className={kind === option ? 'active' : ''}
                onClick={() => chooseKind(option)}
              >
                {option === 'global' ? 'Global defaults' : option === 'tenant' ? 'Tenant overrides' : 'User overrides'}
              </button>
            ))}
          </div>
        </div>
      </Card>

      {isGlobal ? (
        <>
          <div className="grid cols-4 mt-24">
            <StatTile label="Flags" value={totalFlags} hint="catalog entries" />
            <StatTile
              label="Enabled"
              value={flags.data?.filter((flag) => flag.value).length ?? 0}
              tone={(flags.data?.filter((flag) => flag.value).length ?? 0) > 0 ? 'ok' : undefined}
              hint="global effective value"
            />
            <StatTile
              label="Set here"
              value={flags.data?.filter((flag) => flag.overridden).length ?? 0}
              tone={(flags.data?.filter((flag) => flag.overridden).length ?? 0) > 0 ? 'accent' : undefined}
              hint="stored global rows (may match default)"
            />
            <StatTile label="Groups" value={groups.length} hint="code-defined" />
          </div>

          {flags.isLoading ? (
            <Card className="mt-24">
              <LoadingBlock label="Loading flags…" />
            </Card>
          ) : flags.isError ? (
            <Card className="mt-24">
              <ErrorBox message={flags.error instanceof Error ? flags.error.message : 'Unable to load feature flags.'} />
            </Card>
          ) : groups.length === 0 ? (
            <Card className="mt-24">
              <EmptyState title="No flags registered" description="The catalog is empty." icon="flags" />
            </Card>
          ) : (
            <div className="stack mt-24">
              {groups.map(([group, items]) => (
                <Card key={group}>
                  <CardHeader
                    title={group}
                    sub={`${items.length} flag${items.length === 1 ? '' : 's'}`}
                    actions={<span className="small dim">{items.filter((item) => item.overridden).length} set here</span>}
                  />
                  {items.map((flag) => (
                    <FlagRow
                      key={flag.key}
                      flag={flag}
                      targetName={selected.name}
                      busy={setFlag.isPending}
                      resetBusy={resetFlag.isPending}
                      onToggle={(item) => setFlag.mutate({ key: item.key, value: !item.value })}
                      onReset={(key) => resetFlag.mutate(key)}
                      onOpen={setDrawerKey}
                    />
                  ))}
                </Card>
              ))}
            </div>
          )}
        </>
      ) : (
        <>
          <div className="grid cols-4 mt-24">
            <StatTile label="Targets" value={targetStats.data?.total ?? directoryTotal} hint={`${kindLabel(kind)} accounts`} />
            <StatTile
              label="Set here"
              value={targetStats.data?.with_overrides ?? 0}
              tone={(targetStats.data?.with_overrides ?? 0) > 0 ? 'accent' : undefined}
              hint={`${kind}s with at least one stored value`}
            />
            <StatTile label="Flags" value={totalFlags} hint="catalog entries" />
            <StatTile
              label="Enabled globally"
              value={globalEnabled}
              tone={globalEnabled > 0 ? 'ok' : undefined}
              hint="before target inheritance"
            />
          </div>

          <div className="flags-split mt-24">
            <Card>
              <CardHeader
                title={`${kind === 'tenant' ? 'Tenant' : 'User'} directory`}
                sub={
                  deferredSearch
                    ? `${directoryTotal} match${directoryTotal === 1 ? '' : 'es'} for “${deferredSearch}”`
                    : `${targetStats.data?.total ?? directoryTotal} available`
                }
              />
              <label className="field">
                <span className="field-label">Search {kind === 'tenant' ? 'tenants' : 'users'}</span>
                <input
                  className="input"
                  placeholder={kind === 'tenant' ? 'Tenant name or code' : 'User name or email'}
                  value={search}
                  onChange={(event) => {
                    setSearch(event.target.value)
                    setPage(1)
                  }}
                />
              </label>

              <div className="target-list mt-16">
                {directory.isLoading ? (
                  <LoadingBlock label={`Loading ${kind}s…`} />
                ) : directory.isError ? (
                  <ErrorBox
                    message={directory.error instanceof Error ? directory.error.message : 'Unable to load the directory.'}
                  />
                ) : directoryRows.length === 0 ? (
                  <EmptyState title={`No ${kind}s matched`} description="Try a different search term." icon="search" />
                ) : (
                  directoryRows.map((target) => {
                    const active = selected.id === target.id && selected.type === target.type
                    return (
                      <button
                        key={`${target.type}-${target.id}`}
                        type="button"
                        className={`target-row ${active ? 'selected' : ''}`}
                        onClick={() => {
                          setSelected(targetFromOption(target))
                          setDrawerKey(null)
                        }}
                      >
                        <Icon name={target.type === 'tenant' ? 'sessions' : 'developers'} />
                        <span className="min-0 fill">
                          <span className="block strong">{target.name}</span>
                          <span className="small dim">{targetDetail(target)}</span>
                        </span>
                        <span className="spacer" />
                        <Badge tone={target.overrides > 0 ? 'accent' : 'neutral'}>
                          {target.overrides} set here
                        </Badge>
                      </button>
                    )
                  })
                )}
              </div>

              <div className="flex mt-16 g-2">
                <Button
                  size="sm"
                  variant="ghost"
                  icon="chevron"
                  className="flip"
                  disabled={page <= 1 || directory.isFetching}
                  onClick={() => setPage((value) => Math.max(1, value - 1))}
                >
                  Prev
                </Button>
                <span className="small dim fill text-center">
                  Page {directoryMeta?.current_page ?? page} of {directoryMeta?.last_page ?? 1}
                </span>
                <Button
                  size="sm"
                  variant="ghost"
                  icon="chevron"
                  disabled={!directoryMeta || page >= directoryMeta.last_page || directory.isFetching}
                  onClick={() => setPage((value) => value + 1)}
                >
                  Next
                </Button>
              </div>
            </Card>

            <Card>
              <CardHeader
                title={selected.id ? selected.name : `Choose a ${kind}`}
                sub={selected.detail}
                actions={
                  selected.id ? (
                    <Badge tone={targetOverrides > 0 ? 'accent' : 'neutral'}>
                      {targetOverrides} set here
                    </Badge>
                  ) : undefined
                }
              />
              {!hasTarget ? (
                <EmptyState
                  title={`No ${kind} selected`}
                  description={`Pick a ${kind} from the directory to inspect and edit its stored values.`}
                  icon="search"
                />
              ) : flags.isLoading ? (
                <LoadingBlock label="Loading flags…" />
              ) : flags.isError ? (
                <ErrorBox message={flags.error instanceof Error ? flags.error.message : 'Unable to load feature flags.'} />
              ) : (
                <>
                  <div className="small dim mb-8">
                    Values resolve user → tenant → global → code default. Toggling stores a value at this {kind},
                    even when it matches the default; clear it to inherit again.
                  </div>
                  <div className="stack-tight">
                    {(flags.data ?? []).map((flag) => (
                      <FlagRow
                        key={flag.key}
                        flag={flag}
                        targetName={selected.name}
                        busy={setFlag.isPending}
                        resetBusy={resetFlag.isPending}
                        onToggle={(item) => setFlag.mutate({ key: item.key, value: !item.value })}
                        onReset={(key) => resetFlag.mutate(key)}
                        onOpen={setDrawerKey}
                      />
                    ))}
                  </div>
                </>
              )}
            </Card>
          </div>
        </>
      )}

      <Drawer
        open={drawerFlag !== null}
        title={drawerFlag?.key ?? 'Flag'}
        subtitle={drawerFlag?.description}
        onClose={() => setDrawerKey(null)}
        actions={
          drawerFlag ? (
            <Button
              size="sm"
              variant="ghost"
              icon="refresh"
              disabled={!drawerFlag.overridden || resetFlag.isPending}
              onClick={() => resetFlag.mutate(drawerFlag.key)}
            >
              Reset
            </Button>
          ) : undefined
        }
      >
        {drawerFlag ? (
          <div className="stack">
            <div className="flex g-2 wrap">
              <Badge tone={sourceTone(drawerFlag.source)}>
                {drawerFlag.source === 'default' ? 'code default' : drawerFlag.source}
              </Badge>
              <Badge tone={drawerFlag.value ? 'ok' : 'neutral'}>{drawerFlag.value ? 'enabled' : 'disabled'}</Badge>
              {drawerFlag.overridden ? <Badge tone="ok">set here</Badge> : <Badge>inherited</Badge>}
            </div>
            <dl className="kv">
              <dt>Target</dt>
              <dd>{selected.type === 'global' ? 'global' : `${selected.type} · ${selected.name}`}</dd>
              <dt>Effective value</dt>
              <dd>{drawerFlag.value ? 'true' : 'false'}</dd>
              <dt>Rollout</dt>
              <dd>{drawerFlag.rollout != null ? `${drawerFlag.rollout}% of users` : 'everyone'}</dd>
              <dt>Code default</dt>
              <dd>{drawerFlag.default ? 'true' : 'false'}</dd>
              <dt>Matches default</dt>
              <dd>{drawerFlag.value === drawerFlag.default ? 'yes' : 'no'}</dd>
              <dt>Source</dt>
              <dd>{drawerFlag.source}</dd>
              <dt>Stored at target</dt>
              <dd>{drawerFlag.overridden ? 'yes' : 'no'}</dd>
              <dt>Group</dt>
              <dd>{drawerFlag.group}</dd>
            </dl>
            <div className="small dim">
              Flags resolve user → tenant → global → code default. A stored value is an explicit pin at this
              target, so it counts as “set here” even when it equals the default. Clearing it makes the flag
              inherit the next level again.
            </div>
            <RolloutEditor
              key={`${drawerFlag.key}-${drawerFlag.rollout ?? 100}`}
              flag={drawerFlag}
              busy={setFlag.isPending}
              onApply={(rollout) => setFlag.mutate({ key: drawerFlag.key, value: true, rollout })}
            />
            <div className="flex">
              <Button
                icon={drawerFlag.value ? 'pause' : 'play'}
                disabled={setFlag.isPending}
                onClick={() => setFlag.mutate({ key: drawerFlag.key, value: !drawerFlag.value })}
              >
                {drawerFlag.value ? 'Disable' : 'Enable'} at this target
              </Button>
              <Button
                variant="danger"
                icon="trash"
                disabled={!drawerFlag.overridden || resetFlag.isPending}
                onClick={() => resetFlag.mutate(drawerFlag.key)}
              >
                Clear stored value
              </Button>
            </div>
          </div>
        ) : null}
      </Drawer>
    </>
  )
}
