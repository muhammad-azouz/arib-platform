// Package model holds the domain types persisted in MongoDB.
package model

import "time"

// Provider identifies an authentication method linked to an account.
type Provider string

const (
	ProviderEmail    Provider = "email"
	ProviderGoogle   Provider = "google"
	ProviderFacebook Provider = "facebook"
)

// LicenseType distinguishes a free trial from a paid/assigned license.
type LicenseType string

const (
	LicenseTrial LicenseType = "trial"
	LicensePaid  LicenseType = "paid"
)

// LicenseStatus is the admin-controlled lifecycle of a license.
type LicenseStatus string

const (
	LicenseActive    LicenseStatus = "active"
	LicenseSuspended LicenseStatus = "suspended"
	LicenseExpired   LicenseStatus = "expired"
)

// DeviceStatus reflects whether a device currently holds the license seat.
type DeviceStatus string

const (
	DeviceActive   DeviceStatus = "active"
	DeviceReleased DeviceStatus = "released"
)

// Account is a client (business owner) who owns licenses.
type Account struct {
	ID          string            `bson:"_id"`
	Email       string            `bson:"email"`
	FirstName   string            `bson:"first_name"`
	LastName    string            `bson:"last_name"`
	Providers   []Provider        `bson:"providers"`
	ProviderIDs map[string]string `bson:"provider_ids,omitempty"` // provider -> external subject id
	Notes       string            `bson:"notes,omitempty"`        // admin notes (replaces ad-hoc notes)
	CreatedAt   time.Time         `bson:"created_at"`
	UpdatedAt   time.Time         `bson:"updated_at"`
}

// License is a single-device seat owned by an account.
type License struct {
	ID         string        `bson:"_id"`
	Key        string        `bson:"key"`
	AccountID  string        `bson:"account_id"`
	Type       LicenseType   `bson:"type"`
	Features   string        `bson:"features"`
	Modules    []string      `bson:"modules,omitempty"`
	Status     LicenseStatus `bson:"status"`
	ExpiresAt  *time.Time    `bson:"expires_at,omitempty"` // nil = perpetual
	// UpdatesUntil ends the update-entitlement window (maintenance model,
	// desktop/tasks/spec-app-updates.md): releases published before it stay
	// installable forever; later ones need a paid extension. nil = unlimited
	// (grandfathered pre-entitlement licenses). Independent of ExpiresAt.
	UpdatesUntil *time.Time `bson:"updates_until,omitempty"`
	AssignedBy   string     `bson:"assigned_by,omitempty"` // admin email, empty for trial
	Notes      string        `bson:"notes,omitempty"`
	// Source/ExternalRef are a forward seam for Phase-2 billing issuance
	// (provider webhooks); Phase 1 only writes signup_trial/manual_admin and
	// leaves ExternalRef empty.
	Source      string    `bson:"source,omitempty"`
	ExternalRef string    `bson:"external_ref,omitempty"`
	CreatedAt   time.Time `bson:"created_at"`
	UpdatedAt   time.Time `bson:"updated_at"`
}

// Device is the binding of a license to one physical machine.
type Device struct {
	ID              string       `bson:"_id"`
	LicenseID       string       `bson:"license_id"`
	AccountID       string       `bson:"account_id"`
	MachineID       string       `bson:"machine_id"`
	MachineName     string       `bson:"machine_name,omitempty"`
	OS              string       `bson:"os,omitempty"`
	Status          DeviceStatus `bson:"status"`
	BoundAt         time.Time    `bson:"bound_at"`
	LastSeenAt      time.Time    `bson:"last_seen_at"`
	LastValidatedAt time.Time    `bson:"last_validated_at"`
	ReleasedAt      *time.Time   `bson:"released_at,omitempty"`
	ReleaseCount    int          `bson:"release_count"`
	LastReleaseAt   *time.Time   `bson:"last_release_at,omitempty"`
}

