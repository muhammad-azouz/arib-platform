import { useState, type ReactNode } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { format } from 'date-fns'
import { ar } from 'date-fns/locale'
import { ApiError } from '@/lib/api'
import {
  useBundle,
  useCatalogGroups,
  useInventoryBranches,
  useReportBranches,
  useReportProducts,
  useReportSales,
  useReportShiftDetail,
  useReportShifts,
  useReportStaff,
} from '@/lib/hooks'
import { fmtDateTime, toArabicDigits } from '@/lib/format'
import { cn } from '@/lib/utils'
import type { ReportSort, SalesDay, ShiftStatusFilter } from '@/lib/types'
import { Freshness } from '@/components/Freshness'
import { HealthDot } from '@/components/HealthDot'
import { PageHeader } from '@/components/PageHeader'
import { Pagination } from '@/components/Pagination'
import { PeriodPicker } from '@/components/PeriodPicker'
import { LoadingState, EmptyState, ErrorState } from '@/components/States'
import { ArrowLeading, InventoryIcon, ReportsIcon, UsersIcon } from '@/components/icon'
import { Badge } from '@/components/ui/badge'
import { Card } from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
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

const money = new Intl.NumberFormat('ar', { maximumFractionDigits: 2 })
const PAGE_SIZE = 25

const selectClass =
  'flex h-9 w-full rounded-md border border-input bg-background/40 px-3 py-1 text-sm shadow-sm transition-colors focus-visible:outline-none focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/30'

/** A YYYY-MM-DD local-date string as a Date, without a UTC shift. */
function parseDay(day: string): Date {
  return new Date(`${day}T00:00:00`)
}

type ViewKey = 'sales' | 'products' | 'branches' | 'staff' | 'shifts' | 'inventory'
const VIEWS: { key: ViewKey; label: string }[] = [
  { key: 'sales', label: 'المبيعات' },
  { key: 'products', label: 'الأصناف' },
  { key: 'branches', label: 'الفروع' },
  { key: 'staff', label: 'الموظفون' },
  { key: 'shifts', label: 'الورديات' },
  { key: 'inventory', label: 'المخزون' },
]

export function Reports() {
  const { tenantId } = useParams<'tenantId'>()
  const { data: bundle } = useBundle(tenantId)
  const [searchParams, setSearchParams] = useSearchParams()

  const view: ViewKey = (searchParams.get('view') as ViewKey | null) ?? 'sales'
  const from = searchParams.get('from') ?? undefined
  const to = searchParams.get('to') ?? undefined
  const branchId = searchParams.get('branch') ?? undefined

  const setView = (v: ViewKey) => {
    const next = new URLSearchParams(searchParams)
    next.set('view', v)
    setSearchParams(next, { replace: true })
  }
  const setBranchId = (id: string | undefined) => {
    const next = new URLSearchParams(searchParams)
    if (id) next.set('branch', id)
    else next.delete('branch')
    setSearchParams(next, { replace: true })
  }

  if (!bundle) return <LoadingState />

  const branches = (bundle.Branches ?? []).filter((b) => b.Status === 'active')

  return (
    <>
      <PageHeader title="التقارير" description="إجابات جاهزة عن أسئلة عملك." />

      <div className="mb-4 inline-flex rounded-lg border border-border bg-card/50 p-1">
        {VIEWS.map((o) => (
          <button
            key={o.key}
            type="button"
            onClick={() => setView(o.key)}
            className={cn(
              'rounded-md px-3.5 py-1.5 text-sm font-medium transition-colors',
              view === o.key
                ? 'bg-accent text-primary'
                : 'text-muted-foreground hover:text-foreground',
            )}
          >
            {o.label}
          </button>
        ))}
      </div>

      {/* The inventory question is a live snapshot, not a period aggregate. */}
      {view !== 'inventory' && <PeriodPicker className="mb-5" />}

      {view === 'sales' && (
        <SalesView
          tenantId={tenantId}
          from={from}
          to={to}
          branchId={branchId}
          branches={branches}
          onBranchChange={setBranchId}
        />
      )}
      {view === 'products' && (
        <ProductsView
          tenantId={tenantId}
          from={from}
          to={to}
          branchId={branchId}
          branches={branches}
          onBranchChange={setBranchId}
        />
      )}
      {view === 'branches' && <BranchesView tenantId={tenantId} from={from} to={to} />}
      {view === 'staff' && (
        <StaffView
          tenantId={tenantId}
          from={from}
          to={to}
          branchId={branchId}
          branches={branches}
          onBranchChange={setBranchId}
        />
      )}
      {view === 'shifts' && (
        <ShiftsView
          tenantId={tenantId}
          from={from}
          to={to}
          branchId={branchId}
          branches={branches}
          onBranchChange={setBranchId}
        />
      )}
      {view === 'inventory' && <InventoryView tenantId={tenantId} />}
    </>
  )
}

// --- shared bits ---

interface BranchOption {
  ID: string
  Name: string
}

