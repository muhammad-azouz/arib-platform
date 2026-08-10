import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { cn } from '@/lib/utils'
import { toArabicDigits } from '@/lib/format'
import type { CatalogGroup } from '@/lib/types'
import { ArrowLeading, GroupIcon } from '@/components/icon'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'

/** Groups whose parent is the all-zero guid sit at the top of the tree. */
const ROOT_PARENT = '00000000-0000-0000-0000-000000000000'

/**
 * How many trailing crumbs stay visible; everything before them collapses into
 * the "…" menu. Deliberately a fixed depth rather than a measured width — the
 * measure/collapse/re-measure loop that width-fitting needs can oscillate, and
 * long names are already handled by truncation.
 */
const VISIBLE_CRUMBS = 2

/** Must match `--drill-ms` on `.drill-viewport` in index.css. */
const DRILL_MS = 200

/** The level being replaced, kept mounted so it can slide out. */
interface Outgoing {
  /**
   * Monotonic navigation id. Keyed on this rather than on the group id because
   * two navigations can land on the same level (root → drill → back to root),
   * and a stale cleanup timer must never retire a newer transition.
   */
  seq: number
  nodes: GroupNode[]
  /** 1 = drilling deeper, -1 = going back up. */
  dir: 1 | -1
  /**
   * Viewport height at the moment we navigated — the starting point of the
   * height animation, and the height the outgoing pane keeps while it slides
   * out (it's absolutely positioned, so it would otherwise be squashed by the
   * shrinking viewport).
   */
  height: number
}

interface GroupNode extends CatalogGroup {
  children: GroupNode[]
}

function buildGroupTree(groups: CatalogGroup[]) {
  const byId = new Map<string, GroupNode>(groups.map((g) => [g.id, { ...g, children: [] }]))
  const roots: GroupNode[] = []
  for (const node of byId.values()) {
    const parent = node.parent_id !== ROOT_PARENT ? byId.get(node.parent_id) : undefined
    if (parent) parent.children.push(node)
    else roots.push(node)
  }
  const sortTree = (list: GroupNode[]) => {
    list.sort((a, b) => a.num - b.num)
    list.forEach((n) => sortTree(n.children))
  }
  sortTree(roots)
  return { roots, byId }
}

/**
 * The catalog's group filter: one level of groups at a time, with a breadcrumb
 * header as the only way back up.
 *
 * The drill path lives here rather than on the page because the level is *not*
 * derivable from the selected group — clicking a leaf filters the table without
 * moving the column — so `selected` alone cannot encode where we are standing.
 * The path is held as ids, not nodes, so a groups refetch can't leave us
 * pointing at stale objects.
 */
