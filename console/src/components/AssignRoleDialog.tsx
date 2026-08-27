import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { z } from 'zod'
import { toast } from 'sonner'
import { errorMessage } from '@/lib/auth'
import { useAssignMemberRole, useBundle, useRoles } from '@/lib/hooks'
import type { Member } from '@/lib/types'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Label } from '@/components/ui/label'
import { LoadingState } from '@/components/States'

const schema = z.object({
  roleId: z.string().min(1, 'اختر دورًا'),
})
type Form = z.infer<typeof schema>

const NO_BRANCHES_SELECTED = 'اختر فرعًا واحدًا على الأقل، أو فعّل «كل الفروع»'

/**
 * Role + branch-allowlist reassignment dialog for one member row (T113 role
 * picker; T123 adds the branch picker). One instance per open dialog
 * (Settings.tsx only renders this when a member is selected), so `member`
 * is always the current target — no create/edit mode split like
 * `RoleFormDialog`.
 *
 * The branch picker is a mode toggle, not a bare multi-select: "كل الفروع"
 * on means the allowlist is empty (D4 — unscoped, sees every branch,
 * including ones created later); off means exactly the ticked ids, and at
 * least one must be ticked (an empty *scoped* selection would be
 * indistinguishable on the wire from "all branches", so the client refuses
 * to send it rather than let a mis-click silently unscope someone).
 */
export function AssignRoleDialog({
  tenantId,
  member,
  open,
  onOpenChange,
}: {
  tenantId: string
  member: Member
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const rolesQuery = useRoles(tenantId)
  const { data: bundle } = useBundle(tenantId)
  const assign = useAssignMemberRole(tenantId)

  const [allBranches, setAllBranches] = useState(member.branch_ids.length === 0)
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set(member.branch_ids))
  const [branchError, setBranchError] = useState<string | null>(null)

  const form = useForm<Form>({
    resolver: zodResolver(schema),
    defaultValues: { roleId: member.role_id ?? '' },
  })

  const reset = () => {
    form.reset({ roleId: member.role_id ?? '' })
    setAllBranches(member.branch_ids.length === 0)
    setSelectedIds(new Set(member.branch_ids))
    setBranchError(null)
  }

  const toggleBranch = (id: string) => {
    setSelectedIds((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const submit = form.handleSubmit(async (values) => {
    if (!allBranches && selectedIds.size === 0) {
      setBranchError(NO_BRANCHES_SELECTED)
      return
    }
    setBranchError(null)
    try {
      await assign.mutateAsync({
        memberId: member.id,
        roleId: values.roleId,
        branchIds: allBranches ? [] : [...selectedIds],
      })
      toast.success('تم تحديث دور العضو')
      onOpenChange(false)
    } catch (err) {
      toast.error(errorMessage(err))
    }
  })

  const name = [member.first_name, member.last_name].filter(Boolean).join(' ') || member.email
  const branches = bundle?.Branches ?? []

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        if (!o) reset()
        onOpenChange(o)
      }}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle>تعيين دور</DialogTitle>
          <DialogDescription>{name}</DialogDescription>
        </DialogHeader>
        <form onSubmit={submit} className="space-y-4" noValidate>
          <div className="space-y-1.5">
            <Label htmlFor="assign-role-id">
              الدور<span className="text-danger"> *</span>
            </Label>
            {rolesQuery.isLoading ? (
              <LoadingState rows={1} />
            ) : (
              <select
                id="assign-role-id"
                className="flex h-9 w-full rounded-md border border-input bg-background/40 px-3 py-1 text-sm shadow-sm transition-colors focus-visible:outline-none focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/30"
                {...form.register('roleId')}
              >
                <option value="">اختر دورًا</option>
                {(rolesQuery.data ?? []).map((r) => (
                  <option key={r.id} value={r.id}>
                    {r.name}
                  </option>
                ))}
              </select>
            )}
            {form.formState.errors.roleId && (
              <p className="text-xs text-danger">{form.formState.errors.roleId.message}</p>
            )}
          </div>

          <div className="space-y-1.5">
            <Label>الفروع</Label>
            <div className="overflow-hidden rounded-lg border border-border">
              <label className="flex items-center gap-2 border-b border-border bg-card/50 p-2.5 text-sm">
                <input
                  type="checkbox"
                  checked={allBranches}
                  onChange={(e) => {
                    setAllBranches(e.target.checked)
                    if (e.target.checked) setBranchError(null)
                  }}
                />
                كل الفروع
              </label>
              {!allBranches && (
                <div className="max-h-40 overflow-y-auto">
                  {branches.length === 0 ? (
                    <p className="p-2.5 text-xs text-muted-foreground">لا توجد فروع بعد</p>
                  ) : (
                    branches.map((b) => (
                      <label
                        key={b.ID}
                        className="flex items-center gap-2 border-b border-border p-2.5 text-sm last:border-0"
                      >
                        <input
                          type="checkbox"
                          checked={selectedIds.has(b.ID)}
                          onChange={() => toggleBranch(b.ID)}
                        />
                        {b.Name}
                      </label>
                    ))
                  )}
                </div>
              )}
            </div>
            {branchError && <p className="text-xs text-danger">{branchError}</p>}
            {/* D4 — fail-closed by design, and worth saying so: an unscoped
                allowlist auto-includes every future branch, a scoped one
                never does. */}
            <p className="text-xs text-muted-foreground">
              فرع يُنشأ لاحقًا لا يُضاف تلقائيًا إلى تعيين محدد الفروع — العضو المحدد
              يرى فقط الفروع المختارة أعلاه، حتى بعد إضافة فروع جديدة.
            </p>
          </div>

          <DialogFooter>
            <Button type="submit" disabled={assign.isPending}>
              حفظ
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
