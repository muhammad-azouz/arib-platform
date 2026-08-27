# Spec: Console RBAC — custom roles for invited members

Extends `spec-console.md`. Scope: the tenant console's *invited members* — who they are,
what they may do, and which branches they may see. Nothing here touches the desktop POS's
own `Users` table (Tier-A, synced, branch cashiers); that is a separate identity system
that happens to share the word "user".

Revision 2 (2026-08-26) — self-review pass. Changes from r1 are marked **[r2]**; the
substantive ones are D5's scope-escape rule, D10's composition rule, roles embedded on the
tenant document, and the removal of the dotted-hierarchy permission scheme (which was
wrong as specified).

## State of the world (read before touching anything)

`TenantMember` today has exactly two roles (`api/internal/model/model.go:230-249`):

```go
RoleOwner  MemberRole = "owner"  // the tenant's original creator; can't be revoked (T13)
RoleMember MemberRole = "member" // invited (T14); scope of what a member can do is still just "console access"
```

That trailing comment is the whole problem. `membership.Require` returns the role, and
**every caller discards it** — `tenant/service.go:978`, `hq/service.go:115,277,354`. The
only role check anywhere is `role != model.RoleOwner` inside `InviteMember`/`RevokeMember`
(`tenant/service_members.go`). An invited member therefore has byte-for-byte the same
authority as the owner across all ~50 tenant-scoped routes. `Settings.tsx:41` hides the
invite button from non-owners and its own comment admits this is cosmetic.

Also true today:

- `InviteMember` sends **no email** — it creates a bare `Account` and the person is a full
  member instantly, notified out-of-band or not at all.
- A member's role is **fixed at invite time**; there is no update path, only revoke +
  re-invite.
- The gateway's HQ query layer takes `Guid? branchId` — one optional branch — in **12**
  method signatures (`HqApi.cs`), parsed by the same one-liner repeated 8× in `Program.cs`:
  `Guid? branchId = Guid.TryParse(qs["branch_id"].FirstOrDefault(), out var bid) ? bid : null;`
- `mongostore.MemberRole` decodes the **entire** `TenantMember` row and returns only
  `.Role` (`tenant_members.go:38-52`) — every other field is already paid for and thrown
  away. **[r2]**

## Objective

Let a tenant owner **build a role, tick the permissions it should have, and assign it to a
member** — optionally scoped to specific branches. Success = the owner can hand a
bookkeeper read-only reports, give a branch manager full control of branches ٢ and ٧ only,
and know the server enforces both.

Users: the tenant owner (author of roles) and invited members (subject to them). The Arib
admin console has its own `requireAdmin` path and is out of scope.

## Decisions

### D1 — Roles are per-tenant, embedded on the tenant document **[r2 — was a separate collection]**

Roles live as a `roles []TenantRole` array on the existing `Tenant` document, not in a new
`tenant_roles` collection. Reasons, in order of weight:

1. **Zero extra reads.** Both authorization choke points — `tenant.owned()` and
   `hq.resolveGateway()` (`hq/service.go:111`) — already call `TenantByID` on every single
   tenant-scoped request. Embedding means resolving a member's permissions costs nothing
   beyond what is already fetched. A separate collection adds a point read to every
   request in the console.
2. **Bounded and always co-accessed.** A tenant has a handful of roles, they are never
   queried independently of their tenant, and they are always needed when the tenant is.
   That is the textbook case for embedding.
3. **Fewer moving parts** — no new store file, no cross-collection consistency, and a role
   edit is one atomic document update.

Cost: name uniqueness is an application check rather than a unique index (we validate the
name anyway), and concurrent edits to two different roles must use a positional array
update (`$set` with `arrayFilters`) rather than rewriting the array.

Chosen over fixed roles and role+overrides because the required workflow is *create →
configure → assign*, which only per-tenant role documents support (user decision).

`owner` is **not** one of these roles. It stays the hard-coded `MemberRole` constant:
always every permission, never deletable, never editable, never branch-scoped, exactly one
per tenant. `ErrCannotRemoveOwner`, billing, and role-management authority all hang off it,
and making it editable would let an owner lock themselves out of their own tenant.

| `Role` | `RoleID` | meaning |
|---|---|---|
| `owner` | empty | hard-coded full access, unscoped |
| `member` | `rol_…` | permissions resolved from the tenant's embedded role |

