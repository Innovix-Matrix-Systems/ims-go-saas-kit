import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { useEffect, useMemo, useState } from 'react'
import { ApiError, api } from '../api/client'
import type { CursorPage, DevCommand, DevCommandOption, DevCommandRun } from '../api/types'
import { Select } from '../components/inputs'
import { useToast } from '../components/Toasts'
import {
  Badge,
  Button,
  Card,
  Cluster,
  CopyButton,
  Drawer,
  EmptyState,
  ErrorBox,
  Icon,
  LiveDot,
  LoadingBlock,
  Modal,
  PageHeader,
  Spinner,
  Stack,
  StatusDot,
  type BadgeTone,
} from '../components/ui'
import { formatDateTime, formatDuration, relativeTime } from '../format'

export const Route = createFileRoute('/_app/commands')({
  validateSearch: (search: Record<string, unknown>): { run?: string; open?: number } => ({
    run: typeof search.run === 'string' && search.run ? search.run : undefined,
    open: typeof search.open === 'number' ? search.open : undefined,
  }),
  component: CommandsPage,
})

function statusTone(status?: string | null): BadgeTone {
  if (status === 'ok') return 'ok'
  if (status === 'error') return 'danger'
  if (status === 'running') return 'accent'
  return 'neutral'
}

const statusLabel = (status?: string | null) =>
  status === 'ok' ? 'succeeded' : status === 'error' ? 'failed' : status === 'running' ? 'running' : 'never run'

/** A run's status as a dot: static, or live while it runs. */
function RunDot({ status }: { status?: string | null }) {
  if (status === 'running') return <LiveDot />
  return <StatusDot tone={status === 'ok' ? 'ok' : status === 'error' ? 'danger' : 'neutral'} title={statusLabel(status)} />
}

