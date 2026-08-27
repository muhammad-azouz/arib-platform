import { Link, useParams } from 'react-router-dom'
import { useConflicts, useHqBranches, useInventoryAttention, useSubscription } from '@/lib/hooks'
import { deriveAlerts } from '@/lib/alerts'
import { can, PERM, useScope } from '@/lib/perm'
import { toArabicDigits } from '@/lib/format'
import { BellIcon, DangerIcon, InfoIcon, SuccessIcon } from '@/components/icon'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'

/**
 * Bell + count badge in the AppShell header. Mounts the same three queries as
 * Overview's alerts panel (all cached/shared keys, SSE-live) and feeds them
 * through the same `deriveAlerts`, so the badge count always equals the
 * Overview panel's row count by construction — including which sections are
 * gated off, since both read the same `useScope`/`can` (T114, spec D10):
 * the unread count can never include a conflict or stock alert the member
 * has no permission to open.
 */
export function NotificationsBell() {
  const { tenantId } = useParams<'tenantId'>()
  const scope = useScope(tenantId)
  const canBranches = can(scope, PERM.BranchesView)
  const canInventory = can(scope, PERM.InventoryView)
  const canConflicts = can(scope, PERM.ConflictsView)

  const hqQuery = useHqBranches(canBranches ? tenantId : undefined)
  const attentionQuery = useInventoryAttention(canInventory ? tenantId : undefined, {})
  const conflictsQuery = useConflicts(canConflicts ? tenantId : undefined, {})
  const { data: subscription } = useSubscription(tenantId)

  if (!tenantId) return null

  const alerts = deriveAlerts(tenantId, {
    branches: canBranches ? hqQuery.data?.branches : undefined,
    attention: canInventory ? attentionQuery.data?.data.counts : undefined,
    conflictsUnacked: canConflicts ? conflictsQuery.data?.data.unacked : undefined,
    subscription: subscription?.summary,
  })
  const hasConflictAlert = alerts.some((a) => a.key === 'conflicts')
  const badgeLabel = alerts.length > 9 ? '٩+' : toArabicDigits(alerts.length)

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon" className="relative" aria-label="التنبيهات">
          <BellIcon className="size-5" />
          {alerts.length > 0 && (
            <span className="absolute -top-0.5 -end-0.5 grid min-w-[1.1rem] place-items-center rounded-full bg-danger px-1 text-[10px] font-bold leading-none text-white">
              {badgeLabel}
            </span>
          )}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-80">
        <DropdownMenuLabel>التنبيهات</DropdownMenuLabel>
        {alerts.length === 0 ? (
          <div className="flex items-center gap-2.5 px-2 py-3 text-sm text-muted-foreground">
            <SuccessIcon className="size-5 shrink-0 text-success" />
            لا توجد تنبيهات
          </div>
        ) : (
          <div className="max-h-80 overflow-y-auto">
            {alerts.map((a) => (
              <DropdownMenuItem key={a.key} asChild className="items-start whitespace-normal py-2">
                <Link to={a.to}>
                  {a.tone === 'danger' ? (
                    <DangerIcon className="mt-0.5 size-4 shrink-0 text-danger" />
                  ) : (
                    <InfoIcon className="mt-0.5 size-4 shrink-0 text-info" />
                  )}
                  <span className="min-w-0 flex-1 text-start">{a.text}</span>
                </Link>
              </DropdownMenuItem>
            ))}
          </div>
        )}
        {hasConflictAlert && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuItem asChild>
              <Link to={`/tenants/${tenantId}/conflicts`} className="justify-center text-primary">
                عرض كل التعارضات
              </Link>
            </DropdownMenuItem>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