`membership.Require`'s contract and every existing owner check survive unchanged.

### D2 — `members.manage` is owner-only and not assignable

A role cannot grant role- or member-management. A member who can edit roles can grant
themselves every other permission, so a delegable `members.manage` is not a permission, it
is ownership with extra steps. Billing is owner-only for the same reason. Delegated
administration, if ever wanted, needs a privilege-escalation rule (*a role may only grant
permissions its author holds*), not a checkbox.

### D3 — 14 permission codes, per-section `view`/`manage`, no hierarchy **[r2 — hierarchy removed]**

r1 specified dotted hierarchical codes so that a future `catalog.price.write` would "walk
up to `catalog.manage`". **That was wrong**: a prefix walk from `catalog.price.write`
yields `catalog.price` then `catalog` — it never reaches `catalog.manage`. The scheme
could not have worked as described, and it was forward-compatibility machinery for a
granularity the user explicitly did not choose.

Replaced with the whole rule, which fits in one sentence: **`Can` is exact set membership,
plus `X.manage` implies `X.view`.** If finer codes are ever added, the migration that adds
them expands existing roles explicitly — a one-off loop, not a permanent resolution
scheme.

**The catalog** — the UI auto-ticks `view` when `manage` is ticked:

| Section | `view` | `manage` covers |
|---|---|---|
| الفروع (Branches) | `branches.view` | `branches.manage` — add/rename branch, bind device, release seat |
| الكتالوج (Catalog) | `catalog.view` | `catalog.manage` — create product, change prices |
| المخزون (Inventory) | `inventory.view` | — (no HQ inventory writes exist) |
| العملاء (Customers) | `customers.view` | `customers.manage` — create/edit/bulk/import |
| الموردون (Suppliers) | `suppliers.view` | `suppliers.manage` — same shape |
| الطلبات (Orders) | `orders.view` | `orders.manage` — create, cancel, transfer |
| التقارير (Reports) | `reports.view` | — |
| التعارضات (Conflicts) | `conflicts.view` | `conflicts.manage` — acknowledge |
| النشاط التجاري (Company) | — | `company.manage` — edit company profile |

**Always visible to every member, no code required [r2]:** نظرة عامة (Overview — it is a
composition, see D10), تنزيل التطبيق (Download), الإعدادات (Settings — it shows the member
list, which any member may read today; role and member *management* inside it stays
owner-only). Gating these bought nothing and created the "role with zero permissions =
blank console" dead end.

Owner-only, never in the catalog: `members.manage`, billing, tenant creation/deletion.

Nav rule: **a nav item renders iff its `view` permission resolves true.** No permission, no
menu entry — never a greyed-out entry that 403s on click.

### D4 — Branch allowlist lives on the member, not the role

`TenantMember.BranchIDs []string`; empty means *all branches*. Stored on the member because
a role ("مدير فرع") is reusable across people who each manage different branches — putting
branches on the role would force one role per branch. The owner picks role and branches in
the same dialog.

**A branch created later is not added to any allowlist [r2].** Only the empty (unscoped)
state is auto-inclusive. This is fail-closed and correct, but it will be reported as a bug
unless the assign dialog says so.

Owner is always unscoped and cannot be scoped.

### D5 — Branch scoping: SQL-enforced, and scoped members cannot perform unscoped operations

The expensive decision, so the reasoning is recorded in full.

#### D5a — The gateway widens to a branch *list*

Three options were weighed:

1. **API-side param rewriting only** — reject a disallowed `branch_id`, inject the
   member's branch when they have exactly one. Cheapest, but *wrong for anyone scoped to
   two or more branches*: "no `branch_id`" still means "all branches" at the gateway, so
   Overview KPIs, reports, and exports leak every other branch.
2. **N parallel gateway calls merged in Go** — breaks paging (page 2 of a merge is not the
   merge of page 2s), breaks every aggregate that isn't a plain sum (averages, top-N,
   insights), and multiplies gateway load per request.
3. **Widen the gateway signature to a list** — `Guid? branchId` → `IReadOnlyList<Guid>
   branchIds`, empty preserving today's "all branches". Each
   `q.Where(x => x.BranchId == b)` becomes `q.Where(x => branchIds.Contains(x.BranchId))`.
   12 signatures, and the 8 repeated `Guid.TryParse(qs["branch_id"]…)` lines collapse into
   one shared `BranchScope.From(qs)` helper. Every aggregate, page, and export stays
   correct because the filter is still one SQL query.

