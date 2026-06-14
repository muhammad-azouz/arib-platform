import { cn } from '@/lib/utils'

/** The Arib wordmark lockup — an amber tile with the glyph + the name. */
export function Brand({
  className,
  subtitle = 'لوحة التحكم',
}: {
  className?: string
  subtitle?: string
}) {
  return (
    <div className={cn('flex items-center gap-2.5', className)}>
      <div className="grid size-9 place-items-center rounded-xl bg-primary text-primary-foreground shadow-[0_2px_10px_-2px_rgba(245,165,36,0.6)]">
        <span className="font-display text-lg font-extrabold leading-none">أ</span>
      </div>
      <div className="leading-tight">
        <div className="font-display text-base font-bold">أريب</div>
        <div className="text-[11px] text-muted-foreground">{subtitle}</div>
      </div>
    </div>
  )
}
