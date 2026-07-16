import type {
  AckConflictsInput,
  AckConflictsResult,
  AttentionResponse,
  Branch,
  BranchActivityResponse,
  BranchDevice,
  BranchesReportResponse,
  BulkUpdateCustomersInput,
  BulkUpdateCustomersResult,
  BulkUpdateSuppliersInput,
  BulkUpdateSuppliersResult,
  Bundle,
  CatalogGroupsResponse,
  CatalogProductsResponse,
  Company,
  ConflictsResponse,
  CustomerDebtFilter,
  CustomerDetailResponse,
  CustomerEditInput,
  CustomerGroupsResponse,
  CustomerInsightsResponse,
  CustomerLedgerResponse,
  CustomerPurchasesResponse,
  CustomersResponse,
  HqBranchesResponse,
  ImportCustomersResult,
  ImportSuppliersResult,
  InventoryBranchesResponse,
  InventoryProductsResponse,
  InventoryStatusFilter,
  MeView,
  MovementsResponse,
  NewCustomerInput,
  NewCustomerResult,
  NewProductInput,
  NewProductResult,
  NewSupplierInput,
  NewSupplierResult,
  PriceChangeInput,
  PriceChangeResult,
  ProductDetailResponse,
  ProductsReportResponse,
  ReportSort,
  SalesReportResponse,
  Session,
  StaffReportResponse,
  SupplierDebtFilter,
  SupplierDetailResponse,
  SupplierEditInput,
  SupplierInsightsResponse,
  SupplierLedgerResponse,
  SupplierPurchasesResponse,
  SuppliersResponse,
  SyncToken,
  Tenant,
  UpdateCustomerResult,
  UpdateSupplierResult,
} from './types'

const BASE = import.meta.env.VITE_API_BASE_URL ?? ''
const REFRESH_KEY = 'arib_console_refresh'

// Public, ungated Windows installer for the desktop app (see
// platform/api/internal/httpapi/updates_handlers.go — Setup.exe is served
// free of the license gate). Always the `lts` channel: canary is an opt-in
// switch inside the app itself, not something to hand out from the console.
export const installerUrl = () => `${BASE}/updates/lts/win-x64/AribONE-lts-Setup.exe`

let accessToken: string | null = null
let onLogout: (() => void) | null = null

export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
    this.name = 'ApiError'
  }
}

export const session = {
  setAccess(token: string | null) {
    accessToken = token
  },
  getAccess() {
    return accessToken
  },
  getRefresh() {
    return localStorage.getItem(REFRESH_KEY)
  },
  setRefresh(token: string | null) {
    if (token) localStorage.setItem(REFRESH_KEY, token)
    else localStorage.removeItem(REFRESH_KEY)
  },
  store(s: Pick<Session, 'access_token' | 'refresh_token'>) {
    accessToken = s.access_token
    localStorage.setItem(REFRESH_KEY, s.refresh_token)
  },
  clear() {
    accessToken = null
    localStorage.removeItem(REFRESH_KEY)
  },
  setLogoutHandler(fn: () => void) {
    onLogout = fn
  },
  /** Mint a fresh access token from the stored refresh token (cold start). */
  refresh() {
    return tryRefresh()
  },
}

async function parseError(res: Response): Promise<string> {
  try {
    const body = await res.json()
    if (body && typeof body.error === 'string') return body.error
  } catch {
    /* not json */
  }
  return res.statusText || `request failed (${res.status})`
}

async function rawFetch(path: string, init: RequestInit): Promise<Response> {
  const headers = new Headers(init.headers)
  if (!headers.has('Content-Type') && init.body) {
    headers.set('Content-Type', 'application/json')
  }
  if (accessToken) headers.set('Authorization', `Bearer ${accessToken}`)
  return fetch(`${BASE}${path}`, { ...init, headers })
}

// Bearer-only fetch with no auto Content-Type — customers import/export (T61)
// need this instead of rawFetch: a FormData body needs the browser's own
// multipart boundary header, and a blob download needs no Content-Type at all.
async function authedFetch(path: string, init: RequestInit = {}): Promise<Response> {
  const headers = new Headers(init.headers)
  if (accessToken) headers.set('Authorization', `Bearer ${accessToken}`)
  return fetch(`${BASE}${path}`, { ...init, headers })
}

let refreshPromise: Promise<boolean> | null = null

/**
 * Attempt to mint a new access token from the stored refresh token.
 * Concurrent callers (e.g. several requests 401-ing at once) share one
 * in-flight refresh instead of racing — the server rotates the refresh
 * token on each use, so a second parallel call would otherwise submit an
 * already-invalidated token and force a spurious logout.
 */
