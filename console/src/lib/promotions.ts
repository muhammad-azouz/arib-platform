import type { CatalogGroup } from './types'

/**
 * Counts each group's transitive descendants.
 *
 * This is the console half of spec D3's cascade: a promotion on "مشروبات"
 * covers "عصائر" and "عصائر طازجة" beneath it, to unlimited depth, and the
 * expansion is resolved at billing time — never stored. So the author cannot
 * see the actual reach from the stored target list alone, and without this
 * count they would be picking a parent group without knowing what they just
 * discounted.
 *
 * Two properties this must have, both because a `parent_id` chain is data and
 * data can be malformed:
 *
 *  - **Cycle-safe.** A → B → A would otherwise recurse forever and hang the
 *    dialog. The visited set makes a cycle terminate instead.
 *  - **Computed from the tree the catalog already loads**, not a new endpoint.
 *
 * Roots carry the all-zero GUID as `parent_id`, matching the desktop's own
 * `ProductGroupCommandService.IsRoot`.
 */
export function descendantCounts(groups: CatalogGroup[]): Map<string, number> {
  const childrenOf = new Map<string, string[]>()
  for (const g of groups) {
    const siblings = childrenOf.get(g.parent_id)
    if (siblings) siblings.push(g.id)
    else childrenOf.set(g.parent_id, [g.id])
  }

  const counts = new Map<string, number>()
  for (const g of groups) {
    // Breadth-first from this group, counting everything reached below it.
    const seen = new Set<string>([g.id])
    const queue = [...(childrenOf.get(g.id) ?? [])]
    let n = 0
    while (queue.length > 0) {
      const id = queue.pop() as string
      if (seen.has(id)) continue // cycle guard — see doc comment
      seen.add(id)
      n++
      queue.push(...(childrenOf.get(id) ?? []))
    }
    counts.set(g.id, n)
  }
  return counts
}
