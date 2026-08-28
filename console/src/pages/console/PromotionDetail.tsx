import { useState } from 'react'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { format } from 'date-fns'
import { ar } from 'date-fns/locale'
import { toast } from 'sonner'
import { ApiError } from '@/lib/api'
import { errorMessage } from '@/lib/auth'
import { useDeletePromotion, usePromotion, usePromotionPerformance } from '@/lib/hooks'
import { PERM, useCan } from '@/lib/perm'
import {
  fmtDateOnly,
  fmtDateTime,
  promotionLevelLabel,
  promotionReachLabel,
  promotionStatusLabel,
  promotionStatusTone,
  promotionValueLabel,
  toArabicDigits,
} from '@/lib/format'
import { cn } from '@/lib/utils'
import {
  PROMOTION_SCOPE,
  PROMOTION_TARGET_KIND,
  type PromotionPerformance,
  type PromotionPerformanceDay,
} from '@/lib/types'
import { Breadcrumbs } from '@/components/Breadcrumbs'
import { Freshness } from '@/components/Freshness'
import { PeriodPicker } from '@/components/PeriodPicker'
import { LoadingState, EmptyState, ErrorState } from '@/components/States'
import { DeleteIcon, EditIcon, PromotionsIcon } from '@/components/icon'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { PromotionFormDialog } from '@/components/PromotionFormDialog'

const money = new Intl.NumberFormat('ar', { maximumFractionDigits: 2 })

export function PromotionDetail() {
  const { tenantId, promotionId } = useParams<'tenantId' | 'promotionId'>()
  const navigate = useNavigate()
  const canManage = useCan(tenantId, PERM.PromotionsManage)
  const query = usePromotion(tenantId, promotionId)
  const remove = useDeletePromotion(tenantId as string)

  // Same URL-borne period as Reports.tsx — PeriodPicker owns these two keys.
  const [searchParams] = useSearchParams()
  const from = searchParams.get('from') ?? undefined
  const to = searchParams.get('to') ?? undefined
  const perfQuery = usePromotionPerformance(tenantId, promotionId, { from, to })

  const [editing, setEditing] = useState(false)
  const [confirmingStop, setConfirmingStop] = useState(false)

  if (query.isLoading) return <LoadingState />
  if (query.error instanceof ApiError && query.error.status === 404) {
    return (
      <EmptyState
        icon={PromotionsIcon}
        title="العرض غير موجود"
        description="ربما تم إيقافه، أو أنه يخص فرعًا خارج نطاق صلاحياتك."
      />
    )
  }
  if (query.error) {
    return (
      <ErrorState
        message="تعذّر الوصول إلى بيانات العرض الآن."
        onRetry={() => void query.refetch()}
      />
    )
  }
  const p = query.data?.data
  if (!p) return <LoadingState />

  const productTargets = p.targets.filter((t) => t.kind === PROMOTION_TARGET_KIND.Product)
  const groupTargets = p.targets.filter((t) => t.kind === PROMOTION_TARGET_KIND.Group)

  async function stop() {
    try {
      await remove.mutateAsync(promotionId as string)
      toast.success('تم إيقاف العرض')
      navigate(`/tenants/${tenantId}/promotions`)
    } catch (err) {
      toast.error(errorMessage(err))
    }
  }

  return (
    <>
      <Breadcrumbs
        items={[
          { label: 'العروض والخصومات', to: `/tenants/${tenantId}/promotions` },
          { label: p.name },
        ]}
      />

      <div className="mb-5 flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          <h1 className="font-display text-2xl font-bold">{p.name}</h1>
          <Badge tone={promotionStatusTone(p.status)}>{promotionStatusLabel(p.status)}</Badge>
        </div>
        <div className="flex items-center gap-2">
          {query.data && <Freshness source={query.data.source} asOf={query.data.as_of} />}
          {/* Write affordances are absent, not disabled, for a view-only
              member — a greyed button still advertises an action they cannot
              take and invites a support question. */}
          {canManage && (
            <>
              <Button variant="outline" onClick={() => setEditing(true)}>
                <EditIcon className="size-4" />
                تعديل
              </Button>
              <Button variant="outline" onClick={() => setConfirmingStop(true)}>
                <DeleteIcon className="size-4" />
                إيقاف
              </Button>
            </>
          )}
        </div>
      </div>

      <Card className="space-y-3 p-4">
        <h2 className="font-display text-sm font-semibold text-muted-foreground">الإعدادات</h2>
        <dl className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          <Field label="المستوى" value={promotionLevelLabel(p.level)} />
          <Field label="الخصم" value={promotionValueLabel(p.discount_type, p.value)} />
          <Field
            label="النطاق"
            value={promotionReachLabel(p.branch_name)}
            hint={p.branch_id ? undefined : 'يُطبَّق في كل الفروع'}
          />
          <Field
            label="الفترة"
            value={`${fmtDateOnly(p.starts_on)} — ${fmtDateOnly(p.ends_on)}`}
            hint="التاريخان شاملان"
          />
          {p.min_qty != null && <Field label="حد أدنى للكمية" value={toArabicDigits(p.min_qty)} />}
          {p.min_bill_total != null && (
            <Field label="حد أدنى للفاتورة" value={toArabicDigits(p.min_bill_total)} />
          )}
          <Field label="أُنشئ في" value={fmtDateTime(p.created_at)} />
        </dl>
      </Card>

      <PerformancePanel className="mt-4" query={perfQuery} />

      <Card className="mt-4 space-y-3 p-4">
        <h2 className="font-display text-sm font-semibold text-muted-foreground">الأصناف</h2>
        {p.scope === PROMOTION_SCOPE.AllProducts ? (
          <p className="text-sm">
            كل الأصناف
            <span className="ms-2 text-xs text-muted-foreground">
              (لا توجد أصناف محددة — العرض يشمل الكتالوج بالكامل)
            </span>
          </p>
        ) : (
          <ul className="space-y-1.5 text-sm">
            {[...groupTargets, ...productTargets].map((t) => (
              <li key={t.ref_id} className="flex items-center gap-2">
                <Badge tone="muted">
                  {t.kind === PROMOTION_TARGET_KIND.Group ? 'مجموعة' : 'صنف'}
                </Badge>
                {/* A name that no longer resolves comes back null from the
                    server — the row is still listed, showing its id, so the
                    author can see and remove a target whose product was
                    deleted rather than wondering why a count is off. */}
                <span className={t.name ? undefined : 'text-muted-foreground'}>
                  {t.name ?? t.ref_id}
                </span>
              </li>
            ))}
          </ul>
        )}
        {p.scope === PROMOTION_SCOPE.Groups && groupTargets.length > 0 && (
          <p className="text-xs text-muted-foreground">
            يشمل العرض كل المجموعات الفرعية أسفل المجموعات المختارة، بما فيها ما يُضاف لاحقًا.
          </p>
        )}
      </Card>

      {editing && tenantId && (
        <PromotionFormDialog
          tenantId={tenantId}
          existing={p}
          onClose={() => setEditing(false)}
        />
      )}

      <Dialog open={confirmingStop} onOpenChange={setConfirmingStop}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>إيقاف العرض</DialogTitle>
          </DialogHeader>
          {/* D9: this is a soft delete. The wording says the promotion stops
              applying and its history is kept — never "delete permanently",
              which would be false and would make the operator hesitate over a
              reversible action. */}
          <p className="text-sm text-muted-foreground">
            سيتوقف تطبيق «{p.name}» على الفواتير الجديدة فورًا. يظل سجل استخدامه السابق محفوظًا
            للمراجعة والتقارير.
          </p>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setConfirmingStop(false)}>
              إلغاء
            </Button>
            <Button onClick={() => void stop()} disabled={remove.isPending}>
              إيقاف العرض
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}

