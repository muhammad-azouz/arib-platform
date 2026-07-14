# Todo: HQ Console — slices 0–2

Plan: `tasks/plan.md` · Spec: `tasks/spec-console.md`

## Phase 0 — foundation

- [x] **T1: Gateway records last-sync per branch**
  - **Description:** After each successful `/sync` round, upsert `(BranchId, LastSyncAt)` into a central-only `SyncActivity` table in the tenant DB, created lazily — mirror `ConflictLog.cs` exactly (EnsureTable + dialect SQL for both providers). Hook: the point in `Program.cs`'s `/sync` handler after `agent.HandleRequestAsync(http)` completes without error.
  - Acceptance:
    - [x] A completed sync round upserts the row; a failed round does not
    - [x] Table exists in both SQL Server and Postgres dialects; never part of the sync scope
  - Verify: `dotnet build AribSyncGateway.csproj`; run a real desktop sync against local gateway, inspect the table
  - Files: `sync-gateway/SyncActivity.cs` (new), `sync-gateway/Program.cs`, `sync-gateway/Db/*` (dialect SQL)
  - Dependencies: none · **Size: S**

- [x] **T2: API internal sync-completed endpoint**
  - **Description:** `POST /v1/internal/sync-completed` authed by the forwarded sync token (same pattern as `handleInternalTenantRegistry`, `tenant_handlers.go:57`). Persists `last_sync_at` on the Branch doc (new bson field). Also emits to the (later) SSE bus — leave a seam, don't build the bus yet.
  - Acceptance:
    - [x] Valid forwarded sync token updates the branch's `last_sync_at`; invalid token → 401
    - [x] Branch/tenant mismatch in claims → 403
  - Verify: `make test` — table-driven test beside the service
  - Files: `api/internal/httpapi/tenant_handlers.go`, `api/internal/tenant/service.go` + `service_test.go`, `api/internal/model/model.go`, `api/internal/store/mongo/branches or tenants` (wherever Branch persistence lives)
  - Dependencies: none · **Size: S**

- [x] **T3: Gateway fires the callback**
  - **Description:** After T1's upsert, fire-and-forget `POST {LICENSE_API_URL}/v1/internal/sync-completed` forwarding the branch's sync token (same client pattern as `RegistrySeeder`). Failure logs and never blocks the sync response.
  - Acceptance:
    - [x] Successful round triggers exactly one callback; callback failure doesn't fail `/sync`
  - Verify: local api + gateway + desktop sync; watch Branch doc update
  - Files: `sync-gateway/Program.cs`, `sync-gateway/SyncActivity.cs`
  - Dependencies: T1, T2 · **Size: XS**

- [x] **T4: API mints HQ tokens**
  - **Description:** Mint RS256 tokens with claims `scope:"hq"`, `db_name`, short TTL (~5 min), signed with the existing sync-token private key. Server-side helper only — used by the API when calling the gateway; never exposed on any route or sent to the browser.
  - Acceptance:
    - [x] Token validates against the sync public key; carries scope+db_name+exp
    - [x] No route returns it
  - Verify: `make test`
  - Files: `api/internal/tenant/service.go` (beside the existing sync-token mint) + `service_test.go`
  - Dependencies: none · **Size: XS**

- [x] **T5: Gateway HqToken + first read endpoint**
  - **Description:** `HqToken.cs` mirroring `OpsToken.cs` (require `scope:"hq"` **and** `db_name`; reject ops/sync tokens). `GET /hq/branch-activity`: db_name from the token — never from query — returns `SyncActivity` rows as `[{branch_id, last_sync_at}]`.
  - Acceptance:
    - [x] Valid HQ token → rows; sync token / ops token / expired → 401
    - [x] db_name comes only from token claims
  - Verify: `dotnet build`; curl with a token minted by the local API
  - Files: `sync-gateway/HqToken.cs` (new), `sync-gateway/HqApi.cs` (new), `sync-gateway/Program.cs`
  - Dependencies: T1, T4 · **Size: S**

