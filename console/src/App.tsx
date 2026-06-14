import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'
import { useAuth } from '@/lib/auth'
import { RouteLoader } from '@/components/RouteLoader'
import { AppShell } from '@/components/AppShell'
import { SetupGate } from '@/routes/SetupGate'
import { Login } from '@/pages/Login'
import { Home } from '@/pages/Home'
import { Tenants } from '@/pages/Tenants'
import { Placeholder } from '@/pages/Placeholder'
import { SetupWizard } from '@/pages/setup/SetupWizard'
import { Overview } from '@/pages/console/Overview'
import { Company } from '@/pages/console/Company'
import { Branches } from '@/pages/console/Branches'
import { Settings } from '@/pages/console/Settings'
import { AccountIcon, BillingIcon, HelpIcon } from '@/components/icon'

export default function App() {
  const { status } = useAuth()

  if (status === 'loading') return <RouteLoader label="جارٍ استعادة الجلسة…" />

  if (status !== 'authed') {
    return (
      <BrowserRouter>
        <Routes>
          <Route path="*" element={<Login />} />
        </Routes>
      </BrowserRouter>
    )
  }

  return (
    <BrowserRouter>
      <Routes>
        {/* Home tile launcher */}
        <Route index element={<Home />} />

        {/* Tenant resolver: zero → create, one → enter, many → pick */}
        <Route path="tenants" element={<Tenants />} />

        {/* Setup wizard — only reachable while the tenant is incomplete */}
        <Route path="tenants/:tenantId/setup" element={<SetupGate mode="setup" />}>
          <Route index element={<SetupWizard />} />
        </Route>

        {/* Tenant console — gated: incomplete tenants are bounced to /setup */}
        <Route path="tenants/:tenantId" element={<SetupGate mode="app" />}>
          <Route element={<AppShell />}>
            <Route index element={<Overview />} />
            <Route path="company" element={<Company />} />
            <Route path="branches" element={<Branches />} />
            <Route path="settings" element={<Settings />} />
          </Route>
        </Route>

        {/* Account-level standalone pages (Phase 0 placeholders) */}
        <Route
          path="account"
          element={
            <Placeholder
              icon={AccountIcon}
              title="حسابي"
              description="إدارة بياناتك الشخصية ستتوفر قريبًا."
            />
          }
        />
        <Route
          path="billing"
          element={
            <Placeholder
              icon={BillingIcon}
              title="الفوترة"
              description="تفاصيل الاشتراك والفواتير ستتوفر قريبًا."
            />
          }
        />
        <Route
          path="help"
          element={
            <Placeholder
              icon={HelpIcon}
              title="المساعدة"
              description="مركز المساعدة والدعم سيتوفر قريبًا."
            />
          }
        />

        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </BrowserRouter>
  )
}
