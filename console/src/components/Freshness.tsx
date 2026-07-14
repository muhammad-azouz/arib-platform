import { Badge } from '@/components/ui/badge'
import { relative } from '@/lib/format'
import type { FreshnessSource } from '@/lib/types'
import { cn } from '@/lib/utils'

/**
 * The freshness pill — the console's one way of saying how current
 * branch-derived data is (spec: propagation honesty). Renders the envelope's
 * source + as_of:
 *
 *   live    → "مباشر" (pulsing dot; future SignalR tier)
 *   synced  → "تمت المزامنة منذ …"
 *   offline → "غير متصل · آخر البيانات منذ …" — stale data stays visible,
 *              labeled, never blanked.
 */
export function Freshness({
  source,
  asOf,
  className,
}: {
  source: FreshnessSource
  asOf?: string | null
  className?: string
}) {
  if (source === 'live') {
    return (
      <Badge tone="success" className={className}>
        <span className="relative flex size-2">
          <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-success opacity-75" />
          <span className="relative inline-flex size-2 rounded-full bg-success" />
        </span>
        مباشر
      </Badge>
    )
  }

  if (source === 'synced') {
    return (
      <Badge tone="info" className={className}>
        <span className="inline-flex size-2 rounded-full bg-info" />
        {asOf ? `تمت المزامنة ${relative(asOf)}` : 'تمت المزامنة'}
      </Badge>
    )
  }

  return (
    <Badge tone="warning" className={cn('font-normal', className)}>
      <span className="inline-flex size-2 rounded-full bg-warning" />
      {asOf ? `غير متصل · آخر البيانات ${relative(asOf)}` : 'لم تتم المزامنة بعد'}
    </Badge>
  )
}
