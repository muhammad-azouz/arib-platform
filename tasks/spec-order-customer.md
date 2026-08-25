# Spec: New Order — inline customer create + address prefill

Two changes to the order workspace (`console/src/pages/console/NewOrder.tsx`),
both aimed at the same thing: stop making the operator leave the page — or
retype what the system already knows — in the middle of building an order.

1. **Create a customer without leaving the order.** Today the only create path
   is «عميل جديد» on the Customers page, which navigates away and loses the
   cart. The order page gets its own affordance inside the customer picker.
2. **Prefill the delivery address from the chosen customer's profile.**

Scope: console only. No Go API change, no sync-gateway change — every endpoint
this needs already ships (`POST /hq/customers`, `GET /hq/customers/{id}`).

## Objective

**Problem.** A call-centre operator taking an order from a first-time caller
has to abandon the half-built order, go to Customers, create the customer,
come back, and rebuild the cart. And for every delivery order — new customer
or not — they retype an address the customer record already holds.

**Build.**
- A «عميل جديد» affordance inside the order page's customer combobox that opens
  a four-field quick-add dialog (الاسم، الهاتف، العنوان، الفرع — the branch
  read-only, from the order) over the workspace, and on success selects that
  customer in the picker. The cart, branch, mode, and notes are untouched throughout.
- Delivery address auto-fills from `CustomerDetail.address` on customer
  selection, with a manual override that sticks — the same auto/manual
  contract T3b already built for the delivery fee.
- The pickup/delivery choice becomes a two-option segmented control **defaulting
  to توصيل**, replacing today's single on/off «توصيل» button.

**User.** HQ call-centre staff building orders on behalf of branches.

**Success looks like:** an order for a brand-new caller is completed without a
single navigation away from `/tenants/:id/orders/new`, and a delivery order for
a customer with a profile address needs zero typing in the address field.

### Why the delivery address is NOT redundant (decided 2026-08-24)

The user asked whether the field should be removed once the customer's address
is known. It should not:

| | `Customer.address` | `Order.contact_address` |
|---|---|---|
| Meaning | the customer's profile address | where **this** order goes |
| Lives in | Tier-B `Customers` row | the order row (`OrderDetail.address`) |
| Required | no (optional on create) | **yes** for delivery — `hq_handlers.go:1145` rejects an empty one with 400 |
| Changes | rarely, and edits are retroactive | per order; a snapshot, historical record |

Same customer orders to home on Friday and to the office on Monday. Dropping
the field would also make a customer with no profile address un-orderable for
delivery, and would rewrite past orders' delivery address whenever the profile
was edited. So: keep the field, **prefill** it, keep the override.

### Mode is the delivery signal — the address never is (decided 2026-08-24)

Raised by the user: *"I rely on address to know if this is a delivery — what if
the customer has an address but collects from the branch?"*

Checked against the code: nothing infers delivery from the address. `mode`
(`ORDER_MODE.Pickup = 1` / `Delivery = 2`) is the only signal, everywhere:

- `OrderCart.tsx:72,90` — the mode control sets it; the address input renders **only** when the mode is Delivery.
- `NewOrder.tsx` `save()` — `contact_address: isDelivery ? contactAddress.trim() : undefined`. A pickup order never carries an address, even one typed before switching back.
- `hq_handlers.go:1145` — the gateway requires `contact_address` only for `Mode == 2`.
- `OrderDetail.tsx:422` — the order is labelled from `o.mode`; `o.address` is a detail line, never the classifier.

So the customer-with-a-saved-address-who-collects case already resolves
correctly, and the prefill is safe **because** it is gated behind an explicit
mode choice. The weakness the question exposes is the *control*, not the
address: a single on/off button makes pickup a silent default that looks
identical to "not decided yet", so a forgotten click ships a delivery order as
pickup with no address and no fee.

