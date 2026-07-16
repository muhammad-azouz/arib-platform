# Spec: HQ Console — workflow-centric redesign

Source design: Inkdrop note `inkdrop://note/7LVM4Zq0` ("console UI design").
Guiding principle (from the note): design around the decisions HQ needs to make, not around database entities. Every screen answers a real operational question.

## Architecture (as built — read before touching anything)

Four components across sibling repos cooperate; the console sits at the end of the chain.

```
desktop (AribONE, .NET/Avalonia, branch PC)
  └─ SyncService: Dotmim.Sync round every ~5 min (+jitter), branch-filtered
        │  POST /sync  (RS256 sync token minted by the platform API)
        ▼
sync-gateway (AribSyncGateway, one per shard)
  └─ fronts the per-tenant central DB (SQL Server default / Postgres),
     DB-per-tenant, provisions+migrates on first sync, pins BranchId
     from the token. ONLY the gateway holds SQL connection strings —
     the control plane stores just the gateway URL (Shard model).
        │
        ▼
central tenant DB  ←— this is the "cloud DB" the console reads business data from
        ▲
platform/api (Go + Mongo — control plane / "license server")
  └─ accounts, licenses, tenants, branches, devices, shards, updates;
     mints sync tokens (client) and RS256 ops tokens (gateway /admin/*,
     already used by the rollout service). No SQL driver — it cannot
     reach tenant DBs directly.
        ▲
platform/console (React) — talks only to the Go API via nginx (/v1/*)
```

Facts that constrain the console design:

- **Sync scope** (`AribONE.Data/Sync/SyncScope.cs`, schema v6): Tier A master tables replicated in full to every branch (Products, Groups, Barcodes, Accounts, Users, …); Tier B branch documents BranchId-filtered (Bills, BillEntries, Customers, Warehouses\*, InventoryMovements, InventoryBatches, Shifts, BillPayments, OrderFulfillments, …). The central DB therefore already contains everything the console's Overview/Inventory/Orders/Reports screens need — merged across branches.
- **Companies/Branches are cloud-authoritative** in the control plane (Mongo), seeded into central as FK anchors, never DMS-synced.
- **Account aggregates (Debit/Credit/Balance) never sync** (D10) — any console accounting numbers must be recomputed from journal rows, never read from those columns.
- **HQ ordering is modeled but out of scope** (D13): HQ writes Order rows (Bills TPH subtype) into central; the branch syncs them down and answers with OrderFulfillments rows syncing up. The user is redesigning this workflow — **the Orders section is dropped from this pass** and gets its own spec later.
- **Conflict policy is ServerWins (D12)**: the gateway resolves every sync conflict in central's favor and logs the losing write to a per-tenant `ConflictLog` table (central-only, never synced) for review. Because master tables replicate in full, branch-vs-branch and branch-vs-HQ edits to the same master row are the real conflict surface. Note: no synced table carries an `UpdatedAt` column, so timestamp-based last-write-wins is not currently implementable without a schema bump.
- **Data cadence**: a branch's data in central is at most ~6 min stale (5-min interval + jitter) while the branch is up. There is **no last-sync tracking in the control plane today**, and **no SignalR anywhere yet** — it is planned, not built.

### Console read path (decided by the architecture)

Business data reads go **console → Go API → shard gateway → tenant central DB**. Rationale: only the gateway can reach tenant DBs (deliberate — connection strings never leave the shard); the API⇄gateway RS256-token channel already exists (ops tokens, rollout service); the console already speaks only to the API through nginx, keeping auth/session handling in one place. The gateway grows tenant-scoped HQ read endpoints (and, for orders/catalog authoring, HQ write endpoints) authenticated by API-minted tokens, following the existing `/admin/*` + OpsToken pattern.

### Freshness model (maps the note's concept onto the real mechanisms)

| State | Meaning | Mechanism |
|---|---|---|
| `synced` | from central DB; trustworthy to ~6 min | default for everything; `asOf` = that branch's last completed sync round, recorded by the gateway per /sync session and exposed to the API |
| `offline` | branch hasn't completed a sync round recently | derived: last-sync age > threshold (e.g. 🟢 <10 min, 🟡 10–30, 🔴 older) |
| `live` | queried from the branch right now | **later phase** — SignalR hub hosted on the sync-gateway; branch app connects outbound as client; console requests (current shift, live inventory, sync-now nudge) route through the hub. See "Realtime chain" below. |

