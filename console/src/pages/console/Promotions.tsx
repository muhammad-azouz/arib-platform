import { useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { ApiError } from '@/lib/api'
import { useBundle, usePromotions } from '@/lib/hooks'
import { PERM, useCan, useScope } from '@/lib/perm'
import {
  fmtDateOnly,
  promotionLevelLabel,
  promotionReachLabel,
  promotionStatusLabel,
  promotionStatusTone,
  promotionValueLabel,
  toArabicDigits,
} from '@/lib/format'
import { cn } from '@/lib/utils'
import type { Promotion, PromotionStatusFilter } from '@/lib/types'
import { PageHeader } from '@/components/PageHeader'
import { Pagination } from '@/components/Pagination'
import { Freshness } from '@/components/Freshness'
import { LoadingState, EmptyState, ErrorState } from '@/components/States'
import { AddIcon, PromotionsIcon } from '@/components/icon'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { PromotionFormDialog } from '@/components/PromotionFormDialog'

const PAGE_SIZE = 25

const selectClass =
  'flex h-9 w-full rounded-md border border-input bg-background/40 px-3 py-1 text-sm shadow-sm transition-colors focus-visible:outline-none focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/30'

// Mirrors the gateway's five accepted values. The four non-'all' entries are
// mutually exclusive and exhaustive, so a member can tab across them and see
// every promotion exactly once.
const STATUS_OPTIONS: { value: PromotionStatusFilter; label: string }[] = [
  { value: 'all', label: 'كل الحالات' },
  { value: 'active', label: 'نشط' },
  { value: 'scheduled', label: 'مجدول' },
  { value: 'expired', label: 'منتهي' },
  { value: 'paused', label: 'موقوف' },
]

const LEVEL_OPTIONS: { value: '' | 'item' | 'bill'; label: string }[] = [
  { value: '', label: 'كل المستويات' },
  { value: 'item', label: 'خصم صنف' },
  { value: 'bill', label: 'خصم فاتورة' },
]

export function Promotions() {
  const { tenantId } = useParams<'tenantId'>()
  const { data: bundle } = useBundle(tenantId)
  const navigate = useNavigate()
  const canManage = useCan(tenantId, PERM.PromotionsManage)
  // Same narrowing as the form dialog: offering a branch the member cannot see
  // would return an empty page, not an error, which reads as "no promotions
  // here" rather than "not yours to view".
  const scope = useScope(tenantId)
  const branches = (bundle?.Branches ?? []).filter(
    (b) => !scope || scope.branch_ids.length === 0 || scope.branch_ids.includes(b.ID),
  )

  const [status, setStatus] = useState<PromotionStatusFilter>('all')
  const [level, setLevel] = useState<'' | 'item' | 'bill'>('')
  const [branchId, setBranchId] = useState('')
  const [page, setPage] = useState(1)
  const [creating, setCreating] = useState(false)

  // Filter changes reset to page 1 at render time rather than in an effect —
  // the same pattern Orders/Catalog use, which avoids blanking the table to a
  // spinner for one frame.
  const filterKey = `${status}\0${level}\0${branchId}`
  const [lastFilterKey, setLastFilterKey] = useState(filterKey)
  if (filterKey !== lastFilterKey) {
    setLastFilterKey(filterKey)
    setPage(1)
  }

  const query = usePromotions(tenantId, {
    status,
    level: level || undefined,
    branchId: branchId || undefined,
    page,
    pageSize: PAGE_SIZE,
  })

  const notSubscribed = query.error instanceof ApiError && query.error.status === 402
  const gatewayError = query.error instanceof ApiError && query.error.status !== 402

  if (!bundle) return <LoadingState />

  return (
    <>
      <PageHeader
        title="العروض والخصومات"
        description="خصومات تُطبَّق تلقائياً عند الفوترة — على الأصناف أو على الفاتورة كاملة."
        actions={
          <>
            {query.data && <Freshness source={query.data.source} asOf={query.data.as_of} />}
            {canManage && (
              <Button onClick={() => setCreating(true)}>
                <AddIcon className="size-4" />
                عرض جديد
              </Button>
            )}
          </>
        }
      />

      <div className="mb-4 grid gap-3 sm:grid-cols-3">
        <select
          className={selectClass}
          value={status}
          onChange={(e) => setStatus(e.target.value as PromotionStatusFilter)}
          aria-label="الحالة"
        >
          {STATUS_OPTIONS.map((o) => (
            <option key={o.value} value={o.value}>
              {o.label}
            </option>
          ))}
        </select>
        <select
          className={selectClass}
          value={level}
          onChange={(e) => setLevel(e.target.value as '' | 'item' | 'bill')}
          aria-label="المستوى"
        >
          {LEVEL_OPTIONS.map((o) => (
            <option key={o.value} value={o.value}>
              {o.label}
            </option>
          ))}
        </select>
        <select
          className={selectClass}
          value={branchId}
          onChange={(e) => setBranchId(e.target.value)}
          aria-label="الفرع"
        >
          {/* No "company-wide only" option: a company-wide promotion applies at
              every branch, so it is always in the result whatever branch is
              picked (the gateway's own filter is `BranchId IS NULL OR IN (…)`).
              Offering it as a filter would imply an exclusivity that does not
              exist. */}
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
          icon={PromotionsIcon}
          title="لا يوجد اشتراك مزامنة"
          description="فعّل اشتراك المزامنة لإدارة عروض فروعك."
        />
      ) : gatewayError ? (
        <ErrorState
          message="تعذّر الوصول إلى بيانات العروض الآن."
          onRetry={() => void query.refetch()}
        />
      ) : (
        <>
          <PromotionsTable
            items={query.data?.data.items}
            isLoading={query.isLoading}
            onRowClick={(id) => navigate(`/tenants/${tenantId}/promotions/${id}`)}
          />

          {query.data && query.data.data.total > 0 && (
            <Pagination
              page={page}
              pageSize={PAGE_SIZE}
              total={query.data.data.total}
              itemLabel="عرض"
              onPageChange={setPage}
            />
          )}
        </>
      )}

      {creating && tenantId && (
        <PromotionFormDialog tenantId={tenantId} onClose={() => setCreating(false)} />
      )}
    </>
  )
}

