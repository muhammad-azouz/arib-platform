import { useEffect } from 'react'
import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, session } from './api'
import { qk } from './query'
import type {
  AckConflictsInput,
  Bundle,
  BranchStatus,
  InventoryStatusFilter,
  NewProductInput,
  PriceChangeInput,
  ReportSort,
  Tenant,
} from './types'

/** All tenants owned by the signed-in account. */
export function useTenants() {
  return useQuery({ queryKey: qk.tenants, queryFn: api.listTenants })
}

/**
 * The tenant activation bundle (tenant + company + branches). Shared by the
 * completion gate, the setup wizard, and the overview — TanStack Query dedupes
 * them onto one fetch via the `qk.bundle(id)` key.
 */
export function useBundle(tenantId: string | undefined) {
  return useQuery({
    queryKey: qk.bundle(tenantId ?? ''),
    queryFn: () => api.bundle(tenantId as string),
    enabled: !!tenantId,
  })
}

/**
 * Per-branch sync freshness (HQ read chain: API → gateway → central DB).
 * Refetches on a short interval so "synced X ago" stays honest between the
 * branch's ~5-minute sync rounds; the SSE hook invalidates it the moment a
 * round lands (slice 1).
 */
export function useBranchActivity(tenantId: string | undefined) {
  return useQuery({
    queryKey: qk.branchActivity(tenantId ?? ''),
    queryFn: () => api.branchActivity(tenantId as string),
    enabled: !!tenantId,
    refetchInterval: 60_000,
  })
}

/**
 * Branch views for the Branches dashboard: control-plane branch + health tier
 * + freshness-enveloped day snapshot. Same cadence rationale as
 * `useBranchActivity`; cached data is shown while refetching (no blanking).
 */
export function useHqBranches(tenantId: string | undefined) {
  return useQuery({
    queryKey: qk.hqBranches(tenantId ?? ''),
    queryFn: () => api.hqBranches(tenantId as string),
    enabled: !!tenantId,
    refetchInterval: 60_000,
  })
}

/** Every product group for the Catalog page's tree sidebar. */
export function useCatalogGroups(tenantId: string | undefined) {
  return useQuery({
    queryKey: qk.catalogGroups(tenantId ?? ''),
    queryFn: () => api.catalogGroups(tenantId as string),
    enabled: !!tenantId,
  })
}

export interface CatalogProductsParams {
  search?: string
  groupId?: string
  page?: number
  pageSize?: number
}

/**
 * One page of the searchable/filterable product list. `keepPreviousData`
 * keeps the last page's rows on screen while a new page/search/group loads,
 * so paging and typing a search term never blank the table.
 */
export function useCatalogProducts(
  tenantId: string | undefined,
  params: CatalogProductsParams,
) {
  return useQuery({
    queryKey: qk.catalogProducts(tenantId ?? '', params),
    queryFn: () => api.catalogProducts(tenantId as string, params),
    enabled: !!tenantId,
    placeholderData: keepPreviousData,
  })
}

/** One product's full detail: units/prices/barcodes + per-branch availability. */
export function useCatalogProduct(tenantId: string | undefined, productId: string | undefined) {
  return useQuery({
    queryKey: qk.catalogProduct(tenantId ?? '', productId ?? ''),
    queryFn: () => api.catalogProduct(tenantId as string, productId as string),
    enabled: !!tenantId && !!productId,
  })
}

/**
 * Change one or more units' prices (`PUT …/prices`, the first HQ write). On
 * success the product detail is refetched so the units table shows the new
 * numbers; the propagation panel's "has this branch synced it yet" check
 * reuses `useHqBranches`' already-live `last_sync_at` (T14's SSE), so no
 * extra invalidation is needed for chips to flip.
 */
export function useChangeProductPrices(tenantId: string, productId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (changes: PriceChangeInput[]) => api.changeProductPrices(tenantId, productId, changes),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.catalogProduct(tenantId, productId) })
    },
  })
}

/**
 * Create a product (`POST …/catalog/products`, the second HQ write). On
 * success, invalidates every `catalog-products` query for this tenant
 * (TanStack Query's array-key prefix match covers every search/group/page
 * variation) so the new row appears the moment the Catalog page is revisited,
 * and `catalog-groups` too since the group's product_count changed.
 */
export function useCreateProduct(tenantId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: NewProductInput) => api.createProduct(tenantId, input),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['catalog-products', tenantId] })
      void qc.invalidateQueries({ queryKey: qk.catalogGroups(tenantId) })
    },
  })
}

/** One page of the "by product" inventory view. */
export interface InventoryProductsParams {
  search?: string
  groupId?: string
  branchId?: string
  status?: InventoryStatusFilter
  page?: number
  pageSize?: number
}

