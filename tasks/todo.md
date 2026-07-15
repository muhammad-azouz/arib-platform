# Todo: HQ Console — slices 0–3

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

## Phase 3 — Catalog

Open question 1 resolved (user, 2026-07-14): **ServerWins + ConflictLog alerts for v1** — no branch-edit gating, no schema bump. Design notes in `plan.md` §Phase 3: DMS triggers make central writes propagate for free; propagation state = `written_at` vs live `last_sync_at` (no new storage); prices live on `UnitOfMeasure`; HQ create must seed zero-qty `WarehousesProductInventories` rows or the product is invisible at branches.

- [x] **T19: Gateway catalog read endpoints** *(sync-gateway `1b35040`; `dotnet build` clean — curl against a real synced tenant pending, folds into checkpoint 3 e2e)*
  - **Description:** Three reads in `HqApi.cs` (EF via `AribContext`, same style as `BranchSnapshotAsync`): `GET /hq/groups` — full `ProductGroup` list (id, parent_id, name, is_active, num, product_count); `GET /hq/products?search=&group_id=&page=&page_size=` — paged master list (id, code, name, kind, group, is_active, master-unit name/sale/buy, barcodes, company-wide qty = SUM of WPI TotalQty), search on name/code/barcode, ordered by ProductCode; `GET /hq/products/{id}` — full detail: all UoMs (name, val_sub, level, buy, sale, price1–9, barcodes) + availability rows from `WarehousesProductInventories` (branch_id, warehouse_id, warehouse_name, total_qty, unit_cost, updated_at). **Match the desktop's master-unit semantics** (`WarehousesAndProductsViewModel` uses `UnitOfMeasure.First()` — read it first).
  - Acceptance:
    - [ ] List numbers (price, qty) match the desktop products screen for a synced tenant
    - [ ] Search matches Arabic names and barcodes; empty DB / never-synced tenant → empty page, not error
    - [ ] db_name only from the validated HQ token
  - Verify: `dotnet build AribSyncGateway.csproj`; curl against a real synced tenant DB
  - Files: `sync-gateway/HqApi.cs`, `sync-gateway/Program.cs`
  - Dependencies: T5 · **Size: M**

- [x] **T20: API catalog passthrough**
  - **Description:** `GET /v1/tenants/{id}/hq/catalog/groups|products|products/{pid}` in the hq domain (same chain as T6/T10: session auth → ownership → shard → HQ token → gateway). Catalog payloads wrap in the envelope with `source:"synced"`, `as_of` = read time (central is read directly; the pill honestly renders "just synced"). Detail availability rows are decorated with each branch's `health`/`last_sync_at` from the registry the service already loads.
  - Acceptance:
    - [x] Query params passed through (search/group/page); gateway down → 503 clean; no sync subscription → existing 402-style path
    - [x] Availability rows carry branch name + health tier so the console needs no second call
  - Verify: `make test` — table-driven beside the service *(`go build ./... && go test ./... ` clean, 2026-07-14)*
  - Files: `api/internal/hq/service.go` + `service_test.go`, `api/internal/httpapi/hq_handlers.go`, `api/internal/httpapi/server.go`
  - Dependencies: T19 (contract; may start on fakes) · **Size: S**

- [x] **T21: Console Catalog page — groups + products table**
  - **Description:** Replace the `Catalog.tsx` placeholder: groups tree (RTL sidebar or chips row; parent/child from `parent_id`) filtering a products table — code, name, group, master-unit sale price, company qty, active state — with debounced search and server-side pagination. Types/api/hooks per existing `qk` conventions; skeletons via `States.tsx`; stale-while-revalidate (no spinner-blanking).
  - Acceptance:
    - [~] Search + group filter + pagination round-trip to the API; Arabic numerals via `format.ts` *(wired and type-checked; round-trip against real catalog rows needs a synced tenant — folds into checkpoint 3 e2e, same as T19)*
    - [x] Tenant without sync renders a friendly empty state, not an error *(verified live: real API + Mongo, freshly created unsubscribed tenant → 402 → `EmptyState` renders, not a crash)*
  - Verify: `pnpm build && pnpm lint`; manual in dev *(build/lint clean; no browser-automation tool available in this session to click through visually — verified the 402 empty-state path via curl against a live local API instead)*
  - Files: `console/src/pages/console/Catalog.tsx`, `console/src/lib/{types,api,query,hooks}.ts`, `console/src/components/icon.tsx`
  - Dependencies: T20 · **Size: M**

