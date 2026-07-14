import type { ReactNode } from 'react'
import { Link, useParams } from 'react-router-dom'
import { useBundle, useHqBranches } from '@/lib/hooks'
import { tenantStatusLabel, tenantStatusTone, toArabicDigits } from '@/lib/format'
import { cn } from '@/lib/utils'
import { PageHeader } from '@/components/PageHeader'
import { LoadingState } from '@/components/States'
import { Freshness } from '@/components/Freshness'
import { HealthDot } from '@/components/HealthDot'
import { Skeleton } from '@/components/ui/skeleton'
import {
  CompanyIcon,
  BranchIcon,
  WalletIcon,
  DatabaseIcon,
  DangerIcon,
  InfoIcon,
  DownloadIcon,
  PhoneIcon,
  ArrowLeading,
  type IconComponent,
} from '@/components/icon'
import { Badge } from '@/components/ui/badge'
import { Card } from '@/components/ui/card'
import { Button } from '@/components/ui/button'

const money = new Intl.NumberFormat('ar', { maximumFractionDigits: 2 })

export function Overview() {
  const { tenantId } = useParams<'tenantId'>()
  const { data: bundle } = useBundle(tenantId)
  const { data: hq, isLoading: hqLoading } = useHqBranches(tenantId)

  // The gate guarantees a complete bundle before this renders; guard anyway.
  if (!bundle) return <LoadingState />

  const { Tenant: t, Company: company, Branches } = bundle
  const branches = Branches ?? []
  const activeBranches = branches.filter((b) => b.Status === 'active').length
  const deviceCount = branches.reduce((sum, b) => sum + (b.ActiveDevices ?? 0), 0)
  const totals = hq?.totals

  return (
    <>
      <PageHeader
        title="نظرة عامة"
        description="ملخّص نشاطك التجاري وفروعك ومكان بياناتك."
        actions={<Badge tone={tenantStatusTone(t.Status)}>{tenantStatusLabel(t.Status)}</Badge>}
      />

      {t.Status === 'suspended' && (
        <Banner
          tone="danger"
          icon={DangerIcon}
          title="النشاط موقوف"
          message="تم إيقاف هذا النشاط مؤقتًا. تواصل مع الدعم لإعادة التفعيل."
        />
      )}

      {!t.Plan && (
        <Banner
          tone="info"
          icon={InfoIcon}
          title="لا يوجد اشتراك مزامنة"
          message="فعّل اشتراك المزامنة لربط أجهزة الفروع ومزامنة بياناتها."
        />
      )}

      {t.Status !== 'suspended' && deviceCount === 0 && (
        <OnboardingBanner tenantId={t.ID} />
      )}

      {/* Today's KPIs — company-wide sums the API derives from the same
          branch snapshots the Branches cards render, so the numbers agree. */}
      <section className="mb-6">
        <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
          <h2 className="font-display text-base font-bold">اليوم</h2>
          {totals ? (
            <Freshness
              source={totals.synced_branches > 0 ? 'synced' : 'offline'}
              asOf={totals.as_of}
            />
          ) : hqLoading ? (
            <Skeleton className="h-6 w-40 rounded-full" />
          ) : null}
        </div>
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          <KpiCard
            label="مبيعات اليوم"
            value={totals ? money.format(totals.sales_total) : undefined}
          />
          <KpiCard
            label="الفواتير"
            value={totals ? toArabicDigits(totals.sales_count) : undefined}
          />
          <KpiCard
            label="المرتجعات"
            value={totals ? money.format(totals.refunds_total) : undefined}
          />
          <KpiCard
            label="الورديات المفتوحة"
            value={totals ? toArabicDigits(totals.open_shift_count) : undefined}
          />
        </div>
        {totals && totals.offline_branches > 0 && (
          <p className="mt-2 text-xs text-muted-foreground">
            تشمل الأرقام آخر بيانات معروفة لعدد{' '}
            {toArabicDigits(totals.offline_branches)} من الفروع غير المتزامنة.
          </p>
        )}
      </section>

      {/* Branch health strip — one chip per branch, click through to detail. */}
      {hq && hq.branches.length > 0 && (
        <section className="mb-6">
          <h2 className="mb-3 font-display text-base font-bold">صحة الفروع</h2>
          <div className="flex flex-wrap gap-2">
            {hq.branches.map((v) => (
              <Link
                key={v.id}
                to={`/tenants/${t.ID}/branches/${v.id}`}
                className="inline-flex items-center gap-2 rounded-full border border-border bg-card/50 px-3 py-1.5 text-sm font-medium transition-colors hover:border-primary/40 hover:text-primary"
              >
                <HealthDot health={v.health} />
                {v.name}
              </Link>
            ))}
          </div>
        </section>
      )}

      <div className="grid gap-4 sm:grid-cols-2">
        <StatCard
          icon={CompanyIcon}
          label="النشاط التجاري"
          value={company?.Name ?? '—'}
          hint={
            company?.Phone ? (
              <span className="dir-ltr inline-flex items-center gap-1.5 font-mono">
                <PhoneIcon className="size-3.5" />
                {company.Phone}
              </span>
            ) : undefined
          }
          to={`/tenants/${t.ID}/company`}
        />

        <StatCard
          icon={BranchIcon}
          label="الفروع"
          value={toArabicDigits(branches.length)}
          hint={`${toArabicDigits(activeBranches)} مُفعّل`}
          to={`/tenants/${t.ID}/branches`}
        />

        <StatCard
          icon={WalletIcon}
          label="الباقة"
          value={t.Plan || 'بدون اشتراك'}
        />

        <StatCard
          icon={DatabaseIcon}
          label="مكان البيانات"
          value={t.DBName ?? '—'}
          mono
        />
      </div>
    </>
  )
}

