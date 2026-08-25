# Implementation Plan: New Order — inline customer create + address prefill

Spec: `tasks/spec-order-customer.md` (2026-08-24) · Tasks: `tasks/todo.md` §Phase 12 (T96–T100)

## Overview

Three changes to the order workspace, all console-only — no Go API change, no
gateway change, no new dependency, no change to `lib/{types,api,query,hooks}.ts`:

1. The pickup/delivery toggle becomes a two-option segmented control defaulting
   to **توصيل**, and «عنوان التوصيل مطلوب» moves into `saveBlockedReason`.
2. The delivery address prefills from the selected customer's profile, with a
   manual override that sticks (T3b's auto/manual contract, reused verbatim).
3. A four-field quick-add dialog creates a customer without leaving the page,
   scoped to the order's branch — which is now also enforced on the picker and
   on save (no cross-branch orders).

Five tasks. **T96 and T97 each ship standalone value** — a mode control that
can't be silently wrong, and a picker that can't select an unusable customer —
and both land before anything depends on them. The quick-add dialog (T99) is a
new leaf file with no dependencies and can be built at any point; T100 is the
only task that needs three predecessors, and it is pure wiring.

## Architecture decisions (grounded in the code)

- **`OrderCart` keeps rendering the mode control; `NewOrder` keeps owning
  `mode`.** The `mode`/`onModeChange` prop pair already exists
  (`OrderCart.tsx:55–56`) — T96 changes the control's *shape* (one on/off
  button → two labelled options) and the initial value in `NewOrder.tsx`, not
  the ownership. `isDelivery` stays derived from `mode` in both files, so the
  address panel, the fee panel, the T3b fee query's `enabled`, and `save()`'s
  `contact_address` gate all follow the new default with no extra plumbing.

- **`saveBlockedReason` becomes the single explanation of a non-saveable
  order.** Today it covers branch/customer/cart, while a missing delivery
  address fails *after* the click with a toast in `save()`. That was tolerable
  when delivery was opt-in; with توصيل as the default it is the common path, so
  the reason joins the chain and the button disables itself. The `save()` guard
  stays as a plain early return (defense in depth) minus its toast.

- **`saveBlockedReason` and `save()` must both read `displayedAddress`, never
  `contactAddress`.** This is the one real trap in the change: after T98,
  `contactAddress` holds only what was *typed*, and a prefilled-but-untouched
  address lives in the derived value. Reading the raw state would block saving
  a perfectly valid prefilled order. The delivery fee already avoids exactly
  this trap (`displayedDeliveryFee` in `save()`); T98 makes the address match,
  and it is an explicit acceptance criterion there.

- **The cross-branch check costs no request.** `CustomerRow` already carries
  `branch_id` (`types.ts:696`), so `SelectedCustomer` gains a `branchId`
  captured at pick time. A quick-added customer's branch is the order's branch
  by construction. No `useCustomer` call is needed to enforce the rule — the
  detail query exists only for the address (T98) and stays gated to delivery.

- **The address is derived at render, never copied into state.** Same shape as
  T3b's `displayedDeliveryFee`: a `contextKey` (the customer id) re-arms the
  auto flag, `resolvedAddress` comes straight off the query result, and
  `displayedAddress` picks between them. Copying into state on query success
  would fight the operator's own edits on every refetch.

- **The quick-add dialog is a leaf component, not an extension of
  `CreateCustomerDialog`.** It receives `branchId`/`branchName` plus optional
  name/phone seeds and reports the created customer through `onCreated`; it
  holds no page state and never navigates. `CreateCustomerDialog.tsx` stays
  byte-identical, so the Customers page carries zero regression risk from this
  phase — verified with `git diff --stat`, not by inspection.

- **The branch field is read-only rather than absent.** The operator must see
  where the customer is being registered (it determines who can serve them
  later), but with cross-branch orders refused, an editable field could only
  ever produce a customer unusable for the order that created them.

