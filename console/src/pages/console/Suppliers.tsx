import { useEffect, useState } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { format } from 'date-fns'
import { ar } from 'date-fns/locale'
import { toast } from 'sonner'
import { api, ApiError } from '@/lib/api'
import { errorMessage } from '@/lib/auth'
import {
  useBundle,
  useCustomerGroups,
  useSupplierInsights,
  useSuppliers,
} from '@/lib/hooks'
import { toArabicDigits } from '@/lib/format'
import { cn } from '@/lib/utils'
import type {
  CreditWarningRow,
  CustomerGrowthDay,
  CustomerInsightRow,
  CustomerRef,
  SupplierDebtFilter,
  SupplierRow,
} from '@/lib/types'
import { SupplierBulkActionsBar } from '@/components/SupplierBulkActionsBar'
import { CreateSupplierDialog } from '@/components/CreateSupplierDialog'
import { HealthDot } from '@/components/HealthDot'
import { ImportSuppliersDialog } from '@/components/ImportSuppliersDialog'
import { PageHeader } from '@/components/PageHeader'
import { Pagination } from '@/components/Pagination'
import { PeriodPicker } from '@/components/PeriodPicker'
import { LoadingState, EmptyState, ErrorState } from '@/components/States'
import { AddIcon, ArrowLeading, DownloadIcon, SearchIcon, SupplierIcon } from '@/components/icon'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
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

const DEBT_OPTIONS: { value: SupplierDebtFilter | ''; label: string }[] = [
  { value: '', label: 'كل الأرصدة' },
  { value: 'has_debt', label: 'عليه رصيد' },
  { value: 'credit', label: 'موردون آجل' },
  { value: 'exceeding', label: 'تجاوز الحد الائتماني' },
]

type ViewKey = 'list' | 'insights'
const VIEWS: { key: ViewKey; label: string }[] = [
  { key: 'list', label: 'قائمة الموردين' },
  { key: 'insights', label: 'رؤى وتحليلات' },
]

export function Suppliers() {
  const { tenantId } = useParams<'tenantId'>()
  const { data: bundle } = useBundle(tenantId)
  const [searchParams, setSearchParams] = useSearchParams()

  const view: ViewKey = (searchParams.get('view') as ViewKey | null) ?? 'list'
  const setView = (v: ViewKey) => {
    const next = new URLSearchParams(searchParams)
    next.set('view', v)
    setSearchParams(next, { replace: true })
  }

  const [createOpen, setCreateOpen] = useState(false)

  if (!bundle) return <LoadingState />

  const branches = (bundle.Branches ?? []).filter((b) => b.Status === 'active')

  return (
    <>
      <PageHeader
        title="الموردون"
        description="قائمة الموردين وتحليلات نشاطهم عبر كل الفروع."
        actions={
          <Button onClick={() => setCreateOpen(true)}>
            <AddIcon className="size-4" />
            مورد جديد
          </Button>
        }
      />

      {tenantId && (
        <CreateSupplierDialog tenantId={tenantId} open={createOpen} onOpenChange={setCreateOpen} />
      )}

      <div className="mb-5 inline-flex rounded-lg border border-border bg-card/50 p-1">
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

      {view === 'list' ? (
        <ListView tenantId={tenantId} branches={branches} />
      ) : (
        <InsightsView tenantId={tenantId} branches={branches} />
      )}
    </>
  )
}

// --- قائمة الموردين ---

