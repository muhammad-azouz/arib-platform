import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import type { ModuleDef } from '@/lib/types'

interface Props {
  catalog: ModuleDef[] | undefined
  /** Ticked sellable codes (core modules are implied, never listed). */
  value: string[]
  onChange: (codes: string[]) => void
  seats: number
  onSeatsChange: (seats: number) => void
}

/**
 * Module tree rendered from the API catalog: one row per top-level group,
 * its submodules and add-ons indented under it. Core groups show checked and
 * disabled. Ticking a group ticks its children; unticking it clears them;
 * ticking a child ticks its (non-core) parent.
 */
export function ModuleTreePicker({
  catalog,
  value,
  onChange,
  seats,
  onSeatsChange,
}: Props) {
  if (!catalog) {
    return (
      <div className="grid gap-2">
        <Skeleton className="h-5 w-40" />
        <Skeleton className="h-5 w-48" />
        <Skeleton className="h-5 w-36" />
      </div>
    )
  }

  const picked = new Set(value)
  const byOrder = [...catalog].sort((a, b) => a.order - b.order)
  const groups = byOrder.filter((m) => !m.parent)
  const childrenOf = (code: string) => byOrder.filter((m) => m.parent === code)
  const isCore = (code: string) =>
    catalog.find((m) => m.code === code)?.kind === 'core'

  function toggleGroup(group: ModuleDef) {
    const kids = childrenOf(group.code).map((k) => k.code)
    const next = new Set(picked)
    if (picked.has(group.code)) {
      next.delete(group.code)
      kids.forEach((k) => next.delete(k))
    } else {
      next.add(group.code)
      kids.forEach((k) => next.add(k))
    }
    onChange([...next])
  }

  function toggleChild(child: ModuleDef) {
    const next = new Set(picked)
    if (picked.has(child.code)) {
      next.delete(child.code)
    } else {
      next.add(child.code)
      if (child.parent && !isCore(child.parent)) next.add(child.parent)
    }
    onChange([...next])
  }

  return (
    <div className="grid gap-2 rounded-md border border-input p-3">
      {groups.map((g) => {
        const core = g.kind === 'core'
        return (
          <div key={g.code} className="grid gap-1.5">
            <label className="flex items-center gap-2 text-sm font-medium">
              <input
                type="checkbox"
                checked={core || picked.has(g.code)}
                disabled={core}
                onChange={() => toggleGroup(g)}
                className="size-4 rounded border-input"
              />
              {g.name_en}
              <span className="text-muted-foreground" dir="rtl">
                {g.name_ar}
              </span>
              {core && (
                <span className="text-xs font-normal text-muted-foreground">
                  always on
                </span>
              )}
            </label>
            {childrenOf(g.code).map((c) => (
              <div key={c.code} className="ml-6 flex items-center gap-3">
                <label className="flex items-center gap-2 text-sm">
                  <input
                    type="checkbox"
                    checked={picked.has(c.code)}
                    onChange={() => toggleChild(c)}
                    className="size-4 rounded border-input"
                  />
                  {c.name_en}
                  <span className="text-muted-foreground" dir="rtl">
                    {c.name_ar}
                  </span>
                </label>
                {c.valued && picked.has(c.code) && (
                  <Input
                    type="number"
                    min={0}
                    max={100}
                    value={seats || ''}
                    placeholder="Default"
                    onChange={(e) =>
                      onSeatsChange(Math.max(0, Number(e.target.value) || 0))
                    }
                    className="h-8 w-24"
                    aria-label={`${c.name_en} count`}
                  />
                )}
              </div>
            ))}
          </div>
        )
      })}
    </div>
  )
}