**Option 3.** The API sends `branch_id` repeated; the gateway reads all occurrences.
Repeated `branch_id` already has precedent in this codebase — the customer-import endpoint
deliberately uses it over CSV (`Program.cs:1153-1155`). The list is computed server-side
from the member's allowlist intersected with any user-supplied filter, **never** taken from
the client alone — the same rule as `db_name`.

#### D5b — Four endpoint categories, four rules

- **Branch-dimensioned lists and aggregates** (reports, inventory, orders, customers,
  suppliers, branch-activity, branch-snapshot): filtered by the resolved list.
- **Tier-A master data** (`/hq/catalog/groups`, `/hq/catalog/products`,
  `/hq/customer-groups`): *not* branch-scoped — products and groups replicate to every
  branch and have no branch dimension. A scoped member with `catalog.view` sees the full
  product list; the per-branch **availability arrays inside a product detail are filtered**.
- **Row-level detail by id** (`/hq/{customers,suppliers,orders}/{id}` and their
  `/purchases`, `/ledger` sub-resources): out-of-allowlist returns **404, not 403** — a
  scoped member must not be able to probe which ids exist at branches they cannot see.
- **Writes with a target branch** (order create, order transfer, customer/supplier create):
  the target must be in the allowlist, else 403. Transfer checks the **destination** too —
  a scoped member must not push an order into a branch they cannot see.

#### D5c — Scope escape: unscoped operations require an unscoped member **[r2 — new, was a hole]**

r1 missed this and it is the sharpest gap in the spec. `ChangeProductPrices`
(`hq/service.go:672`) takes no branch at all — it is a **Tier-A write that changes the
price at every branch in the tenant**. Under r1, a member scoped to branch ٢ holding
`catalog.manage` could reprice the entire company. The allowlist would have been decorative
for exactly the most damaging write in the console.

The rule that closes it, and generalizes: **an operation that carries no branch identity
cannot be authorized by a branch allowlist, so it requires an unscoped member.** A
branch-scoped member is refused (403) on:

| Operation | Why it has no branch identity |
|---|---|
| `PUT /hq/catalog/products/{id}/prices` | Tier-A — lands at every branch |
| `POST /hq/catalog/products` | Tier-A — the product appears everywhere |
| `PUT /company` | company-wide |
| `POST /branches` | creates a branch that, by D4, they could not then see |
| `PUT /hq/{customers,suppliers}/bulk`, `POST …/import` | operate over a set the member may not fully own — permitted only if every affected row is in scope, else refused |

`catalog.view` remains fully usable while scoped; only the writes are refused. The console
hides those buttons for scoped members rather than letting them 403.

#### D5d — Control-plane branch routes are in scope too **[r2 — new, was missing]**

r1 framed branch scoping entirely around `/hq/*` and forgot that four **control-plane**
routes are also branch-identified and account-session authenticated:

| Route | Rule |
|---|---|
| `PATCH /branches/{branchId}` (rename) | `branches.manage` + branch in allowlist |
| `POST /branches/{branchId}/bind` (bind a device / consume a seat) | `branches.manage` + branch in allowlist |
| `POST /devices/{deviceId}/release` | `branches.manage` + the device's branch in allowlist |
| `POST /sync-token` (mints a device sync token) | branch in allowlist — permission TBD, see OQ4 |

`tenant.ownedBranch()` (`service.go:998`) is the single choke point for the first three.

#### D5e — Also branch-filtered

The SSE stream (`hq/events.go`): events carry `BranchID`, so it is a one-line predicate at
subscribe time.

`ConflictLog` has **no branch column** (`sync-gateway/ConflictLog.cs`), so conflicts cannot
be branch-filtered — see OQ2.

### D6 — Invitations become real; no separate accept token

`InviteMember` sends an email through the existing `mail.Sender` (a new `SendInvite` beside
`SendOTP`, `mail/mail.go:42`).

**Access is granted immediately; "pending" is informational.** There is no accept link and
no accept token, because OTP sign-in already proves the invitee controls that mailbox — an
accept token would prove the same fact twice and add a lifecycle to expire, resend, and
revoke.