function CommandsPage() {
  const { run: requested } = Route.useSearch()
  const navigate = useNavigate({ from: '/commands' })
  const queryClient = useQueryClient()
  const { push } = useToast()
  const [filter, setFilter] = useState('')
  const [prompt, setPrompt] = useState<DevCommand | null>(null)
  const [openRunId, setOpenRunId] = useState<number | null>(null)
  const [historyFor, setHistoryFor] = useState<DevCommand | null>(null)

  const commands = useQuery({
    queryKey: ['commands', 'list'],
    queryFn: ({ signal }) => api<DevCommand[]>('/dev/api/commands', { signal }),
  })
  const runs = useQuery({
    queryKey: ['commands', 'runs'],
    queryFn: ({ signal }) =>
      api<CursorPage<DevCommandRun>>('/dev/api/commands/runs', { query: { limit: 15 }, signal, unwrap: false }),
    refetchInterval: (query) => (query.state.data?.data.some((run) => run.status === 'running') ? 2000 : false),
  })

  // ?run=permission:sync (from the command palette) opens that command's prompt.
  useEffect(() => {
    if (!requested || !commands.data) return
    const command = commands.data.find((item) => item.name === requested)
    if (command) setPrompt(command)
    void navigate({ search: {}, replace: true })
  }, [requested, commands.data, navigate])

  const execute = useMutation({
    mutationFn: ({ command, options }: { command: DevCommand; options: Record<string, string> }) =>
      api<DevCommandRun>(`/dev/api/commands/${encodeURIComponent(command.name)}/run`, {
        method: 'POST',
        body: { options },
      }),
    onSuccess: (run, { command }) => {
      setPrompt(null)
      setOpenRunId(run.id)
      if (run.status === 'running') push(`${command.label} started in the background`, 'info')
      else if (run.status === 'ok') push(`${command.label} finished`, 'success')
      else push(`${command.label} failed`, 'error')
      void queryClient.invalidateQueries({ queryKey: ['commands'] })
      void queryClient.invalidateQueries({ queryKey: ['actions'] })
    },
    onError: (error) => push(error instanceof Error ? error.message : 'The command could not run', 'error'),
  })

  const groups = useMemo(() => {
    const needle = filter.trim().toLowerCase()
    const map = new Map<string, DevCommand[]>()
    for (const command of commands.data ?? []) {
      if (needle && !`${command.name} ${command.label} ${command.description}`.toLowerCase().includes(needle)) continue
      const list = map.get(command.group) ?? []
      list.push(command)
      map.set(command.group, list)
    }
    return [...map.entries()]
  }, [commands.data, filter])

  const start = (command: DevCommand) => {
    if (command.confirm || command.options.length > 0) setPrompt(command)
    else execute.mutate({ command, options: {} })
  }

  return (
    <>
      <PageHeader
        title="Commands"
        description="Maintenance commands: the same work as the ims CLI, with their output kept. Slow ones run in the background."
      />
      <div className="commands-layout">
        <Card
          title="Commands"
          description={commands.data ? `${commands.data.length} available` : undefined}
          flush
          actions={
            <label className="inspector-search command-search">
              <Icon name="search" />
              <input
                className="input"
                type="search"
                placeholder="Find a command"
                value={filter}
                onChange={(event) => setFilter(event.target.value)}
              />
            </label>
          }
        >
          {commands.isLoading ? (
            <LoadingBlock label="Loading commands…" />
          ) : commands.isError ? (
            <ErrorBox message={commands.error instanceof Error ? commands.error.message : 'Unable to load commands.'} />
          ) : groups.length === 0 ? (
            <EmptyState title="No commands match" description="Try another search." icon="terminal" />
          ) : (
            groups.map(([group, items]) => (
              <div key={group} className="command-group">
                <div className="command-group-title">{group}</div>
                {items.map((command) => {
                  const busy = execute.isPending && execute.variables?.command.name === command.name
                  return (
                    <div key={command.name} className="command-row">
                      <div className="command-main">
                        <Cluster gap={2}>
                          <span className="command-label-text">{command.label}</span>
                          {command.confirm ? <Badge tone="warn">changes data</Badge> : null}
                          {command.background ? <Badge tone="info">background</Badge> : null}
                          {command.options.length > 0 ? (
                            <Badge>
                              {command.options.length} option{command.options.length === 1 ? '' : 's'}
                            </Badge>
                          ) : null}
                        </Cluster>
                        <div className="small dim">
                          <span className="mono">{command.name}</span> · {command.description}
                        </div>
                      </div>
                      <div className="command-last small" title={command.last_run_at ? formatDateTime(command.last_run_at) : undefined}>
                        <RunDot status={command.last_status} />
                        {statusLabel(command.last_status)}
                        {command.last_run_at ? ` · ${relativeTime(command.last_run_at)}` : ''}
                      </div>
                      <Button
                        size="sm"
                        variant="ghost"
                        icon="clock"
                        disabled={command.runs === 0}
                        onClick={() => setHistoryFor(command)}
                      >
                        History{command.runs ? ` (${command.runs})` : ''}
                      </Button>
                      <Button size="sm" variant="primary" icon="play" disabled={busy} onClick={() => start(command)}>
                        {busy ? <Spinner /> : null}
                        Run
                      </Button>
                    </div>
                  )
                })}
              </div>
            ))
          )}
        </Card>

        <Card title="Recent runs" description={runs.data ? `The last ${runs.data.data.length}` : undefined} flush>
          {runs.isLoading ? (
            <LoadingBlock />
          ) : (runs.data?.data.length ?? 0) === 0 ? (
            <EmptyState title="Nothing run yet" description="Runs and their output appear here." icon="terminal" />
          ) : (
            <div className="run-list">
              {runs.data?.data.map((run) => (
                <RunRow key={run.id} run={run} onOpen={() => setOpenRunId(run.id)} />
              ))}
            </div>
          )}
        </Card>
      </div>

      <RunPrompt
        command={prompt}
        busy={execute.isPending}
        onCancel={() => setPrompt(null)}
        onRun={(options) => prompt && execute.mutate({ command: prompt, options })}
      />
      <RunDrawer id={openRunId} onClose={() => setOpenRunId(null)} />
      <HistoryDrawer command={historyFor} onClose={() => setHistoryFor(null)} onOpen={(id) => setOpenRunId(id)} />
    </>
  )
}

function RunRow({ run, onOpen }: { run: DevCommandRun; onOpen: () => void }) {
  return (
    <button type="button" className="run-row" onClick={onOpen}>
      <RunDot status={run.status} />
      <span className="run-main">
        <span className="mono run-command">{run.command}</span>
        <span className="small dim">
          {statusLabel(run.status)} · {relativeTime(run.created_at)}
          {run.status !== 'running' ? ` · ${formatDuration(run.duration_ms)}` : ''} · user #{run.actor_user_id}
        </span>
      </span>
      <Icon name="chevron" className="icon-inline dim" />
    </button>
  )
}

function RunPrompt({
  command,
  busy,
  onCancel,
  onRun,
}: {
  command: DevCommand | null
  busy: boolean
  onCancel: () => void
  onRun: (options: Record<string, string>) => void
}) {
  const [values, setValues] = useState<Record<string, string>>({})
  useEffect(() => {
    setValues(Object.fromEntries((command?.options ?? []).map((option) => [option.name, option.default])))
  }, [command])
  if (!command) return null
  return (
    <Modal
      open
      title={command.label}
      description={
        <>
          <span className="mono">{command.name}</span> — {command.description}
        </>
      }
      onClose={onCancel}
      actions={
        <>
          <Button variant="ghost" onClick={onCancel} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" icon="play" onClick={() => onRun(values)} disabled={busy}>
            {busy ? <Spinner /> : null}
            {command.background ? 'Start' : 'Run'}
          </Button>
        </>
      }
    >
      <Stack gap={3}>
        {command.options.map((option) => (
          <OptionField
            key={option.name}
            option={option}
            value={values[option.name] ?? ''}
            onChange={(value) => setValues({ ...values, [option.name]: value })}
          />
        ))}
        {command.confirm ? (
          <div className="confirm-note">
            <Icon name="alert" /> This command changes data. Run it?
          </div>
        ) : null}
      </Stack>
    </Modal>
  )
}

