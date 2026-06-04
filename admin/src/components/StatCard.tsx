import type { LucideIcon } from 'lucide-react'
import { cn } from '@/lib/utils'

interface StatCardProps {
  label: string
  value: number | string
  icon: LucideIcon
  hint?: string
  accent?: boolean
  className?: string
  style?: React.CSSProperties
}

export function StatCard({
  label,
  value,
  icon: Icon,
  hint,
  accent,
  className,
  style,
}: StatCardProps) {
  return (
    <div
      style={style}
      className={cn(
        'animate-rise relative overflow-hidden rounded-lg border border-border bg-card p-5',
        accent && 'border-primary/30',
        className,
      )}
    >
      <div className="flex items-start justify-between">
        <span className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
          {label}
        </span>
        <Icon className={cn('size-4', accent ? 'text-primary' : 'text-muted-foreground')} />
      </div>
      <div className="mt-3 font-display text-3xl font-semibold tabular-nums">
        {value}
      </div>
      {hint && <div className="mt-1 text-xs text-muted-foreground">{hint}</div>}
      {accent && (
        <div className="pointer-events-none absolute inset-x-0 -top-px h-px bg-gradient-to-r from-transparent via-primary/60 to-transparent" />
      )}
    </div>
  )
}