**[r2]** Store only `InvitedAt` and `AcceptedAt *time.Time`; "pending" is *derived* from
`AcceptedAt == nil`. r1 also carried a `Status` enum, which was a second source of truth
for the same fact. `AcceptedAt` is stamped once, on the member's first authenticated
request on that tenant — a single write per member for their entire lifetime, issued
best-effort so it can never fail a read.

### D7 — Role changes take effect on the next request

No session invalidation, no token versioning. Permissions resolve per request from the
tenant document (already fetched), so a revoked permission stops working immediately
without touching the session layer. The console's cached copy may be one refetch stale; the
server is authoritative, so the worst case is a button that 403s.

### D8 — Deleting a role that is still assigned is refused

`DELETE …/roles/{id}` returns 409 with the assigned member count. Reassign first. Chosen
over cascade-to-least-privilege (silently changes what people can do) and cascade-delete-
members (silently removes access).

### D9 — Resolve once per request, in middleware, into the request context **[r2 — new]**

r1 specified a route-table middleware for permission codes *and* service-level checks for
branch scope: two mechanisms, and a second Mongo read per request on top of the one every
service already does.

Instead: **one middleware resolves the member row, their role's permissions, and their
branch allowlist exactly once, enforces the route's permission code, and stashes the result
in the request context.** The services' existing choke points read the scope from that
context instead of re-querying:

- `tenant.owned()` / `tenant.memberRole()` — 12 call sites behind 2 helpers.
- `hq.resolveGateway()` — every one of ~40 HQ methods calls it.
- `hq.CheckOwnership()` — the SSE endpoint only.

Both layers now consume one resolution: the middleware decides *may you call this route*,
the service decides *which rows*. Neither re-checks the other's question, so they cannot
drift. Services keep a lookup fallback when the context is empty, so existing tests and any
non-HTTP caller still work.

**Cost, honestly:** widening `mongostore.MemberRole` to return the full `TenantMember`
(which it already decodes and discards) makes `role_id` and `branch_ids` free. Permissions
come from the tenant document, also already fetched. **Net additional reads per request:
zero.** Two test files construct fakes against the current signature (`hq/service_test.go`,
`tenant/service_test.go`) and need the one-line widening.

**Fail closed [r2]:** the middleware **denies** any tenant-scoped route absent from the
permission table. r1 made the coverage *test* the safety property; a test can be skipped
and a route can be added on a branch that never ran it. The middleware is the safety
property; the test is the CI-time developer experience that tells you at build time instead
of at 3am.

### D10 — Composition rule: tiles gate themselves; embedded row data does not **[r2 — new]**

r1 said nothing about screens that compose data across sections, and there are three:

- **Overview** fetches `hq-branches`, `inventory-attention`, `conflicts`, and `subscription`
  (`Overview.tsx:47-53`).
- **The notifications bell** derives from the same inputs via `deriveAlerts`
  (`lib/alerts.ts:30`).
- **Ctrl+K** searches products via `useCatalogProducts` (`CommandPalette.tsx:114`).

Under r1 these would have handed a reports-only member the full inventory and conflict
picture. Two rules settle every case of this, now and later:

1. **A composed view is not a data source.** Each tile, alert, and search category requires
   its own section's `view` permission and is simply absent otherwise. Overview, the bell,
   and the palette degrade to what the member may see. The API returns partial payloads
   rather than 403ing the whole page.
2. **Data embedded in a row the member may read is not separately gated.** An order detail
   shows its customer's name, phone, and address to anyone with `orders.view`, without
   `customers.view` — that data *is* part of the order. What `customers.view` gates is the
   customer as a *separate resource*: the profile link, the customers list, the ledger.

Without rule 2 every screen becomes a permission maze and the implementation drifts
screen by screen; without rule 1 the Overview is a bypass for everything.

## Data model