function OptionField({
  option,
  value,
  onChange,
}: {
  option: DevCommandOption
  value: string
  onChange: (value: string) => void
}) {
  if (option.type === 'bool') {
    return (
      <div className="option-toggle">
        <div>
          <div className="option-label">{option.label}</div>
          {option.description ? <div className="small dim">{option.description}</div> : null}
        </div>
        <button
          type="button"
          role="switch"
          aria-checked={value === 'true'}
          aria-label={option.label}
          className={`switch ${value === 'true' ? 'on' : ''}`}
          onClick={() => onChange(value === 'true' ? 'false' : 'true')}
        />
      </div>
    )
  }
  return (
    <label className="field">
      <span className="field-label">{option.label}</span>
      {option.type === 'select' ? (
        <Select value={value} onChange={onChange} options={(option.choices ?? []).map((choice) => ({ value: choice, label: choice }))} />
      ) : (
        <input
          className="input"
          inputMode={option.type === 'number' ? 'numeric' : undefined}
          value={value}
          onChange={(event) => onChange(event.target.value)}
        />
      )}
      {option.description ? <span className="small dim">{option.description}</span> : null}
    </label>
  )
}

function RunDrawer({ id, onClose }: { id: number | null; onClose: () => void }) {
  const queryClient = useQueryClient()
  const run = useQuery({
    queryKey: ['commands', 'run', id],
    queryFn: ({ signal }) => api<DevCommandRun>(`/dev/api/commands/runs/${id}`, { signal }),
    enabled: id !== null,
    refetchInterval: (query) => (query.state.data?.status === 'running' ? 1500 : false),
  })
  const status = run.data?.status
  useEffect(() => {
    if (status && status !== 'running') void queryClient.invalidateQueries({ queryKey: ['commands', 'list'] })
  }, [status, queryClient])

  if (id === null) return null
  const data = run.data
  return (
    <Drawer
      open
      title={data ? data.command : 'Command run'}
      subtitle={data ? `Run #${data.id} · ${formatDateTime(data.created_at)} · user #${data.actor_user_id}` : undefined}
      onClose={onClose}
      actions={data?.output ? <CopyButton value={data.output} label="Copy output" /> : undefined}
    >
      {run.isLoading || !data ? (
        <LoadingBlock label="Loading run…" />
      ) : run.error instanceof ApiError ? (
        <ErrorBox message={run.error.message} />
      ) : (
        <Stack gap={3}>
          <Cluster gap={2}>
            <Badge tone={statusTone(data.status)}>{statusLabel(data.status)}</Badge>
            {data.status !== 'running' ? <Badge>{formatDuration(data.duration_ms)}</Badge> : <Spinner />}
            {Object.entries(data.options).map(([key, value]) => (
              <span key={key} className="code-inline">
                {key}={value}
              </span>
            ))}
          </Cluster>
          {data.error ? <ErrorBox message={data.error} /> : null}
          <pre className="terminal">
            <span className="terminal-prompt">$ ims {data.command}</span>
            {'\n'}
            {data.output || (data.status === 'running' ? 'Running…' : '(no output)')}
          </pre>
        </Stack>
      )}
    </Drawer>
  )
}

function HistoryDrawer({
  command,
  onClose,
  onOpen,
}: {
  command: DevCommand | null
  onClose: () => void
  onOpen: (id: number) => void
}) {
  const history = useQuery({
    queryKey: ['commands', 'history', command?.name],
    queryFn: ({ signal }) =>
      api<CursorPage<DevCommandRun>>('/dev/api/commands/runs', {
        query: { command: command?.name, limit: 50 },
        signal,
        unwrap: false,
      }),
    enabled: command !== null,
  })
  if (!command) return null
  return (
    <Drawer open title={`${command.label} history`} subtitle={command.name} onClose={onClose}>
      {history.isLoading ? (
        <LoadingBlock />
      ) : (history.data?.data.length ?? 0) === 0 ? (
        <EmptyState title="No runs yet" icon="terminal" />
      ) : (
        <div className="run-list">
          {history.data?.data.map((run) => (
            <RunRow
              key={run.id}
              run={run}
              onOpen={() => {
                onClose()
                onOpen(run.id)
              }}
            />
          ))}
        </div>
      )}
    </Drawer>
  )
}