- [x] **T6: API HQ passthrough with freshness envelope**
  - **Description:** `GET /v1/tenants/{id}/hq/branch-activity` (session-authed, tenant-ownership-checked like `handleTenantBundle`): resolve tenant → shard → `GatewayURL`, mint HQ token, call T5, wrap each branch as `{data, source: "synced"|"offline", as_of}` (offline = last_sync older than threshold). Establishes the whole read chain every later slice copies.
  - Acceptance:
    - [x] Tenant without sync provisioning → 402-style error (existing "no sync subscription" path)
    - [x] Response uses the envelope; gateway unreachable → 503 with clean message, not a hang (respect the 30 s timeout)
  - Verify: `make test` (service-level with fake gateway); manual curl through the running stack
  - Files: `api/internal/hq/service.go` + `service_test.go` (new domain, rollout-service style), `api/internal/httpapi/hq_handlers.go` (new), `api/internal/httpapi/server.go`
  - Dependencies: T4, T5 · **Size: M**

- [x] **T7: Console freshness primitive**
  - **Description:** Envelope types in `lib/types.ts`, `api.branchActivity(tenantId)` in `lib/api.ts`, `useBranchActivity` hook, and a `<Freshness>` pill component (Live / "تمت المزامنة قبل …" via `format.ts` / Offline+timestamp). Arabic copy consistent with existing pages.
  - Acceptance:
    - [x] One component renders all three states; relative time in Arabic
    - [x] Hook follows existing `qk`/hooks conventions
  - Verify: `pnpm build && pnpm lint`; render on a scratch page in dev
  - Files: `console/src/lib/types.ts`, `console/src/lib/api.ts`, `console/src/lib/query.ts`, `console/src/lib/hooks.ts`, `console/src/components/Freshness.tsx` (new)
  - Dependencies: T6 (contract; may start on mocks) · **Size: S**

- [x] **T8: Console nav shell — final IA**
  - **Description:** Extend `AppShell`'s nav to the spec IA: Overview, Branches, Catalog, Inventory, Reports, Settings (Arabic labels), using the existing `Placeholder` page for not-yet slices. Routes in `App.tsx`.
  - Acceptance:
    - [x] All sections navigable; existing pages unaffected; RTL intact on desktop + mobile nav
  - Verify: `pnpm build && pnpm lint`; click through in dev
  - Files: `console/src/components/AppShell.tsx`, `console/src/App.tsx`, `console/src/components/icon.tsx`
  - Dependencies: none · **Size: S**

### Checkpoint 0
- [x] All three repo gates green (api `go test ./...` with Mongo, gateway `dotnet build`, console `pnpm build && pnpm lint` — 2026-07-14)
- [x] E2E: desktop sync → SyncActivity → callback → Mongo → console shows real per-branch freshness *(human-verified with a real desktop sync round, 2026-07-14)*
- [x] **Human review before Phase 1** *(approved 2026-07-14)*

## Phase 1 — branches dashboard

- [x] **T9: Gateway branch-snapshot endpoint**
  - **Description:** `GET /hq/branch-snapshot`: per branch — today's sales total (Bills, sale subtypes, today's date range) and current shift (open `Shifts` row: name, opened-at). **Start by reading `Bill.cs`/`Shift.cs` entities** to get discriminators/columns right; query via dialect SQL or `AribContext`.
  - Acceptance:
    - [x] Numbers match the desktop app's own today-sales for a test branch *(verified on a seeded clone of a real tenant schema: totals, deleted/yesterday exclusions, open-vs-closed shift)*
    - [x] Branch with no data today → zeros, not errors; closed shift → null
  - Verify: `dotnet build`; compare against a real synced tenant DB
  - Files: `sync-gateway/HqApi.cs`, `sync-gateway/Db/*` (if dialect SQL needed)
  - Dependencies: T5 · **Size: M**

- [x] **T10: API snapshot passthrough + health derivation**
  - **Description:** `GET /v1/tenants/{id}/hq/branches` combining control-plane branches (Mongo) + T9 snapshot + health tier from `last_sync_at` (🟢 <10 min, 🟡 10–30, 🔴 older / never). One response the Branches page can render alone.
  - Acceptance:
    - [x] Health thresholds unit-tested; gateway-down still returns control-plane data with `source:"offline"`
  - Verify: `make test`
  - Files: `api/internal/hq/service.go` + `service_test.go`, `api/internal/httpapi/hq_handlers.go`
  - Dependencies: T6, T9 · **Size: S**

