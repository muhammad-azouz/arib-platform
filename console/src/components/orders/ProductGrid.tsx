import { useEffect, useState } from 'react'
import { useCatalogProducts } from '@/lib/hooks'
import { CatalogIcon, SearchIcon } from '@/components/icon'
import { EmptyState, LoadingState } from '@/components/States'
import { Input } from '@/components/ui/input'
import { Pagination } from '@/components/Pagination'

const money = new Intl.NumberFormat('ar', { maximumFractionDigits: 2 })
const PAGE_SIZE = 40

/**
 * The middle pane: its own search box over the same `hq/catalog/products`
 * read Catalog.tsx uses, cards auto-filling at `minmax(100px, 1fr)`. Every
 * product kind is shown and addable — D16's stock gate applies to
 * `ProductKind.Product` lines only, it never restricts what can go *into*
 * the cart (matches the desktop: a sales-service line is accepted).
 */
export function ProductGrid({
  tenantId,
  groupId,
  onAdd,
}: {
  tenantId: string | undefined
  groupId?: string
  onAdd: (productId: string) => void
}) {
  const [search, setSearch] = useState('')
  const [debouncedSearch, setDebouncedSearch] = useState('')
  const [page, setPage] = useState(1)

  useEffect(() => {
    const t = window.setTimeout(() => setDebouncedSearch(search.trim()), 300)
    return () => window.clearTimeout(t)
  }, [search])

  const filterKey = `${debouncedSearch}\0${groupId ?? ''}`
  const [lastFilterKey, setLastFilterKey] = useState(filterKey)
  if (filterKey !== lastFilterKey) {
    setLastFilterKey(filterKey)
    setPage(1)
  }

  const query = useCatalogProducts(tenantId, {
    search: debouncedSearch || undefined,
    groupId,
    page,
    pageSize: PAGE_SIZE,
  })
  const items = query.data?.data.items ?? []

  return (
    <div className="flex h-full flex-col">
      <div className="relative mb-3 shrink-0">
        <SearchIcon className="pointer-events-none absolute start-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
        <Input
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="ابحث بالاسم أو الكود أو الباركود"
          className="ps-9"
        />
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto">
        {query.isLoading ? (
          <LoadingState rows={6} />
        ) : items.length === 0 ? (
          <EmptyState
            icon={CatalogIcon}
            title="لا توجد أصناف"
            description="لا توجد أصناف مطابقة لبحثك أو للمجموعة المحددة."
          />
        ) : (
          <div
            className="grid gap-2"
            style={{ gridTemplateColumns: 'repeat(auto-fill, minmax(100px, 1fr))' }}
          >
            {items.map((p) => (
              <button
                key={p.id}
                type="button"
                onClick={() => onAdd(p.id)}
                className="flex flex-col items-start gap-1 rounded-lg border border-border bg-card/50 p-2.5 text-start transition-colors hover:border-primary/50 hover:bg-accent/60"
              >
                <span className="line-clamp-2 min-h-[2.5em] text-xs font-medium">{p.name}</span>
                <span className="text-[11px] text-muted-foreground">{p.unit ?? '—'}</span>
                <span className="mt-auto text-sm font-semibold tabular-nums">
                  {money.format(p.sale)}
                </span>
              </button>
            ))}
          </div>
        )}
      </div>

      {query.data && query.data.data.total > PAGE_SIZE && (
        <div className="mt-3 shrink-0">
          <Pagination
            page={page}
            pageSize={PAGE_SIZE}
            total={query.data.data.total}
            itemLabel="صنف"
            onPageChange={setPage}
          />
        </div>
      )}
    </div>
  )
}
