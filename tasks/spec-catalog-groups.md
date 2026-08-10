# Spec: Catalog — drill-down group column

Replaces the always-expanded recursive group tree in the catalog sidebar
(`console/src/pages/console/Catalog.tsx:191` `GroupTree`) with a single-level
drill-down column plus a breadcrumb header, animated like Inkdrop's sidebar
notebook drill — one short slide + crossfade, no per-row stagger, no bounce.

Scope: the catalog page only. Inventory (`Inventory.tsx`) has a similar group
filter and is deliberately **not** touched in this pass.

## Objective

**Problem.** The current sidebar renders the whole group tree expanded at every
depth, indented by `ps-4` per level. With a real tenant's group hierarchy the
240px column becomes a long, deeply-indented scroll where names truncate to
nothing and the user loses track of which level they are on.

**Build.** A group column that shows exactly one level at a time. Clicking a
group both filters the product table to that group and, if it has children,
drills into them. A sticky breadcrumb header at the top of the column shows the
path and is the only way back up.

**User.** HQ staff browsing a tenant's catalog to find products by group.

**Success looks like:** the column never scrolls for a level of ≤12 groups,
names are readable without truncation at 240px, the current position is always
visible in the header, and any ancestor is reachable in one click.

### Interaction contract (decided with the user 2026-08-10)

| Action | Result |
|---|---|
| Click a group **with** children | Column animates to its children **and** table filters to that group. Header appends a crumb. |
| Click a group **without** children (leaf) | Table filters to it. Column stays on the current level; the row shows the selected state. No animation. |
| Click the 📁 icon in the header | Return to root level **and** clear the group filter (back to "كل الأصناف"). |
| Click any crumb in the header | Jump to that level **and** filter by that group. Column animates back. |
| Click the collapsed `…` crumb | Opens a dropdown listing the hidden middle ancestors; picking one behaves exactly like clicking a crumb. |
| `Backspace` while focus is inside the column | Up one level (same as clicking the parent crumb). |

**No "رجوع" row in the list.** The user rejected it as redundant with the
header — the header icon and crumbs are the sole up-navigation.

**Filter semantics are unchanged and exact-match.** `GET
/v1/tenants/{id}/hq/catalog/products?group_id=…` filters to products whose
group is exactly that id — there is no descendant rollup, and this spec does
**not** add one. So drilling into a parent shows only the products directly in
that parent, and the count badge on each row stays `product_count` (direct)
so the badge always matches what the table will show.

**Root level.** The header at root renders `📁 كل الأصناف` — the icon plus a
single non-interactive crumb. There is no separate "كل الأصناف" list row (same
redundancy argument as the back row); standing at root with no leaf selected
*is* the unfiltered state.

**Search interplay.** Unchanged: search term and group filter apply together,
and changing either resets to page 1 via the existing `filterKey` mechanism.

### Animation contract

Reference feel: Inkdrop's sidebar drill — the column content moves a short
distance and crossfades. Restrained, not showy.

- **Distance:** 14px, not a full-width push.
- **Direction:** the app is RTL app-wide (`index.html:2` `dir="rtl"`).
  Drilling **in**: the incoming level enters from the **left** edge, the
  outgoing level exits to the **right**. Going **up**: mirrored.
  *Corrected 2026-08-10 during T93: this originally said "inline-start (left)",
  but inline-start is the **right** edge in RTL — the two halves contradicted
  each other. Left-on-drill-in is the mirror of the LTR/Inkdrop convention and
  is what the parenthetical meant. Written as literal left/right in the CSS,
  since `translateX` does not flip with `dir` anyway.*
- **Duration:** 200ms for the transform, 130ms for opacity, both on
  `cubic-bezier(0.22, 1, 0.36, 1)` — the same easing already used by
  `.animate-rise` in `console/src/index.css:135`, so this page's motion stays
  of a piece with the rest of the app.
- **Both panes animate.** The outgoing level stays mounted, absolutely
  positioned, for the duration of the transition; the container's height
  transitions between the two measured heights so the table beside it does not
  jump.
- **No per-row stagger, no scale, no spring, no blur.**
- **Header crumbs:** an appended crumb fades + slides 6px into place over
  130ms. Removed crumbs disappear without their own exit animation.
- **`prefers-reduced-motion: reduce`:** no transforms and no height animation —
  the level swaps instantly; opacity crossfade may remain at ≤80ms.
- **Rapid clicks** must not queue or stack transitions: a new drill starting
  mid-animation snaps the in-flight one to its end state and starts fresh.

## Tech Stack

React 19 · TypeScript 6 · Vite 8 · Tailwind CSS v4 (`@theme inline` tokens in
`src/index.css`) · TanStack Query v5 · react-router-dom v7 · Radix primitives
via local `src/components/ui/*` · `@solar-icons/react` re-exported through
`src/components/icon.tsx`.

**No animation library.** `framer-motion` is not a dependency and this spec
does **not** add one — the whole effect is CSS keyframes + a height transition.
Adding a dependency for it falls under "Ask first".

## Commands

```
Dev:   pnpm dev            # vite, from console/
Build: pnpm build          # tsc -b && vite build  — the type gate
Lint:  pnpm lint           # eslint .
```

There is no test runner in this project (no vitest/jest in `package.json`) —
see Testing Strategy.

## Project Structure

