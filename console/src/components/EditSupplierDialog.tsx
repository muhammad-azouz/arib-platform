import { useEffect } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { z } from 'zod'
import { toast } from 'sonner'
import { errorMessage } from '@/lib/auth'
import { useCustomerGroups, useUpdateSupplier } from '@/lib/hooks'
import type { SupplierDetail } from '@/lib/types'
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

const selectClass =
  'flex h-9 w-full rounded-md border border-input bg-background/40 px-3 py-1 text-sm shadow-sm transition-colors focus-visible:outline-none focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/30'

const schema = z.object({
  name: z.string().trim().min(1, 'اسم المورد مطلوب').max(100, 'الاسم طويل جدًا'),
  phone1: z.string().trim().min(1, 'رقم الهاتف مطلوب').max(12, 'الرقم طويل جدًا'),
  address: z.string().trim().max(200, 'العنوان طويل جدًا').optional(),
  groupId: z.string().optional(),
  creditLimit: z.number('أدخل رقمًا صحيحًا').min(0, 'الحد الائتماني يجب ألا يكون سالبًا'),
})
type Form = z.infer<typeof schema>

/**
 * Edit/deactivate dialog for one supplier's profile. Only fields the user
 * actually changed are sent — the gateway's partial-update contract — so
 * re-saving unrelated fields is a no-op on the server side either way.
 */
export function EditSupplierDialog({
  tenantId,
  supplier,
  open,
  onOpenChange,
}: {
  tenantId: string
  supplier: SupplierDetail | null
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const groupsQuery = useCustomerGroups(tenantId)
  const update = useUpdateSupplier(tenantId)
  const form = useForm<Form>({
    resolver: zodResolver(schema),
    defaultValues: {
      name: '',
      phone1: '',
      address: '',
      groupId: '',
      creditLimit: 0,
    },
  })

  useEffect(() => {
    if (open && supplier) {
      form.reset({
        name: supplier.name,
        phone1: supplier.phone1,
        address: supplier.address ?? '',
        groupId: supplier.group_id ?? '',
        creditLimit: supplier.credit_limit,
      })
    }
  }, [open, supplier, form])

  const submit = form.handleSubmit(async (values) => {
    if (!supplier) return
    try {
      await update.mutateAsync({
        supplierId: supplier.id,
        name: values.name,
        phone1: values.phone1,
        address: values.address || undefined,
        group_id: values.groupId || undefined,
        credit_limit: values.creditLimit,
      })
      toast.success('تم حفظ بيانات المورد')
      onOpenChange(false)
    } catch (err) {
      toast.error(errorMessage(err))
    }
  })

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>تعديل بيانات المورد</DialogTitle>
        </DialogHeader>
        <form onSubmit={submit} className="space-y-4" noValidate>
          <div className="space-y-1.5">
            <Label htmlFor="edit-supplier-name">
              اسم المورد<span className="text-danger"> *</span>
            </Label>
            <Input id="edit-supplier-name" autoFocus {...form.register('name')} />
            {form.formState.errors.name && (
              <p className="text-xs text-danger">{form.formState.errors.name.message}</p>
            )}
          </div>

          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1.5">
              <Label htmlFor="edit-supplier-phone1">
                الهاتف<span className="text-danger"> *</span>
              </Label>
              <Input
                id="edit-supplier-phone1"
                dir="ltr"
                className="text-start"
                {...form.register('phone1')}
              />
              {form.formState.errors.phone1 && (
                <p className="text-xs text-danger">{form.formState.errors.phone1.message}</p>
              )}
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="edit-supplier-group">المجموعة</Label>
              <select id="edit-supplier-group" className={selectClass} {...form.register('groupId')}>
                <option value="">بدون مجموعة</option>
                {(groupsQuery.data?.data ?? []).map((g) => (
                  <option key={g.id} value={g.id}>
                    {g.name}
                  </option>
                ))}
              </select>
            </div>
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="edit-supplier-credit-limit">الحد الائتماني</Label>
            <Input
              id="edit-supplier-credit-limit"
              type="number"
              step="0.01"
              min="0"
              dir="ltr"
              className="text-start"
              {...form.register('creditLimit', { valueAsNumber: true })}
            />
            {form.formState.errors.creditLimit && (
              <p className="text-xs text-danger">{form.formState.errors.creditLimit.message}</p>
            )}
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="edit-supplier-address">العنوان (اختياري)</Label>
            <Input id="edit-supplier-address" {...form.register('address')} />
            {form.formState.errors.address && (
              <p className="text-xs text-danger">{form.formState.errors.address.message}</p>
            )}
          </div>

          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={() => onOpenChange(false)}
              disabled={form.formState.isSubmitting}
            >
              إلغاء
            </Button>
            <Button type="submit" disabled={form.formState.isSubmitting}>
              حفظ
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