function PromotionsTable({
  items,
  isLoading,
  onRowClick,
}: {
  items?: Promotion[]
  isLoading: boolean
  onRowClick: (id: string) => void
}) {
  if (isLoading) return <LoadingState rows={5} />
  if (!items || items.length === 0) {
    return (
      <EmptyState
        icon={PromotionsIcon}
        title="لا توجد عروض"
        description="لا توجد عروض مطابقة للفلاتر المحددة."
      />
    )
  }
  return (
    <div className="rounded-xl border border-border">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>الاسم</TableHead>
            <TableHead>المستوى</TableHead>
            <TableHead>الخصم</TableHead>
            <TableHead>النطاق</TableHead>
            <TableHead>الأصناف</TableHead>
            <TableHead>الفترة</TableHead>
            <TableHead>الحالة</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {items.map((p) => (
            <TableRow
              key={p.id}
              tabIndex={0}
              onClick={() => onRowClick(p.id)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') onRowClick(p.id)
              }}
              className="cursor-pointer"
            >
              <TableCell className="font-medium">{p.name}</TableCell>
              <TableCell>{promotionLevelLabel(p.level)}</TableCell>
              <TableCell className="font-display">
                {promotionValueLabel(p.discount_type, p.value)}
              </TableCell>
              <TableCell
                className={cn(!p.branch_id && 'text-muted-foreground')}
                title={p.branch_id ? undefined : 'يُطبَّق في كل الفروع'}
              >
                {promotionReachLabel(p.branch_name)}
              </TableCell>
              <TableCell>
                {p.target_count === 0 ? 'كل الأصناف' : toArabicDigits(p.target_count)}
              </TableCell>
              <TableCell className="whitespace-nowrap text-sm text-muted-foreground">
                {fmtDateOnly(p.starts_on)} — {fmtDateOnly(p.ends_on)}
              </TableCell>
              <TableCell>
                {/* Rendered from the SERVER's status, never recomputed from the
                    dates above it (spec D8). The two must not be able to
                    disagree about what a cashier is currently getting. */}
                <Badge tone={promotionStatusTone(p.status)}>
                  {promotionStatusLabel(p.status)}
                </Badge>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}
