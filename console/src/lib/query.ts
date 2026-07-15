import { QueryClient } from '@tanstack/react-query'

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      retry: 1,
      refetchOnWindowFocus: false,
    },
  },
})

export const qk = {
  me: ['me'] as const,
  tenants: ['tenants'] as const,
  bundle: (id: string) => ['bundle', id] as const,
  branchActivity: (id: string) => ['branch-activity', id] as const,
  hqBranches: (id: string) => ['hq-branches', id] as const,
  catalogGroups: (id: string) => ['catalog-groups', id] as const,
  catalogProducts: (
    id: string,
    params: { search?: string; groupId?: string; page?: number; pageSize?: number },
  ) => ['catalog-products', id, params] as const,
  catalogProduct: (id: string, productId: string) => ['catalog-product', id, productId] as const,
  // Shared 'hq-inventory' prefix so one SSE invalidation (see useTenantEvents)
  // covers every inventory view at once.
  inventoryBranches: (id: string) => ['hq-inventory', id, 'branches'] as const,
  inventoryProducts: (
    id: string,
    params: {
      search?: string
      groupId?: string
      branchId?: string
      status?: string
      page?: number
      pageSize?: number
    },
  ) => ['hq-inventory', id, 'products', params] as const,
  inventoryAttention: (
    id: string,
    params: { branchId?: string; page?: number; pageSize?: number },
  ) => ['hq-inventory', id, 'attention', params] as const,
  productMovements: (
    id: string,
    productId: string,
    params: { branchId?: string; from?: string; to?: string; page?: number; pageSize?: number },
  ) => ['hq-inventory', id, 'movements', productId, params] as const,
  // 'hq-conflicts' prefix so useTenantEvents can invalidate every page/filter
  // variant in one call, same pattern as 'hq-inventory'.
  conflicts: (
    id: string,
    params: { page?: number; pageSize?: number; all?: boolean },
  ) => ['hq-conflicts', id, params] as const,
  // Shared 'hq-reports' prefix — one SSE invalidation flips every report view
  // (a sync round is exactly when new bills can land in a period).
  reportSales: (
    id: string,
    params: { from?: string; to?: string; branchId?: string },
  ) => ['hq-reports', id, 'sales', params] as const,
  reportProducts: (
    id: string,
    params: {
      from?: string
      to?: string
      branchId?: string
      groupId?: string
      sort?: string
      page?: number
      pageSize?: number
    },
  ) => ['hq-reports', id, 'products', params] as const,
  reportBranches: (
    id: string,
    params: { from?: string; to?: string },
  ) => ['hq-reports', id, 'branches', params] as const,
  reportStaff: (
    id: string,
    params: { from?: string; to?: string; branchId?: string },
  ) => ['hq-reports', id, 'staff', params] as const,
}
