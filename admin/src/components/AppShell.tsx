import { NavLink, Outlet } from 'react-router-dom'
import {
  LayoutGrid,
  LogOut,
  ScrollText,
  Users,
  type LucideIcon,
} from 'lucide-react'
import { useAuth } from '@/lib/auth'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'

interface NavItem {
  to: string
  label: string
  icon: LucideIcon
  end?: boolean
}

const NAV: NavItem[] = [
  { to: '/', label: 'Overview', icon: LayoutGrid, end: true },
  { to: '/clients', label: 'Clients', icon: Users },
  { to: '/audit', label: 'Audit log', icon: ScrollText },
]

export function AppShell() {
  const { email, logout } = useAuth()
  const initials = (email ?? '?').slice(0, 2).toUpperCase()

  return (
    <div className="flex min-h-screen">
      {/* Sidebar */}
      <aside className="sticky top-0 hidden h-screen w-60 shrink-0 flex-col border-r border-border bg-card/40 p-4 md:flex">
        <div className="flex items-center gap-2.5 px-2 py-3">
          <div className="grid size-8 place-items-center rounded-md bg-primary text-primary-foreground shadow-[0_0_18px_-2px_rgba(245,165,36,0.7)]">
            <span className="font-display text-sm font-bold">A</span>
          </div>
          <div className="leading-tight">
            <div className="font-display text-sm font-semibold">Arib</div>
            <div className="text-[10px] uppercase tracking-[0.18em] text-muted-foreground">
              License Control
            </div>
          </div>
        </div>

        <nav className="mt-6 flex flex-col gap-1">
          {NAV.map(({ to, label, icon: Icon, end }) => (
            <NavLink
              key={to}
              to={to}
              end={end}
              className={({ isActive }) =>
                cn(
                  'group flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-colors',
                  isActive
                    ? 'bg-primary/10 text-primary'
                    : 'text-muted-foreground hover:bg-accent hover:text-foreground',
                )
              }
            >
              {({ isActive }) => (
                <>
                  <Icon className="size-4" />
                  {label}
                  {isActive && (
                    <span className="ml-auto h-4 w-1 rounded-full bg-primary" />
                  )}
                </>
              )}
            </NavLink>
          ))}
        </nav>

        <div className="mt-auto px-2 text-[10px] uppercase tracking-wider text-muted-foreground/60">
          v1 · operator console
        </div>
      </aside>

      {/* Main */}
      <div className="flex min-w-0 flex-1 flex-col">
        <header className="sticky top-0 z-20 flex h-14 items-center justify-between gap-4 border-b border-border bg-background/80 px-5 backdrop-blur-md">
          {/* mobile nav */}
          <nav className="flex items-center gap-1 md:hidden">
            {NAV.map(({ to, label, icon: Icon, end }) => (
              <NavLink
                key={to}
                to={to}
                end={end}
                className={({ isActive }) =>
                  cn(
                    'flex items-center gap-1.5 rounded-md px-2.5 py-1.5 text-xs',
                    isActive
                      ? 'bg-primary/10 text-primary'
                      : 'text-muted-foreground',
                  )
                }
              >
                <Icon className="size-3.5" />
                <span className="sr-only sm:not-sr-only">{label}</span>
              </NavLink>
            ))}
          </nav>
          <div className="hidden md:block" />

          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="ghost" className="h-9 gap-2 px-2">
                <span className="grid size-7 place-items-center rounded-full bg-secondary text-xs font-semibold text-secondary-foreground">
                  {initials}
                </span>
                <span className="hidden max-w-[14rem] truncate text-sm text-muted-foreground sm:inline">
                  {email}
                </span>
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-56">
              <DropdownMenuLabel>Signed in</DropdownMenuLabel>
              <div className="truncate px-2 pb-1.5 font-mono text-xs text-foreground/80">
                {email}
              </div>
              <DropdownMenuSeparator />
              <DropdownMenuItem variant="destructive" onSelect={() => void logout()}>
                <LogOut className="size-4" />
                Sign out
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </header>

        <main className="mx-auto w-full max-w-6xl flex-1 px-5 py-7">
          <Outlet />
        </main>
      </div>
    </div>
  )
}
