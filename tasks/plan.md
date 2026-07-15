# Implementation Plan: HQ Console — slices 0–1 (foundation + branches dashboard)

Spec: `tasks/spec-console.md`. This plan details **Slice 0 (HQ read path + freshness)** and **Slice 1 (branches dashboard + SSE leg)** into S/M tasks, and outlines slices 2–7 at phase level — they get their own task breakdowns when reached, after the patterns from 0–1 are proven.

## Assumptions (spec open questions — none block slices 0–1)

- Conflict policy stays **ServerWins + ConflictLog alerts** unless the user decides otherwise before slice 3 (catalog writes).
- Reports (slice 6) start as **direct SQL aggregates** on tenant DBs via the gateway.
- **Customers** stays out of scope for this pass.

## Architecture decisions (grounded in the code)

- **Last-sync recording lives in the tenant central DB** (`SyncActivity` table: BranchId, LastSyncAt — central-only, never synced, lazily ensured), exactly mirroring the existing `ConflictLog` pattern (`ConflictLog.cs`). The gateway is the only writer; it also fires a **fire-and-forget callback** to the API so the control plane has a cheap copy for branch health without a gateway round-trip per page view.
- **Gateway→API callback reuses the existing internal-route pattern**: `/v1/internal/tenant-registry` is already authed by the branch's forwarded sync token; `/v1/internal/sync-completed` does the same — no new token type for this leg.
- **API→gateway HQ reads use a new `scope:"hq"` RS256 token** mirroring `OpsToken` (`OpsToken.cs` — same public key the gateway already holds, different required claims so an HQ token is never accepted as ops or sync). Claims: `scope:"hq"`, `db_name`, short TTL. Minted server-side per outbound call; **never sent to the browser**.
- **Tenant→gateway routing**: the API resolves tenant → `ShardID` → `Shard.GatewayURL` (all already in Mongo) — the console never learns shards exist.
- **Freshness envelope** (API⇄console contract, from the spec): branch-derived payloads arrive as `{ data, source: "synced"|"offline"|"live", as_of }` per branch; `live` is reserved for the future SignalR tier.
- **SSE** from the Go API (`/v1/tenants/{id}/events`), in-memory per-tenant pub/sub fed by the sync-completed callback; console consumes with `EventSource` and invalidates TanStack Query keys. Nginx: `proxy_buffering off` on that location only.

## Task list

### Phase 0 — foundation (slice 0)

- [x] T1: Gateway records last-sync per branch (`SyncActivity` in tenant DB)
- [x] T2: API internal endpoint `POST /v1/internal/sync-completed` → persist `last_sync_at` on Branch
- [x] T3: Gateway fires sync-completed callback to the API after each successful round
- [x] T4: API mints HQ tokens (`scope:"hq"`, db_name, short TTL)
- [x] T5: Gateway `HqToken` validation + `GET /hq/branch-activity`
- [x] T6: API HQ passthrough `GET /v1/tenants/{id}/hq/branch-activity` with freshness envelope
- [x] T7: Console freshness primitive (`<Freshness>`, envelope types, hook)
- [x] T8: Console nav shell — final IA with placeholders

### Checkpoint 0 (foundation proven end-to-end)
- [x] `make test` (api) green, `dotnet build` (gateway) clean, `pnpm build && pnpm lint` (console) clean
- [x] Real desktop sync round → `SyncActivity` row updates → Branch doc `last_sync_at` updates → console dev build renders per-branch freshness through the full chain *(human-verified 2026-07-14)*
- [x] Human review before Phase 1 *(approved 2026-07-14)*

### Phase 1 — branches dashboard (slice 1)

- [x] T9: Gateway `GET /hq/branch-snapshot` — today's sales + current shift per branch from central (Bills, Shifts)
- [x] T10: API passthrough + branch-health derivation (🟢 <10 min / 🟡 10–30 / 🔴 older, from `last_sync_at`)
- [x] T11: Console Branches page — branch-as-server cards
- [x] T12: Console branch detail page (progressive disclosure)
- [x] T13: API SSE endpoint `GET /v1/tenants/{id}/events` + nginx location
- [x] T14: Console live updates — `EventSource` hook invalidating query keys

### Checkpoint 1 (slice 1 shipped)
- [x] All gates green
- [x] Manual e2e: trigger a desktop "Sync Now" → the branch card's freshness pill and health dot flip in the console **without a page refresh** *(human-verified 2026-07-14)*
- [x] Branch with no sync in >30 min renders 🔴 with "last data from …" *(human-verified 2026-07-14)*
- [x] Human review before Phase 2 *(approved 2026-07-14)*