- [x] **T11: Branches page — branch-as-server cards**
  - **Description:** Rework `pages/console/Branches.tsx`: card per branch — status dot, name, `<Freshness>`, today's sales, current shift — preserving existing management actions (add/rename/bind/seats) where they live today. Skeletons via existing `States.tsx` patterns; stale-while-revalidate.
  - Acceptance:
    - [x] Card shows all five data points; existing branch management flows still work *(human-verified at checkpoint 1)*
    - [x] No spinner-blanking: cached data + background refresh
  - Verify: `pnpm build && pnpm lint`; manual click-through of old flows
  - Files: `console/src/pages/console/Branches.tsx`, `console/src/lib/hooks.ts`, `console/src/lib/api.ts`, `console/src/lib/types.ts`
  - Dependencies: T7, T10 · **Size: M**

- [x] **T12: Branch detail page (progressive disclosure)**
  - **Description:** Route `branches/:branchId`: header (status, freshness, shift), then disclosure sections — devices/seats (existing bundle data), sync activity, diagnostics stub. Breadcrumbs via existing component.
  - Acceptance:
    - [x] Card click navigates; sections collapse/expand; seats usage matches bundle *(no device-list endpoint exists — section shows seat counts)*
  - Verify: `pnpm build && pnpm lint`; manual
  - Files: `console/src/pages/console/BranchDetail.tsx` (new), `console/src/App.tsx`, `console/src/lib/hooks.ts`
  - Dependencies: T11 · **Size: M**

- [x] **T13: API SSE endpoint**
  - **Description:** `GET /v1/tenants/{id}/events` — SSE, session-authed. In-memory per-tenant pub/sub; T2's handler publishes `branch-synced` events; heartbeat comment every ~25 s. Register **outside** the `apiTimeout` group (like `/updates/*`). Nginx: add the location with `proxy_buffering off` (pre-approved in spec boundaries).
  - Acceptance:
    - [x] `curl -N` streams events when a sync lands; connection survives >30 s idle via heartbeats *(human-verified at checkpoint 1)*
    - [x] Auth required; tenant-scoped events only (bus isolation race-tested; ?access_token= supported for EventSource, nginx access_log off on the route)
  - Verify: `make test` (bus unit test) + manual curl during a desktop sync
  - Files: `api/internal/hq/events.go` (new) + test, `api/internal/httpapi/hq_handlers.go`, `api/internal/httpapi/server.go`, `console/nginx.conf`
  - Dependencies: T2 · **Size: M**

- [x] **T14: Console live updates**
  - **Description:** `useTenantEvents(tenantId)` hook: `EventSource` with manual reconnect (the URL-borne access token rotates, so built-in retry would reuse a stale token), on `branch-synced` invalidate the branch-activity/branches query keys. Mounted in `AppShell` so every console page benefits.
  - Acceptance:
    - [x] Desktop "Sync Now" flips the branch card's freshness without refresh *(human-verified at checkpoint 1)*
    - [x] Tab left open >10 min stays subscribed (refresh-then-reconnect on error, 5s backoff)
  - Verify: `pnpm build && pnpm lint`; manual e2e
  - Files: `console/src/lib/hooks.ts`, `console/src/components/AppShell.tsx`
  - Dependencies: T11, T13 · **Size: S**

### Checkpoint 1
- [x] All gates green
- [x] Manual e2e: desktop "Sync Now" → card freshness + health dot flip live, no refresh *(human-verified 2026-07-14)*
- [x] Stale branch (>30 min) renders 🔴 with last-data timestamp *(human-verified 2026-07-14)*
- [x] **Human review before Phase 2 (Overview)** *(approved 2026-07-14)*

## Phase 2 — Overview

No new gateway endpoint (plan outline superseded): company KPIs are summed API-side from the branch snapshots `/hq/branches` already fetches in one gateway call.

