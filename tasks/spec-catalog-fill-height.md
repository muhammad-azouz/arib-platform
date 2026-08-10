# Spec: Catalog products table fills remaining height and scrolls internally

## Objective

On `/tenants/:tenantId/catalog`, the products table grows with its 25 rows and pushes
the whole document past the viewport, so the **window** scrollbar appears and the page
header + group column scroll away with it.

Target behaviour — the same shape the order workspace already has (`NewOrder.tsx` →
`ProductGrid.tsx`): the page occupies exactly the viewport, and the products list is the
only thing that scrolls. Page header, search box, group column and pagination stay put.

**User:** console operator browsing a tenant catalog.
**Success looks like:** the browser window never scrolls on the catalog page (desktop);
the table body does.

## Assumptions I'm making

1. "Like products in the new order page" means the *behaviour* (inner pane scrolls,
   surrounding chrome pinned) — not that the catalog table should become a card grid.
2. Desktop-only concern. Below `lg` the catalog collapses to one column (group column
   stacked over the table); locking both into a split viewport there would leave each a
   few rows tall, so **below `lg` the page keeps normal top-to-bottom scrolling.**
3. The group column (`GroupDrill`) gets the same treatment it has in the order
   workspace — `h-full min-h-0 overflow-y-auto` — so a deep group tree scrolls in place
   instead of stretching the row.
4. Pagination stays *below* the scroll area, always visible (as in `ProductGrid`).
5. `PAGE_SIZE` stays 25. This is a layout fix, not a paging change.
6. No test framework exists in this project (no test script, no test files) — verification
   is `pnpm build` + `pnpm lint` + manual browser check.
7. Sticky table header (`thead` pinned while the body scrolls) is **in scope** — a
   scrolling table without pinned headers is worse than what we have today.

→ Correct me now or I'll proceed with these.

## Tech Stack

React 19 · react-router-dom 7 · TanStack Query 5 · Tailwind CSS 4 · Vite 8 · TypeScript 6

## Commands

```
Dev:   pnpm --dir console dev
Build: pnpm --dir console build     # tsc -b && vite build
Lint:  pnpm --dir console lint
Test:  (none configured)
```

## Project Structure

```
console/src/components/AppShell.tsx        → sidebar + header + <main> scroll shell
console/src/components/GroupDrill.tsx      → group column (already accepts a sizing className)
console/src/components/ui/table.tsx        → Table primitives
console/src/pages/console/Catalog.tsx      → the page being changed
console/src/components/orders/ProductGrid.tsx → the reference implementation
```

## The core decision: where the height constraint comes from

For a child to "fill the remaining height", every ancestor up to the viewport must have a
resolved height. Today `AppShell` is `min-h-screen` — deliberately unbounded — so the
document grows and the *window* scrolls. Two ways out:

### Option A — make the shell the scroll container ✅ CHOSEN

`AppShell` becomes a fixed-height frame; `<main>` becomes the scroll container.

```tsx
// AppShell.tsx
<div className="flex h-screen overflow-hidden">          // was: min-h-screen
  <aside className="... h-full overflow-y-auto ..." />    // sticky no longer needed
  <div className="flex min-w-0 flex-1 flex-col">
    <header className="shrink-0 ..." />                   // sticky no longer needed
    <main className="mx-auto w-full max-w-6xl min-h-0 flex-1 overflow-y-auto px-5 py-7">
      <div className="animate-rise h-full">
        <Outlet />
      </div>
    </main>
  </div>
</div>
```

Every other page is unaffected in appearance: long pages still scroll, the scrollbar just
lives on `<main>` instead of the window, and the header/sidebar stay pinned exactly as
their `sticky` rules already made them. A page that wants to fill opts in by giving its
own root `h-full` (and going flex from there).

**Two details settled during implementation, both load-bearing:**

1. **The wrapper is `h-full`, not `min-h-full`.** `min-height` cannot constrain an
   *oversized* child: with 25 rows the wrapper's auto height simply resolves to the
   content height, free space is zero, and `flex-1` on the table column grows by nothing —
   the original bug, unchanged. Only a definite height produces the negative free space
   that lets the table column shrink into the viewport.
2. **The wrapper stays a plain block, not a flex column.** As a flex container it would
   impose flex sizing on every other page's root children, and any such child that is a
   scroll container (`overflow-*`) has an automatic minimum size of *zero* — it would
   collapse and clip on a page taller than the viewport. Keeping it a block means pages
   that don't opt in are untouched: their content overflows the `h-full` box and `<main>`
   scrolls, exactly as the window used to. `Catalog` supplies its own `flex h-full
   flex-col` root instead.

- **Cost:** touches the shell — every page rides on it.
- **Payoff:** honest layout. Any future page can fill the viewport with two utility
  classes, and `NewOrder`'s `h-[70vh]` magic number becomes deletable (not in this change).

### Option B — page-local computed height

Leave the shell alone; wrap the catalog workspace in
`h-[calc(100vh-3.5rem-3.5rem-<PageHeader height>)]`.

- **Cost:** hardcodes the header height, the `py-7` padding and the PageHeader height into
  a magic number that silently breaks when any of them change. It is exactly why
  `NewOrder` settled for `h-[70vh]` — an approximation that leaves dead space or a hair of
  page scroll depending on the viewport.
