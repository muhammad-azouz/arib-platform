# Spec: Tenant Sync Billing (small, manual-first)

Status: **draft — awaiting human review** · 2026-07-17
Owner decision log: existing console work (Phase 9 Live tier) is **deferred until after publish**; billing ships first.

## Objective

Replace the two manual gaps that block publishing — hand-provisioned sync DBs and
no record of who paid for what — with the smallest billing system that can grow
into a payment-gateway integration later.

The business flow (manual by design, gateway comes later):

1. A tenant pays the owner out of band (cash / bank transfer).
2. The owner opens the admin panel and records a **bill** on that tenant:
   amount paid + subscription period (`starts_at → ends_at`).
3. Recording a paid bill on an unprovisioned tenant **auto-provisions sync**
   (the existing `ProvisionSync`) — no more manual "Provision sync" clicking.
4. As `ends_at` approaches, the tenant sees warnings: starting **30 days
   before** the end in the console (persistent) and the desktop POS (weekly),
   until they pay or the period ends.
5. When the period ends, sync keeps working for a **7-day grace week**, with a
   stronger warning.
6. After the grace week, the platform **stops issuing sync tokens** for the
   tenant. The console stays fully usable (last-synced data + pay banner); the
   POS keeps selling locally — only sync stops. Recording a new bill restores
   sync within ~1 hour (sync-token TTL) with no other action.

Success = the owner never touches the provision button or a database again;
every paying tenant has a bill trail; a lapsed tenant degrades gracefully and
recovers by payment alone.

### Users

- **Owner/admin** (English admin panel): records and voids bills, sees each
  tenant's subscription state at a glance.
- **Tenant** (Arabic console): sees subscription state, bill history, how to
  pay, and timely warnings.
- **Cashier** (desktop POS): sees a non-blocking warning when the subscription
  is expiring/lapsed; is never locked out of local selling.

## Decisions (locked with owner, 2026-07-17)

| Decision | Choice |
|---|---|
| Existing provisioned tenants | **Backfill**: owner records a real bill for each current tenant before enforcement deploys. No grandfather code path. |
| "Stop sync" means | **Refuse sync-token issuance only** (`IssueSyncToken`). Never auto-suspends the tenant; `Status=suspended` stays a separate manual moderation switch. |
| Plans | **None yet.** A bill is amount + currency + period. The dead `Tenant.Plan` field stays unused by billing; console stops rendering it (fixes the misleading «بدون اشتراك» card). Plan catalog arrives with the payment gateway. |
| Warning surfaces | **Console + desktop POS.** Console: persistent banner/bell/billing page. Desktop: warning carried on the sync-token response, shown at most weekly. |

## Data Model (control plane, Mongo)

```go
// Bill is one recorded subscription payment covering a period. Created
// already-paid (the owner records money actually received). Never deleted —
// mistakes are voided, keeping the audit trail.
type Bill struct {
    ID          string     `bson:"_id"` // bil_...
    TenantID    string     `bson:"tenant_id"`
    Amount      int64      `bson:"amount"`   // minor units (piasters/cents)
    Currency    string     `bson:"currency"` // ISO code, default "EGP"
    StartsAt    time.Time  `bson:"starts_at"`
    EndsAt      time.Time  `bson:"ends_at"`
    Status      BillStatus `bson:"status"` // paid | void
    VoidReason  string     `bson:"void_reason,omitempty"`
    Notes       string     `bson:"notes,omitempty"`
    CreatedBy   string     `bson:"created_by"`        // admin email
    Source      string     `bson:"source"`            // "manual_admin" now; gateway ids later
    ExternalRef string     `bson:"external_ref,omitempty"` // gateway txn id, future
    CreatedAt   time.Time  `bson:"created_at"`
    UpdatedAt   time.Time  `bson:"updated_at"`
}
```

Index: `{tenant_id: 1, ends_at: -1}`.

**Subscription state is derived, never stored** — no cron, no background jobs,
no state to drift. One pure function in a new `api/internal/billing` package:

```go
// Coverage end = max EndsAt over the tenant's paid bills.
// now ≤ end−30d            → active
// end−30d < now ≤ end      → expiring   (warn)
// end < now ≤ end+7d       → grace      (warn hard, sync still works)
// now > end+7d (or none)   → expired    (sync tokens refused)
```

Constants (`warnBefore = 30d`, `graceAfter = 7d`) live in that package only.

## Tech Stack

Existing stack throughout — Go API (chi + mongo store), React 18 + TanStack
Query consoles (tenant console Arabic RTL, admin English), Avalonia desktop.
No new dependencies.

## API Surface

### Admin (existing admin auth + audit log)

- `POST /v1/admin/tenants/{id}/bills` — body `{amount, currency, starts_at, ends_at, notes}`.
  Validates `ends_at > starts_at`, `amount > 0`. On success, if the tenant has
  no `DBName`, calls `ProvisionSync` (failure to provision does not roll back
  the bill; response flags `provisioned: false` and the admin panel surfaces it —
  the existing manual provision button remains as the ops fallback).
  Audit action `bill.create`.
- `GET /v1/admin/tenants/{id}/bills` — list, newest first, plus derived
  `subscription` summary.
- `POST /v1/admin/bills/{id}/void` — body `{reason}`. Audit `bill.void`.
  Voiding the covering bill can flip a tenant to grace/expired — that is the
  point (correcting a mis-entered bill).

### Client (console, account auth, tenant-scoped)

- `GET /v1/tenants/{id}/subscription` — `{state, ends_at, grace_until, days_left, bills: [{amount, currency, starts_at, ends_at, status}]}`.
  Read-only; powers the billing page, Overview banner, and alert bell.