function tryRefresh(): Promise<boolean> {
  if (!refreshPromise) {
    refreshPromise = doRefresh().finally(() => {
      refreshPromise = null
    })
  }
  return refreshPromise
}

async function doRefresh(): Promise<boolean> {
  const refresh = session.getRefresh()
  if (!refresh) return false
  const res = await fetch(`${BASE}/v1/auth/refresh`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ refresh_token: refresh }),
  })
  if (!res.ok) return false
  const data = (await res.json()) as Session
  session.setAccess(data.access_token)
  session.setRefresh(data.refresh_token)
  return true
}

/** Authenticated request with one transparent refresh-and-retry on 401. */
async function request<T>(
  path: string,
  init: RequestInit = {},
  retry = true,
): Promise<T> {
  const res = await rawFetch(path, init)
  if (res.status === 401 && retry) {
    if (await tryRefresh()) return request<T>(path, init, false)
    session.clear()
    onLogout?.()
    throw new ApiError(401, 'session expired')
  }
  if (!res.ok) throw new ApiError(res.status, await parseError(res))
  if (res.status === 204) return undefined as T
  return (await res.json()) as T
}

const post = (v: unknown): RequestInit => ({
  method: 'POST',
  body: JSON.stringify(v ?? {}),
})

// ---- Public auth (no bearer needed) ----

export const authApi = {
  async emailStart(email: string): Promise<{ exists: boolean }> {
    const res = await fetch(`${BASE}/v1/auth/email/start`, post({ email }))
    if (!res.ok) throw new ApiError(res.status, await parseError(res))
    return (await res.json()) as { status: string; exists: boolean }
  },
  async emailVerify(
    email: string,
    code: string,
    firstName = '',
    lastName = '',
  ): Promise<Session> {
    const res = await fetch(
      `${BASE}/v1/auth/email/verify`,
      post({ email, code, first_name: firstName, last_name: lastName }),
    )
    if (!res.ok) throw new ApiError(res.status, await parseError(res))
    return (await res.json()) as Session
  },
  async logout(): Promise<void> {
    const refresh = session.getRefresh()
    if (!refresh) return
    await fetch(`${BASE}/v1/auth/logout`, post({ refresh_token: refresh })).catch(
      () => undefined,
    )
  },
}

// ---- Tenant console (bearer required) ----

