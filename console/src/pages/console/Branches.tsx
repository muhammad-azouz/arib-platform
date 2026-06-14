import { useState } from 'react'
import { useParams } from 'react-router-dom'
import { toast } from 'sonner'
import { errorMessage } from '@/lib/auth'
import { useBundle, useUpdateBranch } from '@/lib/hooks'
import { branchStatusLabel, branchStatusTone, toArabicDigits } from '@/lib/format'
import { cn } from '@/lib/utils'
import type { Branch } from '@/lib/types'
import { PageHeader } from '@/components/PageHeader'
import { LoadingState, EmptyState } from '@/components/States'
import { AddBranchDialog } from '@/components/AddBranchDialog'
import { RenameBranchDialog } from '@/components/RenameBranchDialog'
import { AddIcon, BranchIcon, EditIcon, MenuIcon, InfoIcon } from '@/components/icon'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

export function Branches() {
  const { tenantId } = useParams<'tenantId'>()
  const { data: bundle } = useBundle(tenantId)
  const update = useUpdateBranch(tenantId ?? '')
  const [addOpen, setAddOpen] = useState(false)
  const [renaming, setRenaming] = useState<Branch | null>(null)

  // The app gate guarantees company + at least one branch before this renders.
  if (!bundle) return <LoadingState />

  const branches = bundle.Branches ?? []
  const companyId = bundle.Company?.ID ?? ''

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
        description="فروع نشاطك ومقاعد الأجهزة المسموح بها لكل فرع."
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
        <div className="rounded-xl border border-border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>الفرع</TableHead>
                <TableHead>الحالة</TableHead>
                <TableHead>الأجهزة (المستخدم / الحد)</TableHead>
                <TableHead className="w-12" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {branches.map((b) => (
                <TableRow key={b.ID}>
                  <TableCell className="font-medium">{b.Name}</TableCell>
                  <TableCell>
                    <Badge tone={branchStatusTone(b.Status)}>
                      {branchStatusLabel(b.Status)}
                    </Badge>
                  </TableCell>
                  <TableCell>
                    <span
                      className={cn(
                        'dir-ltr inline-block font-mono text-sm',
                        (b.ActiveDevices ?? 0) >= b.Seats
                          ? 'text-warning'
                          : 'text-muted-foreground',
                      )}
                    >
                      {toArabicDigits(b.Seats)} / {toArabicDigits(b.ActiveDevices ?? 0)}
                    </span>
                  </TableCell>
                  <TableCell>
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
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
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