// TrialLedger records that a machine has already consumed a free trial,
// independent of which account it was under (anti-abuse).
type TrialLedger struct {
	MachineID string    `bson:"_id"`
	AccountID string    `bson:"account_id"`
	UsedAt    time.Time `bson:"used_at"`
}

// OTP is a one-time email login code (hashed at rest).
type OTP struct {
	ID        string    `bson:"_id"`
	Email     string    `bson:"email"`
	CodeHash  string    `bson:"code_hash"`
	Attempts  int       `bson:"attempts"`
	ExpiresAt time.Time `bson:"expires_at"`
	CreatedAt time.Time `bson:"created_at"`
}

// Session is a refresh-token record for a logged-in device (token hashed).
type Session struct {
	ID         string    `bson:"_id"`
	AccountID  string    `bson:"account_id"`
	TokenHash  string    `bson:"token_hash"`
	MachineID  string    `bson:"machine_id,omitempty"`
	CreatedAt  time.Time `bson:"created_at"`
	ExpiresAt  time.Time `bson:"expires_at"`
	LastUsedAt time.Time `bson:"last_used_at"`
}

// OAuthExchange is a short-lived one-time code handed to the desktop loopback
// listener after a successful browser OAuth, swapped for a real session.
type OAuthExchange struct {
	Code      string    `bson:"_id"`
	AccountID string    `bson:"account_id"`
	ExpiresAt time.Time `bson:"expires_at"`
}

// AuditLog records sensitive admin/client actions.
type AuditLog struct {
	ID        string         `bson:"_id"`
	Actor     string         `bson:"actor"` // email or account id
	Action    string         `bson:"action"`
	Target    string         `bson:"target,omitempty"`
	Meta      map[string]any `bson:"meta,omitempty"`
	CreatedAt time.Time      `bson:"created_at"`
}

// ---------------------------------------------------------------------------
// Multi-tenant registry (control plane for the centralized sync architecture).
// Company/Branch IDs are SQL-Server uniqueidentifier GUIDs (lowercase string
// form): the cloud mints them, or adopts the tenant's existing local GUIDs
// when a standalone install subscribes. They flow into every tenant DB as FKs.
// ---------------------------------------------------------------------------

// TenantStatus is the admin/subscription-controlled lifecycle of a tenant.
type TenantStatus string

const (
	TenantActive    TenantStatus = "active"
	TenantSuspended TenantStatus = "suspended"
)

// BranchStatus reflects whether a branch is licensed for use.
type BranchStatus string

const (
	BranchActive      BranchStatus = "active"
	BranchDeactivated BranchStatus = "deactivated"
)

// RolloutStatus tracks where a tenant's central DB is in a fleet schema
// rollout (roadmap E3).
type RolloutStatus string

const (
	RolloutIdle      RolloutStatus = "idle"      // at the recorded schema_version, nothing pending
	RolloutMigrating RolloutStatus = "migrating" // a migrate call is in flight
	RolloutFailed    RolloutStatus = "failed"    // last migrate failed; retried on the next rollout
)

// ShardStatus is the operational state of a sync shard.
type ShardStatus string

const (
	ShardActive   ShardStatus = "active"
	ShardDraining ShardStatus = "draining"
)

// Shard is one gateway process + the SQL instance it fronts. The control plane
// only stores the gateway URL; the connection string stays gateway-side
// (SQL_CS_TEMPLATE env), so moving a shard's SQL to an elastic pool is pure ops.
type Shard struct {
	ID         string      `bson:"_id"`
	GatewayURL string      `bson:"gateway_url"`
	Status     ShardStatus `bson:"status"`
	CreatedAt  time.Time   `bson:"created_at"`
	UpdatedAt  time.Time   `bson:"updated_at"`
}