```go
// model.go — new, embedded on Tenant
type TenantRole struct {
    ID          string    `bson:"id"`
    Name        string    `bson:"name"`         // owner-authored, unique per tenant (app-checked)
    Permissions []string  `bson:"permissions"`  // codes from the D3 catalog, >= 1
    CreatedAt   time.Time `bson:"created_at"`
    UpdatedAt   time.Time `bson:"updated_at"`
}

// Tenant gains
Roles []TenantRole `bson:"roles,omitempty"`

// TenantMember gains
RoleID     string     `bson:"role_id,omitempty"`    // empty for owner
BranchIDs  []string   `bson:"branch_ids,omitempty"` // empty = all branches
InvitedAt  time.Time  `bson:"invited_at"`
AcceptedAt *time.Time `bson:"accepted_at,omitempty"` // nil = pending
```

### Backfill (startup, idempotent — same shape as `BackfillOwnerMembers`)

Per tenant: seed **«وصول كامل»** (every code) and **«قراءة فقط»** (every `.view` code),
then point each existing `Role: "member"` row at «وصول كامل», unscoped, with `AcceptedAt`
set to `CreatedAt` (they already have access; they are not pending). **Nobody's access
changes on deploy** — verified against a real tenant with existing invited members. Seeded
roles are ordinary editable rows, with no protected flag. **[r2 — `IsSeeded` dropped, it
was never read.]**

## API surface

```
GET    /v1/tenants/{id}/roles              any member   list roles + assigned counts
POST   /v1/tenants/{id}/roles              owner only   create (>= 1 permission)
PUT    /v1/tenants/{id}/roles/{roleId}     owner only   rename / set permissions
DELETE /v1/tenants/{id}/roles/{roleId}     owner only   409 if assigned
GET    /v1/tenants/{id}/permissions        any member   the code catalog, for the grid
PATCH  /v1/tenants/{id}/members/{memberId} owner only   NEW — set role_id + branch_ids
```

`GET /v1/tenants/{id}` (the bundle) gains a `me` block, so the console renders nav and
buttons from a payload `SetupGate`/`AppShell`/`CommandPalette` already fetch — no extra
request:

```jsonc
"me": {
  "role": "member",
  "role_id": "rol_…",
  "role_name": "مدير فرع",
  "permissions": ["branches.view", "orders.view", "orders.manage", …],
  "branch_ids": ["br_2", "br_7"]   // [] = all
}
```

`GET …/members` rows grow `role_id`, `role_name`, `branch_ids`, `accepted_at`.

### Error contract **[r2 — was unspecified]**

A permission denial is `403` with the existing coded-error shape (precedent:
`tenant_handlers.go:427-429`):

```json
{ "error": "ليس لديك صلاحية للوصول إلى هذا القسم", "code": "forbidden_permission", "required": "catalog.manage" }
```

`code: "forbidden_scope"` for a branch-allowlist refusal, `"forbidden_unscoped"` for D5c.
Distinct codes exist so the console can render the right message — "you can't do this" vs.
"you can't do this *here*" vs. "this affects all branches". Out-of-allowlist row reads stay
**404 with no code**, per D5b.

## Tech stack

No new dependencies in any repo. Go + Mongo (control plane), ASP.NET minimal APIs + EF Core
(gateway), React 19 + TanStack Query + react-hook-form/zod (console), Arabic RTL.

## Commands

- API: `make test` / `make vet` / `make fmt` / `make run` — in `platform/api/`.
- Console: `pnpm build` / `pnpm lint` / `pnpm dev` — in `platform/console/`.
- Gateway: `dotnet build AribSyncGateway.csproj` — in `sync-gateway/`; `run.sh` for local.
- Stack: `docker compose up` in `platform/`.

## Project structure

```
platform/api/internal/perm/                   → NEW: code catalog, Resolve, Can (+ perm_test.go)
platform/api/internal/model/model.go          → TenantRole, Tenant.Roles, TenantMember fields
platform/api/internal/store/mongo/tenants.go  → role array CRUD (arrayFilters), assigned counts
platform/api/internal/store/mongo/tenant_members.go → MemberRole → MemberByAccount; role/branch writes
platform/api/internal/tenant/service_roles.go → NEW: role service (+ _test.go)
platform/api/internal/tenant/service_members.go → invite email, AcceptedAt, PATCH member
platform/api/internal/httpapi/role_handlers.go  → NEW
platform/api/internal/httpapi/middleware.go     → requirePerm + route table + context stash
platform/api/internal/hq/service*.go            → branch-list threading via resolveGateway
platform/api/internal/mail/mail.go              → SendInvite
sync-gateway/HqApi.cs                           → 12 × Guid? branchId → IReadOnlyList<Guid>
sync-gateway/Program.cs                         → BranchScope.From(qs) helper replaces 8 parse lines
platform/console/src/lib/perm.ts                → NEW: useCan, nav + tile filtering
platform/console/src/components/RequirePerm.tsx → NEW: route guard
platform/console/src/pages/console/Settings.tsx → members + roles tabs
platform/console/src/components/RoleFormDialog.tsx   → NEW: name + permission grid
platform/console/src/components/AssignRoleDialog.tsx → NEW: role + branch allowlist
```

