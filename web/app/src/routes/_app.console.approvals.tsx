import { createFileRoute } from '@tanstack/react-router'
import { keepPreviousData, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { api, type Schemas } from '../api/client'
import { can, useSession } from '../auth/session'
import { useMoney } from '../components/billing'
import { Field } from '../components/form'
import { Pagination } from '../components/Pagination'
import { useToast } from '../components/Toasts'
import { Badge, Button, Card, EmptyState, Modal, PageHeader, Spinner } from '../components/ui'
import { intlLocale, useLocale, useT, type MessageKey } from '../i18n'
import { TableSkeleton } from '../components/Skeleton'

export const Route = createFileRoute('/_app/console/approvals')({
  component: ApprovalsPage,
})

type Approval = Schemas['Approval']
type Status = Approval['status']
interface ApprovalPage {
  data: Approval[]
  meta: Schemas['PaginationMeta']
}

const statusLabels: Record<Status, MessageKey> = { pending: 'console.pending', approved: 'console.approved', rejected: 'console.rejected' }
const statusTones = { pending: 'warn', approved: 'ok', rejected: 'danger' } as const

function reference(approval: Approval): string {
  const data = approval.payment_data ?? {}
  const value = data.reference_number ?? data.reference ?? data.notes
  return typeof value === 'string' && value ? value : '—'
}

function ApprovalsPage() {
  const t = useT()
  const session = useSession()
  const money = useMoney()
  const locale = useLocale()
  const [status, setStatus] = useState<Status>('pending')
  const [page, setPage] = useState(1)
  const [perPage, setPerPage] = useState(20)
  const [review, setReview] = useState<{ approval: Approval; decision: 'approve' | 'reject' } | null>(null)
  const approvals = useQuery({
    queryKey: ['console', 'approvals', status, page, perPage],
    queryFn: () => api<ApprovalPage>('/billing/internal/payment-approvals', { asStaff: true, envelope: true, query: { status, page, per_page: perPage } }),
    placeholderData: keepPreviousData,
  })
  const when = (value: string) => new Intl.DateTimeFormat(intlLocale(locale), { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
  const canReview = can(session, 'payment_approval.review')

  return (
    <>
      <PageHeader title={t('console.approvals')} description={t('console.approvalsSubtitle')} />
      <div className="toolbar">
        <div className="segmented">
          {(['pending', 'approved', 'rejected'] as const).map((value) => (
            <button
              key={value}
              type="button"
              className={status === value ? 'active' : ''}
              onClick={() => {
                setStatus(value)
                setPage(1)
              }}
            >
              {t(statusLabels[value])}
            </button>
          ))}
        </div>
      </div>
      <Card flush>
        {approvals.isLoading ? (
          <TableSkeleton columns={6} avatar={false} />
        ) : (approvals.data?.data ?? []).length === 0 ? (
          <EmptyState icon="check" title={t('console.noApprovals')} />
        ) : (
          <div className="table-wrap">
            <table className="table">
              <thead>
                <tr>
                  <th>{t('console.colPayer')}</th>
                  <th>{t('console.colPlan')}</th>
                  <th className="end">{t('console.colAmount')}</th>
                  <th>{t('console.colReference')}</th>
                  <th>{t('console.colSubmitted')}</th>
                  <th>{t('members.colStatus')}</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {approvals.data!.data.map((approval) => (
                  <tr key={approval.id}>
                    <td>
                      <span className="member-meta">
                        <span className="member-name">{approval.details?.tenant_name || `#${approval.tenant_id}`}</span>
                        <span className="small muted mono">{approval.details?.invoice_number}</span>
                      </span>
                    </td>
                    <td className="small">{approval.details?.package_name || '—'}</td>
                    <td className="end">{approval.details ? money(approval.details.amount, approval.details.currency) : '—'}</td>
                    <td className="mono small">
                      {reference(approval)}
                      <div className="small muted">{t(`billing.method.${approval.payment_method}` as MessageKey)}</div>
                    </td>
                    <td className="small muted">{when(approval.created_at)}</td>
                    <td>
                      <Badge tone={statusTones[approval.status]}>{t(statusLabels[approval.status])}</Badge>
                      {approval.reviewer_note ? <div className="small muted">{approval.reviewer_note}</div> : null}
                    </td>
                    <td className="end">
                      {canReview && approval.status === 'pending' ? (
                        <div className="flex justify-end">
                          <Button size="sm" variant="primary" icon="check" onClick={() => setReview({ approval, decision: 'approve' })}>
                            {t('console.approve')}
                          </Button>
                          <Button size="sm" variant="ghost" icon="x" onClick={() => setReview({ approval, decision: 'reject' })}>
                            {t('console.reject')}
                          </Button>
                        </div>
                      ) : null}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        {approvals.data ? (
          <Pagination
            page={page}
            perPage={perPage}
            total={approvals.data.meta.total}
            onPage={setPage}
            onPerPage={(size) => {
              setPerPage(size)
              setPage(1)
            }}
          />
        ) : null}
      </Card>
      {review ? <ReviewDialog approval={review.approval} decision={review.decision} onClose={() => setReview(null)} /> : null}
    </>
  )
}

function ReviewDialog({ approval, decision, onClose }: { approval: Approval; decision: 'approve' | 'reject'; onClose: () => void }) {
  const t = useT()
  const money = useMoney()
  const client = useQueryClient()
  const { push } = useToast()
  const [note, setNote] = useState('')
  const [busy, setBusy] = useState(false)
  const name = approval.details?.tenant_name || `#${approval.tenant_id}`
  const amount = approval.details ? money(approval.details.amount, approval.details.currency) : ''
  const submit = async () => {
    setBusy(true)
    try {
      await api(`/billing/internal/payment-approvals/${approval.id}/${decision}`, { method: 'POST', asStaff: true, body: { note: note.trim() || null } })
      await client.invalidateQueries({ queryKey: ['console', 'approvals'] })
      push(t('form.saved'), 'success')
      onClose()
    } catch (cause) {
      push(cause instanceof Error ? cause.message : String(cause), 'error')
    } finally {
      setBusy(false)
    }
  }
  return (
    <Modal
      open
      title={decision === 'approve' ? t('console.approve') : t('console.reject')}
      description={
        decision === 'approve'
          ? t('console.approveConfirm', { amount, name, plan: approval.details?.package_name ?? '' })
          : t('console.rejectConfirm', { amount, name })
      }
      onClose={onClose}
    >
      <div className="stack">
        <Field label={t('console.note')}>
          <textarea className="input" rows={3} value={note} onChange={(e) => setNote(e.target.value)} />
        </Field>
        <div className="modal-actions">
          <Button variant="ghost" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button variant={decision === 'approve' ? 'primary' : 'danger'} disabled={busy} onClick={() => void submit()}>
            {busy ? <Spinner /> : null} {decision === 'approve' ? t('console.approve') : t('console.reject')}
          </Button>
        </div>
      </div>
    </Modal>
  )
}
