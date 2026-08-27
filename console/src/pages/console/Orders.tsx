import { useEffect, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { ApiError } from '@/lib/api'
import { useBundle, useOrders } from '@/lib/hooks'
import { PERM, useCan } from '@/lib/perm'
import {
  fmtDateTime,
  orderChannelLabel,
  orderStatusLabel,
  orderStatusTone,
  toArabicDigits,
} from '@/lib/format'
import { cn } from '@/lib/utils'
import { ORDER_STATUS, type OrderRow, type OrderStatusValue } from '@/lib/types'
import { PageHeader } from '@/components/PageHeader'
import { Pagination } from '@/components/Pagination'
import { Freshness } from '@/components/Freshness'
import { LoadingState, EmptyState, ErrorState } from '@/components/States'
import { AddIcon, ArrowLeading, OrdersIcon, SearchIcon } from '@/components/icon'
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

const STATUS_OPTIONS: { value: OrderStatusValue | ''; label: string }[] = [
  { value: '', label: 'كل الحالات' },
  { value: ORDER_STATUS.New, label: orderStatusLabel(ORDER_STATUS.New) },
  { value: ORDER_STATUS.Preparing, label: orderStatusLabel(ORDER_STATUS.Preparing) },
  { value: ORDER_STATUS.Ready, label: orderStatusLabel(ORDER_STATUS.Ready) },
  { value: ORDER_STATUS.OutForDelivery, label: orderStatusLabel(ORDER_STATUS.OutForDelivery) },
  { value: ORDER_STATUS.Delivered, label: orderStatusLabel(ORDER_STATUS.Delivered) },
  { value: ORDER_STATUS.Cancelled, label: orderStatusLabel(ORDER_STATUS.Cancelled) },
  { value: ORDER_STATUS.Transferred, label: orderStatusLabel(ORDER_STATUS.Transferred) },
]

export function Orders() {
  const { tenantId } = useParams<'tenantId'>()
  const { data: bundle } = useBundle(tenantId)
  const navigate = useNavigate()
  const canManage = useCan(tenantId, PERM.OrdersManage)

  const [search, setSearch] = useState('')
  const [debouncedSearch, setDebouncedSearch] = useState('')
  const [status, setStatus] = useState<OrderStatusValue | undefined>(undefined)
  const [page, setPage] = useState(1)

  useEffect(() => {
    const t = window.setTimeout(() => setDebouncedSearch(search.trim()), 300)
    return () => window.clearTimeout(t)
  }, [search])

  // Filter changes reset to page 1 without spinner-blanking (Catalog's
  // render-time-reset pattern, not a setState-in-effect), same as
  // Customers/Suppliers.
  const filterKey = `${debouncedSearch}\0${status ?? ''}`
  const [lastFilterKey, setLastFilterKey] = useState(filterKey)
  if (filterKey !== lastFilterKey) {
    setLastFilterKey(filterKey)
    setPage(1)
  }

  const query = useOrders(tenantId, {
    search: debouncedSearch || undefined,
    status,
    page,
    pageSize: PAGE_SIZE,
  })

  // Stat tiles are tenant-wide counts, independent of the search/status
  // filter row above — there's no dedicated stats endpoint (T19 shipped
  // only the list), so each tile reads `.total` off a page_size:1 call
  // filtered to the status it counts. Four small requests beat inventing a
  // backend aggregate for four numbers.
  const totalQuery = useOrders(tenantId, { page: 1, pageSize: 1 })
  const newQuery = useOrders(tenantId, { status: ORDER_STATUS.New, page: 1, pageSize: 1 })
  const deliveredQuery = useOrders(tenantId, { status: ORDER_STATUS.Delivered, page: 1, pageSize: 1 })
  const cancelledQuery = useOrders(tenantId, { status: ORDER_STATUS.Cancelled, page: 1, pageSize: 1 })

  const notSubscribed = query.error instanceof ApiError && query.error.status === 402
  const gatewayError = query.error instanceof ApiError && query.error.status !== 402

  if (!bundle) return <LoadingState />

  return (
    <>
      <PageHeader
        title="الطلبات"
        description="طلبات كول سنتر والتوصيل عبر كل الفروع."
        actions={
          <>
            {query.data && <Freshness source={query.data.source} asOf={query.data.as_of} />}
            {canManage && (
              <Button asChild>
                <Link to="new">
                  <AddIcon className="size-4" />
                  طلب جديد
                </Link>
              </Button>
            )}
          </>
        }
      />

      <div className="mb-5 grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <StatTile label="اجمالى الطلبات" value={totalQuery.data?.data.total} />
        <StatTile label="طلبات جديدة" value={newQuery.data?.data.total} />
        <StatTile label="طلبات مكتملة" value={deliveredQuery.data?.data.total} />
        <StatTile label="طلبات ملغاة" value={cancelledQuery.data?.data.total} />
      </div>

      <div className="mb-4 grid gap-3 sm:grid-cols-[1fr_auto]">
        <div className="relative">
          <SearchIcon className="pointer-events-none absolute start-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="ابحث بالرقم المرجعي أو اسم العميل أو الهاتف"
            className="ps-9"
          />
        </div>
        <select
          className={cn(selectClass, 'sm:w-44')}
          value={status ?? ''}
          onChange={(e) =>
            setStatus(e.target.value === '' ? undefined : (Number(e.target.value) as OrderStatusValue))
          }
        >
          {STATUS_OPTIONS.map((o) => (
            <option key={o.value} value={o.value}>
              {o.label}
            </option>
          ))}
        </select>
      </div>

      {notSubscribed ? (
        <EmptyState
          icon={OrdersIcon}
          title="لا يوجد اشتراك مزامنة"
          description="فعّل اشتراك المزامنة لعرض طلبات فروعك."
        />
      ) : gatewayError ? (
        <ErrorState
          message="تعذّر الوصول إلى بيانات الطلبات الآن."
          onRetry={() => void query.refetch()}
        />
      ) : (
        <>
          <OrdersTable
            items={query.data?.data.items}
            isLoading={query.isLoading}
            onRowClick={(id) => navigate(`/tenants/${tenantId}/orders/${id}`)}
          />

          {query.data && query.data.data.total > 0 && (
            <Pagination
              page={page}
              pageSize={PAGE_SIZE}
              total={query.data.data.total}
              itemLabel="طلب"
              onPageChange={setPage}
            />
          )}
        </>
      )}
    </>
  )
}

function StatTile({ label, value }: { label: string; value: number | undefined }) {
  return (
    <Card className="p-4">
      <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
        {label}
      </div>
      <div className="mt-1 font-display text-xl font-bold">
        {value === undefined ? '—' : toArabicDigits(value)}
      </div>
    </Card>
  )
}

function OrdersTable({
  items,
  isLoading,
  onRowClick,
}: {
  items?: OrderRow[]
  isLoading: boolean
  onRowClick: (id: string) => void
}) {
  if (isLoading) return <LoadingState rows={5} />
  if (!items || items.length === 0) {
    return (
      <EmptyState
        icon={OrdersIcon}
        title="لا توجد طلبات"
        description="لا توجد طلبات مطابقة لبحثك أو للفلاتر المحددة."
      />
    )
  }
  return (
    <div className="rounded-xl border border-border">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>الرقم المرجعي</TableHead>
            <TableHead>العميل</TableHead>
            <TableHead>الفرع</TableHead>
            <TableHead>المبلغ</TableHead>
            <TableHead>الحالة</TableHead>
            <TableHead>المصدر</TableHead>
            <TableHead>التاريخ</TableHead>
            <TableHead className="text-center">تفاصيل</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {items.map((o) => (
            <TableRow
              key={o.id}
              tabIndex={0}
              onClick={() => onRowClick(o.id)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') onRowClick(o.id)
              }}
              className="cursor-pointer"
            >
              <TableCell className="dir-ltr text-start font-mono text-xs">{o.ref}</TableCell>
              <TableCell>
                <div className="font-medium">{o.customer_name || 'تعامل نقدى'}</div>
                {o.phone && (
                  <div className="dir-ltr text-start text-xs text-muted-foreground">{o.phone}</div>
                )}
              </TableCell>
              <TableCell>{o.branch_name}</TableCell>
              <TableCell className="font-semibold tabular-nums">{money.format(o.total)}</TableCell>
              <TableCell>
                <Badge tone={orderStatusTone(o.status)}>{orderStatusLabel(o.status)}</Badge>
              </TableCell>
              <TableCell>
                <Badge tone="muted">{orderChannelLabel(o.channel)}</Badge>
              </TableCell>
              <TableCell className="text-sm text-muted-foreground">
                {fmtDateTime(o.created_at)}
              </TableCell>
              <TableCell className="text-center" onClick={(e) => e.stopPropagation()}>
                <Button variant="ghost" size="sm" className="gap-1" onClick={() => onRowClick(o.id)}>
                  تفاصيل
                  <ArrowLeading className="size-3.5" />
                </Button>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}
