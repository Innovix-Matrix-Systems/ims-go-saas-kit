import { useQuery } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import { useState } from 'react'
import { fetchMe, getCachedMe } from '../api/client'
import { AppearanceMenu } from '../components/AppearanceMenu'
import { DonutChart, HBarChart, TimeBars } from '../components/charts'
import { DatePicker, Select } from '../components/inputs'
import {
  Badge,
  Button,
  Card,
  Cluster,
  CodeBlock,
  ConfirmDialog,
  Drawer,
  EmptyState,
  ErrorBox,
  Grid,
  Kbd,
  LiveDot,
  LoadingBlock,
  MethodBadge,
  PageHeader,
  Split,
  Stack,
  StatTile,
  StatusChip,
  StatusDot,
  Tabs,
  type BadgeTone,
  type DotTone,
} from '../components/ui'

// The component kit in every tone: the reference for new screens and the
// page to check a theme or accent change against. Local environments only,
// like the Playground.
const sampleDays = Array.from({ length: 7 }, (_, index) => new Date(Date.UTC(2026, 0, 1 + index)).toISOString())

export const Route = createFileRoute('/_app/kit')({
  component: KitPage,
})

const badgeTones: BadgeTone[] = ['neutral', 'accent', 'ok', 'warn', 'danger', 'info', 'violet']
const dotTones: DotTone[] = ['neutral', 'ok', 'warn', 'danger', 'info']

