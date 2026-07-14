import { useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { toast } from 'sonner'
import { errorMessage } from '@/lib/auth'
import { useBundle, useHqBranches, useUpdateBranch } from '@/lib/hooks'
import {
  branchStatusLabel,
  branchStatusTone,
  relative,
  toArabicDigits,
} from '@/lib/format'
import { cn } from '@/lib/utils'
import type { Branch, BranchView } from '@/lib/types'
import { PageHeader } from '@/components/PageHeader'
import { LoadingState, EmptyState } from '@/components/States'
import { Freshness } from '@/components/Freshness'
import { HealthDot } from '@/components/HealthDot'
import { AddBranchDialog } from '@/components/AddBranchDialog'
import { RenameBranchDialog } from '@/components/RenameBranchDialog'
import {
  AddIcon,
  BranchIcon,
  DeviceIcon,
  EditIcon,
  MenuIcon,
  InfoIcon,
} from '@/components/icon'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'

const money = new Intl.NumberFormat('ar', { maximumFractionDigits: 2 })

export function Branches() {
  const { tenantId } = useParams<'tenantId'>()
  const { data: bundle } = useBundle(tenantId)
  const { data: hq, isLoading: hqLoading } = useHqBranches(tenantId)
  const update = useUpdateBranch(tenantId ?? '')
  const [addOpen, setAddOpen] = useState(false)
  const [renaming, setRenaming] = useState<Branch | null>(null)

  // The app gate guarantees company + at least one branch before this renders.
  if (!bundle) return <LoadingState />

  const branches = bundle.Branches ?? []
  const companyId = bundle.Company?.ID ?? ''
  const viewById = new Map<string, BranchView>(
    (hq?.branches ?? []).map((v) => [v.id, v]),
  )

  const toggleStatus = (b: Branch) => {
    const next = b.Status === 'active' ? 'deactivated' : 'active'
    update.mutate(
      { branchId: b.ID, status: next },
      {
        onSuccess: () =>
          toast.success(next === 'active' ? 'تم تفعيل الفرع' : 'تم تعطيل الفرع'),
        onError: (err) => toast.error(errorMessage(err)),
      },
    )
  }

  return (
    <>
      <PageHeader
        title="الفروع"
        description="حالة كل فرع الآن: المزامنة والوردية ومبيعات اليوم."
        actions={
          <Button onClick={() => setAddOpen(true)} disabled={!companyId}>
            <AddIcon className="size-4" />
            فرع جديد
          </Button>
        }
      />

      {branches.length === 0 ? (
        <EmptyState
          icon={BranchIcon}
          title="لا توجد فروع"
          description="أضف أول فرع لنشاطك للبدء."
          action={
            <Button onClick={() => setAddOpen(true)} disabled={!companyId}>
              <AddIcon className="size-4" />
              إضافة فرع
            </Button>
          }
        />
      ) : (
        <div className="grid gap-4 sm:grid-cols-2">
          {branches.map((b) => {
            const view = viewById.get(b.ID)
            const snap = view?.snapshot.data
            return (
              <div
                key={b.ID}
                className="flex flex-col gap-3 rounded-xl border border-border bg-card/50 p-4"
              >
                {/* identity row: health dot, name, status, actions */}
                <div className="flex items-center gap-2.5">
                  <HealthDot health={view?.health} />
                  <h3 className="min-w-0 truncate font-display font-bold">
                    <Link
                      to={`/tenants/${tenantId}/branches/${b.ID}`}
                      className="transition-colors hover:text-primary"
                    >
                      {b.Name}
                    </Link>
                  </h3>
                  <Badge tone={branchStatusTone(b.Status)} className="shrink-0">
                    {branchStatusLabel(b.Status)}
                  </Badge>
                  <div className="ms-auto">
                    <DropdownMenu>
                      <DropdownMenuTrigger asChild>
                        <Button variant="ghost" size="icon" aria-label="إجراءات الفرع">
                          <MenuIcon className="size-4" />
                        </Button>
                      </DropdownMenuTrigger>
                      <DropdownMenuContent align="end" className="w-44">
                        <DropdownMenuItem onSelect={() => setRenaming(b)}>
                          <EditIcon className="size-4" />
                          إعادة تسمية
                        </DropdownMenuItem>
                        <DropdownMenuSeparator />
                        <DropdownMenuItem
                          variant={b.Status === 'active' ? 'destructive' : undefined}
                          onSelect={() => toggleStatus(b)}
                        >
                          {b.Status === 'active' ? 'تعطيل الفرع' : 'تفعيل الفرع'}
                        </DropdownMenuItem>
                      </DropdownMenuContent>
                    </DropdownMenu>
                  </div>
                </div>

                {/* freshness */}
                {view ? (
                  <Freshness source={view.snapshot.source} asOf={view.last_sync_at} />
                ) : hqLoading ? (
                  <Skeleton className="h-6 w-40 rounded-full" />
                ) : null}

                {/* day snapshot */}
                <div className="grid grid-cols-2 gap-3 text-sm">
                  <div>
                    <div className="text-xs text-muted-foreground">مبيعات اليوم</div>
                    {view || !hqLoading ? (
                      <div className="mt-0.5 font-medium">
                        {snap
                          ? `${money.format(snap.today_sales_total)} · ${toArabicDigits(snap.today_sales_count)} فاتورة`
                          : '—'}
                      </div>
                    ) : (
                      <Skeleton className="mt-1 h-5 w-24" />
                    )}
                  </div>
                  <div>
                    <div className="text-xs text-muted-foreground">الوردية الحالية</div>
                    {view || !hqLoading ? (
                      <div className="mt-0.5 font-medium">
                        {snap?.open_shift
                          ? `${snap.open_shift.opened_by} · ${relative(snap.open_shift.opened_at)}`
                          : snap
                            ? 'لا توجد وردية مفتوحة'
                            : '—'}
                      </div>
                    ) : (
                      <Skeleton className="mt-1 h-5 w-24" />
                    )}
                  </div>
                </div>

                {/* seats (management) */}
                <div className="mt-auto flex items-center gap-1.5 border-t border-border pt-2.5 text-xs text-muted-foreground">
                  <DeviceIcon className="size-4" />
                  الأجهزة
                  <span
                    className={cn(
                      'dir-ltr inline-block font-mono',
                      (b.ActiveDevices ?? 0) >= b.Seats && 'text-warning',
                    )}
                  >
                    {toArabicDigits(b.Seats)} / {toArabicDigits(b.ActiveDevices ?? 0)}
                  </span>
                </div>
              </div>
            )
          })}
        </div>
      )}

      <p className="mt-4 flex items-start gap-2 text-xs text-muted-foreground">
        <InfoIcon className="mt-0.5 size-4 shrink-0" />
        ربط أجهزة الفروع وإصدار رموز المزامنة يتم من تطبيق سطح المكتب عند الاتصال
        لأول مرة.
      </p>

      <AddBranchDialog
        tenantId={bundle.Tenant.ID}
        companyId={companyId}
        open={addOpen}
        onOpenChange={setAddOpen}
      />
      <RenameBranchDialog
        tenantId={bundle.Tenant.ID}
        branch={renaming}
        open={renaming != null}
        onOpenChange={(o) => !o && setRenaming(null)}
      />
    </>
  )
}
