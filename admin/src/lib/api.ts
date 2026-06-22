import type {
  Account,
  AuditLog,
  ClientView,
  License,
  LicenseStatus,
  Session,
  Stats,
  Tenant,
  TenantDeletionResult,
} from './types'

const BASE = import.meta.env.VITE_API_BASE_URL ?? ''
const REFRESH_KEY = 'arib_admin_refresh'

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

const body = (v: unknown): RequestInit => ({
  method: 'POST',
  body: JSON.stringify(v),
})

// ---- Public auth (no bearer needed) ----

export const authApi = {
  async emailStart(email: string): Promise<void> {
    const res = await fetch(`${BASE}/v1/auth/email/start`, body(email ? { email } : {}))
    if (!res.ok) throw new ApiError(res.status, await parseError(res))
  },
  async emailVerify(
    email: string,
    code: string,
    firstName = '',
    lastName = '',
  ): Promise<Session> {
    const res = await fetch(
      `${BASE}/v1/auth/email/verify`,
      body({ email, code, first_name: firstName, last_name: lastName }),
    )
    if (!res.ok) throw new ApiError(res.status, await parseError(res))
    return (await res.json()) as Session
  },
  async logout(): Promise<void> {
    const refresh = session.getRefresh()
    if (!refresh) return
    await fetch(`${BASE}/v1/auth/logout`, body({ refresh_token: refresh })).catch(
      () => undefined,
    )
  },
}

// ---- Admin (bearer required) ----

export const adminApi = {
  stats: () => request<Stats>('/v1/admin/stats'),

  searchClients: (q: string) =>
    request<{ clients: Account[] | null }>(
      `/v1/admin/clients?q=${encodeURIComponent(q)}`,
    ).then((r) => r.clients ?? []),

  createClient: (input: {
    email: string
    first_name: string
    last_name: string
    notes: string
  }) => request<Account>('/v1/admin/clients', body(input)),

  getClient: (id: string) => request<ClientView>(`/v1/admin/clients/${id}`),

  updateClient: (
    id: string,
    input: { first_name: string; last_name: string; notes: string },
  ) =>
    request<Account>(`/v1/admin/clients/${id}`, {
      method: 'PATCH',
      body: JSON.stringify(input),
    }),

  assignLicenses: (input: {
    email: string
    modules: string[]
    expires_at: string | null // null = perpetual
    count: number
    notes: string
  }) =>
    request<{ licenses: License[] }>('/v1/admin/licenses', body(input)).then(
      (r) => r.licenses,
    ),

  setLicenseStatus: (id: string, status: LicenseStatus) =>
    request<{ status: string }>(
      `/v1/admin/licenses/${id}/status`,
      body({ status }),
    ),

  signOffline: (id: string, machineId: string) =>
    request<{ license: string }>(
      `/v1/admin/licenses/${id}/sign-offline`,
      body({ machine_id: machineId }),
    ).then((r) => r.license),

  forceRelease: (deviceId: string) =>
    request<{ status: string }>(`/v1/admin/devices/${deviceId}/release`, body({})),

  provisionSync: (tenantId: string) =>
    request<Tenant>(`/v1/admin/tenants/${tenantId}/provision-sync`, body({})),

  deleteTenant: (tenantId: string) =>
    request<TenantDeletionResult>(`/v1/admin/tenants/${tenantId}`, {
      method: 'DELETE',
    }),

  audit: () =>
    request<{ audit: AuditLog[] | null }>('/v1/admin/audit').then(
      (r) => r.audit ?? [],
    ),
}
