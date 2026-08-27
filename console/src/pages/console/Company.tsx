import { useParams } from 'react-router-dom'
import { useBundle } from '@/lib/hooks'
import { PERM, useCanUnscoped } from '@/lib/perm'
import { PageHeader } from '@/components/PageHeader'
import { LoadingState } from '@/components/States'
import { CompanyForm } from '@/components/CompanyForm'
import { Card, CardContent } from '@/components/ui/card'

export function Company() {
  const { tenantId } = useParams<'tenantId'>()
  const { data: bundle } = useBundle(tenantId)
  // PUT /company is D5c-unscoped (company-wide), so the form is read-only
  // for a scoped member even when they hold company.manage — same "hide
  // the button, don't let the 403 be the surprise" rule as Catalog/Branches.
  const canManage = useCanUnscoped(tenantId, PERM.CompanyManage)

  // The app gate guarantees a company exists before this page renders.
  if (!bundle) return <LoadingState />

  return (
    <>
      <PageHeader
        title="النشاط التجاري"
        description="بيانات شركتك (نشاط واحد لكل حساب)."
      />
      <Card className="max-w-2xl">
        <CardContent className="pt-5">
          <CompanyForm
            tenantId={bundle.Tenant.ID}
            company={bundle.Company}
            submitLabel="حفظ التغييرات"
            readOnly={!canManage}
          />
        </CardContent>
      </Card>
    </>
  )
}
