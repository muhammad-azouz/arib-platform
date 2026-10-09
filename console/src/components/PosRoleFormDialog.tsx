import { useMemo, useState } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { z } from 'zod'
import { toast } from 'sonner'
import { errorMessage } from '@/lib/auth'
import { useCreatePosRole, useDeletePosRole, useUpdatePosRole } from '@/lib/hooks'
import { toArabicDigits } from '@/lib/format'
import type { PosPermission, PosRole, PosRoleInput } from '@/lib/types'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

/** Mirrors hq/service_posroles.go `validatePosRole` (itself mirroring the gateway's). */
const schema = z.object({
  name: z.string().trim().min(1, 'اسم الدور مطلوب').max(50, 'اسم الدور لا يتجاوز ٥٠ حرفًا'),
  description: z.string().trim().max(200, 'وصف الدور لا يتجاوز ٢٠٠ حرف'),
})
type Form = z.infer<typeof schema>

/**
 * Create / edit an AribOne POS role — NOT a console role (those live in
 * Settings → الأدوار). One dialog for both; `existing` decides. A POS role is
 * company-wide: saving it changes the role at every branch on its next sync, so
 * the dialog says so, and a role that staff still hold cannot be deleted.
 */
export function PosRoleFormDialog({
  tenantId,
  permissions,
  existing,
  onClose,
}: {
  tenantId: string
  permissions: PosPermission[]
  existing?: PosRole
  onClose: () => void
}) {
  const create = useCreatePosRole(tenantId)
  const update = useUpdatePosRole(tenantId)
  const remove = useDeletePosRole(tenantId)

  const [selected, setSelected] = useState<Set<string>>(new Set(existing?.permission_ids ?? []))
  const [filter, setFilter] = useState('')
  const [confirmingDelete, setConfirmingDelete] = useState(false)

  const form = useForm<Form>({
    resolver: zodResolver(schema),
    defaultValues: { name: existing?.name ?? '', description: existing?.description ?? '' },
  })

  const shown = useMemo(() => {
    const q = filter.trim()
    return q ? permissions.filter((p) => p.name.includes(q) || p.description.includes(q)) : permissions
  }, [permissions, filter])

  function toggle(id: string) {
    setSelected((cur) => {
      const next = new Set(cur)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  // Select / clear only what the filter currently shows, so "all" never silently
  // grabs permissions the manager cannot see.
  function setShown(on: boolean) {
    setSelected((cur) => {
      const next = new Set(cur)
      for (const p of shown) {
        if (on) next.add(p.id)
        else next.delete(p.id)
      }
      return next
    })
  }

  const submit = form.handleSubmit(async (values) => {
    const input: PosRoleInput = {
      name: values.name,
      description: values.description,
      permission_ids: [...selected],
    }
    try {
      if (existing) {
        await update.mutateAsync({ roleId: existing.id, input })
        toast.success('تم حفظ الدور — يصل إلى كل الفروع عند المزامنة القادمة')
      } else {
        await create.mutateAsync(input)
        toast.success('تم إنشاء الدور — يصل إلى كل الفروع عند المزامنة القادمة')
      }
      onClose()
    } catch (err) {
      toast.error(errorMessage(err))
    }
  })

  async function onDelete() {
    if (!existing) return
    try {
      await remove.mutateAsync(existing.id)
      toast.success(`تم حذف دور ${existing.name} — يُحذف من كل الفروع عند المزامنة القادمة`)
      onClose()
    } catch (err) {
      setConfirmingDelete(false)
      toast.error(errorMessage(err))
    }
  }

  const pending = create.isPending || update.isPending || remove.isPending

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{existing ? 'تعديل دور نقطة البيع' : 'دور نقطة بيع جديد'}</DialogTitle>
        </DialogHeader>

        <p className="rounded-lg border border-border bg-muted/40 p-3 text-xs text-muted-foreground">
          هذا دور يُمنح لمستخدمي برنامج AribOne، وليس دورًا في لوحة التحكم. الأدوار مشتركة بين كل
          الفروع، وأي تغيير عليها يصل إلى جميعها عند مزامنتها القادمة.
          {existing && existing.staff_count > 0 && (
            <>
              {' '}
              يشمل التغيير {toArabicDigits(existing.staff_count)} موظفًا يحملون هذا الدور حاليًا.
            </>
          )}
        </p>

        <form onSubmit={submit} className="space-y-4" noValidate autoComplete="off">
          <div className="space-y-1.5">
            <Label htmlFor="posrole-name">
              اسم الدور<span className="text-danger"> *</span>
            </Label>
            <Input id="posrole-name" autoFocus {...form.register('name')} />
            {form.formState.errors.name && (
              <p className="text-xs text-danger">{form.formState.errors.name.message}</p>
            )}
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="posrole-description">الوصف</Label>
            <Input id="posrole-description" {...form.register('description')} />
            {form.formState.errors.description && (
              <p className="text-xs text-danger">{form.formState.errors.description.message}</p>
            )}
          </div>

          <fieldset className="space-y-2">
            <legend className="flex w-full items-center justify-between text-sm font-medium">
              <span>الصلاحيات</span>
              <span className="text-xs font-normal text-muted-foreground">
                {toArabicDigits(selected.size)} من {toArabicDigits(permissions.length)}
              </span>
            </legend>
            <div className="flex items-center gap-2">
              <Input
                value={filter}
                onChange={(e) => setFilter(e.target.value)}
                placeholder="ابحث في الصلاحيات"
                aria-label="ابحث في الصلاحيات"
              />
              <Button type="button" variant="outline" size="sm" onClick={() => setShown(true)}>
                تحديد الكل
              </Button>
              <Button type="button" variant="outline" size="sm" onClick={() => setShown(false)}>
                إلغاء الكل
              </Button>
            </div>
            <div className="max-h-64 space-y-1 overflow-y-auto rounded-lg border border-border p-2">
              {shown.length === 0 ? (
                <p className="p-2 text-xs text-muted-foreground">لا توجد صلاحيات مطابقة.</p>
              ) : (
                shown.map((p) => (
                  <label key={p.id} className="flex items-start gap-2 text-sm" title={p.description}>
                    <input
                      type="checkbox"
                      className="mt-0.5 size-4"
                      checked={selected.has(p.id)}
                      onChange={() => toggle(p.id)}
                    />
                    <span>{p.name}</span>
                  </label>
                ))
              )}
            </div>
          </fieldset>

          <DialogFooter className="sm:justify-between">
            {existing ? (
              confirmingDelete ? (
                <div className="flex items-center gap-2">
                  <Button
                    type="button"
                    variant="destructive"
                    size="sm"
                    disabled={pending}
                    onClick={() => void onDelete()}
                  >
                    تأكيد الحذف
                  </Button>
                  <Button type="button" variant="ghost" size="sm" onClick={() => setConfirmingDelete(false)}>
                    تراجع
                  </Button>
                </div>
              ) : (
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  className="text-danger"
                  disabled={pending}
                  onClick={() => setConfirmingDelete(true)}
                >
                  حذف الدور
                </Button>
              )
            ) : (
              <span />
            )}
            <div className="flex gap-2">
              <Button type="button" variant="ghost" onClick={onClose}>
                إلغاء
              </Button>
              <Button type="submit" disabled={pending}>
                {existing ? 'حفظ' : 'إنشاء'}
              </Button>
            </div>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