function Field({ label, value, hint }: { label: string; value: string; hint?: string }) {
  return (
    <div>
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="mt-0.5 text-sm font-medium">
        {value}
        {hint && <span className="ms-2 text-xs font-normal text-muted-foreground">{hint}</span>}
      </dd>
    </div>
  )
}

// --- الأداء (T151-T153) ------------------------------------------------------

function KpiTile({ label, value, hint }: { label: string; value: string; hint?: string }) {
  return (
    <div className="rounded-lg border border-border bg-card/50 p-3">
      <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
        {label}
      </div>
      <div className="mt-1 font-display text-lg font-bold">{value}</div>
      {hint && <div className="mt-0.5 text-xs text-muted-foreground">{hint}</div>}
    </div>
  )
}

/** A YYYY-MM-DD local-date string as a Date, without a UTC shift — same
 * helper Reports.tsx and Customers.tsx each keep their own copy of. */
function parseDay(day: string): Date {
  return new Date(`${day}T00:00:00`)
}

/** Daily promotional-amount bars — same CSS-flex-bar construction as
 * Reports' SalesChart (T46): theme tokens apply directly, no SVG/chart
 * dependency, and the by-day table isn't duplicated since the chart's own
 * tooltip already carries the exact figures per day. */
function PerformanceChart({ days }: { days: PromotionPerformanceDay[] }) {
  const max = Math.max(...days.map((d) => d.total_amount), 0)
  if (max <= 0) {
    return (
      <div className="py-8 text-center text-sm text-muted-foreground">
        لم يُستخدم هذا العرض في هذه الفترة.
      </div>
    )
  }
  const labelStep = Math.ceil(days.length / 8)
  const peak = days.reduce((a, b) => (b.total_amount > a.total_amount ? b : a), days[0])

  return (
    <div dir="ltr">
      <div className="flex h-40 items-end gap-px border-b border-border sm:gap-0.5">
        {days.map((d) => (
          <div
            key={d.day}
            className="group flex h-full min-w-0 flex-1 flex-col items-center justify-end"
            title={`${format(parseDay(d.day), 'EEEE d MMM', { locale: ar })} — ${money.format(d.total_amount)} (${toArabicDigits(d.bills_count)} فاتورة)`}
          >
            {d === peak && (
              <span className="mb-0.5 hidden truncate text-[10px] font-medium text-muted-foreground sm:block">
                {money.format(d.total_amount)}
              </span>
            )}
            <div
              className="w-full max-w-6 rounded-t-[4px] bg-primary/75 transition-colors group-hover:bg-primary"
              style={{ height: `${Math.max((d.total_amount / max) * 100, d.total_amount > 0 ? 2 : 0)}%` }}
            />
          </div>
        ))}
      </div>
      <div className="mt-1 flex gap-px sm:gap-0.5">
        {days.map((d, i) => (
          <div key={d.day} className="min-w-0 flex-1 text-center text-[10px] text-muted-foreground">
            {i % labelStep === 0 ? (
              <span className="truncate">{format(parseDay(d.day), 'd/M', { locale: ar })}</span>
            ) : null}
          </div>
        ))}
      </div>
    </div>
  )
}

