import { useState, type ReactNode } from 'react'
import { Link, useParams } from 'react-router-dom'
import { toast } from 'sonner'
import { ApiError } from '@/lib/api'
import { errorMessage } from '@/lib/auth'
import {
  useSupplier,
  useSupplierLedger,
  useSupplierPurchases,
  useUpdateSupplier,
} from '@/lib/hooks'
import { fmtDateTime, relative, toArabicDigits } from '@/lib/format'
import { cn } from '@/lib/utils'
import { Breadcrumbs } from '@/components/Breadcrumbs'
import { EditSupplierDialog } from '@/components/EditSupplierDialog'
import { Freshness } from '@/components/Freshness'
import { HealthDot } from '@/components/HealthDot'
import { Pagination } from '@/components/Pagination'
import { LoadingState, EmptyState, ErrorState } from '@/components/States'
import { ArrowLeading, EditIcon, HistoryIcon, ReceiptIcon, SupplierIcon } from '@/components/icon'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'

const money = new Intl.NumberFormat('ar', { maximumFractionDigits: 2 })
const PAGE_SIZE = 25

// Bill.Type: Purchase/RePurchase for suppliers (bills the business received
// from the supplier), vs Sale/ReSale for customers — see BillTypesFor
// (HqApi.cs, slice 8).
const BILL_TYPE_LABEL: Record<number, string> = {
  200: 'شراء',
  201: 'مرتجع شراء',
}
const DEALING_LABEL: Record<number, string> = {
  100: 'بيع',
  101: 'مرتجع بيع',
  400: 'تحصيل نقدي',
  401: 'صرف نقدي',
  500: 'تحصيل محفظة',
  501: 'صرف محفظة',
  600: 'تحصيل بنكي',
  601: 'صرف بنكي',
  700: 'رصيد افتتاحي',
  800: 'خصم نقدي',
  900: 'رصيد سابق',
}
function dealingLabel(d: number): string {
  return DEALING_LABEL[d] ?? `نوع ${d}`
}

function StatTile({ label, value }: { label: string; value: ReactNode }) {
  return (
    <Card className="p-4">
      <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
        {label}
      </div>
      <div className="mt-1 font-display text-xl font-bold">{value}</div>
    </Card>
  )
}

/** Collapsible section (native disclosure — accessible, no JS state), same shape as ProductDetail's. */
function Section({
  icon: IconCmp,
  title,
  children,
  defaultOpen,
  onToggle,
}: {
  icon: typeof HistoryIcon
  title: string
  children: ReactNode
  defaultOpen?: boolean
  onToggle?: (open: boolean) => void
}) {
  return (
    <details
      open={defaultOpen}
      className="group rounded-xl border border-border bg-card/50"
      onToggle={onToggle ? (e) => onToggle(e.currentTarget.open) : undefined}
    >
      <summary className="flex cursor-pointer list-none items-center gap-2.5 p-4 font-medium [&::-webkit-details-marker]:hidden">
        <IconCmp className="size-5 text-primary" />
        {title}
        <ArrowLeading className="ms-auto size-4 text-muted-foreground transition-transform group-open:-rotate-90" />
      </summary>
      <div className="border-t border-border p-4 text-sm">{children}</div>
    </details>
  )
}

