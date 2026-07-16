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
    - [ ] Import is disabled until both a file and a branch are selected; per-row Arabic error table on partial failure (missing field, type mismatch) — same UX as Customers' fixed bug — *(code mirrors `ImportCustomersDialog.tsx`'s fixed behavior exactly, including the `disabled={!file || !branchId || ...}` guard; not yet click-through-verified)*
    - [ ] Bulk group/price-tier mutation reflects immediately in the list; export honors active filters — *(code correct via query invalidation on `hq-suppliers`; not yet click-through-verified)*
    - [x] "الموردون" nav tab sits beside "العملاء" in both desktop sidebar and mobile nav, with its own distinguishable icon — confirmed by code: single `nav` array in `AppShell.tsx` drives both, `SupplierIcon` (`Delivery`) is a distinct glyph from `UsersIcon` (`UsersGroupRounded`)
  - Verify: `npx tsc --noEmit`; `npx eslint`; `pnpm build && pnpm lint` — all clean (2026-07-16). Manual click-through of create/edit/bulk/import/export pending — see Checkpoint 8.
  - Files: `console/src/components/{CreateSupplierDialog,EditSupplierDialog,ImportSuppliersDialog,SupplierBulkActionsBar}.tsx` (new), `console/src/{App,components/AppShell,components/icon}.tsx`
  - Dependencies: T70 · **Size: M**

### Checkpoint 8
- [x] All gates green (api `go build ./... && go vet ./... && go test ./...`, gateway `dotnet build AribSyncGateway.csproj`, console `pnpm build && pnpm lint`) *(2026-07-16, machine-verified end-to-end; gateway and API dev processes both restarted onto the new binaries and route-table-smoke-tested — see T67. No minted HQ token or browser-automation tool was available in this session, so the data-level/manual items below are still open — same "living document" convention as every prior phase: only checked once actually verified, not just code-complete.)*
- [ ] Manual regression: Customers list/profile/create/edit/bulk/import/export/insights all unchanged after the T66 parameterization
- [ ] Manual e2e: Suppliers list/profile/create/edit/bulk/import/export/insights match the Customers UX exactly, verified against a real synced tenant
- [ ] RTL/Arabic-numerals audit on the new Suppliers views
- [ ] Human review before Phase 9 (Live tier)
