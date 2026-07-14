import { cn } from '@/lib/utils'
import type { BranchHealth } from '@/lib/types'

// The health dot: the one-glance answer to "which branch needs attention?".
// One component so the Branches cards and the Overview strip can never drift.
const HEALTH_DOT: Record<BranchHealth, string> = {
  ok: 'bg-success',
  lagging: 'bg-warning',
  stale: 'bg-danger',
  never: 'bg-muted-foreground/40',
}
const HEALTH_LABEL: Record<BranchHealth, string> = {
  ok: 'متزامن',
  lagging: 'متأخر في المزامنة',
  stale: 'منقطع عن المزامنة',
  never: 'لم يتصل بعد',
}

// No health yet (HQ data still loading) renders the neutral "never" gray.
export function HealthDot({
  health,
  className,
}: {
  health?: BranchHealth
  className?: string
}) {
  return (
    <span
      title={health ? HEALTH_LABEL[health] : undefined}
      className={cn(
        'size-2.5 shrink-0 rounded-full',
        health ? HEALTH_DOT[health] : 'bg-muted-foreground/40',
        className,
      )}
    />
  )
}
