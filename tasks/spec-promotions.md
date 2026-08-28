# Spec: Promotions & automatic discounts

Company- and branch-scoped promotion rules, authored in the HQ console, replicated to every
branch as master data, and **applied automatically by the desktop POS at billing time**,
with a per-application audit trail that keeps item-level and bill-level promotional money
separable from the cashier's own manual discounts.

Spans four repos: `AribONE.Data` (schema + sync scope), `sync-gateway` (HQ CRUD),
`platform` (API + console), `desktop` (the billing engine). Extends `spec-console.md` and
inherits `spec-console-rbac.md`'s permission and branch-scoping model.

## State of the world (read before touching anything)

**Discount *capture* already exists end-to-end. Discount *automation* does not.**

`Invoice` (`AribONE.Data/Models/Entities/Invoice.cs:35-53`) already carries the item/bill
split this feature's requirement 7 asks for:

```csharp
public decimal ItemDiscount { get; set; }    // Σ of the lines' own discounts
public Guid ItemDiscountId { get; set; }     // GL posting account
public decimal BillDiscount { get; set; }    // whole-bill reduction, resolved to money
public Guid BillDiscountId { get; set; }
public decimal TotalDiscount { get; set; }   // ItemDiscount + BillDiscount
```

and `InvoiceLine` carries `Discount` + `DiscountPercentage` (`InvoiceLine.cs:37-38`).
`DiscountType { Percentage, Fixed }` exists (`desktop/Models/DiscountType.cs`).

What drives those columns today is **entirely manual**:

- The cashier types a number. `NewSaleViewModel.EffectiveBillDiscount(itemsTotal)`
  (`desktop/ViewModels/Bills/NewSaleViewModel.cs:1441`) is the single place the raw entry
  is resolved to money — its own doc comment says every consumer must go through it (D6).
- `CalcBillTotals()` (`:1458`) is the one recompute choke point; every mutation path funnels
  into it. `TouchSaleViewModel : NewSaleViewModel` — "all bill math, payment legs, and the
  save path are inherited untouched" — so **one insertion point covers both POS layouts**.
- Gated by `AppPermission.BillDiscount` / `ProductDiscount` (`desktop/Models/AppPermission.cs:41-42`).
- Posted to the GL under `PostingRole.CashDiscountOut` (`NewSaleViewModel.cs:719-720, 1222-1261`).
- Line math convention: `DiscountPercentage > 0` means "percentage", otherwise `Discount` is
  fixed money. Totals use the plain-rounded percentage value while the *stored* `Discount`
  display value is separately quarter-rounded (`RoundToNearestQuarter`) — deliberately
  decoupled, see `RecalcLineForQty`'s comment (`:1571`).

**Nothing named promotion/promo exists in any of the four repos.** Verified by grep across
`desktop/{ViewModels,Services,Models}`, `AribONE.Data/Models`, `platform/{api,console}`,
`sync-gateway/*.cs`. This is greenfield.

Also true today, and load-bearing below:

- `SyncScope.SchemaVersion` is **16**. Adding tables changes the scope shape, which the v15
  comment documents the hard way: a plain re-provision *returns success* while silently
  leaving the scope at the old table count, with no `_tracking` tables and therefore no sync
  at all. Any table addition needs `overwrite: true` and is a fleet-wide flag day (D11:
  exact version equality, stale branches get HTTP 426).
- Declaration **order inside `BranchTables` is load-bearing for FK correctness**, not just
  documentation — an upload batch is applied table-by-table in array order. v15's
  `Couriers`-after-`Orders` ordering wedged sync permanently on the first order dispatched
  with a new courier (reproduced 2026-08-23; fixed by reordering, not a schema change).
- **Every SQL Server migration needs a matching Postgres migration** in the sibling
  `AribONE.Data.Migrations.Postgres` project — separate model snapshot, same `AribContext`,
  EF never regenerates it for you (`desktop/CLAUDE.md`).
- **There are no unit test projects in the desktop repo** (`AribONE.sln` holds exactly two
  projects: `AribONE` and `AribONE.Data`).
- Console RBAC ships `perm.All` with 15 codes and the rule "`Can` is exact set membership,
  plus `X.manage` implies `X.view`" (`api/internal/perm/perm.go`), mirrored client-side in
  `console/src/lib/perm.ts`. D5c: **an operation carrying no branch identity cannot be
  authorized by a branch allowlist**, so it requires an unscoped member.

## Objective

Let HQ define a discount rule once and have every eligible bill take it automatically,
without a cashier touching anything — and let HQ afterwards answer "what did this promotion
cost us, and where?"

**Users:** the tenant owner / marketing manager authoring promotions in the console
(primary); the branch cashier, who should notice promotions only as a correct total and a
labelled line (secondary, passive).

**Success looks like:** an owner creates "٪١٥ على المشروبات" scoped to branch ٢ for the
month, a cashier at branch ٢ rings up a juice the next morning and the 15% is already off
with the promotion named on the line, a cashier at branch ٣ rings up the same juice at full
price, and the Promotions report shows exactly what branch ٢ gave away, split into item-level
and bill-level money and separable from the discounts cashiers typed by hand.

## Decisions

### D1 — Three new tables: `Promotions` + `PromotionTargets` (master tier), `PromotionApplications` (branch tier)

`Promotions` and `PromotionTargets` join **Tier A** (`MasterTables`) — replicated in full to
every branch, like `Products` and `Roles`. `PromotionApplications` joins **Tier B**
(`BranchTables`) with its own `BranchId` column and an `OwnColumnFilters` entry, like every
other branch document.

Consequence, accepted deliberately: **every branch receives every branch's promotion rows**,
because the master tier has no filter. A branch-scoped promotion is filtered *at evaluation
time* by the POS (D5), not at sync time. This matches the existing posture — `Products`,
`Users`, `Accounts`, and `Roles` all replicate whole — the row count is trivial, and the
alternative (moving `Promotions` to Tier B) would make a company-wide promotion
unrepresentable, since a Tier-B row must belong to exactly one branch.

