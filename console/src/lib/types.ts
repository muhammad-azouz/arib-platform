// These mirror the Go API responses.
//
// IMPORTANT (same gotcha as admin/): the domain models (Tenant/Company/Branch/
// BranchDevice) only carry `bson` tags, so encoding/json serializes them with
// their Go field names (PascalCase). The Bundle wrapper has no json tags either,
// so its keys are PascalCase too. Hand-written response maps (sync-token,
// session) DO use explicit lower/snake_case keys. Keep this in sync with
// internal/model/model.go + internal/tenant/service.go + httpapi handlers.

export type TenantStatus = 'active' | 'suspended'
export type BranchStatus = 'active' | 'deactivated'
export type DeviceStatus = 'active' | 'released'
export type Provider = 'email' | 'google' | 'facebook'

// --- domain models (PascalCase keys) ---

export interface Tenant {
  ID: string
  AccountID: string
  Name: string
  Status: TenantStatus
  Plan?: string
  ShardID?: string
  DBName?: string
  CreatedAt: string
  UpdatedAt: string
  SchemaVersion?: number
  RolloutStatus?: string
}

export interface Company {
  ID: string
  TenantID: string
  Name: string
  Phone?: string
  Address?: string
  TaxNumber?: string
  CreatedAt: string
  UpdatedAt: string
}

export interface Branch {
  ID: string
  TenantID: string
  CompanyID: string
  Name: string
  Phone1?: string // required on the POS branch; printed on receipts
  Phone2?: string
  Phone3?: string
  Address?: string // required on the POS branch; printed on receipts
  Seats: number // admin-controlled seat limit (merchant cannot change it)
  Status: BranchStatus
  CreatedAt: string
  UpdatedAt: string
  ActiveDevices?: number // live seat usage, computed server-side in GetBundle
}

export interface BranchDevice {
  ID: string
  TenantID: string
  BranchID: string
  MachineID: string
  MachineName?: string
  OS?: string
  Status: DeviceStatus
  BoundAt: string
  LastSeenAt: string
  ReleasedAt: string | null
}

// GET /v1/tenants/{id} — the activation/login bundle. `Company` is null until
// the company is registered; the Setup-Wizard completion gate keys off this.
export interface Bundle {
  Tenant: Tenant
  Company: Company | null
  Branches: Branch[] | null
}

// --- hand-written response maps (snake_case keys) ---

// POST /v1/tenants/{id}/sync-token
export interface SyncToken {
  token: string
  expires_at: string
  shard_id: string
  db_name: string
  gateway_url: string
}

// auth session (sessionResponse map in auth_handlers.go)
export interface Session {
  access_token: string
  refresh_token: string
  expires_in: number
  account_id: string
  email: string
}

// GET /v1/me returns a ClientView (account + licenses + devices); the console
// only needs the account identity here.
export interface Account {
  ID: string
  Email: string
  FirstName: string
  LastName: string
  Providers: Provider[] | null
  CreatedAt: string
  UpdatedAt: string
}

export interface MeView {
  account: Account
}
