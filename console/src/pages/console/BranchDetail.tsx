import type { ReactNode } from 'react'
import { Link, useParams } from 'react-router-dom'
import { useBundle, useHqBranches } from '@/lib/hooks'
import {
  branchStatusLabel,
  branchStatusTone,
  fmtDateTime,
  relative,
  toArabicDigits,
} from '@/lib/format'
import { cn } from '@/lib/utils'
import type { BranchHealth } from '@/lib/types'
import { Breadcrumbs } from '@/components/Breadcrumbs'
import { Freshness } from '@/components/Freshness'
import { LoadingState, EmptyState } from '@/components/States'
import {
  BranchIcon,
  DeviceIcon,
  RefreshIcon,
  SecurityIcon,
  ArrowLeading,
} from '@/components/icon'
import { Badge } from '@/components/ui/badge'

const HEALTH_DOT: Record<BranchHealth, string> = {
  ok: 'bg-success',
  lagging: 'bg-warning',
  stale: 'bg-danger',
  never: 'bg-muted-foreground/40',
}
const HEALTH_LABEL: Record<BranchHealth, string> = {
  ok: 'متزامن',
  lagging: 'متأخر في المزامنة',
  stale: 'منقطع عن المزامنة',
  never: 'لم يتصل بعد',
}

const money = new Intl.NumberFormat('ar', { maximumFractionDigits: 2 })

/** Collapsible section (native disclosure — accessible, no JS state). */
function Section({
  icon: IconCmp,
  title,
  children,
  defaultOpen,
}: {
  icon: typeof DeviceIcon
  title: string
  children: ReactNode
  defaultOpen?: boolean
}) {
  return (
    <details
      open={defaultOpen}
      className="group rounded-xl border border-border bg-card/50"
    >
      <summary className="flex cursor-pointer list-none items-center gap-2.5 p-4 font-medium [&::-webkit-details-marker]:hidden">
        <IconCmp className="size-5 text-primary" />
        {title}
        <ArrowLeading className="ms-auto size-4 text-muted-foreground transition-transform group-open:-rotate-90" />
      </summary>
      <div className="border-t border-border p-4 text-sm">{children}</div>
    </details>
  )
}

export function BranchDetail() {
  const { tenantId, branchId } = useParams<'tenantId' | 'branchId'>()
  const { data: bundle } = useBundle(tenantId)
  const { data: hq } = useHqBranches(tenantId)

  if (!bundle) return <LoadingState />

  const branch = (bundle.Branches ?? []).find((b) => b.ID === branchId)
  const view = hq?.branches.find((v) => v.id === branchId)
  const snap = view?.snapshot.data

  if (!branch) {
    return (
      <EmptyState
        icon={BranchIcon}
        title="فرع غير موجود"
        description="هذا الفرع غير مسجل في نشاطك."
        action={
          <Link to={`/tenants/${tenantId}/branches`} className="text-sm text-primary">
            العودة إلى الفروع
          </Link>
        }
      />
    )
  }

  return (
    <>
      <Breadcrumbs
        className="mb-4"
        items={[
          { label: 'الفروع', to: `/tenants/${tenantId}/branches` },
          { label: branch.Name },
        ]}
      />

      {/* header: identity + freshness + day summary */}
      <div className="mb-6 flex flex-col gap-3">
        <div className="flex flex-wrap items-center gap-3">
          <span
            title={view ? HEALTH_LABEL[view.health] : undefined}
            className={cn(
              'size-3 rounded-full',
              view ? HEALTH_DOT[view.health] : 'bg-muted-foreground/40',
            )}
          />
          <h1 className="font-display text-2xl font-bold">{branch.Name}</h1>
          <Badge tone={branchStatusTone(branch.Status)}>
            {branchStatusLabel(branch.Status)}
          </Badge>
          {view && <Freshness source={view.snapshot.source} asOf={view.last_sync_at} />}
        </div>
        <div className="flex flex-wrap gap-x-6 gap-y-1 text-sm text-muted-foreground">
          <span>
            مبيعات اليوم:{' '}
            <span className="font-medium text-foreground">
              {snap
                ? `${money.format(snap.today_sales_total)} · ${toArabicDigits(snap.today_sales_count)} فاتورة`
                : '—'}
            </span>
          </span>
          <span>
            الوردية الحالية:{' '}
            <span className="font-medium text-foreground">
              {snap?.open_shift
                ? `${snap.open_shift.opened_by} · ${relative(snap.open_shift.opened_at)}`
                : snap
                  ? 'لا توجد وردية مفتوحة'
                  : '—'}
            </span>
          </span>
        </div>
      </div>

      <div className="flex flex-col gap-3">
        <Section icon={DeviceIcon} title="الأجهزة والمقاعد" defaultOpen>
          <div className="flex items-center gap-2">
            المقاعد المستخدمة:
            <span
              className={cn(
                'dir-ltr inline-block font-mono',
                (branch.ActiveDevices ?? 0) >= branch.Seats && 'text-warning',
              )}
            >
              {toArabicDigits(branch.Seats)} / {toArabicDigits(branch.ActiveDevices ?? 0)}
            </span>
          </div>
          <p className="mt-2 text-xs text-muted-foreground">
            ربط الأجهزة وتحريرها يتم من تطبيق سطح المكتب عند الاتصال لأول مرة.
          </p>
        </Section>

        <Section icon={RefreshIcon} title="نشاط المزامنة">
          <dl className="grid gap-2">
            <div className="flex gap-2">
              <dt className="text-muted-foreground">آخر مزامنة:</dt>
              <dd className="font-medium">
                {view?.last_sync_at
                  ? `${fmtDateTime(view.last_sync_at)} (${relative(view.last_sync_at)})`
                  : 'لم تتم المزامنة بعد'}
              </dd>
            </div>
            <div className="flex gap-2">
              <dt className="text-muted-foreground">الحالة:</dt>
              <dd className="font-medium">{view ? HEALTH_LABEL[view.health] : '—'}</dd>
            </div>
            {snap && snap.open_shift_count > 1 && (
              <div className="flex gap-2">
                <dt className="text-muted-foreground">ورديات مفتوحة:</dt>
                <dd className="font-medium">{toArabicDigits(snap.open_shift_count)}</dd>
              </div>
            )}
          </dl>
          <p className="mt-2 text-xs text-muted-foreground">
            يزامن الفرع بياناته تلقائيًا كل خمس دقائق تقريبًا عندما يكون متصلًا.
          </p>
        </Section>

        <Section icon={SecurityIcon} title="التشخيص">
          <p className="text-muted-foreground">
            أدوات التشخيص التفصيلية (سجل المزامنة، التعارضات) ستتوفر قريبًا.
          </p>
        </Section>
      </div>
    </>
  )
}
