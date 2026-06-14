import { Fragment } from 'react'
import { Link } from 'react-router-dom'
import { cn } from '@/lib/utils'

export interface Crumb {
  label: string
  to?: string
}

/** RTL-aware breadcrumb trail; the last crumb is the current (non-link) page. */
export function Breadcrumbs({ items, className }: { items: Crumb[]; className?: string }) {
  return (
    <nav
      aria-label="مسار التنقل"
      className={cn('flex items-center gap-1.5 text-xs text-muted-foreground', className)}
    >
      {items.map((c, i) => {
        const last = i === items.length - 1
        return (
          <Fragment key={`${c.label}-${i}`}>
            {c.to && !last ? (
              <Link to={c.to} className="transition-colors hover:text-foreground">
                {c.label}
              </Link>
            ) : (
              <span className={cn(last && 'font-medium text-foreground')}>{c.label}</span>
            )}
            {!last && <span className="select-none text-muted-foreground/50">/</span>}
          </Fragment>
        )
      })}
    </nav>
  )
}