**Fix:** a two-option segmented control — «استلام من الفرع» / «توصيل» — with
**توصيل preselected** (user decision: HQ call-centre orders are deliveries by
default; pickup is the deliberate exception). Both states are always visible and
labelled, so "pickup" is a state someone chose, not the absence of a click.

Consequences of the delivery default:
- The address + fee panel is open from the first render, so the prefill and the
  T3b fee resolution both fire as soon as a customer is picked — the common path,
  not an edge case.
- The required-address check moves out of `save()`'s toast and into
  `saveBlockedReason` («عنوان التوصيل مطلوب»), joining branch/customer/cart. On
  the default path the save button must explain itself up front rather than fail
  on click. In pickup mode the address is not required and not sent.

**Never infer the mode from the customer record.** Preselecting Delivery because
the customer happens to have a profile address is exactly the inference the user
was worried about: a saved address means "we know where they live", not "they
want it delivered". The default is a fixed constant, identical for every customer.

### Interaction contract (decided with the user 2026-08-24)

| Action | Result |
|---|---|
| Open a fresh order | Mode segmented control shows «توصيل» selected; the address + delivery-fee panel is already open. |
| Click «استلام من الفرع» | Address + fee panel collapses. Address is neither required nor sent; a typed address is kept in local state for a switch back. |
| Save in delivery mode with an empty address | Blocked before the click — `saveBlockedReason` shows «عنوان التوصيل مطلوب» and the save button is disabled. |
| Open the customer picker with **no branch selected** | Popover shows «اختر الفرع أولًا لعرض عملائه». The «عميل جديد» button is **disabled** with the same hint — a customer must be created at the order's branch, and there isn't one yet. |
| Open the picker **with** a branch selected | List is scoped to that branch (`useCustomers({ branchId })`) — user decision: the picker filters to the order's branch. |
| Search yields no match | Empty state offers «إنشاء عميل جديد» seeded with the typed text (digits → phone, otherwise → name). |
| Click «عميل جديد» | Popover closes, the quick-add dialog opens over the workspace: **الاسم\*, الهاتف\*, العنوان, الفرع**. The branch is prefilled from the order and **read-only**. |
| Dialog succeeds | Toast «تم إنشاء العميل»; dialog closes; the new customer becomes the selected customer; the address typed into the dialog prefills the delivery address. **No navigation.** |
| Dialog cancelled / fails | Nothing on the order page changes; the previously selected customer (if any) stays selected. |
| Select a customer (delivery mode) | Address field fills from their profile address, hint «عنوان العميل» under it. |
| Select a customer with **no** profile address | Field left empty, no hint, still required before save. |
| Operator edits the address | Hint disappears; the field is theirs. A later re-render never overwrites it. |
| Select a **different** customer | Auto-fill re-arms and overwrites the field — same rule as the delivery fee. |
| Switch branch while a customer from another branch is selected | Customer is **kept** (not silently dropped) but the order is **not saveable**: warning badge «العميل مسجّل في فرع آخر» beside the picker, `saveBlockedReason` = «اختر عميلًا من هذا الفرع». The picker — already scoped to the new branch — is how they replace it. |
| `Esc` while the dialog is open in zen mode | Closes the dialog only; zen mode stays. |

### Quick-add form (resolved OQ2, 2026-08-24)

A **new, order-scoped dialog** — `components/orders/QuickAddCustomerDialog.tsx`
— not an extension of `CreateCustomerDialog`. Four fields, in this order:

| Field | Required | Behavior |
|---|---|---|
| الاسم | yes | Seeded from the picker's search text when it isn't all digits. `autoFocus`. |
| الهاتف | yes | Seeded from the search text when it is digits. |
| العنوان | no | On success, seeds the order's delivery address directly (no detail fetch needed). |
| الفرع | — | **Read-only**, prefilled from the order's branch. Displayed so the operator can see where the customer is being registered, never editable. |

