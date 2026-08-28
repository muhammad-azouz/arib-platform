# Implementation Plan: Promotions & automatic discounts

Spec: `tasks/spec-promotions.md` (r2, 2026-08-27) · Tasks: `tasks/todo.md` §Phase 14 (T126–T153)

## Overview

Twenty-eight tasks across four repos, in seven groups that each end in a gate:

- **A (T126–T129, `AribONE.Data`) — schema and sync scope.** Enums, three entities, the
  migration pair, `SyncScope` v17. Zero behaviour: nothing reads or writes these tables yet.
  This is the group that costs a flag day, so it lands once and lands correctly.
- **B (T130–T132, `sync-gateway`) — HQ CRUD.** Six `HqApi` methods and six routes, branch-list
  aware from the first commit.
- **C (T133–T136, `platform/api`) — permissions and forwarding.** Two `perm` codes, the
  service, the handlers, and the D14 scoping rules with tests.
- **D (T137–T141, `platform/console`) — authoring.** Plumbing, list, form dialog, target
  picker, detail page. Ends in the first human checkpoint: a promotion authored in the console
  reaching a branch database.
- **E (T142–T147, `desktop`) — the engine.** The cache loader with descendant expansion, the
  pure engine, cart integration, audit rows. This is where the feature becomes real.
- **F (T148–T150, `desktop`) — the receipt** (D17).
- **G (T151–T153, all) — reporting.**

The ordering is deliberate. **Authoring (D) ships before the engine (E)** so the engine is
exercised against promotions a human actually created through the real UI, rather than rows
hand-seeded with SQL — which is how a targeting or scoping bug hides until production.

**Two tasks have no dependencies and should be built on day one**, for the same de-risking
reason T118 was in Phase 13:

- **T133** (the two `perm` codes) is four lines of Go that nothing calls yet.
- **T143 (`PromotionEngine`) is pure, DB-free, and depends only on T126's enums.** It is the
  most logic-dense task in the plan and the only one whose bugs are silent — a wrong tie-break
  just quietly charges the wrong price. Building it first, with its tests, means the riskiest
  arithmetic in the feature is settled before any plumbing exists to distract from it.

## Architecture decisions (grounded in the code)

- **The engine is a pure static class, and that is a testability decision, not a style one.**
  The desktop repo has **no test projects** (`AribONE.sln` holds exactly two). Best-single-wins
  resolution across percentage/fixed, clamping, thresholds, and a three-level tie-break is
  precisely the logic that cannot be verified by clicking. Keeping `PromotionEngine` free of
  `AribContext` and ViewModel state is what makes T147's minimal xUnit project possible —
  and T147 is **gated on user approval** (spec D13, Boundaries "ask first").

