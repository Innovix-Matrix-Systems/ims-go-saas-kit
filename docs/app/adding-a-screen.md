---
title: Adding a screen
description: Give a module a page in the app — the typed API types, the route file, the list and the editor, the sidebar and Ctrl+K entry, permissions in the UI, and both languages.
layers: [app]
---

A screen is a route file that reads and writes one API resource. This page builds the **Products**
screen for the module from the [first-module tutorial](../tutorial/first-module.mdx); the same
steps work for any resource. The Branches screen (`web/app/src/routes/_app.org.branches.tsx`) is
the reference to copy from: a searchable, paged list and a drawer that creates, edits and
deletes.

Run `make dev` and open **http://localhost:8082** while you work: the screen reloads as you save.

## 1. Generate the API types

The app's types come from the OpenAPI contract, so a screen can't call a field the API doesn't
have. After any change to `api/openapi.yaml`:

```bash
npm --prefix web/app run openapi:gen
```

This rewrites `web/app/src/api/schema.d.ts`. The module's schemas are now available as
`Schemas['Product']`, `Schemas['ProductUpsertRequest']` and so on. CI fails if the file is
stale.

## 2. Create the route file

Routes are files. The name is the path, with `.` for `/` and a leading `_app.` for pages inside
the signed-in shell:

| File | Path |
| --- | --- |
| `routes/_app.org.products.tsx` | `/org/products` (an organization page) |
| `routes/_app.console.plans.tsx` | `/console/plans` (a staff console page) |
| `routes/_app.jobs.tsx` | `/jobs` (for everyone signed in) |

The router plugin regenerates `src/routeTree.gen.ts` when the dev server or the build sees a new
file; commit it with your screen.

```tsx title="web/app/src/routes/_app.org.products.tsx"
import { createFileRoute } from '@tanstack/react-router'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { api, type Schemas } from '../api/client'
import { can, useSession } from '../auth/session'
import { Pagination } from '../components/Pagination'
import { TableSkeleton } from '../components/Skeleton'
import { Button, Card, EmptyState, PageHeader } from '../components/ui'
import { useT } from '../i18n'
import { useMoney } from '../components/billing'

export const Route = createFileRoute('/_app/org/products')({
  component: ProductsPage,
})

type Product = Schemas['Product']
interface Page {
  data: Product[]
  meta: Schemas['PaginationMeta']
}

function ProductsPage() {
  const t = useT()
  const session = useSession()
  const money = useMoney() // 1250 → "৳12.50", formatted for the chosen language
  const [search, setSearch] = useState('')
  const [query, setQuery] = useState('')
  const [page, setPage] = useState(1)
  const [editing, setEditing] = useState<Product | 'new' | null>(null)

  // Search after the user stops typing for 300 ms.
  useEffect(() => {
    const id = window.setTimeout(() => {
      setQuery(search.trim())
      setPage(1)
    }, 300)
    return () => window.clearTimeout(id)
  }, [search])

  const products = useQuery({
    queryKey: ['products', query, page],
    queryFn: () => api<Page>('/product', { envelope: true, query: { search: query, page, per_page: 20 } }),
    placeholderData: keepPreviousData,
  })

  return (
    <>
      <PageHeader
        title={t('products.title')}
        description={t('products.subtitle')}
        actions={
          can(session, 'product.create') ? (
            <Button variant="primary" icon="plus" onClick={() => setEditing('new')}>
              {t('products.create')}
            </Button>
          ) : null
        }
      />
      <div className="toolbar">
        <input className="input toolbar-search" type="search" placeholder={t('products.search')}
          value={search} onChange={(e) => setSearch(e.target.value)} />
      </div>
      <Card flush>
        {products.isLoading ? (
          <TableSkeleton columns={4} avatar={false} />
        ) : (products.data?.data ?? []).length === 0 ? (
          <EmptyState icon="package" title={t('products.empty')} />
        ) : (
          <div className="table-wrap">
            <table className="table">
              <thead>
                <tr>
                  <th>{t('products.name')}</th>
                  <th>{t('products.sku')}</th>
                  <th className="num">{t('products.price')}</th>
                  <th className="num">{t('products.stock')}</th>
                </tr>
              </thead>
              <tbody>
                {products.data!.data.map((product) => (
                  <tr key={product.id} className="clickable" onClick={() => setEditing(product)}>
                    <td>{product.name}</td>
                    <td className="mono small">{product.sku}</td>
                    <td className="num">{money(product.price)}</td>
                    <td className="num">{product.stock}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        {products.data ? (
          <Pagination page={page} perPage={20} total={products.data.meta.total} onPage={setPage} />
        ) : null}
      </Card>
      {editing ? <ProductDrawer product={editing === 'new' ? null : editing} onClose={() => setEditing(null)} /> : null}
    </>
  )
}
```

