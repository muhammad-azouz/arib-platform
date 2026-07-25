import { cn } from '@/lib/utils'
import logo from '@/assets/images/logo.svg'

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
      <div className="leading-tight">
        <img src={logo} alt="" width={250} />
        <div className="text-[16px] text-muted-foreground text-center">{subtitle}</div>
      </div>
    </div>
  )
}