## Code style

Follow each repo's existing patterns exactly. Permission checks read as one line at the top
of a service method, mirroring how `membership.Require` is already used:

```go
// Roles are owner-authored per tenant; a member may read the list (it labels
// the members table) but only the owner may write one.
func (s *Service) CreateRole(ctx context.Context, accountID, tenantID, name string, codes []string) (*model.TenantRole, error) {
	role, err := s.memberRole(ctx, tenantID, accountID)
	if err != nil {
		return nil, err
	}
	if role != model.RoleOwner {
		return nil, ErrOwnerOnly
	}
	codes, err = perm.Normalize(codes) // rejects unknown codes, dedupes, adds .view for every .manage
	if err != nil {
		return nil, err
	}
	...
}
```

Frontend gating is a hook, never an inline string comparison:

```tsx
const can = useCan()
{can('orders.manage') && <Button onClick={createOrder}>طلب جديد</Button>}
```

Gateway: comment-dense file headers explaining the decision (house style), `Require()` for
env config.

## Console specifics **[r2 — r1 covered only nav]**

- **Route guard.** Nav filtering alone leaves direct URL entry rendering the page, firing
  queries, and landing on a raw error. A `<RequirePerm code="catalog.view">` wrapper in
  `App.tsx` redirects to Overview with a toast instead.
- **Do not retry 403s.** `query.ts` sets `retry: 1` globally, so today every denial costs
  two round trips and doubles the log noise. Narrow it to
  `retry: (n, err) => isServerError(err) && n < 1`.
- **Scoped-member banner.** A branch-scoped member sees a persistent, quiet indicator of
  which branches they are seeing, so an incomplete KPI is never mistaken for a company
  total.
- **Hide, don't 403.** Buttons for D5c unscoped writes are absent for scoped members.

## Testing strategy

- **Go**, table-driven beside each service (existing convention). Mandatory:
  - `perm`: exact match, `manage ⇒ view`, unknown-code rejection, empty-role rejection.
  - **Route-coverage test**: walks the chi router, fails on any tenant-scoped route absent
    from the permission table. The middleware already denies these (D9); this test turns a
    runtime 403 into a build-time failure.
  - Branch scoping per category: list filtered, detail 404s out-of-allowlist, write to a
    disallowed branch 403s, transfer **destination** checked, **D5c unscoped writes refused
    for scoped members** (the price-change case above all).
  - Composition: a member with only `reports.view` gets an Overview payload containing no
    inventory or conflict data.
  - Backfill idempotency across two startups, and an access-preservation test: a member
    that existed before the migration can still reach everything they could before.
- **Gateway**: no test infra exists. The branch-list change is verified through the API's
  tests against the existing `httptest` stub gateway plus a manual end-to-end check; a real
  gateway test project is **ask first**.
- **Console**: `pnpm build` + `pnpm lint` as the gate (a test runner is ask first).
- **Manual per phase**: sign in as a scoped member in a second browser profile against a
  real synced tenant; confirm nav, data, and a hand-crafted 403/404.

## Boundaries

- **Always:** resolve permissions and allowlist from validated server-side state, never
  client input (same rule as `db_name`); 404 not 403 for out-of-allowlist rows; deny
  unmapped tenant-scoped routes; run each repo's gate before committing; keep Arabic RTL
  copy consistent.
- **Ask first:** any `AribONE.Data` schema change (none expected — this is control-plane +
  gateway query shape only); new dependencies; new Mongo collections; gateway test infra;
  making `members.manage` delegable (D2).
- **Never:** let a role grant `members.manage` or billing; let the owner row be deleted,
  scoped, or edited; let a branch-scoped member perform a Tier-A or company-wide write
  (D5c); weaken `db_name`/`BranchId` token rules; break existing flows (login, setup
  wizard, /sync, updates proxy); ship a route without a permission-table entry.

