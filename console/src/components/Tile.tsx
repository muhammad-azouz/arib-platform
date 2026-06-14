import { Link } from 'react-router-dom'
import { cn } from '@/lib/utils'
import type { IconComponent } from '@/components/icon'

/**
 * Home-screen launcher tile (the Dokaan-style hub pattern): a large tappable
 * card with an icon, label and short hint. `primary` highlights the main entry
 * (the console) with the brand amber.
 */
export function Tile({
  to,
  icon: IconCmp,
  label,
  hint,
  primary = false,
  disabled = false,
}: {
  to: string
  icon: IconComponent
  label: string
  hint?: string
  primary?: boolean
  disabled?: boolean
}) {
  const inner = (
    <div
      className={cn(
        'group relative flex h-full flex-col items-center gap-3 rounded-2xl border p-6 text-center transition-all',
        disabled
          ? 'cursor-not-allowed border-border bg-card/40 opacity-60'
          : 'border-border bg-card hover:-translate-y-0.5 hover:border-primary/40 hover:shadow-[0_10px_30px_-12px_rgba(245,165,36,0.45)]',
        primary && !disabled && 'border-primary/40 bg-primary/[0.06]',
      )}
    >
      <div
        className={cn(
          'grid size-16 place-items-center rounded-2xl transition-colors',
          primary
            ? 'bg-primary text-primary-foreground'
            : 'bg-accent text-primary group-hover:bg-primary/15',
        )}
      >
        <IconCmp className="size-8" />
      </div>
      <div>
        <div className="font-display text-base font-bold">{label}</div>
        {hint && <div className="mt-0.5 text-xs text-muted-foreground">{hint}</div>}
      </div>
    </div>
  )

  if (disabled) return <div className="h-full">{inner}</div>
  return (
    <Link to={to} className="h-full">
      {inner}
    </Link>
  )
}