// A number-first KPI tile; undefined value = still loading (skeleton).
// Never blanks: TanStack keeps the last totals while refreshing.
function KpiCard({ label, value }: { label: string; value?: string }) {
  return (
    <Card className="p-4">
      <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
        {label}
      </div>
      {value !== undefined ? (
        <div className="mt-1 truncate font-display text-2xl font-bold">{value}</div>
      ) : (
        <Skeleton className="mt-2 h-7 w-20" />
      )}
    </Card>
  )
}

function StatCard({
  icon: IconCmp,
  label,
  value,
  hint,
  to,
  mono,
}: {
  icon: IconComponent
  label: string
  value: string
  hint?: ReactNode
  to?: string
  mono?: boolean
}) {
  const body = (
    <Card
      className={cn(
        'flex h-full items-start gap-3 p-4',
        to && 'transition-colors hover:border-primary/40',
      )}
    >
      <div className="grid size-10 shrink-0 place-items-center rounded-xl bg-accent text-primary">
        <IconCmp className="size-5" />
      </div>
      <div className="min-w-0 flex-1">
        <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
          {label}
        </div>
        <div
          className={cn(
            'mt-1 truncate font-display text-lg font-bold',
            mono && 'dir-ltr font-mono text-base',
          )}
        >
          {value}
        </div>
        {hint && <div className="mt-0.5 text-xs text-muted-foreground">{hint}</div>}
      </div>
      {to && (
        <ArrowLeading className="size-5 shrink-0 text-muted-foreground" />
      )}
    </Card>
  )

  return to ? (
    <Link to={to} className="group">
      {body}
    </Link>
  ) : (
    body
  )
}

function OnboardingBanner({ tenantId }: { tenantId: string }) {
  return (
    <div className="mb-4 flex flex-col items-start gap-4 rounded-xl border border-primary/30 bg-primary/5 px-5 py-4 sm:flex-row sm:items-center sm:justify-between">
      <div className="flex items-start gap-3">
        <div className="grid size-10 shrink-0 place-items-center rounded-xl bg-primary/10 text-primary">
          <DownloadIcon className="size-5" />
        </div>
        <div>
          <div className="text-sm font-semibold">لم يتم تثبيت التطبيق بعد</div>
          <p className="mt-0.5 text-sm text-foreground/70">
            نزّل تطبيق أريب لسطح المكتب على أجهزة فروعك للبدء بالبيع والمزامنة.
          </p>
        </div>
      </div>
      <Button asChild size="sm" className="shrink-0">
        <Link to={`/tenants/${tenantId}/download`}>
          تنزيل التطبيق
          <ArrowLeading className="size-4" />
        </Link>
      </Button>
    </div>
  )
}

function Banner({
  tone,
  icon: IconCmp,
  title,
  message,
}: {
  tone: 'danger' | 'info'
  icon: IconComponent
  title: string
  message: string
}) {
  return (
    <div
      className={cn(
        'mb-4 flex items-start gap-3 rounded-xl border px-4 py-3',
        tone === 'danger'
          ? 'border-danger/30 bg-danger/5 text-danger'
          : 'border-info/30 bg-info/5 text-info',
      )}
      role={tone === 'danger' ? 'alert' : 'status'}
    >
      <IconCmp className="mt-0.5 size-5 shrink-0" />
      <div>
        <div className="text-sm font-semibold">{title}</div>
        <p className="mt-0.5 text-sm text-foreground/70">{message}</p>
      </div>
    </div>
  )
}
