import { useEffect, useState } from 'react'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { ApiError } from '@/lib/api'
import { useBundle, useCatalogGroups, useCatalogProducts } from '@/lib/hooks'
import { PERM, useCanUnscoped } from '@/lib/perm'
import { toArabicDigits } from '@/lib/format'
import type { CatalogProduct } from '@/lib/types'
import { CreateProductDialog } from '@/components/CreateProductDialog'
import { GroupDrill } from '@/components/GroupDrill'
import { PageHeader } from '@/components/PageHeader'
import { Pagination } from '@/components/Pagination'
import { LoadingState, EmptyState, ErrorState } from '@/components/States'
import { AddIcon, CatalogIcon, SearchIcon } from '@/components/icon'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
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

export function Catalog() {
  const { tenantId } = useParams<'tenantId'>()
  const { data: bundle } = useBundle(tenantId)
  // POST /hq/catalog/products is D5c-unscoped (a Tier-A write that lands at
  // every branch), so the create affordance requires an unscoped member, not
  // just catalog.manage — a scoped manager still sees the read path below.
  const canManage = useCanUnscoped(tenantId, PERM.CatalogManage)
  // The command palette's "بحث في الكتالوج…" row deep-links here with
  // ?search= — honor it as the initial value only (not kept in sync with
  // the URL afterwards, same as every other filter on this page).
  const [searchParams] = useSearchParams()
  const initialSearch = searchParams.get('search') ?? ''
  const [search, setSearch] = useState(initialSearch)
  const [debouncedSearch, setDebouncedSearch] = useState(initialSearch)
  const [groupId, setGroupId] = useState<string | undefined>(undefined)
  const [page, setPage] = useState(1)
  const [createOpen, setCreateOpen] = useState(false)

  useEffect(() => {
    const t = window.setTimeout(() => setDebouncedSearch(search.trim()), 300)
    return () => window.clearTimeout(t)
  }, [search])

  // A new search term or group resets to page 1 — the React-recommended
  // "adjust state during render" pattern (not an effect), so it can't
  // cascade an extra render the way setState-in-effect would.
  const filterKey = `${debouncedSearch}\0${groupId ?? ''}`
  const [lastFilterKey, setLastFilterKey] = useState(filterKey)
  if (filterKey !== lastFilterKey) {
    setLastFilterKey(filterKey)
    setPage(1)
  }

  const groupsQuery = useCatalogGroups(tenantId)
  const productsQuery = useCatalogProducts(tenantId, {
    search: debouncedSearch || undefined,
    groupId,
    page,
    pageSize: PAGE_SIZE,
  })

  if (!bundle) return <LoadingState />

  const notSubscribed =
    productsQuery.error instanceof ApiError && productsQuery.error.status === 402
  const gatewayError =
    productsQuery.error instanceof ApiError && productsQuery.error.status !== 402

  return (
    // Fills the shell's viewport frame so the products table can take whatever
    // height is left over and scroll on its own — below `lg` the two columns
    // stack, where splitting the viewport would leave both a few rows tall, so
    // the layout falls back to normal page flow there.
    <div className="flex h-full flex-col">
      <PageHeader
        title="الكتالوج"
        description="المجموعات والأصناف والأسعار عبر كل الفروع."
        actions={
          canManage ? (
            <Button onClick={() => setCreateOpen(true)}>
              <AddIcon className="size-4" />
              منتج جديد
            </Button>
          ) : undefined
        }
      />

      {tenantId && canManage && (
        <CreateProductDialog tenantId={tenantId} open={createOpen} onOpenChange={setCreateOpen} />
      )}

      {notSubscribed ? (
        <EmptyState
          icon={CatalogIcon}
          title="لا يوجد اشتراك مزامنة"
          description="فعّل اشتراك المزامنة لعرض كتالوج الأصناف والأسعار من فروعك."
        />
      ) : gatewayError ? (
        <ErrorState
          message="تعذّر الوصول إلى بيانات الفروع الآن."
          onRetry={() => {
            void groupsQuery.refetch()
            void productsQuery.refetch()
          }}
        />
      ) : (
        <div className="grid gap-4 lg:min-h-0 lg:flex-1 lg:grid-cols-[240px_1fr]">
          <GroupDrill
            groups={groupsQuery.data?.data ?? []}
            isLoading={groupsQuery.isLoading}
            selected={groupId}
            onSelect={setGroupId}
            className="h-fit lg:h-full lg:min-h-0 lg:overflow-y-auto"
          />

          <div className="flex min-w-0 flex-col lg:min-h-0">
            <div className="relative mb-4 shrink-0">
              <SearchIcon className="pointer-events-none absolute start-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
              <Input
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                placeholder="ابحث بالاسم أو الكود أو الباركود"
                className="ps-9"
              />
            </div>

            <ProductsTable
              tenantId={tenantId}
              items={productsQuery.data?.data.items}
              isLoading={productsQuery.isLoading}
            />

            {productsQuery.data && productsQuery.data.data.total > 0 && (
              <div className="shrink-0">
                <Pagination
                  page={page}
                  pageSize={PAGE_SIZE}
                  total={productsQuery.data.data.total}
                  onPageChange={setPage}
                />
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  )
}

function ProductsTable({
  tenantId,
  items,
  isLoading,
}: {
  tenantId?: string
  items?: CatalogProduct[]
  isLoading: boolean
}) {
  const navigate = useNavigate()
  if (isLoading) return <LoadingState rows={5} />
  if (!items || items.length === 0) {
    return (
      <EmptyState
        icon={CatalogIcon}
        title="لا توجد أصناف"
        description="لا توجد أصناف مطابقة لبحثك أو للمجموعة المحددة."
      />
    )
  }
  return (
    <div className="rounded-xl border border-border lg:min-h-0 lg:flex-1 lg:overflow-hidden">
      <Table containerClassName="lg:h-full lg:overflow-y-auto">
        {/* The header row's own `border-b` would scroll away with the body:
            collapsed table borders are painted by the table, not by the sticky
            `<thead>`. An inset shadow rides along with it instead. */}
        <TableHeader className="sticky top-0 z-10 bg-background/95 backdrop-blur-md [&_tr]:border-0 [&_tr]:shadow-[inset_0_-1px_0_var(--border)]">
          <TableRow>
            <TableHead>الكود</TableHead>
            <TableHead>الاسم</TableHead>
            <TableHead>المجموعة</TableHead>
            <TableHead>سعر البيع</TableHead>
            <TableHead>الكمية</TableHead>
            <TableHead>الحالة</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {items.map((p) => (
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
              <TableCell>{money.format(p.sale)}</TableCell>
              <TableCell>{toArabicDigits(p.total_qty)}</TableCell>
              <TableCell>
                <Badge tone={p.is_active ? 'success' : 'neutral'}>
                  {p.is_active ? 'مُفعّل' : 'مُعطّل'}
                </Badge>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