### Sync-token seam (desktop)

- `POST /v1/tenants/{id}/sync-token` response gains
  `"subscription": {"state": "...", "ends_at": "..."}` (additive — old desktops
  ignore it).
- When state is `expired` (or the tenant has no paid bills): **403** with
  machine code `subscription_expired`. Desktop shows a "sync paused — renew"
  status instead of a generic error; local work continues.

## UI

### Admin panel (`admin/src/pages/ClientDetail.tsx`)

Per tenant: subscription state chip (active / expiring / grace / expired /
none), bills table (amount, period, status, created-by), **Add bill** dialog —
suggested `starts_at` = current coverage end (or today if none), suggested
period = 1 year — and a void action with reason prompt.

### Tenant console (Arabic)

- **Billing page** (`/billing` placeholder becomes real): current state card
  (نشط حتى… / ينتهي خلال… / فترة سماح / منتهي), bill history table, and a
  static "how to pay" instructions block (owner's payment details — content
  supplied by owner, hardcoded for now).
- **Overview**: the `!t.Plan` banner and the «الباقة» card are replaced by
  subscription-state equivalents driven by the new endpoint (this retires the
  dead-field bug found 2026-07-16). Banner only when state ≠ active.
- **Alert bell** (`deriveAlerts`): one alert row while expiring/grace/expired,
  deep-linking to `/billing`. Persistent-while-true replaces "weekly" cadence
  in the console (a live banner needs no schedule).

### Desktop POS

Parse the new `subscription` field in `SyncTokenResult`; when expiring/grace
show a dismissible warning at most **once per 7 days** (stored last-shown
timestamp); on `subscription_expired` show the paused-sync state. Small change:
`Services/LicenseApiClient.cs`, `Services/Sync/SyncService.cs`, one UI banner.

## Commands

```
API:      cd api && make test          # go test ./...
Console:  cd console && pnpm build && pnpm lint
Admin:    cd admin && pnpm build && pnpm lint
Desktop:  cd ../desktop && dotnet build AribONE.csproj
```

## Project Structure (new/touched)

```
api/internal/billing/            → NEW: Bill model logic + derived-state fn + service
api/internal/model/model.go      → Bill struct, BillStatus
api/internal/store/mongo/bills.go→ NEW: bill CRUD + index
api/internal/httpapi/            → admin + client handlers, sync-token change
api/internal/tenant/service.go   → IssueSyncToken gate
admin/src/pages/ClientDetail.tsx → bills UI
admin/src/lib/api.ts, types.ts   → bill endpoints
console/src/pages/Billing.tsx    → NEW real page (replaces Placeholder route)
console/src/pages/console/Overview.tsx → subscription banner/card
console/src/lib/{api,hooks,alerts}.ts  → subscription query + alert
desktop (separate repo)          → SyncTokenResult + warning banner
```

## Code Style

Follow the file being edited. Go: table-driven tests beside the service,
errors as sentinel values (`ErrSubscriptionExpired` beside `ErrTenantSuspended`),
doc comments explain *why*. Console: TanStack Query hooks in `lib/hooks.ts`,
Arabic copy, tone helpers like `tenantStatusTone`.

## Testing Strategy

- **Go (primary):** table-driven unit tests for the derived-state function
  (boundaries: exactly −30d, exactly end, end+7d, no bills, voided covering
  bill, overlapping bills) and for the `IssueSyncToken` gate; handler tests for
  create/void/list + auto-provision paths. `make test`.
- **Consoles:** `pnpm build && pnpm lint` (repo has no JS test rig — unchanged).
- **Manual E2E before deploy:** backfill a bill on the real tenant → confirm
  state active; create an already-expired bill on a scratch tenant → confirm
  403 + desktop paused state → add bill → sync resumes.

## Boundaries

- **Always:** derive state — never store it; refuse tokens only at issuance;
  keep bills append-only (void, don't delete); audit-log every admin bill
  action; additive JSON only on the sync-token response.
- **Ask first:** touching `Tenant.Status` from billing code; any desktop-repo
  change beyond the described banner + result parsing; changing warn/grace
  constants; adding email notifications.
- **Never:** delete bill documents; block console/HQ reads for unpaid tenants;
  auto-suspend tenants; call the payment provider (there is none yet).

## Success Criteria

1. Creating a paid bill on an unprovisioned tenant provisions its DB with no
   further admin action; the desktop syncs on the next round.
2. `IssueSyncToken` refuses with `subscription_expired` iff `now > coverage
   end + 7d` (or no paid bills); a new bill restores issuance immediately.
3. Console shows the correct state in all five states; the misleading
   «لا يوجد اشتراك مزامنة»-while-provisioned contradiction is gone.
4. Desktop shows the expiring warning no more than weekly and a clear paused
   state when refused; selling locally is never blocked.
5. Voiding the covering bill downgrades state on next read; audit log records
   who and why.
6. All existing tests still pass; new derived-state tests cover the boundary
   dates.

## Future seams (explicitly out of scope now)

- Payment gateway: webhooks create bills with `source`/`external_ref` —
  model already carries both.
- Plan catalog + pricing: new `Plan` collection; bills gain `plan_id`;
  `Tenant.Plan` either gets wired then or removed.
- Email reminders (the "weekly" cadence as push, via existing `mail` pkg).
- Multi-tenant accounts invoice rollup.

## Open Questions

1. Default bill currency — assumed **EGP**; confirm.
2. "How to pay" block content for the console billing page (bank/wallet
   details) — needed before the console task ships.