### Phase 2 — Overview (slice 2)

**Design note (2026-07-14):** the outline guessed this slice needs a gateway aggregate endpoint — it doesn't. T9's `/hq/branch-snapshot` already returns per-branch today-sales/refunds/shifts, so company KPIs are a Go-side sum over the same single gateway call `/hq/branches` already makes (table-driven tested, no new gateway surface, no extra gateway load). The console Overview reuses the existing `hq-branches` query key, so the shared cache and T14's SSE invalidation make the KPIs flip live with zero new wiring.

- [x] T15: API — `totals` block on `GET /v1/tenants/{id}/hq/branches` (company KPIs summed from branch snapshots + offline-branch honesty count)
- [x] T16: Console — Overview rework: KPI tiles (sales/bills/refunds/open shifts today) with freshness + offline caveat
- [x] T17: Console — branch health strip (per-branch dot → branch detail)
- [x] T18: Console — alerts stub (stale-sync alerts with deep links) + quick actions row

### Checkpoint 2 (slice 2 shipped)
- [x] All gates green (api `go test ./...`, gateway `dotnet build`, console `pnpm build && pnpm lint` — 2026-07-14)
- [x] Manual e2e: Overview KPI totals match the sum of the Branches cards; desktop "Sync Now" flips the Overview numbers/freshness live without refresh *(human-verified 2026-07-14; surfaced and fixed a real bug: gateway serialized open-shift OpenedAt without a timezone suffix, which zeroed all totals once shift mode was enabled — sync-gateway `12bc3ae`)*
- [x] Stale branch (>30 min) appears as an alert whose link opens that branch's detail page *(human-verified 2026-07-14)*
- [x] Human review before Phase 3 *(approved 2026-07-14)*

### Phase 3 — Catalog (slice 3)

**Design notes (2026-07-14):**
- **Open question 1 resolved (user decision):** v1 keeps **ServerWins + ConflictLog alerts** — no branch-edit gating, no schema bump. HQ writes always win at each branch's next sync; losing branch edits land in ConflictLog and surface as alerts in slice 5.
- **Central writes propagate for free:** DMS provisions its own tracking triggers on the central DB (`CentralProvisioner.cs` header), so gateway-side EF writes to Tier-A tables are tracked like any other change and reach every branch on its next round. T23 proves this e2e before any write UX ships.
- **Propagation state needs no new storage:** a write response carries `written_at` (gateway clock, UTC). The console already holds per-branch `last_sync_at` live via SSE — a branch has the write once `last_sync_at ≥ written_at`. Chips flip live with zero new wiring.
- **Prices live on `UnitOfMeasure`** (`Sale`/`Buy`/`Price1..9`), barcodes hang off UoMs — "change price" = UoM row updates, not Product.
- **HQ product-create must seed inventory rows:** the desktop's product browser queries `WarehousesProductInventories` (`WarehousesAndProductsViewModel.FetchProduct`), so a product without per-branch WPI rows is invisible at branches. HQ create therefore writes Tier-A rows (Product + UoMs + Barcodes) **plus** one zero-qty WPI row per branch warehouse (Tier-B rows carry each branch's BranchId and sync down to their owners). No opening balance from HQ in v1.

- [ ] T19: Gateway catalog reads — `GET /hq/groups`, `GET /hq/products` (paged + search + group filter), `GET /hq/products/{id}` (UoM price tiers, barcodes, per-branch availability)
- [x] T20: API catalog passthrough `GET /v1/tenants/{id}/hq/catalog/*` with freshness envelope + per-branch health on availability rows
- [x] T21: Console Catalog page — groups tree + products table (search, pagination)
- [x] T22: Console product detail — units/prices/barcodes + per-branch availability with freshness
- [~] T23: Gateway first HQ write — `PUT /hq/products/{id}/prices` (UoM price updates); code done, `dotnet build` clean; propagation e2e against a real desktop sync still needs a human pass
- [x] T24: API write passthrough `PUT /v1/tenants/{id}/hq/catalog/products/{pid}/prices`
- [x] T25: Console price editing + per-branch propagation chips (flip live via SSE); build/lint clean — real click-through/desktop-sync visual check folds into checkpoint 3
- [x] T26: HQ product create — gateway `POST /hq/products` (+ WPI seeding), API passthrough, console form; all gates green — real desktop-sync visibility/sellability check needs a human pass