// Tenant is the multi-tenant subscription unit owned by an account. DBName is
// the tenant's central DB on the single sync server; it stays empty until the
// tenant subscribes to sync and a central DB is provisioned (sync is optional).
type Tenant struct {
	ID        string       `bson:"_id"` // tnt_...
	AccountID string       `bson:"account_id"`
	Name      string       `bson:"name"` // business display name
	Status    TenantStatus `bson:"status"`
	Plan      string       `bson:"plan,omitempty"`     // subscription plan code; empty = standalone
	DBName    string       `bson:"db_name,omitempty"`  // central DB; set on sync provisioning
	ShardID   string       `bson:"shard_id,omitempty"` // assigned shard; empty = unassigned (legacy)
	CreatedAt time.Time    `bson:"created_at"`
	UpdatedAt time.Time    `bson:"updated_at"`

	// --- Schema-version registry (roadmap E3) ---
	// SchemaVersion is the last version the gateway verified applied to this
	// tenant's central DB; 0 = never provisioned. The fleet rollout updates
	// these via the gateway's ops API.
	SchemaVersion   int           `bson:"schema_version,omitempty"`
	RolloutStatus   RolloutStatus `bson:"rollout_status,omitempty"`
	RolloutError    string        `bson:"rollout_error,omitempty"`    // last failure detail
	RolloutAttempts int           `bson:"rollout_attempts,omitempty"` // failed-migrate counter
	RolloutAt       time.Time     `bson:"rollout_at,omitempty"`       // last rollout touch
}

// Company is cloud-authoritative company info, pulled by the app at
// activation/login and cached locally; never DMS-synced.
type Company struct {
	ID        string    `bson:"_id"` // GUID (matches the tenant DB row)
	TenantID  string    `bson:"tenant_id"`
	Name      string    `bson:"name"`
	Phone     string    `bson:"phone,omitempty"`
	Address   string    `bson:"address,omitempty"`
	TaxNumber string    `bson:"tax_number,omitempty"`
	CreatedAt time.Time `bson:"created_at"`
	UpdatedAt time.Time `bson:"updated_at"`
}

// Branch is a licensed branch of a company. Seats is the per-branch device
// limit; adding/deactivating a branch is a licensing event.
type Branch struct {
	ID        string       `bson:"_id"` // GUID (matches the tenant DB row)
	TenantID  string       `bson:"tenant_id"`
	CompanyID string       `bson:"company_id"`
	Name      string       `bson:"name"`
	Phone1    string       `bson:"phone1,omitempty"`  // required on the POS branch; printed on receipts
	Phone2    string       `bson:"phone2,omitempty"`  // optional
	Phone3    string       `bson:"phone3,omitempty"`  // optional
	Address   string       `bson:"address,omitempty"` // required on the POS branch; printed on receipts
	Seats     int          `bson:"seats"`
	Status    BranchStatus `bson:"status"`
	CreatedAt time.Time    `bson:"created_at"`
	UpdatedAt time.Time    `bson:"updated_at"`

	// ActiveDevices is the live count of seats currently in use. It is computed
	// in GetBundle and never persisted (bson:"-"); it serializes as JSON
	// "ActiveDevices", matching the no-json-tags PascalCase convention the
	// console mirrors.
	ActiveDevices int `bson:"-"`
}

// BranchDevice binds one PC to a branch seat (the multi-tenant counterpart of
// Device, which binds a machine to a standalone license).
type BranchDevice struct {
	ID          string       `bson:"_id"` // bdv_...
	TenantID    string       `bson:"tenant_id"`
	BranchID    string       `bson:"branch_id"`
	MachineID   string       `bson:"machine_id"`
	MachineName string       `bson:"machine_name,omitempty"`
	OS          string       `bson:"os,omitempty"`
	Status      DeviceStatus `bson:"status"` // active | released
	BoundAt     time.Time    `bson:"bound_at"`
	LastSeenAt  time.Time    `bson:"last_seen_at"`
	ReleasedAt  *time.Time   `bson:"released_at,omitempty"`
}