- [x] **T22: Console product detail**
  - **Description:** Route `catalog/:productId` (breadcrumbs like `BranchDetail`): header (name, code, group, active), units table (name, factor, buy/sale, price tiers, barcodes), and per-branch availability section — branch name, HealthDot, qty, unit cost, `<Freshness>` from the branch's `last_sync_at`. Row click → that branch's detail page.
  - Acceptance:
    - [~] All UoMs and barcodes render; availability shows every branch that has WPI rows with correct health colors *(wired and type-checked against the exact `ProductDetail`/`ProductUnit`/`ProductAvailability` shapes T20 already ships and unit-tests; rendering against real UoM/WPI rows needs a synced tenant — folds into checkpoint 3 e2e, same as T19/T21)*
  - Verify: `pnpm build && pnpm lint` clean, 2026-07-14; no browser-automation tool available this session, so no visual click-through (same gap as T21) — the 402/404 branches call the same `resolveGateway`/`getJSON` code paths already live-verified (T21) and unit-tested (T20's `TestCatalogProductDetail_NotFound`)
  - Files: `console/src/pages/console/ProductDetail.tsx` (new), `console/src/App.tsx`, `console/src/lib/{types,api,query,hooks}.ts`, `console/src/components/icon.tsx`, `console/src/pages/console/Catalog.tsx` (row click → detail)
  - Dependencies: T21 · **Size: M**

- [x] **T23: Gateway price-change write (first HQ write)**
  - **Description:** `PUT /hq/products/{id}/prices` — body `{changes:[{unit_id, sale?, buy?, price1..9?}]}`; every `unit_id` must belong to the product and the token's db (404/400 otherwise); EF update inside one transaction; returns `{written_at}` (UTC now). **This task retires the propagation risk**: verify DMS's central-side tracking triggers capture the EF update and a real desktop pulls the new price on its next round.
  - Acceptance:
    - [ ] Price change lands in central; desktop shows the new price after its next sync round (e2e, real tenant) — **needs a human pass**: this session has no AribONE desktop install to actually trigger a "Sync Now" against, so — same as Checkpoints 0–2's e2e lines — this is the one item only you can verify
    - [x] Sync/ops tokens rejected; unit from another product → 400; db_name only from token *(rejection is structural, not new logic: the endpoint reuses `TryHqAuth`/`HqToken.TryValidate`, which already requires `scope:"hq"` — a sync or ops token fails there before reaching this code, exactly like every other `/hq/*` endpoint since T5. `ApplyPriceChangesAsync` explicitly checks every `unit_id` belongs to `productId` and returns `InvalidUnits` → 400 otherwise; `dbName` is only ever the `TryHqAuth` out-param, never read from the request)*
  - Verify: `dotnet build` clean, 2026-07-14; e2e with a real desktop sync **pending — see acceptance note above**
  - Files: `sync-gateway/HqApi.cs`, `sync-gateway/Program.cs`
  - Dependencies: T19 · **Size: M**

- [x] **T24: API price-change passthrough**
  - **Description:** `PUT /v1/tenants/{id}/hq/catalog/products/{pid}/prices` — same auth chain; body validated (non-negative prices, ≤ N changes); forwards to T23; response `{written_at}` passed through. Log the write (tenant, product, user) via the existing request-log pattern — HQ writes should be traceable.
  - Acceptance:
    - [x] Ownership enforced; negative price → 400 before the gateway is called; gateway error surfaces cleanly *(unit-tested: `TestChangeProductPrices_ForwardsChangesAndReturnsWrittenAt` asserts `ErrForbidden` for a non-owning account and the exact `{changes:[...]}` body/`written_at` round-trip; `TestChangeProductPrices_InvalidUnits`/`_ProductNotFound` assert the gateway's 400/404 map to `ErrInvalidUnits`/`ErrNotFound`. Also live-verified against the real running API + Mongo: negative price, empty `changes`, and blank `unit_id` all return a clean 400 with zero HTTP calls reaching the gateway — confirmed by the request log showing `dur_ms:0` and no gateway process even running; a valid-shaped request against a real but unsubscribed tenant correctly reaches `resolveGateway` and returns 402)*
  - Verify: `make test` — `go build ./... && go test ./...` clean, 2026-07-14; live curl check against a real local API+Mongo (see note above)
  - Files: `api/internal/hq/service.go` + `service_test.go`, `api/internal/httpapi/hq_handlers.go`, `api/internal/httpapi/server.go`
  - Dependencies: T23 (contract) · **Size: S**

- [x] **T25: Console price editing + propagation chips**
  - **Description:** Edit affordance on the product detail units table (dialog, react-hook-form + zod). On success: propagation panel — one chip per branch, «في الانتظار — يصل خلال ~٥ دقائق» until that branch's `last_sync_at ≥ written_at`, then «وصل ✓». Branch data already streams via SSE (T14), so chips flip live; keep recent writes in component/query state (session-scoped is fine for v1 — honesty over persistence).
  - Acceptance:
    - [~] Desktop "Sync Now" flips that branch's chip to «وصل» without refresh; prices refetch after write *(the live-flip wiring is real, not theoretical: the panel reads `useHqBranches`, which is invalidated by `useTenantEvents`' SSE `branch-synced` listener already mounted app-wide in `AppShell` — same mechanism T14 proved live for the Branches/Overview pages. `pnpm build && pnpm lint` clean. What's unverified is the actual visual click-through — no browser-automation tool this session, and no real synced tenant/desktop to trigger a genuine "Sync Now" — folds into checkpoint 3, same as T19/T21/T22/T23)*
    - [x] Offline branch keeps the pending chip with its stale timestamp visible *(`PropagationPanel` never hides a stale/never-synced branch — it always renders the pending chip with either "(آخر مزامنة …)" or "(لم تتم المزامنة بعد)")*
  - Verify: `pnpm build && pnpm lint` clean, 2026-07-15; manual e2e **pending — see acceptance note above**
  - Files: `console/src/pages/console/ProductDetail.tsx`, `console/src/components/EditUnitPriceDialog.tsx` (new), `console/src/lib/{api,hooks,types}.ts`
  - Dependencies: T22, T24 · **Size: M**

- [x] **T26: HQ product create — all three repos**
  - **Description:** Gateway `POST /hq/products`: Tier-A rows (Product — `ProductCode` = central max+1, `ImageId` Guid.Empty, `CreatedAt` now, accounts from `ProductDefaults` per kind, mirroring `ProductService.AddNewProductAsync` defaults — + UoMs with ValSub/levels + barcodes) **plus one zero-qty WPI row per existing branch warehouse** (BranchId from the warehouse row, `UpdatedAt` default) so the product is visible at every branch; no opening balance from HQ. API `POST /v1/tenants/{id}/hq/catalog/products` with zod-matching validation; console «منتج جديد» form (name, group, kind, unit(s), prices, barcode) + the T25 propagation panel on success.
  - **Scope decisions (mirrors T25's minimalism):** accounts are actually wired via `AccountOperand` + the desktop's `ProductTypeProfile` per-kind mapping, not the mostly-unused `ProductDefault` table — confirmed by reading `ProductService.AddNewProductAsync`/`ProductTypeProfile.cs` in the desktop repo directly rather than assuming from the plan's wording. v1's create form supports exactly **one unit** (factor fixed at 1 — no sub-unit hierarchy), Sale/Buy only (no price tiers), one optional barcode; `kind`/`group` are exposed as the plan's field list requires. WPI rows are seeded for every kind (Product/SalesService/PurchaseService) per the plan's literal, unconditional wording.
  - Acceptance:
    - [ ] Created product appears in the desktop products screen after the branch's next sync and is sellable (e2e, real tenant) — **needs a human pass**: same as T23, no desktop client or synced tenant in this session
    - [x] Duplicate barcode rejected with a clear Arabic error; a tenant with zero warehouses still creates the master rows *("Duplicate barcode rejected" — API maps the gateway's tenant-wide uniqueness check (`db.Barcodes` unique index) to 409, console shows a canned Arabic message on that status, unit-tested in `TestCreateProduct_DuplicateBarcode`. "Zero warehouses still creates master rows" — the gateway's WPI-seeding loop iterates whatever `db.Warehouses` returns; an empty list just means zero WPI rows get added while Product/Units/Barcodes are unaffected — verified by code inspection, not a live test, since sync-gateway has no test project (dotnet build is its only gate, same as T19/T23))*
  - Verify: `dotnet build` clean, `go build ./... && go test ./...` clean (`TestCreateProduct_*` × 4), `pnpm build && pnpm lint` clean — all 2026-07-15. Live curl check against the real running API confirmed all validation paths (missing name, invalid kind, no units, non-positive val_sub, negative price, valid-but-unsubscribed → 402) return exactly the coded responses.
  - Files: `sync-gateway/HqApi.cs`, `sync-gateway/Program.cs`, `api/internal/hq/service.go` + `service_test.go`, `api/internal/httpapi/{hq_handlers,server}.go`, `console/src/pages/console/Catalog.tsx`, `console/src/components/{CreateProductDialog,PropagationPanel}.tsx` (new), `console/src/lib/{types,api,hooks}.ts`
  - Dependencies: T22, T25 · **Size: L**

### Checkpoint 3
- [x] All gates green (api `go test ./...`, gateway `dotnet build`, console `pnpm build && pnpm lint`)
- [x] Manual e2e: catalog list/detail matches the desktop products screen for a real synced tenant *(human-verified 2026-07-15)*
- [x] Manual e2e: HQ price change reaches the desktop on its next round; propagation chip flips «وصل» live *(human-verified 2026-07-15)*
- [x] Manual e2e: HQ-created product visible and sellable at a branch after sync *(human-verified 2026-07-15; also cross-checked group create propagating desktop→HQ→other branch, and new-product opening qty correctly zero until set at the branch)*
- [x] Extra edge cases checked and good *(human-verified 2026-07-15: HQ/branch conflict → ServerWins + ConflictLog row; duplicate-barcode create rejected with correct toast; non-stock kinds (خدمة مباعة/مشتراة) behave correctly; barcode scan at branch POS resolves the console-created product)*
- [x] **Human review before Phase 4 (Inventory)** *(approved 2026-07-15)*

## Phase 4 — Inventory

Design notes (2026-07-15): low-stock rule mirrors `InventoryStockRule.cs` byte-for-byte (TotalQty<0→سالب, ==0→نفاد, ReOrder>0 && qty<=ReOrder→تحت حد إعادة الطلب, ReOrder==0 never low). Only `ProductKind.Product` is stockable — every query needs that guard since T26 seeds WPI rows for all kinds. `InventoryMovements` has no BranchId/IssueDate index, so every movements query is ProductId-anchored — no list-all endpoint. Stale-branch condition is free (API already has `healthTier`). Movements live on catalog ProductDetail, not a separate route. View toggle is URL state (`?view=attention|products|branches&branch=`), default `attention`.

- [x] **T27: Gateway — branch-summary + attention reads**
  - **Description:** `GET /hq/inventory/branch-summary`: two grouped aggregates over WPI (by BranchId; by BranchId+Warehouse) — `sku_count` (distinct ProductId), `stock_value` (Σ TotalCost, includes inactive), `negative_count`/`out_count`/`low_count` (IsActive-filtered, desktop rule). `GET /hq/inventory/attention?branch_id=&page=&page_size=`: desktop rule verbatim (`Product.IsActive && (TotalQty<=0 || (double)TotalQty<=Product.ReOrder)`) + `ProductKind.Product` guard; unpaged per-severity `counts` + severity-ordered deterministic paging (negative→out→low, then TotalQty, then ProductId). Shared `StockStatus(qty, reOrder)` classifier.
  - Acceptance:
    - [x] Classification is byte-for-byte `InventoryStockRule` semantics (incl. double cast, ReOrder==0 never low) plus the ProductKind guard
    - [ ] Severity-ordered deterministic paging with correct per-severity counts; empty shapes on `IsDatabaseMissing`; 401 without a valid HqToken; db_name only from token *(structural — same TryHqAuth/IsDatabaseMissing path every other /hq/* endpoint uses since T5; live curl against a real synced tenant DB needs a human pass, same as T19/T23/T26 — folds into checkpoint 4)*
  - Verify: `dotnet build AribSyncGateway.csproj` clean, 2026-07-15; curl against a real tenant DB **pending — see acceptance note above**
  - Files: `sync-gateway/HqApi.cs`, `sync-gateway/Program.cs`
  - Dependencies: none · **Size: M**

- [x] **T28: Gateway — paged by-product inventory list**
  - **Description:** `GET /hq/inventory/products?search=&group_id=&branch_id=&status=&page=&page_size=`. Base `db.Products.Where(ProductKind.Product)`; search/group filters copied from `ProductsAsync`; WPI aggregates via ProductId-indexed subqueries scoped by `branch_id` when present; `status ∈ negative|out|low|attention` filters SQL-side at the aggregate level (company-wide or branch-scoped) so total/paging agree; ordered by ProductCode. Row: id, code, name, group, is_active, unit, re_order, total_qty, stock_value, branches_with_stock, last_activity_at, status (computed in C#; inactive → "ok").
  - Acceptance:
    - [x] `status` param filters SQL-side (CountAsync matches the page); `branch_id` scopes every aggregate correctly *(structural — same query composed before materialization for every branch of the Where; live curl needs a human pass, folds into checkpoint 4)*
    - [x] Services never appear in results; page_size clamped 1..200
  - Verify: `dotnet build AribSyncGateway.csproj` clean, 2026-07-15; curl each status value against a dev tenant DB **pending — see acceptance note above**
  - Files: `sync-gateway/HqApi.cs`, `sync-gateway/Program.cs`
  - Dependencies: T27 (shares classifier) · **Size: S**

- [x] **T29: Gateway — product movement history**
  - **Description:** `GET /hq/products/{id:guid}/movements?branch_id=&from=&to=&page=&page_size=`. 404 on unknown product (same check as `ProductAsync`). Default window = last 30 days; half-open `[from, to+1d)` on raw `IssueDate`; opening balance = `SUM(InQty-OutQty)` before `from` (desktop's Step-1, `!IsDeleted` guard added — vestigial column, kept for parity); page-N seed = net of skipped period rows; running qty accumulated in C# decimal per row of the returned page. `dealing` ships as the raw int.
  - Acceptance:
    - [x] Page N's first `running_qty` = page N−1's last `running_qty` + that row's net (pages are self-contained) *(by construction: seed = opening + Sum of skipped rows' net, verified by code inspection — the seed and the per-row accumulator use the exact same `InQty-OutQty` expression)*
    - [x] Every query is ProductId-anchored (no unfiltered scan); unbounded period's final running qty matches that branch's WPI TotalQty *(structural — live comparison against a dev tenant DB needs a human pass, folds into checkpoint 4)*
  - Verify: `dotnet build AribSyncGateway.csproj` clean, 2026-07-15; compare against a dev tenant DB **pending — see acceptance note above**
  - Files: `sync-gateway/HqApi.cs`, `sync-gateway/Program.cs`
  - Dependencies: none · **Size: M**

- [x] **T30: API — inventory passthrough + stale merge + tests**
  - **Description:** Four `hq.Service` methods mirroring the catalog chain (resolveGateway → getJSON → envelope): `InventoryByBranch` (registry merge — every `store.BranchesByTenant` branch renders, zeros if absent from the gateway payload, decorated with branch_name/health/last_sync_at, plus API-summed `totals{stock_value,negative,out,low}`), `InventoryProducts` (pure passthrough), `InventoryAttention` (item decoration + `stale_branches` array merged from registry branches where `healthTier=="stale"`, respecting `branch_id`; `never` branches excluded), `ProductMovements` (passthrough, 404→ErrNotFound, rows decorated with branch_name). Handlers validate query whitelists (`status` rejects unknown values 400; `from`/`to` validated as `YYYY-MM-DD`). Routes: `GET /hq/inventory/branches|products|attention`, `GET /hq/catalog/products/{productId}/movements`.
  - Acceptance:
    - [x] Every payload is `{data, source:"synced", as_of}`; `stale_branches` present iff a branch is >30 min stale (fake-clock test), absent for "never" *(`TestInventoryAttention_MergesStaleBranchesAndDecoratesItems`: 3 branches — fresh/stale/never — asserts exactly the stale one appears, never-synced excluded, and that `branch_id` scopes the merge)*
    - [x] By-branch includes registry branches missing from the gateway payload as zeros; totals sum correctly; existing error map (402/403/503/404) unchanged *(`TestInventoryByBranch_MergesRegistryAndSumsTotals`: gateway reports only 1 of 2 registry branches, missing one zeros out with non-nil `warehouses:[]`, totals sum the reported branch only)*
  - Verify: `go build ./... && go test ./...` clean, 2026-07-15 (full suite, not just the new tests)
  - Files: `api/internal/hq/service.go` + `service_test.go`, `api/internal/httpapi/hq_handlers.go`, `api/internal/httpapi/server.go`
  - Dependencies: T27, T28, T29 (contract; may start on fakes) · **Size: M**

- [x] **T31: Console — lib plumbing**
  - **Description:** Types (`InventoryStatus`, `InventoryBranchView`, `InventoryTotals`, `AttentionItem`, `StaleBranch`, `MovementRow`, paged response aliases, all `CatalogEnvelope<…>`), `api.ts` functions (URLSearchParams builders, catalog style), `qk` keys under a shared `['hq-inventory', tenantId, …]` prefix, four hooks (`enabled: !!tenantId`; `keepPreviousData` on the three paged ones; movements additionally gated by an `enabled` arg for the lazy section). `useTenantEvents` gains one invalidation line by the `hq-inventory` prefix so `branch-synced` flips every inventory view live. Extract Catalog's private `Pagination` into `components/Pagination.tsx`.
  - Acceptance:
    - [x] `pnpm build` type-checks the contract types against T27–T30's shapes
    - [x] `branch-synced` SSE invalidates all `hq-inventory` keys via the shared prefix; Catalog renders unchanged with the extracted `Pagination`
  - Verify: `pnpm build && pnpm lint` clean, 2026-07-15
  - Files: `console/src/lib/{types,api,query,hooks}.ts`, `console/src/components/Pagination.tsx`, `console/src/pages/console/Catalog.tsx`
  - Dependencies: T30 · **Size: S**

- [x] **T32: Console — Inventory shell + needs-attention view**
  - **Description:** Replace the `Inventory.tsx` placeholder: `PageHeader` + `Freshness` + three-segment URL-state toggle (`?view=`, default `attention`). Attention view: stale-branch strip (warning cards → `/tenants/{id}/branches/{branchId}`), three count tiles, severity-ordered table (status `Badge`, product+code, branch/warehouse with `HealthDot`, qty vs re_order, last-movement relative time) with rows → `/tenants/{id}/catalog/{productId}`; pagination; success-toned empty state when clean.
  - Acceptance:
    - [x] `?view=attention&branch={id}` deep-link filters correctly (this is the Phase-5 alert target) *(`branch` read via `useSearchParams`, passed straight to `useInventoryAttention`'s `branchId`; a branch `<select>` bound to the same param lets a user change it, and the by-branch view's count chips link to exactly this URL shape)*
    - [x] Every row/strip click lands on the screen that resolves it; Arabic digits/RTL throughout; 402 → EmptyState
  - Verify: `pnpm build && pnpm lint` clean, 2026-07-15; manual click-through **pending — no browser-automation tool available this session (same gap as T21/T22/T25); folds into checkpoint 4**
  - Files: `console/src/pages/console/Inventory.tsx`
  - Dependencies: T31 · **Size: M**

- [x] **T33: Console — by-product + by-branch views**
  - **Description:** By-product view: debounced search, group `<select>` (flat, from `useCatalogGroups`), branch `<select>`, status filter chips; table (code/name/group/qty/value/status/branches-with-stock) → ProductDetail. By-branch view: card per branch (HealthDot + Freshness, sku count, stock value, three count chips → `?view=attention&branch={id}`, collapsible warehouse breakdown, «عرض الفرع» → branch detail); totals row.
  - Acceptance:
    - [x] Filter changes reset to page 1 without spinner-blanking (`keepPreviousData` + render-time reset, Catalog's pattern) *(both `AttentionView` and `ProductsView` use the exact `filterKey`/`lastFilterKey` render-time-reset pattern from `Catalog.tsx`)*
    - [x] By-branch count chips navigate to the pre-filtered attention view; totals row matches the sum of the branch cards *(chips link to `?view=attention&branch={id}`; totals come from the API's own `InventoryTotals` sum, same source as the cards — can't drift)*
  - Verify: `pnpm build && pnpm lint` clean, 2026-07-15; manual click-through **pending — no browser-automation tool available this session; folds into checkpoint 4**
  - Files: `console/src/pages/console/Inventory.tsx` (+ subcomponents if it earns extraction)
  - Dependencies: T32 · **Size: M**

- [x] **T34: Console — ProductDetail movements section**
  - **Description:** New collapsible `Section` «حركة الصنف» on `ProductDetail.tsx`, query enabled only once opened (native `<details onToggle>`). Controls: branch select, period presets (٧/٣٠/٩٠ يومًا). Table: pinned «رصيد أول المدة» row (`opening_qty`), then date/dealing (Arabic label map + fallback)/warehouse/in/out/running qty/customer; pagination.
  - Acceptance:
    - [x] Section issues zero requests until opened *(`Section` gained an `onToggle` prop wired to `<details onToggle>`; `MovementsSection` only sets `opened=true` on the open transition and passes it straight through as `useProductMovements`'s `enabled` arg — before that, `query.data` never renders because the whole body short-circuits on `!opened`)*
    - [x] Opening balance and running qty render server values verbatim (no client-side arithmetic); dealing ints map to the seven Arabic labels with a safe fallback for unknown values *(`query.data.data.opening_qty`/`.running_qty` render directly, no arithmetic in the component; `dealingLabel()` covers 100/101/200/201/300/700/2000 with a `نوع ${d}` fallback)*
  - Verify: `pnpm build && pnpm lint` clean, 2026-07-15; manual check **pending — no browser-automation tool available this session; folds into checkpoint 4**
  - Files: `console/src/pages/console/ProductDetail.tsx`, `console/src/components/icon.tsx` (added `HistoryIcon`), `console/src/components/Pagination.tsx` (added optional `itemLabel`)
  - Dependencies: T31 (parallel with T32/T33) · **Size: M**

### Checkpoint 4
- [x] All gates green (api `go build ./... && go vet ./... && go test ./...`, gateway `dotnet build AribSyncGateway.csproj`, console `pnpm build && pnpm lint` — all clean 2026-07-15)
- [x] Manual e2e: attention counts/rows match the desktop notification center for a real synced tenant (incl. ReOrder==0 at qty 0 → نفاد not low; qty==ReOrder boundary → low) *(human-verified 2026-07-15; surfaced and fixed two real bugs along the way — see below)*
- [x] Manual e2e: POS sale past zero → row appears سالب in attention live via SSE, no refresh; branch adjustment clears it the same way *(human-verified 2026-07-15)*
- [x] Manual e2e: by-branch stock value matches desktop warehouse valuation; by-product qty spot-checks against the catalog list *(human-verified 2026-07-15)*
- [x] Manual e2e: movements parity vs desktop ProductMove screen (opening, rows, running qty); unbounded-period final running qty equals that branch's WPI TotalQty *(human-verified 2026-07-15; also validates the `IsDeleted`-exclusion decision (T29) since the desktop's own query doesn't filter it)*
- [x] Stale branch (>30 min) appears in the attention strip, link opens branch detail, disappears after it syncs *(human-verified 2026-07-15)*
- [x] RTL/Arabic-numerals audit across all three views + movements (negative quantities in RTL) *(human-verified 2026-07-15)*
- [x] **Human review before Phase 5 (Notifications + Ctrl+K)** *(approved 2026-07-15)*

**Bugs found and fixed during Checkpoint 4 e2e (2026-07-15):**
- SSE `/v1/tenants/{id}/events` 500ed on every single connection since the feature was first added — `requestLogger`'s `statusWriter` wrapper embeds `http.ResponseWriter` (an interface), which only promotes that interface's own methods, not `Flush()`. Fixed by adding an explicit `Flush()` delegation on `statusWriter` (`api/internal/httpapi/middleware.go`).
- `/hq/inventory/attention` 500ed specifically once a row entered the low/out/negative bucket (i.e. exactly when reorder ≥ stock) — Postgres `timestamp without time zone` columns (`WarehousesProductInventories.LastInDate/LastOutDate`) round-trip through Npgsql as zone-less `DateTime`, which .NET serializes without a `Z`/offset; Go's strict-RFC3339 `time.Time` decoder then fails to parse it. Fixed with a global UTC-forcing `DateTime` JSON converter in the gateway (`sync-gateway/Program.cs`) so every endpoint returning a raw DB timestamp round-trips correctly, not just this one field. Also added error logging to `writeHqError`'s 500 fallback (`api/internal/httpapi/hq_handlers.go`) so future unhandled errors aren't silently swallowed.

## Phase 5 — Notifications + Ctrl+K

Design notes (2026-07-15): only ConflictLog needs new backend surface — stale/never branches and attention counts already flow live. Alert derivation is client-side in a shared `lib/alerts.ts` feeding Overview panel + bell alike. Conflict alerts need server-side ack (`AcknowledgedAt` column — ConflictLog is gateway-ensured central-only DDL, **not** an `AribONE.Data` schema change, no SchemaVersion bump; existing DBs upgrade via add-column-if-missing). DMS upload conflicts: `LocalRow` = kept central row, `RemoteRow` = branch's losing write (orientation verified at checkpoint). Product deep-links extracted gateway-side best-effort (Products → RowPk; UnitOfMeasure → row's ProductId; Barcodes → UoM lookup). Ctrl+K built in-house on the existing Radix Dialog — no cmdk dependency.

- [x] **T35: Gateway — ConflictLog read chain + ack** *(sync-gateway `0dbd2c3`, 2026-07-15)*
  - **Description:** `EnsureConflictLogSql` gains `AcknowledgedAt` (nullable UTC) with add-column-if-missing for pre-existing tables (both dialects); ensure now also runs before HQ conflict reads (today only the first logged conflict creates the table — reads must tolerate/ensure absence). `GET /hq/conflicts?page=&page_size=&all=`: newest-first (Id DESC) page, default unacked-only, `all=1` includes acked; response `{unacked, total, page, page_size, items:[{id, occurred_at, branch_id, table_name, row_pk, conflict_type, resolution, local_row, remote_row, acknowledged_at, product_id, product_name}]}` — product fields best-effort from row JSON (+ one EF lookup batch for Barcodes/UoM resolution and product names). `POST /hq/conflicts/ack` body `{ids?: number[], up_to_id?: number}` → one UPDATE setting `AcknowledgedAt` where null; returns `{acked}` count. Same `TryHqAuth` + db_name-from-token rule as every /hq/* endpoint; empty shapes on missing DB/table.
  - Acceptance:
    - [x] A tenant DB created before this change (ConflictLog without the column) lists and acks correctly after the ensure runs *(structural: ensure DDL now runs before every conflicts read/ack, CREATE-if-missing + add-column-if-missing on both dialects; live pass against a real pre-upgrade DB folds into checkpoint 5)*
    - [x] Paging is stable (Id DESC); `unacked` count is unpaged; ack is idempotent (second call returns 0 — the UPDATEs guard on `AcknowledgedAt IS NULL`); 401 without a valid HqToken *(same TryHqAuth path as every /hq/* endpoint)*
  - Note: ack timestamps are computed **server-side in SQL** (`now() AT TIME ZONE 'utc'` / `SYSUTCDATETIME()`) — Npgsql 6+ rejects UTC-Kind DateTime params on `timestamp without time zone` columns, so no @now param exists to get wrong.
  - Verify: `dotnet build AribSyncGateway.csproj` clean, 2026-07-15; curl against a dev tenant DB with real ConflictLog rows **pending — folds into checkpoint 5**
  - Files: `sync-gateway/Db/IDbDialect.cs`, `sync-gateway/Db/PostgresDialect.cs`, `sync-gateway/Db/SqlServerDialect.cs`, `sync-gateway/HqApi.cs`, `sync-gateway/Program.cs`, `sync-gateway/ConflictLog.cs` (shared ensure)
  - Dependencies: none · **Size: M**

- [x] **T36: API — conflicts passthrough + ack + tests** *(2026-07-15)*
  - **Description:** `hq.Service.Conflicts` (resolveGateway → getJSON → envelope; items decorated with branch_name from the registry, "never"-style fallback for unknown branch ids) and `hq.Service.AckConflicts` (POST passthrough; validates ids/up_to_id present and positive). Handlers whitelist `page/page_size/all`; routes `GET /v1/tenants/{id}/hq/conflicts`, `POST /v1/tenants/{id}/hq/conflicts/ack`. Ack logged like other HQ writes (`hq.conflicts_ack`: tenant, account, email, count). Table-driven tests beside the service: decoration, envelope shape, ack body validation, error map unchanged.
  - Acceptance:
    - [x] Payload is `{data:{unacked,total,page,page_size,items}, source:"synced", as_of}`; branch names resolve from the registry *(`TestConflicts_PassesParamsAndDecoratesBranchNames`: known branch gets its name + product link kept, unknown branch stays undecorated; `TestConflicts_EmptyItemsNeverNil` pins `items: []`)*
    - [x] Ack with neither ids nor up_to_id → 400 without a gateway round-trip *(handler-level check; also caps ids at 200 and rejects non-positive ids/up_to_id)*; `go test ./...` green *(`TestAckConflicts_ForwardsBodyAndReturnsCount`, `TestAckConflicts_Ownership`)*
  - Verify: `go build ./... && go vet ./... && go test ./...` clean, 2026-07-15 (full suite)
  - Files: `api/internal/hq/service.go` + `service_test.go`, `api/internal/httpapi/hq_handlers.go`, `api/internal/httpapi/server.go`
  - Dependencies: T35 (contract; may start on fakes) · **Size: M**

- [x] **T37: Console — lib plumbing + shared alert derivation** *(2026-07-15)*
  - **Description:** Types (`ConflictItem`, `ConflictsResponse`, ack input/result), `api.ts` functions, `qk.conflicts` under a `['hq-conflicts', tenantId, …]` prefix, hooks (`useConflicts(tenantId, {page, all})` with `keepPreviousData`; `useAckConflicts` invalidating the prefix). `useTenantEvents` gains the `hq-conflicts` prefix invalidation (conflicts only change on sync rounds). New `lib/alerts.ts`: `deriveAlerts(tenantId, {branches, attention, conflictsUnacked})` → ordered `Alert[]` (danger: unacked conflicts «تعارض مزامنة» → `/conflicts`, negative/out counts → `/inventory?view=attention`, stale branch → branch detail; info: low count → attention, never → download). Overview drops its private `deriveAlerts` and consumes the shared one (now also calling `useInventoryAttention`/`useConflicts` — two extra cheap queries), keeping panel behavior otherwise identical.
  - Acceptance:
    - [x] Overview alert rows for stale/never render exactly as before (same text/links), now from the shared lib *(same key/tone/text/to strings, just sourced from `lib/alerts.ts`)*
    - [x] `pnpm build` type-checks the contract against T36's shapes; SSE `branch-synced` invalidates conflicts *(`hq-conflicts` prefix added to `useTenantEvents`)*
  - Verify: `pnpm build && pnpm lint` clean, 2026-07-15
  - Files: `console/src/lib/{types,api,query,hooks}.ts`, `console/src/lib/alerts.ts`, `console/src/pages/console/Overview.tsx`
  - Dependencies: T36 · **Size: M**

- [ ] **T38: Console — notifications bell**
  - **Description:** `NotificationsBell` in the AppShell header (both desktop and mobile rows): bell icon + count badge (Arabic digits, hidden at 0, «٩+» cap) over the same `deriveAlerts` output as Overview (bell mounts `useHqBranches` + attention-counts + conflicts queries — all cached/shared keys, SSE-live). Dropdown (existing dropdown-menu primitive): alert rows with tone icon + text, each deep-linking and closing the menu; footer «عرض كل التعارضات» → `/conflicts` when any conflict alert exists; success-toned empty state «لا توجد تنبيهات».
  - Acceptance:
    - [ ] Badge count == Overview alerts panel row count (same derivation, by construction); flips live via SSE without refresh
    - [ ] Every row navigates to the screen that resolves it; RTL layout correct
  - Verify: `pnpm build && pnpm lint` clean; manual click-through folds into checkpoint 5
  - Files: `console/src/components/NotificationsBell.tsx`, `console/src/components/AppShell.tsx`, `console/src/components/icon.tsx` (bell icon)
  - Dependencies: T37 · **Size: S**

- [ ] **T39: Console — conflicts review page**
  - **Description:** Route `/tenants/{id}/conflicts` (no sidebar entry — reached from bell/Overview alerts; AppShell current-section lookup + breadcrumb «التنبيهات والتعارضات»). Header + `Freshness` + unacked count; filter toggle «غير المُراجَعة فقط» (default) / «الكل» (`all=1`). List: one card per conflict — occurred_at (relative + absolute), branch name, table label (Arabic map for known tables: المنتجات/الوحدات/الباركود/… + raw fallback), kept-vs-overridden columns rendered from `local_row`/`remote_row` JSON showing only differing fields (label map for common columns; null remote → «حذف من الفرع»), «افتح المنتج» when product_id present, per-row «تمت المراجعة» + header «تحديد الكل كمُراجَع» (up_to_id = newest visible). Pagination; empty states (clean / all reviewed).
  - Acceptance:
    - [ ] Ack (single + bulk) removes rows from the default view and drops the bell badge without refresh (shared invalidation)
    - [ ] Unknown tables/malformed row JSON degrade gracefully (raw table name, no diff table, page never crashes)
  - Verify: `pnpm build && pnpm lint` clean; real-conflict pass folds into checkpoint 5
  - Files: `console/src/pages/console/Conflicts.tsx`, `console/src/App.tsx` (route), `console/src/components/AppShell.tsx` (breadcrumb lookup)
  - Dependencies: T37 · **Size: M**

- [ ] **T40: Console — top-bar branch-status indicator**
  - **Description:** `BranchStatusIndicator` beside the bell: worst health tier across `useHqBranches` (never < ok < lagging < stale for severity — a stale branch wins) as a `HealthDot` + count label («٣ فروع»); dropdown lists every branch (HealthDot + name + relative last-sync) linking to its detail page; footer «كل الفروع» → `/branches`. Hidden while the tenant has no branches.
  - Acceptance:
    - [ ] Indicator flips live when a branch syncs (SSE, shared `hq-branches` key); dropdown rows deep-link correctly
  - Verify: `pnpm build && pnpm lint` clean; live flip folds into checkpoint 5
  - Files: `console/src/components/BranchStatusIndicator.tsx`, `console/src/components/AppShell.tsx`
  - Dependencies: T37 (shares the always-mounted `useHqBranches`) · **Size: S**

- [ ] **T41: Console — Ctrl+K command palette**
  - **Description:** In-house `CommandPalette` on the existing Radix Dialog (top-aligned, RTL): opened by Ctrl+K/Cmd+K (window keydown, ignoring inputs' own editing only when palette closed — the shortcut always wins) or a search button in the header. Input + grouped results with full keyboard nav (↑/↓ wraps, Enter navigates, Esc closes; listbox/option ARIA roles). Sections: **الصفحات** (static nav registry incl. التعارضات), **الفروع** (client-filtered from the cached bundle), **المنتجات** (debounced ≥2-char `useCatalogProducts` search — name/code/barcode via the existing gateway query — top 8, «بحث في الكتالوج…» row linking to `/catalog?search=`), **إجراءات** (تنزيل التطبيق، إضافة فرع، إضافة منتج — navigation shortcuts to the owning screens). Selecting navigates and closes; query resets on close.
  - Acceptance:
    - [ ] Keyboard-only round trip: Ctrl+K → type → ↑/↓ → Enter lands on the target; Esc restores focus
    - [ ] No new dependency added; product search issues zero requests under 2 chars and is debounced
  - Verify: `pnpm build && pnpm lint` clean; manual pass folds into checkpoint 5
  - Files: `console/src/components/CommandPalette.tsx`, `console/src/components/AppShell.tsx`, `console/src/pages/console/Catalog.tsx` (honor `?search=` deep-link if not already)
  - Dependencies: T37 (product search hook already exists — only ordering with the shell changes) · **Size: M**

### Checkpoint 5
- [ ] All gates green (api `go build ./... && go vet ./... && go test ./...`, gateway `dotnet build AribSyncGateway.csproj`, console `pnpm build && pnpm lint`)
- [ ] Manual e2e: forced real conflict (HQ price change + branch edit before its sync) → ServerWins at the branch; conflict appears in bell + review page live via SSE; kept/overridden orientation correct; product deep-link works; ack clears everywhere
- [ ] Manual e2e: low/out/negative and stale alerts in the bell deep-link to attention view / branch detail and clear when resolved
- [ ] Manual e2e: Ctrl+K keyboard-only navigation (page, branch, product by name/code/barcode); RTL correct
- [ ] Pre-existing ConflictLog rows survive the AcknowledgedAt DDL upgrade and list correctly
- [ ] RTL/Arabic-numerals audit (badge, palette, review page)
- [ ] **Human review before Phase 6 (Reports)**
