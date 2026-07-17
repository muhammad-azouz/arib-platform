# Implementation Plan: Tenant Sync Billing

Spec: `tasks/spec-billing.md` (approved 2026-07-17) · Tasks: `tasks/todo.md` §Phase 10 (T82–T90)

## Overview

Smallest billing system that removes the two manual publish blockers: admin
records paid bills (amount + period) per tenant; a paid bill auto-provisions
sync; subscription state is derived from bills at read time and gates sync-token
issuance (30-day warning window, 7-day grace, then refuse). Warnings surface in
the tenant console and, via the sync-token response, in the desktop POS.

## Architecture decisions (grounded in the code)

- **Derive, never store, subscription state.** One pure function
  `billing.Derive(bills, now)` — no cron, no status field to drift, boundaries
  unit-testable. Mirrors how the console already derives health tiers at read
  time rather than persisting them.
- **Enforcement lives only in `IssueSyncToken`** (`api/internal/tenant/service.go:404`),
  beside the existing `activeTenant` suspension check, as a sibling sentinel
  `ErrSubscriptionExpired` → 403 `subscription_expired`. Tokens live 1 h
  (`SYNC_TOKEN_TTL`), so stop/resume propagates within the hour with **zero
  gateway changes**. `Tenant.Status` stays a manual moderation switch.
- **Bills are append-only** (`paid` → `void`, never deleted), with
  `source`/`external_ref` copied from the `License` model's Phase-2 billing
  seam (`model.go:70-74`) so gateway webhooks slot in later.
- **Desktop seam is additive JSON on the sync-token response** —
  `SyncTokenResult` deserializes with `JsonPropertyName` and ignores unknown
  fields, so old desktops are safe and no new endpoint or polling loop is
  needed.
- **Auto-provision reuses `ProvisionSync` unchanged** (idempotent, shard-safe).
  Provision failure does not roll back the bill — the bill is a record of money
  received; the admin UI surfaces `provisioned: false` and the existing manual
  button remains the fallback.
- **`Tenant.Plan` stays dead** — console stops reading it; no plan catalog
  until the payment gateway (locked owner decision).

## Dependency graph

```
T82 Bill model + store
 ├── T83 billing.Derive (pure fn)
 │     ├── T84 billing service (create/void/list + auto-provision)
 │     │     └── T85 admin HTTP endpoints ──→ T87 admin UI
 │     └── T86 sync-token gate + client subscription endpoint
 │            ├── T88 console billing page ──→ T89 Overview + bell
 │            └── T90 desktop warning/paused state
```

After checkpoint 10a (API complete), T87 (admin UI), T88+T89 (console), and
T90 (desktop) are independent and parallelizable.

## Risks and mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| Enforcement deploys before real tenants have bills → their sync stops within ~1 h | High | Deploy order in checkpoint 10d: deploy API + admin UI, **backfill bills immediately** (well inside the 1 h token TTL); console/desktop follow at leisure |
| Old desktops meet the 403 `subscription_expired` before T90 ships | Med | They already handle non-200 as a failed round and retry — sync pauses with a generic error, nothing crashes; T90 only improves the message |
| Amount stored in minor units mis-entered as major in admin UI | Med | UI takes EGP major units and converts once at the API boundary; bill list renders back from minor units so a mistake is visible immediately |
| Voiding the covering bill silently kills a paying tenant's sync | Low | Void requires a reason, is audit-logged, and the admin UI shows the resulting state chip right after |
| Future-dated bill (paid for a period starting next month) | Low | Coverage = max `ends_at` over paid bills — paying early extends coverage; documented in `billing.Derive` tests |

## Open questions

1. Default currency assumed **EGP** — confirm before T84.
2. "How to pay" copy (bank/wallet details) for the console billing page —
   needed before T88 ships; page lands with a clearly-marked placeholder block
   otherwise.