/** Per-branch stock summary for the "by branch" inventory view. */
export function useInventoryBranches(tenantId: string | undefined) {
  return useQuery({
    queryKey: qk.inventoryBranches(tenantId ?? ''),
    queryFn: () => api.inventoryBranches(tenantId as string),
    enabled: !!tenantId,
  })
}

/** One page of the "by product" inventory view, filterable by branch/status. */
export function useInventoryProducts(
  tenantId: string | undefined,
  params: InventoryProductsParams,
) {
  return useQuery({
    queryKey: qk.inventoryProducts(tenantId ?? '', params),
    queryFn: () => api.inventoryProducts(tenantId as string, params),
    enabled: !!tenantId,
    placeholderData: keepPreviousData,
  })
}

/** One page of the needs-attention view (low/out/negative stock, severity-ordered). */
export function useInventoryAttention(
  tenantId: string | undefined,
  params: { branchId?: string; page?: number; pageSize?: number },
) {
  return useQuery({
    queryKey: qk.inventoryAttention(tenantId ?? '', params),
    queryFn: () => api.inventoryAttention(tenantId as string, params),
    enabled: !!tenantId,
    placeholderData: keepPreviousData,
  })
}

/**
 * One product's movement history. `enabled` additionally gates on the
 * caller's own flag so the ProductDetail page's collapsible section issues
 * zero requests until opened.
 */
export function useProductMovements(
  tenantId: string | undefined,
  productId: string | undefined,
  params: { branchId?: string; from?: string; to?: string; page?: number; pageSize?: number },
  enabled: boolean,
) {
  return useQuery({
    queryKey: qk.productMovements(tenantId ?? '', productId ?? '', params),
    queryFn: () => api.productMovements(tenantId as string, productId as string, params),
    enabled: !!tenantId && !!productId && enabled,
    placeholderData: keepPreviousData,
  })
}

/** One page of the sync-conflict review log (T39), or just the unacked count for the bell (T38). */
export interface ConflictsParams {
  page?: number
  pageSize?: number
  all?: boolean
}

export function useConflicts(tenantId: string | undefined, params: ConflictsParams) {
  return useQuery({
    queryKey: qk.conflicts(tenantId ?? '', params),
    queryFn: () => api.conflicts(tenantId as string, params),
    enabled: !!tenantId,
    placeholderData: keepPreviousData,
  })
}

/** Acknowledge conflicts by id and/or up-to-id; clears them from every mounted view (bell + review page). */
export function useAckConflicts(tenantId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: AckConflictsInput) => api.ackConflicts(tenantId, input),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['hq-conflicts', tenantId] })
    },
  })
}

/**
 * Live console updates: subscribes to the tenant's SSE stream and, on
 * branch-synced, invalidates the branch-derived query keys so every mounted
 * page refetches — a desktop "Sync Now" flips the cards with no page refresh.
 * Mounted once in AppShell. Reconnection is manual (not EventSource's
 * built-in) because the access token in the URL rotates: on any error we
 * close, refresh the session, and reconnect with the fresh token.
 */
export function useTenantEvents(tenantId: string | undefined) {
  const qc = useQueryClient()
  useEffect(() => {
    if (!tenantId) return
    let es: EventSource | null = null
    let retry: number | undefined
    let stopped = false

    const connect = async (refreshFirst: boolean) => {
      if (refreshFirst) await session.refresh()
      if (stopped) return
      es = new EventSource(api.eventsUrl(tenantId))
      es.addEventListener('branch-synced', () => {
        void qc.invalidateQueries({ queryKey: qk.branchActivity(tenantId) })
        void qc.invalidateQueries({ queryKey: qk.hqBranches(tenantId) })
        // Shared 'hq-inventory' prefix covers by-branch/by-product/attention/
        // movements in one call — a POS sale or branch adjustment flips stock
        // numbers and attention rows live, same mechanism as the branch cards.
        void qc.invalidateQueries({ queryKey: ['hq-inventory', tenantId] })
        // Conflicts only change on sync rounds (ServerWins resolves them at
        // upload time), so a branch-synced event is exactly when new rows
        // can appear.
        void qc.invalidateQueries({ queryKey: ['hq-conflicts', tenantId] })
        // Report figures come from Bills the round just uploaded — same
        // moment, same mechanism.
        void qc.invalidateQueries({ queryKey: ['hq-reports', tenantId] })
      })
      es.onerror = () => {
        es?.close()
        window.clearTimeout(retry)
        retry = window.setTimeout(() => void connect(true), 5_000)
      }
    }
    void connect(false)

    return () => {
      stopped = true
      window.clearTimeout(retry)
      es?.close()
    }
  }, [tenantId, qc])
}

