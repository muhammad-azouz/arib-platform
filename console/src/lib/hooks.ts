import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from './api'
import { qk } from './query'
import type { Bundle, BranchStatus, Tenant } from './types'

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