- **Both create forms share one contract.** They call the same
  `useCreateCustomer` with the same `NewCustomerInput`; a field added or renamed
  on that contract breaks `tsc` in both places. Validation bounds are copied
  from the existing `zod` schema (name ≤100, phone ≤12, address ≤200) so the two
  forms reject the same input.

- **Zen mode needs two guards, both known up front.** `Esc` reaches the zen
  handler on `window` after Radix has already closed the dialog on `document`
  (nothing calls `stopPropagation`), so the zen handler bails while the dialog
  is open. And `DialogContent`/`DialogOverlay` are `z-50` against a zen
  container that is also `fixed inset-0 z-50` (`dialog.tsx:18,35` vs
  `NewOrder.tsx`'s portal) — DOM order should win, and if it doesn't the fix is
  to drop the zen container to `z-40`, never to bump the shared primitive.

## Dependency graph

```
T96 mode segmented control + توصيل default + address in saveBlockedReason
 └── T98 address auto-fill (needs the delivery panel + the blocked-reason chain)
       └─┐
T97 branch-scoped picker + SelectedCustomer.branchId + cross-branch block
       ├─── T100 wire quick-add into the picker (button, seeds, selection,
T99 QuickAddCustomerDialog.tsx (new file — no deps, parallelizable) ──┘  zen guards)
```

T99 touches only a new file and can be built at any time, including first.
T96 → T98 and T97 → T100 are the only hard orderings; T96 and T97 both edit
`NewOrder.tsx` and should land in sequence to keep the diffs readable.

## Verification model

No test runner exists in the console (spec §Testing Strategy), so every task's
gate is `pnpm build && pnpm lint` clean plus the named manual check in that
task. The full 19-step checklist in the spec runs as a whole at checkpoint 12a,
against a tenant with **≥2 branches**, customers on each, and at least one
customer with a profile address and one without.

## Risks and mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| `saveBlockedReason`/`save()` read `contactAddress` instead of `displayedAddress` → a valid prefilled order can't be saved | **High** | Called out as its own architecture decision above and as an explicit T98 acceptance criterion; manual step 11 saves a prefilled, untouched order end-to-end |
| توصيل default surprises operators who mostly do pickup | Med | User's explicit decision; both states are always visible and labelled, and pickup is one click. Revisit only with real usage — changing the default is an Ask-first boundary |
| Radix dialog paints behind the zen overlay (spec OQ3) | Low | Both are `z-50` and the dialog mounts later, so DOM order should win; fallback is zen container → `z-40`. Verified in manual step 9, not assumed |
| `Esc` in zen closes the dialog *and* exits zen | Med | Zen keydown handler bails while the dialog is open; explicit criterion in T100 and manual step 9 |
| Combobox's outside-mousedown handler fights the dialog portal | Low | The popover is closed when the dialog opens, so the portal is never a competing outside-click target |
| `GET /hq/customers/{id}` is slow on a large tenant (spec OQ4) | Med | Gated to delivery + a selected customer, cached by `qk.customer` and shared with the customer profile page. If it bites, the fix is `address` on the list row — a gateway change, Ask-first |
| A quick-added customer has no group and `credit_limit: 0` | Low | Accepted and recorded in the spec; correct for order-taking, editable afterwards on the profile |
| Branch switch strands the selected customer | Med | Non-destructive by design: customer kept, badge + blocked save, rescoped picker offers the replacement. Cart is never reset (existing T17 rule) |
| The two create forms drift apart | Low | One `useCreateCustomer`/`NewCustomerInput` contract for both; `CreateCustomerDialog.tsx` untouched, asserted via `git diff --stat` in criterion 7 |

## Open questions

1. **OQ3 — dialog stacking in zen mode.** Settled empirically in T100's manual
   check; the fallback (zen → `z-40`) is decided in advance so it isn't a
   mid-task decision.
2. **OQ4 — customer-detail fetch cost.** Carried, not blocking. Revisit only if
   a real tenant shows it; the alternative is a gateway change and out of scope.
