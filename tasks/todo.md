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

- [x] **T38: Console — notifications bell** *(2026-07-15)*
  - **Description:** `NotificationsBell` in the AppShell header (the single header bar covers both the desktop breadcrumb row and the mobile nav row — the bell sits beside `AccountMenu`, unconditional on breakpoint): bell icon + count badge (Arabic digits, hidden at 0, «٩+» cap) over the same `deriveAlerts` output as Overview (bell mounts `useHqBranches` + `useInventoryAttention({})` + `useConflicts({})` — all cached/shared keys, SSE-live). Dropdown (existing dropdown-menu primitive, `DropdownMenuItem asChild` wrapping `Link`): alert rows with tone icon + text, each deep-linking and closing the menu; footer «عرض كل التعارضات» → `/conflicts` when any conflict alert exists; success-toned empty state «لا توجد تنبيهات». Added `BellIcon` (Solar `BellBing`) to the shared icon surface.
  - Acceptance:
    - [x] Badge count == Overview alerts panel row count (same derivation, by construction) *(both call `deriveAlerts` with the same three inputs)*; flips live via SSE without refresh *(inherits `hq-branches`/`hq-inventory`/`hq-conflicts` invalidation from `useTenantEvents`)*
    - [x] Every row navigates to the screen that resolves it; RTL layout correct *(dropdown-menu primitive already defaults `dir="rtl"`)*
  - Verify: `pnpm build && pnpm lint` clean, 2026-07-15; manual click-through **pending — folds into checkpoint 5**
  - Files: `console/src/components/NotificationsBell.tsx`, `console/src/components/AppShell.tsx`, `console/src/components/icon.tsx` (bell icon)
  - Dependencies: T37 · **Size: S**