### D2 — Scope is one nullable `BranchId`: null = company-wide

`Promotion.BranchId is null` → applies at every branch. Non-null → applies only there. No
join table, no "applies to branches ٢ and ٧" multi-select in v1: the requirement is
explicitly two-level ("company-level apply across all branches; branch-level apply only to
the selected branch"), and a nullable column expresses exactly that. If multi-branch
promotions are wanted later, `PromotionTargets` (D3) already has the shape to absorb them
without touching `Promotions`.

### D3 — One polymorphic `PromotionTargets` table, no FK

Item-level targeting needs products **and** groups (user decision). Rather than two junction
tables, one table with a discriminator:

```csharp
public class PromotionTarget
{
    public Guid Id { get; set; }
    public Guid PromotionId { get; set; }
    public Promotion Promotion { get; set; } = null!;
    public PromotionTargetKind Kind { get; set; }  // Product = 0, Group = 1
    public Guid RefId { get; set; }                // ProductId or GroupId — polymorphic, no FK
}
```

`RefId` carries no FK by construction (it points at two different tables). This is the same
posture `Invoice.OriginalInvoiceId` and `RegNum` already take: "a correlation id, not an
enforced FK".

**A `Scope = AllProducts` promotion has zero target rows.** Storewide is the absence of
targets, not a magic row — so an accidental empty target list can never silently become
"everything". `Promotion.Scope` (D4) is the explicit discriminator; validation refuses
`Products`/`Groups` with an empty list.

**Group targeting cascades to every descendant group** (user decision, 2026-08-27). A
promotion on "مشروبات" covers "عصائر" and "عصائر طازجة" beneath it, to unlimited depth.

The per-keystroke cost this would imply is avoided by expanding **once, at cache load**, not
per line: when the sale screen opens and the promotion set is read (D12), each `Kind.Group`
target is expanded to *itself plus all transitive descendants* and the union is stored on the
`PromotionCandidate` as a flat `HashSet<Guid>`. `Targets(productId, groupId)` therefore stays
a single set lookup on the hot path — identical cost to the non-recursive design.

Storage stores the **chosen group only**, never the expansion. This is what makes a group
added under "مشروبات" tomorrow automatically covered, which is the whole point of resolving
at billing time.

Roots are `ParentId == Guid.Empty` (`ProductGroupCommandService.IsRoot:60`); `Groups` is TPH
with `ProductGroup` as the subtype, so the walk reads `db.Groups.OfType<ProductGroup>()`. The
expansion **must be cycle-safe** — a malformed `ParentId` chain is a corrupt-data possibility,
not a theoretical one, and an unguarded walk would hang the sale screen: build the child index
once, then breadth-first with a visited set.

The console shows the effective reach ("و ٤ مجموعات فرعية") next to a selected group, so the
author sees what they are actually discounting.

### D4 — `Promotions` shape

```csharp
public class Promotion
{
    public Guid Id { get; set; }
    [MaxLength(100)] public required string Name { get; set; }   // shown on the line and the receipt
    public PromotionLevel Level { get; set; }        // Item = 0, Bill = 1
    public PromotionScope Scope { get; set; }        // AllProducts = 0, Products = 1, Groups = 2
    public DiscountType DiscountType { get; set; }   // existing enum: Percentage, Fixed
    public decimal Value { get; set; }               // percent number (15) or money (5.00)
    public Guid? BranchId { get; set; }              // null = company-wide (D2)
    public DateTime StartsOn { get; set; }           // inclusive, date-only semantics (D8)
    public DateTime EndsOn { get; set; }             // inclusive
    public bool IsActive { get; set; }               // manual on/off, independent of the dates
    public decimal? MinQty { get; set; }             // Item level only; null = no threshold
    public decimal? MinBillTotal { get; set; }       // Bill level only; null = no threshold
    public DateTime CreatedAt { get; set; }
    public bool IsDeleted { get; set; }              // soft delete (D9)
    public ICollection<PromotionTarget> Targets { get; set; } = null!;
}
```

`Scope`, `MinQty`, and the target list are meaningless at `Level.Bill`; `MinBillTotal` is
meaningless at `Level.Item`. Validated server-side at create/update rather than modelled as
two tables — one table keeps "list every promotion" a single query and keeps the audit
snapshot (D6) uniform.

`Value` follows the existing convention exactly: a **raw percentage number** when
`DiscountType.Percentage`, money when `Fixed`. Both are `decimal(18,2)` by the money
convention — add no `[Precision]` attribute, the pre-convention default outranks data
annotations (the v16 bug).

### D5 — Eligibility and "best single discount wins"

A promotion is **eligible** for a bill at branch `B` on date `D` when all hold:

1. `!IsDeleted && IsActive`
2. `BranchId is null || BranchId == B`
3. `StartsOn <= D && D <= EndsOn`

Then, per level:

**Item level.** For each cart line, candidates additionally require
`Scope == AllProducts || (Scope == Products && RefId ∋ line.ProductId) || (Scope == Groups
&& RefId ∋ line.Product.GroupId)` and `MinQty is null || line.Qty >= MinQty`. Each candidate
is **resolved to money** against the line's undiscounted total, then the **largest money
amount wins**. Percentage and fixed compete on resolved money, never on raw `Value`.

**Bill level.** Candidates require `MinBillTotal is null || itemsTotal >= MinBillTotal`,
where `itemsTotal` is the sum of line totals **after** item promotions have been applied.
Resolved to money against that same `itemsTotal`; largest wins.

The two levels are independent: a bill can take an item promotion on some lines *and* a bill
promotion on the remainder. That is the point of tracking them separately.

**Tie-break, in order:** branch-level beats company-level (the more specific rule is the more
deliberate one) → later `CreatedAt` wins (the newer campaign) → lower `Id` wins. Three levels
so the outcome is fully deterministic and a re-evaluation of an unchanged cart can never flip.

**Never stacks.** Exactly one item promotion per line and one bill promotion per bill (user
decision). Compounding percentages are unauditable and can walk a line to zero.

**Clamping.** A resolved discount is clamped to the amount it reduces — a fixed 20 promotion
on a 12 line takes 12, not 20, and never produces a negative total. Mirrors the existing
manual guard `if (discountAfter >= totalAfter && totalAfter > 0)` in
`TouchSaleViewModel.ApplyLineDiscount` (`:226`).

### D6 — `PromotionApplications`: append-only, snapshotted, no FK to `Promotions`

```csharp
public class PromotionApplication
{
    public Guid Id { get; set; }
    public Guid InvoiceId { get; set; }              // FK — same tier
    public Invoice Invoice { get; set; } = null!;
    public Guid? InvoiceLineId { get; set; }         // null ⇔ Level.Bill
    public Guid PromotionId { get; set; }            // correlation id, NOT an FK
    [MaxLength(100)] public required string PromotionName { get; set; }  // snapshot
    public PromotionLevel Level { get; set; }        // snapshot
    public DiscountType DiscountType { get; set; }   // snapshot
    public decimal Value { get; set; }               // snapshot of the rate/amount at apply time
    public decimal Amount { get; set; }              // resolved money actually taken off
    public Guid BranchId { get; set; }
    public DateTime CreatedAt { get; set; }
}
```

Three things make this table do its job:

**Snapshotted.** `PromotionName`/`Level`/`DiscountType`/`Value` are frozen at apply time.
Editing a promotion from 15% to 20% next week must not rewrite what last week's bills say
they gave. Same "frozen forever after finalize" posture as `Invoice.PreviousBalance`.

**No FK to `Promotions`.** Deliberate, for two reasons: an audit row must survive its
promotion being hard-deleted, and it is a **cross-tier** reference (branch row → master row),
which is exactly the FK-ordering shape that wedged sync in v15. A correlation id sidesteps
both. `InvoiceId` *is* a real FK — same tier, and `PromotionApplications` is declared after
`Invoices`/`InvoiceLines` in `BranchTables` so the apply order is right.

**Append-only.** Never updated, never deleted — so ServerWins has nothing to resolve, the
same reasoning the v14 comment gives for `DocumentAuditEntries`. Editing a saved bill
replaces its rows by inserting a new set against the new invoice, never by mutating.

**`Amount` records the *stored* discount, not the totals-side one** (found while implementing
T143, 2026-08-27). The sale screen deliberately decouples two numbers for a percentage
discount: the totals subtract the plain-rounded figure, while the value written to
`SaleCartItem.Discount` — and therefore to `InvoiceLine.Discount`, and therefore summed into
`Invoice.ItemDiscount` — is separately **quarter-rounded** (`RoundToNearestQuarter`). See
`NewSaleViewModel.RecalcLineForQty:1571`, which spells the decoupling out.

Since the invariant below reconciles against the invoice's own discount columns, and those
columns hold the quarter-rounded value, `PromotionApplication.Amount` must hold it too.
Recording the totals-side figure instead would leave the invariant off by the rounding
difference on **every percentage promotion** — a slow, plausible-looking drift rather than an
obvious break. `PromotionMatch` therefore carries both numbers explicitly (`Amount` for
totals, `LineDiscountValue` for storage and audit) rather than leaving callers to rediscover
which is which.

**The reconciliation invariant** (a test asserts it, D13):

```
Σ PromotionApplications.Amount WHERE Level=Item AND InvoiceId=X  ≤  Invoice[X].ItemDiscount
Σ PromotionApplications.Amount WHERE Level=Bill AND InvoiceId=X  ≤  Invoice[X].BillDiscount
```

Equality when every discount on the bill was promotional; the shortfall is exactly the money
the cashier typed by hand. **That difference is the answer to requirement 7** — promotional
vs. manual, item vs. bill, four separable numbers, with no new columns on `Invoice`.

### D7 — Promotional money flows through the *existing* columns; no new discount columns

An item promotion writes `SaleCartItem.Discount` / `DiscountPercentage` exactly as the manual
path does → `InvoiceLine.Discount` → summed into `Invoice.ItemDiscount`. A bill promotion
resolves into `Invoice.BillDiscount`.

So `CalcBillTotals`, the totals, the payment legs, the receipt, and **the entire GL posting
path (`CashDiscountOut`) are untouched**. Promotions add no new money concept — they
automate a number the system already knows how to carry. `PromotionApplications` is a pure
audit overlay on top.

### D8 — Dates are date-only, evaluated in branch-local time

`StartsOn`/`EndsOn` are both **inclusive** and compared against the branch's local date, not
UTC — a promotion "ending 31 Aug" must last through the 31st at the till. This follows the
`/hq/branch-snapshot` precedent (`CreatedAt` day-scope in branch-local time; deploy the
gateway in the tenant region's TZ).

**Serialization rule, non-negotiable:** every outbound `DateTime` from a gateway HQ endpoint
must be `SpecifyKind`-ed to UTC before serializing. EF `DateTime`s with `Kind=Unspecified`
serialize *without* a timezone suffix and Go's strict RFC3339 decode rejects the whole
payload — one such value silently zeroed every KPI at checkpoint 2 (fixed as sync-gateway
`12bc3ae`). This applies to `StartsOn`/`EndsOn`/`CreatedAt` on every promotion response.

**Amended 2026-08-27, while implementing T130.** The paragraph above lumped all three fields
under one rule, and they do not belong under one rule. There are two distinct operations and
only one of them is right for a date:

- **`SpecifyKind(v, Utc)` — a stamp.** Relabels the Kind, changes no digits.
- **`ToUtc(v)` (the gateway's existing helper) — a conversion.** Shifts the value by the
  local offset.

`CreatedAt` is a real instant stamped in branch-local time, so it takes the **conversion**,
exactly like `LastPurchaseAt` and every other instant on an HQ endpoint. `StartsOn`/`EndsOn`
are **not instants** — they are calendar dates stored as midnight. Converting them would move
`2026-09-01T00:00` local in a UTC+3 region to `2026-08-31T21:00Z`, and the console would
render a campaign as starting a day early: a silent off-by-one that appears only east of
Greenwich and only for whole-day values.

So the two date fields are passed through **raw**. They still satisfy the rule's actual
requirement, because `UtcDateTimeConverter` (`sync-gateway/Program.cs:74`, registered globally
via `ConfigureHttpJsonOptions`) already stamps *every* outbound `DateTime` with
`SpecifyKind(value, Utc)`. A raw midnight goes out as `2026-09-01T00:00:00Z` — suffix present,
calendar date intact. The rule was satisfied by infrastructure the whole time; what needed
stating was which fields must not *additionally* be converted.

### D9 — Delete is soft; the console calls it "إيقاف"

`DELETE /hq/promotions/{id}` sets `IsDeleted = true`. A hard delete would sync a row removal
to every branch and orphan nothing useful — the audit rows already hold their own snapshot —
but it would also make "why did this bill get a discount from a promotion that doesn't
exist?" unanswerable when reconciling. Soft delete keeps the row joinable forever.

`IsActive` and `IsDeleted` are different things and both exist on purpose: `IsActive=false`
is a pause the owner expects to undo (and the console shows it), `IsDeleted=true` is gone
from the UI.

### D10 — Promotions are *not* gated by the cashier's discount permission

A cashier without `AppPermission.BillDiscount` or `ProductDiscount` still gets promotions
applied. Those permissions gate *the cashier's authority to invent a discount*; a promotion
is HQ policy the cashier has no say in. Gating it would mean the same basket costs different
amounts depending on who is at the till, which is the opposite of the feature.

The cashier **cannot** edit or remove an applied promotion — the line shows the promotion's
name and the amount, read-only. **Decided, not deferred** (user, 2026-08-27): there is no
override permission and no per-bill escape hatch. A promotion is HQ policy; if it is wrong,
HQ pauses it (D9). This also keeps D6's reconciliation invariant exact — an overridable
promotion would put manual and promotional money back in the same column.

### D11 — Manual beats automatic, per line and per bill

If the cashier has manually set a discount on a line, the engine **leaves that line alone**
and records no application for it. Same rule for the bill-level discount. A cart line
therefore carries a `DiscountIsManual` flag (cart-only state, not persisted), set by the
manual entry paths (`ApplyLineDiscount`, `AddProductToSaleCartViewModel.Sure`) and cleared
when the line's discount is cleared.

Two reasons, both decisive:

1. The cashier is the human holding the context (a damaged item, a manager's say-so).
   Silently overwriting a number they just typed is a worse failure than skipping a promotion.
2. It keeps `InvoiceLine.Discount` **either** manual **or** promotional, never a mixture — which
   is precisely what makes D6's reconciliation invariant exact rather than approximate.

### D12 — The engine runs inside `CalcBillTotals()`, guarded against re-entrancy

`CalcBillTotals()` is already the single recompute choke point every mutation path funnels
into (add line, remove line, qty change, tier change, customer change, bill-discount entry).
Evaluating promotions as its **first step** means every existing path picks the feature up for
free, with no edits scattered across a dozen call sites, and `TouchSaleViewModel` inherits it
untouched.

Requirements on the engine, because it runs on every keystroke:

- **Idempotent.** Recompute each line's promotional discount from `PriceBeforeTax * Qty`
  every time, never from the already-discounted total. Running it twice must equal running it once.
- **Pure and DB-free.** `PromotionEngine` is a static class over an in-memory candidate list
  and the cart. Zero `AribContext` access — the promotion set is loaded once when the sale
  screen opens (alongside the price tiers) and cached in the ViewModel. This is also what
  makes it testable (D13).
- **Re-entrancy guard.** A `_applyingPromotions` bool, same shape as the existing recompute
  guards, so a line mutation inside the engine cannot recurse.

### D13 — Add a test project for the engine, and only the engine

The desktop repo has no test projects, and "best single wins" arithmetic with percentage/fixed
resolution, clamping, thresholds, tie-breaks, and quarter-rounding is exactly the logic that
must not be verified by clicking. D12 makes `PromotionEngine` a pure static class specifically
so a minimal xUnit project (`AribONE.Tests`, engine only, no Avalonia, no EF) can cover it.

**This adds a project to `AribONE.sln` and an xUnit dependency — "ask first" territory
(Boundaries). Confirm before Phase 4.** If refused, the engine still ships as a pure class and
the checkpoint 4 e2e matrix carries the whole verification burden.

### D14 — RBAC: `promotions.view` / `promotions.manage`, and company-scope is an unscoped operation

Two new codes appended to `perm.All` and to `console/src/lib/perm.ts`'s `PERM`, plus a
`managePairs` entry.

D5c applies with a twist this feature makes sharp:

| Operation | Rule |
|---|---|
| `GET /hq/promotions`, `GET /hq/promotions/{id}` | `promotions.view`; a scoped member sees company-wide promotions **and** those for branches in their allowlist |
| Create/update/delete a promotion with `branch_id` set | `promotions.manage` + that branch in the allowlist, else 403 |
| Create/update/delete a promotion with `branch_id` **null** (company-wide) | `promotions.manage` **+ unscoped member** — it is a Tier-A write landing at every branch, exactly like `POST /hq/catalog/products` |
| Changing an existing promotion's `branch_id` | both the old and new branch must be in the allowlist — same reasoning as order transfer checking the destination |
| `GET /hq/promotions/{id}` for an out-of-allowlist branch promotion | **404, not 403** — a scoped member must not probe which promotions exist elsewhere |

The console hides the "الشركة كلها" scope option from branch-scoped members via `canUnscoped`
rather than letting the 403 be the first they hear of it.

### D17 — The receipt shows promotions: bill-level always, item-level behind a preference

User decision, 2026-08-27 (resolving what was OQ4). Two different rules on purpose:

| Level | Rule |
|---|---|
| **Bill** | **Mandatory whenever applied.** If a bill-level promotion reduced the total, the receipt names it and shows the amount. Not toggleable. |
| **Item** | **Optional**, governed by a new `Preference` toggle (default **on**). When off, discounted lines print their reduced price with no promotion column and no per-line saving. |

Why the asymmetry is right: a bill-level promotion changes the number the customer is asked
to pay, so hiding it makes the receipt fail to explain its own total — that is a
reconciliation problem, not a layout preference. A per-line promotion column, by contrast, is
real estate on a narrow thermal roll, and a shop selling twenty-line baskets may reasonably
not want it.

Implementation surfaces (all in `desktop`):

- **`Preference.ShowItemPromotionsOnReceipt`** — a `bool` on the JSON-persisted singleton,
  following the `PrintGoodsOutReceipt` pattern (`ViewModels/Preference.cs:783`, plain
  `SetProperty`), surfaced in `Views/Preferences/PrintingSettingsView.axaml`.
- **`InvoiceDoc`** (`Models/InvoiceDoc.cs`) — gains `BillPromotionName`, `BillPromotionAmount`,
  and a `ShowItemPromotions` gate. The gate follows the **exact existing precedent** of
  `ShowCustomerBalance`: a display bool on the doc, set by the caller, with the template band
  bound to it. Note `InvoiceDoc.Discount` today is `bill.BillDiscount` **only** (`:76`) — the
  receipt has never shown item discounts at all, so the item column is genuinely new output,
  not a re-label.
- **`Templates/Rtl/Receipt.frx`** — FastReport XML: a promotion band under the totals for the
  bill level, and a per-line promotion column in the Items band gated on `ShowItemPromotions`.

**Scoped to `Receipt.frx` only.** `Order.frx`, `WarehouseGoodsOut.frx`, and
`PaymentReceipt.frx` are out of scope — an order confirmation is printed before billing, so
no promotion has been applied yet.

### D15 — Out of scope for v1

Not because they are unreasonable, but because each is a separate rule engine:

- **Buy-X-get-Y / free items** — changes the cart's *line set*, not a line's price. Different feature.
- **Coupon codes / customer-specific promotions** — no code entry, no `PartnerGroup` targeting.
- **Time-of-day / day-of-week windows** (happy hour) — the date range is date-only (D8).
- **Purchase-side promotions** — `NewPurchaseViewModel` is untouched; promotions are sales-only.
- **Usage caps** ("first 100 customers") — needs a cross-branch counter, which the eventually-consistent
  sync model cannot provide without a new authority.

Returns are explicitly **excluded, not deferred** — see D16.

### D16 — Returns and reservations never re-evaluate promotions

`SaleCartItemFactory.FromSaleLine` already copies `PriceBeforeTax`/`DiscountPercentage`
verbatim from the original line, with the comment "a price-list change after the sale must
not alter the refund". A promotion change is the same class of hazard, and worse: a refund
computed at today's promotion would hand back money the customer never paid.

**Rule:** the engine skips any line with `SourceSaleLineId is not null`, and skips entirely
when the invoice type is not `Sale`. This is a structural guard in the engine, not just an
ordering convention — return-mode lines are reachable from several paths.

A **reservation converting to a sale** *does* evaluate promotions, at conversion time: that is
the billing moment, and `Reservation` copies the invoice's discount fields
(`Reservation.cs:29-43`) rather than owning them.

An **HQ order** becoming a bill at the branch **also evaluates normally** — `FromOrder` lines
take promotions like any other sale line (user decision, 2026-08-27, resolving what was OQ6).

This is a **deliberate divergence** from the neighbouring manual rule, and the engine needs a
comment saying so, or someone will later "fix" it: `AddProductToSaleCartViewModel` sets
`CanMakeDiscount = !FromOrder && …` (`:100`), i.e. a cashier may *not* hand-discount an order
line, presumably because the customer was quoted a price when the order was placed. Promotions
are exempt from that reasoning for the same reason they are exempt from D10's permission gate:
a promotion is not the cashier inventing a price, it is HQ policy that would have applied to
the same basket walked in off the street. The delivery customer gets the promotion.

## Data model

### `AribONE.Data` — new entities

`Models/Entities/Promotion.cs`, `PromotionTarget.cs`, `PromotionApplication.cs`; enums
`Models/PromotionLevel.cs` (`Item = 0, Bill = 1`), `Models/PromotionScope.cs`
(`AllProducts = 0, Products = 1, Groups = 2`), `Models/PromotionTargetKind.cs`
(`Product = 0, Group = 1`). `DiscountType` is reused as-is — but note it currently lives in
the **desktop** repo (`desktop/Models/DiscountType.cs`) and must **move to `AribONE.Data`**
(`AribONE.Models`) so the gateway and the entity can both reference it. Namespace-only move;
update the desktop `using`s.

Three `DbSet`s on `AribContext`. Indexes:

- `Promotions (IsDeleted, IsActive, BranchId, StartsOn, EndsOn)` — the eligibility predicate.
- `PromotionTargets (PromotionId)` and `(Kind, RefId)`.
- `PromotionApplications (InvoiceId)` and `(PromotionId, CreatedAt)` — reporting.

Money columns get **no** `[Precision]` attribute (convention gives 18,2). `MinQty` is a
quantity → explicitly `(18,3)` in `OnModelCreating`, per the repo's stated rule.

### `SyncScope` → v17

```
MasterTables:  … "FiscalYears", "Promotions", "PromotionTargets",
BranchTables:  … "DocumentAuditEntries", "PromotionApplications",
OwnColumnFilters: … ("PromotionApplications", "BranchId"),
```

`PromotionTargets` **after** `Promotions` (FK), and `PromotionApplications` **after**
`Invoices`/`InvoiceLines` (FK) — the v15 ordering lesson, applied deliberately rather than
discovered in production. Master tables provision before branch tables, so the cross-tier
reference is safe even before D6 removes the FK.

`SchemaVersion` **16 → 17**, with a doc-comment entry in the same style as v10–v16 stating:
three new tables ⇒ **scope shape changed** ⇒ the rollout must reprovision every tenant with
`overwrite: true`, and stale branches get HTTP 426 until updated. **This is a fleet flag day.**

### Migrations

One SQL Server migration (`AddPromotions`) **and** one matching Postgres migration in
`AribONE.Data.Migrations.Postgres`, in the same change — never later. Verify with
`dotnet ef migrations has-pending-model-changes` printing "No changes have been made…".

## API surface

### Gateway (`sync-gateway/HqApi.cs` + `Program.cs`)

All take `IReadOnlyList<Guid> branchIds` (`BranchScope.From(qs)`), consistent with the RBAC
widening. `null`-branch (company-wide) rows are always visible regardless of the list.

```
GET    /hq/promotions?status=&level=&branch_id=&page=&page_size=   → PromotionsPage
GET    /hq/promotions/{id}                                          → PromotionDetail | 404
POST   /hq/promotions                                               → CreatePromotionResult
PUT    /hq/promotions/{id}                                          → UpdatePromotionResult
DELETE /hq/promotions/{id}                                          → soft delete (D9)
GET    /hq/promotions/{id}/performance?from=&to=                    → PromotionPerformance
```

`status` ∈ `active | scheduled | expired | paused | all` — derived from
`IsActive` + the date range against today, computed **server-side** so the console and the POS
can never disagree about what "active" means.

`PromotionPerformance` aggregates `PromotionApplications`: `{ bills_count, items_count,
total_amount, by_branch[], by_day[] }`.

`CreatePromotionResult`/`UpdatePromotionResult` follow the `CreateProductResult` shape —
a status enum (`Created | InvalidBranch | InvalidTarget | InvalidValue | EmptyTargets |
Overlapping | TenantNotProvisioned`), the id, and `WrittenAt`.

### Platform API (`api/internal/hq/service_promotions.go`, `httpapi/hq_handlers.go`, `server.go`)

```
GET    /v1/tenants/{id}/hq/promotions
POST   /v1/tenants/{id}/hq/promotions
GET    /v1/tenants/{id}/hq/promotions/{promotionId}
PUT    /v1/tenants/{id}/hq/promotions/{promotionId}
DELETE /v1/tenants/{id}/hq/promotions/{promotionId}
GET    /v1/tenants/{id}/hq/promotions/{promotionId}/performance
```

Registered **after** any literal sub-path, per the existing routing note. Permission gate per
D14; the branch list is computed server-side from the member's allowlist intersected with the
query filter — **never taken from the client alone**, the same rule as `db_name`.

Validation lives API-side (fail fast, one Arabic message) *and* gateway-side (the DB is the
last line): `Value > 0`; `Percentage ⇒ Value <= 100`; `EndsOn >= StartsOn`; `Level.Item` with
`Scope != AllProducts` ⇒ at least one target; `Level.Bill` ⇒ no targets, no `MinQty`;
`Level.Item` ⇒ no `MinBillTotal`; `Name` non-empty, ≤ 100 chars.

### Error contract

Reuses the existing envelope exactly as `writeErr` emits it (`httpapi/server.go:281`) — a
**flat string**, not a nested object:

```json
{"error": "لا يمكن لعضو مقيد بفرع إنشاء عرض على مستوى الشركة"}
```

`console/src/lib/api.ts:152` reads `body.error` as a string, so a nested `{code, message}`
would break every existing consumer. Where a caller needs structure, the precedent is a
**sibling** field alongside `error` — `RoleAssignedError` adds `count`, the order-create 409
adds `shortfalls` — never a nested envelope. D14's company-scope refusal therefore returns a
plain 403 with an Arabic message; if the console needs to distinguish it programmatically,
add a sibling `reason: "unscoped"` field, not a code object.

## Console specifics

- **Nav:** `{ to: '${base}/promotions', label: 'العروض', icon: …, code: PERM.PromotionsView }`
  in `AppShell.tsx`, placed after الكتالوج (it is catalog-adjacent, not a report).
- **Pages:** `pages/console/Promotions.tsx` (list + status filter + branch filter),
  `pages/console/PromotionDetail.tsx` (detail + performance panel).
- **Components:** `components/PromotionFormDialog.tsx` (create + edit, one dialog),
  `components/PromotionTargetPicker.tsx` (product/group multi-select reusing the existing
  catalog queries).
- **Gating:** list/detail behind `PERM.PromotionsView`; write affordances behind
  `useCan(PERM.PromotionsManage)`; the "الشركة كلها" scope radio behind `canUnscoped` (D14).
  Follows D10 of the RBAC spec: tiles gate themselves, embedded row data does not.
- **RTL + Arabic numerals** throughout, matching every other console page; dates via the
  existing `date-fns` + `lib/format.ts` helpers. The status badge reuses `components/ui/badge.tsx`.
- **Query keys:** `['hq-promotions', tenantId, filters]`, invalidated on every mutation.

## Tech stack

| Repo | Stack |
|---|---|
| `AribONE.Data` | .NET 10, EF Core 10, SQL Server (branch) + Postgres (central) |
| `desktop` | .NET 10, Avalonia, CommunityToolkit.Mvvm |
| `sync-gateway` | ASP.NET Core minimal APIs, Dapper/EF over the tenant DB |
| `platform/api` | Go, chi, MongoDB |
| `platform/console` | React 19, TypeScript, Vite, TanStack Query/Table, Tailwind 4, shadcn-style UI, react-hook-form + zod |

## Commands

```bash
# AribONE.Data — schema
dotnet ef migrations add AddPromotions --project ../AribONE.Data/AribONE.Data.csproj \
  --startup-project ../AribONE.Data/AribONE.Data.csproj
cd ../AribONE.Data.Migrations.Postgres && \
  dotnet ef migrations add AddPromotions --project . --startup-project . && \
  dotnet ef migrations has-pending-model-changes --project . --startup-project .

# desktop
dotnet build AribONE.csproj
./run.sh                       # ARIB_LICENSE_API=http://127.0.0.1:8080 dotnet run
dotnet test AribONE.Tests      # only if D13 is approved

# sync-gateway
dotnet build AribSyncGateway.csproj && ./run.sh

# platform/api
make build && make test && make vet && make fmt

# platform/console
pnpm build                     # tsc -b && vite build
pnpm lint
pnpm dev
```

## Project structure

```
AribONE.Data/Models/Entities/Promotion.cs, PromotionTarget.cs, PromotionApplication.cs
AribONE.Data/Models/{PromotionLevel,PromotionScope,PromotionTargetKind,DiscountType}.cs
AribONE.Data/Sync/SyncScope.cs                     → v17
AribONE.Data/Migrations/…_AddPromotions.cs         (+ Postgres twin)

desktop/Services/Promotions/PromotionEngine.cs      → pure, DB-free, testable (D12)
desktop/Services/Promotions/PromotionCandidate.cs   → flattened rule + expanded group set (D3)
desktop/Services/Promotions/PromotionMatch.cs       → winner + its two amounts (see D6)
desktop/Services/Promotions/PromotionCacheLoader.cs → the one DB read; expands group descendants
desktop/AribONE.Tests/                              → engine fixtures (D13, approved 2026-08-27)
desktop/ViewModels/Bills/NewSaleViewModel.cs        → load cache, call engine, write audit rows
desktop/Models/SaleCartItem.cs                      → +AppliedPromotionId/Name, +DiscountIsManual
desktop/Models/InvoiceDoc.cs                        → +BillPromotion*, +ShowItemPromotions (D17)
desktop/ViewModels/Preference.cs                    → +ShowItemPromotionsOnReceipt (D17)
desktop/Views/Preferences/PrintingSettingsView.axaml → the toggle
desktop/Templates/Rtl/Receipt.frx                   → bill promo band + gated item column

sync-gateway/HqApi.cs                               → 6 methods
sync-gateway/Program.cs                             → 6 routes

platform/api/internal/perm/perm.go                  → 2 codes + managePair
platform/api/internal/hq/service_promotions.go      (+ _test.go)
platform/api/internal/httpapi/hq_handlers.go, server.go

platform/console/src/pages/console/Promotions.tsx, PromotionDetail.tsx
platform/console/src/components/PromotionFormDialog.tsx, PromotionTargetPicker.tsx
platform/console/src/lib/{perm.ts,types.ts,hooks.ts}
```

## Code style

The engine is the heart of the feature, so it is worth showing the intended shape — pure,
allocation-light, and explicit about the tie-break:

```csharp
/// <summary>
/// Picks the single best item-level promotion for one cart line (D5). Candidates are
/// pre-filtered for branch/date/active by <see cref="PromotionCandidate.EligibleAt"/>;
/// this method only decides targeting, threshold, and which of several eligible rules wins.
/// Pure: no DB, no ViewModel state, no side effects — call it as often as you like.
/// </summary>
public static PromotionMatch? BestForLine(
    IReadOnlyList<PromotionCandidate> candidates, Guid productId, Guid? groupId,
    decimal qty, decimal lineTotal)
{
    PromotionMatch? best = null;
    foreach (var c in candidates)
    {
        if (c.Level != PromotionLevel.Item) continue;
        if (!c.Targets(productId, groupId)) continue;
        if (c.MinQty is { } min && qty < min) continue;

        // Percentage and Fixed compete on resolved money, never on raw Value —
        // 10% of a 40 line loses to a flat 5, and must be seen to lose.
        var amount = Math.Min(c.Resolve(lineTotal), lineTotal);
        if (amount <= 0) continue;

        if (best is null || Beats(c, amount, best)) best = new PromotionMatch(c, amount);
    }
    return best;
}

/// <summary>D5's tie-break, in order: larger money → branch-level over company-level →
/// newer CreatedAt → lower Id. Fully deterministic, so re-evaluating an unchanged cart
/// can never flip the winner.</summary>
private static bool Beats(PromotionCandidate c, decimal amount, PromotionMatch best) =>
    (amount, c.BranchId is not null, c.CreatedAt, c.Id.CompareTo(best.Promotion.Id) < 0)
        .CompareTo((best.Amount, best.Promotion.BranchId is not null,
                    best.Promotion.CreatedAt, false)) > 0;
```

Conventions to hold to, per repo: C# doc-comments explain *why* (this codebase's comments
carry decisions, not restatements); Go handlers stay thin and push logic into `internal/hq`;
console components are function components with TanStack Query hooks in `lib/hooks.ts`, never
inline `fetch`.

## Testing strategy

| Layer | How |
|---|---|
| `PromotionEngine` | xUnit, `AribONE.Tests` — **pending D13 approval**. Table-driven over the D5 matrix: percentage vs fixed, clamping, `MinQty`/`MinBillTotal` boundaries, all three tie-break levels, company-vs-branch, expired/paused/deleted exclusion, return-line skip (D16), idempotency (apply twice = apply once). |
| `AribONE.Data` | No tests; correctness is the migration pair + `has-pending-model-changes` clean. |
| `sync-gateway` | No test project. Verified through the API's tests and the checkpoint e2e. |
| `platform/api` | `go test ./...` — `service_promotions_test.go` alongside the existing `service_test.go`/`service_orders_test.go`: validation matrix, D14's permission/scoping rules including the 404-not-403 case and the company-scope unscoped refusal. |
| `platform/console` | No test runner in `package.json`. `pnpm build` (`tsc -b`) + `pnpm lint` clean, then the human-verified checkpoint — matching this project's established practice. |
| **Reconciliation** | One integration-style assertion of D6's invariant against a saved bill carrying a promotional line discount, a manual line discount, and a bill promotion simultaneously. This is the requirement-7 proof and must not be skipped. |

Human-verified checkpoints, as with every prior phase of this console (checkpoints 0, 1, 2, 5
were all human-verified before the next phase started).

## Boundaries

**Always:**
- Add the Postgres migration in the same change as the SQL Server one.
- `SpecifyKind(DateTimeKind.Utc)` every outbound `DateTime` on a gateway HQ endpoint (D8).
- Keep new tables in the correct `BranchTables`/`MasterTables` declaration order (FK apply order).
- Compute the branch list server-side from the member's allowlist; never trust a client `branch_id`.
- Keep `PromotionEngine` pure and DB-free.
- `make test`/`make vet` clean in `api`, `pnpm build` + `pnpm lint` clean in `console`, before commit.

**Ask first:**
- **Adding `AribONE.Tests` to `AribONE.sln`** (D13) — a new project + xUnit dependency.
- **Executing the v17 flag day** — reprovisioning every tenant with `overwrite: true` and
  cutting off branches below schema 17. This is an operational event, not a code change.
- Any change to `Invoice`/`InvoiceLine` shape (this spec deliberately adds none — D7).
- Any change to the GL posting path or `PostingRole` set.

**Never:**
- Let a promotion write a negative total or a discount larger than the amount it reduces (D5).
- Overwrite a discount the cashier typed by hand (D11).
- Re-evaluate promotions on a return line (D16).
- Mutate or delete a `PromotionApplications` row (D6).
- Add a `[Precision]` attribute to a money column, or let a money column be anything but (18,2).
- Hard-delete a promotion (D9).

## Phases

Ordered so each phase is verifiable against the one before it, and the flag day happens
exactly once, at the front.

| # | Phase | Content | Gate |
|---|---|---|---|
| 0 | **Schema & sync** | Entities, enums, `DiscountType` move, `AribContext`, indexes, SQL Server + Postgres migrations, `SyncScope` v17. No behaviour. | Both migrations clean; `has-pending-model-changes` reports none |
| 1 | **Gateway CRUD** | 6 `HqApi` methods + 6 routes, branch-list aware, UTC-kind serialization | curl each route against a local tenant DB |
| 2 | **API + permissions** | `perm` codes, `service_promotions.go`, handlers, routes, D14 scoping rules, Go tests | `make test` green incl. the scoping matrix |
| 3 | **Console authoring** | Nav, list page, detail page, form dialog, target picker, RBAC gating | **Checkpoint A (human):** author a company promo and a branch promo; confirm both rows land in the central DB and sync down to a branch DB |
| 4 | **Desktop engine** | `PromotionEngine`, cache loader with descendant expansion (D3), cart integration in `CalcBillTotals`, `SaleCartItem` fields, audit rows on save, return-mode guard; test project if D13 approved | **Checkpoint B (human):** ring up the e2e matrix below |
| 5 | **Receipt** (D17) | `Preference` toggle, `InvoiceDoc` fields, `Receipt.frx` bill band + gated item column | **Checkpoint C (human):** print a bill with both promotion levels, toggle off, print again |
| 6 | **Reporting** | `/performance` endpoint + console performance panel; promotional-vs-manual split | **Checkpoint D (human):** reported numbers reconcile against the bills from checkpoint B |

**Checkpoint B e2e matrix** (the one that decides whether this feature works): item promo on a
targeted product; group promo on a product in that group; **group promo on a product in a
*grandchild* group (D3's cascade)**; storewide promo; both an item and a bill promo on one
basket; two competing promos where the smaller-percentage-but-larger-money one must win; a
branch promo not firing at another branch; an expired and a paused promo not firing;
`MinQty`/`MinBillTotal` at and just below the boundary; a manual line discount suppressing a
promo (D11); a return not re-applying (D16); **an HQ order line *doing* re-applying (D16)**;
the same basket on the touch POS (inherited path); a cashier with no discount permission still
getting the promo (D10).

## Success criteria

1. A company-scoped promotion authored in the console applies at **every** branch; a
   branch-scoped one applies at **only** that branch, verified on two real branches.
2. Both discount **levels** work: an item promotion reduces `InvoiceLine.Discount` (rolling into
   `Invoice.ItemDiscount`), a bill promotion reduces `Invoice.BillDiscount` — on the same bill.
3. Both discount **types** work, and a percentage and a fixed promotion competing for the same
   line resolve to money before comparison, with the larger money winning.
4. A promotion outside its active period, paused, or deleted **never** applies — verified at
   both boundary dates (inclusive, branch-local).
5. The cashier does nothing: no keystroke, no dialog, no permission. The correct total is
   already there and the line names the promotion.
6. **Requirement 7, precisely:** for any bill, the four numbers — promotional item discount,
   promotional bill discount, manual item discount, manual bill discount — are each
   independently recoverable from the database, and D6's reconciliation invariant holds exactly.
7. `Σ PromotionApplications.Amount` for a promotion equals what the console's performance
   panel reports for it, over the same window.
8. Editing a promotion's rate does not change any already-saved bill's audit rows.
9. A group promotion reaches products in **descendant** groups to arbitrary depth, and a group
   created under a targeted parent *after* the promotion was authored is covered with no edit
   to the promotion (D3).
10. The receipt names a bill-level promotion whenever one applied, and shows or hides the
    per-line promotion column according to `ShowItemPromotionsOnReceipt` (D17).
11. `make test` and `go vet` clean; `pnpm build` and `pnpm lint` clean; both migration
    projects report no pending model changes.

## Open questions

**Resolved 2026-08-27** (user): cashier override → **no**, decided in D10. Group targeting
cascades to descendants → **yes**, D3. Receipt shows promotions → **yes**, D17 (bill mandatory,
item preference-gated). HQ order lines take promotions → **yes**, D16.

Still open:

1. **Flag-day timing and blast radius.** v17 cuts off every branch below schema 17 (HTTP 426)
   until it updates, and needs an `overwrite: true` reprovision of every tenant. The v7 note
   records that zero production tenants existed at 2026-07-18 — **how many are there now, and
   should v17 be batched with other pending schema work rather than spent on promotions alone?**
   This is the single largest cost in the spec and it is an operational decision, not a
   technical one. **It does not block Phases 1–5**, which are all code; it blocks the rollout.
2. **Should the POS surface promotions the basket *nearly* qualified for** ("add 50 more for
   10% off")? Pure upsell, no schema impact, but it is UI work in the touch layout. Deferred,
   not rejected — `MinBillTotal` (D4) is the data it would need, and that ships in v1.