export const api = {
  me: () => request<MeView>('/v1/me'),

  // tenants
  listTenants: () => request<Tenant[] | null>('/v1/tenants').then((r) => r ?? []),
  createTenant: (name: string) => request<Tenant>('/v1/tenants', post({ name })),
  bundle: (tenantId: string) => request<Bundle>(`/v1/tenants/${tenantId}`),

  // company (one per tenant; PUT create-or-update, client-minted GUID id)
  setCompany: (
    tenantId: string,
    input: {
      id?: string
      name: string
      phone?: string
      address?: string
      tax_number?: string
    },
  ) =>
    request<Company>(`/v1/tenants/${tenantId}/company`, {
      method: 'PUT',
      body: JSON.stringify(input),
    }),

  // branches
  addBranch: (
    tenantId: string,
    // seats is admin-controlled (paid licensing lever); the merchant omits it and
    // the server defaults a new branch to 1 seat.
    input: {
      id?: string
      company_id?: string
      name: string
      phone1?: string
      phone2?: string
      phone3?: string
      address?: string
      seats?: number
    },
  ) => request<Branch>(`/v1/tenants/${tenantId}/branches`, post(input)),

  updateBranch: (
    tenantId: string,
    branchId: string,
    input: {
      name?: string
      phone1?: string
      phone2?: string
      phone3?: string
      address?: string
      status?: 'active' | 'deactivated'
    },
  ) =>
    request<{ status: string }>(`/v1/tenants/${tenantId}/branches/${branchId}`, {
      method: 'PATCH',
      body: JSON.stringify(input),
    }),

  // device seats
  bindDevice: (
    tenantId: string,
    branchId: string,
    input: { machine_id: string; machine_name?: string; os?: string },
  ) =>
    request<BranchDevice>(
      `/v1/tenants/${tenantId}/branches/${branchId}/bind`,
      post(input),
    ),

  releaseDevice: (tenantId: string, deviceId: string) =>
    request<{ status: string }>(
      `/v1/tenants/${tenantId}/devices/${deviceId}/release`,
      post({}),
    ),

  // sync token for connecting a desktop install
  syncToken: (tenantId: string, deviceId: string) =>
    request<SyncToken>(`/v1/tenants/${tenantId}/sync-token`, post({ device_id: deviceId })),

  // HQ reads (freshness-enveloped, via the sync gateway)
  branchActivity: (tenantId: string) =>
    request<BranchActivityResponse>(`/v1/tenants/${tenantId}/hq/branch-activity`),

  hqBranches: (tenantId: string) =>
    request<HqBranchesResponse>(`/v1/tenants/${tenantId}/hq/branches`),

  // catalog (slice 3): groups + paged/searchable products, read live off the
  // tenant central DB via the same HQ chain.
  catalogGroups: (tenantId: string) =>
    request<CatalogGroupsResponse>(`/v1/tenants/${tenantId}/hq/catalog/groups`),

  catalogProducts: (
    tenantId: string,
    params: { search?: string; groupId?: string; page?: number; pageSize?: number },
  ) => {
    const q = new URLSearchParams()
    if (params.search) q.set('search', params.search)
    if (params.groupId) q.set('group_id', params.groupId)
    if (params.page) q.set('page', String(params.page))
    if (params.pageSize) q.set('page_size', String(params.pageSize))
    const qs = q.toString()
    return request<CatalogProductsResponse>(
      `/v1/tenants/${tenantId}/hq/catalog/products${qs ? `?${qs}` : ''}`,
    )
  },

  catalogProduct: (tenantId: string, productId: string) =>
    request<ProductDetailResponse>(
      `/v1/tenants/${tenantId}/hq/catalog/products/${productId}`,
    ),

  changeProductPrices: (tenantId: string, productId: string, changes: PriceChangeInput[]) =>
    request<PriceChangeResult>(
      `/v1/tenants/${tenantId}/hq/catalog/products/${productId}/prices`,
      { method: 'PUT', body: JSON.stringify({ changes }) },
    ),

  createProduct: (tenantId: string, input: NewProductInput) =>
    request<NewProductResult>(`/v1/tenants/${tenantId}/hq/catalog/products`, post(input)),

  // inventory (slice 4): one dataset, three perspectives over the same HQ
  // chain — search/group/branch/status params are gateway-owned, same as
  // catalogProducts.
  inventoryBranches: (tenantId: string) =>
    request<InventoryBranchesResponse>(`/v1/tenants/${tenantId}/hq/inventory/branches`),

  inventoryProducts: (
    tenantId: string,
    params: {
      search?: string
      groupId?: string
      branchId?: string
      status?: InventoryStatusFilter
      page?: number
      pageSize?: number
    },
  ) => {
    const q = new URLSearchParams()
    if (params.search) q.set('search', params.search)
    if (params.groupId) q.set('group_id', params.groupId)
    if (params.branchId) q.set('branch_id', params.branchId)
    if (params.status) q.set('status', params.status)
    if (params.page) q.set('page', String(params.page))
    if (params.pageSize) q.set('page_size', String(params.pageSize))
    const qs = q.toString()
    return request<InventoryProductsResponse>(
      `/v1/tenants/${tenantId}/hq/inventory/products${qs ? `?${qs}` : ''}`,
    )
  },

  inventoryAttention: (
    tenantId: string,
    params: { branchId?: string; page?: number; pageSize?: number },
  ) => {
    const q = new URLSearchParams()
    if (params.branchId) q.set('branch_id', params.branchId)
    if (params.page) q.set('page', String(params.page))
    if (params.pageSize) q.set('page_size', String(params.pageSize))
    const qs = q.toString()
    return request<AttentionResponse>(
      `/v1/tenants/${tenantId}/hq/inventory/attention${qs ? `?${qs}` : ''}`,
    )
  },

  productMovements: (
    tenantId: string,
    productId: string,
    params: { branchId?: string; from?: string; to?: string; page?: number; pageSize?: number },
  ) => {
    const q = new URLSearchParams()
    if (params.branchId) q.set('branch_id', params.branchId)
    if (params.from) q.set('from', params.from)
    if (params.to) q.set('to', params.to)
    if (params.page) q.set('page', String(params.page))
    if (params.pageSize) q.set('page_size', String(params.pageSize))
    const qs = q.toString()
    return request<MovementsResponse>(
      `/v1/tenants/${tenantId}/hq/catalog/products/${productId}/movements${qs ? `?${qs}` : ''}`,
    )
  },

  // conflicts (slice 5): ConflictLog review chain, same HQ chain as
  // catalog/inventory.
  conflicts: (
    tenantId: string,
    params: { page?: number; pageSize?: number; all?: boolean },
  ) => {
    const q = new URLSearchParams()
    if (params.page) q.set('page', String(params.page))
    if (params.pageSize) q.set('page_size', String(params.pageSize))
    if (params.all) q.set('all', '1')
    const qs = q.toString()
    return request<ConflictsResponse>(
      `/v1/tenants/${tenantId}/hq/conflicts${qs ? `?${qs}` : ''}`,
    )
  },

  ackConflicts: (tenantId: string, input: AckConflictsInput) =>
    request<AckConflictsResult>(`/v1/tenants/${tenantId}/hq/conflicts/ack`, post(input)),

  // reports (slice 6): question-organized period aggregates, same HQ chain.
  // from/to are plain YYYY-MM-DD dates; the gateway owns defaulting (last 7
  // days) and clamping (max a year).
  reportSales: (
    tenantId: string,
    params: { from?: string; to?: string; branchId?: string },
  ) => {
    const q = new URLSearchParams()
    if (params.from) q.set('from', params.from)
    if (params.to) q.set('to', params.to)
    if (params.branchId) q.set('branch_id', params.branchId)
    const qs = q.toString()
    return request<SalesReportResponse>(
      `/v1/tenants/${tenantId}/hq/reports/sales${qs ? `?${qs}` : ''}`,
    )
  },

  reportProducts: (
    tenantId: string,
    params: {
      from?: string
      to?: string
      branchId?: string
      groupId?: string
      sort?: ReportSort
      page?: number
      pageSize?: number
    },
  ) => {
    const q = new URLSearchParams()
    if (params.from) q.set('from', params.from)
    if (params.to) q.set('to', params.to)
    if (params.branchId) q.set('branch_id', params.branchId)
    if (params.groupId) q.set('group_id', params.groupId)
    if (params.sort) q.set('sort', params.sort)
    if (params.page) q.set('page', String(params.page))
    if (params.pageSize) q.set('page_size', String(params.pageSize))
    const qs = q.toString()
    return request<ProductsReportResponse>(
      `/v1/tenants/${tenantId}/hq/reports/products${qs ? `?${qs}` : ''}`,
    )
  },

  reportBranches: (tenantId: string, params: { from?: string; to?: string }) => {
    const q = new URLSearchParams()
    if (params.from) q.set('from', params.from)
    if (params.to) q.set('to', params.to)
    const qs = q.toString()
    return request<BranchesReportResponse>(
      `/v1/tenants/${tenantId}/hq/reports/branches${qs ? `?${qs}` : ''}`,
    )
  },

  reportStaff: (
    tenantId: string,
    params: { from?: string; to?: string; branchId?: string },
  ) => {
    const q = new URLSearchParams()
    if (params.from) q.set('from', params.from)
    if (params.to) q.set('to', params.to)
    if (params.branchId) q.set('branch_id', params.branchId)
    const qs = q.toString()
    return request<StaffReportResponse>(
      `/v1/tenants/${tenantId}/hq/reports/staff${qs ? `?${qs}` : ''}`,
    )
  },

  // customers (slice 7): read-mostly, branch-specific — same HQ chain as
  // catalog/inventory/reports. search/branch/group/active/debt/page/pageSize
  // are gateway-owned filters, same convention as inventoryProducts.
  customerGroups: (tenantId: string) =>
    request<CustomerGroupsResponse>(`/v1/tenants/${tenantId}/hq/customer-groups`),

  customers: (
    tenantId: string,
    params: {
      search?: string
      branchId?: string
      groupId?: string
      active?: boolean
      debt?: CustomerDebtFilter
      page?: number
      pageSize?: number
    },
  ) => {
    const q = new URLSearchParams()
    if (params.search) q.set('search', params.search)
    if (params.branchId) q.set('branch_id', params.branchId)
    if (params.groupId) q.set('group_id', params.groupId)
    if (params.active !== undefined) q.set('active', String(params.active))
    if (params.debt) q.set('debt', params.debt)
    if (params.page) q.set('page', String(params.page))
    if (params.pageSize) q.set('page_size', String(params.pageSize))
    const qs = q.toString()
    return request<CustomersResponse>(`/v1/tenants/${tenantId}/hq/customers${qs ? `?${qs}` : ''}`)
  },

  customer: (tenantId: string, customerId: string) =>
    request<CustomerDetailResponse>(`/v1/tenants/${tenantId}/hq/customers/${customerId}`),

  customerPurchases: (
    tenantId: string,
    customerId: string,
    params: { page?: number; pageSize?: number },
  ) => {
    const q = new URLSearchParams()
    if (params.page) q.set('page', String(params.page))
    if (params.pageSize) q.set('page_size', String(params.pageSize))
    const qs = q.toString()
    return request<CustomerPurchasesResponse>(
      `/v1/tenants/${tenantId}/hq/customers/${customerId}/purchases${qs ? `?${qs}` : ''}`,
    )
  },

  customerLedger: (
    tenantId: string,
    customerId: string,
    params: { page?: number; pageSize?: number },
  ) => {
    const q = new URLSearchParams()
    if (params.page) q.set('page', String(params.page))
    if (params.pageSize) q.set('page_size', String(params.pageSize))
    const qs = q.toString()
    return request<CustomerLedgerResponse>(
      `/v1/tenants/${tenantId}/hq/customers/${customerId}/ledger${qs ? `?${qs}` : ''}`,
    )
  },

  customerInsights: (
    tenantId: string,
    params: { branchId?: string; from?: string; to?: string },
  ) => {
    const q = new URLSearchParams()
    if (params.branchId) q.set('branch_id', params.branchId)
    if (params.from) q.set('from', params.from)
    if (params.to) q.set('to', params.to)
    const qs = q.toString()
    return request<CustomerInsightsResponse>(
      `/v1/tenants/${tenantId}/hq/customers/insights${qs ? `?${qs}` : ''}`,
    )
  },

  createCustomer: (tenantId: string, input: NewCustomerInput) =>
    request<NewCustomerResult>(`/v1/tenants/${tenantId}/hq/customers`, post(input)),

  updateCustomer: (tenantId: string, customerId: string, input: CustomerEditInput) =>
    request<UpdateCustomerResult>(`/v1/tenants/${tenantId}/hq/customers/${customerId}`, {
      method: 'PUT',
      body: JSON.stringify(input),
    }),

  bulkUpdateCustomers: (tenantId: string, input: BulkUpdateCustomersInput) =>
    request<BulkUpdateCustomersResult>(`/v1/tenants/${tenantId}/hq/customers/bulk`, {
      method: 'PUT',
      body: JSON.stringify(input),
    }),

  // Blob/multipart — bypass the JSON `request` helper (see authedFetch).
  exportCustomers: async (
    tenantId: string,
    params: {
      search?: string
      branchId?: string
      groupId?: string
      active?: boolean
      debt?: CustomerDebtFilter
    },
  ): Promise<Blob> => {
    const q = new URLSearchParams()
    if (params.search) q.set('search', params.search)
    if (params.branchId) q.set('branch_id', params.branchId)
    if (params.groupId) q.set('group_id', params.groupId)
    if (params.active !== undefined) q.set('active', String(params.active))
    if (params.debt) q.set('debt', params.debt)
    const qs = q.toString()
    const res = await authedFetch(
      `/v1/tenants/${tenantId}/hq/customers/export${qs ? `?${qs}` : ''}`,
    )
    if (!res.ok) throw new ApiError(res.status, await parseError(res))
    return res.blob()
  },

  importCustomers: async (
    tenantId: string,
    file: File,
    branchId: string,
  ): Promise<ImportCustomersResult> => {
    const form = new FormData()
    form.set('file', file)
    form.set('branch_id', branchId)
    const res = await authedFetch(`/v1/tenants/${tenantId}/hq/customers/import`, {
      method: 'POST',
      body: form,
    })
    if (!res.ok) throw new ApiError(res.status, await parseError(res))
    return (await res.json()) as ImportCustomersResult
  },

  // suppliers (slice 8): mirrors the customers block above verbatim, one
  // prefix over. customerGroups above is reused for suppliers too — groups
  // aren't type-scoped in the schema, no supplierGroups function.
  suppliers: (
    tenantId: string,
    params: {
      search?: string
      branchId?: string
      groupId?: string
      active?: boolean
      debt?: SupplierDebtFilter
      page?: number
      pageSize?: number
    },
  ) => {
    const q = new URLSearchParams()
    if (params.search) q.set('search', params.search)
    if (params.branchId) q.set('branch_id', params.branchId)
    if (params.groupId) q.set('group_id', params.groupId)
    if (params.active !== undefined) q.set('active', String(params.active))
    if (params.debt) q.set('debt', params.debt)
    if (params.page) q.set('page', String(params.page))
    if (params.pageSize) q.set('page_size', String(params.pageSize))
    const qs = q.toString()
    return request<SuppliersResponse>(`/v1/tenants/${tenantId}/hq/suppliers${qs ? `?${qs}` : ''}`)
  },

  supplier: (tenantId: string, supplierId: string) =>
    request<SupplierDetailResponse>(`/v1/tenants/${tenantId}/hq/suppliers/${supplierId}`),

  supplierPurchases: (
    tenantId: string,
    supplierId: string,
    params: { page?: number; pageSize?: number },
  ) => {
    const q = new URLSearchParams()
    if (params.page) q.set('page', String(params.page))
    if (params.pageSize) q.set('page_size', String(params.pageSize))
    const qs = q.toString()
    return request<SupplierPurchasesResponse>(
      `/v1/tenants/${tenantId}/hq/suppliers/${supplierId}/purchases${qs ? `?${qs}` : ''}`,
    )
  },

  supplierLedger: (
    tenantId: string,
    supplierId: string,
    params: { page?: number; pageSize?: number },
  ) => {
    const q = new URLSearchParams()
    if (params.page) q.set('page', String(params.page))
    if (params.pageSize) q.set('page_size', String(params.pageSize))
    const qs = q.toString()
    return request<SupplierLedgerResponse>(
      `/v1/tenants/${tenantId}/hq/suppliers/${supplierId}/ledger${qs ? `?${qs}` : ''}`,
    )
  },

  supplierInsights: (
    tenantId: string,
    params: { branchId?: string; from?: string; to?: string },
  ) => {
    const q = new URLSearchParams()
    if (params.branchId) q.set('branch_id', params.branchId)
    if (params.from) q.set('from', params.from)
    if (params.to) q.set('to', params.to)
    const qs = q.toString()
    return request<SupplierInsightsResponse>(
      `/v1/tenants/${tenantId}/hq/suppliers/insights${qs ? `?${qs}` : ''}`,
    )
  },

  createSupplier: (tenantId: string, input: NewSupplierInput) =>
    request<NewSupplierResult>(`/v1/tenants/${tenantId}/hq/suppliers`, post(input)),

  updateSupplier: (tenantId: string, supplierId: string, input: SupplierEditInput) =>
    request<UpdateSupplierResult>(`/v1/tenants/${tenantId}/hq/suppliers/${supplierId}`, {
      method: 'PUT',
      body: JSON.stringify(input),
    }),

  bulkUpdateSuppliers: (tenantId: string, input: BulkUpdateSuppliersInput) =>
    request<BulkUpdateSuppliersResult>(`/v1/tenants/${tenantId}/hq/suppliers/bulk`, {
      method: 'PUT',
      body: JSON.stringify(input),
    }),

  // Blob/multipart — bypass the JSON `request` helper (see authedFetch).
  exportSuppliers: async (
    tenantId: string,
    params: {
      search?: string
      branchId?: string
      groupId?: string
      active?: boolean
      debt?: SupplierDebtFilter
    },
  ): Promise<Blob> => {
    const q = new URLSearchParams()
    if (params.search) q.set('search', params.search)
    if (params.branchId) q.set('branch_id', params.branchId)
    if (params.groupId) q.set('group_id', params.groupId)
    if (params.active !== undefined) q.set('active', String(params.active))
    if (params.debt) q.set('debt', params.debt)
    const qs = q.toString()
    const res = await authedFetch(
      `/v1/tenants/${tenantId}/hq/suppliers/export${qs ? `?${qs}` : ''}`,
    )
    if (!res.ok) throw new ApiError(res.status, await parseError(res))
    return res.blob()
  },

  importSuppliers: async (
    tenantId: string,
    file: File,
    branchId: string,
  ): Promise<ImportSuppliersResult> => {
    const form = new FormData()
    form.set('file', file)
    form.set('branch_id', branchId)
    const res = await authedFetch(`/v1/tenants/${tenantId}/hq/suppliers/import`, {
      method: 'POST',
      body: form,
    })
    if (!res.ok) throw new ApiError(res.status, await parseError(res))
    return (await res.json()) as ImportSuppliersResult
  },

  // SSE stream URL. EventSource cannot set an Authorization header, so the
  // current access token rides the query string (the server keeps this route
  // out of access logs). Call again after a refresh — the token rotates.
  eventsUrl: (tenantId: string) =>
    `${BASE}/v1/tenants/${tenantId}/events?access_token=${encodeURIComponent(accessToken ?? '')}`,
}