function PerformancePanel({
  query,
  className,
}: {
  query: ReturnType<typeof usePromotionPerformance>
  className?: string
}) {
  const notSubscribed = query.error instanceof ApiError && query.error.status === 402
  const gatewayError = query.error instanceof ApiError && query.error.status !== 402

  return (
    <Card className={cn('space-y-3 p-4', className)}>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="font-display text-sm font-semibold text-muted-foreground">الأداء</h2>
        <div className="flex flex-wrap items-center gap-3">
          <PeriodPicker />
          {query.data && <Freshness source={query.data.source} asOf={query.data.as_of} />}
        </div>
      </div>

      {notSubscribed ? (
        <p className="py-4 text-center text-sm text-muted-foreground">
          فعّل اشتراك المزامنة لعرض أداء هذا العرض.
        </p>
      ) : gatewayError ? (
        <ErrorState
          message="تعذّر الوصول إلى بيانات الأداء الآن."
          onRetry={() => void query.refetch()}
        />
      ) : query.isLoading || !query.data ? (
        <LoadingState rows={3} />
      ) : (
        <PerformanceBody data={query.data.data} />
      )}
    </Card>
  )
}

function PerformanceBody({ data: r }: { data: PromotionPerformance }) {
  // The reconciliation invariant (spec D6), read at report time rather than
  // stored: bills_discount_total is Σ Invoice.ItemDiscount/BillDiscount over
  // the WHOLE column on exactly the bills this promotion touched, not this
  // promotion's own slice of it — so the shortfall against total_amount is
  // every OTHER manual entry (or other promotion) on those same bills. This
  // promotion's own rows can never be part of that shortfall themselves:
  // spec D10 (no cashier override) plus D11 (manual and promotional never
  // share a line or bill) already rule that out.
  const manual = Math.max(r.bills_discount_total - r.total_amount, 0)
  const promoShare =
    r.bills_discount_total > 0
      ? Math.round((r.total_amount / r.bills_discount_total) * 100)
      : r.total_amount > 0
        ? 100
        : 0

  return (
    <div>
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <KpiTile label="الفواتير" value={toArabicDigits(r.bills_count)} />
        <KpiTile label="الأصناف المتأثرة" value={toArabicDigits(r.items_count)} />
        <KpiTile label="الخصم الترويجي" value={money.format(r.total_amount)} />
        <KpiTile
          label="خصم آخر على نفس الفواتير"
          value={money.format(manual)}
          hint={r.bills_discount_total > 0 ? `٪${toArabicDigits(promoShare)} ترويجي` : undefined}
        />
      </div>

      <div className="mt-4">
        <div className="mb-3 text-xs font-medium uppercase tracking-wide text-muted-foreground">
          الاستخدام اليومي
        </div>
        <PerformanceChart days={r.by_day} />
      </div>

      {r.by_branch.length > 0 && (
        <div className="mt-4 rounded-xl border border-border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>الفرع</TableHead>
                <TableHead>الفواتير</TableHead>
                <TableHead>الأصناف</TableHead>
                <TableHead>الخصم الترويجي</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {r.by_branch.map((b) => (
                <TableRow key={b.branch_id}>
                  <TableCell className="font-medium">{b.branch_name ?? b.branch_id}</TableCell>
                  <TableCell>{toArabicDigits(b.bills_count)}</TableCell>
                  <TableCell>{toArabicDigits(b.items_count)}</TableCell>
                  <TableCell>{money.format(b.total_amount)}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </div>
  )
}
