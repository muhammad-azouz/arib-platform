import { useEffect, useState } from 'react'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { ApiError } from '@/lib/api'
import { useBundle, useCatalogGroups, useCatalogProducts } from '@/lib/hooks'
import { toArabicDigits } from '@/lib/format'
import { cn } from '@/lib/utils'
import type { CatalogGroup, CatalogProduct } from '@/lib/types'
import { CreateProductDialog } from '@/components/CreateProductDialog'
import { PageHeader } from '@/components/PageHeader'
import { Pagination } from '@/components/Pagination'
import { LoadingState, EmptyState, ErrorState } from '@/components/States'
import { AddIcon, CatalogIcon, GroupIcon, SearchIcon } from '@/components/icon'
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
const ROOT_PARENT = '00000000-0000-0000-0000-000000000000'

interface GroupNode extends CatalogGroup {
  children: GroupNode[]
}

function buildGroupTree(groups: CatalogGroup[]): GroupNode[] {
  const nodes = new Map<string, GroupNode>(groups.map((g) => [g.id, { ...g, children: [] }]))
  const roots: GroupNode[] = []
  for (const node of nodes.values()) {
    const parent = node.parent_id !== ROOT_PARENT ? nodes.get(node.parent_id) : undefined
    if (parent) parent.children.push(node)
    else roots.push(node)
  }
  const sortTree = (list: GroupNode[]) => {
    list.sort((a, b) => a.num - b.num)
    list.forEach((n) => sortTree(n.children))
  }
  sortTree(roots)
  return roots
}

export function Catalog() {
  const { tenantId } = useParams<'tenantId'>()
  const { data: bundle } = useBundle(tenantId)
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
    <>
      <PageHeader
        title="الكتالوج"
        description="المجموعات والأصناف والأسعار عبر كل الفروع."
        actions={
          <Button onClick={() => setCreateOpen(true)}>
            <AddIcon className="size-4" />
            منتج جديد
          </Button>
        }
      />

      {tenantId && (
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
        <div className="grid gap-4 lg:grid-cols-[240px_1fr]">
          <aside className="h-fit rounded-xl border border-border bg-card/50 p-2">
            {groupsQuery.isLoading ? (
              <div className="space-y-2 p-2">
                <div className="h-5 w-24 animate-pulse rounded bg-muted" />
                <div className="h-5 w-32 animate-pulse rounded bg-muted" />
                <div className="h-5 w-20 animate-pulse rounded bg-muted" />
              </div>
            ) : (
              <ul className="space-y-0.5">
                <li>
                  <button
                    type="button"
                    onClick={() => setGroupId(undefined)}
                    className={cn(
                      'flex w-full items-center gap-2 rounded-lg px-2.5 py-1.5 text-sm transition-colors hover:bg-accent/60',
                      groupId === undefined
                        ? 'bg-accent font-semibold text-primary'
                        : 'text-foreground/80',
                    )}
                  >
                    <CatalogIcon className="size-4 shrink-0 text-muted-foreground" />
                    كل الأصناف
                  </button>
                </li>
                <GroupTree
                  nodes={buildGroupTree(groupsQuery.data?.data ?? [])}
                  selected={groupId}
                  onSelect={setGroupId}
                />
              </ul>
            )}
          </aside>

          <div className="min-w-0">
            <div className="relative mb-4">
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
              <Pagination
                page={page}
                pageSize={PAGE_SIZE}
                total={productsQuery.data.data.total}
                onPageChange={setPage}
              />
            )}
          </div>
        </div>
      )}
    </>
  )
}

function GroupTree({
  nodes,
  selected,
  onSelect,
  depth = 0,
}: {
  nodes: GroupNode[]
  selected?: string
  onSelect: (id: string) => void
  depth?: number
}) {
  if (nodes.length === 0) return null
  return (
    <ul className={depth === 0 ? 'space-y-0.5' : 'mt-0.5 space-y-0.5 ps-4'}>
      {nodes.map((n) => (
        <li key={n.id}>
          <button
            type="button"
            onClick={() => onSelect(n.id)}
            className={cn(
              'flex w-full items-center gap-2 rounded-lg px-2.5 py-1.5 text-sm transition-colors hover:bg-accent/60',
              selected === n.id ? 'bg-accent font-semibold text-primary' : 'text-foreground/80',
            )}
          >
            <GroupIcon className="size-4 shrink-0 text-muted-foreground" />
            <span className="min-w-0 flex-1 truncate">{n.name}</span>
            <span className="shrink-0 text-xs text-muted-foreground">
              {toArabicDigits(n.product_count)}
            </span>
          </button>
          <GroupTree nodes={n.children} selected={selected} onSelect={onSelect} depth={depth + 1} />
        </li>
      ))}
    </ul>
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
    <div className="rounded-xl border border-border">
      <Table>
        <TableHeader>
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

