import { Link, Navigate, Outlet, useParams } from 'react-router-dom'
import { ApiError } from '@/lib/api'
import { bundleIsComplete, useBundle } from '@/lib/hooks'
import { RouteLoader } from '@/components/RouteLoader'
import { ErrorState } from '@/components/States'
import { Button } from '@/components/ui/button'

/**
 * Setup-completion gate, scoped to the tenant in the URL. It loads the bundle
 * once (shared cache) and enforces the onboarding flow on every load and route
 * change:
 *
 *   mode="app"   — an incomplete tenant is bounced into /setup.
 *   mode="setup" — a completed tenant is bounced out to its overview.
 *
 * Scoping to `:tenantId` means an account with several tenants isn't globally
 * blocked because one of them is half-configured.
 */
export function SetupGate({ mode }: { mode: 'app' | 'setup' }) {
  const { tenantId } = useParams<'tenantId'>()
  const { data, isLoading, isError, error, refetch } = useBundle(tenantId)

  if (!tenantId) return <Navigate to="/tenants" replace />
  if (isLoading) return <RouteLoader />

  if (isError || !data) {
    const status = error instanceof ApiError ? error.status : 0
    const denied = status === 403 || status === 404
    return (
      <div className="grid min-h-screen place-items-center px-4">
        <div className="w-full max-w-md">
          <ErrorState
            title={denied ? 'النشاط غير متاح' : 'تعذّر تحميل النشاط'}
            message={
              denied
                ? 'هذا النشاط غير موجود أو لا يخص حسابك.'
                : error instanceof Error
                  ? error.message
                  : undefined
            }
            onRetry={denied ? undefined : () => void refetch()}
          />
          <div className="mt-4 text-center">
            <Button asChild variant="ghost" size="sm">
              <Link to="/tenants">العودة إلى أنشطتي</Link>
            </Button>
          </div>
        </div>
      </div>
    )
  }

  const complete = bundleIsComplete(data)
  if (mode === 'app' && !complete) {
    return <Navigate to={`/tenants/${tenantId}/setup`} replace />
  }
  if (mode === 'setup' && complete) {
    return <Navigate to={`/tenants/${tenantId}`} replace />
  }

  return <Outlet />
}
