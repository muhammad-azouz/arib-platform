import type { ReactNode } from 'react'
import { Link, useParams } from 'react-router-dom'
import { useBundle } from '@/lib/hooks'
import { tenantStatusLabel, tenantStatusTone, toArabicDigits } from '@/lib/format'
import { cn } from '@/lib/utils'
import { PageHeader } from '@/components/PageHeader'
import { LoadingState } from '@/components/States'
import {
  CompanyIcon,
  BranchIcon,
  WalletIcon,
  ShardIcon,
  DangerIcon,
  InfoIcon,
  PhoneIcon,
  ArrowLeading,
  type IconComponent,
} from '@/components/icon'
import { Badge } from '@/components/ui/badge'
import { Card } from '@/components/ui/card'

export function Overview() {
  const { tenantId } = useParams<'tenantId'>()
  const { data: bundle } = useBundle(tenantId)

  // The gate guarantees a complete bundle before this renders; guard anyway.
  if (!bundle) return <LoadingState />

  const { Tenant: t, Company: company, Branches } = bundle
  const branches = Branches ?? []
  const activeBranches = branches.filter((b) => b.Status === 'active').length

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
          icon={ShardIcon}
          label="مكان البيانات"
          value={t.DBName ?? '—'}
          hint={t.ShardID ? `الخادم: ${t.ShardID}` : 'لم يُخصّص خادم بعد'}
          mono
        />
      </div>
    </>
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