function ListView({
  tenantId,
  branches,
}: {
  tenantId?: string
  branches: { ID: string; Name: string }[]
}) {
  const navigate = useNavigate()
  const groupsQuery = useCustomerGroups(tenantId)

  const [search, setSearch] = useState('')
  const [debouncedSearch, setDebouncedSearch] = useState('')
  const [branchId, setBranchId] = useState<string | undefined>(undefined)
  const [groupId, setGroupId] = useState<string | undefined>(undefined)
  const [active, setActive] = useState<boolean | undefined>(undefined)
  const [debt, setDebt] = useState<SupplierDebtFilter | undefined>(undefined)
  const [page, setPage] = useState(1)
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [importOpen, setImportOpen] = useState(false)
  const [exporting, setExporting] = useState(false)

  useEffect(() => {
    const t = window.setTimeout(() => setDebouncedSearch(search.trim()), 300)
    return () => window.clearTimeout(t)
  }, [search])

  // Filter changes reset to page 1 without spinner-blanking (Catalog's
  // render-time-reset pattern, not a setState-in-effect). A filter/page
  // change also clears the selection — it no longer maps to a visible page.
  const filterKey = `${debouncedSearch}\0${branchId ?? ''}\0${groupId ?? ''}\0${active ?? ''}\0${debt ?? ''}`
  const [lastFilterKey, setLastFilterKey] = useState(filterKey)
  if (filterKey !== lastFilterKey) {
    setLastFilterKey(filterKey)
    setPage(1)
    setSelected(new Set())
  }
  const [lastPage, setLastPage] = useState(page)
  if (page !== lastPage) {
    setLastPage(page)
    setSelected(new Set())
  }

  const query = useSuppliers(tenantId, {
    search: debouncedSearch || undefined,
    branchId,
    groupId,
    active,
    debt,
    page,
    pageSize: PAGE_SIZE,
  })

  const notSubscribed = query.error instanceof ApiError && query.error.status === 402
  const gatewayError = query.error instanceof ApiError && query.error.status !== 402

  const handleExport = async () => {
    if (!tenantId) return
    setExporting(true)
    try {
      const blob = await api.exportSuppliers(tenantId, {
        search: debouncedSearch || undefined,
        branchId,
        groupId,
        active,
        debt,
      })
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = 'suppliers.csv'
      a.click()
      URL.revokeObjectURL(url)
    } catch (err) {
      toast.error(errorMessage(err))
    } finally {
      setExporting(false)
    }
  }

  return (
    <div>
      <div className="mb-4 grid gap-3 sm:grid-cols-[1fr_auto_auto_auto_auto]">
        <div className="relative">
          <SearchIcon className="pointer-events-none absolute start-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="ابحث بالاسم أو الهاتف أو الكود"
            className="ps-9"
          />
        </div>
        <select
          className={cn(selectClass, 'sm:w-36')}
          value={branchId ?? ''}
          onChange={(e) => setBranchId(e.target.value || undefined)}
        >
          <option value="">كل الفروع</option>
          {branches.map((b) => (
            <option key={b.ID} value={b.ID}>
              {b.Name}
            </option>
          ))}
        </select>
        <select
          className={cn(selectClass, 'sm:w-36')}
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
        <select
          className={cn(selectClass, 'sm:w-32')}
          value={active === undefined ? '' : String(active)}
          onChange={(e) =>
            setActive(e.target.value === '' ? undefined : e.target.value === 'true')
          }
        >
          <option value="">كل الحالات</option>
          <option value="true">مُفعّل</option>
          <option value="false">مُعطّل</option>
        </select>
        <select
          className={cn(selectClass, 'sm:w-40')}
          value={debt ?? ''}
          onChange={(e) => setDebt((e.target.value || undefined) as SupplierDebtFilter | undefined)}
        >
          {DEBT_OPTIONS.map((o) => (
            <option key={o.value} value={o.value}>
              {o.label}
            </option>
          ))}
        </select>
      </div>

      <div className="mb-4 flex justify-end gap-2">
        <Button type="button" variant="outline" size="sm" onClick={() => setImportOpen(true)}>
          استيراد
        </Button>
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={exporting}
          onClick={() => void handleExport()}
        >
          <DownloadIcon className="size-4" />
          تصدير
        </Button>
      </div>

      {tenantId && (
        <ImportSuppliersDialog tenantId={tenantId} open={importOpen} onOpenChange={setImportOpen} />
      )}

      {notSubscribed ? (
        <EmptyState
          icon={SupplierIcon}
          title="لا يوجد اشتراك مزامنة"
          description="فعّل اشتراك المزامنة لعرض موردي فروعك."
        />
      ) : gatewayError ? (
        <ErrorState
          message="تعذّر الوصول إلى بيانات الموردين الآن."
          onRetry={() => void query.refetch()}
        />
      ) : (
        <>
          {tenantId && selected.size > 0 && (
            <SupplierBulkActionsBar
              tenantId={tenantId}
              selectedIds={[...selected]}
              onClear={() => setSelected(new Set())}
            />
          )}

          <SuppliersTable
            items={query.data?.data.items}
            isLoading={query.isLoading}
            selected={selected}
            onToggle={(id) =>
              setSelected((prev) => {
                const next = new Set(prev)
                if (next.has(id)) next.delete(id)
                else next.add(id)
                return next
              })
            }
            onToggleAll={(ids, checked) =>
              setSelected((prev) => {
                const next = new Set(prev)
                for (const id of ids) {
                  if (checked) next.add(id)
                  else next.delete(id)
                }
                return next
              })
            }
            onRowClick={(id) => navigate(`/tenants/${tenantId}/suppliers/${id}`)}
          />

          {query.data && query.data.data.total > 0 && (
            <Pagination
              page={page}
              pageSize={PAGE_SIZE}
              total={query.data.data.total}
              itemLabel="مورد"
              onPageChange={setPage}
            />
          )}
        </>
      )}
    </div>
  )
}