- **Payoff:** zero risk to other pages.

**Recommendation: Option A.** It's the fix; B is the workaround.

## Code Style

Matches the file's existing idiom — Tailwind utilities inline, Arabic UI copy, comments
that explain *why* a layout rule exists (as `GroupDrill`'s `className` doc does):

```tsx
// Catalog.tsx — the workspace fills what the shell leaves, and only the
// table body scrolls (same shape as the order workspace's ProductGrid).
<div className="grid gap-4 lg:min-h-0 lg:flex-1 lg:grid-cols-[240px_1fr]">
  <GroupDrill … className="lg:h-full lg:min-h-0 lg:overflow-y-auto" />

  <div className="flex min-w-0 flex-col lg:min-h-0">
    <div className="relative mb-4 shrink-0"> …search… </div>
    <ProductsTable … />                       {/* min-h-0 flex-1 overflow-y-auto inside */}
    <div className="shrink-0"> …Pagination… </div>
  </div>
</div>
```

Sticky header inside the scrolling table:

```tsx
<TableHeader className="sticky top-0 z-10 bg-card">
```

## Testing Strategy

No test framework is configured in `console/`. Verification is:

1. `pnpm --dir console build` — type-check + bundle clean.
2. `pnpm --dir console lint` — no new findings.
3. Manual browser pass (the acceptance list below), at ≥`lg` and at mobile width.

If the change is worth guarding long-term, that's a separate "add Vitest + RTL" task —
out of scope here.

## Boundaries

- **Always:** keep Arabic copy and RTL logical properties (`ps-*`/`start-*`, never
  `pl-*`/`left-*`); reuse `GroupDrill`'s existing `className` escape hatch rather than
  adding a new prop; run build + lint before declaring done.
- **Ask first:** any change to `AppShell` beyond the height/overflow rules above; changing
  `PAGE_SIZE`; touching `NewOrder`/`ProductGrid`; adding a dependency (e.g. a virtualizer).
- **Never:** hardcode a pixel height for the header/PageHeader; introduce a window
  `resize` listener or JS measurement to compute the table height; regress any other
  page's scrolling.

## Success Criteria

1. At ≥1024px on `/tenants/:id/catalog` with ≥25 products, the **window/`<main>` shows no
   vertical scrollbar** — the page fits the viewport exactly.
2. The products table body scrolls internally; its column headers stay visible while it does.
3. `PageHeader`, the search input, the group column's breadcrumb and the pagination bar all
   remain visible without scrolling.
4. The group column scrolls independently when its level has more entries than fit.
5. Loading / empty / not-subscribed / gateway-error states still render correctly inside the
   constrained layout (no clipped or zero-height panes).
6. Below `lg`, the page scrolls normally top-to-bottom — nothing is squashed.
7. Every other console page (Overview, Branches, Inventory, Customers, Suppliers, Orders,
   Reports, Company, Settings, ProductDetail, Conflicts) scrolls and looks as before,
   with the sidebar and top header still pinned.
8. `pnpm build` and `pnpm lint` pass.

## Resolved Questions

1. **Option A or B?** → **A**, the shell fix. Implemented.
2. **Convert `NewOrder`'s `h-[70vh]` too?** → **No**, follow-up. The shell now supports it:
   `NewOrder.tsx:398`'s `<div className="h-[70vh]">` becomes `min-h-0 flex-1` once its root
   `flex flex-col gap-4` also carries `h-full`.

## Files Changed

| File | Change |
|---|---|
| `components/AppShell.tsx` | `min-h-screen` → `h-screen overflow-hidden`; a new full-width box between header and `<main>` is the scroll container (`min-h-0 flex-1 overflow-y-auto py-7`), so the scrollbar sits at the window edge, not at the centered column's; `<main>` keeps `mx-auto max-w-6xl px-5` and gains `h-full`; wrapper gains `h-full`; sidebar/header drop now-redundant `sticky` (sidebar gains `overflow-y-auto`, header gains `shrink-0`) |
| `components/ui/table.tsx` | New optional `containerClassName` — the sticky `<thead>` sticks to `Table`'s own wrapper, so the Y-constraint has to land there |
| `pages/console/Catalog.tsx` | Root becomes `flex h-full flex-col`; workspace `lg:min-h-0 lg:flex-1`; table column flex-col with a scrolling table and a pinned pagination; sticky table header |
| `components/GroupDrill.tsx` | `className` doc updated for the catalog's new fixed-height usage (no behaviour change) |

## Verification Record

- `pnpm build` — clean (tsc + vite).
- `pnpm lint` — 0 errors; the 2 `react-refresh` warnings in `lib/auth.tsx` are pre-existing
  and untouched.
- Emitted CSS confirmed to contain every new utility, including the arbitrary
  `[&_tr]:shadow-[inset_0_-1px_0_var(--border)]`.
- `twMerge` confirmed to drop `TableHeader`'s base `[&_tr]:border-b` in favour of
  `[&_tr]:border-0`, so the header's separator is the shadow alone (a collapsed table
  border is painted by the table, not the sticky `<thead>`, and would scroll away).
- **Not done: the manual browser pass** (success criteria 1–7). It needs a signed-in
  session against a live tenant/gateway, which this environment doesn't have.
