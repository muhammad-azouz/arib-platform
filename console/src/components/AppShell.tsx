import { NavLink, Outlet, useLocation, useParams } from 'react-router-dom'
import { cn } from '@/lib/utils'
import { useTenantEvents } from '@/lib/hooks'
import { Brand } from '@/components/Brand'
import { Breadcrumbs } from '@/components/Breadcrumbs'
import { AccountMenu } from '@/components/AccountMenu'
import { NotificationsBell } from '@/components/NotificationsBell'
import {
  DashboardIcon,
  CompanyIcon,
  BranchIcon,
  CatalogIcon,
  InventoryIcon,
  ReportsIcon,
  DownloadIcon,
  SettingsIcon,
  TenantIcon,
  type IconComponent,
} from '@/components/icon'

interface NavItem {
  to: string
  label: string
  icon: IconComponent
  end?: boolean
}

export function AppShell() {
  const { tenantId } = useParams<'tenantId'>()
  const { pathname } = useLocation()
  const base = `/tenants/${tenantId}`

  // Live branch events for every console page under this shell.
  useTenantEvents(tenantId)

  const nav: NavItem[] = [
    { to: base, label: 'نظرة عامة', icon: DashboardIcon, end: true },
    { to: `${base}/branches`, label: 'الفروع', icon: BranchIcon },
    { to: `${base}/catalog`, label: 'الكتالوج', icon: CatalogIcon },
    { to: `${base}/inventory`, label: 'المخزون', icon: InventoryIcon },
    { to: `${base}/reports`, label: 'التقارير', icon: ReportsIcon },
    { to: `${base}/company`, label: 'النشاط التجاري', icon: CompanyIcon },
    { to: `${base}/download`, label: 'تنزيل التطبيق', icon: DownloadIcon },
    { to: `${base}/settings`, label: 'الإعدادات', icon: SettingsIcon },
  ]

  const current = [...nav]
    .sort((a, b) => b.to.length - a.to.length)
    .find((n) => (n.end ? pathname === n.to : pathname.startsWith(n.to)))

  return (
    <div className="flex min-h-screen">
      {/* Sidebar (right side in RTL) */}
      <aside className="sticky top-0 hidden h-screen w-64 shrink-0 flex-col border-e border-border bg-card/50 p-4 md:flex">
        <Brand className="px-2 py-3" />

        <nav className="mt-6 flex flex-col gap-1">
          {nav.map(({ to, label, icon: IconCmp, end }) => (
            <NavLink
              key={to}
              to={to}
              end={end}
              className={({ isActive }) =>
                cn(
                  'group flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm font-medium transition-colors',
                  isActive
                    ? 'bg-primary/10 text-primary'
                    : 'text-muted-foreground hover:bg-accent hover:text-foreground',
                )
              }
            >
              {({ isActive }) => (
                <>
                  <IconCmp className="size-5" />
                  {label}
                  {isActive && (
                    <span className="ms-auto h-4 w-1 rounded-full bg-primary" />
                  )}
                </>
              )}
            </NavLink>
          ))}
        </nav>

        <NavLink
          to="/tenants"
          className="mt-auto flex items-center gap-2 rounded-lg px-3 py-2.5 text-sm text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
        >
          <TenantIcon className="size-5" />
          كل الأنشطة
        </NavLink>
      </aside>

      {/* Main */}
      <div className="flex min-w-0 flex-1 flex-col">
        <header className="sticky top-0 z-20 flex h-14 items-center justify-between gap-4 border-b border-border bg-background/80 px-5 backdrop-blur-md">
          {/* mobile nav */}
          <nav className="flex items-center gap-1 md:hidden">
            {nav.map(({ to, label, icon: IconCmp, end }) => (
              <NavLink
                key={to}
                to={to}
                end={end}
                aria-label={label}
                className={({ isActive }) =>
                  cn(
                    'flex items-center gap-1.5 rounded-md px-2.5 py-1.5 text-xs',
                    isActive ? 'bg-primary/10 text-primary' : 'text-muted-foreground',
                  )
                }
              >
                <IconCmp className="size-5" />
              </NavLink>
            ))}
          </nav>
          <div className="hidden md:block">
            <Breadcrumbs
              items={[
                { label: 'الرئيسية', to: '/' },
                { label: 'أنشطتي', to: '/tenants' },
                { label: current?.label ?? 'لوحة التحكم' },
              ]}
            />
          </div>

          <div className="flex items-center gap-1">
            <NotificationsBell />
            <AccountMenu />
          </div>
        </header>

        <main className="mx-auto w-full max-w-6xl flex-1 px-5 py-7">
          <div className="animate-rise">
            <Outlet />
          </div>
        </main>
      </div>
    </div>
  )
}