```
console/src/pages/console/Catalog.tsx   → page; owns groupId + drill path state
console/src/components/GroupDrill.tsx   → NEW: column + breadcrumb header + animation
console/src/components/icon.tsx         → icon re-exports (crumb separator)
console/src/index.css                   → @keyframes for the drill panes
platform/tasks/spec-catalog-groups.md   → this spec
```

`buildGroupTree` / `GroupNode` move out of `Catalog.tsx` into `GroupDrill.tsx`
(they exist only to serve the column). `ROOT_PARENT` moves with them.

## Code Style

Match the existing file: Arabic UI strings inline, `cn()` for class merging,
logical properties (`ps-`/`pe-`/`start-`/`end-`) never `pl-`/`left-`, arrow
functions in props, no semicolons, single quotes, comments only where the
*why* is non-obvious (the existing file's comments on the `filterKey`
adjust-during-render pattern are the house style — explain the reasoning, not
the mechanics).

```tsx
<button
  type="button"
  onClick={() => onOpen(node)}
  className={cn(
    'flex w-full items-center gap-2 rounded-lg px-2.5 py-1.5 text-sm transition-colors hover:bg-accent/60',
    selected === node.id ? 'bg-accent font-semibold text-primary' : 'text-foreground/80',
  )}
>
  <GroupIcon className="size-4 shrink-0 text-muted-foreground" />
  <span className="min-w-0 flex-1 truncate">{node.name}</span>
  <span className="shrink-0 text-xs text-muted-foreground">
    {toArabicDigits(node.product_count)}
  </span>
</button>
```

Numbers in the UI always go through `toArabicDigits` (`src/lib/format.ts`).

## Testing Strategy

No automated test framework exists in this repo, and this spec does not
introduce one (that is a separate decision). Verification is therefore:

1. **Type + lint gate:** `pnpm build` and `pnpm lint` must both pass clean.
2. **Manual checklist** against a tenant with a ≥3-level group hierarchy
   (`pnpm dev`, navigate to a tenant's catalog):
   - Drill in/out at every level; header path matches the column contents.
   - Table rows change to the drilled group's direct products on each step.
   - Icon click returns to root *and* clears the filter.
   - Mid-path crumb click lands on that level with that group filtered.
   - `…` dropdown appears only when the path exceeds the visible budget and
     jumps correctly.
   - Leaf click filters without animating or changing level.
   - Spam-clicking two groups alternately leaves no stuck/ghost pane.
   - The products table does not visibly jump while the column changes height.
   - `Backspace` with focus in the column goes up one level and does not
     trigger browser back-navigation.
   - System setting "reduce motion" on → no sliding, nothing broken.
   - Empty state: tenant with zero groups → column shows only the root header
     and an empty-list message, no crash.
3. **Regression check:** the command-palette deep link
   `/tenants/:id/catalog?search=…` still lands with the search prefilled and
   the column at root.

## Boundaries

**Always**
- Run `pnpm build` and `pnpm lint` before considering a task done.
- Use logical CSS properties — this app is RTL.
- Keep every user-facing string Arabic and consistent with existing copy.
- Respect `prefers-reduced-motion`.
- Keep group rows as real `<button>`s (keyboard + focus ring already work).

**Ask first**
- Adding any dependency (animation library included).
- Changing the API/gateway contract — e.g. adding descendant rollup to
  `group_id` filtering, or a rollup count field.
- Applying the same component to Inventory's group filter.
- Putting the drill path in the URL (explicitly out of scope this pass).

**Never**
- Change product-table columns, pagination, or search behavior.
- Touch `platform/api` or the gateway for this feature.
- Remove the existing group-loading skeleton.
- Introduce motion that runs on page load (the column must appear settled).

## Success Criteria

1. The sidebar shows exactly one level of groups at a time; no indentation,
   no expanded descendants.
2. A sticky header at the top of the column renders `📁 › … › parent › current`,
   collapsing middle crumbs into a clickable `…` dropdown. Icon and every crumb
   are clickable per the interaction contract table.
   *Amended 2026-08-10 (`plan-catalog-groups.md` §Architecture): collapse is
   depth-based — at most the last two crumbs, `…` whenever depth > 2 — not
   width-measured, which would need a ResizeObserver and can oscillate. Long
   names are handled by `truncate` + `title` instead.*
3. Clicking a group with children drills in **and** filters; a leaf only
   filters.
4. Level changes animate: 14px directional slide + crossfade, 200ms, correct
   RTL direction in and out, container height transitions with no jump in the
   products area.
5. No `framer-motion` or other new dependency in `package.json`.
6. `prefers-reduced-motion: reduce` disables transform and height animation.
7. `pnpm build` and `pnpm lint` pass clean.
8. The full manual checklist above passes on a ≥3-level hierarchy.
9. No regression to search, pagination, the `?search=` deep link, the 402
   "no subscription" state, or the gateway-error state.

## Open Questions

1. **Product counts on parent rows are direct-only.** A parent with 0 direct
   products but 40 across its children will show `٠`. Accepted for now
   (the badge matches the table), but if it reads as broken to the user the
   fix is a client-side rollup count shown as a secondary figure — flag it
   after seeing real data.
2. **Column height policy.** Animating to the measured height is specified;
   if a level is very long the column may need a max-height with internal
   scroll. Deferred to implementation — pick a `max-h` only if a real tenant
   level exceeds the viewport.
3. **Plan/task output location.** `platform/tasks/plan.md` and `todo.md` are
   already owned by the console redesign. Phase 2/3 output for this feature
   will be appended as a clearly-labelled section there rather than
   overwriting them.
