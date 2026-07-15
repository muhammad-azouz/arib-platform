import { useEffect, useState } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { ApiError } from '@/lib/api'
import {
  useBundle,
  useCatalogGroups,
  useInventoryAttention,
  useInventoryBranches,
  useInventoryProducts,
} from '@/lib/hooks'
import { relative, toArabicDigits, type Tone } from '@/lib/format'
import { cn } from '@/lib/utils'
import type {
  AttentionItem,
  InventoryBranchView,
  InventoryProduct,
  InventoryStatus,
  InventoryStatusFilter,
} from '@/lib/types'
import { Freshness } from '@/components/Freshness'
import { HealthDot } from '@/components/HealthDot'
import { PageHeader } from '@/components/PageHeader'
import { Pagination } from '@/components/Pagination'
import { LoadingState, EmptyState, ErrorState } from '@/components/States'
import {
  ArrowLeading,
  BranchIcon,
  DangerIcon,
  InventoryIcon,
  SearchIcon,
  SuccessIcon,
} from '@/components/icon'
import { Badge } from '@/components/ui/badge'
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

const STATUS_LABEL: Record<InventoryStatus, string> = {
  negative: 'سالب',
  out: 'نفاد',
  low: 'منخفض',
  ok: 'سليم',
}
const STATUS_TONE: Record<InventoryStatus, Tone> = {
  negative: 'danger',
  out: 'danger',
  low: 'warning',
  ok: 'success',
}

/** Same rule the API itself uses to grade a branch's snapshot: ok/lagging
 * reads as trustworthy ("synced"), stale/never as not ("offline"). */
function healthToFreshnessSource(health: InventoryBranchView['health']): 'synced' | 'offline' {
  return health === 'ok' || health === 'lagging' ? 'synced' : 'offline'
}

function lastMovement(item: AttentionItem): string {
  const dates = [item.last_in_date, item.last_out_date].filter((d): d is string => !!d)
  if (dates.length === 0) return '—'
  const latest = dates.reduce((a, b) => (new Date(a) > new Date(b) ? a : b))
  return relative(latest)
}

type ViewKey = 'attention' | 'products' | 'branches'
const VIEWS: { key: ViewKey; label: string }[] = [
  { key: 'attention', label: 'يحتاج انتباه' },
  { key: 'products', label: 'حسب الصنف' },
  { key: 'branches', label: 'حسب الفرع' },
]

export function Inventory() {
  const { tenantId } = useParams<'tenantId'>()
  const { data: bundle } = useBundle(tenantId)
  const [searchParams, setSearchParams] = useSearchParams()

  const view: ViewKey = (searchParams.get('view') as ViewKey | null) ?? 'attention'
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
      <PageHeader title="المخزون" description="مخزون كل الفروع في مكان واحد." />

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

      {view === 'attention' && (
        <AttentionView
          tenantId={tenantId}
          branchId={branchId}
          branches={branches}
          onBranchChange={setBranchId}
        />
      )}
      {view === 'products' && (
        <ProductsView
          tenantId={tenantId}
          branchId={branchId}
          branches={branches}
          onBranchChange={setBranchId}
        />
      )}
      {view === 'branches' && <BranchesView tenantId={tenantId} />}
    </>
  )
}

// --- يحتاج انتباه (default) ---

