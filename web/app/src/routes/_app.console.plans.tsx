import { createFileRoute } from '@tanstack/react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'
import { api } from '../api/client'
import { can, useSession } from '../auth/session'
import { featureName, useFeatureText, usePrice, type Package } from '../components/billing'
import { Field, useApiErrors } from '../components/form'
import { Select } from '../components/inputs'
import { pageSlice, Pagination } from '../components/Pagination'
import { useToast } from '../components/Toasts'
import { Badge, Button, Card, ConfirmDialog, Drawer, EmptyState, Icon, PageHeader, Spinner } from '../components/ui'
import { useT } from '../i18n'
import { TableSkeleton } from '../components/Skeleton'

export const Route = createFileRoute('/_app/console/plans')({
  component: PlansPage,
})

interface CatalogFeature {
  key: string
  description: string
  is_limit: boolean
}

// What a package can grant comes from the API, so a new feature appears here by itself.
const catalogQuery = {
  queryKey: ['console', 'package-features'],
  queryFn: () => api<CatalogFeature[]>('/packages/features', { asStaff: true }),
  staleTime: 300_000,
}

const packagesQuery = { queryKey: ['console', 'packages'], queryFn: () => api<Package[]>('/packages', { asStaff: true }) }

function PlansPage() {
  const t = useT()
  const session = useSession()
  const price = usePrice()
  const featureText = useFeatureText()
  const packages = useQuery(packagesQuery)
  const catalog = useQuery(catalogQuery)
  const [editing, setEditing] = useState<Package | 'new' | null>(null)
  const [page, setPage] = useState(1)
  const [perPage, setPerPage] = useState(20)

  return (
    <>
      <PageHeader
        title={t('console.plans')}
        description={t('console.plansSubtitle')}
        actions={
          can(session, 'package.create') ? (
            <Button variant="primary" icon="plus" onClick={() => setEditing('new')}>
              {t('console.newPlan')}
            </Button>
          ) : null
        }
      />
      <Card flush>
        {!packages.data ? (
          <TableSkeleton columns={4} avatar={false} />
        ) : packages.data.length === 0 ? (
          <EmptyState icon="package" title={t('console.noApprovals')} />
        ) : (
          <div className="table-wrap">
            <table className="table">
              <thead>
                <tr>
                  <th>{t('console.planName')}</th>
                  <th>{t('console.price')}</th>
                  <th>{t('console.features')}</th>
                  <th>{t('members.colStatus')}</th>
                </tr>
              </thead>
              <tbody>
                {pageSlice(packages.data, page, perPage).map((pkg) => (
                  <tr key={pkg.id} className="clickable" onClick={() => setEditing(pkg)}>
                    <td>
                      <span className="member-meta">
                        <span className="member-name">{pkg.name}</span>
                        {pkg.description ? <span className="small muted">{pkg.description}</span> : null}
                      </span>
                    </td>
                    <td className="nowrap">{price(pkg)}</td>
                    <td className="small muted">
                      {pkg.features
                        .filter((feature) => feature.is_enabled)
                        .map((feature) => featureText(feature.feature_key, feature.limit_value))
                        .join(' · ')}
                    </td>
                    <td>{pkg.is_active ? <Badge tone="ok">{t('console.offered')}</Badge> : <Badge tone="neutral">{t('console.hidden')}</Badge>}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        {packages.data && packages.data.length > 0 ? (
          <Pagination
            page={page}
            perPage={perPage}
            total={packages.data.length}
            onPage={setPage}
            onPerPage={(size) => {
              setPerPage(size)
              setPage(1)
            }}
          />
        ) : null}
      </Card>
      {editing && catalog.data ? <PlanDrawer pkg={editing === 'new' ? null : editing} catalog={catalog.data} onClose={() => setEditing(null)} /> : null}
    </>
  )
}

interface FeatureState {
  enabled: boolean
  limit: string
}

function PlanDrawer({ pkg, catalog, onClose }: { pkg: Package | null; catalog: CatalogFeature[]; onClose: () => void }) {
  const limits = new Set(catalog.filter((feature) => feature.is_limit).map((feature) => feature.key))
  const t = useT()
  const session = useSession()
  const client = useQueryClient()
  const { push } = useToast()
  const errors = useApiErrors()
  const editable = pkg ? can(session, 'package.update') : can(session, 'package.create')
  const [name, setName] = useState(pkg?.name ?? '')
  const [description, setDescription] = useState(pkg?.description ?? '')
  const [priceTaka, setPriceTaka] = useState(pkg ? String(pkg.price / 100) : '0')
  const [cycle, setCycle] = useState<Package['billing_cycle']>(pkg?.billing_cycle ?? 'monthly')
  const [trialDays, setTrialDays] = useState(String(pkg?.trial_days ?? 0))
  const [order, setOrder] = useState(String(pkg?.sort_order ?? 0))
  const [active, setActive] = useState(pkg?.is_active ?? true)
  const [features, setFeatures] = useState<Record<string, FeatureState>>(() => {
    const out: Record<string, FeatureState> = {}
    for (const { key } of catalog) {
      const found = pkg?.features.find((feature) => feature.feature_key === key)
      out[key] = { enabled: found?.is_enabled ?? false, limit: String(found?.limit_value ?? (limits.has(key) ? -1 : 0)) }
    }
    return out
  })
  const [busy, setBusy] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)

  const done = async () => {
    await client.invalidateQueries({ queryKey: ['console', 'packages'] })
    await client.invalidateQueries({ queryKey: ['public-packages'] })
    push(t('form.saved'), 'success')
    onClose()
  }

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    setBusy(true)
    errors.clear()
    const body = {
      name: name.trim(),
      description: description.trim() || null,
      price: Math.round(Number(priceTaka || 0) * 100),
      billing_cycle: cycle,
      trial_days: Number(trialDays || 0),
      sort_order: Number(order || 0),
      is_active: active,
      features: catalog
        .map((feature) => feature.key)
        .filter((key) => features[key].enabled)
        .map((key) => ({ feature_key: key, is_enabled: true, limit_value: limits.has(key) ? Number(features[key].limit || 0) : 0 })),
    }
    try {
      if (pkg) await api(`/packages/${pkg.id}`, { method: 'PATCH', asStaff: true, body })
      else await api('/packages', { method: 'POST', asStaff: true, idempotent: true, body })
      await done()
    } catch (cause) {
      errors.setError(cause instanceof Error ? cause : new Error(String(cause)))
    } finally {
      setBusy(false)
    }
  }

  const remove = async () => {
    if (!pkg) return
    setBusy(true)
    try {
      await api(`/packages/${pkg.id}`, { method: 'DELETE', asStaff: true })
      await done()
    } catch (cause) {
      errors.setError(cause instanceof Error ? cause : new Error(String(cause)))
    } finally {
      setBusy(false)
      setConfirmDelete(false)
    }
  }

  return (
    <Drawer open title={pkg ? pkg.name : t('console.newPlan')} onClose={onClose}>
      <form onSubmit={submit} className="stack">
        <div className="form-grid-2">
          <Field label={t('console.planName')} error={errors.field('name')}>
            <input className="input" required disabled={!editable} value={name} onChange={(e) => setName(e.target.value)} />
          </Field>
          <Field label={t('console.cycle')} error={errors.field('billing_cycle')}>
            <Select
              value={cycle}
              disabled={!editable}
              ariaLabel={t('console.cycle')}
              onChange={setCycle}
              options={[
                { value: 'monthly', label: t('console.monthly') },
                { value: 'yearly', label: t('console.yearly') },
                { value: 'one_time', label: t('console.oneTime') },
              ]}
            />
          </Field>
        </div>
        <Field label={`${t('console.planDescription')} (${t('form.optional')})`} error={errors.field('description')}>
          <input className="input" disabled={!editable} value={description} onChange={(e) => setDescription(e.target.value)} />
        </Field>
        <div className="form-grid-3">
          <Field label={t('console.price')} error={errors.field('price')}>
            <input className="input" type="number" min="0" step="0.01" disabled={!editable} value={priceTaka} onChange={(e) => setPriceTaka(e.target.value)} />
          </Field>
          <Field label={t('console.trialDays')} error={errors.field('trial_days')}>
            <input className="input" type="number" min="0" disabled={!editable} value={trialDays} onChange={(e) => setTrialDays(e.target.value)} />
          </Field>
          <Field label={t('console.order')} error={errors.field('sort_order')}>
            <input className="input" type="number" disabled={!editable} value={order} onChange={(e) => setOrder(e.target.value)} />
          </Field>
        </div>
        <label className="flex">
          <button type="button" role="switch" aria-checked={active} className={`switch ${active ? 'on' : ''}`} disabled={!editable} onClick={() => setActive(!active)} />
          {t('console.offered')}
        </label>
        <div className="stack-tight">
          <div className="field-label">{t('console.features')}</div>
          <div className="plan-feature-list">
            {catalog.map(({ key, description }) => (
              <div key={key} className="plan-feature-row" title={description}>
                <label className="check-row">
                  <input
                    type="checkbox"
                    disabled={!editable}
                    checked={features[key].enabled}
                    onChange={(e) => setFeatures((current) => ({ ...current, [key]: { ...current[key], enabled: e.target.checked } }))}
                  />
                  <span className="permission-meta">
                    <span>{featureName(t, key).toLowerCase() === key ? description : featureName(t, key)}</span>
                  </span>
                </label>
                {limits.has(key) && features[key].enabled ? (
                  <input
                    className="input plan-limit"
                    type="number"
                    min="-1"
                    title={t('console.limitHint')}
                    disabled={!editable}
                    value={features[key].limit}
                    onChange={(e) => setFeatures((current) => ({ ...current, [key]: { ...current[key], limit: e.target.value } }))}
                  />
                ) : null}
              </div>
            ))}
          </div>
          <div className="small muted">{t('console.limitHint')}</div>
        </div>
        {errors.box}
        {editable ? (
          <div className="flex wrap">
            <Button variant="primary" type="submit" disabled={busy || name.trim().length < 2}>
              {busy ? <Spinner /> : <Icon name="check" />} {t('console.save')}
            </Button>
            <span className="spacer" />
            {pkg && can(session, 'package.delete') ? (
              <Button variant="danger" icon="trash" type="button" disabled={busy} onClick={() => setConfirmDelete(true)}>
                {t('console.deletePlan')}
              </Button>
            ) : null}
          </div>
        ) : null}
      </form>
      <ConfirmDialog
        open={confirmDelete}
        title={t('console.deletePlan')}
        description={t('console.deletePlanConfirm', { name: pkg?.name ?? '' })}
        confirmLabel={t('console.deletePlan')}
        danger
        busy={busy}
        onConfirm={() => void remove()}
        onCancel={() => setConfirmDelete(false)}
      />
    </Drawer>
  )
}