function SuppliersTable({
  items,
  isLoading,
  selected,
  onToggle,
  onToggleAll,
  onRowClick,
}: {
  items?: SupplierRow[]
  isLoading: boolean
  selected: Set<string>
  onToggle: (id: string) => void
  onToggleAll: (ids: string[], checked: boolean) => void
  onRowClick: (id: string) => void
}) {
  if (isLoading) return <LoadingState rows={5} />
  if (!items || items.length === 0) {
    return (
      <EmptyState
        icon={SupplierIcon}
        title="لا يوجد موردون"
        description="لا يوجد موردون مطابقون لبحثك أو للفلاتر المحددة."
      />
    )
  }
  const allSelected = items.every((c) => selected.has(c.id))
  return (
    <div className="rounded-xl border border-border">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead className="w-8">
              <input
                type="checkbox"
                aria-label="تحديد كل الموردين في هذه الصفحة"
                checked={allSelected}
                onChange={(e) => onToggleAll(items.map((c) => c.id), e.target.checked)}
              />
            </TableHead>
            <TableHead>الكود</TableHead>
            <TableHead>الاسم</TableHead>
            <TableHead>الفرع</TableHead>
            <TableHead>المجموعة</TableHead>
            <TableHead>الهاتف</TableHead>
            <TableHead>الرصيد</TableHead>
            <TableHead>الحد الائتماني</TableHead>
            <TableHead>الحالة</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {items.map((c) => (
            <TableRow
              key={c.id}
              tabIndex={0}
              onClick={() => onRowClick(c.id)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') onRowClick(c.id)
              }}
              className="cursor-pointer"
            >
              <TableCell onClick={(e) => e.stopPropagation()}>
                <input
                  type="checkbox"
                  aria-label={`تحديد ${c.name}`}
                  checked={selected.has(c.id)}
                  onChange={() => onToggle(c.id)}
                />
              </TableCell>
              <TableCell className="dir-ltr text-start font-mono text-xs">
                {toArabicDigits(c.num)}
              </TableCell>
              <TableCell className="font-medium">{c.name}</TableCell>
              <TableCell>
                <div className="flex items-center gap-2">
                  <HealthDot health={c.health} />
                  <span>{c.branch_name}</span>
                </div>
              </TableCell>
              <TableCell className="text-muted-foreground">{c.group_name ?? '—'}</TableCell>
              <TableCell className="dir-ltr text-start">{c.phone1}</TableCell>
              <TableCell className={cn(c.balance > 0 && 'font-semibold text-warning')}>
                {money.format(c.balance)}
              </TableCell>
              <TableCell>{c.credit_limit > 0 ? money.format(c.credit_limit) : '—'}</TableCell>
              <TableCell>
                <Badge tone={c.is_active ? 'success' : 'neutral'}>
                  {c.is_active ? 'مُفعّل' : 'مُعطّل'}
                </Badge>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

// --- رؤى وتحليلات ---

/** A YYYY-MM-DD local-date string as a Date, without a UTC shift. */
function parseDay(day: string): Date {
  return new Date(`${day}T00:00:00`)
}

/** New-suppliers-per-day bars — same CSS-flex-bar construction as Reports'
 * SalesChart (T46): theme tokens apply directly, no SVG/chart dependency. */
function GrowthChart({ days }: { days: CustomerGrowthDay[] }) {
  const max = Math.max(...days.map((d) => d.new_customers), 0)
  if (max <= 0) {
    return (
      <div className="py-8 text-center text-sm text-muted-foreground">
        لا موردون جدد في هذه الفترة.
      </div>
    )
  }
  const labelStep = Math.ceil(days.length / 8)
  const peak = days.reduce((a, b) => (b.new_customers > a.new_customers ? b : a), days[0])

  return (
    <div dir="ltr">
      <div className="flex h-40 items-end gap-px border-b border-border sm:gap-0.5">
        {days.map((d) => (
          <div
            key={d.day}
            className="group flex h-full min-w-0 flex-1 flex-col items-center justify-end"
            title={`${format(parseDay(d.day), 'EEEE d MMM', { locale: ar })} — ${toArabicDigits(d.new_customers)} مورد جديد`}
          >
            {d === peak && (
              <span className="mb-0.5 hidden truncate text-[10px] font-medium text-muted-foreground sm:block">
                {toArabicDigits(d.new_customers)}
              </span>
            )}
            <div
              className="w-full max-w-6 rounded-t-[4px] bg-primary/75 transition-colors group-hover:bg-primary"
              style={{ height: `${Math.max((d.new_customers / max) * 100, d.new_customers > 0 ? 2 : 0)}%` }}
            />
          </div>
        ))}
      </div>
      <div className="mt-1 flex gap-px sm:gap-0.5">
        {days.map((d, i) => (
          <div key={d.day} className="min-w-0 flex-1 text-center text-[10px] text-muted-foreground">
            {i % labelStep === 0 ? <span className="truncate">{format(parseDay(d.day), 'd/M', { locale: ar })}</span> : null}
          </div>
        ))}
      </div>
    </div>
  )
}

function InsightCard({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <Card className="p-4">
      <h3 className="mb-3 font-display text-sm font-bold">{title}</h3>
      {children}
    </Card>
  )
}

function RankedSuppliersList({
  tenantId,
  rows,
  emptyLabel,
}: {
  tenantId?: string
  rows: CustomerInsightRow[]
  emptyLabel: string
}) {
  if (rows.length === 0) {
    return <p className="text-sm text-muted-foreground">{emptyLabel}</p>
  }
  return (
    <ul className="space-y-1.5">
      {rows.map((r, i) => (
        <li key={r.id}>
          <Link
            to={`/tenants/${tenantId}/suppliers/${r.id}`}
            className="flex items-center gap-2.5 rounded-lg px-2 py-1.5 text-sm transition-colors hover:bg-accent/60"
          >
            <span className="w-5 shrink-0 text-center text-xs text-muted-foreground">
              {toArabicDigits(i + 1)}
            </span>
            <span className="min-w-0 flex-1 truncate font-medium">{r.name}</span>
            <span className="shrink-0 text-muted-foreground">{money.format(r.amount)}</span>
          </Link>
        </li>
      ))}
    </ul>
  )
}

function SupplierRefsList({
  tenantId,
  count,
  items,
  emptyLabel,
}: {
  tenantId?: string
  count: number
  items: CustomerRef[]
  emptyLabel: string
}) {
  return (
    <div>
      <div className="mb-2 font-display text-2xl font-bold">{toArabicDigits(count)}</div>
      {items.length === 0 ? (
        <p className="text-sm text-muted-foreground">{emptyLabel}</p>
      ) : (
        <ul className="space-y-1">
          {items.map((c) => (
            <li key={c.id}>
              <Link
                to={`/tenants/${tenantId}/suppliers/${c.id}`}
                className="block truncate rounded-lg px-2 py-1 text-sm transition-colors hover:bg-accent/60 hover:text-primary"
              >
                {c.name}
              </Link>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

function CreditWarningsList({
  tenantId,
  rows,
}: {
  tenantId?: string
  rows: CreditWarningRow[]
}) {
  if (rows.length === 0) {
    return <p className="text-sm text-muted-foreground">لا يوجد موردون يقتربون من حدهم الائتماني.</p>
  }
  return (
    <ul className="space-y-1.5">
      {rows.map((r) => (
        <li key={r.id}>
          <Link
            to={`/tenants/${tenantId}/suppliers/${r.id}`}
            className={cn(
              'flex items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-sm transition-colors',
              r.level === 'exceeding'
                ? 'bg-danger/5 hover:bg-danger/10'
                : 'bg-warning/5 hover:bg-warning/10',
            )}
          >
            <span className="min-w-0 flex-1 truncate font-medium">{r.name}</span>
            <span
              className={cn(
                'shrink-0 text-xs font-medium',
                r.level === 'exceeding' ? 'text-danger' : 'text-warning',
              )}
            >
              {money.format(r.balance)} / {money.format(r.credit_limit)}
            </span>
            <ArrowLeading className="size-3.5 shrink-0 text-muted-foreground" />
          </Link>
        </li>
      ))}
    </ul>
  )
}

function InsightsView({
  tenantId,
  branches,
}: {
  tenantId?: string
  branches: { ID: string; Name: string }[]
}) {
  const [searchParams, setSearchParams] = useSearchParams()
  const branchId = searchParams.get('branch') ?? undefined
  const setBranchId = (id: string | undefined) => {
    const next = new URLSearchParams(searchParams)
    if (id) next.set('branch', id)
    else next.delete('branch')
    setSearchParams(next, { replace: true })
  }

  const from = searchParams.get('from') ?? undefined
  const to = searchParams.get('to') ?? undefined
  const query = useSupplierInsights(tenantId, { branchId, from, to })

  const notSubscribed = query.error instanceof ApiError && query.error.status === 402
  const gatewayError = query.error instanceof ApiError && query.error.status !== 402

  return (
    <div>
      <div className="mb-4 flex flex-wrap items-center gap-3">
        <PeriodPicker />
        <select
          className={cn(selectClass, 'w-40')}
          value={branchId ?? ''}
          onChange={(e) => setBranchId(e.target.value || undefined)}
        >
          <option value="">كل الفروع</option>
          {branches.map((b) => (
            <option key={b.ID} value={b.ID}>
              {b.Name}
            </option>
          ))}
        </select>
      </div>

      {notSubscribed ? (
        <EmptyState
          icon={SupplierIcon}
          title="لا يوجد اشتراك مزامنة"
          description="فعّل اشتراك المزامنة لعرض تحليلات الموردين."
        />
      ) : gatewayError ? (
        <ErrorState
          message="تعذّر الوصول إلى بيانات التحليلات الآن."
          onRetry={() => void query.refetch()}
        />
      ) : query.isLoading || !query.data ? (
        <LoadingState rows={4} />
      ) : (
        <div className="grid gap-4 md:grid-cols-2">
          <InsightCard title="نمو الموردين الجدد">
            <GrowthChart days={query.data.data.growth_over_time} />
          </InsightCard>

          <InsightCard title="موردون جدد هذا الشهر">
            <SupplierRefsList
              tenantId={tenantId}
              count={query.data.data.new_this_month.count}
              items={query.data.data.new_this_month.items}
              emptyLabel="لا يوجد موردون جدد هذا الشهر."
            />
          </InsightCard>

          <InsightCard title="أعلى الموردين (الفترة المحددة)">
            <RankedSuppliersList
              tenantId={tenantId}
              rows={query.data.data.top_customers}
              emptyLabel="لا توجد مشتريات من موردين في هذه الفترة."
            />
          </InsightCard>

          <InsightCard title="الأعلى تعاملًا (كل الأوقات)">
            <RankedSuppliersList
              tenantId={tenantId}
              rows={query.data.data.highest_spenders}
              emptyLabel="لا توجد بيانات تعامل بعد."
            />
          </InsightCard>

          <InsightCard title="موردون غير نشطين">
            <SupplierRefsList
              tenantId={tenantId}
              count={query.data.data.inactive.count}
              items={query.data.data.inactive.items}
              emptyLabel="كل الموردين نشطون."
            />
          </InsightCard>

          <InsightCard title="اقتراب/تجاوز الحد الائتماني">
            <CreditWarningsList tenantId={tenantId} rows={query.data.data.credit_limit_warnings} />
          </InsightCard>
        </div>
      )}
    </div>
  )
}