function BranchSelect({
  branchId,
  branches,
  onChange,
}: {
  branchId?: string
  branches: BranchOption[]
  onChange: (id: string | undefined) => void
}) {
  return (
    <select
      className={cn(selectClass, 'max-w-xs')}
      value={branchId ?? ''}
      onChange={(e) => onChange(e.target.value || undefined)}
    >
      <option value="">كل الفروع</option>
      {branches.map((b) => (
        <option key={b.ID} value={b.ID}>
          {b.Name}
        </option>
      ))}
    </select>
  )
}

function NotSubscribed() {
  return (
    <EmptyState
      icon={ReportsIcon}
      title="لا يوجد اشتراك مزامنة"
      description="فعّل اشتراك المزامنة لعرض تقارير فروعك."
    />
  )
}

function KpiTile({ label, value, tone }: { label: string; value: string; tone?: 'danger' }) {
  return (
    <Card className="p-4">
      <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
        {label}
      </div>
      <div
        className={cn('mt-1 font-display text-xl font-bold', tone === 'danger' && 'text-danger')}
      >
        {value}
      </div>
    </Card>
  )
}

// --- المبيعات ---

function SalesView({
  tenantId,
  from,
  to,
  branchId,
  branches,
  onBranchChange,
}: {
  tenantId?: string
  from?: string
  to?: string
  branchId?: string
  branches: BranchOption[]
  onBranchChange: (id: string | undefined) => void
}) {
  const query = useReportSales(tenantId, { from, to, branchId })

  const notSubscribed = query.error instanceof ApiError && query.error.status === 402
  const gatewayError = query.error instanceof ApiError && query.error.status !== 402

  if (notSubscribed) return <NotSubscribed />
  if (gatewayError) {
    return (
      <ErrorState
        message="تعذّر الوصول إلى بيانات التقارير الآن."
        onRetry={() => void query.refetch()}
      />
    )
  }
  if (query.isLoading || !query.data) return <LoadingState rows={4} />

  const r = query.data.data
  const net = r.sales_total - r.refunds_total
  const avg = r.sales_count > 0 ? r.sales_total / r.sales_count : 0

  return (
    <div>
      <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
        <BranchSelect branchId={branchId} branches={branches} onChange={onBranchChange} />
        <Freshness source={query.data.source} asOf={query.data.as_of} />
      </div>

      <div className="mb-4 grid grid-cols-2 gap-3 lg:grid-cols-5">
        <KpiTile label="المبيعات" value={money.format(r.sales_total)} />
        <KpiTile label="عدد الفواتير" value={toArabicDigits(r.sales_count)} />
        <KpiTile
          label="المرتجعات"
          value={money.format(r.refunds_total)}
          tone={r.refunds_total > 0 ? 'danger' : undefined}
        />
        <KpiTile label="الصافي" value={money.format(net)} />
        <KpiTile label="متوسط الفاتورة" value={money.format(avg)} />
      </div>

      <Card className="mb-4 p-4">
        <div className="mb-1 text-xs font-medium uppercase tracking-wide text-muted-foreground">
          طرق الدفع
        </div>
        <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
          <TenderCell label="نقدًا" value={r.tender.cash} total={r.sales_total} />
          <TenderCell label="بنك / بطاقة" value={r.tender.bank} total={r.sales_total} />
          <TenderCell label="محفظة" value={r.tender.wallet} total={r.sales_total} />
          <TenderCell label="آجل" value={r.tender.credit} total={r.sales_total} />
        </div>
      </Card>

      <Card className="mb-4 p-4">
        <div className="mb-3 text-xs font-medium uppercase tracking-wide text-muted-foreground">
          المبيعات اليومية
        </div>
        <SalesChart days={r.days} />
      </Card>

      <div className="rounded-xl border border-border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>اليوم</TableHead>
              <TableHead>الفواتير</TableHead>
              <TableHead>المبيعات</TableHead>
              <TableHead>المرتجعات</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {[...r.days].reverse().map((d) => (
              <TableRow key={d.day}>
                <TableCell className="font-medium">
                  {format(parseDay(d.day), 'EEEE d MMM', { locale: ar })}
                </TableCell>
                <TableCell>{toArabicDigits(d.sales_count)}</TableCell>
                <TableCell>{money.format(d.sales_total)}</TableCell>
                <TableCell
                  className={cn(d.refunds_total > 0 ? 'text-danger' : 'text-muted-foreground')}
                >
                  {money.format(d.refunds_total)}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
    </div>
  )
}

function TenderCell({ label, value, total }: { label: string; value: number; total: number }) {
  const share = total > 0 ? Math.round((value / total) * 100) : 0
  return (
    <div>
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="mt-0.5 font-medium">{money.format(value)}</div>
      <div className="mt-1.5 h-1.5 overflow-hidden rounded-full bg-muted">
        <div className="h-full rounded-full bg-primary" style={{ width: `${share}%` }} />
      </div>
      <div className="mt-0.5 text-xs text-muted-foreground">٪{toArabicDigits(share)}</div>
    </div>
  )
}

/**
 * Daily sales bars — one series, so the card title carries identity (no
 * legend). CSS flex bars instead of SVG: theme tokens apply directly, RTL
 * needs no coordinate math (the row is pinned dir-ltr so time reads
 * left→right), and each bar gets a native tooltip; the day table below is
 * the accessible/table view of the same numbers. Only the peak day gets a
 * direct label (selective labeling); x labels thin out to ~8.
 */
function SalesChart({ days }: { days: SalesDay[] }) {
  const max = Math.max(...days.map((d) => d.sales_total), 0)
  if (max <= 0) {
    return <div className="py-8 text-center text-sm text-muted-foreground">لا مبيعات في هذه الفترة.</div>
  }
  const labelStep = Math.ceil(days.length / 8)
  const peak = days.reduce((a, b) => (b.sales_total > a.sales_total ? b : a), days[0])

  return (
    <div dir="ltr">
      <div className="flex h-40 items-end gap-px border-b border-border sm:gap-0.5">
        {days.map((d) => (
          <div
            key={d.day}
            className="group flex h-full min-w-0 flex-1 flex-col items-center justify-end"
            title={`${format(parseDay(d.day), 'EEEE d MMM', { locale: ar })} — ${money.format(d.sales_total)} (${toArabicDigits(d.sales_count)} فاتورة)`}
          >
            {d === peak && (
              <span className="mb-0.5 hidden truncate text-[10px] font-medium text-muted-foreground sm:block">
                {money.format(d.sales_total)}
              </span>
            )}
            <div
              className="w-full max-w-6 rounded-t-[4px] bg-primary/75 transition-colors group-hover:bg-primary"
              style={{ height: `${Math.max((d.sales_total / max) * 100, d.sales_total > 0 ? 2 : 0)}%` }}
            />
          </div>
        ))}
      </div>
      <div className="mt-1 flex gap-px sm:gap-0.5">
        {days.map((d, i) => (
          <div key={d.day} className="min-w-0 flex-1 text-center text-[10px] text-muted-foreground">
            {i % labelStep === 0 ? (
              <span className="truncate">
                {format(parseDay(d.day), 'd/M', { locale: ar })}
              </span>
            ) : null}
          </div>
        ))}
      </div>
    </div>
  )
}

// --- الأصناف ---

const SORTS: { key: ReportSort; label: string }[] = [
  { key: 'revenue', label: 'الأعلى قيمةً' },
  { key: 'qty', label: 'الأعلى كميةً' },
  { key: 'profit', label: 'الأعلى ربحًا' },
]

function ProductsView({
  tenantId,
  from,
  to,
  branchId,
  branches,
  onBranchChange,
}: {
  tenantId?: string
  from?: string
  to?: string
  branchId?: string
  branches: BranchOption[]
  onBranchChange: (id: string | undefined) => void
}) {
  const navigate = useNavigate()
  const groupsQuery = useCatalogGroups(tenantId)
  const [sort, setSort] = useState<ReportSort>('revenue')
  const [groupId, setGroupId] = useState<string | undefined>(undefined)
  const [page, setPage] = useState(1)

  const filterKey = `${from ?? ''} ${to ?? ''} ${branchId ?? ''} ${groupId ?? ''} ${sort}`
  const [lastFilterKey, setLastFilterKey] = useState(filterKey)
  if (filterKey !== lastFilterKey) {
    setLastFilterKey(filterKey)
    setPage(1)
  }

  const query = useReportProducts(tenantId, {
    from,
    to,
    branchId,
    groupId,
    sort,
    page,
    pageSize: PAGE_SIZE,
  })

  const notSubscribed = query.error instanceof ApiError && query.error.status === 402
  const gatewayError = query.error instanceof ApiError && query.error.status !== 402

  return (
    <div>
      <div className="mb-4 flex flex-wrap items-center gap-3">
        <div className="inline-flex rounded-lg border border-border bg-card/50 p-1">
          {SORTS.map((s) => (
            <button
              key={s.key}
              type="button"
              onClick={() => setSort(s.key)}
              className={cn(
                'rounded-md px-3 py-1.5 text-sm font-medium transition-colors',
                sort === s.key
                  ? 'bg-accent text-primary'
                  : 'text-muted-foreground hover:text-foreground',
              )}
            >
              {s.label}
            </button>
          ))}
        </div>
        <select
          className={cn(selectClass, 'w-40')}
          value={groupId ?? ''}
          onChange={(e) => setGroupId(e.target.value || undefined)}
        >
          <option value="">كل المجموعات</option>
          {(groupsQuery.data?.data ?? []).map((g) => (
            <option key={g.id} value={g.id}>
              {g.name}
            </option>
          ))}
        </select>
        <BranchSelect branchId={branchId} branches={branches} onChange={onBranchChange} />
      </div>

      {notSubscribed ? (
        <NotSubscribed />
      ) : gatewayError ? (
        <ErrorState
          message="تعذّر الوصول إلى بيانات التقارير الآن."
          onRetry={() => void query.refetch()}
        />
      ) : query.isLoading ? (
        <LoadingState rows={5} />
      ) : !query.data || query.data.data.items.length === 0 ? (
        <EmptyState
          icon={ReportsIcon}
          title="لا مبيعات في هذه الفترة"
          description="لم تُسجَّل مبيعات مطابقة للفلاتر في الفترة المحددة."
        />
      ) : (
        <>
          <div className="rounded-xl border border-border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>الكود</TableHead>
                  <TableHead>الاسم</TableHead>
                  <TableHead>المجموعة</TableHead>
                  <TableHead>الكمية المباعة</TableHead>
                  <TableHead>الإيراد</TableHead>
                  <TableHead>الربح</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {query.data.data.items.map((p) => (
                  <TableRow
                    key={p.id}
                    tabIndex={0}
                    onClick={() => navigate(`/tenants/${tenantId}/catalog/${p.id}`)}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter') navigate(`/tenants/${tenantId}/catalog/${p.id}`)
                    }}
                    className="cursor-pointer"
                  >
                    <TableCell className="dir-ltr text-start font-mono text-xs">
                      {toArabicDigits(p.code)}
                    </TableCell>
                    <TableCell className="font-medium">{p.name}</TableCell>
                    <TableCell className="text-muted-foreground">{p.group_name ?? '—'}</TableCell>
                    <TableCell>
                      {toArabicDigits(p.qty_sold)}
                      {p.unit && <span className="text-xs text-muted-foreground"> {p.unit}</span>}
                    </TableCell>
                    <TableCell>{money.format(p.revenue)}</TableCell>
                    <TableCell className={cn(p.profit < 0 && 'font-semibold text-danger')}>
                      {money.format(p.profit)}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
          {query.data.data.total > 0 && (
            <Pagination
              page={page}
              pageSize={PAGE_SIZE}
              total={query.data.data.total}
              onPageChange={setPage}
            />
          )}
        </>
      )}
    </div>
  )
}

// --- الفروع ---

function BranchesView({ tenantId, from, to }: { tenantId?: string; from?: string; to?: string }) {
  const navigate = useNavigate()
  const query = useReportBranches(tenantId, { from, to })

  const notSubscribed = query.error instanceof ApiError && query.error.status === 402
  const gatewayError = query.error instanceof ApiError && query.error.status !== 402

  if (notSubscribed) return <NotSubscribed />
  if (gatewayError) {
    return (
      <ErrorState
        message="تعذّر الوصول إلى بيانات التقارير الآن."
        onRetry={() => void query.refetch()}
      />
    )
  }
  if (query.isLoading || !query.data) return <LoadingState rows={4} />
  if (query.data.data.branches.length === 0) {
    return (
      <EmptyState
        icon={ReportsIcon}
        title="لا توجد فروع"
        description="أضف فرعًا لتظهر أرقامه هنا."
      />
    )
  }

  const rows = query.data.data.branches
  const sum = rows.reduce(
    (acc, b) => ({
      sales: acc.sales + b.sales_total,
      count: acc.count + b.sales_count,
      refunds: acc.refunds + b.refunds_total,
      profit: acc.profit + b.profit,
    }),
    { sales: 0, count: 0, refunds: 0, profit: 0 },
  )

  return (
    <div>
      <div className="mb-4 flex justify-end">
        <Freshness source={query.data.source} asOf={query.data.as_of} />
      </div>
      <div className="rounded-xl border border-border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>الفرع</TableHead>
              <TableHead>المبيعات</TableHead>
              <TableHead>الفواتير</TableHead>
              <TableHead>متوسط الفاتورة</TableHead>
              <TableHead>المرتجعات</TableHead>
              <TableHead>الصافي</TableHead>
              <TableHead>الربح</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((b) => (
              <TableRow
                key={b.branch_id}
                tabIndex={0}
                onClick={() => navigate(`/tenants/${tenantId}/branches/${b.branch_id}`)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') navigate(`/tenants/${tenantId}/branches/${b.branch_id}`)
                }}
                className="cursor-pointer"
              >
                <TableCell>
                  <div className="flex items-center gap-2 font-medium">
                    <HealthDot health={b.health} />
                    {b.branch_name}
                  </div>
                </TableCell>
                <TableCell>{money.format(b.sales_total)}</TableCell>
                <TableCell>{toArabicDigits(b.sales_count)}</TableCell>
                <TableCell>
                  {money.format(b.sales_count > 0 ? b.sales_total / b.sales_count : 0)}
                </TableCell>
                <TableCell
                  className={cn(b.refunds_total > 0 ? 'text-danger' : 'text-muted-foreground')}
                >
                  {money.format(b.refunds_total)}
                </TableCell>
                <TableCell>{money.format(b.sales_total - b.refunds_total)}</TableCell>
                <TableCell className={cn(b.profit < 0 && 'font-semibold text-danger')}>
                  {money.format(b.profit)}
                </TableCell>
              </TableRow>
            ))}
            <TableRow className="bg-muted/30 font-medium">
              <TableCell>الإجمالي</TableCell>
              <TableCell>{money.format(sum.sales)}</TableCell>
              <TableCell>{toArabicDigits(sum.count)}</TableCell>
              <TableCell>{money.format(sum.count > 0 ? sum.sales / sum.count : 0)}</TableCell>
              <TableCell>{money.format(sum.refunds)}</TableCell>
              <TableCell>{money.format(sum.sales - sum.refunds)}</TableCell>
              <TableCell>{money.format(sum.profit)}</TableCell>
            </TableRow>
          </TableBody>
        </Table>
      </div>
    </div>
  )
}

// --- الموظفون ---

function StaffView({
  tenantId,
  from,
  to,
  branchId,
  branches,
  onBranchChange,
}: {
  tenantId?: string
  from?: string
  to?: string
  branchId?: string
  branches: BranchOption[]
  onBranchChange: (id: string | undefined) => void
}) {
  const query = useReportStaff(tenantId, { from, to, branchId })

  const notSubscribed = query.error instanceof ApiError && query.error.status === 402
  const gatewayError = query.error instanceof ApiError && query.error.status !== 402

  return (
    <div>
      <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
        <BranchSelect branchId={branchId} branches={branches} onChange={onBranchChange} />
        {query.data && <Freshness source={query.data.source} asOf={query.data.as_of} />}
      </div>

      {notSubscribed ? (
        <NotSubscribed />
      ) : gatewayError ? (
        <ErrorState
          message="تعذّر الوصول إلى بيانات التقارير الآن."
          onRetry={() => void query.refetch()}
        />
      ) : query.isLoading ? (
        <LoadingState rows={4} />
      ) : !query.data || query.data.data.staff.length === 0 ? (
        <EmptyState
          icon={UsersIcon}
          title="لا مبيعات في هذه الفترة"
          description="لم يُسجّل أي موظف مبيعات في الفترة المحددة."
        />
      ) : (
        <div className="rounded-xl border border-border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>الموظف</TableHead>
                <TableHead>الفواتير</TableHead>
                <TableHead>المبيعات</TableHead>
                <TableHead>متوسط الفاتورة</TableHead>
                <TableHead>المرتجعات</TableHead>
                <TableHead>الصافي</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {query.data.data.staff.map((u) => (
                <TableRow key={u.user_id}>
                  <TableCell className="font-medium">{u.user_name}</TableCell>
                  <TableCell>{toArabicDigits(u.sales_count)}</TableCell>
                  <TableCell>{money.format(u.sales_total)}</TableCell>
                  <TableCell>
                    {money.format(u.sales_count > 0 ? u.sales_total / u.sales_count : 0)}
                  </TableCell>
                  <TableCell
                    className={cn(u.refunds_total > 0 ? 'text-danger' : 'text-muted-foreground')}
                  >
                    {money.format(u.refunds_total)}
                  </TableCell>
                  <TableCell>{money.format(u.sales_total - u.refunds_total)}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </div>
  )
}

// --- الورديات ---

const SHIFT_STATUSES: { key: ShiftStatusFilter | undefined; label: string }[] = [
  { key: undefined, label: 'الكل' },
  { key: 'open', label: 'مفتوحة' },
  { key: 'closed', label: 'مغلقة' },
]

/** Over/short against the expected drawer cash: positive = over, negative = short. */
function DiffValue({ value }: { value: number | null }) {
  if (value === null) return <span className="text-muted-foreground">—</span>
  if (value === 0) return <span className="text-muted-foreground">{money.format(0)}</span>
  return (
    <span className={cn('font-semibold', value < 0 ? 'text-danger' : 'text-success')}>
      {value > 0 ? '+' : ''}
      {money.format(value)}
    </span>
  )
}

function ShiftsView({
  tenantId,
  from,
  to,
  branchId,
  branches,
  onBranchChange,
}: {
  tenantId?: string
  from?: string
  to?: string
  branchId?: string
  branches: BranchOption[]
  onBranchChange: (id: string | undefined) => void
}) {
  const [searchParams, setSearchParams] = useSearchParams()
  const [status, setStatus] = useState<ShiftStatusFilter | undefined>(undefined)
  const [userId, setUserId] = useState<string | undefined>(undefined)
  const [page, setPage] = useState(1)

  const filterKey = `${from ?? ''} ${to ?? ''} ${branchId ?? ''} ${status ?? ''} ${userId ?? ''}`
  const [lastFilterKey, setLastFilterKey] = useState(filterKey)
  if (filterKey !== lastFilterKey) {
    setLastFilterKey(filterKey)
    setPage(1)
  }

  const query = useReportShifts(tenantId, {
    from,
    to,
    branchId,
    status,
    userId,
    page,
    pageSize: PAGE_SIZE,
  })

  // The open shift lives in the URL so a Z report can be linked to directly.
  const shiftId = searchParams.get('shift') ?? undefined
  const setShiftId = (id: string | undefined) => {
    const next = new URLSearchParams(searchParams)
    if (id) next.set('shift', id)
    else next.delete('shift')
    setSearchParams(next, { replace: true })
  }

  const branchName = (id: string) => branches.find((b) => b.ID === id)?.Name ?? '—'
  const notSubscribed = query.error instanceof ApiError && query.error.status === 402
  const gatewayError = query.error instanceof ApiError && query.error.status !== 402
  const r = query.data?.data

  return (
    <div>
      <div className="mb-4 flex flex-wrap items-center gap-3">
        <div className="inline-flex rounded-lg border border-border bg-card/50 p-1">
          {SHIFT_STATUSES.map((s) => (
            <button
              key={s.label}
              type="button"
              onClick={() => setStatus(s.key)}
              className={cn(
                'rounded-md px-3 py-1.5 text-sm font-medium transition-colors',
                status === s.key
                  ? 'bg-accent text-primary'
                  : 'text-muted-foreground hover:text-foreground',
              )}
            >
              {s.label}
            </button>
          ))}
        </div>
        <select
          className={cn(selectClass, 'w-44')}
          value={userId ?? ''}
          onChange={(e) => setUserId(e.target.value || undefined)}
        >
          <option value="">كل الكاشيرات</option>
          {(r?.cashiers ?? []).map((c) => (
            <option key={c.user_id} value={c.user_id}>
              {c.name}
            </option>
          ))}
        </select>
        <BranchSelect branchId={branchId} branches={branches} onChange={onBranchChange} />
        {query.data && (
          <Freshness className="ms-auto" source={query.data.source} asOf={query.data.as_of} />
        )}
      </div>

      {notSubscribed ? (
        <NotSubscribed />
      ) : gatewayError ? (
        <ErrorState
          message="تعذّر الوصول إلى بيانات التقارير الآن."
          onRetry={() => void query.refetch()}
        />
      ) : query.isLoading || !r ? (
        <LoadingState rows={5} />
      ) : r.total === 0 ? (
        <EmptyState
          icon={ReportsIcon}
          title="لا ورديات في هذه الفترة"
          description="تظهر هنا ورديات الفروع التي تعمل بنظام الورديات فقط."
        />
      ) : (
        <>
          <div className="mb-4 grid grid-cols-2 gap-3 lg:grid-cols-5">
            <KpiTile
              label="الورديات"
              value={
                r.summary.open_count > 0
                  ? `${toArabicDigits(r.summary.shift_count)} (${toArabicDigits(r.summary.open_count)} مفتوحة)`
                  : toArabicDigits(r.summary.shift_count)
              }
            />
            <KpiTile label="المبيعات" value={money.format(r.summary.sales_total)} />
            <KpiTile
              label="المرتجعات"
              value={money.format(r.summary.refunds_total)}
              tone={r.summary.refunds_total > 0 ? 'danger' : undefined}
            />
            <KpiTile
              label="صافي العجز / الزيادة"
              value={`${r.summary.net_difference > 0 ? '+' : ''}${money.format(r.summary.net_difference)}`}
              tone={r.summary.net_difference < 0 ? 'danger' : undefined}
            />
            <KpiTile
              label="ورديات بها عجز"
              value={
                r.summary.short_count > 0
                  ? `${toArabicDigits(r.summary.short_count)} · ${money.format(r.summary.short_total)}`
                  : toArabicDigits(0)
              }
              tone={r.summary.short_count > 0 ? 'danger' : undefined}
            />
          </div>

          <div className="rounded-xl border border-border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>الوردية</TableHead>
                  <TableHead>الفرع</TableHead>
                  <TableHead>الكاشير</TableHead>
                  <TableHead>الفتح</TableHead>
                  <TableHead>الإغلاق</TableHead>
                  <TableHead>المبيعات</TableHead>
                  <TableHead>المرتجعات</TableHead>
                  <TableHead>النقدية المتوقعة</TableHead>
                  <TableHead>النقدية الفعلية</TableHead>
                  <TableHead>الفرق</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {r.items.map((s) => (
                  <TableRow
                    key={s.id}
                    tabIndex={0}
                    onClick={() => setShiftId(s.id)}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter') setShiftId(s.id)
                    }}
                    className="cursor-pointer"
                  >
                    <TableCell className="font-medium">#{toArabicDigits(s.num)}</TableCell>
                    <TableCell>{branchName(s.branch_id)}</TableCell>
                    <TableCell>{s.opened_by}</TableCell>
                    <TableCell className="whitespace-nowrap">{fmtDateTime(s.opened_at)}</TableCell>
                    <TableCell className="whitespace-nowrap">
                      {s.is_open ? (
                        <Badge tone="info">مفتوحة</Badge>
                      ) : (
                        <span className="inline-flex items-center gap-2">
                          {fmtDateTime(s.closed_at)}
                          {s.is_force_closed && <Badge tone="warning">إغلاق إجباري</Badge>}
                        </span>
                      )}
                    </TableCell>
                    <TableCell>{money.format(s.sales_total)}</TableCell>
                    <TableCell
                      className={cn(s.refunds_total > 0 ? 'text-danger' : 'text-muted-foreground')}
                    >
                      {money.format(s.refunds_total)}
                    </TableCell>
                    <TableCell>{money.format(s.expected_cash)}</TableCell>
                    <TableCell>
                      {s.actual_cash === null ? (
                        <span className="text-muted-foreground">—</span>
                      ) : (
                        money.format(s.actual_cash)
                      )}
                    </TableCell>
                    <TableCell>
                      <DiffValue value={s.difference} />
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
          <Pagination page={page} pageSize={PAGE_SIZE} total={r.total} onPageChange={setPage} />
        </>
      )}

      <ShiftDetailDialog
        tenantId={tenantId}
        shiftId={shiftId}
        onClose={() => setShiftId(undefined)}
      />
    </div>
  )
}

function duration(fromIso: string, toIso: string | null): string {
  const end = toIso ? new Date(toIso) : new Date()
  const mins = Math.max(0, Math.round((end.getTime() - new Date(fromIso).getTime()) / 60000))
  const h = Math.floor(mins / 60)
  const m = mins % 60
  return h > 0 ? `${toArabicDigits(h)} س ${toArabicDigits(m)} د` : `${toArabicDigits(m)} د`
}

function DetailRow({
  label,
  value,
  strong,
}: {
  label: string
  value: ReactNode
  strong?: boolean
}) {
  return (
    <div className={cn('flex items-center justify-between py-1 text-sm', strong && 'font-semibold')}>
      <span className={cn(!strong && 'text-muted-foreground')}>{label}</span>
      <span>{value}</span>
    </div>
  )
}

/** The online Z report (X while the shift is still open). */
function ShiftDetailDialog({
  tenantId,
  shiftId,
  onClose,
}: {
  tenantId?: string
  shiftId?: string
  onClose: () => void
}) {
  const query = useReportShiftDetail(tenantId, shiftId)
  const d = query.data?.data

  return (
    <Dialog open={!!shiftId} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-h-[90vh] max-w-2xl overflow-y-auto">
        <DialogHeader>
          <DialogTitle className="flex flex-wrap items-center gap-2">
            {d ? `وردية #${toArabicDigits(d.num)}` : 'الوردية'}
            {d?.is_open && <Badge tone="info">مفتوحة · تقرير X</Badge>}
            {d && !d.is_open && <Badge tone="muted">مغلقة · تقرير Z</Badge>}
            {d?.is_force_closed && <Badge tone="warning">إغلاق إجباري</Badge>}
          </DialogTitle>
          {d && (
            <DialogDescription>
              {d.branch_name || '—'} · {d.opened_by} · {fmtDateTime(d.opened_at)}
              {d.closed_at && ` ← ${fmtDateTime(d.closed_at)}`} · {duration(d.opened_at, d.closed_at)}
            </DialogDescription>
          )}
        </DialogHeader>

        {query.error ? (
          <ErrorState
            message={
              query.error instanceof ApiError && query.error.status === 404
                ? 'لم يتم العثور على هذه الوردية.'
                : 'تعذّر تحميل تقرير الوردية الآن.'
            }
            onRetry={() => void query.refetch()}
          />
        ) : !d ? (
          <LoadingState rows={4} />
        ) : (
          <div className="space-y-4">
            <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
              <KpiTile label="المبيعات" value={money.format(d.sales_total)} />
              <KpiTile label="عدد الفواتير" value={toArabicDigits(d.sales_count)} />
              <KpiTile
                label="المرتجعات"
                value={`${money.format(d.refunds_total)} (${toArabicDigits(d.refunds_count)})`}
                tone={d.refunds_total > 0 ? 'danger' : undefined}
              />
              <KpiTile label="الصافي" value={money.format(d.sales_total - d.refunds_total)} />
            </div>

            <Card className="p-4">
              <div className="mb-1 text-xs font-medium uppercase tracking-wide text-muted-foreground">
                طرق الدفع
              </div>
              <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
                <TenderCell label="نقدًا" value={d.tender.cash} total={d.sales_total} />
                <TenderCell label="بنك / بطاقة" value={d.tender.bank} total={d.sales_total} />
                <TenderCell label="محفظة" value={d.tender.wallet} total={d.sales_total} />
                <TenderCell label="آجل" value={d.tender.credit} total={d.sales_total} />
              </div>
            </Card>

            <div className="grid gap-3 sm:grid-cols-2">
              <Card className="p-4">
                <div className="mb-2 text-xs font-medium uppercase tracking-wide text-muted-foreground">
                  حركة الدرج
                </div>
                <DetailRow label="وارد نقدي" value={money.format(d.cash_in)} />
                <DetailRow label="منصرف نقدي" value={money.format(d.cash_out)} />
                <DetailRow label="مصروفات" value={money.format(d.expenses)} />
                <DetailRow label="إيرادات أخرى" value={money.format(d.revenue)} />
              </Card>
              <Card className="p-4">
                <div className="mb-2 text-xs font-medium uppercase tracking-wide text-muted-foreground">
                  مطابقة النقدية
                </div>
                <DetailRow label="رصيد الافتتاح" value={money.format(d.opening_cash)} />
                <DetailRow
                  label={d.is_open ? 'المتوقع حتى الآن' : 'المتوقع'}
                  value={money.format(d.expected_cash)}
                />
                <DetailRow
                  label="الفعلي (المعدود)"
                  value={d.actual_cash === null ? '—' : money.format(d.actual_cash)}
                />
                <div className="mt-1 border-t border-border pt-1">
                  <DetailRow label="الفرق" value={<DiffValue value={d.difference} />} strong />
                </div>
              </Card>
            </div>

            <div className="flex flex-wrap gap-x-6 gap-y-1 text-sm text-muted-foreground">
              <span>العملاء: {toArabicDigits(d.customers_served)}</span>
              <span>تسويات المخزون: {toArabicDigits(d.inventory_adjustments)}</span>
              <span className="dir-ltr">{d.workstation_id}</span>
              {d.closed_by && d.closed_by !== d.opened_by && <span>أغلقها: {d.closed_by}</span>}
            </div>

            {(d.open_note || d.close_note) && (
              <Card className="space-y-1 p-4 text-sm">
                {d.open_note && <DetailRow label="ملاحظة الفتح" value={d.open_note} />}
                {d.close_note && <DetailRow label="ملاحظة الإغلاق" value={d.close_note} />}
              </Card>
            )}
          </div>
        )}
      </DialogContent>
    </Dialog>
  )
}

// --- المخزون (snapshot — reuses the slice-4 data, zero new backend) ---

function InventoryView({ tenantId }: { tenantId?: string }) {
  const query = useInventoryBranches(tenantId)

  const notSubscribed = query.error instanceof ApiError && query.error.status === 402
  const gatewayError = query.error instanceof ApiError && query.error.status !== 402

  if (notSubscribed) return <NotSubscribed />
  if (gatewayError) {
    return (
      <ErrorState
        message="تعذّر الوصول إلى بيانات المخزون الآن."
        onRetry={() => void query.refetch()}
      />
    )
  }
  if (query.isLoading || !query.data) return <LoadingState rows={3} />

  const { totals } = query.data.data
  const attention = totals.negative_count + totals.out_count + totals.low_count

  return (
    <div>
      <div className="mb-5 grid grid-cols-2 gap-3 sm:grid-cols-4">
        <KpiTile label="قيمة المخزون" value={money.format(totals.stock_value)} />
        <KpiTile
          label="سالب"
          value={toArabicDigits(totals.negative_count)}
          tone={totals.negative_count > 0 ? 'danger' : undefined}
        />
        <KpiTile
          label="نفاد"
          value={toArabicDigits(totals.out_count)}
          tone={totals.out_count > 0 ? 'danger' : undefined}
        />
        <KpiTile label="منخفض" value={toArabicDigits(totals.low_count)} />
      </div>

      <div className="grid gap-3 sm:grid-cols-2">
        <Link
          to={`/tenants/${tenantId}/inventory?view=branches`}
          className="flex items-center gap-2.5 rounded-xl border border-border bg-card/50 p-4 text-sm transition-colors hover:bg-accent/50"
        >
          <InventoryIcon className="size-5 shrink-0 text-muted-foreground" />
          <span className="min-w-0 flex-1">
            <span className="block font-medium">المخزون حسب الفرع</span>
            <span className="text-xs text-muted-foreground">
              قيمة المخزون وعدد الأصناف لكل فرع ومخزن.
            </span>
          </span>
          <ArrowLeading className="size-4 shrink-0 text-muted-foreground" />
        </Link>
        <Link
          to={`/tenants/${tenantId}/inventory?view=attention`}
          className="flex items-center gap-2.5 rounded-xl border border-border bg-card/50 p-4 text-sm transition-colors hover:bg-accent/50"
        >
          <InventoryIcon className="size-5 shrink-0 text-muted-foreground" />
          <span className="min-w-0 flex-1">
            <span className="block font-medium">ما يحتاج انتباهًا</span>
            <span className="text-xs text-muted-foreground">
              {attention > 0
                ? `${toArabicDigits(attention)} صنفًا بحاجة إلى مراجعة الآن.`
                : 'كل الفروع بخير حاليًا.'}
            </span>
          </span>
          <ArrowLeading className="size-4 shrink-0 text-muted-foreground" />
        </Link>
      </div>
    </div>
  )
}