- **One insertion point covers both POS layouts.** `TouchSaleViewModel : NewSaleViewModel`
  inherits all bill math ("all bill math, payment legs, and the save path are inherited
  untouched"). Hooking `CalcBillTotals()` — already the choke point every mutation funnels
  into — means the touch screen gets promotions with zero touch-specific code. T145 is one
  file for two UIs.

- **Group descendant expansion happens at cache load, not per line.** D3's cascade would
  otherwise be a tree walk per line per keystroke. Expanding each `Kind.Group` target to a flat
  `HashSet<Guid>` once, when the sale screen opens, keeps the hot path a single set lookup —
  the same cost as the non-recursive design that was rejected. The walk must be cycle-safe
  (visited set); a corrupt `ParentId` chain would otherwise hang the sale screen, and roots are
  `Guid.Empty`, not null (`ProductGroupCommandService.IsRoot:60`).

- **No new discount columns anywhere.** Promotional money flows through
  `InvoiceLine.Discount` and `Invoice.BillDiscount` exactly as manual money does (D7), so
  `CalcBillTotals`, the totals, the payment legs, and the whole `CashDiscountOut` GL path are
  untouched by this plan. `PromotionApplications` is a pure audit overlay. **No task in this
  plan modifies `Invoice` or `InvoiceLine`** — if one starts to, something has gone wrong.

- **`PromotionApplications` holds no FK to `Promotions`.** It is a cross-tier reference
  (branch row → master row), which is exactly the shape that wedged sync permanently in v15,
  and an audit row must outlive its promotion. A correlation id sidesteps both — the same
  posture `Invoice.OriginalInvoiceId` already takes.

- **Declaration order in `SyncScope` is a correctness constraint, not documentation.** An
  upload batch applies table-by-table in array order. T129 places `PromotionTargets` after
  `Promotions` and `PromotionApplications` after `Invoices`/`InvoiceLines`, and its acceptance
  says so explicitly rather than leaving it to a reviewer's memory.

- **Validation is written twice, on purpose.** API-side (fail fast, one Arabic message) and
  gateway-side (the database is the last line). This mirrors how the console already treats
  every other write; the cost is one duplicated table of rules, the benefit is that a direct
  gateway call cannot store a 150% promotion.

## Dependency graph

```
T126 enums + DiscountType move ──┬── T127 entities + context ── T129 SyncScope v17 ── T128 migration pair
                                 │                              (T129 BEFORE T128 — see below)
                                 │
                                 └── T143 PromotionEngine (PURE — build day one) ──┐
                                                                                   │
T127+T128 ── T130 gateway reads ──┬── T132 routes ── T134 api service ── T135 handlers ──┬── T136 api tests
            T131 gateway writes ──┘                      │                               │
                                                         │                               └── T137 console plumbing
T133 perm codes (NO DEPS — day one) ─────────────────────┘                                    ├── T138 list + nav
                                                                                              ├── T139 form dialog
                                                                                              │     └── T140 target picker
                                                                                              └── T141 detail page
                                                          ══ Checkpoint 14b (human) ══
T127 ── T142 cache loader + group expansion ──┐
T143 ─────────────────────────────────────────┼── T145 CalcBillTotals integration ── T146 audit rows on save
T144 SaleCartItem fields ─────────────────────┘                                          │
T143 ── T147 AribONE.Tests  [GATED on D13 approval]                                      │
                                                          ══ Checkpoint 14c (human) ══   │
T146 ── T148 Preference toggle ── T149 InvoiceDoc fields ── T150 Receipt.frx ═══ Checkpoint 14d
T146 ── T151 gateway performance ── T152 api performance ── T153 console panel ═══ Checkpoint 14e
```

Hard orderings: T127 gates every repo downstream; T129 is what makes the tables actually
sync, so nothing in group E can be verified end-to-end before it; T137 gates every console
task; T146 gates both F and G, because a receipt and a report both need audit rows to exist.

**T129 must run BEFORE T128** — corrected 2026-08-27 while implementing T127; the graph
originally had them as independent siblings off T127. The tail of `OnModelCreating` declares a
`{table}_dms_sync` trigger to EF for every table in `SyncScope.AllTables`, and that trigger
metadata **lands in the model snapshot** (50 `HasTrigger` entries in
`AribContextModelSnapshot.cs` today, confirmed). Generating the migration first would snapshot
the three new tables *without* their trigger declarations, and adding them to `SyncScope`
immediately afterwards would make the snapshot stale — forcing the migration pair to be thrown
away and regenerated. Cheap to get right, annoying to discover from a failing
`has-pending-model-changes`.

T139 and T140 are one dialog and its child; T138/T141 are mutually independent once T137
lands. T148–T150 are strictly sequential (each consumes the previous one's field).

## Verification model

Four gates, one per repo, run per task:

- **`AribONE.Data`**: `dotnet build`, plus `dotnet ef migrations has-pending-model-changes`
  printing "No changes have been made…" **in both migration projects**. The Postgres twin is
  not optional and not deferrable — same change, not later (`desktop/CLAUDE.md`).
- **Gateway**: `dotnet build AribSyncGateway.csproj`. No test project exists, so group B is
  additionally covered by group C's stub-gateway tests and by curl at its own gate.
- **API**: `make test && make vet`, gofmt clean on touched files. Every task in group C adds
  tests; none is verified by inspection.
- **Console**: `pnpm build && pnpm lint` clean, no new warnings beyond the two pre-existing
  `auth.tsx` react-refresh ones.
- **Desktop**: `dotnet build AribONE.csproj`. Tests only if T147 is approved.

**Four tests carry disproportionate weight** and are called out as their own acceptance
criteria rather than folded into a general suite:

1. **Tie-break determinism** (T143) — a smaller *percentage* that resolves to larger *money*
   must win, and re-evaluating an unchanged cart must never flip the winner. Silent-failure
   logic; nothing downstream would catch it.
2. **Idempotency** (T145) — the engine runs on every keystroke. Applying twice must equal
   applying once. A recompute that reads the already-discounted total instead of
   `PriceBeforeTax * Qty` compounds invisibly until a line hits zero.
3. **Reconciliation** (T146) — D6's invariant, asserted against one bill carrying a
   promotional line discount, a manual line discount, **and** a bill promotion at once. This is
   the requirement-7 proof; if it does not hold, the feature has not met its brief.
4. **Cycle safety** (T142) — a `ParentId` cycle must terminate, not hang the sale screen.

**The manual check that matters** is checkpoint 14c's e2e matrix (spec §Phases), run on a real
two-branch tenant. Checkpoint 14b is different in kind: its check is that a row authored in a
browser *arrives in a branch's local database*, which is the one thing no unit test in any of
the four repos can observe.

## Risks and mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| **The v17 flag day** — every branch below schema 17 is cut off (HTTP 426) until it updates, and every tenant needs an `overwrite: true` reprovision | **High** | Spec OQ1, still open and **owned by the user, not this plan**. Groups A–G are all code and none of them require the flag day to have happened; only the rollout does. T129's acceptance records the `overwrite: true` requirement in the `SchemaVersion` doc comment so the operator cannot miss it — the v15 comment documents exactly this trap being fallen into |
| A plain re-provision silently leaves the scope at the old table count — success returned, no `_tracking` tables, no sync at all | **High** | The v15 failure verbatim. Checkpoint 14a provisions a scratch tenant with `overwrite: true` and **asserts the three new `_tracking` tables exist**, rather than trusting the success return |
| Engine runs per keystroke and janks the sale screen | **High** | One DB read at screen open (T142), flat `HashSet` lookups on the hot path, no allocation in the match loop, re-entrancy guard in `CalcBillTotals` (T145). Perf is an explicit T145 acceptance criterion, not an afterthought |
| Promotional and manual discount collide in one column, breaking the audit split | **High** | D11: manual wins, per line and per bill, via `DiscountIsManual` set by the manual entry paths (T144). This is what makes D6's invariant exact rather than approximate — and T146 asserts the invariant directly |
| A malformed `ParentId` cycle hangs the sale screen during group expansion | Med | T142 builds the child index once and walks breadth-first with a visited set; a cycle fixture is an acceptance criterion |
| Engine rounding drifts from the existing manual convention | Med | The codebase deliberately decouples the *stored* `Discount` (quarter-rounded, `RoundToNearestQuarter`) from the *totals* (plain-rounded) — see `RecalcLineForQty:1571`. T143 mirrors this exactly rather than inventing a rule; a fixture pins both numbers |
| Cross-tier FK on `PromotionApplications` wedges sync permanently | Med | Removed by construction (D6 — correlation id, no FK). T129's acceptance additionally pins the `BranchTables` declaration order |
| `Receipt.frx` is hand-edited FastReport XML | Med | T150 is isolated as its own task, touches no `.cs`, and is verified by actually printing — not by the file parsing |
| `DiscountType` move breaks desktop compilation broadly | Low | Namespace-only move (T126); it is referenced in a handful of files, all in `ViewModels/Bills`. `dotnet build` catches every site immediately |
| Desktop ships with no engine tests because D13 is refused | Low | The engine is still written pure (T143), and checkpoint 14c's matrix absorbs the burden. Flagged in T147 rather than assumed |
| A scoped member authors a company-wide promotion | Low | D14: company scope is an unscoped operation, refused server-side (T134) and hidden client-side via `canUnscoped` (T139) — the same two-layer treatment `POST /hq/catalog/products` already gets |
| Order-line promotions read as a bug to a future maintainer | Low | D16 diverges deliberately from the neighbouring `CanMakeDiscount = !FromOrder` rule; T145 requires the comment saying why, or someone will "fix" it |

## Open questions

1. **Spec OQ1 — flag-day timing.** Does not block any task in this plan; blocks the rollout
   only. Needs the current production tenant count and a decision on whether to batch v17 with
   other pending schema work.
2. **Spec D13 — may `AribONE.Tests` be added to `AribONE.sln`?** Blocks **T147 only**. Every
   other task proceeds either way. Answer needed before group E completes.
3. **Default for `ShowItemPromotionsOnReceipt`** (T148). The plan assumes **on** — the feature
   is worth showing by default and a shop that dislikes it will find the toggle. Trivial to
   flip; flagged because it is a customer-visible default, not an implementation detail.
