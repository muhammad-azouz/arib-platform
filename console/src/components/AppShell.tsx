import { NavLink, Outlet, useLocation, useParams } from 'react-router-dom'
import { cn } from '@/lib/utils'
import { useBundle, useTenantEvents } from '@/lib/hooks'
import { can, PERM, useScope } from '@/lib/perm'
import { Brand } from '@/components/Brand'
import { Breadcrumbs } from '@/components/Breadcrumbs'
import { AccountMenu } from '@/components/AccountMenu'
import { NotificationsBell } from '@/components/NotificationsBell'
import { BranchStatusIndicator } from '@/components/BranchStatusIndicator'
import { CommandPalette } from '@/components/CommandPalette'
import {
  DashboardIcon,
  CompanyIcon,
  BranchIcon,
  CatalogIcon,
  InventoryIcon,
  UsersIcon,
  SupplierIcon,
  OrdersIcon,
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
  // The D3 `view` code gating this section's nav entry. Absent means always
  // visible (spec D3: نظرة عامة/تنزيل التطبيق/الإعدادات need no code, and
  // النشاط التجاري has no `view` code at all — company.manage gates only
  // the edit form (T115), never the page, so a member without it still
  // reaches the read-only company profile).
  code?: string
}

export function AppShell() {
  const { tenantId } = useParams<'tenantId'>()
  const { pathname } = useLocation()
  const base = `/tenants/${tenantId}`

  // Live branch events for every console page under this shell.
  useTenantEvents(tenantId)

  // Single scope read for this render; every nav item below is checked
  // against it via the plain `can()` function rather than calling a hook
  // per item.
  const scope = useScope(tenantId)

  // T123 — the persistent scope banner. `useBundle` is already fetched by
  // `SetupGate` above this shell (same tenant id, same query key), so this
  // costs no extra request; it's read here only for branch *names* to put
  // next to the ids the scope already carries.
  const { data: bundle } = useBundle(tenantId)
  const scopedBranchNames =
    scope && scope.branch_ids.length > 0
      ? scope.branch_ids
          .map((id) => bundle?.Branches?.find((b) => b.ID === id)?.Name)
          .filter((n): n is string => !!n)
      : []

  // Nav rule (spec D3): a nav item renders iff its `view` permission
  // resolves true — never a greyed-out entry that 403s on click.
  const nav: NavItem[] = [
    { to: base, label: 'نظرة عامة', icon: DashboardIcon, end: true },
    { to: `${base}/branches`, label: 'الفروع', icon: BranchIcon, code: PERM.BranchesView },
    { to: `${base}/catalog`, label: 'الكتالوج', icon: CatalogIcon, code: PERM.CatalogView },
    { to: `${base}/inventory`, label: 'المخزون', icon: InventoryIcon, code: PERM.InventoryView },
    { to: `${base}/customers`, label: 'العملاء', icon: UsersIcon, code: PERM.CustomersView },
    { to: `${base}/suppliers`, label: 'الموردون', icon: SupplierIcon, code: PERM.SuppliersView },
    { to: `${base}/orders`, label: 'الطلبات', icon: OrdersIcon, code: PERM.OrdersView },
    { to: `${base}/reports`, label: 'التقارير', icon: ReportsIcon, code: PERM.ReportsView },
    { to: `${base}/company`, label: 'النشاط التجاري', icon: CompanyIcon },
    { to: `${base}/download`, label: 'تنزيل التطبيق', icon: DownloadIcon },
    { to: `${base}/settings`, label: 'الإعدادات', icon: SettingsIcon },
  ].filter((item) => !item.code || can(scope, item.code))

  // Reachable only via deep-link (the notifications bell / Overview alerts) —
  // deliberately absent from `nav` so it has no sidebar entry, but still
  // needs a breadcrumb label.
  const hiddenRoutes: Pick<NavItem, 'to' | 'label'>[] = [
    { to: `${base}/conflicts`, label: 'التنبيهات والتعارضات' },
  ]

  const current = [...nav, ...hiddenRoutes]
    .sort((a, b) => b.to.length - a.to.length)
    .find((n) => pathname.startsWith(n.to))

  // The shell is a fixed-height frame, not a growing document: the box wrapping
  // `<main>` below is the only scroll container. That is what lets a page hand
  // its own remaining height to a child (the catalog table) instead of pushing
  // the whole window past the viewport — and why the sidebar and header no
  // longer need `sticky`.
  return (
    <div className="flex h-screen overflow-hidden">
      {/* Sidebar (right side in RTL) */}
      <aside className="hidden h-full w-64 shrink-0 flex-col overflow-y-auto border-e border-border bg-card/50 p-4 md:flex">
        <Brand className="px-2 py-3 w-25 h-10" />

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
        <header className="z-20 flex h-14 shrink-0 items-center justify-between gap-4 border-b border-border bg-background/80 px-5 backdrop-blur-md">
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
            <CommandPalette />
            <BranchStatusIndicator />
            <NotificationsBell />
            <AccountMenu />
          </div>
        </header>

        {/* T123 — quiet persistent scope indicator (spec D4/D5): a scoped
            member sees this on every screen so a branch-filtered number is
            never mistaken for a company total. Lives outside the scroll
            container so it never scrolls away, and renders nothing at all
            for an unscoped member (owner or unscoped role) — no behaviour
            change for that case. */}
        {scopedBranchNames.length > 0 && (
          <div className="shrink-0 border-b border-info/30 bg-info/10 px-5 py-2 text-xs text-info">
            أنت ترى بيانات {scopedBranchNames.length === 1 ? 'فرع' : 'فروع'} محددة فقط:{' '}
            <span className="font-medium">{scopedBranchNames.join('، ')}</span>
          </div>
        )}

        {/* The scroll container is this full-width box rather than `<main>`, so
            the scrollbar rides the window edge instead of the centered column's.
            It also owns the vertical padding: a scroll container's end padding
            counts as scrollable overflow, whereas `py-*` on the `h-full` main
            below would land mid-content once a page outgrows the viewport. */}
        <div className="min-h-0 flex-1 overflow-y-auto py-7">
          {/* `h-full` here — not `min-h-full` — is what makes the height
              *definite*, so a page can resolve `h-full` against it and hand the
              leftover to a scrolling child. It stays a plain block, so pages
              that outgrow it simply overflow and scroll as before. */}
          <main className="mx-auto h-full w-full max-w-6xl px-5">
            <div className="animate-rise h-full">
              <Outlet />
            </div>
          </main>
        </div>
      </div>
    </div>
  )
}