function PurchasesSection({
  tenantId,
  supplierId,
}: {
  tenantId?: string
  supplierId?: string
}) {
  const [page, setPage] = useState(1)
  const query = useSupplierPurchases(tenantId, supplierId, { page, pageSize: PAGE_SIZE })

  if (query.error) {
    return (
      <ErrorState message="تعذّر تحميل سجل المشتريات الآن." onRetry={() => void query.refetch()} />
    )
  }
  if (query.isLoading) return <LoadingState rows={3} />
  if (!query.data) return null

  const { items, total } = query.data.data
  if (items.length === 0) {
    return <p className="text-muted-foreground">لا توجد فواتير مسجّلة لهذا المورد.</p>
  }
  return (
    <>
      <div className="-mx-4 overflow-x-auto">
        <table className="w-full min-w-[560px] text-start">
          <thead>
            <tr className="text-xs text-muted-foreground">
              <th className="px-4 py-1.5 text-start font-medium">الفاتورة</th>
              <th className="px-4 py-1.5 text-start font-medium">التاريخ</th>
              <th className="px-4 py-1.5 text-start font-medium">النوع</th>
              <th className="px-4 py-1.5 text-start font-medium">عدد الأصناف</th>
              <th className="px-4 py-1.5 text-start font-medium">الإجمالي</th>
              <th className="px-4 py-1.5 text-start font-medium">السداد</th>
            </tr>
          </thead>
          <tbody>
            {items.map((b) => (
              <tr key={b.id} className="border-t border-border">
                <td className="dir-ltr px-4 py-2 text-start font-mono text-xs">{b.num}</td>
                <td className="px-4 py-2 text-xs text-muted-foreground">
                  {fmtDateTime(b.issued_at)}
                </td>
                <td className="px-4 py-2">{BILL_TYPE_LABEL[b.type] ?? `نوع ${b.type}`}</td>
                <td className="px-4 py-2">{toArabicDigits(b.item_count)}</td>
                <td className="px-4 py-2 font-medium">{money.format(b.total)}</td>
                <td className="px-4 py-2">
                  <Badge tone={b.is_paid ? 'success' : 'warning'}>
                    {b.is_paid ? 'مسدد' : 'غير مسدد'}
                  </Badge>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {total > 0 && (
        <Pagination
          page={page}
          pageSize={PAGE_SIZE}
          total={total}
          itemLabel="فاتورة"
          onPageChange={setPage}
        />
      )}
    </>
  )
}

function LedgerSection({
  tenantId,
  supplierId,
}: {
  tenantId?: string
  supplierId?: string
}) {
  const [page, setPage] = useState(1)
  const query = useSupplierLedger(tenantId, supplierId, { page, pageSize: PAGE_SIZE })

  if (query.error) {
    return (
      <ErrorState message="تعذّر تحميل كشف الحساب الآن." onRetry={() => void query.refetch()} />
    )
  }
  if (query.isLoading) return <LoadingState rows={3} />
  if (!query.data) return null

  const { items, total } = query.data.data
  if (items.length === 0) {
    return <p className="text-muted-foreground">لا توجد حركات في كشف حساب هذا المورد.</p>
  }
  return (
    <>
      <div className="-mx-4 overflow-x-auto">
        <table className="w-full min-w-[600px] text-start">
          <thead>
            <tr className="text-xs text-muted-foreground">
              <th className="px-4 py-1.5 text-start font-medium">التاريخ</th>
              <th className="px-4 py-1.5 text-start font-medium">نوع الحركة</th>
              <th className="px-4 py-1.5 text-start font-medium">مدين</th>
              <th className="px-4 py-1.5 text-start font-medium">دائن</th>
              <th className="px-4 py-1.5 text-start font-medium">الرصيد الجاري</th>
              <th className="px-4 py-1.5 text-start font-medium">ملاحظة</th>
            </tr>
          </thead>
          <tbody>
            {items.map((t) => (
              <tr key={t.id} className="border-t border-border">
                <td className="px-4 py-2 text-xs text-muted-foreground">
                  {fmtDateTime(t.created_at)}
                </td>
                <td className="px-4 py-2">{dealingLabel(t.dealing)}</td>
                <td className="px-4 py-2">{t.debit > 0 ? money.format(t.debit) : '—'}</td>
                <td className="px-4 py-2">{t.credit > 0 ? money.format(t.credit) : '—'}</td>
                <td
                  className={cn(
                    'px-4 py-2 font-medium',
                    t.running_balance > 0 && 'text-warning',
                  )}
                >
                  {money.format(t.running_balance)}
                </td>
                <td className="px-4 py-2 text-xs text-muted-foreground">{t.note ?? '—'}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {total > 0 && (
        <Pagination
          page={page}
          pageSize={PAGE_SIZE}
          total={total}
          itemLabel="حركة"
          onPageChange={setPage}
        />
      )}
    </>
  )
}

export function SupplierDetail() {
  const { tenantId, supplierId } = useParams<'tenantId' | 'supplierId'>()
  const query = useSupplier(tenantId, supplierId)
  const update = useUpdateSupplier(tenantId ?? '')
  const [editOpen, setEditOpen] = useState(false)

  const crumbs = [
    { label: 'الموردون', to: `/tenants/${tenantId}/suppliers` },
    { label: query.data?.data.name ?? 'المورد' },
  ]

  if (query.error instanceof ApiError && query.error.status === 402) {
    return (
      <>
        <Breadcrumbs className="mb-4" items={crumbs} />
        <EmptyState
          icon={SupplierIcon}
          title="لا يوجد اشتراك مزامنة"
          description="فعّل اشتراك المزامنة لعرض تفاصيل هذا المورد."
        />
      </>
    )
  }

  if (query.error instanceof ApiError && query.error.status === 404) {
    return (
      <>
        <Breadcrumbs className="mb-4" items={crumbs} />
        <EmptyState
          icon={SupplierIcon}
          title="المورد غير موجود"
          description="لم يعد هذا المورد موجودًا."
          action={
            <Link to={`/tenants/${tenantId}/suppliers`} className="text-sm text-primary">
              العودة إلى الموردين
            </Link>
          }
        />
      </>
    )
  }

  if (query.error) {
    return (
      <>
        <Breadcrumbs className="mb-4" items={crumbs} />
        <ErrorState
          message="تعذّر الوصول إلى بيانات المورد الآن."
          onRetry={() => void query.refetch()}
        />
      </>
    )
  }

  if (!query.data) return <LoadingState />

  const c = query.data.data

  const toggleActive = () => {
    update.mutate(
      { supplierId: c.id, is_active: !c.is_active },
      {
        onSuccess: () => toast.success(c.is_active ? 'تم تعطيل المورد' : 'تم تفعيل المورد'),
        onError: (err) => toast.error(errorMessage(err)),
      },
    )
  }

  return (
    <>
      <Breadcrumbs className="mb-4" items={crumbs} />

      <div className="mb-6 flex flex-col gap-3">
        <div className="flex flex-wrap items-center gap-3">
          <h1 className="font-display text-2xl font-bold">{c.name}</h1>
          <Badge tone={c.is_active ? 'success' : 'neutral'}>
            {c.is_active ? 'مُفعّل' : 'مُعطّل'}
          </Badge>
          <Freshness source={query.data.source} asOf={query.data.as_of} />
          <div className="ms-auto flex items-center gap-2">
            <Button variant="outline" size="sm" onClick={() => setEditOpen(true)}>
              <EditIcon className="size-4" />
              تعديل
            </Button>
            <Button
              variant="outline"
              size="sm"
              disabled={update.isPending}
              onClick={toggleActive}
            >
              {c.is_active ? 'تعطيل المورد' : 'تفعيل المورد'}
            </Button>
          </div>
        </div>
        <div className="flex flex-wrap items-center gap-x-6 gap-y-1 text-sm text-muted-foreground">
          <span>
            الكود:{' '}
            <span className="dir-ltr font-mono font-medium text-foreground">
              {toArabicDigits(c.num)}
            </span>
          </span>
          <span className="flex items-center gap-1.5">
            الفرع: <HealthDot health={c.health} />
            <span className="font-medium text-foreground">{c.branch_name}</span>
          </span>
          <span>
            المجموعة: <span className="font-medium text-foreground">{c.group_name ?? '—'}</span>
          </span>
          <span>
            الهاتف:{' '}
            <span dir="ltr" className="font-medium text-foreground">
              {c.phone1}
            </span>
          </span>
        </div>
      </div>

      <div className="mb-5 grid grid-cols-2 gap-3 sm:grid-cols-4">
        <StatTile label="الرصيد الحالي" value={money.format(c.balance)} />
        <StatTile label="عدد الطلبات" value={toArabicDigits(c.stats.number_of_orders)} />
        <StatTile label="إجمالي المشتريات" value={money.format(c.stats.total_spent)} />
        <StatTile
          label="آخر عملية شراء"
          value={c.stats.last_purchase_date ? relative(c.stats.last_purchase_date) : '—'}
        />
      </div>

      {c.credit_limit > 0 && (
        <Card
          className={cn(
            'mb-5 p-4 text-sm',
            c.balance >= c.credit_limit
              ? 'border-danger/30 bg-danger/5'
              : c.balance >= c.credit_limit * 0.8
                ? 'border-warning/30 bg-warning/5'
                : undefined,
          )}
        >
          <div className="flex flex-wrap items-center justify-between gap-2">
            <span className="text-muted-foreground">الحد الائتماني</span>
            <span className="font-medium">
              {money.format(c.balance)} / {money.format(c.credit_limit)}
            </span>
          </div>
        </Card>
      )}

      <div className="flex flex-col gap-3">
        <Section icon={ReceiptIcon} title="سجل المشتريات" defaultOpen>
          <PurchasesSection tenantId={tenantId} supplierId={supplierId} />
        </Section>

        <Section icon={HistoryIcon} title="كشف الحساب">
          <LedgerSection tenantId={tenantId} supplierId={supplierId} />
        </Section>
      </div>

      {tenantId && (
        <EditSupplierDialog
          tenantId={tenantId}
          supplier={c}
          open={editOpen}
          onOpenChange={setEditOpen}
        />
      )}
    </>
  )
}