What the pieces do:

- **`queryKey`** names the data. Anything that changes the products list invalidates
  `['products']`, and every screen showing it refreshes.
- **`envelope: true`** keeps `meta` (the total for paging). Without it, `api()` returns `data`.
- **`keepPreviousData`** keeps the old page on screen while the next one loads, so the table
  doesn't flash.
- **Loading, empty, data**: every list shows a skeleton, then an empty state, then rows. Use the
  shared `TableSkeleton` and `EmptyState` so screens look alike.
- Use the shared formatters before writing your own: `useMoney()` (`src/components/billing.tsx`)
  for prices, and `src/format.ts` for dates, durations and sizes.

## 3. The editor drawer

Creating and editing happen in a `Drawer` with a form. Field errors from the API (a 422) appear
under the right field through `useApiErrors()`:

```tsx title="web/app/src/routes/_app.org.products.tsx (continued)"
import { useQueryClient } from '@tanstack/react-query'
import { type FormEvent } from 'react'
import { Field, useApiErrors } from '../components/form'
import { useToast } from '../components/Toasts'
import { Drawer, Icon, Spinner } from '../components/ui'

function ProductDrawer({ product, onClose }: { product: Product | null; onClose: () => void }) {
  const t = useT()
  const session = useSession()
  const client = useQueryClient()
  const { push } = useToast()
  const errors = useApiErrors()
  const editable = product ? can(session, 'product.update') : can(session, 'product.create')
  const [name, setName] = useState(product?.name ?? '')
  const [sku, setSku] = useState(product?.sku ?? '')
  const [price, setPrice] = useState(product ? String(product.price / 100) : '')
  const [stock, setStock] = useState(product ? String(product.stock) : '0')
  const [busy, setBusy] = useState(false)

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    setBusy(true)
    errors.clear()
    // Money travels in minor units: 12.50 is 1250.
    const body = { name: name.trim(), sku: sku.trim(), price: Math.round(Number(price) * 100), stock: Number(stock), is_active: true }
    try {
      if (product) await api(`/product/${product.id}`, { method: 'PATCH', body })
      else await api('/product', { method: 'POST', idempotent: true, body })
      await client.invalidateQueries({ queryKey: ['products'] })
      push(t('form.saved'), 'success')
      onClose()
    } catch (cause) {
      errors.setError(cause instanceof Error ? cause : new Error(String(cause)))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Drawer open title={product ? product.name : t('products.create')} onClose={onClose} size="md">
      <form onSubmit={submit} className="stack">
        <Field label={t('products.name')} error={errors.field('name')}>
          <input className="input" required disabled={!editable} value={name} onChange={(e) => setName(e.target.value)} />
        </Field>
        <Field label={t('products.sku')} error={errors.field('sku')}>
          <input className="input mono" required disabled={!editable} value={sku} onChange={(e) => setSku(e.target.value)} />
        </Field>
        <div className="form-grid-2">
          <Field label={t('products.price')} error={errors.field('price')}>
            <input className="input" type="number" min="0" step="0.01" disabled={!editable} value={price} onChange={(e) => setPrice(e.target.value)} />
          </Field>
          <Field label={t('products.stock')} error={errors.field('stock')}>
            <input className="input" type="number" min="0" step="1" disabled={!editable} value={stock} onChange={(e) => setStock(e.target.value)} />
          </Field>
        </div>
        {errors.box}
        {editable ? (
          <Button variant="primary" type="submit" disabled={busy}>
            {busy ? <Spinner /> : <Icon name="check" />} {t('products.save')}
          </Button>
        ) : null}
      </form>
    </Drawer>
  )
}
```