function KitPage() {
  const me = useQuery({ queryKey: ['me'], queryFn: fetchMe, staleTime: 60_000, placeholderData: getCachedMe() ?? undefined })
  const [tab, setTab] = useState<'one' | 'two' | 'three'>('one')
  const [segment, setSegment] = useState('list')
  const [on, setOn] = useState(true)
  const [choice, setChoice] = useState('queue')
  const [date, setDate] = useState('')
  const [drawer, setDrawer] = useState(false)
  const [confirm, setConfirm] = useState(false)

  if (me.isLoading && !me.data) return <LoadingBlock label="Loading…" />
  if (!me.data?.playground) {
    return (
      <Card>
        <EmptyState icon="grid" title="The kit is local only" description="It is shown in local and development environments, like the Playground." />
      </Card>
    )
  }

  return (
    <>
      <PageHeader
        title="Component kit"
        description="Every dashboard component in every tone. Switch the theme and accent to check both."
        actions={<AppearanceMenu />}
      />
      <Stack gap={5}>
        <Card title="Typography" description="Six sizes, three weights.">
          <Stack gap={2}>
            <div className="page-title">Page title · 20</div>
            <div className="stat-value">1,284 · 24</div>
            <div className="card-title">Card title · 14 semibold</div>
            <div>Body text · 14. Readable first: content never goes below 14 px, metadata 12.</div>
            <div className="mono">mono · 13 · /api/v1/branch · 01JD8Z…</div>
            <div className="small muted">Meta · 12 · updated 12 s ago</div>
          </Stack>
        </Card>

        <Grid min="lg" gap={4}>
          <Card title="Buttons">
            <Cluster gap={2}>
              <Button variant="primary" icon="play">Primary</Button>
              <Button icon="refresh">Default</Button>
              <Button variant="ghost">Ghost</Button>
              <Button variant="danger" icon="trash">Danger</Button>
              <Button size="sm">Small</Button>
              <Button disabled>Disabled</Button>
            </Cluster>
          </Card>
          <Card title="Badges and dots">
            <Stack gap={3}>
              <Cluster gap={2}>
                {badgeTones.map((tone) => (
                  <Badge key={tone} tone={tone}>
                    {tone}
                  </Badge>
                ))}
              </Cluster>
              <Cluster gap={3}>
                <StatusChip status={200} />
                <StatusChip status={302} />
                <StatusChip status={422} />
                <StatusChip status={500} />
                <MethodBadge method="GET" />
                <MethodBadge method="POST" />
                <MethodBadge method="PATCH" />
                <MethodBadge method="DELETE" />
              </Cluster>
              <Cluster gap={3}>
                {dotTones.map((tone) => (
                  <Cluster key={tone} gap={1}>
                    <StatusDot tone={tone} /> <span className="small muted">{tone}</span>
                  </Cluster>
                ))}
                <Cluster gap={1}>
                  <LiveDot /> <span className="small muted">running</span>
                </Cluster>
              </Cluster>
            </Stack>
          </Card>
        </Grid>

        <Grid min="sm" gap={3}>
          <StatTile label="Health" value="All OK" hint="checked 12 s ago" to="/inspector" />
          <StatTile label="Queue" value={42} hint="3 running" tone="warn" />
          <StatTile label="Errors, 24 h" value={7} hint="2 classes" tone="danger" />
          <StatTile label="Migrations" value="Up to date" tone="ok" />
          <StatTile label="Accent" value={128} tone="accent" />
        </Grid>

        <Split
          aside={
            <Card title="Aside" description="Split: a list next to its detail.">
              <Stack gap={1}>
                <div className="group-list-item selected">
                  <span className="group-name">Selected row</span>
                </div>
                <div className="group-list-item">
                  <span className="group-name">Another row</span>
                </div>
              </Stack>
            </Card>
          }
        >
          <Card title="Table in a flush card" description="Headers 11 px uppercase; numbers right-aligned." flush actions={<Button size="sm">Action</Button>}>
            <div className="table-wrap">
              <table className="table">
                <thead>
                  <tr>
                    <th>Route</th>
                    <th>Status</th>
                    <th className="right">Duration</th>
                  </tr>
                </thead>
                <tbody>
                  <tr className="clickable selected">
                    <td className="path">/api/v1/branch</td>
                    <td><StatusChip status={201} /></td>
                    <td className="right num">8.4 ms</td>
                  </tr>
                  <tr className="clickable">
                    <td className="path">/api/v1/login</td>
                    <td><StatusChip status={422} /></td>
                    <td className="right num">112.0 ms</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </Card>
        </Split>

        <Grid min="lg" gap={4}>
          <Card title="Controls">
            <Stack gap={3}>
              <Tabs
                tabs={[
                  { id: 'one', label: 'Overview' },
                  { id: 'two', label: 'Queries', count: 9 },
                  { id: 'three', label: 'Logs', count: 2 },
                ]}
                active={tab}
                onChange={setTab}
              />
              <Cluster gap={3}>
                <div className="segmented">
                  {['grid', 'list'].map((value) => (
                    <button key={value} type="button" className={segment === value ? 'active' : ''} onClick={() => setSegment(value)}>
                      {value}
                    </button>
                  ))}
                </div>
                <button type="button" className={`switch ${on ? 'on' : ''}`} aria-pressed={on} aria-label="Switch" onClick={() => setOn(!on)} />
                <span className="keys">
                  <Kbd>Ctrl</Kbd>
                  <Kbd>k</Kbd>
                </span>
              </Cluster>
              <label className="field">
                <span className="field-label">Text input</span>
                <input className="input" placeholder="Placeholder text" />
              </label>
              <Cluster gap={3}>
                <Select
                  value={choice}
                  onChange={setChoice}
                  ariaLabel="Select"
                  options={[
                    { value: 'queue', label: 'Queue' },
                    { value: 'logs', label: 'Logs' },
                  ]}
                />
                <DatePicker value={date} onChange={setDate} ariaLabel="Date" />
              </Cluster>
            </Stack>
          </Card>
          <Card title="States">
            <Stack gap={3}>
              <ErrorBox message="The request failed (503)." onRetry={() => undefined} />
              <div className="confirm-note">This changes data: you will be asked to confirm.</div>
              <CodeBlock label="JSON" code={'{\n  "status": "ok",\n  "checks": 11\n}'} />
              <Cluster gap={2}>
                <Button onClick={() => setDrawer(true)}>Open drawer</Button>
                <Button variant="danger" onClick={() => setConfirm(true)}>Open dialog</Button>
              </Cluster>
            </Stack>
          </Card>
        </Grid>

        <Card title="Empty state" flush>
          <EmptyState icon="queue" title="No jobs yet" description="Queued jobs appear here as they run." />
        </Card>

        <div className="chart-row">
          <Card title="DonutChart" description="Parts of a whole; seriesTones by default, a meaning tone when it has one.">
            <DonutChart
              label="Sample donut"
              totalLabel="files"
              slices={[
                { key: 'image', label: 'Images', value: 42 },
                { key: 'document', label: 'Documents', value: 27 },
                { key: 'video', label: 'Videos', value: 9 },
                { key: 'other', label: 'Other', value: 4 },
              ]}
            />
          </Card>
          <Card title="HBarChart" description="One bar per row, stacked from parts.">
            <HBarChart
              label="Sample bars"
              labelWidth={140}
              parts={[
                { key: 'ok', label: 'Succeeded', tone: 'accent' },
                { key: 'errors', label: 'Failed', tone: 'danger' },
              ]}
              rows={[
                { key: 'a', label: 'GET /api/v1/branch', values: { ok: 40, errors: 2 } },
                { key: 'b', label: 'POST /api/v1/login', values: { ok: 22, errors: 6 } },
                { key: 'c', label: 'GET /api/health', values: { ok: 15, errors: 0 } },
              ]}
            />
          </Card>
        </div>
        <Card title="TimeBars" description="Stacked bars over time with a hover readout.">
          <TimeBars
            label="Sample time bars"
            parts={[
              { key: 'sent', label: 'Sent', tone: 'ok' },
              { key: 'failed', label: 'Failed', tone: 'danger' },
            ]}
            points={sampleDays.map((day, index) => ({ at: day, values: { sent: [12, 18, 9, 22, 17, 0, 14][index], failed: [1, 0, 3, 1, 0, 0, 2][index] } }))}
            tick={(at) => at.slice(5, 10)}
            title={(at) => at.slice(0, 10)}
          />
        </Card>
      </Stack>

      <Drawer open={drawer} title="Drawer" subtitle="md, lg or fit" size="md" onClose={() => setDrawer(false)}>
        <LoadingBlock />
      </Drawer>
      <ConfirmDialog
        open={confirm}
        title="Delete the record?"
        description="This cannot be undone."
        confirmLabel="Delete"
        danger
        onConfirm={() => setConfirm(false)}
        onCancel={() => setConfirm(false)}
      />
    </>
  )
}