### Checkpoint 3 (slice 3 shipped)
- [x] All gates green (api `go test ./...`, gateway `dotnet build`, console `pnpm build && pnpm lint`)
- [x] Manual e2e: catalog list/detail numbers match the desktop's own products screen for a real synced tenant *(human-verified 2026-07-15)*
- [x] Manual e2e: HQ price change → desktop shows the new price after its next sync round; propagation chip flips «وصل» without refresh *(human-verified 2026-07-15)*
- [x] Manual e2e: HQ-created product appears in the desktop products screen after sync and is sellable *(human-verified 2026-07-15)*
- [x] Extra edge cases checked and good *(human-verified 2026-07-15: HQ/branch conflict → ServerWins + ConflictLog; duplicate barcode rejected; non-stock kinds; barcode scan at POS)*
- [x] Human review before Phase 4 (Inventory) *(approved 2026-07-15)*

### Phase 4 — Inventory (slice 4)

**Design notes (2026-07-15):**
- **Low-stock rule mirrors the desktop exactly** (`AribONE.Data/Services/Notifications/Rules/InventoryStockRule.cs`): rows where `Product.IsActive && (TotalQty <= 0 || (double)TotalQty <= Product.ReOrder)`; classify `<0` → سالب, `==0` → نفاد, else `ReOrder>0 && qty<=ReOrder` → تحت حد إعادة الطلب. `ReOrder==0` never low. No other threshold exists anywhere in the schema.
- **Only `ProductKind.Product` is stockable** — and T26's HQ create seeds zero-qty WPI rows for every kind, so every inventory query needs an explicit `ProductKind == Product` guard (the desktop rule gets away without it only because the desktop never creates WPI rows for services).
- **Movements must be ProductId-anchored**: `InventoryMovements` is indexed only on ProductId/WarehouseId/CustomerId — no BranchId or IssueDate index — so no "list all movements" endpoint exists; every movements read requires a product.
- **Stale-branch detection is free**: the Go API already computes `healthTier` (ok/lagging/stale/never) per branch from `last_sync_at` — the fourth needs-attention condition needs zero gateway work, just an API-side merge.
- **Movements drill-in lives on the catalog ProductDetail page**, not a separate route — attention/by-product rows already deep-link there; it's the single "investigate this product" surface. Running qty is computed gateway-side in decimal so every page is self-contained (opening balance + net of skipped rows as the page-N seed).
- **View toggle lives in the URL** (`?view=attention|products|branches&branch=`), default `attention` (spec: problems surface unprompted) — this makes Phase 5's alert deep-links free.

- [x] T27: Gateway — `/hq/inventory/branch-summary` + `/hq/inventory/attention` (shared `StockStatus` classifier + stockable-kind guard)
- [x] T28: Gateway — `/hq/inventory/products` paged by-product list (search/group/branch/status filters)
- [x] T29: Gateway — `/hq/products/{id}/movements` (opening balance, running qty, ProductId-anchored, paged)
- [x] T30: API — four `hq.Service` passthrough methods + handlers + routes (registry merge for by-branch, stale-branch merge for attention, table-driven tests)
- [x] T31: Console — lib plumbing (types/api/query/hooks under `hq-inventory` key prefix), shared `Pagination` extraction, SSE invalidation
- [x] T32: Console — Inventory shell + needs-attention view (default view, URL-state toggle)
- [x] T33: Console — by-product + by-branch views
- [x] T34: Console — ProductDetail movements section (lazy, collapsible)

### Checkpoint 4 (slice 4 shipped)
- [x] All gates green (api `go build ./... && go vet ./... && go test ./...`, gateway `dotnet build`, console `pnpm build && pnpm lint` — 2026-07-15)
- [x] Manual e2e: attention counts/rows match the desktop notification center for a real synced tenant (incl. ReOrder==0 and qty==ReOrder boundary cases) *(human-verified 2026-07-15; two real bugs found and fixed during this pass, see below)*
- [x] Manual e2e: POS sale past zero → row appears سالب in attention live (SSE, no refresh); branch adjustment clears it the same way *(human-verified 2026-07-15)*
- [x] Manual e2e: movements parity vs desktop ProductMove screen (opening balance, rows, running qty); unbounded-period final running qty equals that branch's WPI TotalQty *(human-verified 2026-07-15)*
- [x] Stale branch (>30 min) appears in the attention strip with a working link; disappears after sync *(human-verified 2026-07-15)*
- [x] Human review before Phase 5 (Notifications + Ctrl+K) *(approved 2026-07-15)*