`CreateCustomerDialog.tsx` is **not touched** — the Customers page keeps its full
form and its navigate-to-profile behavior with zero regression risk. The two
forms stay honest with each other by both going through the one
`useCreateCustomer` / `NewCustomerInput` contract; drift can't get past `tsc`.

**Why the branch is read-only, not merely prefilled:** OQ1 is resolved as *no
cross-branch orders*. A customer created at any branch other than the order's is
instantly unusable for the order that created them — an editable field here is a
footgun with no legitimate use.

**Accepted consequence:** a quick-added customer has no group and a credit limit
of `0` (the fields aren't shown, so `group_id: undefined`, `credit_limit: 0`).
That is correct for taking an order — an order isn't a payment — and either is
editable afterwards on the customer's profile.

### Auto-fill contract (mirrors T3b exactly)

```
contextKey        = selected customer id
addressAuto       = true, re-armed whenever contextKey changes
resolvedAddress   = customerQuery.data?.data.address        (undefined until loaded)
displayedAddress  = addressAuto && resolvedAddress ? resolvedAddress : contactAddress
onChange(value)   → setContactAddress(value); setAddressAuto(false)
hint              = addressAuto && resolvedAddress ? 'عنوان العميل' : undefined
```

The resolved value is never copied into state — it is derived at render time,
the same "adjust state, don't duplicate it" shape the delivery fee already
uses in this file. `useCustomer(tenantId, customerId)` is enabled only while a
customer is selected **and** mode is Delivery — with Delivery the default this
usually fires, and only an explicit switch to pickup saves the call (see OQ4). A customer created through the dialog seeds `contactAddress`
directly from the submitted form value, so its address shows before (and
without) any detail fetch.

## Tech Stack

- React 19 + TypeScript, Vite, React Router, TanStack Query v5
- Tailwind + local `ui/*` primitives over Radix (`@radix-ui/react-dialog`)
- `react-hook-form` + `zod` v4 (`@hookform/resolvers`) — the create dialog's existing stack
- `sonner` for toasts, `@solar-icons/react` via `components/icon.tsx` (`AddIcon`, `AddressIcon` already exported)
- **No new dependency.** `package.json` must be unchanged by this work.

## Commands

```
Dev:    pnpm dev                 # from platform/console
Build:  pnpm build               # tsc -b && vite build — the type gate
Lint:   pnpm lint
Test:   (none in console today — verification is build + lint + manual)
```

API-side, unchanged but run if anything strays over the line:
`cd platform/api && go build ./... && go vet ./... && go test ./...`

## Project Structure

```
console/src/pages/console/NewOrder.tsx                    → the order workspace (owns picker + auto-fill state)
console/src/components/orders/QuickAddCustomerDialog.tsx → NEW — four-field in-order create form
console/src/components/orders/OrderCart.tsx              → mode control + address input (gains a hint prop)
console/src/components/CreateCustomerDialog.tsx          → UNTOUCHED — Customers page keeps its full form
console/src/lib/{types,api,query,hooks}.ts               → unchanged — every call already exists
```

Touched files, in full:

| File | Change |
|---|---|
| `orders/QuickAddCustomerDialog.tsx` | **new** — الاسم/الهاتف/العنوان + read-only الفرع; `zod` + `react-hook-form` like its sibling; calls `useCreateCustomer`; reports `{ id, name, phone1, address, branchId }` up through `onCreated`; never navigates |
| `NewOrder.tsx` | default mode → Delivery; address + cross-branch reasons into `saveBlockedReason`; branch-scoped picker; create affordance + dialog wiring; address auto-fill state; zen `Esc` guard; `SelectedCustomer` gains `branchId` |
| `orders/OrderCart.tsx` | mode toggle → two-option segmented control; `contactAddressHint?: string` prop rendered under the address input |

## Code Style

Follow the file's existing voice: Arabic UI strings inline, decisions explained
in comments that name the decision and why, never what the code obviously does.

```tsx
// The address auto-fill mirrors T3b's delivery-fee rule one-to-one: any manual
// edit hands the field to the operator for good, and a new customer re-arms it
// — an order's contact_address is a per-order snapshot, so the profile address
// is a starting point, never the source of truth.
const [addressAuto, setAddressAuto] = useState(true)
const [lastCustomerKey, setLastCustomerKey] = useState(customer?.id ?? '')
if ((customer?.id ?? '') !== lastCustomerKey) {
  setLastCustomerKey(customer?.id ?? '')
  setAddressAuto(true)
}

const resolvedAddress = customerQuery.data?.data.address ?? undefined
const displayedAddress = addressAuto && resolvedAddress ? resolvedAddress : contactAddress
```

`SelectedCustomer` gains `branchId`, captured from the `CustomerRow` at pick
time (the list row already carries it) — the cross-branch check costs no extra
request, and a quick-added customer's branch is the order's branch by construction.

Conventions that apply here:
- Arabic numerals through `toArabicDigits` for any number rendered to the user.
- RTL-safe classes only: `start-*`/`end-*`, `ms-*`/`me-*` — never `left-*`/`ml-*`.
- Query keys via `qk.*` in `lib/query.ts`; no inline key arrays.
- `save()` still reads `displayedAddress`, not `contactAddress` — same trap the
  delivery fee already avoids.

## Testing Strategy

The console has no test runner today (deliberate — see plan §Risks). Verification
is the type gate plus a scripted manual pass; this spec does not introduce one.

**Automated:** `pnpm build && pnpm lint` clean, with no new warnings beyond the
two pre-existing `auth.tsx` react-refresh ones.

**Manual checklist** (dev server, a tenant with ≥2 branches and synced customers):
1. Load a fresh order → «توصيل» is preselected and the address + fee panel is open.
2. Switch to «استلام من الفرع» → panel collapses; save is no longer blocked on an address; the saved request body has no `contact_address`.
3. Type an address in delivery mode → switch to pickup → switch back → the typed address is still there.
4. Delivery mode, empty address → save button disabled with «عنوان التوصيل مطلوب» (not a toast on click).
5. Pick a branch → open the picker → only that branch's customers listed.
6. No branch → picker shows the «اختر الفرع أولًا» hint and the create button is disabled.
7. Search a nonsense string → empty state offers create, seeded with the text.
8. Create from the picker → dialog shows four fields with الفرع read-only and matching the order's branch; toast, dialog closes, new customer selected, **URL unchanged**, cart intact.
9. Same flow inside zen mode: dialog paints above the overlay; `Esc` closes the dialog only; a second `Esc` exits zen.
10. Cancel the dialog → previously selected customer still selected, nothing else moved.
11. Delivery mode + customer with a profile address → field prefilled, hint shown.
12. Edit the address → hint gone; toggle pickup/delivery and back → the edit survives.
13. Switch to a different customer → address re-fills from the new profile.
14. Customer with no address → empty field, no hint, save still blocked by the required-address guard.
15. Create a customer with an address from the dialog → the address lands in the field with no extra fetch.
16. Save the order → `contact_address` in the request body is what the field showed.
17. Switch branch mid-order to one the selected customer doesn't belong to → warning badge, save blocked with «اختر عميلًا من هذا الفرع», cart survives; switching back re-enables save.
18. Pick a replacement customer from the now-rescoped picker → warning clears, save re-enables.
19. Regression: Customers page «عميل جديد» still opens the **full** form and still navigates to the new profile.

## Boundaries

**Always**
- `pnpm build && pnpm lint` before calling a task done.
- Keep the cart, branch, mode, note, and zen state alive across the whole create flow.
- RTL/Arabic-numeral audit on every new string and number.
- Leave `CreateCustomerDialog.tsx` untouched — the Customers page's full form and its navigate-to-profile behavior are out of this change's blast radius.

**Ask first**
- Any change under `platform/api/` or to a gateway contract.
- Adding a dependency, or a new dialog/form component instead of reusing the existing one.
- Changing `NewOrderInput` / the create-customer request shape.
- Changing the default mode away from توصيل.
- Making the quick-add's branch field editable, or adding fields to it.

**Never**
- Remove the `contact_address` field or send the profile address without the operator seeing it in the field.
- Navigate away from the order page during the create flow.
- Reset the cart on customer or branch change.
- Copy a resolved value into state to "keep it in sync" — derive at render.
- Infer the mode from the customer record (a saved address must never preselect توصيل).
- Let an order reach the gateway with a customer from a different branch than `branch_id`.
- Send `contact_address` on a pickup order.

## Success Criteria

1. From `/tenants/:id/orders/new`, an operator creates a new customer and saves an order for them without one navigation away and without rebuilding the cart. **Verify:** manual steps 8 + 16.
2. The picker lists only the selected branch's customers; with no branch it says so instead of showing an unscoped list. **Verify:** steps 5–6.
3. Selecting a customer in delivery mode fills the address from their profile and labels it «عنوان العميل». **Verify:** step 11.
4. A manual address edit is never overwritten by a re-render, a mode toggle, or a refetch — only by selecting a different customer. **Verify:** steps 12–13.
5. A customer with no profile address leaves the field empty and the existing required-address save guard still fires. **Verify:** step 14.
6. The whole flow works in zen mode, and `Esc` with the dialog open does not exit zen. **Verify:** step 9.
7. The Customers page's own «عميل جديد» still opens the full form and navigates to the new customer's profile — `CreateCustomerDialog.tsx` is byte-identical to before. **Verify:** step 19 + `git diff --stat`.
8. Both delivery and pickup are visible, labelled states; a fresh order opens on «توصيل»; choosing pickup sends no `contact_address` and requires none. **Verify:** steps 1–4.
9. The mode default is a constant — it never changes based on whether the selected customer has a profile address. **Verify:** step 11 with a customer who has an address and one who doesn't; «توصيل» is preselected identically in both.
10. An order can never be saved for a customer registered at another branch: the picker doesn't offer them, and a branch switch that creates the mismatch blocks the save with a stated reason instead of a gateway 400. **Verify:** steps 17–18.
11. The quick-add dialog registers the customer at the order's branch and nowhere else — the field is visible and not editable. **Verify:** step 8.
12. `pnpm build && pnpm lint` clean; `package.json` unchanged.

## Open Questions

- ~~**OQ1 — Cross-branch orders.**~~ **Resolved 2026-08-24 (user): no cross-branch
  orders.** A customer may only be ordered for at their own branch. Enforced in
  three places — the picker is branch-scoped, the quick-add's branch is read-only,
  and a branch switch that strands the selected customer blocks the save. The
  console refuses this before the request, so the gateway's own answer (which this
  repo cannot determine) never has to be relied on.
- ~~**OQ2 — Form scope.**~~ **Resolved 2026-08-24 (user): quick-add** — الاسم،
  الهاتف، العنوان، الفرع (prefilled, read-only). See §Quick-add form.
- **OQ3 — Dialog stacking in zen mode.** `DialogContent`/`DialogOverlay` are
  `z-50` and the zen container is also `fixed inset-0 z-50`; DOM order should put
  the later-mounted dialog on top. If it paints behind, the fix is to drop the
  zen container to `z-40` (it only needs to clear page content), not to bump the
  shared dialog primitive.
- **OQ4 — Detail-fetch cost.** `GET /hq/customers/{id}` also computes purchase
  stats. Gated to delivery mode — but since Delivery is now the default, that gate
  saves a call only when the operator explicitly picks up. If it proves slow on a large tenant,
  the alternative is adding `address` to the customer **list** row — but that is a
  gateway change and out of this spec's scope.