export function GroupDrill({
  groups,
  isLoading,
  selected,
  onSelect,
}: {
  groups: CatalogGroup[]
  isLoading: boolean
  selected?: string
  onSelect: (id: string | undefined) => void
}) {
  const [pathIds, setPathIds] = useState<string[]>([])
  const [outgoing, setOutgoing] = useState<Outgoing | null>(null)
  // Kept in state, not a ref, because the incoming pane is keyed on it: a ref
  // wouldn't re-key, and keying on `outgoing` instead would remount the pane a
  // second time when the transition retires — dropping keyboard focus with it.
  const [navSeq, setNavSeq] = useState(0)
  const { roots, byId } = useMemo(() => buildGroupTree(groups), [groups])

  const viewportRef = useRef<HTMLDivElement>(null)
  const paneRef = useRef<HTMLDivElement>(null)
  const focusPendingRef = useRef(false)

  // A keyboard-driven drill must land focus in the level it opened, or the
  // user is stranded on a button that no longer exists. Pointer clicks are left
  // alone — moving focus there would scroll the page under the cursor.
  useLayoutEffect(() => {
    if (!focusPendingRef.current) return
    focusPendingRef.current = false
    paneRef.current?.querySelector('button')?.focus()
  }, [navSeq])

  // Animate the viewport between the two levels' heights so the products table
  // beside it doesn't jump. Height can't transition to or from `auto`, so this
  // is the px→px dance: pin the old height, force a reflow, set the measured
  // new one — then hand the height back to the content when the run ends.
  useLayoutEffect(() => {
    const viewport = viewportRef.current
    const pane = paneRef.current
    if (!outgoing || !viewport || !pane) return
    const target = pane.offsetHeight
    viewport.style.height = `${outgoing.height}px`
    void viewport.offsetHeight
    viewport.style.height = `${target}px`
  }, [outgoing])

  // Retire the outgoing pane on a timer rather than on `animationend`: under
  // `prefers-reduced-motion` there is no animation to end, and the pane would
  // otherwise stay mounted forever.
  useEffect(() => {
    if (!outgoing) return
    const t = window.setTimeout(() => {
      // Back to `auto`, so the column tracks its own content again — otherwise
      // it stays frozen at a stale px height across resizes and refetches.
      if (viewportRef.current) viewportRef.current.style.height = ''
      setOutgoing((cur) => (cur?.seq === outgoing.seq ? null : cur))
    }, DRILL_MS)
    return () => window.clearTimeout(t)
  }, [outgoing])

  // Resolve ids to nodes, stopping at the first one the current data no longer
  // has — a group deleted at a branch shouldn't strand us in a phantom level.
  const path = useMemo(() => {
    const out: GroupNode[] = []
    for (const id of pathIds) {
      const node = byId.get(id)
      if (!node) break
      out.push(node)
    }
    return out
  }, [pathIds, byId])

  const level = path.length > 0 ? path[path.length - 1].children : roots

  // Ancestors beyond the last VISIBLE_CRUMBS collapse into the "…" menu; a
  // hidden crumb's index in `path` is its index in `hidden`, so jumping to one
  // needs no offset — unlike the visible tail.
  const hidden = path.slice(0, Math.max(0, path.length - VISIBLE_CRUMBS))
  const visible = path.slice(hidden.length)

  /**
   * Every level change goes through here. A navigation that doesn't change
   * depth (clicking a leaf, or the root icon while already at root) only moves
   * the filter, so it must not animate.
   */
  const navigate = (nextIds: string[], groupId: string | undefined, viaKeyboard = false) => {
    if (nextIds.length !== path.length) {
      const seq = navSeq + 1
      setNavSeq(seq)
      focusPendingRef.current = viaKeyboard
      // Replacing an in-flight outgoing pane unmounts it immediately — that is
      // the "snap to end, start clean" rule for rapid clicks, for free.
      setOutgoing({
        seq,
        nodes: level,
        dir: nextIds.length > path.length ? 1 : -1,
        height: viewportRef.current?.offsetHeight ?? 0,
      })
    }
    setPathIds(nextIds)
    onSelect(groupId)
  }

  /** A group with children drills in *and* filters; a leaf only filters. */
  const openGroup = (node: GroupNode, viaKeyboard = false) => {
    const ids = path.map((n) => n.id)
    navigate(node.children.length > 0 ? [...ids, node.id] : ids, node.id, viaKeyboard)
  }

  const goRoot = (viaKeyboard = false) => navigate([], undefined, viaKeyboard)

  const goCrumb = (index: number, viaKeyboard = false) =>
    navigate(
      path.slice(0, index + 1).map((n) => n.id),
      path[index].id,
      viaKeyboard,
    )

  /** Backspace anywhere in the column goes up one level. */
  const onKeyDown = (e: React.KeyboardEvent<HTMLElement>) => {
    if (e.key !== 'Backspace' || path.length === 0) return
    // Never steal the key from a text field — and never let it reach the
    // browser as a back-navigation.
    const target = e.target as HTMLElement
    if (target.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(target.tagName)) return
    e.preventDefault()
    if (path.length === 1) goRoot(true)
    else goCrumb(path.length - 2, true)
  }

  return (
    <aside
      onKeyDown={onKeyDown}
      className="h-fit rounded-xl border border-border bg-card/50 p-2"
    >
      {isLoading ? (
        <div className="space-y-2 p-2">
          <div className="h-5 w-24 animate-pulse rounded bg-muted" />
          <div className="h-5 w-32 animate-pulse rounded bg-muted" />
          <div className="h-5 w-20 animate-pulse rounded bg-muted" />
        </div>
      ) : (
        <>
          <nav
            aria-label="مسار المجموعات"
            className="sticky top-0 z-10 mb-1 flex items-center gap-0.5 border-b border-border/60 bg-card/50 px-1 pb-2 text-xs backdrop-blur-sm"
          >
            <button
              type="button"
              onClick={(e) => goRoot(e.detail === 0)}
              aria-label="كل الأصناف"
              className="shrink-0 rounded p-0.5 text-muted-foreground transition-colors hover:text-foreground"
            >
              <GroupIcon className="size-4" />
            </button>
            {path.length === 0 && (
              <span className="truncate font-medium text-foreground">كل الأصناف</span>
            )}

            {hidden.length > 0 && (
              <span className="flex shrink-0 items-center gap-0.5">
                <ArrowLeading className="size-3 shrink-0 text-muted-foreground/50" />
                <DropdownMenu>
                  <DropdownMenuTrigger asChild>
                    <button
                      type="button"
                      aria-label="مستويات أعلى"
                      className="rounded px-1 leading-none text-muted-foreground transition-colors hover:text-foreground"
                    >
                      …
                    </button>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="start" className="max-w-64">
                    {hidden.map((node, i) => (
                      <DropdownMenuItem key={node.id} onSelect={() => goCrumb(i)}>
                        <GroupIcon className="size-4 shrink-0 text-muted-foreground" />
                        <span className="truncate">{node.name}</span>
                      </DropdownMenuItem>
                    ))}
                  </DropdownMenuContent>
                </DropdownMenu>
              </span>
            )}

            {visible.map((node, i) => {
              const index = hidden.length + i
              const last = index === path.length - 1
              return (
                <span
                  key={node.id}
                  className="animate-crumb-in flex min-w-0 items-center gap-0.5"
                >
                  <ArrowLeading className="size-3 shrink-0 text-muted-foreground/50" />
                  {last ? (
                    <span
                      title={node.name}
                      className="truncate font-medium text-foreground"
                      aria-current="page"
                    >
                      {node.name}
                    </span>
                  ) : (
                    <button
                      type="button"
                      onClick={(e) => goCrumb(index, e.detail === 0)}
                      title={node.name}
                      className="truncate rounded px-0.5 text-muted-foreground transition-colors hover:text-foreground"
                    >
                      {node.name}
                    </button>
                  )}
                </span>
              )
            })}
          </nav>

          <div ref={viewportRef} className="drill-viewport relative overflow-hidden">
            {outgoing && (
              <div
                key={`out-${outgoing.seq}`}
                aria-hidden
                inert
                style={{ height: outgoing.height }}
                className={cn(
                  'absolute inset-x-0 top-0',
                  outgoing.dir === 1 ? 'animate-drill-out-forward' : 'animate-drill-out-back',
                )}
              >
                <GroupLevel nodes={outgoing.nodes} selected={selected} />
              </div>
            )}
            {/* Remounts on every navigation so the enter animation replays even
                when two navigations land on the same level. */}
            <div
              key={navSeq}
              ref={paneRef}
              className={cn(
                outgoing &&
                  (outgoing.dir === 1 ? 'animate-drill-in-forward' : 'animate-drill-in-back'),
              )}
            >
              <GroupLevel nodes={level} selected={selected} onOpen={openGroup} />
            </div>
          </div>
        </>
      )}
    </aside>
  )
}

