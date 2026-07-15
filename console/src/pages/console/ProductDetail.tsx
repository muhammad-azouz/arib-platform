import { useState, type ReactNode } from 'react'
import { Link, useLocation, useParams } from 'react-router-dom'
import { ApiError } from '@/lib/api'
import { useBundle, useCatalogProduct, useHqBranches, useProductMovements } from '@/lib/hooks'
import { fmtDateTime, relative, toArabicDigits } from '@/lib/format'
import { cn } from '@/lib/utils'
import type { BranchHealth, MovementRow, ProductUnit } from '@/lib/types'
import { Breadcrumbs } from '@/components/Breadcrumbs'
import { EditUnitPriceDialog } from '@/components/EditUnitPriceDialog'
import { Freshness } from '@/components/Freshness'
import { Pagination } from '@/components/Pagination'
import { PropagationPanel } from '@/components/PropagationPanel'
import { LoadingState, EmptyState, ErrorState } from '@/components/States'
import {
  ArrowLeading,
  BarcodeIcon,
  BranchIcon,
  CatalogIcon,
  EditIcon,
  HistoryIcon,
  UnitIcon,
} from '@/components/icon'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'

const HEALTH_DOT: Record<BranchHealth, string> = {
  ok: 'bg-success',
  lagging: 'bg-warning',
  stale: 'bg-danger',
  never: 'bg-muted-foreground/40',
}
const HEALTH_LABEL: Record<BranchHealth, string> = {
  ok: 'متزامن',
  lagging: 'متأخر في المزامنة',
  stale: 'منقطع عن المزامنة',
  never: 'لم يتصل بعد',
}

const money = new Intl.NumberFormat('ar', { maximumFractionDigits: 2 })

