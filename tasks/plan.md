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

- [ ] T15: API — `totals` block on `GET /v1/tenants/{id}/hq/branches` (company KPIs summed from branch snapshots + offline-branch honesty count)
- [ ] T16: Console — Overview rework: KPI tiles (sales/bills/refunds/open shifts today) with freshness + offline caveat
- [ ] T17: Console — branch health strip (per-branch dot → branch detail)
- [ ] T18: Console — alerts stub (stale-sync alerts with deep links) + quick actions row

### Checkpoint 2 (slice 2 shipped)
- [ ] All gates green
- [ ] Manual e2e: Overview KPI totals match the sum of the Branches cards; desktop "Sync Now" flips the Overview numbers/freshness live without refresh
- [ ] Stale branch (>30 min) appears as an alert whose link opens that branch's detail page
- [ ] Human review before Phase 3

### Later phases (outline only — broken down when reached)
- **Phase 3 — Catalog (slice 3):** master-table reads; first HQ **write** endpoint (product create / price change) + propagation-state UX. **Gated on open question 1** (branch-edit gating decision).
- **Phase 4 — Inventory (slice 4):** three views over WarehousesProductInventories + movements; "needs attention" query.
- **Phase 5 — Notifications + Ctrl+K (slice 5):** alert derivation (stale sync, low stock, ConflictLog), deep links, command palette.
- **Phase 6 — Reports (slice 6):** question-organized report pages. **Gated on open question 2** (aggregate cost).
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

Carried in the spec (§ Open questions): master-edit gating (blocks Phase 3), report aggregate strategy (blocks Phase 6), Customers scope. None block Phases 0–1.