- [x] **T15: API — `totals` block on `/hq/branches`**
  - **Description:** Extend `hq.Service.Branches` to also return company-wide totals summed over the branch views' snapshot data: `{sales_total, sales_count, refunds_total, open_shift_count, synced_branches, offline_branches, as_of}`. Sums include every branch whose snapshot `Data` is set (stale data stays visible per T10's philosophy — honesty comes from `offline_branches` + `as_of` = oldest contributing `last_sync_at`). Handler wraps as `{branches, totals}`.
  - Acceptance:
    - [x] Mixed healthy/stale/never branches: sums correct, `offline_branches` counts stale+never, `as_of` is the oldest contributing sync
    - [x] Gateway down / not subscribed: totals present with zeros and all branches counted offline (page still renders)
  - Verify: `make test` — table-driven beside the service
  - Files: `api/internal/hq/service.go` + `service_test.go`, `api/internal/httpapi/hq_handlers.go`
  - Dependencies: T10 · **Size: S**

- [x] **T16: Console — Overview KPI tiles**
  - **Description:** Rework `pages/console/Overview.tsx`: KPI row from `totals` (مبيعات اليوم، عدد الفواتير، المرتجعات، الورديات المفتوحة) with `<Freshness>` and an offline-branches caveat («لا يشمل X فروع غير متزامنة»). Reuses `useHqBranches` (shared cache + SSE invalidation already wired). Existing banners (suspended / no-plan / onboarding) preserved; company/plan cards demoted below the KPIs.
  - Acceptance:
    - [x] KPI numbers match the sum of the Branches page cards; Arabic numerals via `format.ts` *(same API sums; e2e match at checkpoint 2)*
    - [x] No spinner-blanking; tenant without sync renders control-plane view with offline states, not errors
  - Verify: `pnpm build && pnpm lint`; manual in dev
  - Files: `console/src/lib/types.ts`, `console/src/pages/console/Overview.tsx`
  - Dependencies: T15 · **Size: M**

- [x] **T17: Console — branch health strip**
  - **Description:** Compact strip on Overview: one dot+name chip per branch (health color from `BranchView.health`), click → `branches/:branchId`. Same query, no new fetch.
  - Acceptance:
    - [x] Every branch renders a chip with the correct tier color; click navigates to its detail page *(HealthDot extracted to a shared component so Branches cards and the strip can't drift)*
  - Verify: `pnpm build && pnpm lint`; manual
  - Files: `console/src/pages/console/Overview.tsx` (+ small component if it earns extraction)
  - Dependencies: T16 · **Size: S**

- [x] **T18: Console — alerts stub + quick actions**
  - **Description:** Alerts panel derived from data already on hand: stale/never-sync branches → «لم يزامن منذ …» deep-linking to that branch's detail (spec rule: an alert with no destination doesn't ship). Empty state «لا توجد تنبيهات». Shaped so slice 5's derived alerts (low stock, conflicts) slot into the same list. Quick actions row: إضافة فرع (→ الفروع), تنزيل التطبيق (→ التنزيل).
  - Acceptance:
    - [x] Stale branch produces an alert whose link opens the branch detail; healthy tenant shows the empty state *(stale → branch detail; never-connected → download page; live render at checkpoint 2)*
    - [x] Quick actions navigate correctly
  - Verify: `pnpm build && pnpm lint`; manual
  - Files: `console/src/pages/console/Overview.tsx`
  - Dependencies: T16 · **Size: S**

### Checkpoint 2
- [x] All gates green
- [x] Manual e2e: Overview KPI totals match the Branches cards; desktop "Sync Now" flips Overview numbers/freshness live, no refresh *(human-verified 2026-07-14, including two branches + shift mode; found/fixed sync-gateway `12bc3ae`: OpenedAt serialized without TZ suffix zeroed all totals)*
- [x] Stale branch (>30 min) appears as an alert; its link opens the branch detail *(human-verified 2026-07-14)*
- [x] **Human review before Phase 3 (Catalog)** *(approved 2026-07-14)*
