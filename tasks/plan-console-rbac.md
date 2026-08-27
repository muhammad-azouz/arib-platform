# Implementation Plan: Console RBAC — custom roles for invited members

Spec: `tasks/spec-console-rbac.md` (r2, 2026-08-26) · Tasks: `tasks/todo.md` §Phase 13 (T101–T125)

## Overview

Twenty-five tasks across three repos, in four groups that each end in a checkpoint:

- **A (T101–T106, api only) — the enforcement layer, shipped invisibly.** `internal/perm`,
  roles embedded on the tenant document, a resolve-once middleware that fails closed, and a
  backfill that reproduces today's access exactly. Nothing in the UI changes; nobody's
  access changes. This is the group that must be right.
- **B (T107–T117, api + console) — roles become usable.** Role CRUD, assignment, the `me`
  block on the bundle, and then the console reading one permission source for nav, routes,
  tiles, and buttons.
- **C (T118–T123, gateway + api + console) — branch scoping.** The widest mechanical change
  (12 gateway signatures) plus the two rules that make an allowlist mean anything: row-level
  404s and the D5c refusal of unscoped writes.
- **D (T124–T125, api + console) — invitations stop being silent.**

The ordering is deliberate: **group A ships with zero behaviour change**, so the layer that
every later task trusts is verified against real traffic before any UI depends on it. Group
C is the widest and lands only once the route table exists to tell it which endpoints even
need scoping.

**T118 (the gateway widening) has no dependencies and can be built and merged on day one.**
It is a no-op by construction — an empty branch list preserves today's "all branches"
behaviour — so it de-risks the largest mechanical diff in the plan long before anything
calls it differently.

## Architecture decisions (grounded in the code)

- **`internal/perm` is a pure package with no store dependency.** The catalog is a Go map,
  `Can` and `Normalize` are pure functions over string slices. No mocks, no context, no
  fixtures — the most security-critical logic in the feature is also the cheapest thing in
  the repo to test exhaustively. Everything that needs Mongo lives in `tenant`/`httpapi`.

- **The route→permission table lives beside the router in `server.go`.** Adding a route and
  declaring its permission is then one diff in one file, and a reviewer sees both at once.
  Putting the table in `perm` would split the two halves of a decision across packages and
  invite exactly the drift the coverage test exists to catch.

- **`MemberByAccount` is the enabling one-line change.** `mongostore.MemberRole`
  (`tenant_members.go:38-52`) already decodes the whole `TenantMember` and returns only
  `.Role`. Widening it to return the row makes `role_id` and `branch_ids` free on every
  request. Two test files build fakes against the current signature
  (`hq/service_test.go`, `tenant/service_test.go`) and need the same widening — that is the
  entire blast radius, checked before planning rather than discovered during it.

- **Roles embedded on the tenant document means the store API is four array operations**
  (`$push`, `$set` with `arrayFilters`, `$pull`, and a read that comes free with
  `TenantByID`). Assigned counts for the delete-guard come from a `tenant_members` count
  keyed on `role_id`, never from the role itself — a role does not know who holds it, and
  keeping it that way avoids a second write on every assignment.

- **One resolution per request, two consumers.** The middleware builds a `perm.Scope`
  (member row + resolved permissions + branch allowlist), enforces the route's code, and
  stashes it. `tenant.owned()`/`memberRole()` (12 call sites behind 2 helpers) and
  `hq.resolveGateway()` (every one of ~40 HQ methods) read it from context. Both keep a
  lookup fallback when the context is empty, so existing tests and any future non-HTTP
  caller still work unchanged — this is what lets group A land without touching a single
  service test.

- **The gateway change is mechanical, and the parse side collapses.** Eight copies of
  `Guid? branchId = Guid.TryParse(qs["branch_id"].FirstOrDefault(), out var bid) ? bid : null;`
  in `Program.cs` become one `BranchScope.From(qs)`; 12 `Guid? branchId` signatures in
  `HqApi.cs` become `IReadOnlyList<Guid> branchIds`; each `q.Where(x => x.BranchId == b)`
  becomes `q.Where(x => branchIds.Contains(x.BranchId))`, which EF translates to a SQL `IN`
  on both dialects. An empty list skips the filter entirely, which is why the whole change
  is a no-op until the API sends something.

- **Console plumbing lands before any page edit (T110 before T111–T117).** One `useCan()`
  reading the bundle's `me` block — which `SetupGate`, `AppShell`, and `CommandPalette`
  already fetch, so there is no extra request — is the single source for nav visibility,
  route guards, tile composition, and button gating. If page work started first, each page
  would invent its own check and D10's two rules would be applied inconsistently.

- **Manage-gating is split across three tasks by page group, not done in one sweep.**
  T115–T117 each touch 3–5 files. A single "gate every write button" task would touch a
  dozen and be unreviewable, and these are the edits most likely to miss a spot.

- **D5c is enforced in the service, not the route table.** Whether an operation is
  unscoped is a property of the operation (a price change reaches every branch), not of the
  request, so it belongs next to the branch check in `resolveGateway`'s consumers — one
  `scope.RequireUnscoped()` line at the top of the five affected methods.

## Dependency graph