Until SignalR lands, "branch health" = last-sync recency, which the gateway can start recording immediately. The console renders all three states from day one through one `<Freshness>` primitive; `live` simply never occurs until the hub exists. HQ writes (catalog changes, prices) reach a branch only on its next sync round — the UI must say so ("queued — reaches the branch within ~5 min"), and the SignalR sync-now nudge later shortens that.

### Realtime chain (decided 2026-07-14)

SignalR cannot reach the console: it is a .NET protocol, and the console's backend is the Go API. The realtime path therefore has three legs with different transports — and the console keeps talking only to the API:

```
desktop  ⇄ SignalR (persistent WSS)    ⇄ sync-gateway hub        ← presence source of truth
gateway  → signed HTTP callback         → Go API                  ← existing API⇄gateway token pattern, reversed
Go API   → SSE stream (/v1/…/events)    → console browser         ← EventSource; nginx needs proxy_buffering off
```

- **Presence = hub connection state.** The desktop holds a persistent SignalR connection to a hub on the gateway (natural host: .NET, per-shard, already internet-facing, already validates RS256 tokens). `OnConnectedAsync` = online, `OnDisconnectedAsync` = offline; SignalR keepalive (~15 s ping / 30 s timeout) detects a dead branch within ~30–45 s for free. The same connection later carries the sync-now nudge and live queries.
- **The API is the presence aggregator.** Each shard's gateway reports only its own branches; the API assembles the tenant-wide picture, so the console never knows shards exist.
- **SSE, not WebSocket, to the browser.** One-directional server→browser is all that's needed (console commands are ordinary REST POSTs); native in Go; `EventSource` auto-reconnects; flows through the existing nginx proxy.
- **Debounce flaps:** hold 30–60 s grace before emitting "offline" (a network blip must not flash a branch red at HQ); emit "online" immediately.
- **Reconcile, don't just push:** callbacks can be lost (API restart, network); the API pulls a full presence snapshot from each gateway periodically and on startup — events are the fast path, the snapshot is the truth.
- **The SSE leg ships before SignalR** (slice 1): once the gateway records last-sync per /sync session, it can notify the API "branch X synced" and the console's freshness pills flip live at sync cadence over the same stream. SignalR later only upgrades granularity (~5 min → ~30 s); nothing console-side changes — the events just get faster.

## Objective

Extend the tenant console from its Phase-0 state (Overview, Company, Branches, Download, Settings) into the workflow-centric HQ console of the note:

- **Overview** — company KPIs, branch health, alerts, quick actions.
- **Branches** — each branch presented like a "server": status, last sync, current shift, today's sales; progressive disclosure into diagnostics.
- **Catalog** — groups → products, price, barcode, per-branch availability and inventory.
- **Inventory** — one dataset, three perspectives: by product, by branch, problem-centric ("needs attention").
- **Reports** — organized by question (Sales, Products, Branches, Inventory, Staff).
- **Customers** — customer management and insights, scoped per branch (see "Customers module" below); explicitly not sales or inventory.
- **Global**: branch-status indicator in the top bar, actionable notifications, Ctrl+K quick search, explicit freshness on every branch-derived number.

**Out of scope:** the Orders section (user decision 2026-07-13 — the ordering workflow is being redesigned and will get its own spec later; nothing here may depend on it).

Users are HQ staff. Success = answering "which branch needs attention?", "where is product X?", "why is Branch 5 offline?" without translating database concepts.

## Implementation strategy (decided)

**Vertical slices ("scoop by scoop"), not frontend-first.** The risky work is the HQ read path through the gateway and the freshness plumbing, not the UI; frontend-first would invent data shapes the central schema doesn't have and mock a freshness model that only becomes real end-to-end. Each slice ships something usable. The one advantage of frontend-first — visual coherence — is captured by Slice 0's shared primitives.

**Inside each slice, contract-first:** define the gateway endpoint + API passthrough + TS types together first; frontend may start on mocks the same day; the slice closes only wired end-to-end.

### Slice order

