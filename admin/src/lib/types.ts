// These mirror the Go API responses.
//
// IMPORTANT: the domain models (Account/License/Device/AuditLog) only carry
// `bson` tags, so encoding/json serializes them with their Go field names
// (PascalCase). Hand-written response wrappers (ClientView, Stats, Tokens)
// DO carry json tags, so those keys are lower/snake_case. Keep this in sync.

export type Provider = 'email' | 'google' | 'facebook'
export type LicenseType = 'trial' | 'paid'
export type LicenseStatus = 'active' | 'suspended' | 'expired'
export type DeviceStatus = 'active' | 'released'
export type TenantStatus = 'active' | 'suspended'

export interface Account {
  ID: string
  Email: string
  FirstName: string
  LastName: string
  Providers: Provider[] | null
  ProviderIDs: Record<string, string> | null
  Notes?: string
  CreatedAt: string
  UpdatedAt: string
}

export interface License {
  ID: string
  Key: string
  AccountID: string
  Type: LicenseType
  Features: string
  Status: LicenseStatus
  ExpiresAt: string
  AssignedBy?: string
  Notes?: string
  CreatedAt: string
  UpdatedAt: string
}

export interface Device {
  ID: string
  LicenseID: string
  AccountID: string
  MachineID: string
  MachineName?: string
  OS?: string
  Status: DeviceStatus
  BoundAt: string
  LastSeenAt: string
  LastValidatedAt: string
  ReleasedAt: string | null
  ReleaseCount: number
  LastReleaseAt: string | null
}

export interface AuditLog {
  ID: string
  Actor: string
  Action: string
  Target?: string
  Meta?: Record<string, unknown> | null
  CreatedAt: string
}

export interface Tenant {
  ID: string
  AccountID: string
  Name: string
  Status: TenantStatus
  DBName?: string
  CreatedAt: string
  UpdatedAt: string
}

// ClientView carries json tags -> lowercase keys.
export interface ClientView {
  account: Account
  licenses: License[]
  devices: Device[]
  tenants: Tenant[]
}

// Stats carries json tags -> snake_case keys.
export interface Stats {
  clients: number
  licenses_active: number
  licenses_suspended: number
  licenses_trial: number
  licenses_paid: number
  devices_active: number
  licenses_expiring_30d: number
}

export interface Session {
  access_token: string
  refresh_token: string
  expires_in: number
  account_id: string
  email: string
}
