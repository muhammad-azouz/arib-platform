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
	Status     LicenseStatus `bson:"status"`
	ExpiresAt  time.Time     `bson:"expires_at"`
	AssignedBy string        `bson:"assigned_by,omitempty"` // admin email, empty for trial
	Notes      string        `bson:"notes,omitempty"`
	CreatedAt  time.Time     `bson:"created_at"`
	UpdatedAt  time.Time     `bson:"updated_at"`
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
