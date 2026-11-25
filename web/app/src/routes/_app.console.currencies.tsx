import { createFileRoute } from '@tanstack/react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'
import { api, type Schemas } from '../api/client'
import { can, useSession } from '../auth/session'
import { Field, useApiErrors } from '../components/form'
import { Select } from '../components/inputs'
import { TableSkeleton } from '../components/Skeleton'
import { useToast } from '../components/Toasts'
import { Badge, Button, Card, ConfirmDialog, Drawer, EmptyState, Icon, PageHeader, Spinner } from '../components/ui'
import { useLocale, useT } from '../i18n'
import { currenciesQuery, formatMoney } from '../money'

export const Route = createFileRoute('/_app/console/currencies')({
  component: CurrenciesPage,
})

type Currency = Schemas['Currency']

const listQuery = { queryKey: ['console', 'currencies'], queryFn: () => api<Currency[]>('/currencies', { asStaff: true }) }

/** 1,234.50 written the way the currency says. */
const EXAMPLE = 123450

/** Makes a currency the default; refreshes the staff list and the app's currencies. */
function useMakeDefault() {
  const t = useT()
  const client = useQueryClient()
  const { push } = useToast()
  const [busy, setBusy] = useState<number | null>(null)
  const run = async (currency: Currency) => {
    setBusy(currency.id)
    try {
      await api(`/currencies/${currency.id}/default`, { method: 'POST', asStaff: true })
      await client.invalidateQueries({ queryKey: listQuery.queryKey })
      await client.invalidateQueries({ queryKey: currenciesQuery.queryKey })
      push(t('currencies.nowDefault', { code: currency.code }), 'success')
      return true
    } catch (cause) {
      push(cause instanceof Error ? cause.message : String(cause), 'error')
      return false
    } finally {
      setBusy(null)
    }
  }
  return { run, busy }
}