Bugs found and fixed during this checkpoint's e2e pass (2026-07-15):
- SSE `/v1/tenants/{id}/events` had 500ed on every connection since the feature was first built — `requestLogger`'s `statusWriter` wraps `http.ResponseWriter` by embedding the interface, which promotes only that interface's own methods, not `Flush()`. Fixed with an explicit `Flush()` delegation (`api/internal/httpapi/middleware.go`).
- `/hq/inventory/attention` 500ed the instant a row entered the low/out/negative bucket — Postgres `timestamp without time zone` values for `LastInDate`/`LastOutDate` round-trip through Npgsql/System.Text.Json without a `Z`/offset, and Go's strict-RFC3339 decoder rejects that. Fixed with a global UTC-forcing `DateTime` JSON converter in the gateway (`sync-gateway/Program.cs`), plus error logging on `writeHqError`'s 500 fallback (`api/internal/httpapi/hq_handlers.go`) so this class of bug surfaces immediately next time.

### Phase 5 — Notifications + Ctrl+K (slice 5)

**Design notes (2026-07-15):**
- **Only ConflictLog needs new backend surface.** The other alert sources already flow live: stale/never branches ride `useHqBranches` (SSE-invalidated), low/out/negative counts ride `/hq/inventory/attention`'s unpaged `counts`. Alert derivation is therefore client-side (`lib/alerts.ts`), one shared function feeding both the Overview panel (grows the existing `OverviewAlert` shape as planned) and the new bell — same rows, same deep links, can't drift.
- **Conflict alerts need server-side ack state.** Derived alerts clear when their condition clears; a ConflictLog row is a historical event that never clears on its own. `ConflictLog` is gateway-ensured DDL, central-only, NOT in `AribONE.Data` — adding a nullable `AcknowledgedAt` column is **not** a SchemaVersion bump (no fleet flag day). Existing tenant DBs upgrade via add-column-if-missing in the ensure DDL (both dialects). Ack lives next to the data and holds across sessions/users, unlike a client-side watermark.
- **Kept vs overridden:** the gateway logs upload conflicts, where DMS's `LocalRow` = the central/server row (kept under ServerWins) and `RemoteRow` = the branch's losing write. The review page presents them as «القيمة المعتمدة» / «تعديل الفرع المرفوض». A null RemoteRow (branch delete) renders as such. Checkpoint 5 verifies this orientation against a real forced conflict.
- **Product deep-links from conflicts are best-effort, gateway-side:** TableName `Products` → RowPk is the product id; `UnitOfMeasure` → row JSON carries ProductId; `Barcodes` → row's UnitOfMeasureId resolved via EF. Anything else gets `product_id: null` — the alert still has a destination (the conflicts review page), satisfying the spec rule.
- **Ctrl+K is built in-house** on the existing Radix Dialog (cmdk would be a new dependency — not needed). Sections: pages, branches (from the cached bundle), products (debounced `useCatalogProducts` search ≥2 chars — name/code/barcode, same gateway query as the Catalog page). Arabic RTL, ↑/↓/Enter/Esc.
- **Top-bar branch-status indicator** (the spec's remaining "Global" item) = worst health tier over `useHqBranches` + per-branch dropdown; mounting the hook in AppShell means every console page now keeps that query warm — acceptable, it's the API-side sum over one gateway call, already on a 60s/SSE cadence.

- [x] T35: Gateway — ConflictLog `AcknowledgedAt` + `GET /hq/conflicts` (paged, unacked count, best-effort product link) + `POST /hq/conflicts/ack` *(sync-gateway `0dbd2c3`)*
- [x] T36: API — conflicts passthrough + ack + branch-name decoration + tests
- [x] T37: Console — lib plumbing (conflict types/api/hooks, `hq-conflicts` SSE invalidation) + shared `lib/alerts.ts` derivation adopted by Overview *(platform, 2026-07-15)*
- [x] T38: Console — notifications bell in the AppShell header (badge + dropdown, every row deep-links) *(platform, 2026-07-15)*
- [x] T39: Console — conflicts review page (kept-vs-overridden diff, per-row + bulk ack, product deep-links) *(platform, 2026-07-15)*
- [x] T40: Console — top-bar branch-status indicator *(platform, 2026-07-15)*
- [x] T41: Console — Ctrl+K command palette *(platform, 2026-07-15)*

### Checkpoint 5 (slice 5 shipped)
- [x] All gates green (api `go build ./... && go vet ./... && go test ./...`, gateway `dotnet build`, console `pnpm build && pnpm lint`)
- [x] Manual e2e: force a real conflict (HQ price change + branch edit of the same unit before its sync) → ServerWins at the branch, conflict appears in bell + review page **without refresh** (SSE), kept/overridden columns correctly oriented, product deep-link opens the right product, ack clears the badge everywhere
- [x] Manual e2e: low/out/negative stock and stale-branch alerts show in the bell with working deep links (attention view / branch detail); alerts clear when conditions clear
- [x] Manual e2e: Ctrl+K — navigate to a page, jump to a branch, find a product by name/code/barcode; keyboard-only round trip; RTL rendering correct
- [x] Existing ConflictLog rows from before this phase (no AcknowledgedAt column) survive the DDL upgrade and list correctly
- [x] RTL/Arabic-numerals audit (badge counts, palette, review page)
- [x] Human review before Phase 6 (Reports — gated on open question 2)

Bugs found and fixed during this checkpoint's e2e pass (2026-07-15): desktop `UpsertAccountViewModel.SaveAccount` re-stamped `Account.CreatedAt` to now on every edit, diverging from central and flooding `ConflictLog` with spurious `Accounts` conflicts — fixed by preserving the original `CreatedAt` on the edit path. Separately, ~1508 pre-existing `ConflictLog` rows were a harmless DMS artifact (a branch's first sync re-uploads all pre-existing local rows as "untracked," including the deterministic seed `Accounts` rows already on central, producing an identical-row `RemoteExistsLocalExists` "conflict") — fixed in `sync-gateway/ConflictLog.cs` by skipping the log write when `LocalRow`/`RemoteRow` are field-for-field equal.

