// Console RBAC (spec-console-rbac T110): the single source every nav item,
// route guard (T111), composed tile (T114), and write button (T115-T117)
// gates through. `useScope` reads the tenant bundle's `me` block (T108) via
// the same `useBundle` query every other bundle consumer already shares, so
// calling it here issues no extra request. `can`/`useCan` are exact set
// membership only: the server has already expanded every stored role's
// `X.manage` into `X.view` (perm.Normalize, api/internal/perm/perm.go)
// before it ever reaches the bundle, so the client never re-derives D3's
// implication rule.
import { useBundle } from './hooks'
import type { TenantMeView } from './types'

// Mirrors api/internal/perm/perm.go's catalog constants one-to-one — the
// single typed source for every permission code referenced client-side.
export const PERM = {
  BranchesView: 'branches.view',
  BranchesManage: 'branches.manage',
  CatalogView: 'catalog.view',
  CatalogManage: 'catalog.manage',
  InventoryView: 'inventory.view',
  CustomersView: 'customers.view',
  CustomersManage: 'customers.manage',
  SuppliersView: 'suppliers.view',
  SuppliersManage: 'suppliers.manage',
  OrdersView: 'orders.view',
  OrdersManage: 'orders.manage',
  ReportsView: 'reports.view',
  ConflictsView: 'conflicts.view',
  ConflictsManage: 'conflicts.manage',
  CompanyManage: 'company.manage',
} as const
export type PermCode = (typeof PERM)[keyof typeof PERM]

/**
 * The requesting member's own role, permissions, and branch allowlist for
 * this tenant — `undefined` while the bundle is still loading (callers
 * should treat that the same as "nothing granted yet", not "denied").
 */
export function useScope(tenantId: string | undefined): TenantMeView | undefined {
  const { data } = useBundle(tenantId)
  return data?.me
}

/**
 * Exact set membership against an already-read scope. A plain function
 * (not a hook) so callers filtering a list — nav items, tiles — read the
 * scope once via `useScope` and check each item against it, rather than
 * calling a hook once per item.
 */
export function can(scope: TenantMeView | undefined, code: string): boolean {
  return scope?.permissions.includes(code) ?? false
}

/** Whether the requesting member holds permission `code` on this tenant. */
export function useCan(tenantId: string | undefined, code: string): boolean {
  return can(useScope(tenantId), code)
}

/**
 * Whether the scope is unscoped — an empty branch allowlist, D4's "sees
 * every branch" state. `undefined` (bundle still loading) is treated as not
 * unscoped, same "nothing granted yet" convention `can` uses, so a write
 * affordance this gates never flashes visible before the scope is known.
 */
export function isUnscoped(scope: TenantMeView | undefined): boolean {
  return scope != null && scope.branch_ids.length === 0
}

/**
 * `can` narrowed by D5c: an operation with no branch identity of its own
 * (product create, price change, add-branch, company edit — see spec D5c's
 * table) cannot be authorized by a branch allowlist, so only an unscoped
 * member may see its affordance. A scoped member holding the permission
 * still passes `can`, but not this — the console hides the button rather
 * than letting the server's `forbidden_unscoped` 403 be the first they hear
 * of it.
 */
export function canUnscoped(scope: TenantMeView | undefined, code: string): boolean {
  return can(scope, code) && isUnscoped(scope)
}

/** Whether the requesting member holds `code` *and* is unscoped (D5c). */
export function useCanUnscoped(tenantId: string | undefined, code: string): boolean {
  return canUnscoped(useScope(tenantId), code)
}