function CurrenciesPage() {
  const t = useT()
  const locale = useLocale()
  const session = useSession()
  const currencies = useQuery(listQuery)
  const makeDefault = useMakeDefault()
  const [editing, setEditing] = useState<Currency | 'new' | null>(null)
  const canUpdate = can(session, 'currency.update')

  return (
    <>
      <PageHeader
        title={t('currencies.title')}
        description={t('currencies.subtitle')}
        actions={
          can(session, 'currency.create') ? (
            <Button variant="primary" icon="plus" onClick={() => setEditing('new')}>
              {t('currencies.new')}
            </Button>
          ) : null
        }
      />
      <Card flush>
        {!currencies.data ? (
          <TableSkeleton columns={4} avatar={false} />
        ) : currencies.data.length === 0 ? (
          <EmptyState icon="coins" title={t('currencies.none')} />
        ) : (
          <div className="table-wrap">
            <table className="table">
              <thead>
                <tr>
                  <th>{t('currencies.colCurrency')}</th>
                  <th>{t('currencies.colExample')}</th>
                  <th>{t('currencies.colDecimals')}</th>
                  <th>{t('currencies.colStatus')}</th>
                  <th className="end">
                    <span className="sr-only">{t('currencies.colActions')}</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {currencies.data.map((currency) => (
                  <tr key={currency.id} className="clickable" onClick={() => setEditing(currency)}>
                    <td>
                      <span className="member-meta">
                        <span className="member-name">{currency.code}</span>
                        <span className="small muted">{currency.name}</span>
                      </span>
                    </td>
                    <td className="nowrap" dir="ltr">
                      {formatMoney(EXAMPLE, currency, locale)}
                    </td>
                    <td>{currency.precision}</td>
                    <td>
                      <span className="flex wrap">
                        {currency.is_default ? <Badge tone="ok">{t('currencies.default')}</Badge> : null}
                        {currency.is_enabled ? null : <Badge tone="neutral">{t('currencies.disabled')}</Badge>}
                        {currency.in_use ? <Badge tone="neutral">{t('currencies.inUse')}</Badge> : null}
                      </span>
                    </td>
                    <td className="end nowrap">
                      {canUpdate && !currency.is_default && currency.is_enabled ? (
                        <Button
                          size="sm"
                          variant="ghost"
                          disabled={makeDefault.busy !== null}
                          onClick={(event) => {
                            event.stopPropagation()
                            void makeDefault.run(currency)
                          }}
                        >
                          {makeDefault.busy === currency.id ? <Spinner /> : null} {t('currencies.makeDefault')}
                        </Button>
                      ) : null}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>
      <p className="small muted">{t('currencies.noConversion')}</p>
      {editing ? <CurrencyDrawer currency={editing === 'new' ? null : editing} onClose={() => setEditing(null)} /> : null}
    </>
  )
}

const separators = [',', '.', ' ', "'", '']

function CurrencyDrawer({ currency, onClose }: { currency: Currency | null; onClose: () => void }) {
  const t = useT()
  const locale = useLocale()
  const session = useSession()
  const client = useQueryClient()
  const { push } = useToast()
  const errors = useApiErrors()
  const editable = currency ? can(session, 'currency.update') : can(session, 'currency.create')
  const [code, setCode] = useState(currency?.code ?? '')
  const [name, setName] = useState(currency?.name ?? '')
  const [symbol, setSymbol] = useState(currency?.symbol ?? '')
  const [symbolFirst, setSymbolFirst] = useState(currency?.symbol_first ?? true)
  const [precision, setPrecision] = useState(String(currency?.precision ?? 2))
  const [decimalMark, setDecimalMark] = useState(currency?.decimal_mark ?? '.')
  const [thousands, setThousands] = useState(currency?.thousands_separator ?? ',')
  const [enabled, setEnabled] = useState(currency?.is_enabled ?? true)
  const [busy, setBusy] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const makeDefault = useMakeDefault()

  const preview = {
    code: code || '—',
    name,
    symbol: symbol || code || '¤',
    symbol_first: symbolFirst,
    precision: Number(precision),
    decimal_mark: decimalMark,
    thousands_separator: thousands,
  }
  const separatorLabel = (value: string) =>
    value === ' ' ? t('currencies.space') : value === '' ? t('currencies.noSeparator') : value === "'" ? `' (${t('currencies.apostrophe')})` : value

  const refresh = async () => {
    await client.invalidateQueries({ queryKey: listQuery.queryKey })
    await client.invalidateQueries({ queryKey: currenciesQuery.queryKey })
  }

  const run = async (request: () => Promise<unknown>, close = true) => {
    setBusy(true)
    errors.clear()
    try {
      await request()
      await refresh()
      push(t('form.saved'), 'success')
      if (close) onClose()
    } catch (cause) {
      errors.setError(cause instanceof Error ? cause : new Error(String(cause)))
    } finally {
      setBusy(false)
    }
  }

  const submit = (event: FormEvent) => {
    event.preventDefault()
    const body = {
      name: name.trim(),
      symbol: symbol.trim(),
      symbol_first: symbolFirst,
      precision: Number(precision),
      decimal_mark: decimalMark,
      thousands_separator: thousands,
      is_enabled: enabled,
    }
    void run(() =>
      currency
        ? api(`/currencies/${currency.id}`, { method: 'PATCH', asStaff: true, body })
        : api('/currencies', { method: 'POST', asStaff: true, idempotent: true, body: { ...body, code: code.trim().toUpperCase() } }),
    )
  }

  return (
    <Drawer open title={currency ? `${currency.code} · ${currency.name}` : t('currencies.new')} onClose={onClose}>
      <form onSubmit={submit} className="stack">
        {currency ? (
          <div className={`currency-status ${currency.is_default ? 'is-default' : ''}`}>
            {currency.is_default ? (
              <>
                <Badge tone="ok">{t('currencies.default')}</Badge>
                <span className="small">{t('currencies.isDefault')}</span>
              </>
            ) : (
              <>
                <span className="small muted">{currency.is_enabled ? t('currencies.makeDefaultHint') : t('currencies.enableFirst')}</span>
                {can(session, 'currency.update') && currency.is_enabled ? (
                  <Button
                    size="sm"
                    type="button"
                    disabled={makeDefault.busy !== null || busy}
                    onClick={() => void makeDefault.run(currency).then((done) => done && onClose())}
                  >
                    {makeDefault.busy === currency.id ? <Spinner /> : <Icon name="check" />} {t('currencies.makeDefault')}
                  </Button>
                ) : null}
              </>
            )}
          </div>
        ) : null}
        <div className="form-grid-2">
          <Field label={t('currencies.code')} hint={currency ? undefined : t('currencies.codeHint')} error={errors.field('code')}>
            <input
              className="input mono"
              required
              maxLength={3}
              dir="ltr"
              disabled={!!currency || !editable}
              value={code}
              onChange={(e) => setCode(e.target.value.toUpperCase())}
            />
          </Field>
          <Field label={t('currencies.name')} error={errors.field('name')}>
            <input className="input" required maxLength={100} disabled={!editable} value={name} onChange={(e) => setName(e.target.value)} />
          </Field>
        </div>
        <div className="form-grid-2">
          <Field label={t('currencies.symbol')} error={errors.field('symbol')}>
            <input className="input" required maxLength={10} dir="ltr" disabled={!editable} value={symbol} onChange={(e) => setSymbol(e.target.value)} />
          </Field>
          <Field label={t('currencies.position')} error={errors.field('symbol_first')}>
            <Select
              value={symbolFirst ? 'before' : 'after'}
              disabled={!editable}
              ariaLabel={t('currencies.position')}
              onChange={(value) => setSymbolFirst(value === 'before')}
              options={[
                { value: 'before', label: t('currencies.before') },
                { value: 'after', label: t('currencies.after') },
              ]}
            />
          </Field>
        </div>
        <div className="form-grid-3">
          <Field
            label={t('currencies.decimals')}
            hint={currency?.in_use ? t('currencies.decimalsLocked') : undefined}
            error={errors.field('precision')}
          >
            <Select
              value={precision}
              disabled={!editable || currency?.in_use}
              ariaLabel={t('currencies.decimals')}
              onChange={setPrecision}
              options={['0', '1', '2', '3', '4'].map((value) => ({ value, label: value }))}
            />
          </Field>
          <Field label={t('currencies.decimalMark')} error={errors.field('decimal_mark')}>
            <Select
              value={decimalMark}
              disabled={!editable}
              ariaLabel={t('currencies.decimalMark')}
              onChange={setDecimalMark}
              options={['.', ','].map((value) => ({ value, label: value }))}
            />
          </Field>
          <Field label={t('currencies.thousands')} error={errors.field('thousands_separator')}>
            <Select
              value={thousands}
              disabled={!editable}
              ariaLabel={t('currencies.thousands')}
              onChange={setThousands}
              options={separators.map((value) => ({ value, label: separatorLabel(value) }))}
            />
          </Field>
        </div>
        <div className="currency-preview">
          <span className="small muted">{t('currencies.preview')}</span>
          <span className="currency-preview-value" dir="ltr">
            {formatMoney(EXAMPLE, preview, locale)}
          </span>
        </div>
        <label className="flex">
          <button
            type="button"
            role="switch"
            aria-checked={enabled}
            className={`switch ${enabled ? 'on' : ''}`}
            disabled={!editable || currency?.is_default}
            onClick={() => setEnabled(!enabled)}
          />
          {t('currencies.enabled')}
        </label>
        {currency?.is_default ? <p className="small muted">{t('currencies.defaultHint')}</p> : null}
        {errors.box}
        {editable ? (
          <div className="flex wrap">
            <Button variant="primary" type="submit" disabled={busy || name.trim().length === 0 || code.trim().length !== 3}>
              {busy ? <Spinner /> : <Icon name="check" />} {t('console.save')}
            </Button>
            <span className="spacer" />
            {currency && !currency.is_default && !currency.in_use && can(session, 'currency.delete') ? (
              <Button variant="danger" icon="trash" type="button" disabled={busy} onClick={() => setConfirmDelete(true)}>
                {t('currencies.delete')}
              </Button>
            ) : null}
          </div>
        ) : null}
      </form>
      <ConfirmDialog
        open={confirmDelete}
        title={t('currencies.delete')}
        description={t('currencies.deleteConfirm', { code: currency?.code ?? '' })}
        confirmLabel={t('currencies.delete')}
        danger
        busy={busy}
        onConfirm={() => {
          setConfirmDelete(false)
          if (currency) void run(() => api(`/currencies/${currency.id}`, { method: 'DELETE', asStaff: true }))
        }}
        onCancel={() => setConfirmDelete(false)}
      />
    </Drawer>
  )
}