- **`idempotent: true`** on the create sends an `Idempotency-Key`, so a double click or a retried
  request creates one product, not two.
- **`errors.field('sku')`** shows the service's "This SKU is already in use." under the SKU input;
  `errors.box` shows any error that isn't about one field (a 402 plan limit, a 403).
- Add a delete button behind `can(session, 'product.delete')` with a `ConfirmDialog`, as the
  Branches drawer does.

## 4. Add it to the sidebar and Ctrl+K

Every signed-in page is listed once in `src/components/navigation.ts`. The sidebar, the Ctrl+K
palette, the shortcuts sheet and the top bar title all read that list:

```ts title="web/app/src/components/navigation.ts"
{ to: '/org/products', label: 'products.title', icon: 'package', group: 'organization', key: 't',
  keywords: 'catalogue inventory sku stock', permission: 'product.view', tenantOnly: true },
```

| Field | Meaning |
| --- | --- |
| `group` | sidebar section: `workspace`, `console`, `organization` or `account` |
| `key` | `g` then this letter opens the page; a duplicate letter fails in development |
| `permission` | the page is listed only when the session holds it |
| `tenantOnly` / `platformOnly` | organization pages, or staff console pages |
| `keywords` | extra words the palette matches |
| `hidden` | reachable from the palette but not listed in the sidebar |

## 5. Permissions in the UI

`can(session, 'product.create')` answers from the permissions loaded with the session (a
Super-Admin passes everything). Use it to **hide** what the user can't do: a create button, a
delete action, an editable form for someone who may only read.

The API is the security boundary, not the UI: it checks the same permission on every call. Hiding
a button is about not offering an action that will fail.

## 6. Write the text in both languages

Every string comes from the catalog through `t()`. Add your keys to the English reference, then to
Bangla; TypeScript fails the build if a key is missing from any language:

```ts title="web/app/src/i18n/en.ts"
'products.title': 'Products',
'products.subtitle': 'Your catalogue: names, SKUs, prices and stock.',
'products.create': 'New product',
'products.search': 'Search by name or SKU',
'products.empty': 'No products yet.',
'products.name': 'Name',
'products.sku': 'SKU',
'products.price': 'Price',
'products.stock': 'In stock',
'products.save': 'Save product',
```

```ts title="web/app/src/i18n/bn.ts"
'products.title': 'পণ্য',
'products.subtitle': 'আপনার ক্যাটালগ: নাম, SKU, দাম ও স্টক।',
'products.create': 'নতুন পণ্য',
// … every key from en.ts
```

See [Translations and theming](./translations-and-theming.md) for variables, plural-free wording
and adding a language.

## 7. Check it

```bash
npm --prefix web/app run typecheck     # types, including every translation key
npm --prefix web/app run check:style   # contrast, type scale, no raw colours
npm --prefix web/app run build         # what the release embeds
```

Sign in at http://localhost:8082 as an organization admin and open **Products** (or press `g t`).
A member whose role lacks `product.view` doesn't see the page in the sidebar, and the API refuses
the call if they type the address.

## Patterns to reuse

| Need | Use |
| --- | --- |
| a table with sorting and column choice | `DataTable` (`src/components/DataTable.tsx`) |
| a dropdown, date or time input | `Select`, `DatePicker`, `TimePicker` (`src/components/inputs.tsx`) |
| a confirmation before a destructive action | `ConfirmDialog` (`src/components/ui.tsx`) |
| a number with a trend | `KpiCard`, `Sparkline`, `TrendChart` (`src/components/charts.tsx`) |
| an export that runs in the background | `useStartExport()` (`src/components/jobs.tsx`): the job appears in the jobs tray |
| refresh on a live event, poll otherwise | `usePollInterval()` and a kind in `invalidates` (`src/components/realtime.ts`); see [Adding live updates](../guides/realtime.md#adding-live-updates-to-your-feature) |
| a staff screen that reads one organization | `api(path, { actingTenant: id })` |