| # | Slice | Repos touched | Notes |
|---|-------|--------------|-------|
| 0 | Foundation: HQ read path + freshness | sync-gateway, platform/api, platform/console | Gateway: HQ token validation + first read endpoint + record last-sync per branch per /sync session. API: mint HQ tokens, proxy tenant-scoped reads, expose last-sync. Console: `{data, source, asOf}` envelope handling, `<Freshness>` pill, final nav shell (IA from the note). |
| 1 | Branches dashboard | platform/console, platform/api, sync-gateway | Branch-as-server card + detail (progressive disclosure). Health = last-sync recency. Today's sales / current shift from central (Bills, Shifts). Includes the SSE leg of the realtime chain: gateway notifies API on each completed sync round; API streams `/v1/…/events` to the console so health/freshness flip live at sync cadence. |
| 2 | Overview | platform/console (+gateway aggregates) | KPI tiles (revenue/sales today from Bills), branch health strip, alerts, quick actions. Tiles grow as later slices land. |
| 3 | Catalog / Products | all three | Read: master tables from central. Write (create product / change price everywhere): HQ write endpoint on gateway into Tier-A tables; propagates on each branch's next sync — show propagation state. Availability per branch from WarehousesProductInventories. |
| 4 | Inventory | platform/console, sync-gateway | The three views are one dataset (WarehousesProductInventories + InventoryMovements/Batches) under a view toggle. "Needs attention": low stock, out of stock, negative inventory, stale branch data. |
| 5 | Notifications + quick search (Ctrl+K) | platform/console, platform/api | Alerts derived from data already flowing (last-sync stale, low stock, **sync conflicts from the ConflictLog table** — the note's "⚠ Price conflict" alert); every alert deep-links to the screen that resolves it. |
| 6 | Reports | platform/console, sync-gateway | Question-organized pages; SQL aggregates on central per tenant. Watch query cost — see open questions. |
| 7 | Customers | platform/console, sync-gateway | List + profile + insights + bulk ops, read-mostly. Reuses the Catalog groups-tree pattern (customer groups are already Tier-A via the `Groups` TPH discriminator) and the Reports slice's Bills-aggregation style for stats. Full detail in "Customers module" below. |
| 8 | Live tier (SignalR) — separate follow-up spec | sync-gateway, desktop, platform/api | **Hub hosted on the sync-gateway** (decided — see "Realtime chain"): branch client + presence + live queries + **sync-now nudge** (decided direction: after an HQ write, broadcast "sync now" to the tenant's branches — SignalR carries no data, it only triggers the normal Dotmim round, so propagation drops from ~5 min to seconds with zero change to sync/conflict semantics; nudges must be coalesced sender-side and staggered 0–15 s per branch to avoid a gateway stampede; the branch routes it through `SyncService.SyncNow()`, whose existing gate makes mid-round nudges a no-op; the 5-min timer stays as fallback for offline branches). Out of scope here; the freshness envelope already reserves `source: "live"` so nothing rewires. |
| 9 | Loyalty program — separate follow-up spec | AribONE.Data, desktop, sync-gateway, platform/api, platform/console | **Full auto-earning program (user decision 2026-07-16), not squeezed into slice 7.** No loyalty/points concept exists anywhere in the schema today; auto-earning at checkout means the desktop POS writes a points-earning event per sale, which needs a new Tier-B table (points ledger, `SyncScope.SchemaVersion` bump = fleet-wide flag day, same cost class as v4's Shift Management or v6's product-type split) plus desktop UI for the earn logic and HQ-console UI for configuring earn-rate and redemption-as-discount at checkout. Real multi-repo project, same size class as Orders/Live-tier — gets its own spec once reached; slice 7 (Customers) ships without it. |

Orders will re-enter as its own spec once the workflow is redesigned; slices 0–6 must not build anything that presumes the D13 shape.

## UX improvements added beyond the note

1. **Actionable alerts everywhere** — every alert deep-links to the screen that resolves it; an alert with no destination doesn't ship. Includes surfacing the gateway's ConflictLog as "price conflict"-style alerts.
2. **Close the loop from inventory to orders** *(deferred with Orders)* — when the redesigned ordering ships, a "needs attention" row should offer a one-click pre-filled replenishment order; design inventory rows so that affordance can slot in.
3. **Freshness as a single primitive** — one `<Freshness>` component + one API envelope, not per-page ad-hoc labels. Consistency is what makes users trust it.
4. **Propagation honesty for HQ writes** — since Tier-A writes (catalog, prices) reach branches only on their next sync round, show it: "queued — reaches Branch 2 within ~5 min", flipping when the branch's sync watermark passes the write.
5. **Stale-while-revalidate, not spinners** — TanStack Query already in place; show last-synced data instantly with the pill, refresh in background. The console never blanks because a branch is slow.
6. **Arabic-first RTL** — console copy is Arabic (the note is English); numerals/dates via existing `format.ts`; layouts mirror.

## Customers module (slice 7)

Source: user-supplied spec, 2026-07-16, adapted from the general note's "Customers" section. Guiding rule carried from the source: stay focused on customer management and customer insights — not sales, not inventory. The module should answer four questions: who is this customer, what's their relationship with the business, what have they purchased and where, and do they need action (credit, inactivity, duplicates)?

### Scope decision: branch-specific, not global (user decision 2026-07-16)

`Customers` and `CustomerTransactions` are Tier-B (`SyncScope.BranchTables`, `AribONE.Data/Sync/SyncScope.cs`) — BranchId-filtered, exactly like Bills/Inventory. There is **no cross-branch customer identity** in the schema: a customer who shops at two branches is two unrelated `Customer` rows with no link between them. The note offered a "global" leg (activity across all branches, spending per branch, favorite branch) and a "branch-specific" leg (owning branch shown, filterable by branch) — **this pass takes the branch-specific leg**, matching the schema as it exists today and how Catalog/Inventory already treat Tier-B data. A true global identity would need a schema/architecture change (new tenant-wide identity or Tier-A promotion, likely a `SchemaVersion` bump) and is out of scope — flagged under Future Features.

Consequence: every customer row in the HQ list carries a mandatory owning-branch badge; there's no "combine these two branches' records" operation, because there's nothing to combine — they're independent business relationships that happen to share a person.

### Scope decision: merge dropped from this pass (user decision 2026-07-16)

"Merge duplicate customers" has no precedent anywhere in the desktop app. A real merge means reassigning `CustomerTransactions`/`Bills`/`JournalEntries` FK rows from a duplicate to a survivor — HQ's first-ever write into branch-owned Tier-B *history* tables (every HQ write so far, slice 3's catalog prices/create, has targeted Tier-A master data). That's a large, risky change that doesn't belong bundled into a first customers slice. **Dropped to Future Features**, alongside loyalty points, membership tiers, gift cards, marketing campaigns, tags/segmentation, and CRM notes — none of which have schema support today either.

### Data grounding (`AribONE.Data/Models/Entities/Customer.cs`, `CustomerTransaction.cs`)

- **List/search scope**: `Customer.Type == CustomerType.Customer` only (the table also holds `Supplier` and `All` rows — this module is not the supplier ledger).
- **Search fields**: name, `Phone1`/`Phone2`/`Phone3`, `Num` (the customer code — same role as `Product.ProductCode`). **No barcode field exists on `Customer`** anywhere in the schema (unlike `Products`→`Barcodes`); the note's "search by barcode" doesn't map to anything real — dropped from v1 search, flagged as a gap rather than silently implemented against a field that doesn't exist.
- **Customer group filter**: free — `Groups` is already Tier-A (`AribContext.cs:206-209`, TPH discriminator `Kind="Customer"` vs `"Product"`), so this reuses the exact groups-tree plumbing Catalog already built in slice 3. No new master-data sync.
- **Active/Inactive filter**: `Customer.IsActive` — already exists, same semantics as `Product.IsActive`. "Deactivate" in the note = flip this flag, non-destructive (never a hard delete — matches the Catalog pattern and preserves the FK history `RemoveCustomer` would otherwise have to untangle).
- **Debt / credit filter, credit-limit insight**: `Customer.CreditLimit`, `Customer.IsCredit` are plain columns, safe to read directly. **`Customer.Debit`/`Credit`/`Balance` are D10-style stored aggregates on a shared row — same rule as `Accounts`: never trust the synced value.** HQ must recompute balance as `SUM(CustomerTransaction.Debit − Credit)` per customer (mirrors `CustomerService.GetLedgerBalanceBeforeAsync`'s desktop-side pattern exactly). "Has debt" / "approaching or exceeding credit limit" filters and insights key off this recomputed balance vs. `CreditLimit`, not the stored columns.
- **Statistics** (total purchases, total spent, last purchase date, average order value, number of orders): computed from `Bills` where `CustomerId = customer.Id`, `Type IN (Sale, ReSale)`, `!IsDeleted` — same bill-type/deletion semantics the Reports slice already established (`plan.md` Phase 6 design notes) for its sales totals. No new aggregation rule to invent; reuse it.
- **Purchase history**: the same `Bills` rows, listed with `BillEntries`, exactly the shape `CustomerStatementViewModel` already renders on the desktop side (`desktop/ViewModels/Customers/CustomerStatementViewModel.cs`) — HQ's version reads the same data through the gateway, scoped to the customer's one branch per the scope decision above.
- **Outstanding balance / credit history**: the recomputed running balance over `CustomerTransactions`, same ledger semantics as `CustomerStatementViewModel`'s balance column.
- **Attached documents**: no schema support (no customer-document/attachment table exists anywhere in `AribONE.Data`). Moved to Future Features, not built against a table that doesn't exist.
- **Pricing tier bulk update**: `Customer.PriceTier` (nullable int) already exists on the entity — the note's "update pricing tier (if supported)" bullet maps directly, no schema change needed.
- **Import/Export**: no existing precedent (unlike products, there's no desktop-side bulk customer import visible in this pass's grounding beyond `ImportCustomerViewModel`, which is worth reading before Plan phase scopes this task) — the desktop already has *a* customer import flow (`desktop/ViewModels/Customers/ImportCustomerViewModel.cs`); Plan should read it before deciding whether HQ import reuses its column mapping or defines its own.

### Screens (from the note, adapted to the decisions above)

- **Customer List** — search (name/phone/code — no barcode, see above); filters: branch (single-select, since customers are branch-owned), customer group, active/inactive, has-debt/credit; create, edit, deactivate (no merge). Table columns mirror the note: name, branch badge, group, balance (recomputed, with freshness), last purchase, status.
- **Customer Profile** — basic info (name, phones, address, notes, group, credit limit) + owning-branch badge; statistics tiles (total purchases, total spent, last purchase date, avg order value, order count); purchase history (paged, deep-links to the order's branch context — no cross-branch Orders page exists yet per the Orders-deferred decision, so a purchase row shows bill detail inline rather than linking out); outstanding balance / credit history (ledger view); attached documents section omitted (Future Features).
- **Insights** — top customers, new customers this month, inactive customers, customers approaching/exceeding credit limit, highest spenders, customer growth over time. All computed per-branch or company-summed-across-branches (sums are safe even without global identity — it's aggregating independent branch relationships, not merging them); each insight row deep-links to that customer's profile.
- **Bulk Operations** — import/export, assign customer group, update pricing tier. Merge and promotions/notifications explicitly excluded (see scope decisions).

### Loyalty points — promoted to its own follow-up spec, not built here (user decision 2026-07-16)

The note listed loyalty points under "Future Features"; the user asked to take it out of that unscoped bucket — but a real auto-earning program (points accrue automatically per sale, redeemable as a checkout discount) needs the desktop POS to write earning events, a new Tier-B ledger table, and a `SyncScope.SchemaVersion` bump (fleet-wide flag day, same cost class as the v4 Shift Management or v6 product-type-split bumps already in the changelog). That's real multi-repo work, not a console-only addition, so it's promoted to **slice 9** in the table above — a separate follow-up spec, same treatment as Orders and the Live tier. It is **not** part of slice 7; the Customers module below ships without any points UI, and Insights/Profile leave room for a future "loyalty" tile the same way inventory rows were left open for a future replenishment affordance.

### Explicitly out of scope / Future Features (unchanged from the note except loyalty, reinforced by the grounding above)

Merge duplicate customers (this pass — see scope decision above), attached documents (no schema), membership tiers, gift cards, marketing campaigns, customer tags/segmentation, CRM notes and follow-up reminders — none have any schema support today and would each need their own spec. Loyalty points is no longer in this bucket — see above, it has a scheduled follow-up spec (slice 9) instead of an open-ended "someday."

## Tech stack

- Console: React 19 + Vite + Tailwind 4 + shadcn-style ui components, TanStack Query/Table, react-router 7, react-hook-form + zod. Arabic RTL.
- Control plane: Go + Mongo, RS256 token minting (existing `auth`/`rollout` patterns).
- HQ data endpoints: ASP.NET minimal APIs on the sync-gateway (existing style: Program.cs + small focused classes), EF Core via `AribONE.Data`'s `AribContext` or raw dialect SQL (`IDbDialect` covers SQL Server/Postgres).
- Live tier (later): SignalR — hub hosted on the sync-gateway, desktop connects outbound; gateway→API signed callbacks; API→console via SSE (see "Realtime chain").

## Commands

- Console: `pnpm dev` / `pnpm build` / `pnpm lint` — in `platform/console/`.
- API: `make test` / `make run` / `make vet` / `make fmt` — in `platform/api/`.
- Gateway: `dotnet build AribSyncGateway.csproj` — in `sync-gateway/`; `run.sh` for local; no test project exists (mirror `sync-poc/` smoke-check style if needed).
- Full platform stack: `docker compose up` in `platform/`.

## Project structure

```
platform/console/src/pages/console/  → console feature pages
platform/console/src/components/     → shared components (Tile, States, TopBar, AppShell, Freshness)
platform/console/src/lib/            → api client, types, query, hooks, format
platform/api/internal/httpapi/       → HTTP handlers per domain
platform/api/internal/<domain>/      → domain services (+ service_test.go beside)
platform/api/internal/store/mongo/   → persistence, one file per collection
sync-gateway/                        → gateway; HQ endpoints as new top-level files (HqApi.cs, HqToken.cs …)
AribONE.Data/                        → shared entities/EF context — schema changes bump SyncScope.SchemaVersion (fleet flag day; avoid unless unavoidable)
```

## Code style

Follow each repo's existing patterns exactly. Frontend data access:

```tsx
const { data: branches, isPending } = useQuery({
  queryKey: ['tenant', tenantId, 'branches'],
  queryFn: () => api.listBranches(tenantId),
})
```

Go: services take the store interface, handlers stay thin, table-driven tests beside the service. Gateway: comment-dense file headers explaining the decision (existing house style), env-var config via `Require()`.

## Testing strategy

- Go API: table-driven tests per service (existing convention); `make test` green before every commit.
- Gateway: no test infra exists; HQ endpoints get coverage via `sync-poc`-style check programs or a new xunit project (**ask first**).
- Console: `pnpm build` (type-check) + `pnpm lint` as the gate; adding a test runner is **ask first**.
- Each slice ends with a manual end-to-end check: real desktop sync into a tenant DB, console showing it with correct freshness.

## Boundaries

- **Always:** run each repo's gate before committing; return branch-derived data in the freshness envelope; keep gateway HQ endpoints tenant-scoped by token claims (db_name from token, never client input — same rule as /sync); keep Arabic RTL copy consistent; recompute accounting numbers from journals (never the D10 aggregate columns).
- **Ask first:** any `AribONE.Data` schema change (SchemaVersion bump = fleet flag day); new dependencies in any repo; Mongo collection additions; docker-compose/nginx changes beyond the SSE `proxy_buffering off` location; adding test infrastructure.
- **Never:** commit secrets/keys (`api/keys/`, `sync-gateway/keys/`); let the console or API hold tenant SQL connection strings; write to tenant DBs outside the gateway; weaken the token rules (db_name/BranchId always from validated token claims); break existing flows (login, setup wizard, /sync, updates proxy).

## Success criteria

- Overview answers "is the company healthy?" in one glance — KPI tiles, per-branch status dots, actionable alerts, no menu navigation.
- Every branch-derived number carries a freshness state; branch health reflects real last-sync recency recorded at the gateway.
- Inventory's three views are one dataset under a toggle; "needs attention" surfaces low stock, out-of-stock, negative inventory, and stale branches unprompted.
- Every alert deep-links to the screen that resolves it.
- Branch online/offline and freshness flips reach the console without a page refresh (SSE at sync cadence from slice 1; ~30 s granularity once the SignalR hub lands).
- HQ writes visibly report propagation per branch.
- Each slice merged with green gates and verified end-to-end against a real synced tenant DB.
- Customers: every list/profile balance and credit-limit check is recomputed from `CustomerTransactions` (never the stored `Debit`/`Credit`/`Balance` columns); every customer row carries an owning-branch badge; group/active/debt filters and insights all deep-link correctly; import/export and pricing-tier bulk update work; no merge UI ships (moved to Future Features).

## Open questions (need user input)

1. ~~**Conflict policy on master tables**~~ resolved 2026-07-14: ServerWins (D12) + ConflictLog for v1.
2. ~~**Report query cost**~~ resolved (plan proceeded past the Phase-6 checkpoint 2026-07-15): direct SQL aggregates on tenant DBs for v1; revisit pre-aggregation only if fleet growth makes it hurt.
3. ~~**Customers section**~~ resolved 2026-07-16: in scope as slice 7, branch-specific (not global), merge dropped to Future Features. See "Customers module" above.
