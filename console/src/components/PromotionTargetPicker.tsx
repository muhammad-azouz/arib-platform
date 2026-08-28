import { useMemo, useState } from 'react'
import { useCatalogGroups, useCatalogProducts } from '@/lib/hooks'
import { descendantCounts } from '@/lib/promotions'
import { toArabicDigits } from '@/lib/format'
import { cn } from '@/lib/utils'
import {
  PROMOTION_SCOPE,
  PROMOTION_TARGET_KIND,
  type PromotionScopeValue,
  type PromotionTargetKindValue,
} from '@/lib/types'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import { SearchIcon } from '@/components/icon'

export interface PickedTarget {
  kind: PromotionTargetKindValue
  ref_id: string
}

/**
 * Product and group multi-select for an item-level promotion (T140). Rendered
 * only for `Level.Item`; the form clears the selection when the scope moves to
 * `AllProducts`, so storewide stays the *absence* of targets (D3) rather than a
 * list the payload quietly carries along.
 */
export function PromotionTargetPicker({
  tenantId,
  scope,
  value,
  onChange,
  error,
}: {
  tenantId: string
  scope: PromotionScopeValue
  value: PickedTarget[]
  onChange: (next: PickedTarget[]) => void
  error?: string
}) {
  const [search, setSearch] = useState('')
  const isGroups = scope === PROMOTION_SCOPE.Groups

  const groupsQuery = useCatalogGroups(tenantId)
  const productsQuery = useCatalogProducts(tenantId, {
    search: search.trim() || undefined,
    page: 1,
    pageSize: 50,
  })

  const groups = useMemo(() => groupsQuery.data?.data ?? [], [groupsQuery.data])
  const counts = useMemo(() => descendantCounts(groups), [groups])

  const selected = useMemo(() => new Set(value.map((t) => t.ref_id)), [value])

  function toggle(kind: PromotionTargetKindValue, refId: string) {
    onChange(
      selected.has(refId)
        ? value.filter((t) => t.ref_id !== refId)
        : [...value, { kind, ref_id: refId }],
    )
  }

  const filteredGroups = useMemo(() => {
    const q = search.trim()
    return q ? groups.filter((g) => g.name.includes(q)) : groups
  }, [groups, search])

  const rows = isGroups
    ? filteredGroups.map((g) => ({
        id: g.id,
        name: g.name,
        // «و ٤ مجموعات فرعية» — the effective reach, so the author sees what
        // they are actually discounting.
        hint:
          (counts.get(g.id) ?? 0) > 0
            ? `و${toArabicDigits(counts.get(g.id) as number)} مجموعات فرعية`
            : undefined,
      }))
    : (productsQuery.data?.data.items ?? []).map((p) => ({
        id: p.id,
        name: p.name,
        hint: p.group_name ?? undefined,
      }))

  const loading = isGroups ? groupsQuery.isLoading : productsQuery.isLoading

  return (
    <div className="space-y-2">
      <div className="relative">
        <SearchIcon className="pointer-events-none absolute start-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
        <Input
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder={isGroups ? 'ابحث في المجموعات' : 'ابحث في الأصناف'}
          className="ps-9"
        />
      </div>

      {value.length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {value.map((t) => {
            const name = isGroups
              ? groups.find((g) => g.id === t.ref_id)?.name
              : productsQuery.data?.data.items.find((p) => p.id === t.ref_id)?.name
            return (
              <Badge key={t.ref_id} tone="info" className="cursor-pointer">
                <button
                  type="button"
                  onClick={() => toggle(t.kind, t.ref_id)}
                  aria-label={`إزالة ${name ?? ''}`}
                >
                  {/* A selection made before a search narrowed the list still
                      shows — as its id — rather than silently vanishing from
                      the chip row while remaining in the payload. */}
                  {name ?? t.ref_id} ✕
                </button>
              </Badge>
            )
          })}
        </div>
      )}

      <div className="max-h-56 overflow-y-auto rounded-md border border-border">
        {loading ? (
          <p className="p-3 text-sm text-muted-foreground">جارٍ التحميل…</p>
        ) : rows.length === 0 ? (
          <p className="p-3 text-sm text-muted-foreground">لا توجد نتائج.</p>
        ) : (
          <ul className="divide-y divide-border">
            {rows.map((r) => {
              const on = selected.has(r.id)
              return (
                <li key={r.id}>
                  <button
                    type="button"
                    onClick={() =>
                      toggle(
                        isGroups ? PROMOTION_TARGET_KIND.Group : PROMOTION_TARGET_KIND.Product,
                        r.id,
                      )
                    }
                    className={cn(
                      'flex w-full items-center justify-between gap-2 px-3 py-2 text-start text-sm transition-colors hover:bg-muted/50',
                      on && 'bg-muted',
                    )}
                    aria-pressed={on}
                  >
                    <span className="flex items-center gap-2">
                      <span
                        className={cn(
                          'size-4 shrink-0 rounded border border-input',
                          on && 'border-primary bg-primary',
                        )}
                        aria-hidden
                      />
                      {r.name}
                    </span>
                    {r.hint && (
                      <span className="text-xs text-muted-foreground">{r.hint}</span>
                    )}
                  </button>
                </li>
              )
            })}
          </ul>
        )}
      </div>

      {error && <p className="text-xs text-danger">{error}</p>}
    </div>
  )
}
