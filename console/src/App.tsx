import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'
import { useAuth } from '@/lib/auth'
import { RouteLoader } from '@/components/RouteLoader'
import { AppShell } from '@/components/AppShell'
import { SetupGate } from '@/routes/SetupGate'
import { Login } from '@/pages/Login'
import { Home } from '@/pages/Home'
import { Tenants } from '@/pages/Tenants'
import { Placeholder } from '@/pages/Placeholder'
import { Billing } from '@/pages/Billing'
import { SetupWizard } from '@/pages/setup/SetupWizard'
import { Overview } from '@/pages/console/Overview'
import { Company } from '@/pages/console/Company'
import { Branches } from '@/pages/console/Branches'
import { BranchDetail } from '@/pages/console/BranchDetail'
import { Catalog } from '@/pages/console/Catalog'
import { ProductDetail } from '@/pages/console/ProductDetail'
import { Inventory } from '@/pages/console/Inventory'
import { Customers } from '@/pages/console/Customers'
import { CustomerDetail } from '@/pages/console/CustomerDetail'
import { Suppliers } from '@/pages/console/Suppliers'
import { SupplierDetail } from '@/pages/console/SupplierDetail'
import { Conflicts } from '@/pages/console/Conflicts'
import { Reports } from '@/pages/console/Reports'
import { Download } from '@/pages/console/Download'
import { Settings } from '@/pages/console/Settings'
import { AccountIcon, HelpIcon } from '@/components/icon'

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
            <Route path="branches/:branchId" element={<BranchDetail />} />
            <Route path="catalog" element={<Catalog />} />
            <Route path="catalog/:productId" element={<ProductDetail />} />
            <Route path="inventory" element={<Inventory />} />
            <Route path="customers" element={<Customers />} />
            <Route path="customers/:customerId" element={<CustomerDetail />} />
            <Route path="suppliers" element={<Suppliers />} />
            <Route path="suppliers/:supplierId" element={<SupplierDetail />} />
            <Route path="conflicts" element={<Conflicts />} />
            <Route path="reports" element={<Reports />} />
            <Route path="download" element={<Download />} />
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
        <Route path="billing" element={<Billing />} />
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