// --- Reports (slice 6): question-organized period aggregates. All four keep
// previous data so a period/filter change never blanks the view (Catalog's
// pattern); the shared 'hq-reports' key prefix is SSE-invalidated. ---

export interface ReportPeriodParams {
  from?: string
  to?: string
}

/** Period sales report: totals, tender split and gap-filled day series. */
export function useReportSales(
  tenantId: string | undefined,
  params: ReportPeriodParams & { branchId?: string },
) {
  return useQuery({
    queryKey: qk.reportSales(tenantId ?? '', params),
    queryFn: () => api.reportSales(tenantId as string, params),
    enabled: !!tenantId,
    placeholderData: keepPreviousData,
  })
}

/** One page of the period products report (top sellers by revenue/qty/profit). */
export function useReportProducts(
  tenantId: string | undefined,
  params: ReportPeriodParams & {
    branchId?: string
    groupId?: string
    sort?: ReportSort
    page?: number
    pageSize?: number
  },
) {
  return useQuery({
    queryKey: qk.reportProducts(tenantId ?? '', params),
    queryFn: () => api.reportProducts(tenantId as string, params),
    enabled: !!tenantId,
    placeholderData: keepPreviousData,
  })
}

/** Period branches comparison (registry-complete, health-decorated). */
export function useReportBranches(tenantId: string | undefined, params: ReportPeriodParams) {
  return useQuery({
    queryKey: qk.reportBranches(tenantId ?? '', params),
    queryFn: () => api.reportBranches(tenantId as string, params),
    enabled: !!tenantId,
    placeholderData: keepPreviousData,
  })
}

/** Period per-cashier report. */
export function useReportStaff(
  tenantId: string | undefined,
  params: ReportPeriodParams & { branchId?: string },
) {
  return useQuery({
    queryKey: qk.reportStaff(tenantId ?? '', params),
    queryFn: () => api.reportStaff(tenantId as string, params),
    enabled: !!tenantId,
    placeholderData: keepPreviousData,
  })
}

/** Create a tenant and prime the list cache so the resolver sees it instantly. */
export function useCreateTenant() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (name: string) => api.createTenant(name),
    onSuccess: (t) => {
      qc.setQueryData<Tenant[]>(qk.tenants, (prev) => (prev ? [...prev, t] : [t]))
      void qc.invalidateQueries({ queryKey: qk.tenants })
    },
  })
}

export interface CompanyInput {
  id?: string
  name: string
  phone?: string
  address?: string
  tax_number?: string
}

/**
 * Create-or-update the tenant's single company (`PUT …/company`). On success we
 * patch the cached bundle so the setup wizard advances to the branch step
 * instantly, then revalidate.
 */
export function useSetCompany(tenantId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: CompanyInput) => api.setCompany(tenantId, input),
    onSuccess: (company) => {
      qc.setQueryData<Bundle>(qk.bundle(tenantId), (prev) =>
        prev ? { ...prev, Company: company } : prev,
      )
      void qc.invalidateQueries({ queryKey: qk.bundle(tenantId) })
    },
  })
}

export interface BranchInput {
  id?: string
  company_id?: string
  name: string
  phone1?: string // required by the form; printed on receipts
  phone2?: string
  phone3?: string
  address?: string // required by the form; printed on receipts
  seats?: number // admin-controlled; omitted by the merchant console (defaults to 1)
}

/**
 * Create a branch (`POST …/branches`). Appending to the cached bundle is what
 * clears the completion gate: the setup wizard's gate re-reads the bundle, sees
 * a branch, and routes the user out to the tenant dashboard.
 */
export function useAddBranch(tenantId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: BranchInput) => api.addBranch(tenantId, input),
    onSuccess: (branch) => {
      qc.setQueryData<Bundle>(qk.bundle(tenantId), (prev) =>
        prev ? { ...prev, Branches: [...(prev.Branches ?? []), branch] } : prev,
      )
      void qc.invalidateQueries({ queryKey: qk.bundle(tenantId) })
    },
  })
}

/** Rename and/or activate-deactivate a branch (`PATCH …/branches/{branchId}`). */
export function useUpdateBranch(tenantId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({
      branchId,
      ...input
    }: {
      branchId: string
      name?: string
      phone1?: string
      phone2?: string
      phone3?: string
      address?: string
      status?: BranchStatus
    }) => api.updateBranch(tenantId, branchId, input),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.bundle(tenantId) })
    },
  })
}

/**
 * Setup is complete only once the tenant has its one company AND at least one
 * branch. The completion gate keys off this; the server returns nullable
 * Company + Branches in the bundle, so no extra endpoint is needed.
 */
export function bundleIsComplete(b: Bundle): boolean {
  return b.Company != null && (b.Branches?.length ?? 0) > 0
}
