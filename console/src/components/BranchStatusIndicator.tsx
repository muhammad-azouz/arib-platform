import { Link, useParams } from 'react-router-dom'
import { useHqBranches } from '@/lib/hooks'
import { relative, toArabicDigits } from '@/lib/format'
import type { BranchHealth } from '@/lib/types'
import { HealthDot } from '@/components/HealthDot'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'

// never < ok < lagging < stale — a single stale branch outranks everything
// else, same severity order the alerts panel uses to pick what to surface.
const SEVERITY: Record<BranchHealth, number> = { never: 0, ok: 1, lagging: 2, stale: 3 }

/**
 * Branch health summary beside the notifications bell: the worst health tier
 * across every branch, a count, and a dropdown listing each branch's status
 * so it's reachable from any page without a detour through the sidebar.
 */
export function BranchStatusIndicator() {
  const { tenantId } = useParams<'tenantId'>()
  const { data: hq } = useHqBranches(tenantId)

  if (!tenantId || !hq || hq.branches.length === 0) return null

  const worst = hq.branches.reduce<BranchHealth>(
    (w, b) => (SEVERITY[b.health] > SEVERITY[w] ? b.health : w),
    'never',
  )

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="sm" className="gap-1.5 px-2" aria-label="حالة الفروع">
          <HealthDot health={worst} />
          <span className="text-sm text-muted-foreground">
            {toArabicDigits(hq.branches.length)} فروع
          </span>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-72">
        <DropdownMenuLabel>حالة الفروع</DropdownMenuLabel>
        <div className="max-h-80 overflow-y-auto">
          {hq.branches.map((b) => (
            <DropdownMenuItem key={b.id} asChild className="items-center gap-2.5">
              <Link to={`/tenants/${tenantId}/branches/${b.id}`}>
                <HealthDot health={b.health} />
                <span className="min-w-0 flex-1 truncate">{b.name}</span>
                <span className="shrink-0 text-xs text-muted-foreground">
                  {relative(b.last_sync_at)}
                </span>
              </Link>
            </DropdownMenuItem>
          ))}
        </div>
        <DropdownMenuSeparator />
        <DropdownMenuItem asChild>
          <Link to={`/tenants/${tenantId}/branches`} className="justify-center text-primary">
            كل الفروع
          </Link>
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
