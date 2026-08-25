import { api } from '@/lib/api'

// Duplicate-name guard for customer create (CreateCustomerDialog and
// QuickAddCustomerDialog both call this before create.mutateAsync). Client-
// side, not gateway-enforced — the gateway's `search` filter is a substring
// match, so this narrows the page it returns down to an exact, trimmed,
// case-insensitive name match. Scoped to one branch: Customers is a Tier-B
// branch-specific table (same reasoning as T97's cross-branch order block —
// two different real customers at two different branches sharing a name is
// not a duplicate).
//
// pageSize is 50, not the UI picker's 5–8: `search` is a substring match, so
// a branch with more than a handful of names containing the search term
// would otherwise push the real duplicate past page 1 and silently miss it.
export async function findDuplicateCustomerName(
  tenantId: string,
  branchId: string,
  name: string,
): Promise<boolean> {
  const trimmed = name.trim()
  if (!trimmed) return false
  const page = await api.customers(tenantId, { search: trimmed, branchId, page: 1, pageSize: 50 })
  return page.data.items.some((c) => c.name.trim().toLowerCase() === trimmed.toLowerCase())
}
