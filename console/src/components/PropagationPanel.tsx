import { relative } from '@/lib/format'
import { cn } from '@/lib/utils'
import type { BranchView } from '@/lib/types'
import { RefreshIcon } from '@/components/icon'
import { Badge } from '@/components/ui/badge'

/**
 * One chip per active branch: «وصل ✓» once that branch's live `last_sync_at`
 * (already streamed via SSE — see `useHqBranches`) reaches or passes
 * `writtenAt`, otherwise the honest pending state — including a stale
 * branch's last-known sync time, never hidden. Shared by any HQ write that
 * needs to show propagation (price changes, product create).
 */
export function PropagationPanel({ writtenAt, branches }: { writtenAt: string; branches: BranchView[] }) {
  const written = new Date(writtenAt)
  if (branches.length === 0) return null
  return (
    <div className="rounded-xl border border-border bg-card/50 p-4">
      <h3 className="mb-3 flex items-center gap-2 text-sm font-medium">
        <RefreshIcon className="size-4 text-primary" />
        وصول التعديل للفروع
      </h3>
      <div className="flex flex-wrap gap-2">
        {branches.map((b) => {
          const arrived = !!b.last_sync_at && new Date(b.last_sync_at) >= written
          return (
            <Badge key={b.id} tone={arrived ? 'success' : 'warning'} className="font-normal">
              <span
                className={cn('inline-flex size-2 rounded-full', arrived ? 'bg-success' : 'bg-warning')}
              />
              {b.name} · {arrived ? 'وصل ✓' : 'في الانتظار — يصل خلال ~٥ دقائق'}
              {!arrived &&
                (b.last_sync_at ? ` (آخر مزامنة ${relative(b.last_sync_at)})` : ' (لم تتم المزامنة بعد)')}
            </Badge>
          )
        })}
      </div>
    </div>
  )
}
