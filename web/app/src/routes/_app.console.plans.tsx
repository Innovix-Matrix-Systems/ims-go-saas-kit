import { createFileRoute } from '@tanstack/react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Fragment, useState, type FormEvent } from 'react'
import { api } from '../api/client'
import { can, useSession } from '../auth/session'
import { atCycle, featureName, formatMB, includedFeatures, useDefaultCurrency, useFeatureText, usePrice, type Package } from '../components/billing'
import { Field, useApiErrors } from '../components/form'
import { MoneyInput, Select } from '../components/inputs'
import { fromMinor, toMinor } from '../money'
import { pageSlice, Pagination } from '../components/Pagination'
import { useToast } from '../components/Toasts'
import { Badge, Button, Card, ConfirmDialog, DetailStrip, Drawer, EmptyState, Icon, PageHeader, Spinner } from '../components/ui'
import { branchesEnabled } from '../product'
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
                    <td className="nowrap">
                      {price(pkg)}
                      {pkg.yearly_price != null ? ` · ${price(atCycle(pkg, 'yearly'))}` : ''}
                    </td>
                    <td className="small muted">
                      {includedFeatures(pkg.features)
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

/** The catalog without the branch limit while the product uses one branch (product.ts). */
function shownCatalog(catalog: CatalogFeature[]): CatalogFeature[] {
  return catalog.filter((feature) => branchesEnabled || feature.key !== 'branches.max')
}

/** A feature's label: the app's name for it, or the catalog's description when the app has none. */
function useFeatureLabel() {
  const t = useT()
  return (key: string, description: string) => (featureName(t, key).toLowerCase() === key ? description : featureName(t, key))
}

/**
 * A plan opens as a read-only summary; Edit switches to the form, Cancel back. A new plan opens
 * straight in the form.
 */
function PlanDrawer({ pkg, catalog, onClose }: { pkg: Package | null; catalog: CatalogFeature[]; onClose: () => void }) {
  const t = useT()
  const session = useSession()
  const client = useQueryClient()
  const { push } = useToast()
  const [mode, setMode] = useState<'view' | 'edit'>(pkg ? 'view' : 'edit')
  const [confirmDelete, setConfirmDelete] = useState(false)
  const [busy, setBusy] = useState(false)
  const canEdit = pkg ? can(session, 'package.update') : can(session, 'package.create')
  const canDelete = pkg != null && can(session, 'package.delete')

  const saved = async () => {
    await client.invalidateQueries({ queryKey: ['console', 'packages'] })
    await client.invalidateQueries({ queryKey: ['public-packages'] })
    push(t('form.saved'), 'success')
    onClose()
  }

  const remove = async () => {
    if (!pkg) return
    setBusy(true)
    try {
      await api(`/packages/${pkg.id}`, { method: 'DELETE', asStaff: true })
      await saved()
    } catch (cause) {
      push(cause instanceof Error ? cause.message : String(cause), 'error')
    } finally {
      setBusy(false)
      setConfirmDelete(false)
    }
  }

  const viewing = pkg != null && mode === 'view'
  return (
    <Drawer
      open
      title={pkg ? (mode === 'edit' ? t('console.editPlan') : pkg.name) : t('console.newPlan')}
      subtitle={viewing ? (pkg.description ?? undefined) : undefined}
      onClose={onClose}
      actions={
        viewing ? (
          <>
            {canEdit ? (
              <Button icon="edit" onClick={() => setMode('edit')}>
                {t('console.editPlan')}
              </Button>
            ) : null}
            {canDelete ? (
              <Button variant="danger" icon="trash" disabled={busy} onClick={() => setConfirmDelete(true)}>
                {t('console.deletePlan')}
              </Button>
            ) : null}
          </>
        ) : undefined
      }
    >
      {viewing ? (
        <PlanView pkg={pkg} catalog={catalog} />
      ) : (
        <PlanForm pkg={pkg} catalog={catalog} onCancel={() => (pkg ? setMode('view') : onClose())} onSaved={() => void saved()} />
      )}
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

/** What a plan costs and grants, read-only. */
function PlanView({ pkg, catalog }: { pkg: Package; catalog: CatalogFeature[] }) {
  const t = useT()
  const price = usePrice()
  const label = useFeatureLabel()
  const value = (key: string) => pkg.features.find((feature) => feature.feature_key === key)
  const features = shownCatalog(catalog)
  const limits = features.filter((feature) => feature.is_limit)
  const switches = features.filter((feature) => !feature.is_limit)
  const included = switches.filter((feature) => value(feature.key)?.is_enabled)
  const missing = switches.filter((feature) => !value(feature.key)?.is_enabled)

  const limitText = (key: string) => {
    const found = value(key)
    if (!found?.is_enabled || found.limit_value === 0) return <span className="muted">{t('console.notIncluded')}</span>
    if (found.limit_value === -1) return t('billing.unlimited')
    return key === 'storage.mb' ? formatMB(found.limit_value) : String(found.limit_value)
  }

  return (
    <div className="stack">
      <DetailStrip
        items={[
          { label: pkg.billing_cycle === 'monthly' ? t('console.monthlyPrice') : t('console.price'), value: price(pkg) },
          ...(pkg.billing_cycle === 'monthly'
            ? [{ label: t('console.yearlyPrice'), value: pkg.yearly_price != null ? price(atCycle(pkg, 'yearly')) : <span className="muted">{t('console.monthlyOnly')}</span> }]
            : []),
          { label: t('console.trialDays'), value: pkg.trial_days > 0 ? String(pkg.trial_days) : '—' },
          { label: t('console.order'), value: String(pkg.sort_order) },
          {
            label: t('members.colStatus'),
            value: pkg.is_active ? <Badge tone="ok">{t('console.offered')}</Badge> : <Badge tone="neutral">{t('console.hidden')}</Badge>,
          },
        ]}
      />

      <section className="drawer-section">
        <h3 className="drawer-section-title">{t('console.sectionLimits')}</h3>
        <dl className="details">
          {limits.map((feature) => (
            <Fragment key={feature.key}>
              <dt title={feature.description}>{label(feature.key, feature.description)}</dt>
              <dd>{limitText(feature.key)}</dd>
            </Fragment>
          ))}
        </dl>
      </section>

      <section className="drawer-section">
        <h3 className="drawer-section-title">{t('console.features')}</h3>
        {included.length > 0 ? (
          <ul className="plan-includes plan-includes-plain">
            {included.map((feature) => (
              <li key={feature.key} title={feature.description}>
                <Icon name="check" />
                <span>{label(feature.key, feature.description)}</span>
              </li>
            ))}
          </ul>
        ) : null}
        {missing.length > 0 ? (
          <p className="small muted">
            {t('console.notIncluded')}: {missing.map((feature) => label(feature.key, feature.description)).join(' · ')}
          </p>
        ) : null}
      </section>
    </div>
  )
}

/** The plan form, grouped: the plan, its pricing, its limits and its features. */
function PlanForm({ pkg, catalog, onCancel, onSaved }: { pkg: Package | null; catalog: CatalogFeature[]; onCancel: () => void; onSaved: () => void }) {
  const t = useT()
  const label = useFeatureLabel()
  const errors = useApiErrors()
  const currency = useDefaultCurrency()
  const limits = new Set(catalog.filter((feature) => feature.is_limit).map((feature) => feature.key))
  const [name, setName] = useState(pkg?.name ?? '')
  const [description, setDescription] = useState(pkg?.description ?? '')
  const [price, setPrice] = useState(pkg ? fromMinor(pkg.price, currency) : '')
  const [priceError, setPriceError] = useState<string>()
  // A monthly plan can also be sold yearly; empty means monthly only.
  const [yearlyPrice, setYearlyPrice] = useState(pkg?.yearly_price != null ? fromMinor(pkg.yearly_price, currency) : '')
  const [yearlyError, setYearlyError] = useState<string>()
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
  const shown = shownCatalog(catalog)
  const setFeature = (key: string, change: Partial<FeatureState>) => setFeatures((current) => ({ ...current, [key]: { ...current[key], ...change } }))

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    errors.clear()
    setPriceError(undefined)
    setYearlyError(undefined)
    const minor = price.trim() ? toMinor(price, currency) : 0
    if (minor === null) {
      setPriceError(t('inputs.amountInvalid', { example: fromMinor(150000, currency) }))
      return
    }
    const yearlyMinor = cycle === 'monthly' && yearlyPrice.trim() ? toMinor(yearlyPrice, currency) : null
    if (cycle === 'monthly' && yearlyPrice.trim() && yearlyMinor === null) {
      setYearlyError(t('inputs.amountInvalid', { example: fromMinor(1500000, currency) }))
      return
    }
    setBusy(true)
    const body = {
      name: name.trim(),
      description: description.trim() || null,
      price: minor,
      billing_cycle: cycle,
      yearly_price: yearlyMinor,
      trial_days: Number(trialDays || 0),
      sort_order: Number(order || 0),
      is_active: active,
      // Every enabled feature is sent, hidden ones (the branch limit) included, so saving keeps them.
      features: catalog
        .map((feature) => feature.key)
        .filter((key) => features[key].enabled)
        .map((key) => ({ feature_key: key, is_enabled: true, limit_value: limits.has(key) ? Number(features[key].limit || 0) : 0 })),
    }
    try {
      if (pkg) await api(`/packages/${pkg.id}`, { method: 'PATCH', asStaff: true, body })
      else await api('/packages', { method: 'POST', asStaff: true, idempotent: true, body })
      onSaved()
    } catch (cause) {
      errors.setError(cause instanceof Error ? cause : new Error(String(cause)))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form onSubmit={submit} className="stack">
      <section className="stack">
        <h3 className="drawer-section-title">{t('console.sectionPlan')}</h3>
        <Field label={t('console.planName')} error={errors.field('name')}>
          <input className="input" required value={name} onChange={(e) => setName(e.target.value)} />
        </Field>
        <Field label={`${t('console.planDescription')} (${t('form.optional')})`} error={errors.field('description')}>
          <input className="input" value={description} onChange={(e) => setDescription(e.target.value)} />
        </Field>
        <div className="form-grid-2">
          <Field label={t('console.order')} hint={t('console.orderHint')} error={errors.field('sort_order')}>
            <input className="input" type="number" value={order} onChange={(e) => setOrder(e.target.value)} />
          </Field>
          <Field label={t('members.colStatus')} hint={t('console.offeredHint')}>
            <label className="flex">
              <button type="button" role="switch" aria-checked={active} className={`switch ${active ? 'on' : ''}`} onClick={() => setActive(!active)} />
              {active ? t('console.offered') : t('console.hidden')}
            </label>
          </Field>
        </div>
      </section>

      <section className="drawer-section">
        <h3 className="drawer-section-title">{t('console.sectionPricing')}</h3>
        <div className="form-grid-2">
          <Field label={t('console.cycle')} error={errors.field('billing_cycle')}>
            <Select
              value={cycle}
              ariaLabel={t('console.cycle')}
              onChange={setCycle}
              options={[
                { value: 'monthly', label: t('console.monthly') },
                { value: 'yearly', label: t('console.yearly') },
                { value: 'one_time', label: t('console.oneTime') },
              ]}
            />
          </Field>
          <Field label={t('console.trialDays')} error={errors.field('trial_days')}>
            <input className="input" type="number" min="0" value={trialDays} onChange={(e) => setTrialDays(e.target.value)} />
          </Field>
          <Field label={cycle === 'monthly' ? t('console.monthlyPrice') : t('console.price')} error={priceError ?? errors.field('price')}>
            <MoneyInput currency={currency} ariaLabel={t('console.price')} value={price} onChange={setPrice} />
          </Field>
          {cycle === 'monthly' ? (
            <Field label={`${t('console.yearlyPrice')} (${t('form.optional')})`} hint={t('console.yearlyPriceHint')} error={yearlyError ?? errors.field('yearly_price')}>
              <MoneyInput currency={currency} ariaLabel={t('console.yearlyPrice')} value={yearlyPrice} onChange={setYearlyPrice} />
            </Field>
          ) : null}
        </div>
      </section>

      <section className="drawer-section">
        <h3 className="drawer-section-title">{t('console.sectionLimits')}</h3>
        <p className="small muted">{t('console.limitHint')}</p>
        <div className="plan-feature-list">
          {shown
            .filter((feature) => feature.is_limit)
            .map(({ key, description }) => (
              <div key={key} className="plan-feature-row" title={description}>
                <label className="check-row">
                  <input type="checkbox" checked={features[key].enabled} onChange={(e) => setFeature(key, { enabled: e.target.checked })} />
                  <span>{label(key, description)}</span>
                </label>
                {features[key].enabled ? (
                  <input
                    className="input plan-limit"
                    type="number"
                    min="-1"
                    aria-label={label(key, description)}
                    value={features[key].limit}
                    onChange={(e) => setFeature(key, { limit: e.target.value })}
                  />
                ) : null}
              </div>
            ))}
        </div>
      </section>

      <section className="drawer-section">
        <h3 className="drawer-section-title">{t('console.features')}</h3>
        <div className="plan-feature-list">
          {shown
            .filter((feature) => !feature.is_limit)
            .map(({ key, description }) => (
              <div key={key} className="plan-feature-row" title={description}>
                <label className="check-row">
                  <input type="checkbox" checked={features[key].enabled} onChange={(e) => setFeature(key, { enabled: e.target.checked })} />
                  <span>{label(key, description)}</span>
                </label>
              </div>
            ))}
        </div>
      </section>

      {errors.box}
      <div className="flex wrap">
        <Button variant="primary" type="submit" disabled={busy || name.trim().length < 2}>
          {busy ? <Spinner /> : <Icon name="check" />} {t('console.save')}
        </Button>
        <Button type="button" disabled={busy} onClick={onCancel}>
          {t('common.cancel')}
        </Button>
      </div>
    </form>
  )
}