/** One level's rows. Rendered without `onOpen` for the outgoing pane. */
function GroupLevel({
  nodes,
  selected,
  onOpen,
}: {
  nodes: GroupNode[]
  selected?: string
  onOpen?: (node: GroupNode, viaKeyboard: boolean) => void
}) {
  if (nodes.length === 0) {
    return <p className="px-2.5 py-3 text-center text-xs text-muted-foreground">لا توجد مجموعات</p>
  }
  return (
    <ul className="space-y-0.5">
      {nodes.map((node) => (
        <li key={node.id}>
          <button
            type="button"
            /* `detail === 0` means Enter/Space rather than a real pointer
               click — the cue for whether focus should follow the drill. */
            onClick={(e) => onOpen?.(node, e.detail === 0)}
            aria-current={selected === node.id ? 'true' : undefined}
            className={cn(
              'flex w-full items-center gap-2 rounded-lg px-2.5 py-1.5 text-sm transition-colors hover:bg-accent/60',
              selected === node.id ? 'bg-accent font-semibold text-primary' : 'text-foreground/80',
            )}
          >
            <GroupIcon className="size-4 shrink-0 text-muted-foreground" />
            <span className="min-w-0 flex-1 truncate text-start" title={node.name}>
              {node.name}
            </span>
            <span className="shrink-0 text-xs text-muted-foreground">
              {toArabicDigits(node.product_count)}
            </span>
          </button>
        </li>
      ))}
    </ul>
  )
}
