import { useEffect } from 'react'
import { Navigate, useParams } from 'react-router-dom'
import { toast } from 'sonner'
import { useBundle } from '@/lib/hooks'
import { can, useScope, type PermCode } from '@/lib/perm'
import { RouteLoader } from '@/components/RouteLoader'

/**
 * Route guard (T111): wraps a route element and redirects to Overview,
 * with a toast, when the member's scope lacks `code`. Sits below
 * `SetupGate`, so by the time this mounts the bundle (and therefore the
 * scope) is normally already cached — `RouteLoader` only covers the rare
 * case of landing here before that fetch resolves. Denying here means the
 * guarded page's own element never mounts, so it never fires its own
 * queries — a direct URL hit costs zero requests for a denied section.
 */
export function RequirePerm({ code, children }: { code: PermCode; children: React.ReactNode }) {
  const { tenantId } = useParams<'tenantId'>()
  const { isLoading } = useBundle(tenantId)
  const scope = useScope(tenantId)
  const allowed = can(scope, code)

  useEffect(() => {
    if (!isLoading && !allowed) {
      toast.error('لا تملك صلاحية الوصول إلى هذا القسم')
    }
  }, [isLoading, allowed])

  if (isLoading) return <RouteLoader />
  if (!allowed) return <Navigate to={`/tenants/${tenantId}`} replace />
  return <>{children}</>
}
