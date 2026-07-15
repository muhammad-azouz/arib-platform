import type {
  AckConflictsInput,
  AckConflictsResult,
  AttentionResponse,
  Branch,
  BranchActivityResponse,
  BranchDevice,
  Bundle,
  CatalogGroupsResponse,
  CatalogProductsResponse,
  Company,
  ConflictsResponse,
  HqBranchesResponse,
  InventoryBranchesResponse,
  InventoryProductsResponse,
  InventoryStatusFilter,
  MeView,
  MovementsResponse,
  NewProductInput,
  NewProductResult,
  PriceChangeInput,
  PriceChangeResult,
  ProductDetailResponse,
  Session,
  SyncToken,
  Tenant,
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

/** Attempt to mint a new access token from the stored refresh token. */
async function tryRefresh(): Promise<boolean> {
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

  // SSE stream URL. EventSource cannot set an Authorization header, so the
  // current access token rides the query string (the server keeps this route
  // out of access logs). Call again after a refresh — the token rotates.
  eventsUrl: (tenantId: string) =>
    `${BASE}/v1/tenants/${tenantId}/events?access_token=${encodeURIComponent(accessToken ?? '')}`,
}