/** Collapsible section (native disclosure — accessible, no JS state). */
function Section({
  icon: IconCmp,
  title,
  children,
  defaultOpen,
  onToggle,
}: {
  icon: typeof UnitIcon
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

const selectClass =
  'flex h-9 rounded-md border border-input bg-background/40 px-3 py-1 text-sm shadow-sm transition-colors focus-visible:outline-none focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/30'

const DEALING_LABEL: Record<number, string> = {
  100: 'بيع',
  101: 'مرتجع بيع',
  200: 'شراء',
  201: 'مرتجع شراء',
  300: 'طلب',
  700: 'رصيد افتتاحي',
  2000: 'تسوية مخزون',
}
function dealingLabel(d: number): string {
  return DEALING_LABEL[d] ?? `نوع ${d}`
}

function toDateParam(d: Date): string {
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${y}-${m}-${day}`
}

const PERIOD_OPTIONS = [
  { days: 7, label: '٧ أيام' },
  { days: 30, label: '٣٠ يومًا' },
  { days: 90, label: '٩٠ يومًا' },
]
const MOV_PAGE_SIZE = 25

/**
 * Movement history for one product ("حركة الصنف"). Zero requests until the
 * section is opened — `opened` gates the query and, once true, stays true so
 * re-collapsing doesn't discard the fetched page.
 */
function MovementsSection({
  tenantId,
  productId,
  branches,
}: {
  tenantId?: string
  productId?: string
  branches: { id: string; name: string }[]
}) {
  const [opened, setOpened] = useState(false)
  const [branchId, setBranchId] = useState<string | undefined>(undefined)
  const [periodDays, setPeriodDays] = useState(30)
  const [page, setPage] = useState(1)

  const filterKey = `${branchId ?? ''} ${periodDays}`
  const [lastFilterKey, setLastFilterKey] = useState(filterKey)
  if (filterKey !== lastFilterKey) {
    setLastFilterKey(filterKey)
    setPage(1)
  }

  const to = new Date()
  const from = new Date(to)
  from.setDate(from.getDate() - periodDays)

  const query = useProductMovements(
    tenantId,
    productId,
    { branchId, from: toDateParam(from), to: toDateParam(to), page, pageSize: MOV_PAGE_SIZE },
    opened,
  )

  return (
    <Section icon={HistoryIcon} title="حركة الصنف" onToggle={(open) => open && setOpened(true)}>
      {!opened ? null : (
        <>
          <div className="mb-4 flex flex-wrap items-center gap-3">
            <select
              className={selectClass}
              value={branchId ?? ''}
              onChange={(e) => setBranchId(e.target.value || undefined)}
            >
              <option value="">كل الفروع</option>
              {branches.map((b) => (
                <option key={b.id} value={b.id}>
                  {b.name}
                </option>
              ))}
            </select>
            <div className="inline-flex rounded-lg border border-border p-1">
              {PERIOD_OPTIONS.map((o) => (
                <button
                  key={o.days}
                  type="button"
                  onClick={() => setPeriodDays(o.days)}
                  className={cn(
                    'rounded-md px-3 py-1 text-xs font-medium transition-colors',
                    periodDays === o.days
                      ? 'bg-accent text-primary'
                      : 'text-muted-foreground hover:text-foreground',
                  )}
                >
                  {o.label}
                </button>
              ))}
            </div>
          </div>

          {query.error ? (
            <ErrorState
              message="تعذّر تحميل حركة الصنف الآن."
              onRetry={() => void query.refetch()}
            />
          ) : query.isLoading ? (
            <LoadingState rows={3} />
          ) : !query.data ? null : (
            <>
              <div className="-mx-4 overflow-x-auto">
                <table className="w-full min-w-[720px] text-start">
                  <thead>
                    <tr className="text-xs text-muted-foreground">
                      <th className="px-4 py-1.5 text-start font-medium">التاريخ</th>
                      <th className="px-4 py-1.5 text-start font-medium">نوع الحركة</th>
                      <th className="px-4 py-1.5 text-start font-medium">الفرع</th>
                      <th className="px-4 py-1.5 text-start font-medium">المخزن</th>
                      <th className="px-4 py-1.5 text-start font-medium">وارد</th>
                      <th className="px-4 py-1.5 text-start font-medium">صادر</th>
                      <th className="px-4 py-1.5 text-start font-medium">الرصيد الجاري</th>
                    </tr>
                  </thead>
                  <tbody>
                    <tr className="border-t border-border bg-muted/30 font-medium">
                      <td className="px-4 py-2" colSpan={6}>
                        رصيد أول المدة
                      </td>
                      <td className="px-4 py-2">{toArabicDigits(query.data.data.opening_qty)}</td>
                    </tr>
                    {query.data.data.items.map((m: MovementRow) => (
                      <tr key={m.id} className="border-t border-border">
                        <td className="px-4 py-2 text-xs text-muted-foreground">
                          {fmtDateTime(m.issue_date)}
                        </td>
                        <td className="px-4 py-2">{dealingLabel(m.dealing)}</td>
                        <td className="px-4 py-2 text-muted-foreground">{m.branch_name}</td>
                        <td className="px-4 py-2 text-muted-foreground">{m.warehouse_name}</td>
                        <td className="px-4 py-2">
                          {m.in_qty > 0 ? toArabicDigits(m.in_qty) : '—'}
                        </td>
                        <td className="px-4 py-2">
                          {m.out_qty > 0 ? toArabicDigits(m.out_qty) : '—'}
                        </td>
                        <td className="px-4 py-2 font-medium">
                          {toArabicDigits(m.running_qty)}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
              {query.data.data.items.length === 0 && (
                <p className="mt-3 text-muted-foreground">لا توجد حركات في هذه الفترة.</p>
              )}
              {query.data.data.total > 0 && (
                <Pagination
                  page={page}
                  pageSize={MOV_PAGE_SIZE}
                  total={query.data.data.total}
                  itemLabel="حركة"
                  onPageChange={setPage}
                />
              )}
            </>
          )}
        </>
      )}
    </Section>
  )
}

export function ProductDetail() {
  const { tenantId, productId } = useParams<'tenantId' | 'productId'>()
  const location = useLocation()
  const { data: bundle } = useBundle(tenantId)
  const productQuery = useCatalogProduct(tenantId, productId)
  const { data: hqBranches } = useHqBranches(tenantId)

  const [editingUnit, setEditingUnit] = useState<ProductUnit | null>(null)
  // Session-scoped (v1: honesty over persistence, per the plan) — resets on
  // navigation away from this product, which is fine: it exists only to
  // drive the propagation panel for the write just made in this visit. Seeded
  // from router state when we just navigated here from a fresh product
  // creation, so its propagation panel shows immediately too.
  const [writtenAt, setWrittenAt] = useState<string | null>(
    () => (location.state as { writtenAt?: string } | null)?.writtenAt ?? null,
  )

  if (!bundle || productQuery.isLoading) return <LoadingState />

  const crumbs = [
    { label: 'الكتالوج', to: `/tenants/${tenantId}/catalog` },
    { label: productQuery.data?.data.name ?? 'الصنف' },
  ]

  if (productQuery.error instanceof ApiError && productQuery.error.status === 402) {
    return (
      <>
        <Breadcrumbs className="mb-4" items={crumbs} />
        <EmptyState
          icon={CatalogIcon}
          title="لا يوجد اشتراك مزامنة"
          description="فعّل اشتراك المزامنة لعرض تفاصيل هذا الصنف."
        />
      </>
    )
  }

  if (productQuery.error instanceof ApiError && productQuery.error.status === 404) {
    return (
      <>
        <Breadcrumbs className="mb-4" items={crumbs} />
        <EmptyState
          icon={CatalogIcon}
          title="الصنف غير موجود"
          description="لم يعد هذا الصنف موجودًا في الكتالوج."
          action={
            <Link to={`/tenants/${tenantId}/catalog`} className="text-sm text-primary">
              العودة إلى الكتالوج
            </Link>
          }
        />
      </>
    )
  }

  if (productQuery.error) {
    return (
      <>
        <Breadcrumbs className="mb-4" items={crumbs} />
        <ErrorState
          message="تعذّر الوصول إلى بيانات الصنف الآن."
          onRetry={() => void productQuery.refetch()}
        />
      </>
    )
  }

  if (!productQuery.data) return <LoadingState />

  const p = productQuery.data.data

  return (
    <>
      <Breadcrumbs className="mb-4" items={crumbs} />

      <div className="mb-6 flex flex-col gap-3">
        <div className="flex flex-wrap items-center gap-3">
          <h1 className="font-display text-2xl font-bold">{p.name}</h1>
          <Badge tone={p.is_active ? 'success' : 'neutral'}>
            {p.is_active ? 'مُفعّل' : 'مُعطّل'}
          </Badge>
          <Freshness source={productQuery.data.source} asOf={productQuery.data.as_of} />
        </div>
        <div className="flex flex-wrap gap-x-6 gap-y-1 text-sm text-muted-foreground">
          <span>
            الكود:{' '}
            <span className="dir-ltr font-mono font-medium text-foreground">
              {toArabicDigits(p.code)}
            </span>
          </span>
          <span>
            المجموعة: <span className="font-medium text-foreground">{p.group_name ?? '—'}</span>
          </span>
        </div>
      </div>

      <div className="flex flex-col gap-3">
        <Section icon={UnitIcon} title="الوحدات والأسعار" defaultOpen>
          {p.units.length === 0 ? (
            <p className="text-muted-foreground">لا توجد وحدات مسجّلة لهذا الصنف.</p>
          ) : (
            <div className="-mx-4 overflow-x-auto">
              <table className="w-full min-w-[520px] text-start">
                <thead>
                  <tr className="text-xs text-muted-foreground">
                    <th className="px-4 py-1.5 text-start font-medium">الوحدة</th>
                    <th className="px-4 py-1.5 text-start font-medium">الشراء</th>
                    <th className="px-4 py-1.5 text-start font-medium">البيع</th>
                    <th className="px-4 py-1.5 text-start font-medium">أسعار أخرى</th>
                    <th className="px-4 py-1.5 text-start font-medium">الباركود</th>
                    <th className="px-4 py-1.5" />
                  </tr>
                </thead>
                <tbody>
                  {p.units.map((u) => {
                    const extraPrices = u.prices.filter((v) => v > 0)
                    return (
                      <tr key={u.id} className="border-t border-border">
                        <td className="px-4 py-2 font-medium">{u.name}</td>
                        <td className="px-4 py-2">{money.format(u.buy)}</td>
                        <td className="px-4 py-2">{money.format(u.sale)}</td>
                        <td className="px-4 py-2 text-muted-foreground">
                          {extraPrices.length > 0
                            ? extraPrices.map((v) => money.format(v)).join(' · ')
                            : '—'}
                        </td>
                        <td className="px-4 py-2">
                          {u.barcodes.length > 0 ? (
                            <div className="flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground">
                              <BarcodeIcon className="size-3.5 shrink-0" />
                              <span className="dir-ltr font-mono">{u.barcodes.join(' · ')}</span>
                            </div>
                          ) : (
                            '—'
                          )}
                        </td>
                        <td className="px-4 py-2 text-end">
                          <Button
                            type="button"
                            variant="ghost"
                            size="sm"
                            onClick={() => setEditingUnit(u)}
                          >
                            <EditIcon className="size-4" />
                            تعديل
                          </Button>
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>
          )}
        </Section>

        {writtenAt && (
          <PropagationPanel
            writtenAt={writtenAt}
            branches={(hqBranches?.branches ?? []).filter((b) => b.status === 'active')}
          />
        )}

        <Section icon={BranchIcon} title="التوفر في الفروع" defaultOpen>
          {p.availability.length === 0 ? (
            <p className="text-muted-foreground">لا تتوفر بيانات مخزون لهذا الصنف.</p>
          ) : (
            <div className="-mx-4 overflow-x-auto">
              <table className="w-full min-w-[560px] text-start">
                <thead>
                  <tr className="text-xs text-muted-foreground">
                    <th className="px-4 py-1.5 text-start font-medium">الفرع</th>
                    <th className="px-4 py-1.5 text-start font-medium">المخزن</th>
                    <th className="px-4 py-1.5 text-start font-medium">الكمية</th>
                    <th className="px-4 py-1.5 text-start font-medium">تكلفة الوحدة</th>
                    <th className="px-4 py-1.5 text-start font-medium">آخر مزامنة</th>
                  </tr>
                </thead>
                <tbody>
                  {p.availability.map((a) => (
                    <tr key={`${a.branch_id}-${a.warehouse_id}`} className="border-t border-border">
                      <td className="px-4 py-2">
                        <Link
                          to={`/tenants/${tenantId}/branches/${a.branch_id}`}
                          className="flex items-center gap-2 font-medium hover:text-primary"
                        >
                          <span
                            title={HEALTH_LABEL[a.health]}
                            className={cn('size-2 shrink-0 rounded-full', HEALTH_DOT[a.health])}
                          />
                          {a.branch_name || '—'}
                        </Link>
                      </td>
                      <td className="px-4 py-2 text-muted-foreground">{a.warehouse_name}</td>
                      <td className="px-4 py-2">{toArabicDigits(a.total_qty)}</td>
                      <td className="px-4 py-2">{money.format(a.unit_cost)}</td>
                      <td className="px-4 py-2 text-xs text-muted-foreground">
                        {a.last_sync_at
                          ? `${fmtDateTime(a.last_sync_at)} (${relative(a.last_sync_at)})`
                          : 'لم تتم المزامنة بعد'}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </Section>

        <MovementsSection
          tenantId={tenantId}
          productId={productId}
          branches={(hqBranches?.branches ?? [])
            .filter((b) => b.status === 'active')
            .map((b) => ({ id: b.id, name: b.name }))}
        />
      </div>

      {tenantId && productId && (
        <EditUnitPriceDialog
          tenantId={tenantId}
          productId={productId}
          unit={editingUnit}
          open={!!editingUnit}
          onOpenChange={(open) => {
            if (!open) setEditingUnit(null)
          }}
          onWritten={setWrittenAt}
        />
      )}
    </>
  )
}
