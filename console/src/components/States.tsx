import type { ReactNode } from 'react'
import { DangerIcon, RefreshIcon, type IconComponent } from '@/components/icon'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { cn } from '@/lib/utils'

/** Centered loading shimmer for a panel or page region. */
export function LoadingState({
  rows = 3,
  className,
}: {
  rows?: number
  className?: string
}) {
  return (
    <div className={cn('space-y-3', className)} aria-busy="true">
      <Skeleton className="h-8 w-48" />
      {Array.from({ length: rows }).map((_, i) => (
        <Skeleton key={i} className="h-16 w-full" />
      ))}
    </div>
  )
}

/** Friendly empty placeholder with an optional primary action. */
export function EmptyState({
  icon: IconCmp,
  title,
  description,
  action,
  className,
}: {
  icon: IconComponent
  title: string
  description?: string
  action?: ReactNode
  className?: string
}) {
  return (
    <div
      className={cn(
        'flex flex-col items-center justify-center rounded-xl border border-dashed border-border bg-card/50 px-6 py-14 text-center',
        className,
      )}
    >
      <div className="mb-4 grid size-14 place-items-center rounded-2xl bg-accent text-primary">
        <IconCmp className="size-7" />
      </div>
      <h3 className="font-display text-lg font-bold">{title}</h3>
      {description && (
        <p className="mt-1.5 max-w-sm text-sm text-muted-foreground">{description}</p>
      )}
      {action && <div className="mt-5">{action}</div>}
    </div>
  )
}

/** Error panel with a retry affordance. */
export function ErrorState({
  title = 'تعذّر تحميل البيانات',
  message,
  onRetry,
  className,
}: {
  title?: string
  message?: string
  onRetry?: () => void
  className?: string
}) {
  return (
    <div
      className={cn(
        'flex flex-col items-center justify-center rounded-xl border border-danger/30 bg-danger/5 px-6 py-12 text-center',
        className,
      )}
      role="alert"
    >
      <div className="mb-3 grid size-12 place-items-center rounded-2xl bg-danger/10 text-danger">
        <DangerIcon className="size-6" />
      </div>
      <h3 className="font-display text-base font-bold text-danger">{title}</h3>
      {message && <p className="mt-1.5 max-w-sm text-sm text-muted-foreground">{message}</p>}
      {onRetry && (
        <Button variant="outline" size="sm" className="mt-4" onClick={onRetry}>
          <RefreshIcon className="size-4" />
          إعادة المحاولة
        </Button>
      )}
    </div>
  )
}