```
T101 perm package ─┐
                   ├── T104 middleware (resolve once, stash, fail closed) ─┬── T105 services read scope
T102 model + store ┤                                                       └── T106 coverage test
       │           └── T107 role CRUD ──┬── T109 PATCH member ──────┐
       └── T103 backfill                │                           │
                                        └── T108 `me` on bundle ── T110 console perm plumbing
                                                                     ├── T111 route guard + retry fix
                                                                     ├── T112 roles tab + grid ── T113 assign dialog
                                                                     ├── T114 composition (Overview/bell/⌘K)
                                                                     └── T115/T116/T117 manage-gating by page group

T118 gateway widening (NO DEPS — build day one) ── T119 API scope injection ─┬── T120 row 404 + D5c
                                                                             ├── T122 SSE filter
T105 ───────────────────────────────────────────── T121 control-plane branch routes
T113 + T120 ──────────────────────────────────────── T123 branch picker + scoped banner

T109 ── T124 SendInvite ── T125 AcceptedAt + pending badge
```

Hard orderings: T104 gates everything in group A; T110 gates every console task; T119 gates
every scoping task. T118 is free-floating and should be merged early. T115–T117 are
mutually independent and parallelizable once T110 lands.

## Verification model

Three gates, one per repo, run per task:

- **API**: `make test && make vet` — table-driven tests beside each service, the existing
  convention. Every task in groups A, C, D adds tests; none is verified by inspection.
- **Gateway**: `dotnet build AribSyncGateway.csproj`. No test project exists (spec
  §Testing Strategy), so T118 is additionally verified by the API's `httptest` stub-gateway
  tests in T119 and by the checkpoint-13c manual run.
- **Console**: `pnpm build && pnpm lint` clean, no new warnings beyond the two pre-existing
  `auth.tsx` react-refresh ones.

**The manual check that matters** runs at every checkpoint from 13b onward: a second browser
profile signed in as a real invited member, against a tenant with ≥2 branches. Checkpoint
13a is different — its manual check is that *nothing changed*, verified by an existing
member reaching every screen exactly as before.

Two tests carry disproportionate weight and are called out as their own acceptance criteria
rather than left to a general suite:

1. **Access preservation** (T103) — a member that existed before the migration can still
   reach everything they could before. This is the one that stops a deploy from locking
   people out.
2. **Scope escape** (T120) — a branch-scoped member with `catalog.manage` is refused a
   price change. This is the hole r1 of the spec would have shipped.

## Risks and mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| Backfill mis-maps existing members → people lose access on deploy | **High** | T103 ships an access-preservation test as an acceptance criterion, is idempotent across restarts (precedent: `BackfillOwnerMembers` + `InsertMemberIfAbsent`), and logs seeded/updated counts so a wrong run is visible immediately rather than as a support ticket |
| A new write endpoint ships unscoped and escapes the allowlist (D5c) | **High** | The middleware denies unmapped tenant-scoped routes (fail closed), so an unguarded route 403s rather than leaking; T106's coverage test turns that runtime failure into a build-time one; T120 tests the price-change case explicitly |
| 12 gateway signatures + 8 parse sites is a wide mechanical diff | Med | T118 is a standalone no-op commit — empty list preserves behaviour, nothing calls it differently, `dotnet build` verifies it in isolation before any API change exists |
| API deployed before gateway in group C | Med | An old gateway binds only the *first* `branch_id`, so a two-branch member sees one — under-fetch, not a leak. Deploy order is an explicit checkpoint-13c item |
| Overview/bell/⌘K leak other sections' data (D10) | Med | T114 is its own task rather than a footnote on the Overview page, with a test that a `reports.view`-only member's Overview payload contains no inventory or conflict data |
| The two enforcement layers drift apart | Med | D9: one resolution, two consumers. The middleware decides *may you call this*, the service decides *which rows*; neither re-checks the other's question, so there is nothing to drift |
| Role edit locks the owner out | Low | Owner is a hard-coded constant, never a role row — there is no edit that can reach it |
| `GET /permissions` codes drift from the console's Arabic labels | Low | The grid renders the raw code when a label is missing, so a new code is visibly unlabelled rather than silently absent |
| Widening `MemberRole` breaks test fakes | Low | Exactly two files (`hq/service_test.go`, `tenant/service_test.go`), verified before planning |
| Backfill races across API replicas | Low | Idempotent by construction; the API is single-instance today anyway (`hq/events.go`'s EventBus already depends on that) |
| A branch added later is invisible to scoped members | Low | Fail-closed and intended (spec D4); T123's assign dialog says so in the UI, which is the only place it could otherwise read as a bug |

## Open questions

Carried from the spec; each names the task it blocks so it is answered before that task
starts, not during it.

1. **OQ1 — delegated administration.** Not blocking; `members.manage` stays owner-only
   throughout. Only revisit if the answer changes before T109.
2. **OQ2 — conflicts have no branch column.** Affects T120: with no branch dimension,
   conflicts cannot be filtered. Default is to leave them visible and say so in the UI.
   Needs an answer before T120, not before group A.
3. **OQ3 — export under a `view`-only role.** Affects T116. Default: `customers.view`
   permits export, as specified. A "yes, gate it" answer adds one code and one checkbox.
4. **OQ4 — `POST /sync-token`.** Blocks **T121**. It is consumed by the desktop app with an
   account session, not by the console, so gating it wrongly breaks desktop activation for
   member accounts. Interim rule: any member, branch must be in the allowlist. This needs
   someone who knows the activation flow before T121 is written.
5. **OQ5 — no audit trail.** Accepted residual risk (ownership transfer and audit log were
   both offered and declined). Not blocking any task.
