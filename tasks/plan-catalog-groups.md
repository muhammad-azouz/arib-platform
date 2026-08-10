# Implementation Plan: Catalog — drill-down group column

Spec: `tasks/spec-catalog-groups.md` (2026-08-10) · Tasks: `tasks/todo.md` §Phase 11 (T91–T95)

## Overview

Replace the always-expanded recursive group tree in the catalog sidebar
(`console/src/pages/console/Catalog.tsx:191`) with a one-level-at-a-time drill
column plus a sticky breadcrumb header, animated like Inkdrop's sidebar drill —
a 14px directional slide + crossfade, no stagger, no spring. Console-only
change: no API, gateway, or type changes, and no new dependency.

Five tasks, front-loaded so the **behavioral** change (T91–T92) lands and gets
reviewed before any **motion** is added (T93–T94). If the motion work has to be
abandoned, T91–T92 alone still ship a strictly better sidebar.

## Architecture decisions (grounded in the code)

- **`GroupDrill` owns the drill path; `Catalog` keeps owning `groupId`.** The
  current level is *not* derivable from the selected group — clicking a leaf
  filters the table without changing level, so `groupId` alone can't encode
  where the column is standing. `GroupDrill` therefore holds `path: GroupNode[]`
  internally and reports selection outward through the existing `onSelect`
  shape. `Catalog.tsx`'s diff stays ~10 lines (swap `<aside>`'s body, delete
  `GroupTree`/`buildGroupTree`/`ROOT_PARENT`), and every existing concern on
  that page — `filterKey` page-reset, `?search=` deep link, 402/gateway states,
  `keepPreviousData` — is untouched by construction.

- **Crumb collapse is depth-based, not width-measured** — *spec amendment*.
  The spec said "collapse whenever the path would exceed the column width";
  measuring that needs a ResizeObserver plus a measure→collapse→re-measure
  loop that can oscillate. Deterministic rule instead: always render
  `[📁] › (… if depth > 2) › parent › current`, i.e. at most the last two
  crumbs, with `truncate` + `title` on each. Same visual the user approved
  (`📁 › … › مشروبات › ساخنة`), no measurement, no oscillation. Long names are
  handled by truncation rather than by hiding more crumbs.

- **Both panes stay mounted during a transition**, outgoing one absolutely
  positioned inside an `overflow-hidden` viewport. An enter-only animation is
  cheaper but makes the old level vanish abruptly, which is exactly the
  jumpiness the drill is meant to remove.

- **Transitions are keyed on a monotonic `navSeq`, not on the group id.** Two
  navigations can land on the same id (root → drill → back to root), and a
  stale cleanup timer must never clear a newer transition. `navSeq` also gives
  the rapid-click rule a trivial implementation: a new navigation drops the
  in-flight outgoing pane immediately (snap to end) and promotes the current
  pane to outgoing.

- **Height animation is its own task (T94), explicitly droppable.** Animating
  to/from `auto` requires a px→px dance (freeze current px, reflow, set target,
  release to `auto` on end). It is the one piece most likely to fight the
  layout; isolating it means T93's slide can ship even if T94 is reverted.

- **RTL direction sign is a named constant, not a logical property.**
  `transform: translateX()` does not flip with `dir`, and the app is
  `dir="rtl"` app-wide (`index.html:2`). One `DRILL_SHIFT` constant fed to the
  keyframes via a CSS custom property, so the sign is stated once and readable.

- **Keyframes live in `src/index.css` beside `.animate-rise`** and reuse its
  `cubic-bezier(0.22, 1, 0.36, 1)` easing, so this page's motion belongs to the
  same system as the rest of the console.

## Dependency graph

```
T91 GroupDrill — tree helpers + one-level column + header (instant swap)
 ├── T92 crumb collapse (… dropdown)          ─┐
 └── T93 pane slide + crossfade (keyframes)    ├─ parallelizable after T91
       ├── T94 height smoothing                │
       └── T95 a11y: reduced motion, Backspace, focus
```

T92 and T93 touch disjoint parts of `GroupDrill.tsx` (header vs pane shell) and
can run in parallel after T91. T95's reduced-motion branch needs T93's
keyframes to exist, so it goes last.

## Verification model

No test runner exists in this repo (spec §Testing Strategy), so every task's
gate is `pnpm build && pnpm lint` plus a named manual check in `pnpm dev`
against a tenant with a ≥3-level group hierarchy. The full checklist lives in
the spec and is executed as a whole at checkpoint 11c.

## Risks and mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| Height px→`auto` dance fights the layout or flickers | Med | Isolated in T94; if it misbehaves, revert that one task — T93's slide stands alone. Fallback if reverted: a `min-h` on the viewport so short levels don't collapse the column |
| Rapid clicks leave a ghost/stuck outgoing pane | Med | `navSeq`-keyed transitions + explicit acceptance criterion in T93; spam-click both directions during manual check |
| RTL transform sign inverted (drill-in appears to come from the wrong edge) | Low | Named `DRILL_SHIFT` constant; both directions verified visually at checkpoint 11b, which is a human review anyway |
| Parent rows show `٠` because `product_count` is direct-only | Med | Known and accepted (spec open Q1) — the badge intentionally matches what the table will show. Revisit only after the user sees real tenant data; a rollup is a gateway change (Ask-first) |
| A level with many groups overflows the viewport | Low | Deferred to T94's height work; add `max-h` + internal scroll only if a real tenant level exceeds it (spec open Q2) |
| `Backspace` handler swallows typing or triggers browser back | Low | Handler bails when the event target is an input/textarea and calls `preventDefault`; explicit acceptance criterion in T95 |
| Deleting `GroupTree` breaks another importer | Low | It is file-local to `Catalog.tsx` (verified — not exported); `pnpm build` catches any surprise |

## Open questions

1. **Direct-only parent counts** (spec open Q1) — carried, not blocking. Decide
   after seeing a real tenant's hierarchy at checkpoint 11a.
2. **Column max-height** (spec open Q2) — decided during T94 against real data,
   not up front.