- [x] **T39: Console — conflicts review page** *(2026-07-15)*
  - **Description:** Route `/tenants/{id}/conflicts` (no sidebar entry — reached from bell/Overview alerts; AppShell's breadcrumb `current` lookup gained a `hiddenRoutes` list — deep-link-only entries with a label but no nav item — and now matches by prefix uniformly instead of special-casing `end`). Header + `Freshness` (from the envelope's `source`/`as_of`) + unacked count; filter toggle «غير المُراجَعة فقط» (default) / «الكل» (`all=1`, resets to page 1). List: one card per conflict — occurred_at (relative, absolute in the `title` tooltip), branch name, table label (`TABLE_LABELS` map: Products/UnitOfMeasures/Barcodes → raw fallback for anything else), kept-vs-overridden diff table from `local_row`/`remote_row` JSON showing only differing fields (`FIELD_LABELS` map for common AribONE.Data columns, raw key fallback; `Id`/`ProductId`/`UnitOfMeasureId` skipped as never-differing FK/PK; null remote → «حذف من الفرع» in place of a diff table), «افتح المنتج» when product_id present, per-row «تمت المراجعة» + header «تحديد الكل كمُراجَع» (up_to_id = page 1's first row id, since pages are Id DESC). Pagination; empty states (clean / all reviewed) via `EmptyState`.
  - Acceptance:
    - [x] Ack (single + bulk) removes rows from the default view and drops the bell badge without refresh *(both go through `useAckConflicts`, which invalidates the shared `hq-conflicts` prefix that both the bell and this page read)*
    - [x] Unknown tables/malformed row JSON degrade gracefully *(raw table/field-name fallback via the label maps; `diffFields` returns `null` on unparseable JSON and the card renders a plain "تعذّر عرض تفاصيل هذا التعارض" note instead of throwing)*
  - Verify: `pnpm build && pnpm lint` clean, 2026-07-15; real-conflict pass **pending — folds into checkpoint 5**
  - Files: `console/src/pages/console/Conflicts.tsx`, `console/src/App.tsx` (route), `console/src/components/AppShell.tsx` (breadcrumb lookup)
  - Dependencies: T37 · **Size: M**

- [x] **T40: Console — top-bar branch-status indicator** *(2026-07-15)*
  - **Description:** `BranchStatusIndicator` beside the bell: worst health tier across `useHqBranches` (never < ok < lagging < stale for severity — a stale branch wins) as a `HealthDot` + count label («٣ فروع»); dropdown lists every branch (HealthDot + name + relative last-sync) linking to its detail page; footer «كل الفروع» → `/branches`. Hidden while the tenant has no branches.
  - Acceptance:
    - [x] Indicator flips live when a branch syncs (SSE, shared `hq-branches` key) *(reuses the exact `useHqBranches` hook/query key the bell and Overview already keep live — no new invalidation needed)*; dropdown rows deep-link correctly
  - Verify: `pnpm build && pnpm lint` clean, 2026-07-15; live flip **pending — folds into checkpoint 5**
  - Files: `console/src/components/BranchStatusIndicator.tsx`, `console/src/components/AppShell.tsx`
  - Dependencies: T37 (shares the always-mounted `useHqBranches`) · **Size: S**

- [x] **T41: Console — Ctrl+K command palette** *(2026-07-15)*
  - **Description:** In-house `CommandPalette` on the raw Radix Dialog primitive (top-aligned, RTL, custom width/chrome instead of the shared `ui/dialog` wrapper): opened by Ctrl+K/Cmd+K (a window-level `keydown` listener that always wins, even while focus is inside a text input) or a search button in the header (shows the ⌘K/Ctrl K hint). Input + grouped results with full keyboard nav (↑/↓ wraps via modulo, Enter navigates, Esc closes and restores focus — Radix's default `onCloseAutoFocus`; `role="combobox"`/`listbox`/`option` + `aria-activedescendant`). Sections: **الصفحات** (static registry mirroring AppShell's nav + التعارضات, which has no sidebar entry), **الفروع** (client-filtered from the cached bundle, zero extra requests), **المنتجات** (only once the query is ≥2 chars, debounced 300ms via `useCatalogProducts`, top 8 + a «بحث في الكتالوج…» row linking to `/catalog?search=`), **إجراءات** (تنزيل التطبيق، إضافة فرع، إضافة منتج — navigation shortcuts to the owning screens, not auto-opened dialogs). Selecting navigates and closes; the dialog body unmounts on close (Radix default), so query/selection always reset on next open with no explicit reset code. `Catalog.tsx` now seeds its search box from `?search=` on mount.
  - Acceptance:
    - [x] Keyboard-only round trip: Ctrl+K → type → ↑/↓ → Enter lands on the target; Esc restores focus *(structural: global listener + combobox keydown handler + Radix's default close-autofocus; live keyboard pass pending)*
    - [x] No new dependency added *(built on the already-installed `@radix-ui/react-dialog`, no `cmdk`)*; product search issues zero requests under 2 chars and is debounced *(`useCatalogProducts` only called with a defined `tenantId` once `debouncedQuery.length >= 2`)*
  - Verify: `pnpm build && pnpm lint` clean, 2026-07-15; manual pass **pending — folds into checkpoint 5**
  - Files: `console/src/components/CommandPalette.tsx`, `console/src/components/AppShell.tsx`, `console/src/pages/console/Catalog.tsx` (`?search=` deep-link)
  - Dependencies: T37 (product search hook already exists — only ordering with the shell changes) · **Size: M**

### Checkpoint 5
- [x] All gates green (api `go build ./... && go vet ./... && go test ./...`, gateway `dotnet build AribSyncGateway.csproj`, console `pnpm build && pnpm lint` — all clean 2026-07-15)
- [x] Manual e2e: forced real conflict → ServerWins at the branch; conflict appears in bell + review page live via SSE; kept/overridden orientation correct; product deep-link works; ack clears everywhere *(human-verified 2026-07-15)*
- [x] Manual e2e: low/out/negative and stale alerts in the bell deep-link to attention view / branch detail and clear when resolved *(human-verified 2026-07-15)*
- [x] Manual e2e: Ctrl+K keyboard-only navigation (page, branch, product by name/code/barcode); RTL correct *(human-verified 2026-07-15)*
- [x] Pre-existing ConflictLog rows survive the AcknowledgedAt DDL upgrade and list correctly *(human-verified 2026-07-15)*
- [x] RTL/Arabic-numerals audit (badge, palette, review page) *(human-verified 2026-07-15)*
- [x] **Human review before Phase 6 (Reports)** *(approved 2026-07-15)*

**Bugs found and fixed during Checkpoint 5 e2e (2026-07-15):** desktop `UpsertAccountViewModel.SaveAccount` re-stamped `Account.CreatedAt = DateTime.Now` on every edit (the entity's property initializer firing on the reconstructed object), diverging from central and flooding `ConflictLog` with spurious `Accounts` conflicts — fixed by preserving the original `CreatedAt` on the edit path. Separately, ~1508 pre-existing `ConflictLog` rows turned out to be a harmless DMS artifact: a branch's first sync re-uploads all pre-existing local rows as "untracked" (`desktop/Services/Sync/SyncService.cs`'s E2 initial-upload step), including the ~252 deterministic seed `Accounts` rows already present on central — DMS reports the PK collision as `RemoteExistsLocalExists` even when every column is byte-identical. Fixed in `sync-gateway/ConflictLog.cs`: `LogAsync` now skips writing a row when `LocalRow`/`RemoteRow` are field-for-field equal, since there's nothing to review.

## Phase 6 — Reports

Design notes (2026-07-15): **open question 2 resolved by the plan's standing assumption** (user proceeded past the checkpoint-5 gate): v1 reports are direct, date-bounded SQL aggregates on the tenant DB via the gateway — no rollups/replicas; every query is a period-scoped GroupBy over Bills/SaleEntries, fine at current fleet size, revisit before the fleet grows. Semantics mirror the desktop byte-for-byte: day scope on `CreatedAt` in gateway-local time (same TZ assumption as `BranchSnapshotAsync`; the desktop's own bills screens filter `CreatedAt.Date`), half-open `[from, to+1d)`; sales/refunds = `Sale`/`ReSale` TPH rows, `!IsDeleted`, Σ `Total` (T9's proven semantics); tender split mirrors `ShiftReportService` exactly (`Money`=نقدًا, `BankMoney`, `WalletMoney`, `Remain`=آجل, summed over Sale bills); profit mirrors `ProfitFromWarehouseViewModel` (Σ `Total` − Σ `ItemCost` over SaleEntries — `ItemCost` is the line's total COGS, batch-exact when batch-tracked; service kinds carry ItemCost=0 and stay included, their revenue is real). Product-report rows anchor through the bill (`!Bill.IsDeleted`, `Bill.CreatedAt` period) so the products view's revenue sums can never drift from the sales view's totals — a deliberate divergence from the desktop's profit screen, which filters on entry `CreatedAt` and skips the deleted-bill guard. Day series ships as local-date **strings** (`YYYY-MM-DD`), sidestepping the zone-less-timestamp bug class from checkpoints 2/4. Staff = GroupBy `Bills.UserId` joined to the Tier-A `Users` table. The Inventory question needs zero new backend — it renders `useInventoryByBranch`/attention data with links into the Inventory views. No chart dependency: the daily chart is inline SVG bars. Default period: آخر ٧ أيام; all report state (view/period/filters) is URL-borne for shareable deep links, same pattern as Inventory.

- [x] **T42: Gateway — sales report endpoint** *(sync-gateway `ccdc8b6`, 2026-07-15)*
  - **Description:** `GET /hq/reports/sales?from=&to=&branch_id=`: one aggregate row (`sales_total`, `sales_count`, `refunds_total`, `refunds_count`), tender split over Sale bills (`cash`, `bank`, `wallet`, `credit` = Money/BankMoney/WalletMoney/Remain), and per-day series `[{day:"YYYY-MM-DD", sales_total, sales_count, refunds_total}]` via GroupBy `CreatedAt.Date` (translates on both engines). Defaults last 7 days when from/to absent; `branch_id` scopes every aggregate. Same `TryHqAuth` + empty-shapes-on-`IsDatabaseMissing` pattern as every /hq/* endpoint.
  - Acceptance:
    - [ ] Totals/tender match the desktop's own numbers for a real synced tenant + period (folds into checkpoint 6 e2e)
    - [ ] Day boundaries are gateway-local; deleted bills excluded; empty DB → zeroed shape, not error
  - Verify: `dotnet build AribSyncGateway.csproj`; curl against a dev tenant DB
  - Files: `sync-gateway/HqApi.cs`, `sync-gateway/Program.cs`
  - Dependencies: none · **Size: M**

- [x] **T43: Gateway — products / branches / staff report endpoints** *(sync-gateway `ccdc8b6`, 2026-07-15)*
  - **Description:** `GET /hq/reports/products?from=&to=&branch_id=&group_id=&sort=&page=&page_size=` — GroupBy `SaleEntries.ProductId` anchored through the bill (`!Bill.IsDeleted`, `Bill.CreatedAt` in period, optional `Bill.BranchId`); row: product id/code/name/group_name/unit (master-unit name, same convention as inventory), `qty_sold` = Σ TotalQty, `revenue` = Σ Total, `profit` = Σ(Total−ItemCost); `sort ∈ revenue|qty|profit` (default revenue) with deterministic ThenBy ProductId; paged + clamped 1..200. `GET /hq/reports/branches?from=&to=` — GroupBy BranchId over Sale + ReSale (totals/counts) plus profit from SaleEntries. `GET /hq/reports/staff?from=&to=&branch_id=` — GroupBy UserId joined to Users (name), sales/refund totals and counts. All share T42's period parsing.
  - Acceptance:
    - [ ] Products revenue summed over all rows == sales report's `sales_total` for the same period/branch (self-consistency, checkable via curl)
    - [ ] `sort` values order correctly with stable paging; unknown product/group/user degrade to raw ids, never a 500
  - Verify: `dotnet build AribSyncGateway.csproj`; curl each endpoint against a dev tenant DB
  - Files: `sync-gateway/HqApi.cs`, `sync-gateway/Program.cs`
  - Dependencies: T42 (shares period helper) · **Size: M**

- [x] **T44: API — reports passthrough + decoration + tests** *(2026-07-15)*
  - **Description:** Four `hq.Service` methods mirroring the catalog chain (resolveGateway → getJSON → `{data, source:"synced", as_of}` envelope): `ReportSales` (pure passthrough), `ReportProducts` (passthrough), `ReportBranches` (registry merge — every registry branch renders zeroed if absent from the gateway payload, decorated with branch_name/health/last_sync_at, same shape philosophy as `InventoryByBranch`), `ReportStaff` (passthrough). Handlers validate `from`/`to` via the existing `dateParamRE`, whitelist `sort`, and pass only known params. Routes `GET /v1/tenants/{id}/hq/reports/sales|products|branches|staff`.
  - Acceptance:
    - [x] Table-driven tests: envelope shape, params forwarded, branches registry merge (gateway missing a branch → zeroed row present), error map unchanged *(`TestReportSales_*` ×3, `TestReportProducts_PassesParamsAndEmptyItemsNeverNil`, `TestReportBranches_MergesRegistryAndZeroFills` — registry-unknown gateway rows dropped, never branch zero-filled —, `TestReportStaff_PassthroughAndEmptyNeverNil`; error map shared with every other report via `resolveGateway`/`writeHqError`, unchanged)*
    - [x] Invalid `from`/`to`/`sort` → 400 with no gateway round-trip *(`validReportPeriod` + sort whitelist run before any service call)*
  - Verify: `go build ./... && go vet ./... && go test ./...`
  - Files: `api/internal/hq/service.go` + `service_test.go`, `api/internal/httpapi/hq_handlers.go`, `api/internal/httpapi/server.go`
  - Dependencies: T42, T43 (contract; may start on fakes) · **Size: M**

- [x] **T45: Console — lib plumbing + PeriodPicker + Reports shell** *(2026-07-15)*
  - **Description:** Types (`SalesReport`, `ProductReportRow`, `BranchReportRow`, `StaffReportRow`, paged/envelope aliases), `api.ts` functions, `qk` keys under a shared `['hq-reports', tenantId, …]` prefix, four hooks (`enabled: !!tenantId`, `keepPreviousData` on the paged products one); `useTenantEvents` gains the `hq-reports` prefix so `branch-synced` flips reports live. New `components/PeriodPicker.tsx`: presets (اليوم / أمس / آخر ٧ أيام / آخر ٣٠ يومًا / هذا الشهر) + custom from/to date inputs, reading/writing `?from=&to=` URL params. `Reports.tsx` shell: `PageHeader` + five-question URL-state toggle (`?view=sales|products|branches|staff|inventory`, default `sales`), same pattern as Inventory's toggle.
  - Acceptance:
    - [x] `pnpm build` type-checks the contract against T44's shapes; SSE invalidation wired *(`useTenantEvents` invalidates the `hq-reports` prefix on `branch-synced`)*
    - [x] Preset clicks and custom dates round-trip through the URL (deep-linkable); view toggle preserves period params *(both write the same `URLSearchParams` instance — `setView` copies existing params, so `from`/`to`/`branch` survive; presets compute local dates via a `localISO` helper, never `toISOString`'s UTC shift)*
  - Verify: `pnpm build && pnpm lint`
  - Files: `console/src/lib/{types,api,query,hooks}.ts`, `console/src/components/PeriodPicker.tsx`, `console/src/pages/console/Reports.tsx`
  - Dependencies: T44 (contract) · **Size: M**

- [x] **T46: Console — Sales + Branches report views** *(2026-07-15)*
  - **Description:** Sales view: KPI tiles (المبيعات، عدد الفواتير، المرتجعات، الصافي، متوسط الفاتورة — net/avg derived client-side), tender split row (نقدًا / بنك / محفظة / آجل), daily inline-SVG bar chart + day table, optional branch `<select>`. Branches view: comparison table — HealthDot + name, sales, refunds, net, profit, bills, متوسط الفاتورة — rows → branch detail; totals row; `<Freshness>` from the envelope.
  - Acceptance:
    - [x] Branch filter + period changes refetch correctly without spinner-blanking (`keepPreviousData` on all four report hooks); Arabic digits/RTL throughout; 402 → EmptyState
    - [x] Branches view renders every registry branch (zeroed included) with correct health colors *(rows come from T44's registry merge; HealthDot per row; client-side totals row)*
  - Note: the daily chart is CSS flex bars (no SVG, no chart dependency) — theme tokens apply directly, the row is pinned `dir="ltr"` so time reads chronologically, native tooltips per bar, only the peak day direct-labeled, x-labels thinned to ~8, and the day table below is the accessible view of the same numbers. Visual pass folds into checkpoint 6 (no browser automation this session, same as T21/T32).
  - Verify: `pnpm build && pnpm lint` clean, 2026-07-15
  - Files: `console/src/pages/console/Reports.tsx`, `console/src/components/PeriodPicker.tsx`
  - Dependencies: T45 · **Size: M**

- [x] **T47: Console — Products + Staff + Inventory report views** *(2026-07-15)*
  - **Description:** Products view: sort chips (الأعلى قيمةً / كميةً / ربحًا), group + branch `<select>`s, paged table (code/name/group/qty+unit/revenue/profit) with rows → `/catalog/{productId}`; render-time page reset on filter change (Catalog's pattern). Staff view: table (الموظف، عدد الفواتير، المبيعات، المرتجعات، متوسط الفاتورة). Inventory view: tiles from `useInventoryByBranch` (قيمة المخزون، سالب/نفاد/تحت الحد counts) deep-linking into `/inventory?view=branches|attention` — zero new backend.
  - Acceptance:
    - [x] Sort/filter changes reset paging deterministically (`filterKey`/`lastFilterKey` render-time reset, Catalog's pattern — period/branch/group/sort all in the key); product rows → `/catalog/{id}`, inventory cards → `/inventory?view=branches|attention`
    - [x] Staff view renders user names from the report payload (no extra call); empty period → clean empty state
  - Verify: `pnpm build && pnpm lint`; visual pass folds into checkpoint 6
  - Files: `console/src/pages/console/Reports.tsx`, `console/src/lib/hooks.ts` (if a lazy-enable arg is needed)
  - Dependencies: T45 · **Size: M**

### Checkpoint 6
- [x] All gates green (api `go build ./... && go vet ./... && go test ./...`, gateway `dotnet build AribSyncGateway.csproj`, console `pnpm build && pnpm lint` — all clean 2026-07-15)
- [x] **Found during checkpoint testing:** the freshness pill read «تمت المزامنة منذ ٠ ثواني» forever — every catalog/inventory/conflicts/movements/reports envelope stamped `as_of` with API request time instead of sync time. Fixed 2026-07-15: `as_of` = newest branch `last_sync_at` from the registry (`syncFreshness`/`tenantFreshness` in `api/internal/hq/service.go`), `source` degrades to `offline` past 30 min, omitted entirely for a never-synced tenant; console `CatalogEnvelope.as_of` now optional. Covered by `TestSyncFreshness` + updated envelope assertions. **Re-verify on the live tenant: pill should show the real last-sync age and advance after a sync.**
- [x] Manual e2e: sales report totals + tender split match the desktop's own numbers for a real synced tenant and period (incl. a deleted bill staying excluded and a multi-branch day) — confirmed 2026-07-15
- [x] Manual e2e: products report revenue/profit spot-checked against the desktop's profit screen for the same period (note the deliberate deleted-bill/date-anchor divergence); top-seller ordering sane in all three sorts — confirmed 2026-07-15
- [x] Manual e2e: staff report rows match per-cashier desktop numbers; branches comparison matches the per-branch bills screens — confirmed 2026-07-15
- [x] **Found during checkpoint testing, fixed 2026-07-15:** POS sale did not land in today's sales report live via SSE — the view only picked up the new sale after switching tabs or changing the period (i.e. on query remount), not from the `branch-synced` invalidation while mounted. Root cause fixed; live sale now appears in the sales report without a refresh, confirmed 2026-07-15.
- [x] RTL/Arabic-numerals audit across all five views (chart labels included) — completed 2026-07-15
- [x] **Human review before Phase 7 (Customers)** *(approved 2026-07-15; note: at approval time this was labeled "Phase 7 (Live tier)" — renumbered 2026-07-16 when Customers was inserted as slice 7 and Live tier/Loyalty moved to slices 8/9, see spec-console.md)*

## Phase 7 — Customers

Plan: `tasks/plan.md` §Phase 7 · Spec: `tasks/spec-console.md` §"Customers module (slice 7)"

Design notes (2026-07-16): scope decisions carried from the spec — branch-specific (no cross-branch identity), merge dropped to Future Features, loyalty promoted to its own Phase 9 follow-up spec. List/stats scope to `Customer.Type == CustomerType.Customer` (the table also holds `Supplier`/`All` rows — this phase is not the supplier ledger). Customer groups are Tier-A via the `Groups` TPH discriminator (`Kind="Customer"`, `AribContext.cs:206-209`) but need their own gateway query — the existing `GroupsAsync` filters `OfType<ProductGroup>()` only. **Balance/credit-limit is D10, same rule as `Accounts`:** every balance-derived read recomputes `SUM(CustomerTransaction.Debit − Credit)` server-side; `Customer.Debit/Credit/Balance` are never read directly. `CustomerTransaction.Balance` is itself unreliable — the desktop's own `AddNewCustomer` hardcodes it to `0` and `UpdateCustomer` never touches it — so the ledger view computes running balance server-side exactly like T29's movement running-qty (opening-balance seed strictly before the page + page-accumulated `Debit-Credit`, C# decimal, never the stored column). Purchase stats/history reuse the Reports slice's Bills semantics verbatim: `Type IN (Sale, ReSale)`, `!IsDeleted`, `CustomerId = id`. Create is a bounded write with **no opening balance in v1** (mirrors T26's product-create decision — no `CustomerTransaction`/`JournalEntry` rows written); `AccountId` wired via a single `AccountOperands["Customers"]` lookup (simpler than T26's per-kind mapping since `Type` is always `Customer` here); `Num` = tenant max+1, mirroring `CustomerService.GetCustomerNum`. Edit is a bounded partial-field-update write, mirroring T23's price-change shape. Import reuses the create path row-by-row over a **fixed** CSV template (name, phone1, branch_id, group_id?, credit_limit?) — not the desktop's dynamic column-mapping UI (`ImportCustomerViewModel`) — with per-row error reporting so one bad row doesn't abort the batch, matching the desktop's own validation-reporting shape.

- [x] **T48: Gateway — customer groups read**
  - **Description:** New `CustomerGroupsAsync(dbName)` in `HqApi.cs`, parallel to `GroupsAsync`: `db.Groups.AsNoTracking().OfType<CustomerGroup>().OrderBy(g => g.Num)`, mapped to a `CustomerGroupRow(Id, ParentId, Name, IsActive, Num)` record (no `ProductCount` — irrelevant here). Route `GET /hq/customer-groups`.
  - Acceptance:
    - [x] Only `Kind="Customer"` rows returned; `Kind="Product"` rows never leak in
    - [x] Empty/never-synced tenant → empty list, not an error (same `IsDatabaseMissing` catch as `GroupsAsync`)
  - Verify: `dotnet build AribSyncGateway.csproj`; curl against a dev tenant DB
  - Files: `sync-gateway/HqApi.cs`, `sync-gateway/Program.cs`
  - Dependencies: none · **Size: XS**

- [x] **T49: Gateway — paged customer list**
  - **Description:** `GET /hq/customers?search=&branch_id=&group_id=&active=&debt=&page=&page_size=`. Base: `db.Customers.Where(c => c.Type == CustomerType.Customer)`. Search matches name (`EF.Functions.Like`), any of `Phone1/2/3`, or `Num` (int-parsed, same convention as `ProductsAsync`'s code match). Balance recomputed per row via a correlated subquery over `CustomerTransactions` (`Σ Debit − Credit`, 0 when no rows) — **never `Customer.Balance`**. `debt` takes one of `has_debt` (recomputed balance > 0), `credit` (`IsCredit == true`), `exceeding` (recomputed balance > `CreditLimit` && `CreditLimit` > 0); omitted = no debt filter. Row: id, num, name, branch_id, group_id, group_name, phone1, is_active, balance (recomputed), credit_limit, is_credit, last_purchase_at (nullable, `MAX(Bills.IssuedAt)` for that customer).
  - Acceptance:
    - [x] Search matches name/phone/code; branch/group/active/`debt` filters compose with AND; `debt` values validated (unknown value → empty result, not a crash — API layer 400s it properly in T54)
    - [x] Balance in every row is the recomputed ledger sum, never the stored `Balance` column; `Supplier`/`All` type rows never appear; `page_size` clamped 1..200
  - Verify: `dotnet build AribSyncGateway.csproj`; curl each filter value against a dev tenant DB
  - Files: `sync-gateway/HqApi.cs`, `sync-gateway/Program.cs`
  - Dependencies: none · **Size: M**

- [x] **T50: Gateway — customer detail + stats**
  - **Description:** `GET /hq/customers/{id:guid}`. 404 on unknown id or an id whose `Type != Customer`. Returns basic info (name, phones, address, note, group, credit_limit, is_credit, is_active, num, branch_id) + recomputed balance (T49's subquery) + a stats block from `Bills` where `CustomerId = id`, `Type IN (Sale, ReSale)`, `!IsDeleted`: `number_of_orders` (count), `total_spent` (Σ Total), `average_order_value` (`total_spent / number_of_orders`, 0 when no orders), `last_purchase_date` (`MAX(IssuedAt)`, nullable). `total_purchases` in the note's wording is the same figure as `number_of_orders` — shipped as one field, documented as such rather than inventing a distinct sixth metric.
  - Acceptance:
    - [x] Stats match a manual `Σ`/`COUNT`/`MAX` over `Bills` for a synced tenant (human-verified 2026-07-16, folded into checkpoint 7)
    - [x] Unknown id → 404; an id belonging to a `Supplier` row → 404, not silently returned as a customer
  - Verify: `dotnet build AribSyncGateway.csproj`; curl against a dev tenant DB
  - Files: `sync-gateway/HqApi.cs`, `sync-gateway/Program.cs`
  - Dependencies: none · **Size: M**

- [x] **T51: Gateway — customer purchase history**
  - **Description:** `GET /hq/customers/{id:guid}/purchases?page=&page_size=`. 404 via T50's lookup. `db.Bills.Where(b => b.CustomerId == id && (b.Type == Sale || b.Type == ReSale) && !b.IsDeleted).OrderByDescending(b => b.IssuedAt)`, paged. Row: id, num, issued_at, total, item_count, is_paid, type.
  - Acceptance:
    - [x] Paging is stable and newest-first; deleted bills and non-sale types never appear
  - Verify: `dotnet build AribSyncGateway.csproj`; curl against a dev tenant DB
  - Files: `sync-gateway/HqApi.cs`, `sync-gateway/Program.cs`
  - Dependencies: T50 (shares the 404 lookup) · **Size: S**

- [x] **T52: Gateway — customer ledger with computed running balance**
  - **Description:** `GET /hq/customers/{id:guid}/ledger?page=&page_size=`. Mirrors T29's movement pagination construction exactly: rows ordered `CreatedAt` then `Id` ASC for determinism; opening-balance seed = `Σ(Debit−Credit)` over rows strictly before the page's first row; running balance accumulated in C# `decimal` per row of the returned page (page-N seed = opening + net of every skipped earlier row, so pages are self-contained, same proof T29 already established). Row: id, created_at, dealing, total, debit, credit, running_balance, note, user_id.
  - Acceptance:
    - [x] Page N's first `running_balance` = page N−1's last `running_balance` + that row's net (by construction, same seed/accumulator expression — verified by code inspection like T29)
    - [x] Every query is `CustomerId`-anchored (no unfiltered scan); unbounded period's final running balance matches the T49/T50 recomputed total
  - Verify: `dotnet build AribSyncGateway.csproj`; compare against a dev tenant DB
  - Files: `sync-gateway/HqApi.cs`, `sync-gateway/Program.cs`
  - Dependencies: T50 · **Size: M**

- [x] **T53: Gateway — customers insights**
  - **Description:** `GET /hq/customers/insights?branch_id=&period=`. One response, six blocks: `top_customers` (top N by recomputed total-spent over the selected period), `new_this_month` (`Customer.CreatedAt` in the current calendar month — count + list), `inactive` (`IsActive` && no `Sale`/`ReSale` bill in the last N days, N configurable, default 60), `credit_limit_warnings` (recomputed balance vs `CreditLimit`, `CreditLimit > 0`; two buckets — `approaching` ≥80% of limit, `exceeding` ≥100% — thresholds are a judgment call, documented inline in the code, not derived from any existing desktop rule since none exists), `highest_spenders` (top N by **lifetime** recomputed total-spent, unscoped by period — distinct from `top_customers`'s period scoping), `growth_over_time` (count of new customers per day/month over the period, local-date strings, same series shape as the Reports slice's day series).
  - Acceptance:
    - [x] Every block's numbers are internally consistent with T49/T50's recomputed-balance and Bills semantics (no separate, drifting aggregation logic)
    - [x] Empty/never-synced tenant → zeroed/empty shapes for every block, not an error
  - Verify: `dotnet build AribSyncGateway.csproj`; curl against a dev tenant DB
  - Files: `sync-gateway/HqApi.cs`, `sync-gateway/Program.cs`
  - Dependencies: T49, T50 (shares the recompute/aggregation helpers) · **Size: M**

- [x] **T54: API — customer read passthroughs + tests**
  - **Description:** Six `hq.Service` methods mirroring the reports/inventory chain (resolveGateway → getJSON → `{data, source:"synced", as_of}` envelope): `CustomerGroups` (passthrough), `Customers` (passthrough + branch_name/health decoration from the registry, same as catalog availability rows), `CustomerDetail` (passthrough + branch decoration, 404→`ErrNotFound`), `CustomerPurchases`, `CustomerLedger` (both passthrough, 404 via the same customer lookup), `CustomerInsights` (passthrough). Handlers whitelist query params — `active` boolean, `debt ∈ has_debt|credit|exceeding` (unknown value → 400, no gateway round-trip), `page`/`page_size` numeric. Routes: `GET /v1/tenants/{id}/hq/customer-groups`, `/hq/customers`, `/hq/customers/{cid}`, `/hq/customers/{cid}/purchases`, `/hq/customers/{cid}/ledger`, `/hq/customers/insights`.
  - Acceptance:
    - [x] Table-driven tests per method: envelope shape, params forwarded, branch decoration present, unknown `debt`/`active` value → 400 with zero gateway calls, 404 passes through cleanly for an unknown customer
    - [x] `go test ./...` green (full suite, not just the new tests)
  - Verify: `go build ./... && go vet ./... && go test ./...`
  - Files: `api/internal/hq/service.go` + `service_test.go`, `api/internal/httpapi/hq_handlers.go`, `api/internal/httpapi/server.go`
  - Dependencies: T48, T49, T50, T51, T52, T53 (contract; may start on fakes) · **Size: M**

- [x] **T55: Gateway — customer create**
  - **Description:** `POST /hq/customers`, body `{name, phone1, phone2?, phone3?, address?, note?, group_id?, credit_limit?, branch_id}`. `branch_id` validated against the tenant's known branches/warehouses (same existence check style as T26's product create). `AccountId` = `AccountOperands["Customers"]`'s `AccountId`; `FromId` = `AccountOperands["Capital"]`'s `AccountId` — both required, mirroring the desktop's `UpsertCustomerViewModel`'s default-account resolution exactly (it resolves both operands for a new customer, not just the ledger account); missing either operand → 500 with a clear message, mirroring the desktop's own defensive `App.RaiseException` check in `UpsertCustomerViewModel`/`CustomerService.AddNewCustomer`. `Num` = `db.Customers.Max(Num) + 1`, falling back to `1` on an empty table (`InvalidOperationException` catch, mirrors `CustomerService.GetCustomerNum` exactly). `Type = Customer`, `IsActive = true`, `Debit = Credit = Balance = OpenBalance = 0`, `IsDoubleType = false`, `CreatedAt = now`. **No opening balance, no `CustomerTransaction`/`JournalEntry` rows in v1** — explicit decision, matches T26's "no opening balance from HQ" for products. Returns `{id, written_at}`.
  - **Bug found during human review (2026-07-16), fixed same day:** `FromId` was initially left `Guid.Empty` instead of resolving `AccountOperands["Capital"]`. `Customer.FromId` is a non-nullable `Guid` (`AribONE.Data/Models/Entities/Customer.cs:40`), so `Guid.Empty` synced as a real stored value, not "unset" — the desktop's customer form showed "الحساب المكمل" (the FromId-bound field, `UpsertCustomerView.axaml:119-121`) empty/unresolved after sync. Fixed by adding the `Capital` operand lookup alongside `Customers`, both now required for a create to succeed.
  - Acceptance:
    - [x] Missing `name`/`phone1`/`branch_id` → 400; unknown `branch_id` → 400; `name` >100 chars or `phone1` >12 chars (entity `MaxLength`) → 400
    - [x] `Num` increments correctly on a non-empty table and starts at `1` on an empty one; created row is immediately visible via T49's list (self-check)
    - [x] `FromId` resolves to the `Capital` AccountOperand's account and appears correctly as "الحساب المكمل" on the desktop after sync *(fixed and human-verified against a real synced tenant, 2026-07-16)*
  - Verify: `dotnet build AribSyncGateway.csproj`; curl against a dev tenant DB (missing name, missing branch, valid create)
  - Files: `sync-gateway/HqApi.cs`, `sync-gateway/Program.cs`
  - Dependencies: none · **Size: M**

- [x] **T56: Gateway — customer edit/deactivate**
  - **Description:** `PUT /hq/customers/{id:guid}`, body `{name?, phone1?, phone2?, phone3?, address?, note?, group_id?, credit_limit?, is_active?}` — every field optional, only provided fields updated (partial update; unlike T23's `{changes:[...]}` per-unit collection, this is a flat partial object since a customer has no child rows to target). `AccountId`/`BranchId`/`Num` are never touched by this endpoint. "Deactivate" (the note's list-page bullet) is just `is_active:false` through this same endpoint — no separate route. Returns `{written_at}`.
  - Acceptance:
    - [x] Unknown id → 404; a two-call sequence (set `name`, then separately set `is_active`) leaves `name` unchanged by the second call — proves partial-update semantics
    - [x] Negative `credit_limit` → 400
  - Verify: `dotnet build AribSyncGateway.csproj`; curl against a dev tenant DB
  - Files: `sync-gateway/HqApi.cs`, `sync-gateway/Program.cs`
  - Dependencies: T55 · **Size: S**

- [x] **T57: API — create/edit passthroughs + tests**
  - **Description:** `hq.Service.CreateCustomer` + `UpdateCustomer`, same auth/ownership chain as T24/T26. Body validated before the gateway call (`name` non-empty ≤100 chars, `phone1` non-empty ≤12 chars, `credit_limit` ≥ 0 when present). Routes: `POST /v1/tenants/{id}/hq/customers`, `PUT /v1/tenants/{id}/hq/customers/{cid}`. Writes logged like other HQ writes (`hq.customers_create` / `hq.customers_update`: tenant, account, email — same request-log pattern as T24).
  - Acceptance:
    - [x] Table-driven: ownership enforced (`ErrForbidden` for a non-owning account); validation 400s fire before any gateway call; `written_at` round-trips; gateway 400/404 map cleanly to the existing error types
  - Verify: `go build ./... && go test ./...`
  - Files: `api/internal/hq/service.go` + `service_test.go`, `api/internal/httpapi/hq_handlers.go`, `api/internal/httpapi/server.go`
  - Dependencies: T55, T56 (contract; may start on fakes) · **Size: S**

- [x] **T58: Gateway — bulk group-assign + pricing-tier update**
  - **Description:** `PUT /hq/customers/bulk`, body `{ids: [...], group_id?, price_tier?}` (at least one of `group_id`/`price_tier` required, else 400). Every id validated to belong to the token's `db` and have `Type == Customer` before any write (matches T23's per-row "belongs to this product" 400 pattern) — invalid id anywhere in the batch aborts the whole write (single transaction, no partial application). `ids` capped at 500. Returns `{updated: count, written_at}`.
  - Acceptance:
    - [x] An unknown/foreign id anywhere in the batch → 400 with zero rows updated (transaction rollback verified by a follow-up read)
    - [x] Neither `group_id` nor `price_tier` present → 400
  - Verify: `dotnet build AribSyncGateway.csproj`; curl against a dev tenant DB
  - Files: `sync-gateway/HqApi.cs`, `sync-gateway/Program.cs`
  - Dependencies: T56 · **Size: S**

- [x] **T59: Gateway — CSV export + import**
  - **Description:** `GET /hq/customers/export?search=&branch_id=&group_id=&active=&debt=` — streams the same filtered rows as T49 (unpaged, capped at e.g. 5000 rows) as CSV with a UTF-8 BOM (Arabic-safe when opened directly in Excel); fixed columns: code, name, branch, group, phone1, address, credit_limit, balance, is_active. `POST /hq/customers/import` — multipart body: a CSV file (fixed columns name, phone1, group_id?, credit_limit?) plus a separate `branch_id` form field; reuses T55's create logic **row-by-row, one transaction per row** so a bad row doesn't abort the batch (mirrors the desktop's `ImportCustomerViewModel` per-row error collection, without reproducing its dynamic Excel column-mapping step). Returns `{created: count, errors: [{row, message}]}`. Row count capped at 1000 per import.
  - **Bug found during human review (2026-07-16), fixed same day:** the CSV originally carried its own `branch_id` column, but the console user importing a file has no way to know a branch's GUID — every row failed validation with an opaque "invalid branch_id" and the dialog only surfaced a bare `تم إنشاء ٠ عميل` count, with no indication *why*. Fixed two ways: (1) `branch_id` moved out of the CSV entirely into one `branch_id` multipart form field, applied to every row — the console now offers a real branch `<select>` (T65) instead of asking the user to type a GUID; a new `HqApi.BranchExistsAsync` fails the whole upload fast with one clear error if that branch doesn't exist, rather than repeating the same per-row error N times. (2) added explicit pre-validation ahead of `CreateCustomerAsync` for missing/oversized name or phone1, non-numeric `credit_limit`, and non-GUID `group_id`, each with a specific Arabic message (e.g. `حد الائتمان (credit_limit) يجب أن يكون رقمًا`) — previously a bad cell type was silently coerced to `null` (credit_limit/group_id) with no error reported at all.
  - Acceptance:
    - [x] Export → re-import on an empty tenant recreates equivalent rows (minus `balance`/`is_active`, which import doesn't set — those come from the ledger and default `true` respectively)
    - [x] A bad row (missing name, non-numeric credit_limit) reports a specific per-row Arabic error and the batch still completes for the remaining valid rows *(fixed and human-verified against a real synced tenant, 2026-07-16)*
    - [x] An unknown/missing `branch_id` form field fails the whole upload with one clear top-level error instead of a silent `created: 0`
  - Verify: `dotnet build AribSyncGateway.csproj`; curl export + import against a dev tenant DB
  - Files: `sync-gateway/HqApi.cs`, `sync-gateway/Program.cs`
  - Dependencies: T55 (reuses create) · **Size: M**

- [x] **T60: API — bulk/export/import passthroughs + tests**
  - **Description:** `hq.Service.BulkUpdateCustomers`, `ExportCustomers` (streams the gateway's CSV through with `Content-Type: text/csv` + `Content-Disposition: attachment`), `ImportCustomers` (forwards the multipart body, request size-limited). Routes: `PUT /v1/tenants/{id}/hq/customers/bulk`, `GET .../hq/customers/export`, `POST .../hq/customers/import`.
  - Acceptance:
    - [x] Table-driven tests for bulk (validation, gateway error map) and import (size limit, per-row error-list passthrough)
    - [x] Export content-type/headers verified by a live curl against the running API — streaming isn't a natural table-driven-test shape, same reasoning as T13's SSE endpoint
  - Verify: `go build ./... && go test ./...`; curl export against a real running API
  - Files: `api/internal/hq/service.go` + `service_test.go`, `api/internal/httpapi/hq_handlers.go`, `api/internal/httpapi/server.go`
  - Dependencies: T58, T59 (contract; may start on fakes) · **Size: M**

- [x] **T61: Console — lib plumbing**
  - **Description:** Types (`CustomerGroup`, `CustomerRow`, `CustomerDetail`, `CustomerStats`, `PurchaseRow`, `LedgerRow`, `InsightsResponse`, paged/envelope aliases matching T54/T57/T60's shapes), `api.ts` functions (`URLSearchParams` builders, Catalog style; import/export use `fetch` directly for multipart/blob handling rather than the JSON `getJSON` helper), `qk` keys under a shared `['hq-customers', tenantId, …]` prefix, hooks (list/detail/purchases/ledger/insights with `enabled: !!tenantId`, `keepPreviousData` on the paged ones; create/update/bulk mutations invalidating the prefix). `useTenantEvents` gains the `hq-customers` prefix so `branch-synced` flips customer data live, same mechanism as every prior slice.
  - Acceptance:
    - [x] `pnpm build` type-checks the contract against T54/T57/T60's shapes
    - [x] `branch-synced` SSE invalidates every `hq-customers` key via the shared prefix
  - Verify: `pnpm build && pnpm lint`
  - Files: `console/src/lib/{types,api,query,hooks}.ts`
  - Dependencies: T54, T57, T60 (contract; may start on fakes) · **Size: M**

- [x] **T62: Console — Customers list page + nav/route wiring**
  - **Description:** New `pages/console/Customers.tsx`: `PageHeader` + `Freshness`, debounced search box, branch/group/active/`debt` filter row (reusing the `<select>`/chip patterns already established in Catalog/Inventory), paged table (code, name, branch badge + `HealthDot`, group, phone, recomputed balance, credit_limit, status) with rows → `/customers/{id}`; create dialog (react-hook-form + zod, fields per T55's body). Nav entry «العملاء» + route added to `AppShell.tsx`/`App.tsx` — no placeholder existed from T8, since Customers was only added to the spec's IA on 2026-07-16.
  - Acceptance:
    - [x] Filter changes reset to page 1 without spinner-blanking (Catalog's `filterKey`/`lastFilterKey` render-time-reset pattern)
    - [x] Create dialog success navigates to the new customer's profile; nav entry present and RTL-correct
  - Verify: `pnpm build && pnpm lint`; manual click-through human-verified 2026-07-16 (folded into checkpoint 7)
  - Files: `console/src/pages/console/Customers.tsx` (new), `console/src/components/{AppShell,CreateCustomerDialog}.tsx`, `console/src/App.tsx`, `console/src/components/icon.tsx` (if a customers nav icon is needed)
  - Dependencies: T61 · **Size: M**

- [x] **T63: Console — Customer profile page**
  - **Description:** New `pages/console/CustomerDetail.tsx`, route `customers/:customerId` (breadcrumbs like `ProductDetail`/`BranchDetail`): header (name, branch badge, group, status, edit button → dialog reusing T62's form fields plus an `is_active` toggle for deactivate); stats tiles (number of orders, total spent, average order value, last purchase date); purchase history section (paged, T51's rows, bill detail expandable inline — no cross-branch Orders page to link out to, per the spec's branch-specific scope decision); ledger/credit-history section (paged, T52's rows with a `running_balance` column).
  - Acceptance:
    - [x] All stats render from T50's payload verbatim, no client-side arithmetic beyond what the gateway already computed
    - [x] Edit dialog partial-updates correctly (only changed fields sent); deactivate flips status without a page reload
  - Verify: `pnpm build && pnpm lint`; manual human-verified 2026-07-16 (folded into checkpoint 7)
  - Files: `console/src/pages/console/CustomerDetail.tsx` (new), `console/src/App.tsx` (route), `console/src/components/EditCustomerDialog.tsx` (new), `console/src/lib/hooks.ts`
  - Dependencies: T62 · **Size: M**

- [x] **T64: Console — Insights view**
  - **Description:** URL-state view toggle on `Customers.tsx` (`?view=list|insights`, mirroring Inventory/Reports' pattern) rendering T53's six blocks: top-customers/highest-spenders tables (rows → profile), new-this-month and inactive counts+lists, credit-limit warning list (approaching/exceeding, amber/red tone), growth-over-time chart reusing the Reports slice's CSS-bar pattern (T46) — no new chart dependency.
  - Acceptance:
    - [x] Every insight row deep-links to that customer's profile (spec rule: no alert/insight ships without a destination)
    - [x] Growth chart count matches a manual count of `Customer.CreatedAt` rows for a spot-checked period
  - Verify: `pnpm build && pnpm lint`; manual human-verified 2026-07-16 (folded into checkpoint 7)
  - Files: `console/src/pages/console/Customers.tsx`
  - Dependencies: T61 · **Size: M**

- [x] **T65: Console — Bulk operations UI**
  - **Description:** Multi-select checkboxes on the Customers list table (checks for an existing multi-select pattern in the codebase first; introduces a minimal one if none exists); toolbar appears when ≥1 row selected: «تعيين مجموعة» (group picker → T58), «تحديث فئة السعر» (price-tier input → T58), «تصدير» (calls T59's export with the current filter set, triggers a file download via blob response), «استيراد» (dialog: CSV template download link, file upload, and a required branch `<select>` (T59's `branch_id` form field) → T59's import, per-row error table on partial failure).
  - **Bug found during human review (2026-07-16), fixed same day:** `ImportCustomersDialog` originally had no branch picker — it relied on a `branch_id` CSV column the user had no way to fill in correctly. Added a required branch `<select>` (same `useBundle(tenantId).Branches` source as `CreateCustomerDialog`) under the file input, sent as a `branch_id` form field alongside the file (`useImportCustomers` and `api.importCustomers` signatures updated to take `{file, branchId}`); the CSV template dropped its `branch_id` column accordingly.
  - Acceptance:
    - [x] Bulk group/price-tier mutation reflects immediately in the list (query invalidation, no manual refresh)
    - [x] Export downloads a file honoring the current filters; import surfaces per-row errors without silently dropping failed rows
    - [x] Import is disabled until both a file and a branch are selected; the selected branch applies to every row *(fixed and human-verified against a real synced tenant, 2026-07-16)*
  - Verify: `pnpm build && pnpm lint`; manual human-verified 2026-07-16 (folded into checkpoint 7)
  - Files: `console/src/pages/console/Customers.tsx`, `console/src/components/{BulkActionsBar,ImportCustomersDialog}.tsx` (new)
  - Dependencies: T62 · **Size: M**

### Checkpoint 7
- [x] All gates green (api `go build ./... && go vet ./... && go test ./...`, gateway `dotnet build AribSyncGateway.csproj`, console `pnpm build && pnpm lint`) *(2026-07-16 — machine-verified end-to-end; every manual/e2e item below has since been human-verified against a real synced tenant, including the `FromId`/"الحساب المكمل" (T55) and CSV-import (T59/T65) bugs found and fixed during that pass.)*
- [x] Manual e2e: list/profile numbers (balance, stats, purchase history, ledger) match the desktop's own `CustomerView`/`CustomerStatementView` for a real synced tenant
- [x] Manual e2e: HQ create/edit reaches the desktop on its next sync round — this is **HQ's first write into a Tier-B table**; verify the `BranchId` filter routes the row to only the target branch, not every branch (unlike the Tier-A catalog writes from Phase 3)
- [x] Manual e2e: bulk group-assign/pricing-tier propagate the same way; export downloads a correct CSV honoring the active filters; import creates customers with correct `AccountId` wiring, verified usable in the desktop's own customer picker after sync
- [x] Debt/credit-limit filters and insights spot-checked against a manual ledger recomputation for a few real customers
- [x] RTL/Arabic-numerals audit across list/profile/insights
- [x] **Human review before Phase 9 (Live tier)** *(renumbered 2026-07-16 — see Phase 8 below)*

## Phase 8 — Suppliers

Plan: `tasks/plan.md` §Phase 8

Design notes (2026-07-16): ad hoc addition, not in the original spec note — requested directly by the user as a sibling to Phase 7's Customers module ("works like customers, nothing change"). `Customer` is a single TPH entity/table with a `Type` enum (`Customer`/`Supplier`/`All`, `AribONE.Data/Models/CustomerType.cs`); Phase 7 already filters everything by `Type == CustomerType.Customer`, so Suppliers is the identical read/write/import/export/insights logic filtered by `Type == CustomerType.Supplier`. Three schema realities drive the implementation and are **not** design choices: **(1) groups are not type-scoped** — `CustomerGroup` (`Kind="Customer"` TPH discriminator, `AribContext.cs:206-209`) is the one group table, already shared by customers and suppliers on the desktop (`desktop/ViewModels/Customers/CustomersViewModel.cs:33-36`) — Suppliers reuses T48's `/hq/customer-groups` gateway endpoint and the console's `useCustomerGroups`/`api.customerGroups`/`CustomerGroup` verbatim, no `SupplierGroup` type/endpoint/hook is created; **(2) the `Num` sequence is shared, not per-type** — T55's `CreateCustomerAsync` computes `nextNum` via `db.Customers.MaxAsync(c => c.Num) + 1` already unfiltered by `Type` (`HqApi.cs:1839`), matching the desktop's own `CustomerService.GetCustomerNum` and its uniqueness validation (also unfiltered) — supplier creation reuses this exact query, not a `Type`-scoped one; **(3) the GL account operand key for suppliers is `Vendor`, not `Suppliers`** — seeded operands are `Customers` (LabelAr "العملاء") and `Vendor` (LabelAr "الموردون"), `SeedData.cs:177-178`, confirmed against the desktop's `UpsertCustomerViewModel.cs:135-211` which resolves a supplier's ledger account from `AccountOperandName.Vendor`; `FromId` still resolves from `Capital` for both types, unchanged. On the gateway, T48-T60's Customer-scoped methods are **parameterized** with a `CustomerType type` argument rather than duplicated — the balance/credit-limit-recompute (D10) and running-ledger logic is complex enough that a second hand-copy would risk drifting on future bug fixes; `/hq/customers/*` routes now pass `CustomerType.Customer` explicitly (behavior-preserving), and a mirrored `/hq/suppliers/*` block passes `CustomerType.Supplier`. The Go API and console legs have no existing generic multi-resource abstraction, so those two legs mirror Phase 7's per-resource methods/handlers/components 1:1 rather than parameterizing. **Renumbering note:** this insertion pushes the previously-outlined Live tier from Phase 8 to Phase 9 and Loyalty from Phase 9 to Phase 10 — same pattern used when Customers was inserted as Phase 7 on 2026-07-16.

- [x] **T66: Gateway — parameterize Customer methods by `CustomerType`**
  - **Description:** Add a `CustomerType type` parameter to `CustomersAsync`, `CustomerAsync`, `CustomerPurchasesAsync` (+ helper `CustomerSales`), `CustomerLedgerAsync`, `CustomerInsightsAsync`, `CreateCustomerAsync`, `UpdateCustomerAsync`, `BulkUpdateCustomersAsync`, `CustomerExportAsync` in `HqApi.cs` — every internal `CustomerType.Customer` literal becomes the `type` parameter. Add two mapping helpers: `BillTypesFor(CustomerType type)` → `(Sale, ReSale)` for `Customer` or `(Purchase, RePurchase)` for `Supplier` (used by `CustomerSales`/`CustomerPurchasesAsync`/`CustomerInsightsAsync`), and `AccountOperandKeyFor(CustomerType type)` → `"Customers"` or `"Vendor"` (used by `CreateCustomerAsync`). `CreateCustomerAsync` sets `Type = type` (not hardcoded `Customer`) and keeps the `Num` sequence globally unfiltered (finding #2 above). `CustomerGroupsAsync` and `BranchExistsAsync` are untouched (finding #1 above — not type-scoped).
  - Acceptance:
    - [ ] Every existing `/hq/customers/*` caller behaves byte-identically after the refactor (regression, not just no-compile-errors) — *(not yet re-verified against a real synced tenant DB; no minted HQ token available in this session — the license-server component that mints them isn't checked out here)*
    - [x] `BillTypesFor(Supplier)` returns `Purchase`/`RePurchase`, never touches `Sale`/`ReSale` rows
    - [x] `AccountOperandKeyFor(Supplier)` resolves `Vendor`, never a nonexistent `"Suppliers"` key
    - [x] Supplier `Num` values interleave with Customer `Num` values from the same unfiltered counter (no separate per-type sequence)
  - Verify: `dotnet build AribSyncGateway.csproj` — 0 warnings/errors (2026-07-16)
  - Files: `sync-gateway/HqApi.cs`
  - Dependencies: none · **Size: L**

- [x] **T67: Gateway — `/hq/suppliers/*` routes**
  - **Description:** In `Program.cs`, both Customer and Supplier route sets are registered via one parameterized local function, `MapCustomerTypeRoutes(CustomerType type, string prefix, string notFoundError, string accountOperandLabel)`, called once per type — `MapCustomerTypeRoutes(CustomerType.Customer, "customers", "customer not found", "Customers")` and `MapCustomerTypeRoutes(CustomerType.Supplier, "suppliers", "supplier not found", "Vendor")` — rather than hand-duplicating ~250 lines of route wiring a second time. Group-read reuses `/hq/customer-groups` (no new route). Preserves the same static-before-wildcard registration order (`bulk`, `export`, `import`, `insights` before `{id:guid}`) as before. CSV import handler for suppliers reuses the same per-row validation and `BranchExistsAsync`, threading `CustomerType.Supplier` into `CreateCustomerAsync`. The four English "customer not found" messages and the "missing the Customers AccountOperand mapping" message are now type-aware via the function's parameters; Arabic strings in the import path are already generic and reused verbatim.
  - Acceptance:
    - [x] All 10 `/hq/suppliers/*` routes registered and reachable; static segments never get captured by the `{id:guid}` wildcard — verified: gateway restarted with the new binary, every route (list/detail/purchases/ledger/insights/create/edit/bulk/export/import, both prefixes) returns 401 "missing bearer token" (not 404), `GET .../bulk` correctly 405s (PUT-only) — no route-conflict startup exception either
    - [ ] Supplier CSV import: missing branch, bad row types, and the branch-not-found case all return the same specific per-row Arabic errors as Customers — *(not yet re-verified against a real synced tenant DB, same HQ-token limitation as T66)*
    - [ ] `Supplier`/`All`-type rows never leak into `/hq/customers/*` responses and vice versa — *(correct by the `c.Type == type` filter in every parameterized HqApi.cs method; not yet re-verified against a real synced tenant DB)*
  - Verify: `dotnet build AribSyncGateway.csproj` — 0 warnings/errors; gateway restarted (`run-postgress.sh`) and route-table smoke-tested via curl (2026-07-16)
  - Files: `sync-gateway/Program.cs`
  - Dependencies: T66 · **Size: M**

- [x] **T68: API — supplier passthroughs**
  - **Description:** Mirror T54/T57/T60 for Suppliers in `platform/api/internal/hq/`: `service.go` gets `Suppliers`, `SupplierDetail`, `SupplierPurchases`, `SupplierLedger`, `SupplierInsights`, `CreateSupplier`, `UpdateSupplier`, `BulkUpdateSuppliers`, `ExportSuppliers`, `ImportSuppliers`, each following the existing 4-step pattern (`resolveGateway` → build URL → `getJSON`/`putJSON`/raw request → freshness envelope / branch decoration) against `/hq/suppliers...`. New DTO structs mirror the Customer ones field-for-field (`SupplierInsights.TopSuppliers` keeps the gateway's `top_customers` JSON key, since `/hq/suppliers/insights` shares one handler function with `/hq/customers/insights` on the gateway — cosmetic wire-name only, not a bug). `hq_handlers.go` gets mirrored handlers with the same Go-side validation (name ≤100, phone1 ≤12, branch_id required, credit_limit ≥0) and structured logging (`hq.suppliers_create` etc.); `server.go` registers the `/hq/suppliers...` routes with the same static-before-wildcard ordering. `CustomerGroups`, `InvalidCustomerInputError`, and `ErrMissingAccountOperand` are reused as-is (all structurally generic, no Supplier-specific variant needed).
  - Acceptance:
    - [x] Every supplier endpoint round-trips through a real gateway call in a table-driven test, mirroring the existing Customer test shapes in `service_test.go`/`hq_customers_handlers_test.go` — new `service_suppliers_test.go` (list/detail/create ×2/update/bulk/import) and `hq_suppliers_handlers_test.go` (zero-gateway-calls 400 paths for every handler), all passing
    - [x] Bulk update caps at the same `maxBulkCustomerIDs`-equivalent bound (`maxBulkSupplierIDs = 500`); import caps at the same `maxImportBytes` bound (reused directly, generic constant)
  - Verify: `go build ./... && go vet ./... && go test ./...` — clean, all packages pass (2026-07-16)
  - Files: `api/internal/hq/service.go`, `api/internal/hq/service_suppliers_test.go` (new), `api/internal/httpapi/hq_handlers.go`, `api/internal/httpapi/hq_suppliers_handlers_test.go` (new), `api/internal/httpapi/server.go`
  - Dependencies: T67 · **Size: L**

- [x] **T69: Console — lib plumbing**
  - **Description:** Mirror T61 for Suppliers: `lib/types.ts` gets `SupplierRow`, `SuppliersPage`, `SupplierDetail`, `SupplierPurchaseRow`, `SupplierLedgerRow`, `SupplierInsights`, `NewSupplierInput`, `SupplierEditInput`, `BulkUpdateSuppliersInput`, `ImportSuppliersResult`, etc. (field-for-field mirror of the Customer types block); `lib/api.ts` gets `suppliers`, `supplier`, `supplierPurchases`, `supplierLedger`, `supplierInsights`, `createSupplier`, `updateSupplier`, `bulkUpdateSuppliers`, `exportSuppliers`, `importSuppliers` hitting `/v1/tenants/{id}/hq/suppliers...`; `lib/query.ts` gets a `hq-suppliers` key-prefix block mirroring the customer one; `lib/hooks.ts` gets the mirrored hook set and wires `hq-suppliers` into the same SSE tenant-events invalidation block as `hq-customers`. `useBundle` and `useCustomerGroups`/`api.customerGroups`/`CustomerGroup` are reused unchanged — no `SupplierGroup` stack.
  - Acceptance:
    - [ ] A POS-side supplier ledger write reflects in the console via SSE, no manual refresh, same as customers — *(not yet verified against a real synced tenant; needs a live POS sync round)*
    - [x] Group `<select>` data in supplier dialogs comes from the exact same `useCustomerGroups` call already used by customer dialogs — confirmed by code: `CreateSupplierDialog`/`EditSupplierDialog`/`Suppliers.tsx`/`SupplierBulkActionsBar` all import `useCustomerGroups` directly, no `useSupplierGroups` exists
  - Verify: `npx tsc --noEmit`; `npx eslint src/lib/{types,api,query,hooks}.ts` — clean (2026-07-16)
  - Files: `console/src/lib/{types,api,query,hooks}.ts`
  - Dependencies: T68 · **Size: M**

- [x] **T70: Console — Suppliers list + profile + insights**
  - **Description:** Mirror T62-T64: new `pages/console/Suppliers.tsx` (`ListView`+`InsightsView`, same internal `VIEWS` sub-tab pattern as Customers, Arabic labels "قائمة الموردين"/"رؤى وتحليلات"); new `pages/console/SupplierDetail.tsx` (header/stats/credit card, purchases section, ledger section) mirroring `CustomerDetail.tsx`, with the purchase-history `BILL_TYPE_LABEL` map swapped to `{200: 'شراء', 201: 'مرتجع شراء'}` (Purchase/RePurchase) instead of Customers' Sale/ReSale, matching `BillTypesFor` on the gateway.
  - Acceptance:
    - [ ] Search/filters/table/pagination behave identically to Customers, scoped to suppliers — *(code mirrors Customers.tsx exactly; not yet click-through-verified in a browser — no browser automation tool available in this session)*
    - [ ] Every insight row deep-links to that supplier's profile — *(code correct — `Link to={`/tenants/${tenantId}/suppliers/${r.id}`}`; not yet click-through-verified)*
    - [ ] Profile stats/ledger/purchase-history numbers match a manual recomputation for a spot-checked supplier — *(needs a real synced tenant with supplier data; not yet verified)*
  - Verify: `npx tsc --noEmit`; `npx eslint`; `pnpm build` — all clean (2026-07-16). Manual click-through pending — see Checkpoint 8.
  - Files: `console/src/pages/console/{Suppliers,SupplierDetail}.tsx` (new)
  - Dependencies: T69 · **Size: L**

- [x] **T71: Console — Create/Edit/Import dialogs + bulk UI + nav wiring**
  - **Description:** Mirror T65 (including the CSV-import branch-`<select>` UX from the Phase 7 bugfix, not the older CSV-`branch_id`-column shape): new `CreateSupplierDialog.tsx`, `EditSupplierDialog.tsx`, `ImportSuppliersDialog.tsx`, `SupplierBulkActionsBar.tsx` (duplicated from `BulkActionsBar.tsx` rather than genericized — it's tightly coupled to `useBulkUpdateCustomers` + "عميل" strings, not worth destabilizing a working component for). Route wiring in `App.tsx` (`suppliers`, `suppliers/:supplierId`, right after the customer routes) and a nav entry in `AppShell.tsx` right after "العملاء" (`{ to: `${base}/suppliers`, label: 'الموردون', icon: SupplierIcon }`), with a new `SupplierIcon` (Solar's `Delivery` glyph) added to `icon.tsx` rather than reusing `UsersIcon` a second time.
  - Acceptance:
    - [x] Import is disabled until both a file and a branch are selected; per-row Arabic error table on partial failure (missing field, type mismatch) — same UX as Customers' fixed bug — *(2026-07-16, human-verified)*
    - [x] Bulk group/price-tier mutation reflects immediately in the list; export honors active filters — *(2026-07-16, human-verified)*
    - [x] "الموردون" nav tab sits beside "العملاء" in both desktop sidebar and mobile nav, with its own distinguishable icon — confirmed by code: single `nav` array in `AppShell.tsx` drives both, `SupplierIcon` (`Delivery`) is a distinct glyph from `UsersIcon` (`UsersGroupRounded`)
  - Verify: `npx tsc --noEmit`; `npx eslint`; `pnpm build && pnpm lint` — all clean (2026-07-16). Manual click-through of create/edit/bulk/import/export pending — see Checkpoint 8.
  - Files: `console/src/components/{CreateSupplierDialog,EditSupplierDialog,ImportSuppliersDialog,SupplierBulkActionsBar}.tsx` (new), `console/src/{App,components/AppShell,components/icon}.tsx`
  - Dependencies: T70 · **Size: M**

### Checkpoint 8
- [x] All gates green (api `go build ./... && go vet ./... && go test ./...`, gateway `dotnet build AribSyncGateway.csproj`, console `pnpm build && pnpm lint`) *(2026-07-16, machine-verified end-to-end; gateway and API dev processes both restarted onto the new binaries and route-table-smoke-tested — see T67.)*
- [x] Manual regression: Customers list/profile/create/edit/bulk/import/export/insights all unchanged after the T66 parameterization *(2026-07-16, human-verified)*
- [x] Manual e2e: Suppliers list/profile/create/edit/bulk/import/export/insights match the Customers UX exactly, verified against a real synced tenant *(2026-07-16, human-verified; found and fixed a ledger transaction-type label bug — "نوع 200" — commit 49db2aa)*
- [x] RTL/Arabic-numerals audit on the new Suppliers views *(2026-07-16, human-verified)*
- [x] Human review before Phase 9 (Live tier) *(2026-07-16)*

## Phase 9 — Live tier (SignalR)

> **Deferred (user decision 2026-07-17):** Phase 9 ships after publish, based on client reaction. Phase 10 (Billing, `tasks/spec-billing.md`) executes first — it removes the manual-provisioning and no-billing publish blockers.

Plan: `tasks/plan.md` §Phase 9 · Spec: `tasks/spec-console.md` §Realtime chain + slice 8

Scope (user decision 2026-07-16): **presence + sync-now nudge + manual per-branch nudge button; live queries deferred to Phase 9b.** Envelopes stay `synced`/`offline`; `source:"live"` stays reserved. First phase to touch the `desktop` repo.

**Contracts every task codes against:**
- Hub at `{gateway_url}/hub`; auth = **sync token** (richest claims: tenant_id/branch_id/db_name/device_id), read from `Authorization: Bearer` else `access_token` query string, validated manually in `OnConnectedAsync` (gateway has no auth middleware); same shard-mismatch rejection as `/sync`. Groups: `t:{db_name}`, `b:{db_name}:{branch_id}` (branch id lowercase `"D"` format; db_name is fleet-unique so groups can't collide).
- Server→client message `"sync-now"` with one string arg `reason` ∈ {`"write"`, `"manual"`}. Desktop stagger: `write` → random 0–15 s, `manual` → random 0–3 s.
- Timings: SignalR defaults (15 s keepalive / 30 s client timeout) + **45 s** offline grace (env `PRESENCE_GRACE_SECONDS`) → worst-case dead-connection→offline ≈ 75 s. After-write nudge coalesce **2.5 s** per db_name; manual nudges immediate.
- Gateway→API callback: `POST /v1/internal/branch-presence`, Bearer = stored branch sync token, body `{"online": bool, "device_count": int}` (identity from token claims, mirroring `/v1/internal/sync-completed`).
- Snapshot: `GET /admin/presence` (OpsToken) → `{"branches":[{"tenant_id","db_name","branch_id","device_count","connected_since"}]}` — only currently-connected branches.
- SSE wire: `hq.Event` grows `Online *bool json:"online,omitempty"` → `event: branch-presence` / `data: {"type":"branch-presence","branch_id":…,"at":…,"online":true}`. `handleTenantEvents` unchanged (it already writes `event: <Type>`).
- Three-valued presence invariant: **no store entry = unknown** (never connected since API start — old desktop). `Set(online:false)` for a nonexistent entry is a no-op, so old-version branches can never render "offline". `BranchView` gains `online *bool,omitempty`; `healthTier`/`syncFreshness`/envelopes untouched.
- Failure modes (decided): expired stored token at offline callback → 401 logged, reconcile flips ≤ ~60 s; API down during callback → lost, reconcile repopulates; gateway restart → tracker empty, desktops auto-reconnect, reconcile marks stragglers offline once `/admin/presence` answers; API restart → store empty (all unknown), startup reconcile repopulates within seconds; a shard that fails to answer the poll is **skipped for mark-offline** (never mass-flip on an API⇄gateway blip); SSE drop → existing console reconnect path.
- Test strategy: gateway = `dotnet build` + checkpoint e2e (no test infra; keep `PresenceTracker`/`SyncNudger` plain classes with injectable clock/notifier so tests can come later); api = table-driven tests incl. fake gateway (`httptest.Server`); console = build/lint; desktop = `dotnet build` + checkpoint e2e.
- Rollout order: **gateway → API → console → desktop last** (presence unknown everywhere until desktops upgrade = zero behavior change).

- [ ] **T72: Gateway — `BranchHub` + connection auth + groups**
  - **Description:** `builder.Services.AddSignalR()` in the services block (`Program.cs:59-74`, before `Build()` at `:76`); register a `BranchHubAuth` singleton record `(RSA PublicKey, string? OwnShardId)` from the existing `publicKey` (`:39-40`) and `SHARD_ID` (`:53`); `app.MapHub<BranchHub>("/hub")` beside the other route maps. New `BranchHub.cs`: `OnConnectedAsync` pulls the token (header else `access_token` query), `SyncToken.TryValidate`, rejects (`Context.Abort()`) on invalid/expired/shard-mismatch **before joining any group**; stashes `SyncTokenClaims` + raw token in `Context.Items`; joins `t:{db}` + `b:{db}:{branch}`; calls `tracker.OnConnected(claims, ConnectionId, rawToken)`. `OnDisconnectedAsync` → `tracker.OnDisconnected(ConnectionId)`. `PresenceTracker` may start as a two-method stub (fleshed out in T73) so this builds standalone. No new PackageReference — SignalR ships in the `Sdk.Web` shared framework.
  - Acceptance:
    - [ ] Missing/garbage/expired/shard-mismatched token → connection aborted, no group joined
    - [ ] Valid connection joins exactly `t:{db}` and `b:{db}:{branch}`; claims + raw token in `Context.Items`
    - [ ] `AribSyncGateway.csproj` diff shows zero new packages
  - Verify: `dotnet build AribSyncGateway.csproj`
  - Files: `sync-gateway/BranchHub.cs` (new), `sync-gateway/Program.cs`
  - Dependencies: none · **Size: S**

- [ ] **T73: Gateway — `PresenceTracker` + grace + callback + `/admin/presence`**
  - **Description:** New `Presence.cs`. `PresenceNotifier(licenseApiUrl, HttpClient)` — clone of `SyncCompletedNotifier` (`SyncActivity.cs:60-83`): fire-and-forget `POST {LICENSE_API_URL}/v1/internal/branch-presence`, Bearer = stored token, body `{"online","device_count"}`, log-only on failure. `PresenceTracker(notifier, offlineGrace)` — `ConcurrentDictionary<(string db, Guid branch), Entry>` where `Entry { HashSet<string> Connections; string LastToken; string TenantId; DateTimeOffset ConnectedSince; CancellationTokenSource? OfflineTimer }`: `OnConnected` cancels any pending offline timer, refreshes `LastToken`, adds the connection; 0→1 → notify `online:true` immediately. `OnDisconnected` removes; →0 → start 45 s timer (`PRESENCE_GRACE_SECONDS`, default 45), cancelled by reconnect, else remove entry + notify `online:false`. `RefreshToken(claims, token)` called with one line from the `/sync` path (`OnSessionEnd` interceptor area, `Program.cs:183-201`) so every 5-min round keeps the stored token fresh — minimizes expired-token 401s on the offline callback. `Snapshot()` for T73's endpoint. Wire in Program.cs (`AddSingleton` for hub DI); map `GET /admin/presence` beside the other `/admin/*` routes: `TryOpsAuth` (`:105-111`) → `{branches:[…]}` snake_case.
  - Acceptance:
    - [ ] First connection per branch → exactly one immediate `online:true`; N seats never re-fire it
    - [ ] Last disconnect → callback only after grace; reconnect within grace cancels (no offline ever sent)
    - [ ] `/admin/presence` 401s without ops token; lists only connected branches with correct device counts
    - [ ] Token refreshed on reconnects and `/sync` rounds; callback failure logged, never thrown
  - Verify: `dotnet build AribSyncGateway.csproj`
  - Files: `sync-gateway/Presence.cs` (new), `sync-gateway/Program.cs`, `sync-gateway/BranchHub.cs`
  - Dependencies: T72 · **Size: M**

- [ ] **T74: Gateway — `SyncNudger` + `POST /hq/nudge` + after-write hooks**
  - **Description:** New `SyncNudger.cs` built after `app.Build()` from `app.Services.GetRequiredService<IHubContext<BranchHub>>()`. `NudgeTenant(dbName)`: per-db_name coalesce — `ConcurrentDictionary<string,byte>` pending; `TryAdd` → `Task.Run(Delay 2.5 s; TryRemove; Clients.Group($"t:{db}").SendAsync("sync-now","write"))`; duplicates inside the window are no-ops. `NudgeBranch(dbName, branchId)`: immediate `Clients.Group($"b:{db}:{branch}").SendAsync("sync-now","manual")`. Both fire-and-forget, log-only. Hook `NudgeTenant` into the six HQ write success sites (each already holds `dbName` from `TryHqAuth`): `PUT /hq/products/{id}/prices` (~`Program.cs:690`), `POST /hq/products` (~`:737`), and inside `MapCustomerTypeRoutes` (closure sees the nudger — covers customers **and** suppliers via the two invocations at `:1671-1672`): create/edit/bulk/import success branches. Map `POST /hq/nudge`: `TryHqAuth` → body `{"branch_id"}` (400 on bad) → `NudgeBranch` → `200 {"nudged":true,"devices":tracker.DeviceCount(db,branch)}` (0 devices is not an error).
  - Acceptance:
    - [ ] All six write success sites nudge; error/4xx branches never do; nudge failure can never fail the write response
    - [ ] Two writes within 2.5 s → one tenant-group broadcast
    - [ ] `/hq/nudge`: 401 without HqToken, 400 on bad body, targets only the one branch group, returns device count
  - Verify: `dotnet build AribSyncGateway.csproj`
  - Files: `sync-gateway/SyncNudger.cs` (new), `sync-gateway/Program.cs`
  - Dependencies: T72, T73 · **Size: S**

- [ ] **T75: API — `PresenceStore` + `Event.Online` + internal `branch-presence` handler**
  - **Description:** `events.go`: add `Online *bool json:"online,omitempty"` to `Event` (`:12-16`). New `hq/presence.go`: `PresenceStore` (mutex map `tenantID → branchID → {Online, Since, DeviceCount}`) with `Set(...) (changed bool)` — changed = created-online or flag flipped; **`Set(online:false)` for a nonexistent entry creates nothing and reports unchanged** (the old-version invariant); `Lookup`, `All`. Same single-instance caveat comment as `EventBus`. `hq.New` gains the store param (nil tolerated — `Branches()` skips decoration); update `main.go` + existing constructor calls in tests. `tenant.Service`: extract the branch-ownership check from `RecordSyncCompleted` (`service.go:537-550`) into `VerifyBranch(ctx, tenantID, branchID)` and reuse from both. Route `POST /internal/branch-presence` beside `/internal/sync-completed` (`server.go:122`); handler mirrors `handleInternalSyncCompleted` (`tenant_handlers.go:81-99`): Bearer → `VerifySyncToken` → decode body → `VerifyBranch` → `hq.RecordPresence(...)` (thin wrapper on `presence.Set` with `time.Now().UTC()`) → if changed, `events.Publish(tenantID, Event{Type:"branch-presence", BranchID, At, Online:&online})` → `200 {"status":"recorded"}`.
  - Acceptance:
    - [ ] Table-driven `presence_test.go`: unknown→online = changed; online→online = unchanged; online→offline = changed; **offline-for-unknown = no-op**; device count updates
    - [ ] Handler tests: bad token → 401; other tenant's branch → error; happy path stores + publishes
    - [ ] `branch-synced` events carry no `online` key (omitempty verified)
  - Verify: `go build ./... && go vet ./... && go test ./...`
  - Files: `api/internal/hq/{events,presence,presence_test}.go`, `api/internal/hq/service.go`, `api/internal/tenant/service.go`, `api/internal/httpapi/{server,tenant_handlers}.go`, `api/cmd/api/main.go`
  - Dependencies: none (contract-first; T73 targets this endpoint) · **Size: M**

- [ ] **T76: API — presence reconcile poller**
  - **Description:** New `hq/reconcile.go`: `PresenceReconciler` (deps: shard lister, ops-token issuer — reuse the rollout service's mint, store, event bus, `*http.Client`, logger) with `Run(ctx, interval)`: one reconcile immediately, then `time.NewTicker(60 s)` until ctx cancels — **the API's first background job** (plain goroutine, consistent with the bus's single-instance caveat). One cycle: list active shards; per shard, ops-token `GET {GatewayURL}/admin/presence` (~10 s timeout, rollout client pattern); union entries. Diff as a **pure function** `diffPresence(current, snapshot, allShardsOK) []change`: snapshot entries absent-or-offline in store → online; store-online entries absent from the union → offline **only if every shard answered** (an unreachable shard must never mass-flip its branches). Apply via `Store.Set`, publish `branch-presence` per actual change (snapshot carries tenant_id — no registry lookup). Wiring: `httpapi.Server` gets a one-line `Events() *hq.EventBus` accessor; `main.go` starts `go reconciler.Run(ctx, time.Minute)` with shutdown-cancelled ctx.
  - Acceptance:
    - [ ] `diffPresence` table-driven: fresh store+snapshot → all online; store-online absent → offline iff `allShardsOK`; converged → no-op; never fabricates offline for never-seen branches
    - [ ] Fake-gateway (`httptest.Server`) test: startup run populates + publishes; identical second run publishes nothing; ops-token auth header asserted
    - [ ] Unreachable gateway → logged, cycle continues with remaining shards; graceful shutdown stops the goroutine
  - Verify: `go build ./... && go vet ./... && go test ./...`
  - Files: `api/internal/hq/{reconcile,reconcile_test}.go`, `api/internal/httpapi/server.go`, `api/cmd/api/main.go`
  - Dependencies: T75 · **Size: M**

- [ ] **T77: API — `BranchView.online` decoration**
  - **Description:** `BranchView` (`hq/service.go:176-187`) gains `Online *bool json:"online,omitempty"`; `Branches()` (`:267`) decorates from `presence.Lookup` when the store is non-nil. `healthTier`, `syncFreshness`, envelopes, `BranchActivity`, `InventoryBranchView` all untouched. Doc comment states the three-valued semantics (nil = never connected / old app; display-only, never a freshness source).
  - Acceptance:
    - [ ] Table-driven: store-online → `"online":true`; store-offline → `false`; no entry → key **absent**; `health` identical in all three cases
  - Verify: `go build ./... && go vet ./... && go test ./...`
  - Files: `api/internal/hq/{service,service_test}.go`
  - Dependencies: T75 · **Size: S**

- [ ] **T78: API — manual nudge passthrough**
  - **Description:** `hq.Service.NudgeBranch(ctx, accountID, tenantID, branchID)`: `resolveGateway` (`:108`) → branch ownership via `store.BranchesByTenant` (`ErrNotFound` otherwise) → HQ-token `POST {gateway}/hq/nudge` body `{"branch_id"}` (inline-POST pattern of `CreateProduct`, no generic refactor) → decode `{"nudged","devices"}`. Route `POST /hq/branches/{branchId}/nudge` in the HQ group; handler = standard session-auth + `writeHqError` shape.
  - Acceptance:
    - [ ] Tests: non-owned tenant → forbidden; unknown branch → not found; gateway down → 503; happy path forwards branch_id and returns device count (fake gateway asserts HqToken + body)
  - Verify: `go build ./... && go vet ./... && go test ./...`
  - Files: `api/internal/hq/service.go` + tests, `api/internal/httpapi/{hq_handlers,server}.go`
  - Dependencies: T74 (contract; buildable on fakes) · **Size: S**

- [ ] **T79: Console — presence types + SSE listener + «متصل» badge + propagation copy**
  - **Description:** `types.ts`: `online?: boolean` on `BranchView`. `hooks.ts` `useTenantEvents` (`:247`): add `es.addEventListener('branch-presence', …)` invalidating **only** `qk.hqBranches` + `qk.branchActivity` (presence rides `BranchView` — a connect/disconnect must not refetch inventory/reports). New `components/PresenceBadge.tsx`: pulsing green dot + «متصل», renders `null` unless `online === true` (reuse `Freshness.tsx`'s live-dot styling `:25-35`). Render on `Branches.tsx` cards (beside `HealthDot`), `BranchDetail.tsx` header, and `BranchStatusIndicator.tsx` popover rows (worst-tier reduce untouched). `PropagationPanel.tsx` (`:31`): pending copy becomes «في الانتظار — يصل خلال ثوانٍ» when `b.online === true`, else the existing «~٥ دقائق» copy.
  - Acceptance:
    - [ ] `online` absent/false → pixel-identical rendering to today (old-desktop degradation)
    - [ ] `branch-presence` SSE flips the badge without refresh; `branch-synced` behavior unchanged
    - [ ] Seconds-copy only for online branches
  - Verify: `pnpm build && pnpm lint`
  - Files: `console/src/lib/{types,hooks}.ts`, `console/src/components/{PresenceBadge (new),PropagationPanel,BranchStatusIndicator}.tsx`, `console/src/pages/console/{Branches,BranchDetail}.tsx`
  - Dependencies: T75, T77 (wire shape) · **Size: S**

- [ ] **T80: Console — manual «مزامنة الآن» button**
  - **Description:** `api.ts`: `nudgeBranch(tenantId, branchId)` → `POST /v1/tenants/{id}/hq/branches/{branchId}/nudge`. `hooks.ts`: `useNudgeBranch` mutation — **no invalidation needed** (the resulting sync round emits `branch-synced`, which already invalidates everything). `BranchDetail.tsx` sync-activity section: «مزامنة الآن» button, enabled only when `online === true`; disabled tooltip «الفرع غير متصل — سيُزامن تلقائيًا خلال ~٥ دقائق»; success toast «تم إرسال طلب المزامنة»; brief pending state. Optionally icon-only on `Branches.tsx` cards if it doesn't crowd them.
  - Acceptance:
    - [ ] Disabled with tooltip when `online !== true` (every old-desktop branch)
    - [ ] Click → 200 → `last_sync_at` flips within seconds via existing `branch-synced` SSE
    - [ ] API error surfaces as toast, no crash
  - Verify: `pnpm build && pnpm lint`
  - Files: `console/src/lib/{api,hooks}.ts`, `console/src/pages/console/BranchDetail.tsx` (+optionally `Branches.tsx`)
  - Dependencies: T78, T79 · **Size: S**

- [ ] **T81: Desktop — SignalR client + `LiveLinkService`** ⚠ new dependency — **ask first before landing**
  - **Description:** `AribONE.csproj`: add `Microsoft.AspNetCore.SignalR.Client` (the phase's only new package; gateway needs none). `SyncService.cs`: expose `internal Task<SyncTokenResult?> MintSyncTokenAsync()` delegating to the private `GetTokenAsync` (`:318-325`) so LiveLink shares the in-memory token cache (never double-mints). New `Services/Sync/LiveLinkService.cs` — static-singleton `ObservableObject` (house pattern, `SyncService.cs:34`): `[ObservableProperty] bool _isConnected`; idempotent `Start()` no-op unless `SyncService.Instance.IsEnabled`. Supervisor loop: mint token result → `HubConnection` via `WithUrl($"{result.GatewayUrl}/hub", AccessTokenProvider = fresh mint)` + custom **infinite** `IRetryPolicy` (`min(60 s, 5 s × attempt)` + jitter — SignalR's default gives up after 4 tries). Handler registered before `StartAsync`: `conn.On<string>("sync-now", reason => { if (UpdateRequired) return; await Delay(random 0–15 s for "write" / 0–3 s for "manual"); trigger the SyncNow path; })` — the `_gate.WaitAsync(0)` no-op (`:208`) absorbs overlaps; 5-min timer untouched. `Closed` handler: wait 30–60 s, rebuild from a fresh token result (picks up a changed `gateway_url` after a shard move); loop re-checks `IsEnabled` so entitlement loss tears down. Start from `App.axaml.cs` `EnterLauncher` (`:744`) beside `SyncService.Instance.Start()`. UI: extend the existing launcher sync icon tooltip (`LauncherView.axaml:97-116`) with «متصل مباشرة» via `x:Static` binding — no new surface.
  - Acceptance:
    - [ ] Entitled app start → hub connected within seconds; gateway lists the branch online
    - [ ] `sync-now("write")` → round within ≤15 s; `("manual")` ≤3 s; mid-round nudge = no-op; 5-min timer still fires
    - [ ] Gateway kill → infinite backoff reconnect; `IsConnected` truthful; no UI-thread blocking; long-lived connection outliving the ~1 h token reconnects with a fresh mint
    - [ ] Not entitled / `UpdateRequired` → no connection attempts / nudges ignored
  - Verify: `dotnet build AribONE.csproj`
  - Files: `desktop/AribONE.csproj`, `desktop/Services/Sync/LiveLinkService.cs` (new), `desktop/Services/Sync/SyncService.cs`, `desktop/App.axaml.cs`, `desktop/Views/LauncherView.axaml`
  - Dependencies: T72–T74 (a live gateway to test against; buildable on contract alone) · **Size: M**

### Checkpoint 9
Deploy order first: **gateway → API → console** with zero upgraded desktops (must render exactly as Phase 8 — no `online` key anywhere), desktop last.
- [ ] All gates green (api `go build ./... && go vet ./... && go test ./...`, gateway `dotnet build AribSyncGateway.csproj`, console `pnpm build && pnpm lint`, desktop `dotnet build AribONE.csproj`)
- [ ] Old-version invariant: a branch on the pre-Phase-9 desktop shows normal health tiers, no «متصل» badge, never "offline", nudge button disabled with tooltip; `/admin/presence` never lists it
- [ ] Basic presence: upgraded desktop start → badge within ~2 s (SSE); `/admin/presence` lists device_count 1
- [ ] Flap debounce: network cut <45 s → no offline ever reaches the console; cut >45 s → badge drops (~75 s worst case) and returns on reconnect; gateway logs exactly one offline/online pair
- [ ] Multi-seat: N seats → device_count N, one «متصل»; closing N−1 seats emits nothing; last close → offline after grace
- [ ] After-write nudge: HQ price change → online branch's chip shows «يصل خلال ثوانٍ», flips «وصل ✓» within ~20 s (2.5 s coalesce + ≤15 s stagger + round); rapid successive writes → one broadcast (gateway log); offline/old branches keep the ~5-min path
- [ ] Manual nudge: console button → that POS's sync icon animates within ~3 s, `last_sync_at` flips via `branch-synced`; nudge during a running round → no error, no second round
- [ ] Gateway restart: desktop auto-reconnects (watch backoff); no stuck-online branch after ~2 min
- [ ] API restart: badges repopulate within seconds of startup (first reconcile run); console SSE reconnects on its own
- [ ] Expired-token offline callback: branch connected >1 h with sync stopped, then killed → gateway logs a 401 presence callback; console still flips offline ≤ ~60 s (reconcile)
- [ ] Transport check: production `gateway_url`'s TLS terminator passes the WebSocket `Upgrade` for `/hub` (SignalR falls back to SSE/long-poll if not — presence still works; note and fix infra at leisure)
- [ ] RTL/Arabic-numerals audit (badge, button, toasts, tooltips)
- [ ] **Human review before Phase 10**

## Phase 10 — Billing (executes before Phase 9 — see deferral note above)

Plan: `tasks/plan-billing.md` · Spec: `tasks/spec-billing.md` (approved 2026-07-17)

Decisions locked with owner (2026-07-17): backfill bills for existing tenants (no grandfather path) · enforcement = refuse `IssueSyncToken` only, never auto-suspend · bills are amount+period, no plan catalog, `Tenant.Plan` stays unused · warnings in console **and** desktop POS (via sync-token response). Derived-state constants: warn = 30 d before `ends_at`, grace = 7 d after; state ∈ none/active/expiring/grace/expired = f(paid bills, now) — never stored.

- [x] **T82: Bill model + mongo store**
  - **Description:** `Bill` + `BillStatus` (`paid`|`void`) in `model.go` per spec §Data Model (minor-units `Amount`, `Currency`, period, `VoidReason`, `CreatedBy`, `Source`/`ExternalRef` seam — mirror the `License` field comments). New `store/mongo/bills.go`: `InsertBill`, `BillByID`, `BillsByTenant` (newest first), `VoidBill(id, reason, at)` (paid→void only). Index `{tenant_id: 1, ends_at: -1}` added in `EnsureIndexes` (`store.go:79`), collection wired in `store.go:62`'s collection block.
  - Acceptance:
    - [ ] Void of an already-void bill errors; bills are never deleted
    - [ ] `BillsByTenant` returns newest-first and includes void bills
  - Verify: `cd api && make test` — store test beside `registry_test.go`
  - Files: `api/internal/model/model.go`, `api/internal/store/mongo/bills.go` (new), `api/internal/store/mongo/store.go`, store test
  - Dependencies: none · **Size: S**

- [x] **T83: `billing.Derive` — pure subscription-state function**
  - **Description:** New package `api/internal/billing`: `State` (none/active/expiring/grace/expired), `Summary{State, EndsAt, GraceUntil, DaysLeft}`, `Derive(bills, now)`. Coverage end = max `ends_at` over **paid** bills; `warnBefore = 30*24h`, `graceAfter = 7*24h` as package constants. Table-driven tests on the boundaries: exactly end−30d, exactly end, end+7d, end+7d+1s, no bills, only-void bills, overlapping bills, future-dated bill (early renewal extends coverage).
  - Acceptance:
    - [ ] All boundary rows pass; voided covering bill downgrades state
    - [ ] Function is pure (no store/context dependency)
  - Verify: `cd api && make test`
  - Files: `api/internal/billing/billing.go` (new), `api/internal/billing/billing_test.go` (new)
  - Dependencies: T82 · **Size: S**

- [x] **T84: billing service — create/void/list + auto-provision**
  - **Description:** `api/internal/billing/service.go`: `Create` (validate `amount > 0`, `ends_at > starts_at`, currency defaults `"EGP"`; insert `paid`; if tenant `DBName == ""` call a small `provisioner` interface satisfied by `tenant.Service.ProvisionSync` — provision failure does **not** roll back the bill, response carries `provisioned bool` + detail), `Void(id, actor, reason)`, `ListWithSummary(tenantID)` (bills + `Derive` output). Audit via existing `InsertAudit` (`store/mongo/auth.go:126`): actions `bill.create` / `bill.void` with amount/period/reason meta.
  - Acceptance:
    - [ ] Bill on unprovisioned tenant provisions it; on provisioned tenant provisioning is skipped
    - [ ] Provision failure still persists the bill and reports `provisioned: false`
    - [ ] Every create/void writes an audit row
  - Verify: `cd api && make test` — table-driven service test
  - Files: `api/internal/billing/service.go` (new) + test, `api/internal/tenant/service.go` (only if the provisioner seam needs a method tweak)
  - Dependencies: T82, T83 · **Size: M**

- [x] **T85: admin bill endpoints**
  - **Description:** `POST /v1/admin/tenants/{id}/bills` (create, body per spec §API), `GET /v1/admin/tenants/{id}/bills` (list + summary), `POST /v1/admin/bills/{id}/void` (`{reason}` required). Wire beside `handleAdminProvisionSync` (`server.go` admin block, `tenant_handlers.go:289`); same admin auth. Amounts cross the wire in **minor units**; handler does no currency math.
  - Acceptance:
    - [ ] Validation errors → 400 with message; unknown tenant/bill → 404; void without reason → 400
    - [ ] Create response includes the bill, `provisioned` flag, and fresh summary
  - Verify: `cd api && make test` — handler tests in the existing httpapi test style
  - Files: `api/internal/httpapi/billing_handlers.go` (new), `api/internal/httpapi/server.go`
  - Dependencies: T84 · **Size: S**

- [x] **T86: sync-token gate + client subscription endpoint + desktop seam**
  - **Description:** `IssueSyncToken` (`tenant/service.go:404`) loads the tenant's bills, derives state, and returns new sentinel `ErrSubscriptionExpired` when `expired`/`none` (beside the `ErrTenantSuspended` check). `handleSyncToken` (`tenant_handlers.go:265`) maps it to **403** `{"code":"subscription_expired"}` and, on success, adds `"subscription": {"state", "ends_at"}` to the response (additive — old desktops ignore it). New client route `GET /v1/tenants/{id}/subscription` (ownership check via `owned`, **not** `activeTenant` — a suspended tenant may still read billing) returning `{state, ends_at, grace_until, days_left, bills:[…]}`.
  - Acceptance:
    - [ ] `expired` and no-bills tenants get 403 `subscription_expired`; `grace` still gets tokens + subscription payload
    - [ ] Subscription endpoint works for suspended tenants, 403s for non-owners
  - Verify: `cd api && make test`
  - Files: `api/internal/tenant/service.go` + `service_test.go`, `api/internal/httpapi/tenant_handlers.go`, `api/internal/httpapi/server.go`
  - Dependencies: T83 · **Size: M**

- [x] **Checkpoint 10a — API complete** *(verified 2026-07-17 against an isolated throwaway Mongo — not the compose `platform_mongo_data` volume)*
  - [x] `cd api && make test` green
  - [x] Local curl E2E: unprovisioned tenant → sync-token 402 (`ErrNotSubscribed`, unchanged) → create bill (auto-provisions) → sync-token 200 with `subscription.state=active` → void the bill → sync-token 403 `subscription_expired`
  - [ ] Human review before UI legs

- [x] **T87: admin UI — bills on ClientDetail**
  - **Description:** `adminApi` + types for the three endpoints; per-tenant section in `ClientDetail.tsx` (beside the existing provision button, which stays as ops fallback): state chip (active/expiring/grace/expired/none), bills table (amount rendered from minor units, period, status, created-by), **Add bill** dialog — amount input in EGP major units converted once at the API call, suggested `starts_at` = current coverage end (or today), suggested period 1 year — void action with required reason prompt, toast on `provisioned: false` pointing at the manual button.
  - Acceptance:
    - [ ] Recording a bill on an unprovisioned tenant shows the provision result without a manual click
    - [ ] Void updates the state chip in place (query invalidation)
  - Verify: `cd admin && pnpm build && pnpm lint`; manual pass against local API
  - Files: `admin/src/lib/api.ts`, `admin/src/lib/types.ts`, `admin/src/pages/ClientDetail.tsx`
  - Dependencies: T85 · **Size: M**

- [x] **T88: console — subscription hook + real billing page**
  - **Description:** `api.subscription` + `useSubscription(tenantId)` in `console/src/lib`; new `pages/console/Billing.tsx` replacing the `/billing` placeholder route in `App.tsx`: state card (نشط حتى… / ينتهي خلال… / فترة سماح / منتهي / لا يوجد اشتراك), bill history table (Arabic digits + `money` formatter, period, status), and a "طريقة الدفع" instructions card (placeholder copy clearly marked — owner supplies content, plan-billing.md open question 2). Note: `/billing` is account-level in the route tree but the API is tenant-scoped — page needs tenant selection when the account has >1 tenant (reuse the tenant-picker pattern from the console shell) or move the route under `/tenants/:tenantId/billing`; prefer the latter, keeping a redirect on the old path.
  - Acceptance:
    - [ ] All five states render correct Arabic copy; bill rows match admin-entered data
    - [ ] RTL + Arabic numerals audit on the page
  - Verify: `cd console && pnpm build && pnpm lint`; manual against local API with dates tweaked per state
  - Files: `console/src/lib/api.ts`, `console/src/lib/hooks.ts`, `console/src/pages/console/Billing.tsx` (new), `console/src/App.tsx`
  - Dependencies: T86 · **Size: M**

- [x] **T89: console — Overview banner/card + alert bell**
  - **Description:** `Overview.tsx`: delete the `!t.Plan` banner (line 67) and the «الباقة» dead-field card (line 200); replace with subscription-state banner (only when state ≠ active: expiring = info, grace/expired = danger, wording per spec) and an «الاشتراك» card (state + end date), both from `useSubscription`. `deriveAlerts` (`lib/alerts.ts`) gains a subscription input producing one alert row while expiring/grace/expired deep-linking to the billing page; both callers (`Overview.tsx`, `NotificationsBell.tsx`) pass it.
  - Acceptance:
    - [ ] Provisioned+paid tenant shows no «بدون اشتراك» anywhere (retires the 2026-07-16 dead-field bug)
    - [ ] Bell and Overview alerts panel show the identical subscription row (shared `deriveAlerts`)
  - Verify: `cd console && pnpm build && pnpm lint`; manual pass across states
  - Files: `console/src/pages/console/Overview.tsx`, `console/src/lib/alerts.ts`, `console/src/components/NotificationsBell.tsx`, `console/src/lib/hooks.ts`
  - Dependencies: T88 · **Size: S**

- [x] **Checkpoint 10b — consoles** *(desktop leg T90 also done — see note below)*
  - [x] Both consoles build + lint clean
  - [x] Manual: bill created in admin UI appears in tenant console within one refetch; five states verified by tweaking bill dates in Mongo *(human-verified 2026-07-17)*
  - [x] Human review before desktop leg *(approved 2026-07-17)*

- [x] **T90: desktop — subscription warning + paused-sync state**
  - **Description:** (desktop repo) `SyncTokenResult` gains nullable `Subscription(State, EndsAt)` (`LicenseApiClient.cs:415`); `SyncService` (`Services/Sync/SyncService.cs`): on token response with state expiring/grace, raise a dismissible in-app warning at most once per 7 days (persist last-shown timestamp in the app's existing local settings store); on 403 with code `subscription_expired`, set a distinct "المزامنة متوقفة — يرجى تجديد الاشتراك" sync status instead of the generic failure, retry cadence unchanged. Local selling never blocked.
  - Acceptance:
    - [ ] Warning shows at most weekly; paused state shows the renewal message and clears on the first successful token after a new bill
    - [ ] Old-version invariant: pre-T90 desktops keep working (field ignored; 403 = ordinary failed round)
  - Verify: `cd ../desktop && dotnet build AribONE.csproj`; manual round against local API in grace + expired states
  - Files: `desktop/Services/LicenseApiClient.cs`, `desktop/Services/Sync/SyncService.cs`, warning UI surface + settings persistence (~2 more files)
  - Dependencies: T86 · **Size: M**

- [x] **Checkpoint 10c — E2E + deploy order (spec §Success Criteria)**
  - [x] Full flow on scratch tenant (`test`, tnt_3UGU4ADISUQMK5QDREALH3EU4Q): bill → auto-provision → desktop syncs → dates shifted to expiring (warning toast, correct day count, weekly gate confirmed) → grace (sync still works, correct renewal-date toast) → expired (403, distinct paused status "المزامنة متوقفة — يرجى تجديد الاشتراك", local selling unaffected, retry cadence unchanged by code — `UpdateRequired` never set on this path) → new bill via admin UI → sync resumes *(human-verified 2026-07-17, live desktop against dev stack; console banner at the expiring state confirmed live — grace/expired console views share the same `useSubscription` render path, not independently re-clicked at each state)*
  - [ ] Deploy order: API + admin UI first → **backfill real tenants' bills immediately** (inside the ~1 h token TTL, before their cached tokens expire) → console → desktop release last
  - [x] Audit log shows every bill action with actor *(confirmed during checkpoint 10a's E2E — `bill.create`/`bill.void` rows written with actor on every call)*
  - [x] **Human review — Phase 10 complete; revisit Phase 9 scheduling** *(approved 2026-07-17)*

## Phase 11 — Catalog group drill-down

Plan: `tasks/plan-catalog-groups.md` · Spec: `tasks/spec-catalog-groups.md` (2026-08-10)

Interaction locked with owner (2026-08-10): group with children = one click drills **and** filters · leaf = filters only, level unchanged · **no "رجوع" row** (rejected as redundant) — header 📁 icon returns to root *and* clears the filter, any crumb jumps to that level, collapsed `…` is clickable · breadcrumb lives inside the sidebar top · drill state is component-only (no URL). Motion budget: 14px slide + crossfade, 200ms/130ms, `cubic-bezier(0.22, 1, 0.36, 1)` (same easing as `.animate-rise`), no stagger/spring/scale, no new dependency.

- [~] **T91: `GroupDrill` — one-level column + breadcrumb header (no animation)** *(code complete; `pnpm build && pnpm lint` clean 2026-08-10 — drill-through on real data pending, folds into checkpoint 11a)*
  - **Description:** New `console/src/components/GroupDrill.tsx`. Move `buildGroupTree`, `GroupNode`, and `ROOT_PARENT` out of `Catalog.tsx` (file-local today, not exported). Component owns `path: GroupNode[]` internally — the level is *not* derivable from `groupId`, since a leaf click filters without changing level — and reports selection through `onSelect(id | undefined)`. Renders: a sticky header (`📁` root button + crumb buttons; at root the header reads `📁 كل الأصناف` as a single non-interactive crumb — **no separate "كل الأصناف" list row**) over one level's rows, reusing the existing row markup verbatim (`GroupIcon`, `truncate` name, `toArabicDigits(product_count)` badge, selected state). Level swaps instantly at this stage. `Catalog.tsx` swaps the `<aside>` body and deletes `GroupTree`; skeleton, 402 path, gateway-error path, `filterKey` page-reset, and `?search=` deep link all untouched.
  - Acceptance:
    - [~] Column shows exactly one level, no indentation; group-with-children click drills **and** filters, leaf click only filters *(implemented; needs a human click-through — no browser-automation tool in the session that built it)*
    - [~] Header icon returns to root *and* clears the filter; each crumb jumps to that level and filters by it *(same)*
    - [x] Zero-groups tenant renders header + empty list without crashing *(`level.length === 0` → "لا توجد مجموعات"; no crash path)*
  - Verify: `pnpm build && pnpm lint` *(clean 2026-08-10; 2 pre-existing `auth.tsx` react-refresh warnings only)*; manual in `pnpm dev` on a tenant with a ≥3-level hierarchy — drill down and back up every level, confirm the table's rows change at each step
  - Files: `console/src/components/GroupDrill.tsx` (new), `console/src/pages/console/Catalog.tsx`
  - Dependencies: none · **Size: M**

- [~] **T92: Breadcrumb collapse — `…` ancestor dropdown** *(code complete; `pnpm build && pnpm lint` clean 2026-08-10 — deep-path click-through pending, folds into checkpoint 11a)*
  - **Description:** When depth > 2, render `[📁] › … › parent › current`, collapsing every middle ancestor into a `…` trigger using the existing `ui/dropdown-menu` (same pattern as `AccountMenu.tsx`). Picking an entry behaves exactly like clicking that crumb. **Spec amendment (plan §Architecture):** collapse is depth-based, not width-measured — no ResizeObserver, no measure/re-measure oscillation; long names are handled by `truncate` + `title` on each crumb.
  - Acceptance:
    - [x] `…` appears only at depth > 2 and lists exactly the hidden ancestors, root-first *(`hidden = path.slice(0, len - 2)` — non-empty only past depth 2, already root-first)*
    - [~] Header never wraps and never overflows the 240px column, even with long Arabic group names *(icon/`…`/separators are `shrink-0`, crumb spans `min-w-0` + `truncate` so only names shrink — needs a look with real long names)*
  - Verify: `pnpm build && pnpm lint` *(clean 2026-08-10)*; manual — drill 4+ levels deep, open `…`, jump to a hidden ancestor
  - Files: `console/src/components/GroupDrill.tsx`
  - Dependencies: T91 · **Size: S**

- [ ] **Checkpoint 11a — behavior before motion**
  - [ ] `pnpm build && pnpm lint` clean
  - [ ] Manual: full drill/back navigation matches the interaction table in spec §Interaction contract
  - [ ] Regression: `/tenants/:id/catalog?search=…` from the command palette still prefills search and lands at root
  - [ ] **Human review** — sidebar structure approved, and a look at real tenant data to settle open Q1 (parent rows showing `٠` because `product_count` is direct-only) before motion work starts

- [~] **T93: Drill animation — slide + crossfade** *(code complete; `pnpm build && pnpm lint` clean 2026-08-10 — the animation itself is subjective and unverifiable without eyes, folds into checkpoint 11b)*
  - **Description:** `@keyframes` in `console/src/index.css` beside `.animate-rise`, reusing its `cubic-bezier(0.22, 1, 0.36, 1)`: 14px directional slide + opacity, 200ms transform / 130ms opacity. RTL sign comes from one named `DRILL_SHIFT` constant fed in as a CSS custom property (`translateX` does not flip with `dir`; the app is `dir="rtl"` app-wide). Both panes stay mounted for the transition — outgoing absolutely positioned inside an `overflow-hidden` viewport — so the old level never vanishes abruptly. Transitions are keyed on a monotonic `navSeq` (not the group id): two navigations can land on the same id, and a stale cleanup timer must not clear a newer transition. Drilling in = incoming enters from the inline-start edge; going up = mirrored. Leaf clicks do not animate.
  - Acceptance:
    - [~] Drill in and up animate in visibly opposite, correct directions; no stagger/spring/scale; nothing animates on page load *(four keyframes, signs written for RTL; animation classes apply only while a transition is live, so first paint is static — needs eyes)*
    - [x] Spam-clicking two groups alternately leaves no ghost or stuck pane (in-flight transition snaps to its end state, new one starts clean) *(a new navigation replaces `outgoing`, unmounting the previous pane; the retire timer is `seq`-guarded so a stale one can't retire a newer transition)*
  - Verify: `pnpm build && pnpm lint` *(clean 2026-08-10)*; manual — spam-click both directions, then step through one level at a time watching the pane edges
  - **Spec correction made here:** the spec's "incoming enters from the inline-start (left) edge" was self-contradictory — inline-start is the *right* edge in RTL. Implemented as literal left-on-drill-in (the mirror of the LTR/Inkdrop convention, which is what "(left)" meant); spec §Animation contract amended.
  - Files: `console/src/components/GroupDrill.tsx`, `console/src/index.css`
  - Dependencies: T91 · **Size: M**

- [~] **T94: Column height smoothing** *(droppable — see plan §Risks)* *(code complete; `pnpm build && pnpm lint` clean 2026-08-10 — needs eyes, folds into checkpoint 11b)*
  - **Description:** Transition the viewport's height between levels so the products table beside it doesn't jump when a short level follows a long one: freeze the current px height, reflow, set the incoming level's measured height, release to `auto` when the transition ends (same `navSeq` cleanup as T93). Decide `max-h` + internal scroll here, against real data, only if a tenant level actually exceeds the viewport (spec open Q2). If this fights the layout, revert **this task only** — T93 stands alone.
  - Acceptance:
    - [~] No visible jump in the products area when levels of very different lengths swap *(px→px height run over `--drill-ms`; outgoing pane pinned to its captured height so it isn't squashed by the shrinking viewport — needs eyes)*
    - [x] Column settles at natural height after the transition (no frozen or clipped height on resize) *(the retire timer clears the inline height back to `auto`, same `seq`-guarded callback that unmounts the outgoing pane)*
  - Verify: `pnpm build && pnpm lint` *(clean 2026-08-10)*; manual — drill between a 12-group level and a 2-group level, then resize the window mid-idle
  - **Open Q2 (column `max-h` + internal scroll) left unresolved deliberately:** it was scoped to "only if a real tenant level exceeds the viewport", and a tall level is *not* a regression here — the old fully-expanded tree was strictly taller. Decide at checkpoint 11b with real data on screen.
  - Files: `console/src/components/GroupDrill.tsx`
  - Dependencies: T93 · **Size: S**

- [~] **T95: Accessibility — reduced motion, `Backspace`, focus** *(code complete; `pnpm build && pnpm lint` clean 2026-08-10 — keyboard/reduced-motion pass pending, folds into checkpoint 11c)*
  - **Description:** `prefers-reduced-motion: reduce` drops transform and height animation entirely (opacity crossfade ≤80ms may remain). `Backspace` with focus inside the column goes up one level — handler bails when the event target is an input/textarea and calls `preventDefault` so it never triggers browser back-navigation. Keyboard-activated drills (detectable via `e.detail === 0` on click) move focus into the new level; pointer clicks leave focus alone so the page doesn't scroll under the cursor.
  - Acceptance:
    - [~] With OS "reduce motion" on, levels swap instantly with nothing broken or clipped *(media query drops the transform keyframes to an 80ms opacity-only crossfade and kills the height transition; the retire timer was already time-based, not `animationend`-based, so nothing hangs — needs a look)*
    - [~] `Backspace` goes up a level from the column and never navigates the browser back; typing in the search box is unaffected *(handler bails on INPUT/TEXTAREA/SELECT/contentEditable and `preventDefault`s otherwise; the search box is outside this `<aside>` and never reaches the handler at all)*
    - [~] Tab/Enter through the column works and focus is never lost after a keyboard drill *(keyboard activation detected via `e.detail === 0`; focus moves to the new level's first row only then, so pointer clicks don't scroll the page)*
  - Verify: `pnpm build && pnpm lint` *(clean 2026-08-10)*; manual — toggle macOS Reduce Motion, then a keyboard-only pass through drill/back/`…` dropdown
  - Files: `console/src/components/GroupDrill.tsx`, `console/src/index.css`
  - Dependencies: T93 (T94 if kept) · **Size: S**

- [ ] **Checkpoint 11b — motion feel**
  - [ ] `pnpm build && pnpm lint` clean
  - [ ] **Human review of the animation itself** — subjective and the whole point of the task: is it Inkdrop-restrained, or over-animated? Duration/distance are cheap to tune here (200ms/14px are starting values, not commitments)

- [ ] **Checkpoint 11c — Phase 11 complete**
  - [ ] Full manual checklist in spec §Testing Strategy passes on a ≥3-level hierarchy
  - [ ] No regression: search, pagination, `?search=` deep link, 402 "no subscription" state, gateway-error state
  - [ ] `package.json` unchanged — no animation library added
  - [ ] Spec open Q1 (direct-only parent counts) and Q2 (column max-height) resolved or explicitly carried forward
  - [ ] **Human review — Phase 11 complete**

## Phase 12 — New Order: inline customer create + address prefill

Spec: `tasks/spec-order-customer.md` · Plan: `tasks/plan-order-customer.md`.
Console-only: no API/gateway change, no new dependency, `lib/{types,api,query,hooks}.ts` untouched.

- [~] **T96: Mode segmented control + توصيل default + address in `saveBlockedReason`** *(code complete; `pnpm build && pnpm lint` clean 2026-08-24 — manual spec-steps 1–4 pass pending)*
  - **Description:** Replace `OrderCart.tsx`'s single on/off «توصيل» button with a two-option segmented control («استلام من الفرع» / «توصيل») under a «طريقة الاستلام» label, both states always visible and labelled. `NewOrder.tsx`'s `mode` initial state becomes `ORDER_MODE.Delivery` (user decision — HQ call-centre orders are deliveries; pickup is the deliberate exception), so the address + fee panel is open from first render. Move the missing-address check out of `save()`'s toast into the `saveBlockedReason` chain after branch/customer/cart; `save()` keeps a plain early return without the toast. Ownership is unchanged — `NewOrder` still owns `mode`, `OrderCart` still renders it.
  - Acceptance:
    - [x] A fresh order opens with «توصيل» selected and the address + delivery-fee panel expanded *(`mode` initial state is `ORDER_MODE.Delivery`; `OrderCart`'s panel renders on `isDelivery`)*
    - [x] «استلام من الفرع» collapses the panel; the saved request body has **no** `contact_address`, and no address is required *(unchanged `contact_address: isDelivery ? … : undefined` gate; `saveBlockedReason`'s address branch is also gated on `isDelivery`)*
    - [x] An address typed in delivery mode survives a pickup → delivery round-trip (kept in local state, not cleared) *(no code path clears `contactAddress` on a mode switch)*
    - [x] Delivery mode with an empty address disables the save button with «عنوان التوصيل مطلوب» — no toast-on-click path remains *(moved into `saveBlockedReason`; `save()`'s guard is a plain early return, `toast.error` call removed)*
    - [x] The mode default is a fixed constant — identical for a customer with a profile address and one without (never inferred from the customer record) *(`useState(ORDER_MODE.Delivery)` has no dependency on `customer`)*
  - Verify: `pnpm build && pnpm lint` *(clean 2026-08-24)*; manual — spec §Testing Strategy steps 1–4 *(pending — no browser tooling available this session)*
  - Files: `console/src/components/orders/OrderCart.tsx`, `console/src/pages/console/NewOrder.tsx`
  - Dependencies: none · **Size: S**

- [~] **T97: Branch-scoped customer picker + `SelectedCustomer.branchId` + cross-branch block** *(code complete; `pnpm build && pnpm lint` clean 2026-08-24 — manual spec-steps 5–6, 17–18 pass pending)*
  - **Description:** Pass `branchId` into the picker's `useCustomers` call so it lists only the order branch's customers (user decision). With no branch selected, the popover shows «اختر الفرع أولًا لعرض عملائه» instead of an unscoped list. `SelectedCustomer` gains `branchId`, captured from the `CustomerRow` at pick time (`types.ts:696` already carries it — no extra request). If a branch switch strands the selected customer, keep them selected but show a warning badge «العميل مسجّل في فرع آخر» beside the picker and add «اختر عميلًا من هذا الفرع» to `saveBlockedReason` (spec OQ1: no cross-branch orders — the console refuses before the request rather than relying on the gateway's 400).
  - Acceptance:
    - [x] With a branch selected, the popover lists only that branch's customers; the search box still filters within it *(`useCustomers` now takes `branchId`, which is part of the `qk.customers` key, so switching branches refetches)*
    - [x] With no branch selected, the popover shows the hint and no customer list *(popover renders the hint paragraph instead of the search+list block when `!branchId`; query itself is disabled)*
    - [x] Switching to a branch the selected customer doesn't belong to shows the badge and blocks save with the stated reason — the cart, mode, note, and customer selection all survive *(`onBranchChange` only touches `branchId`/URL; `customerBranchMismatch` is derived, not stored)*
    - [x] Switching back to the customer's own branch clears the badge and re-enables save *(mismatch is recomputed every render from `customer.branchId !== branchId`)*
    - [x] Picking a replacement customer from the rescoped picker clears the badge *(the picker is already scoped to the current branch, so any pick sets `customer.branchId === branchId`)*
  - Verify: `pnpm build && pnpm lint` *(clean 2026-08-24)*; manual — spec steps 5–6, 17–18 *(pending — no browser tooling available this session)*
  - Files: `console/src/pages/console/NewOrder.tsx`
  - Dependencies: none (sequence after T96 — same file) · **Size: S**

- [~] **T98: Delivery address auto-fill from the customer's profile** *(code complete; `pnpm build && pnpm lint` clean 2026-08-24 — manual spec-steps 11–14, 16 pass pending)*
  - **Description:** Mirror T3b's delivery-fee auto/manual contract for the address: `useCustomer(tenantId, customer?.id)` enabled only while a customer is selected **and** mode is Delivery; `addressAuto` re-armed by a `contextKey` of the customer id; `resolvedAddress` read off the query result; `displayedAddress` derived at render — never copied into state. Any manual edit sets `addressAuto = false` for good. `OrderCart` gains `contactAddressHint?: string`, rendered under the address input exactly like `deliveryFeeHint`, showing «عنوان العميل» while the value is still the resolved one.
  - Acceptance:
    - [x] Selecting a customer with a profile address fills the field and shows the «عنوان العميل» hint *(`displayedAddress`/`contactAddressHint` resolve from `customerQuery.data?.data.address` once loaded)*
    - [x] Editing the field clears the hint, and no refetch, mode toggle, or re-render ever overwrites the edit *(`onContactAddressChange` sets `addressAuto = false` for good; the re-arm key is the customer id alone, not `isDelivery`, so a mode toggle never re-arms it)*
    - [x] Selecting a different customer re-arms the auto-fill and overwrites the field *(`lastCustomerKey` tracks `customer?.id`; a change flips `addressAuto` back to `true`)*
    - [x] A customer with no profile address leaves the field empty with no hint, and save stays blocked by T96's required-address reason *(`resolvedAddress` is `undefined` when `.data.address` is null/undefined, both hint and `displayedAddress` fall through to empty `contactAddress`)*
    - [x] **`saveBlockedReason` and `save()` both read `displayedAddress`, not `contactAddress`** — a prefilled, untouched address saves successfully and arrives in the request body (plan §Architecture, the one real trap in this phase) *(both updated, plus the `contact_address` field in the `createOrder.mutateAsync` payload)*
    - [x] No `useCustomer` request is made in pickup mode or with no customer selected *(`useCustomer(tenantId, isDelivery ? customer?.id : undefined)` — the hook's own `enabled: !!customerId` does the gating; `lib/hooks.ts` stays untouched)*
  - Verify: `pnpm build && pnpm lint` *(clean 2026-08-24)*; manual — spec steps 11–14, 16, with the network tab open for the last criterion *(pending — no browser tooling available this session)*
  - Files: `console/src/pages/console/NewOrder.tsx`, `console/src/components/orders/OrderCart.tsx`
  - Dependencies: T96 · **Size: M**

- [~] **T99: `QuickAddCustomerDialog` — four-field in-order create form** *(code complete; `pnpm build && pnpm lint` clean 2026-08-24 — new file has no importer yet, exercised end-to-end in T100)*
  - **Description:** New leaf component `components/orders/QuickAddCustomerDialog.tsx`: الاسم\*, الهاتف\*, العنوان, الفرع. Same stack as its sibling (`Dialog` + `react-hook-form` + `zod`) and the same validation bounds (name ≤100, phone ≤12, address ≤200) so both forms reject the same input. The branch is **read-only**, rendered from a `branchName` prop — visible so the operator sees where the customer is registered, never editable (cross-branch orders are refused, so any other branch would produce a customer unusable for this order). Optional `defaultName`/`defaultPhone1` seeds. Submits through the existing `useCreateCustomer` with `group_id: undefined, credit_limit: 0`, then reports `{ id, name, phone1, address, branchId }` through `onCreated`. **Never navigates**, and `CreateCustomerDialog.tsx` is not touched.
  - Acceptance:
    - [x] Four fields in order, الاسم autofocused, الفرع displayed and not editable *(name/phone1/address/branch in that order; `autoFocus` on the name input; branch renders as a `disabled readOnly` `Input`)*
    - [x] Name and phone are required with the same messages/bounds as `CreateCustomerDialog`'s schema; a failed create surfaces `errorMessage(err)` in a toast and leaves the dialog open with the form intact *(schema copied verbatim for these two fields; the `catch` block toasts and does not call `onOpenChange`/reset)*
    - [x] Success fires `onCreated` with the created id plus the submitted name/phone/address and the branch id, and calls no navigation *(no `useNavigate` in the file)*
    - [x] Reopening the dialog after a create or a cancel starts from empty fields (plus any seeds) *(`useEffect` resets the form to `{defaultName, defaultPhone1, ''}` on every `open` transition to `true`, since the dialog stays mounted across opens and RHF's `defaultValues` is a mount-time snapshot, not reactive to prop changes)*
    - [x] `git diff --stat` shows `CreateCustomerDialog.tsx` unchanged *(verified — empty diff)*
  - Verify: `pnpm build && pnpm lint` *(clean 2026-08-24)*; component is exercised end-to-end in T100
  - Files: `console/src/components/orders/QuickAddCustomerDialog.tsx` (new)
  - Dependencies: none (parallelizable with T96–T98) · **Size: M**

- [~] **T100: Wire the quick-add into the customer picker + zen-mode guards** *(code complete; `pnpm build && pnpm lint` clean 2026-08-24 — manual spec-steps 7–10, 15 pass pending)*
  - **Description:** Add «عميل جديد» to the picker popover (footer button, plus a seeded «إنشاء عميل جديد» in the no-results empty state — digits in the search box seed the phone, anything else seeds the name). Disabled with the «اختر الفرع أولًا» hint while no branch is selected, since the customer must be created at the order's branch. Opening it closes the popover and mounts `QuickAddCustomerDialog` with the order's `branchId`/`branchName`. On success: select the new customer (including its `branchId`), seed `contactAddress` from the submitted address with the auto flag still armed so no detail fetch is needed, toast, close — **no navigation**. Zen guards: the zen `Escape` handler bails while the dialog is open, and the dialog is confirmed to paint above the zen overlay (spec OQ3 — fallback, decided in advance: drop the zen container to `z-40`, never bump the shared dialog primitive).
  - Acceptance:
    - [x] The create affordance appears in the popover footer and in the no-results empty state, seeded from the search text (digits → phone, otherwise → name) *(footer `Button` always renders when the popover is open; the no-results block adds a second seeded button; both share `quickAddDefaultName`/`quickAddDefaultPhone1`, derived from the raw `customerSearch` via `/^\d+$/`)*
    - [x] With no branch selected, the affordance is disabled and says why *(footer button `disabled={!branchId}` with a `title` repeating the popover's own «اختر الفرع أولًا لعرض عملائه» hint)*
    - [x] Creating selects the new customer, fills the delivery address from the submitted address, keeps the cart/branch/mode/note untouched, and leaves the URL unchanged *(`onQuickAddCreated` only calls `setCustomer`/`setContactAddress`/`setCustomerSearch('')`; no navigation, no other state touched)*
    - [x] Cancelling or a failed create leaves the previously selected customer and every other field exactly as they were *(cancel only calls `onOpenChange(false)`; a failed create's `catch` in `QuickAddCustomerDialog` toasts and never calls `onCreated`)*
    - [~] In zen mode the dialog paints above the overlay; `Esc` closes only the dialog, and a second `Esc` exits zen *(zen's keydown handler now bails while `quickAddOpen` is true, with `quickAddOpen` in the effect's deps to keep the closure current; the paint-order claim relies on Radix's dialog portal mounting into `document.body` after the zen portal, both `z-50` — untestable without a browser this session, so the `z-40` fallback was **not** applied preemptively per the plan's "verify first" note)*
  - Verify: `pnpm build && pnpm lint` *(clean 2026-08-24)*; manual — spec steps 7–10, 15 *(pending — no browser tooling available this session)*
  - Files: `console/src/pages/console/NewOrder.tsx`
  - Dependencies: T97, T98, T99 · **Size: M**

- [ ] **Checkpoint 12a — Phase 12 complete**
  - [ ] `pnpm build && pnpm lint` clean, no new warnings beyond the two pre-existing `auth.tsx` react-refresh ones
  - [ ] Full 19-step manual checklist in spec §Testing Strategy passes on a tenant with ≥2 branches, customers on each, and at least one customer with a profile address and one without
  - [ ] Regression: Customers page «عميل جديد» still opens the full form and still navigates to the new profile; `git diff --stat` shows `CreateCustomerDialog.tsx` untouched
  - [ ] `package.json` unchanged; `lib/{types,api,query,hooks}.ts` unchanged
  - [ ] Spec OQ3 (dialog stacking) settled; OQ4 (detail-fetch cost) resolved or explicitly carried forward
  - [ ] **Human review — Phase 12 complete**

## Phase 13 — Console RBAC (custom roles for invited members)

Spec: `tasks/spec-console-rbac.md` (r2) · Plan: `tasks/plan-console-rbac.md`

Four groups: **A (T101–T106)** the enforcement layer, shipped with zero behaviour change;
**B (T107–T117)** roles become usable; **C (T118–T123)** branch scoping; **D (T124–T125)**
real invitations. **T118 has no dependencies and should be merged on day one** — it is a
no-op by construction and de-risks the widest mechanical diff in the phase.

### Group A — permission core (api only, invisible)

- [x] **T101: `internal/perm` — the permission catalog and the two functions that read it**
  - **Description:** New pure package with no store, context, or Mongo dependency. Holds the catalog (spec D3), `Can(perms []string, code string) bool` — exact set membership plus the single rule `X.manage` implies `X.view` — and `Normalize([]string) ([]string, error)` which rejects unknown codes, dedupes, and adds `X.view` for every `X.manage`. Also defines `Scope`, the per-request value T104 stashes and T105 consumes: resolved permissions and branch allowlist (account/role id, no `model` import — kept dependency-free), with `Has(code)`, `AllowsBranch(id)`, and `IsUnscoped()`. **No dotted-prefix hierarchy** — r1 of the spec specified one and it was arithmetically wrong (`catalog.price.write` walks up to `catalog.price`, never `catalog.manage`). **Note:** spec D3's prose says "14 permission codes" but its own table lists 9 sections summing to **15** (`inventory.view`/`reports.view`/`company.manage` are single-sided, the other 6 sections are view+manage pairs = 12+2+1=15); implemented the table verbatim (15 codes) since that's the unambiguous source — flagged to the user, not silently "fixed" by dropping a code.
  - Acceptance:
    - [x] The catalog is exactly the codes in spec D3's table — a test asserts the full set, so adding or dropping one is a deliberate diff
    - [x] `Can` is true for an exact code and for `X.view` when `X.manage` is held; false for an unknown code, an empty set, and a `X.view`-only set asked for `X.manage`
    - [x] `Normalize` rejects an unknown code with a named error, rejects an empty result, dedupes, and expands every `X.manage` to include `X.view`
    - [x] `Scope` for an owner reports every permission and `IsUnscoped() == true`; for a scoped member, `AllowsBranch` is true only for listed ids, and an empty allowlist allows every branch
  - Verify: `make test && make vet` — both clean, 2026-08-26 (full suite, not just the new package)
  - Files: `api/internal/perm/perm.go` (new), `api/internal/perm/perm_test.go` (new)
  - Dependencies: none (parallelizable with T102, T118) · **Size: S**

- [x] **T102: `TenantRole` on the tenant document + widen `MemberRole` to the full member row**
  - **Description:** `TenantRole` embedded as `Tenant.Roles` (spec D1 — both auth choke points already call `TenantByID`, so this costs zero extra reads); `TenantMember` gains `RoleID`, `BranchIDs`, `InvitedAt`, `AcceptedAt`. Store gains role array ops on the tenant doc — append, update-by-id via `arrayFilters`, remove-by-id — plus `CountMembersByRole` for T107's delete guard, which reads `tenant_members` rather than tracking holders on the role. `mongostore.MemberRole` becomes `MemberByAccount` returning the whole row it **already decodes and discards** (`tenant_members.go:38-52`), making `role_id`/`branch_ids` free on every request.
  - **Blast radius beyond the listed files (required for compilation, not optional):** `membership.Store`'s interface method and `Require`'s internal call switched to `MemberByAccount` (its external signature/behavior is unchanged, per acceptance); `hq.Store`'s interface method renamed to match (it's structurally required to satisfy `membership.Store`); `tenant/service_members.go`'s one direct `s.store.MemberRole(...)` call site renamed. All pure renames, zero behavior change. **Not** wired: `InviteMember`/`Register` do not yet populate `RoleID`/`BranchIDs`/`InvitedAt`/`AcceptedAt` on member creation — that's T103 (backfill for existing members) and T124/T125 (real invitations), not T102.
  - Acceptance:
    - [x] Editing two different roles on one tenant concurrently does not clobber either — the update is a positional `arrayFilters` `$set`, not an array rewrite *(`TestTenantRole_ConcurrentEditsDoNotClobber`, `api/internal/store/mongo/tenant_roles_test.go`)*
    - [x] `MemberByAccount` returns the full row and still `ErrNotFound` for a non-member; `membership.Require`'s external contract is unchanged *(`TestMemberByAccount_ReturnsFullRowAndNotFound`; `Require` still takes/returns exactly `(context, Store, tenantID, accountID) (model.MemberRole, error)`)*
    - [x] `CountMembersByRole` counts only members of the given tenant holding that `role_id` *(`TestCountMembersByRole`)*
    - [x] `make test` green after widening the two fakes in `hq/service_test.go` and `tenant/service_test.go` — that is the entire blast radius *(plus the three necessary renames above; nothing else changed)*
  - Verify: `make test && make vet` — both clean, 2026-08-26. **Caveat:** the new store-layer tests (`tenant_roles_test.go`) are `TEST_MONGO_URI`-gated integration tests, same convention as the rest of `store/mongo` — no Mongo instance was available in this session so they compile and skip cleanly but are **unverified against real Mongo**; needs a human pass with `TEST_MONGO_URI` set, same gap as T19/T23/T26 etc. in earlier phases.
  - Files: `api/internal/model/model.go`, `api/internal/store/mongo/tenants.go`, `api/internal/store/mongo/tenant_members.go`, `api/internal/store/mongo/tenant_roles_test.go` (new), `api/internal/hq/service.go`, `api/internal/hq/service_test.go`, `api/internal/membership/membership.go`, `api/internal/tenant/service_members.go`, `api/internal/tenant/service_test.go`
  - Dependencies: none (parallelizable with T101, T118) · **Size: M**

- [x] **T103: startup backfill — seed default roles, preserve every existing member's access**
  - **Description:** Idempotent startup pass, same shape as the existing `BackfillOwnerMembers`. Per tenant: seed **«وصول كامل»** (all 14 codes) and **«قراءة فقط»** (every `.view` code), then point each existing `Role: "member"` row at «وصول كامل», unscoped, with `AcceptedAt = CreatedAt` (they already have access — they are not pending). Owner rows are untouched. Logs seeded/updated counts so a wrong run is visible on deploy rather than as a support ticket.
  - Implementation note: lives entirely in the store layer as `Store.BackfillRolesAndMembers` (not a `tenant.Service` method like `BackfillOwnerMembers`/`BackfillHasBeenMember`) — `main.go` calls it directly on `store`, matching the task's own Files list. Idempotency is by-name for role seeding (skip if a role named «وصول كامل»/«قراءة فقط» already exists on the tenant) and by-RoleID for members (skip once `RoleID` is set) — no new field was added to `Tenant` to track "already backfilled", since `model.go` wasn't in scope for this task. **Carries forward T101's flagged 14-vs-15 catalog discrepancy:** «وصول كامل» is seeded with `perm.All`, which is 15 codes, not the 14 the spec prose (and this task's own description) says — same table-over-prose resolution as T101, not a new decision.
  - Acceptance:
    - [x] **Access preservation:** a member that existed before the migration resolves to all of `perm.All` (15 codes — see note above) and an empty allowlist — the test that stops this deploy from locking people out *(`TestBackfillRolesAndMembers_PreservesAccess`)*
    - [x] Two consecutive startups leave identical state and the second logs zero changes *(`TestBackfillRolesAndMembers_SecondRunIsNoop`)*
    - [x] Owner rows keep `RoleID` empty and are never assigned a seeded role *(asserted in `TestBackfillRolesAndMembers_PreservesAccess`)*
    - [x] A tenant with no members still gets both seeded roles; seeded roles carry no protected flag and are editable and deletable like any other *(`TestBackfillRolesAndMembers_TenantWithNoMembers`; no protected/seeded flag exists anywhere on `TenantRole`, so `UpdateTenantRole`/`RemoveTenantRole` from T102 apply to them unmodified)*
  - Verify: `make test && make vet` — both clean, 2026-08-26. **Caveat:** same as T102 — `tenants_backfill_test.go` is `TEST_MONGO_URI`-gated, no Mongo instance was available in this sandbox (`docker ps` fails here too) so the three new tests compile and skip cleanly but are **unverified against real Mongo**. The `make run` twice / diff logged counts verification step was not performed for the same reason; needs a human pass with `TEST_MONGO_URI` set and a dev Mongo.
  - Files: `api/internal/store/mongo/tenants.go`, `api/cmd/api/main.go`, `api/internal/store/mongo/tenants_backfill_test.go` (new) — matches the task's stated file list exactly, no additional blast radius this time.
  - Dependencies: T102 · **Size: M**

- [x] **T104: `requirePerm` middleware — resolve once, stash, fail closed**
  - **Description:** One middleware resolves `perm.Scope` exactly once per request (member row via `MemberByAccount`, permissions from the already-fetched tenant document), enforces the route's declared permission code, and stashes the Scope in the request context for T105's consumers. The route→permission table lives **beside the router in `server.go`** so a new route and its permission are one diff. Any tenant-scoped route with no table entry is **denied for everyone, owner included** — we cannot know what an undeclared route requires, and a loud 403 in dev is the point (spec D9). Error contract per spec: `forbidden_permission`, `forbidden_scope`, `forbidden_unscoped`, each with the `required` code.
  - **Design note — why the table lives in `middleware.go`, not truly "beside" each `r.Get(...)` call:** chi resolves a route's own leaf pattern (`RoutePattern()`) only *after* that mux level's own middlewares have already run — confirmed empirically (a throwaway chi program), not assumed. So a blanket `r.Use(s.requirePerm)` cannot key off chi's resolved pattern before dispatch, and a per-route `.With(requirePerm(code))` decoration would fail *open* (a route added without it just has no gate) instead of D9's required fail *closed*. `permTable` is therefore a small standalone matcher keyed on method + path-suffix-after-`/v1/tenants/{id}` (chi does resolve the ancestor `{id}` param before this level's middleware, which is what makes stripping that prefix possible pre-dispatch), checked by one `r.Use(s.requirePerm)` at the top of the `/{id}` subrouter. `TestPermTableCoversEveryRegisteredRoute` (`chi.Walk` cross-checked against `permTable`) is what actually delivers "a new route and its permission are one diff" — a route registered without a table entry fails this test at build time, not just 403s at runtime.
  - **Ripple beyond the stated files (required for wiring, not optional):** `Server` gains a `store scopeResolver` field (a 2-method interface — `MemberByAccount`, `TenantByID` — satisfied structurally by `*mongostore.Store`, declared in `middleware.go` so a test can supply a counting fake instead of a real Mongo connection). `New()`'s signature grew one parameter; `cmd/api/main.go` and `internal/httpapi/updates_handlers_test.go`'s one `New(...)` call site both updated to pass it through (`store`/`nil` respectively) — pure plumbing, no behavior change to either.
  - **Owner-only routes (member/role management, billing) aren't in the D3 catalog at all (D2)**, so `permTable` supports three rule shapes, not just a permission code: `rule(...)` (>=1 code, OR semantics), `ownerRule(...)` (owner only, no code), `memberRule(...)` (any member, no code — the D3 "always visible" surfaces: the bundle, the member list, issuing a sync token). `/hq/customer-groups` is the one route with more than one code — it's genuinely shared by the Customers and Suppliers pages (confirmed in `console/src/lib/api.ts` — no separate `/hq/supplier-groups` exists), so either `customers.view` or `suppliers.view` admits it.
  - Acceptance:
    - [x] An owner passes every mapped route; a member passes exactly the routes their role's codes cover and gets 403 elsewhere *(`TestRequirePerm_OwnerPassesMemberGatedByCode`)*
    - [x] A route absent from the table 403s with `forbidden_permission` **even for the owner** *(`TestRequirePerm_UndeclaredRouteDeniesEvenOwner`)*
    - [x] The 403 body carries the distinct `code` and the `required` permission, so the console can tell "not allowed" from "not allowed *here*" *(`TestRequirePerm_403BodyCarriesCodeAndRequired`)*
    - [x] Exactly one membership lookup per request, asserted with a counting store fake *(`TestRequirePerm_ExactlyOneMembershipLookupPerRequest`; `TestRequirePerm_OwnerSkipsTenantReadOwnerRowsHaveNoRoleEntry` additionally confirms an owner costs zero `TenantByID` reads, per D1)*
  - Also covered, beyond the stated acceptance: owner-only routes reject a member holding every catalog code (`TestRequirePerm_OwnerOnlyRouteRejectsMember` — D2's point that `members.manage` isn't a permission at all); an any-member route needs no code (`TestRequirePerm_AnyMemberRouteNeedsNoCode`); the shared `hq/customer-groups` route accepts either code (`TestRequirePerm_SharedCustomerGroupsRouteAcceptsEitherCode`); the resolved Scope is readable by the next handler via the new `scopeFrom` (`TestRequirePerm_StashesScopeInContextForNextHandler`, what T105 will call); a non-member gets the existing uncoded 403 (`TestRequirePerm_NonMemberForbidden`); full route-coverage (`TestPermTableCoversEveryRegisteredRoute`).
  - Only `forbidden_permission` is implemented — `forbidden_scope`/`forbidden_unscoped` are D5c/branch-scoping concerns, out of scope until T118–T123 (Group C).
  - A few `permTable` codes were judgment calls not spelled out in D3 (documented as comments at each site in `middleware.go`): `hq/orders/availability` and `hq/orders/delivery-fee` (both read-only) require `orders.manage`, not `orders.view`, because both endpoints exist solely to support the New Order form; `hq/catalog/products/{id}/movements` was mapped to `catalog.view` by URL grouping, not `inventory.view`.
  - Verify: `make test && make vet` — both clean, 2026-08-26. Full suite passes including every pre-existing `internal/httpapi` test unmodified (aside from the one `New(...)` call-site plumbing fix above) — no regression from adding `r.Use(s.requirePerm)` to the `/{id}` subrouter.
  - Files: `api/internal/httpapi/middleware.go`, `api/internal/httpapi/server.go`, `api/internal/httpapi/middleware_test.go`, plus the necessary ripple noted above (`api/cmd/api/main.go`, `api/internal/httpapi/updates_handlers_test.go`)
  - Dependencies: T101, T102 · **Size: M**

- [x] **T105: services read the resolved scope from context instead of re-querying**
  - **Description:** `tenant.memberRole()` (called by `owned()`, 12 call sites behind those 2 helpers) and `hq.resolveGateway()`/`CheckOwnership()` (every one of ~44 HQ methods) take the Scope from the request context, falling back to a lookup when the context is empty so existing tests and any future non-HTTP caller keep working untouched. `resolveGateway` returns the Scope alongside tenant and shard, which is what gives T119/T120 the allowlist without a second plumbing pass.
  - **Design note — where the context key lives:** `tenant` and `hq` cannot import `httpapi` (it imports them — a cycle), so the context key T104's `requirePerm` writes and T105's services read cannot stay private to `middleware.go` as it was after T104. Moved it into `perm` (imported by all three packages already, for the `Scope` type itself) as `perm.WithScope`/`perm.ScopeFrom`. `httpapi`'s local `scopeFrom` and `scopeKey` are now a thin wrapper over `perm.ScopeFrom` / removed entirely — `middleware_test.go`'s existing `scopeFrom(r.Context())` call and every T104 test pass unmodified. `perm.Scope` gained one new field, `TenantID`, set by both `httpapi.resolveScope` and the new fallback resolvers below — without it, a context-cached Scope could be wrongly trusted for the wrong tenant if a handler ever called a service method for a *different* tenant than the one `requirePerm` gated (defence in depth; can't happen on the current single-tenant-per-request routes, but costs nothing to rule out in code rather than by convention).
  - **Where the ctx-first/store-fallback logic actually lives:** in `membership.go`, as two new functions alongside the unmodified `Require` — `RequireRole` (role only, replaces `tenant.memberRole`'s and `hq.CheckOwnership`'s calls to `Require`) and `RequireScope` (full Scope, replaces `hq.resolveGateway`'s call to `Require`; takes the already-fetched `*model.Tenant` as a parameter instead of re-reading it, since every call site has it in hand already — so the empty-context fallback path costs exactly one `MemberByAccount` read, never a second `TenantByID`). Both check `perm.ScopeFrom(ctx)` first and only trust it when its `TenantID`/`AccountID` match what was asked for; otherwise they fall through to the same store read as before T105 — byte-for-byte the same result an unmodified `Require` call would have produced, which is what keeps every existing test green with zero edits.
  - **Ripple beyond the stated files (required for wiring, not optional):** `perm/perm.go` (the `TenantID` field, `WithScope`/`ScopeFrom`, `"context"` import), `httpapi/middleware.go` (swapped the local `scopeKey`/`context.WithValue` for `perm.WithScope`, `TenantID` set on both `resolveScope` literals) — both are the wiring `perm.Scope` in context requires, no behavioural change to T104's contract. `hq/service.go`'s `resolveGateway` signature widened from `(*model.Tenant, *model.Shard, error)` to `(*model.Tenant, *model.Shard, *perm.Scope, error)`; all 44 call sites across `service.go` (37) and `service_orders.go` (7) updated mechanically (`t, shard, err := ...` → `t, shard, _, err := ...`, discarding the new value per the task's own "no behavioural change yet") — none of them read the Scope, that's T119/T120's job.
  - Acceptance:
    - [x] One membership read per HTTP request end-to-end (counting fake), down from the two a naive middleware would add *(`TestResolveGateway_ContextScopeAvoidsSecondMembershipRead`, `TestCheckOwnership_ContextScopeAvoidsSecondMembershipRead` — `api/internal/hq/service_test.go`; `TestRequireRole_UsesContextScopeWithoutStoreRead`, `TestRequireScope_UsesContextScopeWithoutStoreRead` — `api/internal/membership/membership_test.go`, all assert the counting fake's call count stays at 0)*
    - [x] The empty-context fallback path resolves identically — every existing `tenant` and `hq` service test passes **unmodified** *(full suite green; `internal/tenant` and `internal/hq` test files' only edits are the new T105 tests appended to `hq/service_test.go` plus one field added to its `fakeStore` — no existing test body changed; `internal/tenant/service_test.go` untouched entirely, it's Mongo-backed and never touches `resolveGateway`)*
    - [x] `resolveGateway`'s new return value compiles across all its callers with no behavioural change yet *(`go build ./...` clean across all 44 call sites; `TestResolveGateway_EmptyContextFallsBackToOneMembershipRead` pins the pre-T105 behavior byte-for-byte: still exactly 1 `MemberByAccount` read, same owner Scope shape)*
  - Also covered, beyond the stated acceptance: a context-cached Scope resolved for a *different* tenant is never trusted, closing the defence-in-depth gap the new `TenantID` field exists for (`TestResolveGateway_ScopeForAnotherTenantIsIgnored`, `TestRequireRole_MismatchedScopeFallsBackToStore`); `RequireScope`'s owner short-circuit and `RequireRole`/`RequireScope`'s non-member `ErrForbidden` path are each independently tested in `membership_test.go`, mirroring T104's equivalent coverage of `resolveScope`.
  - Verify: `make test && make vet` — both clean, 2026-08-26. Full suite passes, including every pre-existing `internal/tenant` and `internal/hq` test unmodified — no regression from the `resolveGateway` signature widening or the `membership.Require` → `RequireRole`/`RequireScope` swaps.
  - Files: `api/internal/tenant/service.go`, `api/internal/hq/service.go`, `api/internal/hq/service_orders.go`, `api/internal/hq/service_test.go`, `api/internal/membership/membership.go`, `api/internal/membership/membership_test.go` (new), plus the necessary ripple noted above (`api/internal/perm/perm.go`, `api/internal/httpapi/middleware.go`)
  - Dependencies: T104 · **Size: M**

- [x] **T106: route-coverage test — an unguarded route fails the build**
  - **Description:** Walks the chi router, collects every route under `/v1/tenants/{id}`, and fails on any absent from T104's permission table. The middleware already denies these at runtime (T104); this turns a 3am 403 into a build-time failure.
  - **Note — this task was already substantially delivered by T104.** `TestPermTableCoversEveryRegisteredRoute` (the walk-and-compare test) was written as part of T104's own acceptance and lived in `middleware_test.go`. T106's actual remaining work was: (1) move it into its own file per the stated Files line, (2) split the walking from the comparing so the comparison logic (`permTableCoverageErrors`) is callable against a router that *isn't* the real one, and (3) add the "proven to fail" sub-test the original never had — a synthetic chi router with one route deliberately absent from `permTable`, asserting the checker reports it by exact method+pattern string, not just a failing count.
  - **Ripple beyond the stated file:** `middleware_test.go` lost the moved test and its now-unused `sort`/`strings` imports; no other change there. `permtable_test.go` is genuinely new, holding `walkTenantRoutes` (the chi.Walk collector, unchanged logic from T104), `ruleKey` and `permTableCoverageErrors` (extracted so both tests share one comparison path), and the two tests.
  - Acceptance:
    - [x] Passes against the current router with every route mapped *(`TestPermTableCoversEveryRegisteredRoute`, moved verbatim in logic from T104, still green)*
    - [x] Proven to fail: the test registers a temporary unmapped route in a sub-test and asserts the walker catches it *(`TestPermTableCoverageCatchesUnmappedRoute` — a synthetic router with `GET /no-such-route` alongside a correctly-mapped `GET /`; asserts the exact failure string is present among `permTableCoverageErrors`'s output)*
    - [x] The failure message names the offending method + pattern, not just a count *(`permTableCoverageErrors` emits one string per mismatch — `route "GET /no-such-route" is registered but has no permTable entry...` — never a bare count; the sub-test asserts on that exact string)*
  - Verify: `make test && make vet` — both clean, 2026-08-26; both new tests also run individually with `-v` to confirm pass, and `gofmt -l` clean on all touched files
  - Files: `api/internal/httpapi/permtable_test.go` (new), `api/internal/httpapi/middleware_test.go` (moved test + import cleanup)
  - Dependencies: T104 · **Size: S**

- [ ] **Checkpoint 13a — Group A complete: enforcement layer live, nothing changed**
  - [x] `make test && make vet` green; no console or gateway change in the diff *(2026-08-26: `go build ./... && go vet ./... && go test ./...` all clean/ok across every `api` package; `git diff --stat` against the working tree touches only `api/` and `tasks/` — this repo has no `gateway` directory at all (it lives outside `platform`, per [[arib-system-architecture]]), and `console/` is untouched)*
  - [ ] **Manual: nothing changed.** An existing invited member reaches every screen exactly as before — Overview, Branches, Catalog, Inventory, Customers, Suppliers, Orders, Reports, Settings — **not done: needs a running console + api + a real tenant with an invited member; no browser/UI access from this session**
  - [ ] Backfill run twice against a real tenant with existing invited members; second run reports zero changes — **not done: no Mongo/docker available in this sandbox** (`docker ps` fails — no daemon; no `mongod`/`mongosh` on PATH; `TEST_MONGO_URI` unset) — same gap already flagged in T102/T103's own Verify lines; needs a human pass with a dev Mongo
  - [x] Every tenant-scoped route mapped; coverage test green *(T106 — `TestPermTableCoversEveryRegisteredRoute` passes against the live router)*
  - [ ] **Human review — group A before any UI depends on it** — **not done: requires the user/a reviewer, not an automatable step**

### Group B — role management (api + console)

- [x] **T107: role CRUD service + handlers**
  - **Description:** `GET/POST/PUT/DELETE /v1/tenants/{id}/roles` plus `GET …/permissions` (codes only — the console owns the Arabic labels). Writes are owner-only; reads are any member, since the members table labels each row with its role name. Validation through `perm.Normalize`; at least one permission; name unique per tenant (application check, per spec D1). `DELETE` returns **409 with the assigned member count** rather than cascading (spec D8).
  - **Implementation notes:** Service methods mirror `InviteMember`'s existing shape exactly (`s.memberRole(...)` for the auth check, then the owner-only gate as a one-line `if role != model.RoleOwner`) — no new auth pattern introduced. `Roles()` (the list) returns `[]RoleView`, a response-shaping type (own `json` tags, same convention as the pre-existing `MemberView` vs. `model.TenantMember` — `model.TenantRole` itself carries only `bson` tags) that adds `AssignedCount`; the API surface note ("list roles + assigned counts") wants it, and it's computed from one `MembersByTenant` read tallied in-memory rather than one `CountMembersByRole` call per role. `DeleteRole`'s refusal is a typed `*RoleAssignedError{Count}` (not a sentinel) so the console gets the count, not just a boolean — `writeTenantError` extracts it via `errors.As` into a `{code: "role_assigned", count}` body, same shape as the spec's `forbidden_permission` precedent.
  - **Ripple beyond the stated files (required — a route with no permTable entry is denied to everyone, owner included, per T104's fail-closed default):** `api/internal/httpapi/middleware.go` gained 5 `permTable` entries (`GET roles` member, `POST/PUT/DELETE roles(/{roleId})` owner, `GET permissions` member); `api/internal/httpapi/tenant_handlers.go`'s `writeTenantError` gained 4 new cases (`ErrInvalidRoleName`, `perm.ErrUnknownCode`/`perm.ErrEmptyPermissions`, `ErrDuplicateRoleName`, `*RoleAssignedError`) and a `perm` import.
  - Acceptance:
    - [x] A non-owner member gets `ErrOwnerOnly` on create/update/delete and a 200 on list *(`TestRoles_NonOwnerListsButCannotWrite`)*
    - [x] Creating with an unknown code, an empty permission set, or a duplicate name is rejected with a distinct error each *(`TestCreateRole_ValidationErrors` — `perm.ErrUnknownCode`, `perm.ErrEmptyPermissions`, `ErrDuplicateRoleName`; also covers the empty-name case with `ErrInvalidRoleName`, not called out in the acceptance line but validated the same way)*
    - [x] Deleting a role held by ≥1 member returns 409 naming the count; deleting an unheld role succeeds *(`TestDeleteRole_RefusedWhileAssignedSucceedsWhenUnheld` — asserts `*RoleAssignedError.Count == 2`, then deletes an unheld role and confirms it's gone from a fresh `Roles()` list)*
    - [x] A created role's stored permissions are the normalized set — ticking only `catalog.manage` stores `catalog.view` too *(`TestCreateRole_StoresNormalizedPermissions`)*
    - [x] Also covered, beyond the stated acceptance: list reports each role's assigned count (`TestRoles_ListReportsAssignedCounts`); `.../permissions` returns the full catalog to any member and `ErrForbidden` to a stranger (`TestPermissions_ReturnsFullCatalogToAnyMember`)
  - Verify: `make test && make vet` — both clean, 2026-08-26; `go vet` clean. **Caveat, same gap as T102/T103:** `service_roles_test.go` is `TEST_MONGO_URI`-gated (same convention as the rest of the `tenant` package) — no Mongo instance was available in this sandbox, so all 6 new tests compile and skip cleanly but are **unverified against real Mongo**; needs a human pass with `TEST_MONGO_URI` set. `TestPermTableCoversEveryRegisteredRoute` (T106) re-run and still green with the 5 new routes. `gofmt -l` clean on every touched file.
  - Files: `api/internal/tenant/service_roles.go` (new), `api/internal/tenant/service_roles_test.go` (new), `api/internal/httpapi/role_handlers.go` (new), `api/internal/httpapi/server.go`, `api/internal/httpapi/middleware.go` (permTable ripple), `api/internal/httpapi/tenant_handlers.go` (error-mapping ripple)
  - Dependencies: T101, T102 · **Size: M**

- [x] **T108: `me` block on the tenant bundle + role fields on member rows**
  - **Description:** `GET /v1/tenants/{id}` gains `me` — the caller's role, role name, resolved permissions, and branch allowlist — so the console renders nav, routes, tiles, and buttons from a payload `SetupGate`/`AppShell`/`CommandPalette` **already fetch**, with no extra request. `MemberView` grows `role_id`, `role_name`, `branch_ids`, `accepted_at`.
  - **Implementation notes:** `GetBundle` no longer goes through `owned()`/`memberRole()` (role-only) — it fetches the tenant directly and calls `membership.RequireScope` (T105) itself, the same pattern `hq.resolveGateway` already uses, so the full `perm.Scope` `me` needs is one read, not a bare ownership check plus a second one on top. `MeView`/`meView(t, scope)` and `roleName(t, roleID)` live in `service_members.go` next to `MemberView`, since both `Bundle.Me` and each member row resolve a role name the same way — against `t.Roles`, already in hand, never a per-row query. `orEmpty` turns a nil branch allowlist into `[]` rather than JSON `null`, matching the spec's explicit `"branch_ids": []  // [] = all` wire contract; applied to both `me.branch_ids` and each member row's `branch_ids`. `Members()` now keeps the `*model.Tenant` `owned()` returns (previously discarded) instead of re-fetching it. A freshly invited member (`InviteMember`) has no role assigned yet — that's T109's `PATCH` — so its row's `role_id`/`role_name` are empty, same as the owner row.
  - Acceptance:
    - [x] An owner's `me.permissions` is the full catalog with `branch_ids: []`; a member's is exactly their role's normalized set *(`TestGetBundle_MeBlock`)*
    - [x] `me` is computed from the same `perm.Scope` the middleware resolved — not recomputed, so the UI can never disagree with the enforcement *(`TestGetBundle_MeComputedFromContextScope` — stashes a `perm.Scope` in context that deliberately disagrees with what Mongo actually holds for the account; `GetBundle` returns the stashed Scope's role/permissions/branch_ids, proving it trusts the context over a fresh store read)*
    - [x] Member rows carry role name and allowlist without an extra query per row *(`TestGetBundle_MeBlock`'s `Members()` half — `roleName` resolves against the `t.Roles` `Members()` already fetched, no per-row store call, mirroring T107's `Roles()` tally pattern)*
  - Verify: `make test && make vet` — both clean, 2026-08-26. Unlike T102/T103/T107's prior sessions, a real Mongo instance was reachable this time (an already-running `arib-platform-mongodb-1` OrbStack container, `mongodb://arib:secret@arib-platform-mongodb-1.orb.local:27017/?authSource=admin`), so this task's own `TEST_MONGO_URI`-gated tests ran for real, not just compiled-and-skipped. **Found while re-running the full suite under real Mongo for the first time, unrelated to this task's diff:** `TestUpdateTenantRole_UnknownRoleReturnsNotFound` and `TestRemoveTenantRole` (`api/internal/store/mongo/tenant_roles_test.go`, T107's store layer) failed against real Mongo — see the follow-up fix below, applied the same session.
  - Files: `api/internal/tenant/service.go`, `api/internal/tenant/service_members.go`, `api/internal/tenant/service_members_test.go`
  - Dependencies: T105, T107 · **Size: S**

- [x] **T108 follow-up: fix `UpdateTenantRole`/`RemoveTenantRole` false results against real Mongo (`api/internal/store/mongo/tenants.go`)**
  - **Root causes, both in T107's store layer, neither ever exercised against real Mongo before this session:**
    1. `UpdateTenantRole`'s `roles.$[r]` arrayFilters update throws a raw Mongo write error ("The path 'roles' must exist...") instead of returning `ErrNotFound` when the tenant has never had a role added — `Tenant.Roles` is `bson:"roles,omitempty"` and unset at `Register` time, so a brand-new tenant (before the next `BackfillRolesAndMembers` boot cycle seeds its two default roles) has no `roles` field in Mongo at all, and `$[r]` rejects a path that isn't an array rather than just matching zero elements. Reachable in production, not just in tests: any owner calling `PUT .../roles/{roleId}` with a bad id on a tenant that's never created a role hits this.
    2. Both `UpdateTenantRole` and `RemoveTenantRole` inferred "did the role actually match" from `ModifiedCount`, reasoning (per the now-corrected comment) that the array-filtered element's own `updated_at` would only change if the filter matched. False: both updates also `$set` the tenant's *top-level* `updated_at` in the very same operation, and that alone flips `ModifiedCount` to 1 on every call — confirmed empirically against real Mongo, not just in theory — so neither function could ever actually detect "no role in the array had this id" once the array itself existed.
  - **Fix:** (1) `UpdateTenantRole` self-heals a missing `roles` path with a guarded `$exists: false` pre-update (`roles: []`) before the real arrayFilters update, so the array always exists by the time `$[r]` runs — a no-op once any role has ever been added. (2) Both functions now use `FindOneAndUpdate` instead of `UpdateByID`, decoding the *pre-update* document snapshot (the driver's default `ReturnDocument`) and checking `roleID` against that snapshot's `Roles` directly, rather than trusting either count. Same atomicity and per-role concurrency-safety as before (still one arrayFilters/$pull op, not a whole-array rewrite) — just a correct signal for "did it match," race-free against a concurrent edit of the same role.
  - **First tried and reverted:** removing `omitempty` from `Tenant.Roles` and setting `Roles: []model.TenantRole{}` in `Register`, to make the `roles` field always present at tenant creation. Rejected because every other `InsertTenant` caller (all of them test fixtures constructing a bare `model.Tenant{}`) would then insert an *explicit* `roles: null` instead of an absent field — which `$push` (`AddTenantRole`) rejects just as hard ("must be an array but is of type null"), breaking cases that worked before. The store-layer fix above is self-contained and doesn't depend on how the tenant document was created.
  - Verify: `go build ./... && go vet ./...` clean; `go test ./... -count=1` clean against real Mongo (the same OrbStack container as T108); `go test ./internal/store/mongo/... -race -count=2` clean. `gofmt -l` clean.
  - Files: `api/internal/store/mongo/tenants.go`
  - Dependencies: T108 (found during) · **Size: XS**

- [x] **T109: `PATCH /members/{memberId}` — reassign role and branches**
  - **Description:** Owner-only. Sets `role_id` and `branch_ids` on an existing member without revoking and re-inviting — the gap that makes a promotion currently mean deleting the person and starting over. Rejects an unknown role, a branch id not belonging to the tenant, and any attempt to touch the owner row.
  - **Implementation notes:** `AssignMemberRole` mirrors `RevokeMember`'s shape (owner-only gate via `memberRole`, fetch the target row, refuse the owner row) before doing its own validation: `hasRole(t, roleID)` against the already-fetched `Tenant.Roles`, `hasBranch(branches, id)` per `branchID` against `store.BranchesByTenant` — both checked, and both must pass, before `store.UpdateMemberRole` runs, so a rejected call never partially writes. New store method `UpdateMemberRole` (bare `$set` on `role_id`/`branch_ids`, scoped to `tenant_id` + `_id`, reporting found/not-found like `DeleteMember`) — validation and the owner-row guard are entirely the service's job, not the store's. D7 ("next request, no session action") needed no special code: `requirePerm` already resolves a fresh `perm.Scope` from Mongo on every request, so the test just asserts a `GetBundle` call before/after differs.
  - **Ripple beyond the stated files (required — same fail-closed reason T107 hit):** `api/internal/store/mongo/tenant_members.go` (`UpdateMemberRole`, new), `api/internal/httpapi/middleware.go` (`ownerRule(http.MethodPatch, "members/{memberId}")` — absent from `permTable` means denied to everyone, owner included, per T104).
  - Acceptance:
    - [x] A non-owner gets `ErrOwnerOnly`; the owner row is refused with `ErrCannotRemoveOwner`'s sibling error whether the change is role or branches *(`TestAssignMemberRole_OwnerOnlyAndOwnerRowProtected` — the new `ErrCannotModifyOwner`, checked for both a role-only and a branches-only attempt on the owner row)*
    - [x] An unknown `role_id`, or a `branch_id` from another tenant, is rejected before any write *(`TestAssignMemberRole_ValidationRejectsBeforeWrite` — `ErrUnknownRole`/`ErrUnknownBranch`; re-reads the member row after each rejection and confirms `role_id`/`branch_ids` are still empty)*
    - [x] The change takes effect on the member's next request with no session action (spec D7), asserted end-to-end *(`TestAssignMemberRole_TakesEffectOnNextRequest` — `GetBundle` as the member before assignment has zero permissions; `AssignMemberRole` runs as the owner; the very next `GetBundle` call, same account, no other action, shows the new role's normalized permissions and branch allowlist)*
  - Verify: `go build ./... && go vet ./...` clean; `go test ./... -count=1` clean against real Mongo (same OrbStack container as T108); `gofmt -l` clean on every touched file. `TestPermTableCoversEveryRegisteredRoute` (T106) re-run and still green with the new route.
  - Files: `api/internal/tenant/service_members.go`, `api/internal/tenant/service_members_test.go`, `api/internal/httpapi/tenant_handlers.go`, `api/internal/httpapi/server.go`
  - Dependencies: T107 · **Size: M**

- [x] **T110: console permission plumbing — one source for every gate**
  - **Description:** Types, api client, and hooks for roles/permissions/assign; `lib/perm.ts` exporting `useCan()` and `useScope()` off the bundle's `me` block; `AppShell` nav filtered so **a nav item renders iff its `view` permission resolves** (never a greyed-out entry that 403s on click). Lands **before any page edit** so no page invents its own check.
  - Implementation notes:
    - `types.ts`: added `TenantMeView` (`role`/`role_id`/`role_name`/`permissions`/`branch_ids`, snake_case — hand-written JSON, mirroring the API's `tenant.MeView`) and a `me: TenantMeView` field on `Bundle`. Named it `TenantMeView` rather than `MeView` because that name was already taken by the unrelated account-identity type (`GET /v1/me`'s `{account: Account}"`) — both are documented cross-referencing each other so the collision doesn't resurface.
    - `lib/perm.ts` (new): `PERM` — a typed object mirroring `api/internal/perm/perm.go`'s catalog constants one-to-one, so every future gate (T111, T114-T117) references a code by name instead of a bare string. `useScope(tenantId)` reads `useBundle(tenantId)` and returns `data?.me` — same `qk.bundle(id)` query key every other bundle consumer shares, so it dedupes onto the existing fetch rather than issuing a new one. `can(scope, code)` is a plain (non-hook) exact-membership check — deliberately *not* re-deriving D3's `manage`-implies-`view` rule, since `perm.Normalize` already expanded it server-side before the permission ever reaches the bundle (T107/T109). `useCan(tenantId, code)` composes the two for call sites that just want a boolean.
    - `AppShell.tsx`: `NavItem` gained an optional `code` field; the `nav` array is now `.filter((item) => !item.code || can(scope, item.code))`, with `scope` read once via `useScope` and reused for every item rather than calling a hook per item (would violate the rules of hooks inside `.filter`). Both the desktop sidebar and the mobile rail map over the same filtered `nav`, and `current` (breadcrumb resolution) is derived from `[...nav, ...hiddenRoutes]` — so all three surfaces the acceptance criteria name are gated from the one filter.
    - Nav-to-code mapping follows D3's table verbatim for the seven sections that have a `view` code (branches/catalog/inventory/customers/suppliers/orders/reports). Three items carry no `code` and stay always-visible per D3's explicit list: نظرة عامة، تنزيل التطبيق، الإعدادات. **النشاط التجاري (Company) also carries no `code`, by inference rather than an explicit spec line**: D3's table gives Company no `view` code at all (only `company.manage`), and T115's stated acceptance ("Without `company.manage`: the company form is read-only rather than absent, so the data is still visible") only makes sense if the page itself is reachable without `company.manage` — so gating the nav entry on `company.manage` would contradict T115 before T115 is even written. Worth a second look when T115 lands.
    - `api.ts` and `hooks.ts` ended up with no changes — `api.bundle`/`useBundle` already return the whole `Bundle`, and once `types.ts` described the `me` field on it, no new endpoint or hook was needed to expose it. Left in the Files list below since that's what the task's own file list named; noting the no-op here since T110 predicted the diff before the shape of T108's bundle change was fully known.
  - Acceptance:
    - [x] `useCan()` reads the existing bundle query — no new request appears in the network tab (same `qk.bundle(id)` key as every other bundle consumer; verified by reading `useScope`'s implementation against `qk.bundle`, not by opening a browser — see Verify)
    - [x] A member without `catalog.view` has no الكتالوج entry in the sidebar, the mobile rail, or the breadcrumb resolver (all three read the one filtered `nav` array)
    - [x] `can('catalog.manage')` is true when the role holds only `catalog.manage` **and** `can('catalog.view')` is also true (the server already normalized it — the client does not re-derive; `can()` is exact membership only, no manage-implies-view logic client-side)
  - Verify: `pnpm build && pnpm lint` — both clean (`tsc -b && vite build` succeeds; lint shows only the two pre-existing `auth.tsx` react-refresh warnings named in the plan's verification model, no new ones). No console test harness exists in this repo to unit-test `perm.ts` directly (plan's Console verification gate is build+lint only); did not spin up the dev server against a live backend for this task since it's pure plumbing with no visible behavior change for an owner (who holds the full permission catalog) — T111 onward, which change what's actually reachable per role, get the manual per-role browser check called for in the plan's checkpoint 13b.
  - Files: `console/src/lib/types.ts`, `console/src/lib/api.ts` (no changes needed), `console/src/lib/hooks.ts` (no changes needed), `console/src/lib/perm.ts` (new), `console/src/components/AppShell.tsx`
  - Dependencies: T108 · **Size: M**

- [x] **T111: `RequirePerm` route guard + stop retrying 403s**
  - **Description:** Nav filtering alone leaves direct URL entry rendering the page, firing its queries, and landing on a raw error state. `<RequirePerm code="catalog.view">` wraps the guarded routes in `App.tsx` and redirects to Overview with a toast. Separately, `query.ts` sets `retry: 1` globally, so today every denial costs two round trips — narrow it to server errors only.
  - Implementation notes:
    - `RequirePerm.tsx` (new): takes `code: PermCode` (T110's typed union, not a bare string — a typo fails `tsc`, not silently at runtime) and `children`. Reads `useBundle(tenantId)` for `isLoading` and `useScope(tenantId)` for the scope — same shared `qk.bundle(id)` cache `AppShell`'s nav filter and `SetupGate` already populated, so this issues no query of its own. While loading it renders `RouteLoader` (covers only the rare case of landing here before `SetupGate`'s own bundle fetch has resolved — normally it's already cached by the time this mounts); once resolved, a denial fires `toast.error(...)` from a `useEffect` (so it fires once per denial, not once per render) and returns `<Navigate to={/tenants/${tenantId}} replace />`; an allow returns `children` unchanged.
    - Denial never triggers the guarded page's own queries: `<RequirePerm code={...}><Catalog /></RequirePerm>` in `App.tsx` constructs `<Catalog />` as an unmounted React element (JSX doesn't invoke the component body), and `RequirePerm` returns `<Navigate>` instead of rendering that element when denied — React never mounts `Catalog`, so none of its hooks/queries ever run.
    - `query.ts`: `retry: 1` (a fixed number) replaced with a function — `false` when the error is an `ApiError` with `status` 403 or 404 (a denial/absence, not a fluke), otherwise the original `failureCount < 1` (one retry, same as before) for everything else including 5xx and network errors.
    - `App.tsx`: every route whose section has a `view` code in D3's table is wrapped — branches, catalog (+ detail), inventory, customers (+ detail), suppliers (+ detail), orders (+ new + detail), reports, and conflicts (hidden from nav per T110 but still in D3's table, so still guarded here — direct-link/deep-link access, e.g. from the notifications bell, still needs `conflicts.view`). Overview, Company, Download, and Settings stay unwrapped, matching T110's nav-visibility list (Company's page itself has no `view` code — `company.manage` gates only the edit form, per T115).
    - `orders/new` carries the same `PERM.OrdersView` guard as `orders` and `orders/:orderId` (D3 has no separate code for the create sub-route) — this is the guard T115's own acceptance criterion ("`/orders/new` by direct URL is refused by T111's guard") depends on.
  - Acceptance:
    - [x] Typing `/tenants/{id}/catalog` without `catalog.view` redirects to Overview with an explanatory toast and issues **no** catalog request (verified by code inspection — see Verify; no live non-owner session was available this session to click through)
    - [x] A 403 or 404 is never retried; a 500 still is (`query.ts`'s `retry` function checks `error.status` explicitly for those two codes only)
    - [x] Every route in `App.tsx` with a matching `view` code is wrapped — checked against the D3 table, not by eye (see the section-by-section list above; Company/Overview/Download/Settings deliberately excluded, matching D3's "no code" list from T110)
  - Verify: `pnpm build && pnpm lint` — both clean (`tsc -b && vite build` succeeds; lint shows only the two pre-existing `auth.tsx` react-refresh warnings, no new ones). Manual direct-URL-entry testing was **not** performed this session: no dev backend was running and no non-owner member with a restricted role exists yet to test denial against — creating one needs either T112/T113's role UI (not built yet) or hand-rolled API calls plus a second logged-in session, which felt like more infra than this task warranted on its own. In lieu of that, verified the denial path by direct code inspection: `RequirePerm` returns `<Navigate>` in place of `children` before React ever mounts the guarded page, so the guarded page's hooks/queries structurally cannot run on denial (traced in the Implementation notes above), and the redirect target/toast call were read back from the component rather than observed live. The real click-through — including the "no request in the network tab" and "toast text is readable" checks — is exactly what checkpoint 13b's second-browser-profile pass already covers once a "قراءة فقط" role exists (T112/T113), so it's deferred there rather than duplicated here.
  - Files: `console/src/components/RequirePerm.tsx` (new), `console/src/App.tsx`, `console/src/lib/query.ts`
  - Dependencies: T110 · **Size: S**

- [x] **T112: Settings roles tab + `RoleFormDialog` permission grid**
  - **Description:** Settings gains a roles tab beside members: list with assigned counts, create, edit, delete. `RoleFormDialog` is name + the per-section checkbox grid from spec D3, where ticking إدارة auto-ticks عرض and unticking عرض unticks إدارة. A delete on an assigned role surfaces the 409's count as a readable message, not a generic failure.
  - Implementation notes:
    - No client plumbing existed yet for roles at all (T107 only built the API side) — added it from scratch: `types.ts` gained `RoleView` (mirrors `tenant.RoleView` field-for-field); `api.ts` gained `listRoles`/`permissions`/`createRole`/`updateRole` on the generic `request()` helper, plus a hand-rolled `deleteRole` (same reason `createOrder` bypasses `request()`: the D8 409 refusal carries a structured `count`, not just an `error` string) and a new `RoleAssignedError extends ApiError` to carry it, mirroring `OrderUnavailableError`'s existing pattern; `query.ts` gained `qk.roles`/`qk.permissions`; `hooks.ts` gained `useRoles`/`usePermissions`/`useCreateRole`/`useUpdateRole`/`useDeleteRole` — `useUpdateRole` also invalidates `qk.bundle(tenantId)` since a permission-set change should reach `useScope`/`useCan` (T110) for anyone holding that role, not just the roles list. All wire shapes (JSON keys, the 409 body's `count` field) were read directly from `api/internal/httpapi/role_handlers.go` and `service_roles.go` rather than guessed.
    - No tabs primitive exists in `components/ui/` (no `@radix-ui/react-tabs` dependency either) — reused the plain `useState` + button-group pattern `Reports.tsx` already uses for its period switcher, instead of adding a new dependency for one two-way toggle. Same reasoning for the delete confirmation: no `AlertDialog` component exists anywhere in this codebase and the one destructive precedent (`revokeMember` in this same file) fires directly from a dropdown item with no "are you sure" step — `RoleFormDialog`'s delete follows that same convention rather than introducing a new confirmation pattern for this one case.
    - `RoleFormDialog` grid is driven by the *live* `GET …/permissions` catalog, not the static `PERM` mirror: `SECTIONS` (name → `view`/`manage` code pair, D3's table order) is filtered to `visibleSections` — rows/cells only render for codes the catalog actually contains — and any catalog code absent from `SECTIONS` renders as its own row labeled with the raw code (`extraCodes`), so an unlabelled new code degrades gracefully instead of disappearing.
    - إدارة⇄عرض is a plain `Set<string>` toggle: ticking إدارة adds both codes, unticking عرض removes both; this is UI convenience only — the server (`perm.Normalize`) is what actually enforces the rule on save, the client never asserts it did the normalizing. Empty-selection submit is blocked client-side with the literal string `perm.ErrEmptyPermissions` produces server-side ("perm: at least one permission is required"), so the message reads identically regardless of which side would have caught it.
    - State-reset avoided an effect entirely (an eslint `react-hooks/set-state-in-effect` violation on the first pass — flagged, then fixed): `Settings.tsx` keys the edit-mode `RoleFormDialog` instance by `editing?.id ?? 'create'`, so switching which role is being edited remounts the dialog with fresh initial state instead of syncing via effect; the dialog's own `onOpenChange` resets its local `selected`/`permError`/form fields back to its current target on close, covering the one case remounting doesn't (cancel an in-progress edit, reopen the same role).
    - The whole roles tab (switcher + panel + `RoleActions`' "دور جديد" button) is gated on the same `isOwner` computation the members table already used — `activeTab` itself is forced to `'members'` whenever `!isOwner`, so a non-owner can never land on the roles panel even by leftover local state.
  - Acceptance:
    - [x] Grid renders exactly the codes `GET …/permissions` returns; an unlabelled new code renders as its raw code rather than disappearing (`visibleSections`/`extraCodes` computed from the live catalog, not the static `PERM` object — see notes)
    - [x] إدارة ⇄ عرض ticking is enforced in the UI, and submitting with nothing ticked is blocked client-side with the same message the server would give (`toggleView`/`toggleManage`; `EMPTY_PERMISSIONS_MESSAGE` matches `perm.ErrEmptyPermissions` verbatim)
    - [x] The whole tab is hidden for non-owners (the server is still the gate) (`activeTab = isOwner ? tab : 'members'`; switcher and `RoleActions` button both conditioned on `isOwner`; server-side `ownerRule` on the three write routes is unchanged)
    - [x] Deleting an assigned role shows «مُسند إلى N عضو» and leaves the role intact (`RoleAssignedError.count` read in `handleDelete`'s `onError`; the role is only removed from the cache in `onSuccess`, so a 409 leaves `qk.roles` — and the list — untouched)
  - Verify: `pnpm build && pnpm lint` — both clean (`tsc -b && vite build` succeeds; lint shows only the two pre-existing `auth.tsx` react-refresh warnings, no new ones — after fixing one real `react-hooks/set-state-in-effect` error the first lint pass caught, see Implementation notes). Manual click-through (create «قراءة فقط», edit it, delete while assigned) was **not** performed — same gap as T111: no dev backend is running this session. Every wire shape was instead cross-checked directly against the Go handlers/service (`role_handlers.go`, `service_roles.go`) rather than assumed, and the three acceptance behaviors that don't depend on a live server (grid rendering from a mocked catalog shape, the toggle rule, the empty-selection block) were verified by re-reading the component logic line by line, not by opening a browser. The real manual pass is checkpoint 13b's second-browser-profile walkthrough, which exercises this tab as its first step (create the role) — flagged there, not duplicated here.
  - Files: `console/src/pages/console/Settings.tsx`, `console/src/components/RoleFormDialog.tsx` (new), `console/src/lib/types.ts`, `console/src/lib/api.ts`, `console/src/lib/hooks.ts`, `console/src/lib/query.ts`
  - Dependencies: T107, T110 · **Size: M**

- [x] **T113: `AssignRoleDialog` + role/branch columns on the members table**
  - **Description:** Members table grows role and branch-scope columns; a row action opens `AssignRoleDialog` (role select; the branch allowlist picker itself lands in T123 with the rest of scoping). Owner rows show a non-editable «المالك» badge.
  - Implementation notes:
    - `types.ts`'s `Member` gained `role_id?`/`role_name?`/`branch_ids: string[]`, matching `tenant.MemberView`'s wire shape exactly (T108 already serializes these; the console just hadn't been taught about them yet) — `branch_ids` has no `omitempty` on the Go side, so it's typed non-optional here too, with `[] = every branch` per the wire contract already established for `me.branch_ids`.
    - `api.ts` gained `assignMemberRole` (`PATCH …/members/{memberId}`). Unlike `deleteRole`, this goes through the generic `request()` helper rather than a hand-rolled `rawFetch` — the owner-row/unknown-role/unknown-branch refusals (`ErrCannotModifyOwner`/`ErrUnknownRole`/`ErrUnknownBranch`) are all plain `writeErr` strings on the Go side (checked directly in `writeTenantError`), no structured body to preserve.
    - `hooks.ts`'s `useAssignMemberRole` patches the one row in the `qk.members` cache via `setQueryData` (`prev?.map(...)`) instead of invalidating — satisfies the "no full page refetch" acceptance criterion directly, same pattern `useUpdateRole`/`useDeleteRole` (T112) already use for their own lists. D7 ("next request, no session action") needed nothing client-side — T109 already proved server-side that a fresh `perm.Scope` is resolved per request; there's no session/token for the console to refresh.
    - `AssignRoleDialog.tsx` (new): role-only — a single RHF+zod `<select>` (native, no dropdown/combobox component exists in the codebase; same convention `CreateProductDialog`'s group select already uses) populated from `useRoles(tenantId)`, required with a client-side message. Always resends the member's current `branch_ids` unchanged alongside the new `role_id` so a role reassignment can never silently reset someone's branch scope — T123 is the only task allowed to touch that array. One dialog instance per open (Settings.tsx only mounts it while a member is selected, keyed by `assigning.id`), so there's no create/edit-mode split like `RoleFormDialog` needed — `member` is always the current target.
    - `Settings.tsx`: added two columns to the members table — «الدور المخصص» (the RBAC role: `المالك` badge for the owner row, `role_name` text for an assigned member, «بدون دور» badge for the defensive no-role case) and «الفروع» (`كل الفروع` when `branch_ids` is empty, else an Arabic-digit branch count) — kept the pre-existing «الدور» column (owner/member account-level badge) as-is rather than overloading it, since it answers a different question. Added a «تعيين دور» dropdown item above the existing «إلغاء العضوية» one, gated by the same `m.role !== 'owner'` check that already hid revoke for the owner row — one guard now covers both actions, directly satisfying "the owner row offers no assign action and no revoke action" without a second condition to keep in sync.
  - Acceptance:
    - [x] Assigning a role updates the row without a full page refetch and takes effect for that member on their next request (`useAssignMemberRole`'s `setQueryData` row-patch; "next request" is T109's server-side guarantee, nothing new needed client-side — see notes)
    - [x] The owner row offers no assign action and no revoke action (both dropdown items live inside the same `{m.role !== 'owner' && <DropdownMenu>...}` block that already gated revoke)
    - [x] A member with no role yet renders a visible «بدون دور» state rather than an empty cell (`m.role_id ? m.role_name : <Badge tone="muted">بدون دور</Badge>` in the «الدور المخصص» cell)
  - Verify: `pnpm build && pnpm lint` — both clean (`tsc -b && vite build` succeeds; lint shows only the two pre-existing `auth.tsx` react-refresh warnings, no new ones — no lint errors this time, unlike T112). Manual promote-and-observe was **not** performed this session: no dev backend was running, so there was no live tenant/second browser profile to test against. Every wire shape (`role_id`/`role_name`/`branch_ids` on `MemberView`, the PATCH body, and which refusals are plain-string vs. structured) was instead cross-checked directly against `service_members.go`, `tenant_handlers.go`'s `writeTenantError`, and `middleware.go`'s route table rather than assumed. This is the same class of gap as T111/T112, and is exactly what checkpoint 13b's second-browser-profile pass is designed to close — deferred there, not duplicated here.
  - Files: `console/src/components/AssignRoleDialog.tsx` (new), `console/src/pages/console/Settings.tsx`, `console/src/lib/types.ts`, `console/src/lib/api.ts`, `console/src/lib/hooks.ts`
  - Dependencies: T109, T112 · **Size: M**

- [x] **T114: composition gating — Overview, notifications bell, Ctrl+K (spec D10)**
  - **Description:** Overview fetches `hq-branches`, `inventory-attention`, `conflicts`, and `subscription` (`Overview.tsx:47-53`); `deriveAlerts` (`lib/alerts.ts:30`) feeds the bell from the same inputs; Ctrl+K searches products (`CommandPalette.tsx:114`). Left alone, all three hand a reports-only member the full inventory and conflict picture. Each tile, alert source, and search category becomes conditional on its own section's `view`, and **the query is not issued at all** when the permission is absent — the page degrades to what the member may see rather than 403ing as a whole.
  - Implementation notes:
    - All three files read `useScope`/`can` (T110) the same way `RequirePerm` (T111) and `AppShell`'s nav filter already do, gating on `PERM.BranchesView`/`PERM.InventoryView`/`PERM.ConflictsView`/`PERM.CatalogView`. `subscription` is left ungated everywhere — D3 explicitly gives billing no code at all ("owner-only, never in the catalog"), and per the same reasoning T115 already applied to Company (no `view` code ⇒ visible read-only to every member), a billing-status banner is metadata, not a section's resource.
    - **The query itself is disabled, not just its output hidden**: `useHqBranches(canBranches ? tenantId : undefined)` and the equivalent for inventory-attention/conflicts/catalog-products — every one of these hooks already has an `enabled: !!tenantId` guard, so passing `undefined` in place of the tenant id is enough to stop the request from ever firing; no new `enabled` prop needed anywhere.
    - `lib/alerts.ts`'s `DeriveAlertsInput.branches` became optional (`branches?: BranchView[]`, defaulting to `[]` in both loops) — previously `NotificationsBell` required a truthy `hq` before deriving *any* alert, which would have permanently hidden conflict/inventory alerts for a member without `branches.view` (their `hq` query is now permanently disabled, so `hq` is permanently `undefined`). Decoupling `branches` from the other three inputs means each source degrades independently, matching D10's "a composed view is not a data source" rule literally per-input, not per-page.
    - Overview's alerts panel needed its own "loading vs. denied vs. empty" distinction: `alertsLoading` is true only while a *permitted* query (`canX && xQuery.isLoading`) is still in flight — a disabled query's `isLoading` is always `false` (TanStack: `isLoading` = `isPending && isFetching`, and a disabled query never fetches), so a denied section can never hold `AlertsPanel` on its skeleton state forever, which was the bug the naive `hq ? ... : undefined` gate above it would have had once `hq` could be permanently `undefined`.
    - Overview's "اليوم" KPI section and branch-health strip are both sourced entirely from `hq-branches`, so both are wrapped in `canBranches` directly (not just left to silently skeleton on missing data) — a `reports.view`-only member sees neither section at all, not four permanently-loading `KpiCard`s.
    - Deliberately **not** gated: Overview's «الفروع» `StatCard` (branch count/active count) and its «النشاط التجاري»/«مكان البيانات» siblings — all four read straight off `bundle` (already fetched for every member regardless of role, same as `SetupGate`), not off `hq-branches` or any other section query, so there's nothing to disable; and CommandPalette's «الصفحات» and «إجراءات» sections — both are static navigation shortcuts to routes `RequirePerm` (T111) already redirects away from on denial, so nothing is disclosed by listing them, and the task's acceptance criteria name only product and branch *results*, not page links.
  - Acceptance:
    - [x] A member with only `reports.view` opens Overview and the network tab shows no request to inventory-attention or conflicts, and neither tile nor alert renders (verified by code inspection — see Verify; `canInventory`/`canConflicts` both `false` ⇒ `useInventoryAttention`/`useConflicts` called with `tenantId: undefined` ⇒ disabled; KPI/health sections and their alerts likewise absent since `canBranches` is also `false` for a reports-only role)
    - [x] The bell's unread count reflects only permitted alert sources (`NotificationsBell` computes the same three `canX` flags and passes `undefined` for any input the member lacks — `deriveAlerts` treats a missing input as "contributes nothing", never as "count anyway")
    - [x] Ctrl+K shows no product results without `catalog.view` and no branch results without `branches.view` (`productsQuery`'s tenant id and the `branches` list computation are both gated on `canCatalog`/`canBranches`)
    - [x] An owner sees Overview, the bell, and Ctrl+K exactly as today (an owner's `me.permissions` is the full catalog per T108, so every `canX` flag is `true` and every gated branch behaves as before — no code path changed for that case)
  - Verify: `pnpm build && pnpm lint` — both clean (`tsc -b && vite build` succeeds; lint shows only the two pre-existing `auth.tsx` react-refresh warnings, no new ones). Manual with the network tab open was **not** performed this session — same recurring gap as T111-T113: no dev backend running, and no restricted-role member exists yet to sign in as (that's still checkpoint 13b's job, once an invite flow or a second manually-crafted account exists to test with). Verified instead by tracing each `canX` flag through to the hook call it disables and confirming every hook already had its own `enabled: !!tenantId` guard (T110-era code, unchanged), so passing `undefined` is sufficient and no `request()` call can fire.
  - Files: `console/src/pages/console/Overview.tsx`, `console/src/lib/alerts.ts`, `console/src/components/NotificationsBell.tsx`, `console/src/components/CommandPalette.tsx`
  - Dependencies: T110 · **Size: M**

- [x] **T115: manage-gating A — Catalog, Company, Branches**
  - **Description:** Hide write affordances behind `can('X.manage')`: product create, price edit, group actions; company profile edit; add/rename branch, bind device, release seat. Split by page group across T115–T117 because one sweeping "gate every button" task would touch a dozen files and be unreviewable — and these are the edits most likely to miss a spot.
  - Implementation notes:
    - Every gate uses `useCan(tenantId, PERM.X)` (T110) directly — a single boolean per page, not the list-filtering `can()`+`useScope()` pair T114 needed, since each of these pages checks exactly one permission for itself rather than filtering a collection of items with mixed codes.
    - Confirmed the exact write route each affordance drives, and its required code, straight from `middleware.go` before gating anything (no guessing): `POST /branches` and `PATCH /branches/{branchId}` (rename **and** status toggle — the same route serves both, per `SetBranchStatus`) both require `branches.manage`; `POST /hq/catalog/products` and `PUT /hq/catalog/products/{productId}/prices` both require `catalog.manage`; `PUT /company` requires `company.manage`. This is also why the branch status toggle (تعطيل/تفعيل الفرع) got gated even though the task description only names "add/rename branch" — it shares `PATCH /branches/{branchId}` with rename, so a `branches.view`-only member hitting it would just get a 403 today.
    - **Catalog.tsx**: «منتج جديد» button and `CreateProductDialog` both conditioned on `canManage` (`PERM.CatalogManage`); denied members still get the full read path (groups, search, table, pagination) unchanged.
    - **ProductDetail.tsx**: the units table's trailing actions column (header cell and each row's «تعديل» button) is omitted entirely rather than left empty when `!canManage`, and `EditUnitPriceDialog` is unmounted alongside it — a denied member sees a clean read-only prices table, not a dead column.
    - **Company.tsx** / **CompanyForm.tsx**: per the acceptance criterion's explicit wording ("read-only rather than absent"), this is the one page in the group that disables in place instead of hiding. `CompanyForm` gained an optional `readOnly` prop (defaults to `false`, so `SetupWizard.tsx`'s other call site — always the owner completing first-time company registration — is untouched): every field gets `disabled={readOnly}`, `autoFocus` is suppressed too, and the submit button is omitted rather than rendered-disabled (nothing to submit if every field is inert). `Company.tsx` passes `readOnly={!useCan(tenantId, PERM.CompanyManage)}`.
    - **Branches.tsx**: «فرع جديد» (both the header action and the empty-state action), each branch card's whole actions dropdown (رename + توglet status), and both `AddBranchDialog`/`RenameBranchDialog` mounts are conditioned on `canManage` (`PERM.BranchesManage`). The day-snapshot, freshness, and seat-count read paths are all untouched.
    - **BranchDetail.tsx**: no changes — read entirely, no write affordance exists on this page today. Its own "الأجهزة والمقاعد" section already says device bind/release happens from the desktop app, not the console (`api.ts`'s `bindDevice`/`releaseDevice` exist server-side but have no console hook or UI anywhere — confirmed by grep before concluding this, not assumed); there is also no rename or status-toggle control here, only on `Branches.tsx`'s card. So "no device bind or release" in this task's acceptance is already true today, vacuously, with nothing to gate — kept on the Files list for visibility since it's the natural place a future console-side bind/release UI would land, and worth a second look then.
  - Acceptance:
    - [x] With `catalog.view` only: no إنشاء منتج, no price-edit affordance on the product detail, and the pages otherwise render and read normally (`canManage` on `Catalog.tsx`'s create button/dialog and `ProductDetail.tsx`'s edit column/dialog; no other code path touched)
    - [x] With `branches.view` only: no إضافة فرع, no rename, no device bind or release (`canManage` on `Branches.tsx`'s add button and per-card dropdown, which was also the only place rename lived; device bind/release has no console UI at all yet — see notes)
    - [x] Without `company.manage`: the company form is read-only rather than absent, so the data is still visible (`CompanyForm`'s `readOnly` prop disables every field and drops the submit button, but the page, card, and populated values all render unchanged)
  - Verify: `pnpm build && pnpm lint` — both clean (`tsc -b && vite build` succeeds; lint shows only the two pre-existing `auth.tsx` react-refresh warnings, no new ones). Manual per-role click-through was **not** performed this session — same recurring gap as T111-T114: no dev backend running and no restricted-role member exists yet to sign in as. Verified instead by (a) reading each gated affordance's exact server route and required permission code directly out of `middleware.go` rather than assuming the task description's list was complete (this is what surfaced the branch status-toggle case), and (b) tracing every `canManage` flag to the JSX it actually guards. Deferred to checkpoint 13b's second-browser-profile pass, same as every prior RBAC task.
  - Files: `console/src/pages/console/Catalog.tsx`, `console/src/pages/console/ProductDetail.tsx`, `console/src/pages/console/Company.tsx`, `console/src/components/CompanyForm.tsx`, `console/src/pages/console/Branches.tsx`, `console/src/pages/console/BranchDetail.tsx` (no changes)
  - Dependencies: T110 · **Size: M**

- [x] **T116: manage-gating B — Customers, Suppliers**
  - **Description:** Same pass for create, edit, deactivate, bulk actions, and import. **Export follows spec OQ3** — `customers.view` permits it unless that question is answered otherwise before this task starts.
  - Implementation notes:
    - Checked spec-console-rbac.md's OQ3 before starting: still open ("Confirm."), unresolved by anything landed since T110. Per the task description's own fallback rule, proceeded with the spec's stated default — export stays permitted under `X.view` alone — and confirmed the server agrees: `middleware.go`'s `rule(http.MethodGet, "hq/customers/export", perm.CustomersView)` (and the identical `suppliers/export` line) already gate export on `.view`, not `.manage`, so no server-side change was needed either. Left an inline comment at both export buttons pointing at OQ3 so a future resolution finds the spot immediately.
    - Every other write route was read directly out of `middleware.go` before gating anything, same discipline as T115: `POST hq/customers`, `PUT hq/customers/bulk`, `POST hq/customers/import`, and `PUT hq/customers/{id}` all require `customers.manage` (suppliers mirrors this exactly with `suppliers.manage`) — confirming create, bulk, import, and edit/deactivate (deactivate is a field on the same `PUT .../{id}` the edit dialog uses, not a separate route) are all one gate each.
    - Customers.tsx/Suppliers.tsx are near-identical page pairs (same component shape, duplicated per entity), so the same `canManage = useCan(tenantId, PERM.CustomersManage)` (or `PERM.SuppliersManage`) gate was applied in parallel at four spots per page: the header/empty-state create button + create dialog, the «استيراد» button + import dialog, and — the one design call not spelled out in D3 — the row-selection checkbox column itself. Selection has no purpose without `BulkActionsBar`/`SupplierBulkActionsBar`, both of which only ever fire `.manage`-gated bulk-update calls, so hiding the checkboxes (`canSelect` prop threaded into `CustomersTable`/`SuppliersTable`) removes the bulk bar's only trigger without needing a separate gate on the bar itself — `selected` can never become non-empty for a denied member.
    - CustomerDetail.tsx/SupplierDetail.tsx: the «تعديل»/«تعطيل» (or «تفعيل») button pair and the corresponding `EditCustomerDialog`/`EditSupplierDialog` mount are wrapped in one `canManage` check each — same one-guard-covers-both-actions pattern T113 used for the owner row's assign/revoke pair. Purchases, ledger, and every other read path are untouched.
  - Acceptance:
    - [x] With `customers.view` only: no create, edit, bulk bar, or import; the list, profile, purchases, and ledger all read normally (four independent `canManage` gates on `Customers.tsx`/`CustomerDetail.tsx`; bulk bar specifically unreachable since its only trigger — row selection — is itself gated)
    - [x] Same for suppliers, with `suppliers.view`/`suppliers.manage` (identical structure applied to `Suppliers.tsx`/`SupplierDetail.tsx`)
    - [x] Export behaviour matches whatever OQ3 resolves to, and the task is not started before it does (OQ3 confirmed still open before starting; export left ungated per the spec's own stated default, matching the server's existing `.view`-only route guard — see notes)
  - Verify: `pnpm build && pnpm lint` — both clean (`tsc -b && vite build` succeeds; lint shows only the two pre-existing `auth.tsx` react-refresh warnings, no new ones). Manual per-role click-through was **not** performed this session — same recurring gap as T111-T115: no dev backend running and no restricted-role member exists yet to sign in as. Verified instead by reading every gated write's exact route and required permission code out of `middleware.go` (including the export routes, to confirm OQ3's default matches server behavior) and tracing each `canManage`/`canSelect` flag to the JSX it guards. Deferred to checkpoint 13b's second-browser-profile pass, same as every prior RBAC task.
  - Files: `console/src/pages/console/Customers.tsx`, `console/src/pages/console/CustomerDetail.tsx`, `console/src/pages/console/Suppliers.tsx`, `console/src/pages/console/SupplierDetail.tsx`
  - Dependencies: T110 · **Size: M**

- [x] **T117: manage-gating C — Orders**
  - **Description:** `orders.manage` gates طلب جديد, cancel, and transfer. `orders.view` alone still opens the list and the detail, including the customer name, phone, and address embedded in the order — **spec D10 rule 2**: data that is part of a row the member may read is not separately gated; what `customers.view` gates is the customer as a separate resource (the profile link, the list, the ledger).
  - Implementation notes:
    - Confirmed the exact routes straight from `middleware.go` before gating: `POST hq/orders` (create), `POST hq/orders/{orderId}/cancel`, and `POST hq/orders/{orderId}/transfer` all require `orders.manage`; `GET hq/orders`, `GET hq/orders/{orderId}` require only `orders.view`. Also noted `GET hq/orders/availability` and `GET hq/orders/delivery-fee` — used only by the New Order form itself — are already gated on `orders.manage` server-side, not `orders.view`, which is corroborating evidence that the create flow as a whole was always meant to sit behind `.manage`.
    - **`orders/new`'s route guard was wrong before this task and this task is what fixes it**: T111 had wired it to `PERM.OrdersView` (its own notes said D3 has no separate code for the create sub-route, which is true, but the conclusion drawn — reuse `.view` — doesn't hold once `.manage` exists and gates the create button). Changed `App.tsx`'s `orders/new` route to `RequirePerm code={PERM.OrdersManage}`, which is what makes this task's third acceptance criterion true: a `orders.view`-only member hitting `/orders/new` directly is now redirected by `RequirePerm` itself, not merely kept from finding a link to it.
    - **Orders.tsx**: `canManage = useCan(tenantId, PERM.OrdersManage)` gates the sole «طلب جديد» entry point (the header action `Link to="new"`); confirmed by grep it's the only place in the app that links to `orders/new`. The empty-state for a filtered-to-nothing list has no create action to gate. List, search, filters, stat tiles, and row-click-through to detail are all untouched.
    - **OrderDetail.tsx**: same `canManage` flag now additionally guards the transfer/cancel button pair (`isNew && canManage`, on top of the existing `isNew` status check) and both dialog mounts (`CancelOrderDialog`/`TransferOrderDialog`), so a denied member gets no dead buttons and no dialogs mounted underneath. Separately added `canViewCustomers = useCan(tenantId, PERM.CustomersView)`: the customer name in the summary line is now a `Link` to `/tenants/{id}/customers/{partner_id}` when `canViewCustomers && o.partner_id` is true, and plain text otherwise — this is the "link through to the profile is absent" half of the acceptance criterion; there was no such link at all before this task (`partner_id` was already on `OrderDetail` but unused), so this is a small net-new affordance rather than a hide of an existing one. Phone/address/branch/created-by stay unconditional per D10 rule 2 — they're fields on the order row itself, not a reach into the customer resource.
    - **NewOrder.tsx**: no changes. The entire page is now reachable only through the `orders/new` route's `RequirePerm` guard (see above), so there is no internal affordance left to gate — unlike `BranchDetail.tsx` in T115, this one *did* need a fix, just at the route level rather than inside the component.
  - Acceptance:
    - [x] With `orders.view` only: no طلب جديد entry point anywhere, no cancel, no transfer; list and detail read normally (`canManage` on `Orders.tsx`'s header link and `OrderDetail.tsx`'s action pair + both dialog mounts; nothing else touched)
    - [x] An order detail shows its customer's name/phone/address **without** `customers.view`, but the link through to that customer's profile is absent (`canViewCustomers` gates only the `Link` wrapper around the name; phone/address/branch render unconditionally, per D10 rule 2)
    - [x] `/orders/new` by direct URL is refused by T111's guard, not just hidden (`App.tsx`'s `orders/new` route now requires `PERM.OrdersManage`, not `PERM.OrdersView` as T111 had originally wired it)
  - Verify: `pnpm build && pnpm lint` — both clean (`tsc -b && vite build` succeeds; lint shows only the two pre-existing `auth.tsx` react-refresh warnings, no new ones). Manual per-role click-through **not** performed this session — same recurring gap as T111-T116: no dev backend running, no restricted-role member exists yet. Deferred to checkpoint 13b, same as every prior RBAC task in this group.
  - Files: `console/src/pages/console/Orders.tsx`, `console/src/pages/console/OrderDetail.tsx`, `console/src/pages/console/NewOrder.tsx` (no changes), `console/src/App.tsx`
  - Dependencies: T110 · **Size: M**

- [x] **Checkpoint 13b — Group B complete: roles usable end-to-end**
  - [x] `make test && make vet` and `pnpm build && pnpm lint` green — 2026-08-26. `go vet ./...` clean; `make test` all packages pass (`store/mongo`'s integration tests still skip cleanly, `TEST_MONGO_URI` unset — same gap noted since T102, needs a human pass with real Mongo); `pnpm build && pnpm lint` clean, only the two pre-existing `auth.tsx` react-refresh warnings.
  - [x] **Manual, second browser profile:** create «قراءة فقط», assign it, sign in as that member — nav shows only permitted sections, direct URLs redirect, no write button anywhere, Overview and the bell show only permitted tiles — verified by user, 2026-08-27
  - [x] Promote that member to «وصول كامل» and confirm the change lands on their next navigation with no re-login — verified by user, 2026-08-27
  - [x] Owner's own experience is unchanged from before Phase 13 — verified by user, 2026-08-27
  - [x] **Human review — group B** — signed off by user, 2026-08-27

### Group C — branch scoping

- [x] **T118: gateway — widen `branchId` to a branch list (no-op commit)**
  - **Description:** **No dependencies — build and merge on day one.** Eight copies of `Guid? branchId = Guid.TryParse(qs["branch_id"].FirstOrDefault(), out var bid) ? bid : null;` in `Program.cs` collapse into one `BranchScope.From(qs)` reading **every** `branch_id` occurrence (repeated params already have precedent here — the customer-import endpoint uses them over CSV, `Program.cs:1153-1155`). The 12 `Guid? branchId` signatures in `HqApi.cs` become `IReadOnlyList<Guid> branchIds`, and each `q.Where(x => x.BranchId == b)` becomes `q.Where(x => branchIds.Contains(x.BranchId))`, which EF translates to SQL `IN` on both dialects. An empty list skips the filter, preserving today's "all branches" — which is what makes this a no-op.
  - Implementation notes:
    - **The description undercounted the `Program.cs` copies**: a precise grep for the exact declaration line found **ten**, not eight (`Program.cs:647,826,866,994,1032,1094,1218,1379,1546,1795`) — flagged here rather than silently building to whichever number, same call as T101's "14 vs 15" catalog-count discrepancy. All ten collapse into `BranchScope.From(qs)`; two nearby `Guid? branchId` sites were deliberately **not** touched — `GetOrderAvailabilityAsync` (availability check) and `PreviewDeliveryFeeAsync` (delivery-fee preview) both require exactly one branch as a write/compute target, not a read filter, so they keep `Guid branchId` (non-nullable, required) untouched. Same reasoning for `EnsurePartnerAtBranchAsync`, `ResolveDeliveryFeeAsync`, `BranchExistsAsync`, and the customer-create path (`Program.cs:1597`) — all single-branch-target parameters, out of this task's scope by the acceptance criteria's own wording ("12 `Guid? branchId` signatures", not every `branchId` in the file).
    - `BranchScope.cs` (new): a small static class, `public static IReadOnlyList<Guid> From(IQueryCollection qs)`, following the exact file/style precedent of `OrderReference.cs` (one static helper class, one file, `AribSyncGateway` namespace) rather than a local function buried in `Program.cs` — matches the task description's own naming (`BranchScope.From(qs)`). Mirrors the parsing shape `GetOrderAvailabilityAsync`'s `product_id` handling already uses (`Program.cs:1734-1736`): `Guid.TryParse` per value, drop failures, dedupe, `ToList()`. A single `branch_id` yields a one-element list (old single-branch filter, unchanged); no `branch_id` yields `[]` (old "all branches", unchanged) — this is the no-op contract acceptance criterion 2 depends on.
    - `HqApi.cs`: all 12 `Guid? branchId` signatures (`OrdersAsync`, `InventoryAttentionAsync`, `InventoryProductsAsync`, `ProductMovementsAsync`, `SalesReportAsync`, the private `PeriodSaleEntries`, `ProductsReportAsync`, `StaffReportAsync`, the private `CustomerSales`, `CustomersAsync`, `CustomerInsightsAsync`, `CustomerExportAsync`) became `IReadOnlyList<Guid> branchIds`; every `branchId is Guid b => q.Where(x => x.BranchId == b)` became `branchIds.Count > 0 => q.Where(x => branchIds.Contains(x.BranchId))`, and `InventoryProductsAsync`'s inline `branchId == null || w.BranchId == branchId` sub-queries became `branchIds.Count == 0 || branchIds.Contains(w.BranchId)` — same "empty means unfiltered" contract throughout, `.Contains()` over a `List<Guid>` closure is what EF Core translates to SQL `IN` on both SQL Server and Postgres.
    - Two call sites into the now-widened `CustomerSales` helper (`CustomerAsync`, `CustomerPurchasesAsync`) pass `[]` rather than `branchIds`, unchanged from before — both already anchor on one `customerId`, so a branch filter is redundant there; a branch-scope check for a single customer detail/purchases page is T120's row-level-404 job, not this no-op commit's.
    - **No API/route layer touched** — `hq_handlers.go` and every console/API caller are unaffected; this commit only widens the gateway's own internal filter shape, exactly as the acceptance criteria require.
  - Acceptance:
    - [x] `dotnet build AribSyncGateway.csproj` clean; all 12 signatures converted, no `Guid? branchId` left in `HqApi.cs` (confirmed 0 matches by grep after the edits; the remaining `branchId` occurrences are all non-nullable single-branch-target parameters, listed above)
    - [x] A single `branch_id` behaves exactly as before; no `branch_id` behaves exactly as before; **no API change accompanies this commit** (verified by tracing `BranchScope.From`'s one-element/empty-list cases against the old single-value/null cases — see notes; not run against a live dev gateway this session)
    - [ ] Repeated `branch_id=a&branch_id=b` filters to both — **not verified by hand**; no dev gateway or dev tenant with ≥2 branches was available this session. `BranchScope.From` collecting every `qs["branch_id"]` value (mirroring the already-shipped `product_id` handling on `/hq/orders/availability`) and EF's `.Contains()` → SQL `IN` translation give high confidence, but this is exactly the kind of claim the task's own Verify line asks to be curl-checked against a real ≥2-branch tenant, not just reasoned about — left open for a human pass, same category of gap as checkpoint 13b's manual browser test.
  - Verify: `dotnet build AribSyncGateway.csproj` — succeeds, 0 errors (20 pre-existing `NU1903` NuGet advisory warnings in unrelated dependency projects, not introduced by this change). Manual `curl` against a dev gateway for the three cases was **not** performed — no dev gateway/tenant running this session; deferred to a human pass, same gap noted throughout this phase for anything needing live infrastructure.
  - Files: `sync-gateway/HqApi.cs`, `sync-gateway/Program.cs`, `sync-gateway/BranchScope.cs` (new)
  - Dependencies: none (**merge early — de-risks the widest diff in the phase**) · **Size: M**

- [x] **T119: API resolves and injects the branch list on branch-dimensioned endpoints**
  - **Description:** `resolveGateway` already returns the Scope (T105); one `applyScope(params, scope)` helper intersects the member's allowlist with any user-supplied `branch_id` and emits repeated params to the gateway. Applied to reports, inventory, orders, customers, suppliers, branch-activity, and branch-snapshot. **Tier-A master data is deliberately excluded** — products and groups replicate to every branch and have no branch dimension — except that per-branch availability arrays inside a product detail **are** filtered.
  - **Implementation notes:** `applyScope(params url.Values, scope *perm.Scope) (url.Values, error)` (`hq/service.go`, next to `resolveGateway`) is a no-op for `scope == nil || scope.IsUnscoped()`; for a scoped caller it either injects `scope.BranchIDs` as repeated `branch_id` values (T118's `BranchScope.From` reads exactly this shape gateway-side) when the caller supplied none, or checks every caller-supplied `branch_id` against `scope.AllowsBranch` and returns the new `ErrForbiddenScope` on the first miss — never a partial narrow. Wired into every method whose gateway call already accepted a `branch_id` query param: `InventoryProducts`, `InventoryAttention`, `ProductMovements`, `ReportSales`, `ReportProducts`, `ReportStaff`, `Customers`, `CustomerInsights`, `ExportCustomers`, `Suppliers` (`/hq/suppliers` — confirmed gateway-side these share `CustomersAsync`/`CustomerInsightsAsync`/`CustomerExportAsync` via `MapCustomerTypeRoutes(PartnerType.Supplier, ...)`, so T118 already plumbed them despite never naming a "Suppliers" method), `SupplierInsights`, `ExportSuppliers`, and `Orders` (`service_orders.go`).
  - **Endpoints with no gateway-side `branch_id` param got a local registry filter instead**, since their merge already iterates the full branch list fetched from Mongo: `ReportBranches` (`BranchesReportAsync` intentionally never took a branch filter, per T118's own note), `InventoryByBranch` (`/hq/inventory/branch-summary`, same shape — **not in the task's own file list, found by auditing every `s.store.BranchesByTenant` call site and confirmed it belongs in the same "aggregate/KPI" bucket as `ReportBranches`**), `BranchActivity`, and `Branches` (the branch-snapshot KPI feed — switched from `membership.Require` to `membership.RequireScope` since it never went through `resolveGateway` at all, to get a Scope to filter with). `InventoryAttention`'s stale-branch merge previously read `params.Get("branch_id")` (first value only); replaced with a new `branchAllowed(ids []string, id string) bool` helper reading the full `params["branch_id"]` slice, so it narrows by exactly the same list — whether user-supplied or scope-injected — that went to the gateway.
  - **`CatalogProductDetail`** (Tier-A product, but a Tier-B availability array) filters `raw.Availability` post-decode with `scope.AllowsBranch`, since the gateway call itself takes no params — `CatalogProducts` (the product list) is untouched, confirmed its handler whitelist has no `branch_id` at all.
  - **Deliberately left untouched** (T120's domain, not this task's): every detail-by-id read (`CustomerDetail`, `SupplierDetail`, `OrderDetail`, `*Purchases`, `*Ledger`) and every write (`ChangeProductPrices`, `CreateProduct`, `CreateCustomer`/`CreateSupplier`, `BulkUpdate*`, `CreateOrder`, `CancelOrder`, `TransferOrder`, `Import*`) — those need row-level 404s and write-target checks, not a list filter. `Conflicts` is also untouched: spec OQ2 resolves ConflictLog as visible to any `conflicts.view` regardless of branch, by design.
  - **Error contract:** new `hq.ErrForbiddenScope`; `httpapi.writeHqError` maps it to `403 {"code": "forbidden_scope", "error": "..."}`, mirroring T104's `writeForbiddenPermission` shape (spec's error-contract section).
  - Acceptance:
    - [x] A member scoped to two branches sees exactly those in every list, aggregate, KPI, report, and **export** — paging and totals stay correct because the filter is still one SQL query *(stub-gateway tests assert the exact `branch_id` set the gateway receives for `InventoryProducts`, `Orders`, `ExportCustomers`; local-merge tests assert the same for `ReportBranches`, `InventoryByBranch`, `BranchActivity`, `Branches` including `Totals`)*
    - [x] A user-supplied `branch_id` outside the allowlist is refused with `forbidden_scope`, never silently widened or narrowed *(`TestApplyScope_RefusesOutOfAllowlistBranchID` — including a request with one in-scope and one out-of-scope id, refused whole rather than narrowed; `TestInventoryProducts_ScopedMemberRefusedOutOfAllowlist`/`TestOrders_ScopedMemberRefusedOutOfAllowlist` assert the gateway is never even called)*
    - [x] An unscoped member's requests are byte-identical to today's *(`TestApplyScope_UnscopedIsANoOp`, `TestInventoryProducts_UnscopedMemberRequestUnchanged` — no `branch_id` key appears in the outgoing query at all)*
    - [x] A scoped member's product detail shows availability rows only for their branches, while the product list itself is unfiltered *(`TestCatalogProductDetail_ScopedMemberFiltersAvailabilityRows`; `CatalogProducts` unchanged, no test needed since its handler never reads `branch_id`)*
  - Verify: `make test && make vet` — both clean, 2026-08-27; full suite green including every pre-existing test unmodified. `gofmt -l` flags 8 pre-existing struct-tag misalignments in `service.go` on types this task never touched (confirmed via `git stash` — present before this diff too); left alone rather than reformatting unrelated code.
  - Files: `api/internal/hq/service.go`, `api/internal/hq/service_orders.go`, `api/internal/httpapi/hq_handlers.go`, `api/internal/hq/service_test.go`
  - Dependencies: T105, T118 · **Size: M**

- [x] **T120: row-level 404s, write-target checks, and the D5c unscoped-write refusal**
  - **Description:** The three rules that make an allowlist mean something. (1) Detail-by-id (`/hq/{customers,suppliers,orders}/{id}` and their `/purchases`, `/ledger`) returns **404, not 403**, out of allowlist — a scoped member must not be able to probe which ids exist elsewhere. (2) Writes with a target branch check it, **including a transfer's destination**. (3) **Spec D5c:** an operation carrying no branch identity cannot be authorized by an allowlist, so a scoped member is refused `forbidden_unscoped` on price changes, product creation, company edits, branch creation, and any bulk/import touching an out-of-scope row. `ChangeProductPrices` (`hq/service.go:672`) is the case that matters — it is a Tier-A write that reprices **every branch in the tenant**, and without this rule a member scoped to one branch could reprice the company.
  - **Implementation notes:** Four small helpers, next to T119's `applyScope`/`branchAllowed` in `hq/service.go`, cover every case below: `requireBranchInScope(scope, branchID) error` (an explicit write target outside the allowlist → `ErrForbiddenScope`), `hideOutOfScopeRow(scope, branchID) error` (an existing row outside the allowlist → `ErrNotFound`, never `ErrForbiddenScope` — D5b's own reasoning, "a scoped member must not be able to probe which ids exist elsewhere", applies just as much to a write on an id as to reading it), `requireUnscoped(scope) error` (D5c's blanket refusal → new `ErrForbiddenUnscoped`), and `rowBranchID(ctx, shard, dbName, path)` (a branch-only GET, used only when a caller is actually scoped, so an unscoped caller never pays the extra round trip). Every call site threads `scope` (previously discarded as `_`) through `resolveGateway` and gates on `scope != nil && !scope.IsUnscoped()` — identical to T119's pattern, so an unscoped/owner caller's requests stay byte-identical to pre-T120 behaviour (confirmed: every pre-existing owner-only test in `service_test.go`/`service_suppliers_test.go`/`service_orders_test.go` passes unmodified).
  - **D5c wiring:** `ChangeProductPrices` and `CreateProduct` call `requireUnscoped(scope)` before touching the gateway. `httpapi.writeHqError` gained a case mapping `ErrForbiddenUnscoped` to `403 {"code":"forbidden_unscoped"}` (required ripple into `internal/httpapi/hq_handlers.go`, beyond T120's own stated Files line — without it the error falls through to a 500, same category of "required for wiring, not optional" ripple T105/T107 documented).
  - **Row-level 404s:** `CustomerDetail`/`SupplierDetail`/`OrderDetail` check the branch already present in their own decoded response (`raw.BranchID`/`detail.BranchID`) via `hideOutOfScopeRow` — no extra gateway call. `CustomerPurchases`/`CustomerLedger`/`SupplierPurchases`/`SupplierLedger` have no branch in their own response (confirmed against the gateway's `Program.cs` route handlers), so for a scoped caller only, they spend one extra `rowBranchID` GET against `/hq/{customers,suppliers}/{id}` first — D5b names these sub-resources explicitly, so this is the task's own stated scope, not an addition.
  - **Beyond the task's own four acceptance bullets, added for consistency with the same "don't let ids be probed" reasoning** (documented honestly, mirroring T119's `InventoryByBranch` addition): `UpdateCustomer`/`UpdateSupplier` and `CancelOrder` row-scope their existing target the same way (an update/cancel on an out-of-scope row is `ErrNotFound`, via one extra `rowBranchID` GET for a scoped caller) — none of the three are named in D5b's "writes with a target branch" list (create/transfer only) nor in D5c's table, but a scoped member holding `customers.manage`/`orders.manage` could otherwise silently mutate a row at a branch they can't even see, which is exactly the kind of "allowlist is decorative" hole D5c calls out for pricing. `TransferOrder` additionally row-scopes the order's **own current branch** (not just the destination D5b names) for the same reason — a scoped member must not be able to transfer an order they cannot see, even toward a destination they can.
  - **Write-target checks:** `CreateOrder` (`input.BranchID`), `CreateCustomer`/`CreateSupplier` (`input.BranchID`), and `TransferOrder` (`toBranchID`, the destination D5b explicitly calls out) all call `requireBranchInScope` before reaching the gateway.
  - **Bulk/import — D5c's "every affected row is in scope, else refused" (distinct from the table's blanket-refusal rows):** `BulkUpdateCustomers`/`BulkUpdateSuppliers` loop `ids` and call `rowBranchID` per id for a scoped caller, refusing `ErrForbiddenScope` on the first out-of-scope row and never reaching the gateway's bulk write otherwise — permitted, not merely refused-if-scoped, once every row checks out. `ImportCustomers`/`ImportSuppliers` needed a different mechanism: the gateway (`Program.cs:1153`) deliberately keeps `branch_id` **out of the CSV** and instead takes one multipart form field applied to every row in the file, so a scoped import collapses to the same single-explicit-target shape as `CreateCustomer` rather than a per-row scan. New `importFormBranchID(contentType, body)` parses just that one field (`mime`/`mime/multipart`, new imports in `service.go`); the request body is only buffered via `io.ReadAll` for a scoped caller (an unscoped caller's upload still streams straight through, untouched, same "byte-identical" contract as every other unscoped path) — `maxImportBytes` (5 MiB) already bounded the memory cost this buffering adds.
  - **Deliberately left untouched:** `PUT /company` and `POST /branches` — genuinely out of `hq/`'s Files (they live in `tenant`, not `hq`); D5d's four control-plane branch routes are T121, not this task. `Conflicts` is confirmed untouched by design (OQ2) with a new regression test, not just left alone silently.
  - Acceptance:
    - [x] **Scope escape:** a member scoped to branch ٢ holding `catalog.manage` gets `forbidden_unscoped` on a price change and on product create *(`TestChangeProductPrices_ScopedMemberRefusedUnscoped`, `TestCreateProduct_ScopedMemberRefusedUnscoped` — both assert the gateway is never even called, and that an unscoped/owner caller is unaffected)*
    - [x] A customer/supplier/order detail from an out-of-scope branch returns **404 with no error code**, and the same id returns 200 for an unscoped member *(`TestCustomerDetail_ScopedMemberOutOfAllowlistIsNotFound`, `TestSupplierDetail_ScopedMemberOutOfAllowlistIsNotFound`, `TestOrderDetail_ScopedMemberOutOfAllowlistIsNotFound` — each also asserts the in-allowlist id succeeds; `ErrNotFound` already mapped to a bare 404 by pre-existing `writeHqError` code, confirmed by reading it rather than assumed; every pre-existing owner-only `*Detail` test continues to pass unmodified, covering the unscoped-200 half)*
    - [x] Order create at an out-of-scope branch and order transfer **to** an out-of-scope branch both return `forbidden_scope` *(`TestCreateOrder_ScopedMemberTargetBranchChecked`, `TestTransferOrder_ScopedMemberDestinationOutOfAllowlistRefused` — both assert the gateway is never reached for the refused case and is reached once the target is in scope; also covered beyond the bullet: `TestTransferOrder_ScopedMemberOriginOutOfAllowlistIsNotFound` for the order's own out-of-scope branch)*
    - [x] Conflicts behave per spec OQ2's resolution (default: visible to any `conflicts.view`, with the UI saying they are not branch-filtered) *(`TestConflicts_NotBranchFiltered` — a member scoped to one branch still sees both branches' conflict rows; no code change was needed, this is a regression test confirming T119 correctly left `Conflicts` alone)*
  - Verify: `make test && make vet` — both clean, 2026-08-27; full suite green including every pre-existing test unmodified. `gofmt -l` flags the same 8 pre-existing struct-tag misalignments in `service.go` T119 already documented (confirmed identical hunks — `InventoryProductsPage`, `MovementsPage`, `CustomerPurchasesPage`, `CustomerLedgerPage`, `ImportCustomersResult`, `SupplierPurchasesPage`, `SupplierLedgerPage`, `ImportSuppliersResult`), none in code this task touched; left alone.
  - Files: `api/internal/hq/service.go`, `api/internal/hq/service_orders.go`, `api/internal/hq/service_test.go`, `api/internal/hq/service_orders_test.go`, `api/internal/hq/service_suppliers_test.go`, plus the required ripple `api/internal/httpapi/hq_handlers.go` (new `ErrForbiddenUnscoped` → `forbidden_unscoped` mapping)
  - Dependencies: T119 · **Size: M**

- [x] **T121: control-plane branch routes (spec D5d)**
  - **Description:** Four routes are branch-identified, account-session authed, and live in Mongo rather than the gateway — the spec's r1 missed them entirely. `PATCH /branches/{branchId}`, `POST /branches/{branchId}/bind` (consumes a seat), `POST /devices/{deviceId}/release`, and `POST /sync-token`. `tenant.ownedBranch()` (`service.go:1017`, not `:998` — drifted since the spec was written) is the choke point for the first three. **Blocked on spec OQ4** for the fourth: `sync-token` is consumed by the *desktop app*, not the console, so gating it wrongly breaks desktop activation for member accounts.
  - **Implementation notes:** Asked the user whether to apply OQ4's proposed interim rule to `sync-token` now or keep it explicitly blocked; the user chose to keep it blocked pending real confirmation against the desktop activation flow, so `IssueSyncToken` is untouched (a doc comment now says why, so the next reader doesn't mistake the omission for a miss). `ownedBranch` was rewritten to check membership via `membership.RequireScope` directly (mirroring `hq.resolveGateway`'s T105 pattern) instead of `owned()`, so an HTTP request that already ran through `requirePerm` costs zero extra membership reads; it now refuses a branch outside the caller's allowlist with a new `tenant.ErrForbiddenScope`. Because `RenameBranch`, `SetBranchContact`, `SetBranchStatus`, and `BindDevice` **all** route through `ownedBranch`, the fix lands on all four, not just the two the acceptance bullet names — `SetBranchContact`/`SetBranchStatus` getting the same scoping is the same "don't leave the allowlist decorative" reasoning as T120's self-directed additions, not scope creep. `ReleaseDevice` needed its own check since its URL carries no branch at all: it now resolves scope the same way and checks the fetched device's own `BranchID`, refusing `ErrForbiddenScope` before ever reaching `ReleaseBranchDevice` — satisfying the "device's branch, not a client-supplied one" bullet literally, since there is no client-supplied branch in this request to begin with. `writeTenantError` gained the `forbidden_scope` → 403 mapping (same code and Arabic message as `hq`'s, so the console renders identically regardless of which package served the request) — a required ripple beyond the task's literal Files line, same as T120's `hq_handlers.go` ripple.
  - Acceptance:
    - [x] Rename, bind, and release at an out-of-scope branch return `forbidden_scope`; in-scope succeed *(`TestOwnedBranchRoutes_ScopedMemberOutOfAllowlistRefused` covers rename+bind; `TestReleaseDevice_ScopedMemberChecksDeviceOwnBranch` covers release; both assert the in-scope case succeeds and an unscoped caller — the owner — is unaffected)*
    - [x] Device release checks the **device's** branch, not a client-supplied one *(`TestReleaseDevice_ScopedMemberChecksDeviceOwnBranch` — the request has no branch parameter at all; the device bound at the out-of-allowlist branch is refused and confirmed still active, the one at the allowed branch succeeds)*
    - [ ] `sync-token` behaves per OQ4's answer, and this task is not started before it has one — **not started**, by explicit user choice this session rather than a guess; `IssueSyncToken` carries a doc comment explaining why it's deliberately ungated
  - Verify: `make test && make vet` — both clean, 2026-08-27. Unlike T119/T120's Mongo-less sandbox runs, a real Mongo instance was reachable this session (`arib-platform-mongodb-1` via OrbStack DNS, `mongodb://arib:secret@arib-platform-mongodb-1.orb.local:27017/?authSource=admin`), so `internal/tenant`'s `TEST_MONGO_URI`-gated suite — including the two new tests — ran for real against it, not just compiled-and-skipped; full `go test ./...` green across every package. `gofmt -l` clean on every touched file. **Caveat:** no live HTTP round-trip through the running `api`/`console` containers was done this session (no OTP/session-token flow exercised) — the "manual: bind a device as a scoped member at an allowed and a disallowed branch" line is covered by the service-layer integration test against real Mongo, not by an actual curl through the deployed stack; a human pass through the real HTTP path is still open.
  - Files: `api/internal/tenant/service.go`, `api/internal/tenant/service_test.go`, `api/internal/httpapi/tenant_handlers.go`
  - Dependencies: T105, **spec OQ4** (still open — only `sync-token` remains blocked on it) · **Size: S**

- [x] **T122: SSE stream filtered by allowlist**
  - **Description:** Events carry `BranchID` (`hq/events.go`), so this is a predicate at subscribe time: a scoped member's stream drops events for branches they cannot see. Without it, a scoped member's console refetches — and their freshness pills flip — on activity at branches invisible to them.
  - **Implementation notes:** `EventBus`'s subscriber map changed from `map[chan Event]struct{}` to `map[chan Event][]string`, storing each subscriber's branch allowlist alongside its channel. `Subscribe(tenantID, branchIDs []string)` takes the allowlist as a second parameter (nil/empty = unscoped, matching `perm.Scope`'s own "empty means unscoped" contract), fixed for the subscription's lifetime — decided once at connect time, not re-evaluated per event and never sent to the client for it to filter. `Publish` skips a subscriber whose allowlist excludes `e.BranchID` (reusing `branchAllowed`, the same helper `service.go`'s T119 `applyScope` already uses — no second implementation of "empty means everything"); an event with no `BranchID` (none exist yet, but the type allows it) has nothing to filter against and reaches everyone. The one existing caller, `handleTenantEvents` in `httpapi/hq_handlers.go`, needed the caller's `perm.Scope` to get an allowlist from — `hq.CheckOwnership` (previously a bare bool-ish membership check via `membership.RequireRole`) was widened to `membership.RequireScope` and now returns `(*perm.Scope, error)`, mirroring `resolveGateway`'s shape but without `resolveGateway`'s subscription/shard requirement (SSE has no gateway dependency; CheckOwnership's own doc comment already called it "the bare check" the SSE endpoint needs). The handler passes `scope.BranchIDs` straight into `Subscribe`. Acceptance bullet 3 ("dropping every event never closes or stalls the stream") holds by construction — `Publish`'s filter is a `continue` before the channel send, so a filtered-out subscriber's channel is untouched, and the handler's existing 25s heartbeat ticker (unmodified) is what actually keeps the connection alive, not traffic on the event channel.
  - Acceptance:
    - [x] A scoped member's stream receives events only for allowlisted branches; an unscoped member's stream is unchanged — `TestEventBus_ScopedSubscriberSeesOnlyAllowlistedBranches` (two subscribers on the same tenant, one scoped to `b1` only, one unscoped; the scoped one receives only the `b1` event, the unscoped one receives both, byte-identical to pre-T122 behaviour)
    - [x] The filter is applied at subscribe, not per-event at the client — by construction: the allowlist is captured once in `Subscribe`'s parameter and consulted server-side inside `Publish`; the SSE wire payload for a filtered event is never written, so there is nothing for client JS to filter
    - [x] Dropping every event for a member never closes or stalls their stream — `TestEventBus_ScopedSubscriberDroppingEveryEventStaysOpen` (10 out-of-allowlist publishes, channel confirmed still open via a non-blocking receive, then a matching publish still arrives)
  - Verify: `go build ./... && go vet ./... && gofmt -l internal/hq/... internal/httpapi/...` clean (no new formatting issues; `internal/hq/service.go` has pre-existing gofmt drift at `InventoryProductsPage`/`MovementsPage` from before this session, confirmed unchanged at `git show HEAD` — not touched here). `go test ./...` clean, including `go test -race -run TestEventBus ./internal/hq/...` for the new concurrent-map-of-channels code. Manual `curl` against a running stack not performed this session (no live containers exercised) — same honestly-flagged gap as T121's Verify line.
  - Files: `api/internal/hq/events.go`, `api/internal/hq/events_test.go`, `api/internal/httpapi/hq_handlers.go`, `api/internal/hq/service.go` (`CheckOwnership` widened to return `*perm.Scope`), `api/internal/hq/service_test.go` (`TestCheckOwnership_ContextScopeAvoidsSecondMembershipRead` updated for the new return value)
  - Dependencies: T119 · **Size: S**

- [x] **T123: console — branch picker in the assign dialog, scoped banner, unscoped-write hiding**
  - **Description:** `AssignRoleDialog` gains the branch allowlist picker (multi-select, empty = «كل الفروع»), with copy stating that **a branch created later is not added automatically** — fail-closed by design (spec D4), and the only place that could otherwise read as a bug. A quiet persistent indicator tells a scoped member which branches they are seeing, so an incomplete KPI is never mistaken for a company total. Buttons for D5c unscoped writes are hidden for scoped members rather than left to 403.
  - Implementation notes:
    - `AssignRoleDialog.tsx`: the picker is a mode toggle, not a bare checkbox list — a «كل الفروع» checkbox drives `allBranches: boolean` (init `member.branch_ids.length === 0`), and a `Set<string>` of ids (init `new Set(member.branch_ids)`) backs the individual checkboxes, sourced from `useBundle(tenantId)?.data?.Branches` (the control-plane branch list every member's bundle already carries, same as `Company` — no new fetch). Submitting sends `branchIds: allBranches ? [] : [...selectedIds]`; unchecking «كل الفروع» with nothing ticked is refused client-side (`اختر فرعًا واحدًا على الأقل، أو فعّل «كل الفروع»`) rather than silently resubmitting `[]`, which would read as "all branches" on the wire — the exact ambiguity D4's empty-means-unscoped contract creates if the UI doesn't distinguish "chose nothing yet" from "chose everything". The dialog's own caption states the fail-closed rule D4 calls out by name (a branch created later is not auto-added to a scoped assignment) — the copy the spec says is required "unless [it] is reported as a bug".
    - `perm.ts` gained `isUnscoped`/`canUnscoped`/`useCanUnscoped`, mirroring `can`/`useCan`'s shape exactly. `canUnscoped` is `can(scope, code) && isUnscoped(scope)` — D5c narrows an existing grant (holding the permission is still necessary), it doesn't add a separate one. `useScope`'s established "undefined while loading ⇒ nothing granted yet" convention carries through: `isUnscoped(undefined)` is `false`, so a write affordance gated by `canUnscoped` never flashes visible before the scope resolves.
    - `Catalog.tsx` (product create) and `ProductDetail.tsx` (price edit) both swapped their existing `useCan(tenantId, PERM.CatalogManage)` for `useCanUnscoped(tenantId, PERM.CatalogManage)` — a one-line change at each site since T115 already isolated `canManage` to exactly the write affordance in both files; nothing else in either page changed.
    - `AppShell.tsx`: the banner is a slim, non-dismissible bar between the header and the scroll container (so it never scrolls away and is present "on every screen" literally, a strict superset of "every screen with branch-derived numbers"), styled with the same `border-info/30 bg-info/10 text-info` tone `Badge tone="info"` already uses elsewhere (quiet/informational, not `warning`/`danger`, since a scoped view isn't an error state). Branch names come from `useBundle(tenantId)?.data?.Branches` matched against `scope.branch_ids` — `useBundle` is already warmed by `SetupGate` above this shell for the same tenant id/query key, so this is a cache read, not a new request. Renders nothing when `scopedBranchNames.length === 0`, which covers both an unscoped member (`scope.branch_ids` empty) and the scope still loading (`scope` undefined) — no separate loading-state branch needed.
    - **Self-directed additions beyond the task's own Files line, both required to satisfy this task's own third acceptance bullet and D5c's table literally:** `Branches.tsx`'s single `canManage` (`PERM.BranchesManage`) was split into `canManage` (rename + status-toggle, unchanged — those route through `PATCH /branches/{id}`, D5d's per-branch-allowlist rule, not D5c's blanket refusal, so a scoped manager acting on a branch they can already see is legitimate) and a new `canAddBranch = useCanUnscoped(...)` gating only «فرع جديد»/«إضافة فرع» and the `AddBranchDialog` mount (`POST /branches` is explicitly D5c-listed: "creates a branch that, by D4, they could not then see"). `Company.tsx`'s `canManage` (gating `CompanyForm`'s `readOnly`, T115) likewise switched from `useCan` to `useCanUnscoped` — `PUT /company` is D5c-listed as "company-wide". Without these two, the task's own acceptance bullet ("no إضافة فرع") would be false, and a scoped company-manager would still see an editable company form the server was about to refuse.
    - **Server-side gap found and fixed, beyond this task's console-only Files line — flagged to the user and fixed with explicit approval before continuing:** cross-checking D5c's table against the actual server code (this session's standing rule, not a new one) found that `tenant.SetCompany` and `tenant.AddBranch` had **no `requireUnscoped`-equivalent check at all** — T120 wired `hq.requireUnscoped` into `ChangeProductPrices`/`CreateProduct` but explicitly deferred `PUT /company`/`POST /branches` as "genuinely out of `hq/`'s Files", and T121 gave the `tenant` package's control-plane routes only D5d's per-branch-allowlist check (`ownedBranch`), never D5c's blanket-unscoped one. Concretely: a member scoped to one branch and holding `company.manage`/`branches.manage` could call these two routes directly and succeed, entirely bypassing the console buttons this task hides. Fixed with a new `tenant.ownedUnscoped` helper (mirrors `ownedBranch`'s shape — `TenantByID` + `membership.RequireScope` directly, one membership read — but refuses on `!scope.IsUnscoped()` instead of an out-of-allowlist branch id) and a new `tenant.ErrForbiddenUnscoped` sentinel, wired into `SetCompany`/`AddBranch` in place of the plain `owned()` call, plus the `forbidden_unscoped` → 403 mapping in `writeTenantError` (byte-identical code and Arabic message to `hq`'s own mapping, so the console renders the same regardless of which package served the request).
  - Acceptance:
    - [x] Assigning branches ٢ and ٧ produces exactly that allowlist, and clearing the picker means all branches (`allBranches` toggle → `branchIds: []`; ticking exactly two ids with `allBranches` off → `branchIds: [id2, id7]`, resent verbatim through `useAssignMemberRole`'s existing PATCH — see notes)
    - [x] A scoped member sees the banner on every screen with branch-derived numbers, naming their branches (`AppShell.tsx` renders it for every route this shell wraps whenever `scope.branch_ids.length > 0`, joining resolved branch names — a strict superset of the bullet's own scope)
    - [x] A scoped member sees no price-edit or product-create affordance, and no إضافة فرع (`Catalog.tsx`/`ProductDetail.tsx`/`Branches.tsx`'s `canAddBranch` all gated by the new `useCanUnscoped` — see notes for why `Branches.tsx` needed a split rather than a straight swap)
    - [x] An unscoped member sees no banner and no behaviour change (banner: `scopedBranchNames` is `[]` whenever `scope.branch_ids` is empty; write affordances: `canUnscoped(scope, code) === can(scope, code)` whenever `isUnscoped(scope)` is already `true`, so every previously-`useCan`-gated button is byte-identical for an unscoped caller, owner included)
  - Verify: `pnpm build && pnpm lint` — both clean (`tsc -b && vite build` succeeds; lint shows only the two pre-existing `auth.tsx` react-refresh warnings, no new ones). For the self-directed server-side fix: `go build ./... && go vet ./...` clean; `gofmt -l` clean on every touched Go file; full `go test ./...` green **against a real Mongo instance** (`TEST_MONGO_URI=mongodb://arib:secret@arib-platform-mongodb-1.orb.local:27017/?authSource=admin`, reachable this session via OrbStack), including a new integration test (`TestSetCompany_AddBranch_ScopedMemberRefusedUnscoped`, `api/internal/tenant/service_test.go`) covering the refusal, the owner's unaffected path, and a scoped-then-reassigned-unscoped member regaining access. **Manual as a two-branch scoped member was not performed this session** — no dev backend/second browser profile running, same recurring gap as T111-T122; every wire shape (the assign dialog's PATCH body, `TenantMeView.branch_ids`, the `forbidden_unscoped` error code/message) was instead cross-checked directly against the Go handlers/services rather than assumed. This is exactly what checkpoint 13c's manual pass is for — deferred there, not duplicated here. **Checkpoint 13c's own hand-crafted-request line only names "price change → 403 `forbidden_unscoped`"; it should also cover `PUT /company` and `POST /branches` now that both are actually enforced** — worth adding when that checkpoint is run.
  - Files: `console/src/components/AssignRoleDialog.tsx`, `console/src/components/AppShell.tsx`, `console/src/lib/perm.ts`, `console/src/pages/console/Catalog.tsx`, `console/src/pages/console/ProductDetail.tsx`, plus the necessary ripple noted above (`console/src/pages/console/Branches.tsx`, `console/src/pages/console/Company.tsx`) and the self-directed server-side fix (`api/internal/tenant/service.go`, `api/internal/tenant/service_test.go`, `api/internal/httpapi/tenant_handlers.go`)
  - Dependencies: T113, T120 · **Size: M**

- [x] **Checkpoint 13c — Group C complete: branch scoping enforced**
  - [x] `make test && make vet`, `pnpm build && pnpm lint` all green — `go vet ./...` clean; `go test ./...` green against real Mongo (OrbStack, `TEST_MONGO_URI`) after `go clean -testcache` to force a non-cached run, all 13 packages with tests `ok`; `pnpm build` (`tsc -b && vite build`) clean; `pnpm lint` shows only the two pre-existing `auth.tsx` react-refresh warnings, no new ones. **`dotnet build` — N/A, not run:** no `.csproj`/`.sln` exists anywhere in this repo (confirmed via `find`); the desktop POS/sync-gateway that this refers to lives in a separate repo this session has no access to.
  - [x] **Deploy order verified: gateway before API.** An old gateway binds only the first `branch_id`, so a two-branch member would see one — under-fetch, not a leak, but wrong. Confirmed by the user directly (outside this repo/session's reach).
  - [x] **Manual, tenant with ≥3 branches:** a member scoped to two sees exactly those in Overview KPIs, Branches, Inventory, every Report, Orders, Customers, Suppliers, and a CSV export. Confirmed by the user directly via a real click-through — not performed by the assistant this session.
  - [x] Hand-crafted requests: out-of-scope detail → 404; out-of-scope order create → 403; **price change → 403 `forbidden_unscoped`**. Confirmed via existing integration-test coverage at the enforcing layer (re-run on every future change): `TestOrderDetail_ScopedMemberOutOfAllowlistIsNotFound` (`hq/service_orders_test.go:392`, 404/`ErrNotFound`), `TestCreateOrder_ScopedMemberTargetBranchChecked` (`hq/service_orders_test.go:418`, 403/`ErrForbiddenScope`), `TestChangeProductPrices_ScopedMemberRefusedUnscoped` (`hq/service_test.go:1980`, 403/`ErrForbiddenUnscoped`) plus `CreateProduct`'s sibling test. The HTTP-layer error→status/code mapping (`writeHqError`/`writeTenantError`) was cross-checked byte-for-byte against these codes in T120/T123.
  - [x] Spec OQ2 (conflicts) and OQ4 (sync-token) — **both reviewed with the user, 2026-08-27; resolved differently, neither carried silently.** OQ2: **settled** — confirmed leave unfiltered for v1, no code change; spec's "Open questions" §2 updated to `[RESOLVED]`. OQ4: **explicitly deferred**, not part of Group C's scope — `IssueSyncToken` (`api/internal/tenant/service.go:527`) already deliberately ships unscoped rather than guessing at the desktop activation flow's behavior; spec's §4 updated to `[DEFERRED]` recording that this was a conscious choice, not an oversight, and naming a follow-up task once the desktop flow can be exercised.
  - [x] **Human review — group C** — completed by the user, 2026-08-27.

### Group D — invitations

- [x] **T124: `SendInvite` — invitations stop being silent**
  - **Description:** Today `InviteMember` sends **no email at all**: it creates a bare `Account` and the person becomes a member instantly, notified out-of-band or not at all. Adds `SendInvite` beside `SendOTP` (`mail/mail.go:42`) and extends `InviteMember` to take `role_id` + `branch_ids` so a person is invited *into* a role rather than assigned one afterwards. No accept token — OTP sign-in already proves the invitee controls the mailbox (spec D6).
  - **Implementation notes:**
    - `mail.Sender.SendInvite(ctx, to, tenantName)` mirrors `SendOTP`'s shape exactly (log-only in dev when SMTP is unconfigured, real `smtp.SendMail` otherwise). No accept link or token — deliberately no URL in the body either, since there's no console/dashboard base-URL config to point at (`PublicBaseURL` is the *API's* own URL; `DashboardOrigins` is a CORS allowlist, not a canonical console URL) and guessing one would violate the "never guess URLs" rule. The email just names the tenant and says to open the app and enter this address for a login code.
    - `tenant.Service` gained a local `Mailer` interface (`SendInvite(ctx, to, tenantName) error`) — same pattern as `auth.Mailer` for `SendOTP` — plus a `log *slog.Logger` field (new; no prior `tenant` service had one), both threaded through `New(...)`. `httpClient`/`log` still default sanely on nil so every non-invite caller (the rollout package's signer-only reuse of `tenant.New`, `TestIssueHQToken`) is a one-token diff, not a rewrite.
    - **`role_id`/`branch_ids` are optional, not required, on `InviteMember`.** The task description reads as "invited into a role," but T109's own already-shipped acceptance (`TestAssignMemberRole_TakesEffectOnNextRequest`) explicitly tests that a freshly-invited, not-yet-assigned member has *zero* permissions — a real, tested, current behavior this task must not silently break. Making role_id required would have forced rewriting that contract along with 11 existing `InviteMember` call sites that legitimately don't care about role at invite time. So both stay optional: empty `role_id` skips role validation entirely (preserves "invite bare, assign later" exactly as before); a non-empty one is checked with the identical `hasRole`/`hasBranch` helpers `AssignMemberRole` already uses. The console's own invite dialog (T125) is what actually makes a role mandatory *from the UI* — the API stays a permissive superset.
    - Validation (role, then branch) happens right after the email-format check, **before** `AccountByEmail`/`InsertAccount` — an unknown role or an out-of-tenant branch never creates an account, asserted directly (`AccountByEmail` still returns `ErrNotFound` after a rejected invite) rather than inferred.
    - The invite email is sent **after** `InsertMember`/`MarkHasBeenMember` succeed, guarded with `if s.mailer != nil` (safe for the nil-mailer signer-only reuse) and a `if err != nil { s.log.Warn(...) }` that never propagates — the membership is durable either way. Verified with a `fakeMailer` that returns a configured error: `TestInviteMember_MailFailureDoesNotRollBack` asserts both that `InviteMember` still returns success *and* that the invited account can immediately `GetBundle`, plus captures `s.log`'s output into a buffer to confirm the warning line actually contains the failing email — not just "no error propagated."
    - `model.TenantMember.InvitedAt` already existed (added ahead of this task, alongside `AcceptedAt`, per the model's own doc comment citing D6) but was never populated. Set it here (`InvitedAt: now`, same timestamp as `CreatedAt`) since it's exactly this task's concern and free to do — not surfaced on `MemberView`/JSON yet, so no wire-contract change and no test churn.
    - `main.go`: moved `mailer := mail.New(...)` above `tenantSvc := tenant.New(...)` (it used to sit after, for `authSvc`'s benefit only) and passed both `mailer` and the app `log` into `tenant.New`.
  - Acceptance:
    - [x] Inviting sends one email naming the tenant and how to sign in; a send failure does **not** roll back the membership (they still have access, matching today's behaviour) but is logged — `TestInviteMember_SendsInviteEmail` (content/recipient), `TestInviteMember_MailFailureDoesNotRollBack` (non-fatal + logged, log content asserted)
    - [x] `role_id` and `branch_ids` are validated exactly as T109 validates them, and an invite with an unknown role is rejected before the account is created — `TestInviteMember_RoleAndBranchValidation` (unknown role, out-of-tenant branch, no account created for either, no mail sent for either, and a valid role+allowlist takes effect immediately via `GetBundle` with no separate `AssignMemberRole` call)
    - [x] Re-inviting an existing member still returns `ErrAlreadyMember` and sends nothing — `TestInviteMember_ReinviteSendsNothing`, plus the pre-existing `TestInviteMember` subtest still passes unchanged
  - Verify: `go build ./...` clean; `go vet ./...` clean; `gofmt -l` clean on every file this task touched (four *other*, untouched files — `internal/config/config.go`, `internal/store/mongo/branches.go`, `internal/store/mongo/companies.go`, and `internal/hq/service.go`, already `M` before this task from earlier session work — show pre-existing `gofmt -l` drift unrelated to T124; left alone rather than reformatted as unrequested scope). Full `go test ./...` green against real Mongo (OrbStack, `TEST_MONGO_URI`) after `go clean -testcache`, all 13 packages with tests `ok`, including all 5 new/updated `TestInviteMember*` tests run individually with `-v` to confirm none were silently skipped. **Manual — invite a real address, confirm delivery and sign-in — not performed this session** (no SMTP configured locally, no dev backend running); the dev/log-only path (`s.host == ""`) was exercised implicitly by every test above, but real SMTP delivery is unverified. `console/pnpm build && pnpm lint` not re-run: T124 is API-only (confirmed `InviteMemberDialog.tsx` still sends only `{email}`, which stays valid since `role_id`/`branch_ids` are optional) — no console file changed.
  - Files: `api/internal/mail/mail.go`, `api/internal/tenant/service.go`, `api/internal/tenant/service_members.go`, `api/internal/tenant/service_members_test.go`, `api/internal/tenant/service_test.go`, `api/internal/httpapi/tenant_handlers.go`, `api/cmd/api/main.go`, `api/internal/rollout/rollout_integration_test.go`
  - Dependencies: T109 · **Size: M**

- [x] **T125: `AcceptedAt` stamping + pending badge + role/branches in the invite dialog**
  - **Description:** `AcceptedAt` is stamped once, on a member's first authenticated request on that tenant, from the middleware that already has the row — best-effort, never able to fail a read, one write per member for their entire lifetime. "Pending" is **derived** from `AcceptedAt == nil`, not a second stored enum. `InviteMemberDialog` gains role and branch selection; the members table shows «بانتظار الانضمام» until first sign-in.
  - **Implementation notes:**
    - The stamp lives in `httpapi.resolveScope` (called by `requirePerm` on every tenant-scoped request), right after `MemberByAccount` — that's the one place that already has the row on every authenticated request, so no new read was added. Gated on `m.Role != model.RoleOwner && m.AcceptedAt == nil`: the owner row is never stamped (D1 — it was never invited, "pending" has no meaning for it), and a member whose in-memory row already shows `AcceptedAt` set skips the write outright.
    - **True "exactly once" comes from the store, not the in-memory check.** `mongostore.MarkMemberAccepted` filters the update on `accepted_at: {$exists: false}}` — so even two concurrent first requests for the same member (the in-memory check alone can't serialize across goroutines/replicas) still produce exactly one successful write; the loser's update simply matches zero documents and returns no error. The in-memory check in `resolveScope` is purely an optimization that skips the round-trip entirely on the (overwhelmingly common) already-accepted path.
    - **Never fails the request that triggered it:** `MarkMemberAccepted`'s error is logged via `s.log.Warn(...)` and swallowed — same shape as T124's mail-send failure. Guarded with `s.log != nil` since `middleware_test.go` builds bare `&Server{store: fake}` fixtures with no logger; a nil-receiver `Warn` call would otherwise panic on the very failure path the test is trying to exercise.
    - `scopeResolver` (the narrow interface `requirePerm` depends on, declared in `middleware.go` so tests can fake it without Mongo) gained the third method `MarkMemberAccepted(ctx, tenantID, memberID) error`. It's the only implementer of this interface in the codebase (`tenant.Store`/`hq.Store` are separate, unrelated interfaces that happen to also have `TenantByID`) — confirmed by grep before editing, so no other fake needed updating.
    - **Invite dialog:** `InviteMemberDialog` now requires a role (`roleId: z.string().min(1, ...)`, same validation shape as `AssignRoleDialog`'s schema) and offers the identical branch allowlist picker (`allBranches` toggle defaulting to true / a ticked subset, refusing an empty scoped selection client-side) — copied from `AssignRoleDialog` rather than extracted into a shared component, since T125's acceptance says "matching `AssignRoleDialog`'s picker," not "share one," and the two dialogs' surrounding form state (react-hook-form + a separately-tracked branch `Set`) differ enough that a premature extraction would need its own generalization pass. `useInviteMember`'s mutation signature changed from `(email: string)` to `{email, roleId, branchIds}`; `api.inviteMember` now posts `role_id`/`branch_ids` alongside `email` — the API's own params stay optional server-side (T124), this dialog just never sends them empty.
    - **Pending badge:** `Member.accepted_at?: string` added to the console's type (mirrors `MemberView.AcceptedAt` — already serialized server-side since T102/T108, just not read by the console yet). Settings' members table renders a `warning`-tone «بانتظار الانضمام» badge beside the owner/member role badge whenever `m.role !== 'owner' && !m.accepted_at` — not a new table column, since the existing role-badge cell was the natural place for a member-lifecycle indicator and every other column (custom role, branches, joined-date) is a orthogonal fact about the row.
  - Acceptance:
    - [x] A newly invited member shows as pending; after their first request the badge clears and never returns — `TestRequirePerm_StampsAcceptedAtOnFirstRequestOnly` (member row's `AcceptedAt` is nil before, non-nil and stable after); console side: `accepted_at` is now read by `Settings.tsx`'s badge condition, and the API always returns the current row on every `GET …/members` poll, so the clear is visible without a page reload once stamped
    - [x] The stamp happens exactly once — a second request issues no write (asserted with a counting fake) — `TestRequirePerm_StampsAcceptedAtOnFirstRequestOnly` (a 2nd request from the same member leaves `acceptCalls` at 1); `TestMarkMemberAccepted_StampsOnceAndIsIdempotent` proves the same at the real-Mongo store layer via the `$exists:false` filter, including that the timestamp doesn't move on a 2nd call
    - [x] A failed stamp never fails the request that triggered it — `TestRequirePerm_FailedAcceptStampDoesNotFailRequest` (fake returns an error from `MarkMemberAccepted`; the wrapped handler still runs and the response is still 200)
    - [x] The invite dialog requires a role and offers branches, matching `AssignRoleDialog`'s picker — `roleId` is a required zod field with the same picker markup/behavior (all-branches toggle, per-branch checkboxes, "select at least one" guard) copied from `AssignRoleDialog.tsx`
  - Verify: `go build ./... && go vet ./...` clean; `gofmt -l` clean on every Go file this task touched. Full `go test ./...` green against real Mongo (OrbStack, `TEST_MONGO_URI`) after `go clean -testcache`, all 16 packages with tests `ok`, including `TestRequirePerm_StampsAcceptedAtOnFirstRequestOnly`, `TestRequirePerm_FailedAcceptStampDoesNotFailRequest` and `TestMarkMemberAccepted_StampsOnceAndIsIdempotent` individually re-run with `-v` to confirm none were skipped. `pnpm build` (tsc -b + vite build) and `pnpm lint` both clean — the only lint output is 2 pre-existing `react-refresh/only-export-components` warnings in `lib/auth.tsx`, a file this task never touched. **Manual — invite, observe pending, sign in as the invitee, observe it clear — not performed this session**, same gap as T124: no SMTP configured locally and no dev backend running (starting one needs `.env` values this session couldn't read), so the end-to-end OTP round trip is unverified against a live browser; every mechanical piece it would exercise (the stamp, its exactly-once guarantee, its failure isolation, the dialog's new required fields, the badge's derivation) is covered by the tests above instead.
  - Files: `api/internal/httpapi/middleware.go`, `api/internal/httpapi/middleware_test.go`, `api/internal/store/mongo/tenant_members.go`, `api/internal/store/mongo/tenant_members_test.go` (new), `console/src/components/InviteMemberDialog.tsx`, `console/src/pages/console/Settings.tsx`, `console/src/lib/api.ts`, `console/src/lib/hooks.ts`, `console/src/lib/types.ts`
  - Dependencies: T124 · **Size: M**

- [ ] **Checkpoint 13d — Phase 13 complete**
  - [x] All three repo gates green; `package.json` and `go.mod` unchanged (no new dependencies anywhere) — 2026-08-27. `git diff -- api/go.mod api/go.sum console/package.json console/pnpm-lock.yaml` all empty. `go build ./... && go vet ./...` clean; `gofmt -l` shows only the same pre-existing struct-tag drift in `internal/hq/service.go` already documented at T119/T122 (diffed byte-for-byte against `git show HEAD:./internal/hq/service.go` to confirm identical hunks, just shifted by line offset — nothing this phase touched). Full `go test ./...` green against real Mongo (OrbStack, `TEST_MONGO_URI`) after `go clean -testcache`, all 16 packages `ok`. `pnpm build` (`tsc -b && vite build`) and `pnpm lint` clean — only the two pre-existing `auth.tsx` react-refresh warnings. **Correction to checkpoint 13c's "N/A, no `.csproj` exists anywhere in this repo":** that was true of `platform/` alone — this session had filesystem access to the sibling `sync-gateway` repo (`~/dev/arib/sync-gateway`) and ran `dotnet build AribSyncGateway.csproj` directly: succeeds, 0 errors, only the same 20 pre-existing `NU1903` advisory warnings T118 already documented. `sync-gateway`'s T118 diff (`BranchScope.cs`, `HqApi.cs`, `Program.cs`) is still uncommitted there, per this session's own no-commit-without-being-asked rule extended across repos.
  - [ ] End-to-end as a real invited person: receive the email, sign in with OTP, land in a console shaped by the role and branches they were invited into, pending badge clears — **not done: needs a running api+console+SMTP and a real second inbox; `.env` is unreadable this session (tool permission denial) and no dev servers are up (`docker compose ps` shows only `mongodb`)**. Same recurring gap as every manual step since T102 — needs a human pass, same as checkpoint 13b/13c's second-browser-profile verification.
  - [x] Regression: owner experience identical to pre-Phase-13; existing members' access unchanged since checkpoint 13a — verified by code inspection + test continuity, not a live click-through (that was already done by the user through 13a–13c, and nothing landed since T118 touches the owner path): T125's stamp explicitly skips owner rows (`m.Role != model.RoleOwner`, `middleware.go`); T123's branch picker/unscoped-write hiding only renders for scoped roles; T124's `SendInvite` is an owner-only write with no read-path change. Every pre-existing owner-focused test in `service_test.go`/`service_orders_test.go`/`service_suppliers_test.go`/`tenant/service_test.go` still passes unmodified in this session's full `go test ./...` run (cited already under T120/T124/T125's own Verify lines).
  - [x] `AribONE.Data` untouched — no `SchemaVersion` bump in the diff — 2026-08-27. `git status --short` inside `~/dev/arib/AribONE.Data` is empty (repo has zero uncommitted changes, HEAD unmoved since `7937703`), and `git diff` across the whole `platform` working tree has no `SchemaVersion` hit outside this checklist line itself.
  - [x] Spec OQ1/OQ3/OQ5 explicitly resolved or carried forward with a reason — re-checked against current server code, 2026-08-27. **OQ1** (delegated administration): still not blocking — `members` POST/DELETE/PATCH are all `ownerRule` in `middleware.go:144-146`, confirming `members.manage` never left owner-only through the whole phase. **OQ3** (export under a view-only role): still resolved per its stated default — `middleware.go:187,198` gate `hq/customers/export` and `hq/suppliers/export` on `perm.CustomersView`/`SuppliersView` alone, matching T116's own resolution. **OQ5** (no audit trail): accepted residual risk, unchanged — no audit log was added anywhere in this phase's diff, consistent with the plan doc's explicit "not blocking any task."
  - [ ] **Human review — Phase 13 complete** — **not done: requires the user, not an automatable step.**
