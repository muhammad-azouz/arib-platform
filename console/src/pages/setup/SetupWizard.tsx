import { Link, useParams } from 'react-router-dom'
import { useBundle } from '@/lib/hooks'
import { TopBar } from '@/components/TopBar'
import { Stepper, type Step } from '@/components/Stepper'
import { CompanyForm } from '@/components/CompanyForm'
import { BranchForm } from '@/components/BranchForm'
import { CompanyIcon, BranchIcon } from '@/components/icon'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card'

const STEPS: Step[] = [
  { key: 'company', label: 'النشاط التجاري' },
  { key: 'branch', label: 'الفرع الأول' },
]

/**
 * Setup wizard shell. The completion gate only routes here while the tenant is
 * incomplete, so we derive the active step from the bundle: no company → step 1,
 * company but no branch → step 2. Each form patches the cached bundle on save;
 * registering the first branch makes the bundle complete, so the parent
 * SetupGate (mode="setup") re-reads it and routes the user out to the dashboard.
 */
export function SetupWizard() {
  const { tenantId } = useParams<'tenantId'>()
  const { data: bundle } = useBundle(tenantId)

  // The gate guarantees the bundle is loaded before rendering this route.
  if (!bundle) return null

  const company = bundle.Company
  const current = company != null ? 1 : 0

  return (
    <div className="min-h-screen">
      <TopBar subtitle="إعداد النشاط" />

      <main className="mx-auto w-full max-w-2xl px-5 py-10 sm:py-14">
        <div className="animate-rise space-y-8">
          <div>
            <h1 className="font-display text-2xl font-extrabold tracking-tight">
              إعداد {bundle.Tenant.Name}
            </h1>
            <p className="mt-1 text-sm text-muted-foreground">
              خطوتان لتجهيز نشاطك: تسجيل بيانات الشركة ثم إضافة أول فرع.
            </p>
          </div>

          <Stepper steps={STEPS} current={current} />

          {current === 0 ? (
            <Card>
              <CardHeader>
                <CardTitle className="flex items-center gap-2">
                  <CompanyIcon className="size-5 text-primary" />
                  بيانات النشاط التجاري
                </CardTitle>
                <CardDescription>
                  سجّل بيانات شركتك. يمكنك تعديلها لاحقًا من إعدادات النشاط.
                </CardDescription>
              </CardHeader>
              <CardContent>
                {/* On save the bundle gains a Company → this wizard re-renders at
                    the branch step automatically (current becomes 1). */}
                <CompanyForm
                  tenantId={bundle.Tenant.ID}
                  company={null}
                  submitLabel="حفظ ومتابعة"
                />
              </CardContent>
            </Card>
          ) : (
            <Card>
              <CardHeader>
                <CardTitle className="flex items-center gap-2">
                  <BranchIcon className="size-5 text-primary" />
                  إضافة أول فرع
                </CardTitle>
                <CardDescription>
                  أنشئ فرعك الأول لإكمال الإعداد والانتقال إلى لوحة التحكم.
                </CardDescription>
              </CardHeader>
              <CardContent>
                {/* Creating the branch completes the bundle → the SetupGate above
                    redirects out to the tenant dashboard. */}
                <BranchForm
                  tenantId={bundle.Tenant.ID}
                  companyId={company!.ID}
                  submitLabel="إنشاء الفرع وإنهاء الإعداد"
                />
              </CardContent>
            </Card>
          )}

          <div className="text-center">
            <Button asChild variant="ghost" size="sm">
              <Link to="/tenants">حفظ والعودة لاحقًا</Link>
            </Button>
          </div>
        </div>
      </main>
    </div>
  )
}