### Phase 6 — Reports (slice 6)

**Design notes (2026-07-15):** open question 2 resolved by this plan's standing assumption (user proceeded past the checkpoint-5 gate): v1 = direct, date-bounded SQL aggregates on tenant DBs via the gateway; revisit pre-aggregation only if fleet growth makes it hurt. Semantics mirror the desktop: day scope on `CreatedAt` gateway-local (T9's assumption), `Sale`/`ReSale` `!IsDeleted` Σ`Total`; tender split = `ShiftReportService`'s (Money/BankMoney/WalletMoney/Remain); profit = `ProfitFromWarehouseViewModel`'s Σ(Total−ItemCost) over SaleEntries, anchored through `!Bill.IsDeleted` so products revenue can't drift from sales totals. Day series as local-date strings (no zone-less-timestamp round-trip). Staff = GroupBy `UserId` join Tier-A Users. Inventory question reuses slice-4 data — no new backend. No chart dependency (inline SVG bars). Full task detail in `todo.md`.

- [ ] T42: Gateway — `GET /hq/reports/sales` (totals + tender split + per-day series)
- [ ] T43: Gateway — `GET /hq/reports/products|branches|staff` (period GroupBys; paged products with revenue/qty/profit sorts)
- [ ] T44: API — four passthroughs + registry decoration on branches + table-driven tests
- [ ] T45: Console — lib plumbing + `PeriodPicker` + Reports shell (`?view=` toggle, default sales)
- [ ] T46: Console — Sales + Branches views (KPI tiles, tender split, SVG daily bars, comparison table)
- [ ] T47: Console — Products + Staff + Inventory views

### Checkpoint 6 (slice 6 shipped)
- [ ] All gates green
- [ ] Manual e2e: sales totals/tender, products revenue/profit, staff and branch rows all match the desktop's own screens for a real synced tenant + period
- [ ] POS sale lands in today's report live via SSE, no refresh
- [ ] RTL/Arabic-numerals audit across all five views
- [ ] Human review before Phase 7

### Later phases (outline only — broken down when reached)
- **Phase 7 — Live tier (SignalR):** separate spec, per the main spec's slice 7.

## Risks and mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| HQ reads add load/latency to the gateway serving /sync | Med | Endpoints are per-tenant indexed lookups (T5/T9 are cheap queries); aggregates deferred to Phase 2+; watch before Reports |
| Missed sync-completed callbacks leave stale `last_sync_at` in Mongo | Low | `SyncActivity` in the tenant DB is the truth; `/hq/branch-activity` (T5) reads it and can reconcile; staleness self-heals next round (~5 min) |
| SSE through nginx buffers/times out | Med | `proxy_buffering off` + heartbeat comment every ~25 s (under the 30 s API timeout exemption — SSE route must sit outside the `apiTimeout` group like `/updates/*` already does) |
| Gateway queries hit tables whose columns I've inferred (Bills totals, Shifts open-state) | Med | T9 starts by reading the entity classes in `AribONE.Data/Models/Entities/` (Bill.cs, Shift.cs) before writing SQL; acceptance includes verifying against a real synced DB |
| New gateway surface weakens token isolation | High | HqToken mirrors OpsToken's "different required claims" isolation; db_name always from token, never query params (same rule as /sync); explicit test in T5 |

## Open questions

Carried in the spec (§ Open questions): ~~master-edit gating (blocks Phase 3)~~ **resolved 2026-07-14: ServerWins + conflict alerts for v1**; report aggregate strategy (blocks Phase 6); Customers scope.
