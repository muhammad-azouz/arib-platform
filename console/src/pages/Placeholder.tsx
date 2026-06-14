import { Link } from 'react-router-dom'
import { Brand } from '@/components/Brand'
import { EmptyState } from '@/components/States'
import { ArrowTrailing, type IconComponent } from '@/components/icon'

/**
 * Standalone account-level page frame (reached from the Home tiles, outside the
 * console shell). Phase 0 ships these as honest "coming soon" placeholders.
 */
export function Placeholder({
  icon,
  title,
  description,
}: {
  icon: IconComponent
  title: string
  description: string
}) {
  return (
    <div className="min-h-screen">
      <header className="flex h-16 items-center justify-between px-5 sm:px-8">
        <Brand />
        <Link
          to="/"
          className="flex items-center gap-1.5 text-sm text-muted-foreground transition-colors hover:text-foreground"
        >
          <ArrowTrailing className="size-4" />
          الرئيسية
        </Link>
      </header>
      <main className="mx-auto w-full max-w-2xl px-5 py-16">
        <EmptyState
          icon={icon}
          title={title}
          description={description}
          className="animate-rise"
        />
      </main>
    </div>
  )
}