## Phases

Vertical slices, matching `spec-console.md`'s convention. Each ends green and usable.

| # | Phase | Repos | Ships |
|---|---|---|---|
| 1 | Permission core | api | `internal/perm`, `TenantRole` on the tenant doc, backfill, `MemberRole`→`MemberByAccount`, middleware + route table + context stash, coverage test. **No UI, no behaviour change** — seeded roles reproduce today's access exactly. |
| 2 | Role management | api, console | Role CRUD + `me` in the bundle; Settings roles tab with the permission grid; `PATCH /members/{id}`; nav + route guards + `can()` gating; D10 composition filtering. |
| 3 | Branch scoping | gateway, api, console | Gateway branch list; API resolution and injection; row-level 404s; write-target checks; **D5c unscoped-write refusal**; D5d control-plane routes; SSE filter; branch picker; scoped banner. |
| 4 | Invitations | api, console | `SendInvite`, `AcceptedAt`, pending badge, role + branches chosen at invite time. |

Sequencing notes:

- Phase 1 lands invisibly on purpose, so the enforcement layer is verified against real
  traffic before any UI depends on it.
- **Phase 3 starts with a no-op gateway commit** — signatures widened to a list, empty
  preserving today's behaviour, nothing calling it differently. It builds and behaves
  identically, so the mechanical 12-signature change is verified in isolation before the
  API sends anything new. **[r2]**
- **Deploy gateway before API in phase 3.** An old gateway binds only the *first*
  `branch_id` occurrence, so a member scoped to two branches would silently see one. That
  under-fetches rather than leaks, but it is still wrong. **[r2]**

## Success criteria

- The owner can create a role, tick per-section permissions, assign it to a member with a
  branch allowlist, and change it later — without revoking and re-inviting.
- A member with no `X.view` has no `X` nav item, is redirected from `/X` by URL, gets 403 on
  a hand-crafted request, and sees no `X` data through Overview, the bell, or Ctrl+K.
- A member scoped to branches ٢ and ٧ sees exactly those branches in every aggregate, KPI,
  report, and export; a branch-٣ row returns 404; an order create or transfer targeting
  branch ٣ returns 403; **a price change returns 403 because it would reach all branches**.
- The route-coverage test fails when a new tenant-scoped route ships unguarded, and the
  middleware denies it even if the test is skipped.
- After deploy, every existing member and owner has exactly the access they had before.
- An invited person receives an email and shows as «بانتظار الانضمام» until first sign-in.
- `make test`, `pnpm build`, `pnpm lint`, `dotnet build` green at every phase boundary.

## Open questions

1. **Delegated administration.** D2 makes `members.manage` owner-only. A second person who
   can invite would need a privilege-escalation rule (a role may only grant what it holds).
   Confirm owner-only is acceptable for v1.
2. **Conflicts have no branch dimension. [RESOLVED, checkpoint 13c, 2026-08-27]** `ConflictLog`
   carries no branch column, so a scoped member with `conflicts.view` sees conflicts from
   branches they cannot otherwise see. **Decision: leave unfiltered for v1** — confirmed, no
   code change. Revisit only if `ConflictLog` gains a branch column (central-only addition,
   not a synced-schema change, no `SchemaVersion` bump).
3. **Export under a `view`-only role.** `customers.view` currently permits export. If
   bulk extraction should be privileged it needs its own code. Confirm.
4. **`POST /sync-token` [r2]. [DEFERRED, checkpoint 13c, 2026-08-27 — explicitly out of
   scope for Group C]** It is consumed by the *desktop app* using an account session, not
   by the console. Gating it on `branches.manage` could break desktop activation for member
   accounts. Interim rule: any member, but the device's branch must be in their allowlist.
   Still needs confirmation from the desktop activation flow before it can be closed — not
   blocking Group C's completion, since `IssueSyncToken` deliberately left it unscoped
   (`api/internal/tenant/service.go:527`) rather than guessing. Track as a follow-up task
   once the desktop activation flow can actually be exercised.
5. **No audit trail.** Ownership transfer and an audit log were both offered and not
   selected. Residual risk: role and permission changes are security-relevant and leave no
   record, and if the founder leaves, ownership still requires a manual Mongo edit.