function AttentionView({
  tenantId,
  branchId,
  branches,
  onBranchChange,
}: {
  tenantId?: string
  branchId?: string
  branches: { ID: string; Name: string }[]
  onBranchChange: (id: string | undefined) => void
}) {
  const navigate = useNavigate()
  const [page, setPage] = useState(1)

  const filterKey = branchId ?? ''
  const [lastFilterKey, setLastFilterKey] = useState(filterKey)
  if (filterKey !== lastFilterKey) {
    setLastFilterKey(filterKey)
    setPage(1)
  }

  const query = useInventoryAttention(tenantId, { branchId, page, pageSize: PAGE_SIZE })

  const notSubscribed = query.error instanceof ApiError && query.error.status === 402
  const gatewayError = query.error instanceof ApiError && query.error.status !== 402

  return (
    <div>
      <div className="mb-4 max-w-xs">
        <select
          className={selectClass}
          value={branchId ?? ''}
          onChange={(e) => onBranchChange(e.target.value || undefined)}
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
          icon={InventoryIcon}
          title="لا يوجد اشتراك مزامنة"
          description="فعّل اشتراك المزامنة لعرض مخزون فروعك."
        />
      ) : gatewayError ? (
        <ErrorState
          message="تعذّر الوصول إلى بيانات المخزون الآن."
          onRetry={() => void query.refetch()}
        />
      ) : query.isLoading ? (
        <LoadingState rows={5} />
      ) : (
        <>
          {query.data && query.data.data.stale_branches.length > 0 && (
            <div className="mb-4 space-y-2">
              {query.data.data.stale_branches.map((b) => (
                <Link
                  key={b.branch_id}
                  to={`/tenants/${tenantId}/branches/${b.branch_id}`}
                  className="flex items-center gap-2.5 rounded-lg border border-warning/30 bg-warning/5 px-4 py-2.5 text-sm transition-colors hover:bg-warning/10"
                >
                  <DangerIcon className="size-4 shrink-0 text-warning" />
                  <span className="min-w-0 flex-1">
                    {b.branch_name}: بيانات قديمة — آخر مزامنة {relative(b.last_sync_at)}
                  </span>
                  <ArrowLeading className="size-4 shrink-0 text-muted-foreground" />
                </Link>
              ))}
            </div>
          )}

          {query.data && (
            <div className="mb-4 grid grid-cols-3 gap-3">
              <CountTile label="سالب" value={query.data.data.counts.negative} tone="danger" />
              <CountTile label="نفاد" value={query.data.data.counts.out} tone="danger" />
              <CountTile label="منخفض" value={query.data.data.counts.low} tone="warning" />
            </div>
          )}

          {!query.data || query.data.data.items.length === 0 ? (
            query.data &&
            query.data.data.stale_branches.length === 0 && (
              <Card className="flex items-center gap-2.5 p-4 text-sm text-muted-foreground">
                <SuccessIcon className="size-5 shrink-0 text-success" />
                لا توجد أصناف تحتاج انتباهك — كل الفروع بخير.
              </Card>
            )
          ) : (
            <>
              <div className="rounded-xl border border-border">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>الحالة</TableHead>
                      <TableHead>الصنف</TableHead>
                      <TableHead>الفرع</TableHead>
                      <TableHead>الكمية</TableHead>
                      <TableHead>آخر حركة</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {query.data.data.items.map((item) => (
                      <TableRow
                        key={`${item.product_id}-${item.warehouse_id}`}
                        tabIndex={0}
                        onClick={() => navigate(`/tenants/${tenantId}/catalog/${item.product_id}`)}
                        onKeyDown={(e) => {
                          if (e.key === 'Enter')
                            navigate(`/tenants/${tenantId}/catalog/${item.product_id}`)
                        }}
                        className="cursor-pointer"
                      >
                        <TableCell>
                          <Badge tone={STATUS_TONE[item.status]}>
                            {STATUS_LABEL[item.status]}
                          </Badge>
                        </TableCell>
                        <TableCell>
                          <div className="font-medium">{item.product_name}</div>
                          <div className="dir-ltr text-start font-mono text-xs text-muted-foreground">
                            {toArabicDigits(item.product_code)}
                          </div>
                        </TableCell>
                        <TableCell>
                          <div className="flex items-center gap-2">
                            <HealthDot health={item.health} />
                            <span>{item.branch_name}</span>
                          </div>
                          <div className="text-xs text-muted-foreground">
                            {item.warehouse_name}
                          </div>
                        </TableCell>
                        <TableCell>
                          <span
                            className={cn(
                              item.total_qty < 0 && 'font-semibold text-danger',
                            )}
                          >
                            {toArabicDigits(item.total_qty)}
                          </span>
                          {item.re_order > 0 && (
                            <span className="text-xs text-muted-foreground">
                              {' '}
                              / {toArabicDigits(item.re_order)}
                            </span>
                          )}
                        </TableCell>
                        <TableCell className="text-xs text-muted-foreground">
                          {lastMovement(item)}
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
        </>
      )}
    </div>
  )
}

function CountTile({
  label,
  value,
  tone,
}: {
  label: string
  value: number
  tone: 'danger' | 'warning'
}) {
  return (
    <Card className="p-4">
      <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
        {label}
      </div>
      <div
        className={cn(
          'mt-1 font-display text-2xl font-bold',
          tone === 'danger' ? 'text-danger' : 'text-warning',
        )}
      >
        {toArabicDigits(value)}
      </div>
    </Card>
  )
}

// --- حسب الصنف ---

const STATUS_FILTER_OPTIONS: { value: InventoryStatusFilter | ''; label: string }[] = [
  { value: '', label: 'كل الحالات' },
  { value: 'attention', label: 'يحتاج انتباه' },
  { value: 'negative', label: 'سالب' },
  { value: 'out', label: 'نفاد' },
  { value: 'low', label: 'منخفض' },
]

function ProductsView({
  tenantId,
  branchId,
  branches,
  onBranchChange,
}: {
  tenantId?: string
  branchId?: string
  branches: { ID: string; Name: string }[]
  onBranchChange: (id: string | undefined) => void
}) {
  const navigate = useNavigate()
  const groupsQuery = useCatalogGroups(tenantId)
  const [search, setSearch] = useState('')
  const [debouncedSearch, setDebouncedSearch] = useState('')
  const [groupId, setGroupId] = useState<string | undefined>(undefined)
  const [status, setStatus] = useState<InventoryStatusFilter | undefined>(undefined)
  const [page, setPage] = useState(1)

  useEffect(() => {
    const t = window.setTimeout(() => setDebouncedSearch(search.trim()), 300)
    return () => window.clearTimeout(t)
  }, [search])

  const filterKey = `${debouncedSearch} ${groupId ?? ''} ${branchId ?? ''} ${status ?? ''}`
  const [lastFilterKey, setLastFilterKey] = useState(filterKey)
  if (filterKey !== lastFilterKey) {
    setLastFilterKey(filterKey)
    setPage(1)
  }

  const query = useInventoryProducts(tenantId, {
    search: debouncedSearch || undefined,
    groupId,
    branchId,
    status,
    page,
    pageSize: PAGE_SIZE,
  })

  const notSubscribed = query.error instanceof ApiError && query.error.status === 402
  const gatewayError = query.error instanceof ApiError && query.error.status !== 402

  return (
    <div>
      <div className="mb-4 grid gap-3 sm:grid-cols-[1fr_auto_auto_auto]">
        <div className="relative">
          <SearchIcon className="pointer-events-none absolute start-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="ابحث بالاسم أو الكود"
            className="ps-9"
          />
        </div>
        <select
          className={cn(selectClass, 'sm:w-40')}
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
          className={cn(selectClass, 'sm:w-40')}
          value={branchId ?? ''}
          onChange={(e) => onBranchChange(e.target.value || undefined)}
        >
          <option value="">كل الفروع</option>
          {branches.map((b) => (
            <option key={b.ID} value={b.ID}>
              {b.Name}
            </option>
          ))}
        </select>
        <select
          className={cn(selectClass, 'sm:w-40')}
          value={status ?? ''}
          onChange={(e) => setStatus((e.target.value || undefined) as InventoryStatusFilter | undefined)}
        >
          {STATUS_FILTER_OPTIONS.map((o) => (
            <option key={o.value} value={o.value}>
              {o.label}
            </option>
          ))}
        </select>
      </div>

      {notSubscribed ? (
        <EmptyState
          icon={InventoryIcon}
          title="لا يوجد اشتراك مزامنة"
          description="فعّل اشتراك المزامنة لعرض مخزون فروعك."
        />
      ) : gatewayError ? (
        <ErrorState
          message="تعذّر الوصول إلى بيانات المخزون الآن."
          onRetry={() => void query.refetch()}
        />
      ) : query.isLoading ? (
        <LoadingState rows={5} />
      ) : !query.data || query.data.data.items.length === 0 ? (
        <EmptyState
          icon={InventoryIcon}
          title="لا توجد أصناف"
          description="لا توجد أصناف مطابقة لبحثك أو للفلاتر المحددة."
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
                  <TableHead>الكمية</TableHead>
                  <TableHead>القيمة</TableHead>
                  <TableHead>الفروع</TableHead>
                  <TableHead>الحالة</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {query.data.data.items.map((p: InventoryProduct) => (
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
                    <TableCell className="text-muted-foreground">
                      {p.group_name ?? '—'}
                    </TableCell>
                    <TableCell className={cn(p.total_qty < 0 && 'font-semibold text-danger')}>
                      {toArabicDigits(p.total_qty)}
                    </TableCell>
                    <TableCell>{money.format(p.stock_value)}</TableCell>
                    <TableCell>{toArabicDigits(p.branches_with_stock)}</TableCell>
                    <TableCell>
                      <Badge tone={STATUS_TONE[p.status]}>{STATUS_LABEL[p.status]}</Badge>
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

// --- حسب الفرع ---

function BranchesView({ tenantId }: { tenantId?: string }) {
  const query = useInventoryBranches(tenantId)

  const notSubscribed = query.error instanceof ApiError && query.error.status === 402
  const gatewayError = query.error instanceof ApiError && query.error.status !== 402

  if (notSubscribed) {
    return (
      <EmptyState
        icon={InventoryIcon}
        title="لا يوجد اشتراك مزامنة"
        description="فعّل اشتراك المزامنة لعرض مخزون فروعك."
      />
    )
  }
  if (gatewayError) {
    return (
      <ErrorState
        message="تعذّر الوصول إلى بيانات المخزون الآن."
        onRetry={() => void query.refetch()}
      />
    )
  }
  if (query.isLoading) return <LoadingState rows={4} />
  if (!query.data || query.data.data.branches.length === 0) {
    return (
      <EmptyState
        icon={BranchIcon}
        title="لا توجد فروع"
        description="أضف فرعًا ليظهر مخزونه هنا."
      />
    )
  }

  const { branches, totals } = query.data.data

  return (
    <div>
      <div className="mb-5 grid grid-cols-2 gap-3 sm:grid-cols-4">
        <Card className="p-4">
          <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
            قيمة المخزون
          </div>
          <div className="mt-1 font-display text-xl font-bold">
            {money.format(totals.stock_value)}
          </div>
        </Card>
        <CountTile label="سالب" value={totals.negative_count} tone="danger" />
        <CountTile label="نفاد" value={totals.out_count} tone="danger" />
        <CountTile label="منخفض" value={totals.low_count} tone="warning" />
      </div>

      <div className="grid gap-4 sm:grid-cols-2">
        {branches.map((b) => (
          <div
            key={b.branch_id}
            className="flex flex-col gap-3 rounded-xl border border-border bg-card/50 p-4"
          >
            <div className="flex items-center gap-2.5">
              <HealthDot health={b.health} />
              <h3 className="min-w-0 truncate font-display font-bold">{b.branch_name}</h3>
            </div>
            <Freshness source={healthToFreshnessSource(b.health)} asOf={b.last_sync_at} />

            <div className="grid grid-cols-2 gap-3 text-sm">
              <div>
                <div className="text-xs text-muted-foreground">عدد الأصناف</div>
                <div className="mt-0.5 font-medium">{toArabicDigits(b.sku_count)}</div>
              </div>
              <div>
                <div className="text-xs text-muted-foreground">قيمة المخزون</div>
                <div className="mt-0.5 font-medium">{money.format(b.stock_value)}</div>
              </div>
            </div>

            <div className="flex flex-wrap gap-2 text-xs">
              <Link
                to={`/tenants/${tenantId}/inventory?view=attention&branch=${b.branch_id}`}
                className={cn(
                  'rounded-full px-2.5 py-1 font-medium',
                  b.negative_count > 0
                    ? 'bg-danger/10 text-danger'
                    : 'bg-muted text-muted-foreground',
                )}
              >
                سالب {toArabicDigits(b.negative_count)}
              </Link>
              <Link
                to={`/tenants/${tenantId}/inventory?view=attention&branch=${b.branch_id}`}
                className={cn(
                  'rounded-full px-2.5 py-1 font-medium',
                  b.out_count > 0 ? 'bg-danger/10 text-danger' : 'bg-muted text-muted-foreground',
                )}
              >
                نفاد {toArabicDigits(b.out_count)}
              </Link>
              <Link
                to={`/tenants/${tenantId}/inventory?view=attention&branch=${b.branch_id}`}
                className={cn(
                  'rounded-full px-2.5 py-1 font-medium',
                  b.low_count > 0
                    ? 'bg-warning/10 text-warning'
                    : 'bg-muted text-muted-foreground',
                )}
              >
                منخفض {toArabicDigits(b.low_count)}
              </Link>
            </div>

            {b.warehouses.length > 0 && (
              <details className="text-xs text-muted-foreground">
                <summary className="cursor-pointer select-none">
                  المخازن ({toArabicDigits(b.warehouses.length)})
                </summary>
                <ul className="mt-1.5 space-y-1">
                  {b.warehouses.map((w) => (
                    <li key={w.warehouse_id} className="flex items-center justify-between gap-2">
                      <span className="min-w-0 truncate">{w.warehouse_name}</span>
                      <span className="shrink-0">
                        {toArabicDigits(w.sku_count)} صنف · {money.format(w.stock_value)}
                      </span>
                    </li>
                  ))}
                </ul>
              </details>
            )}

            <Link
              to={`/tenants/${tenantId}/branches/${b.branch_id}`}
              className="mt-auto flex items-center gap-1 border-t border-border pt-2.5 text-xs text-primary hover:underline"
            >
              عرض الفرع
              <ArrowLeading className="size-3.5" />
            </Link>
          </div>
        ))}
      </div>
    </div>
  )
}
