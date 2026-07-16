import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { z } from 'zod'
import { toast } from 'sonner'
import { useNavigate } from 'react-router-dom'
import { errorMessage } from '@/lib/auth'
import { useBundle, useCreateSupplier, useCustomerGroups } from '@/lib/hooks'
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

// v1 keeps this bounded, matching CreateCustomerDialog's same scope decision:
// no opening balance — a supplier created from HQ starts at a zero ledger.
const schema = z.object({
  name: z.string().trim().min(1, 'اسم المورد مطلوب').max(100, 'الاسم طويل جدًا'),
  phone1: z.string().trim().min(1, 'رقم الهاتف مطلوب').max(12, 'الرقم طويل جدًا'),
  address: z.string().trim().max(200, 'العنوان طويل جدًا').optional(),
  groupId: z.string().optional(),
  creditLimit: z.number('أدخل رقمًا صحيحًا').min(0, 'الحد الائتماني يجب ألا يكون سالبًا'),
  branchId: z.string().min(1, 'الفرع مطلوب'),
})
type Form = z.infer<typeof schema>

/** Create-supplier dialog («مورد جديد» on the Suppliers page). On success,
 * navigates straight to the new supplier's profile. */
export function CreateSupplierDialog({
  tenantId,
  open,
  onOpenChange,
}: {
  tenantId: string
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const navigate = useNavigate()
  const { data: bundle } = useBundle(tenantId)
  const groupsQuery = useCustomerGroups(tenantId)
  const create = useCreateSupplier(tenantId)
  const branches = (bundle?.Branches ?? []).filter((b) => b.Status === 'active')

  const form = useForm<Form>({
    resolver: zodResolver(schema),
    defaultValues: {
      name: '',
      phone1: '',
      address: '',
      groupId: '',
      creditLimit: 0,
      branchId: '',
    },
  })

  const submit = form.handleSubmit(async (values) => {
    try {
      const result = await create.mutateAsync({
        name: values.name,
        phone1: values.phone1,
        address: values.address || undefined,
        group_id: values.groupId || undefined,
        credit_limit: values.creditLimit,
        branch_id: values.branchId,
      })
      toast.success('تم إنشاء المورد')
      onOpenChange(false)
      form.reset()
      navigate(`/tenants/${tenantId}/suppliers/${result.id}`)
    } catch (err) {
      toast.error(errorMessage(err))
    }
  })

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>مورد جديد</DialogTitle>
        </DialogHeader>
        <form onSubmit={submit} className="space-y-4" noValidate>
          <div className="space-y-1.5">
            <Label htmlFor="new-supplier-name">
              اسم المورد<span className="text-danger"> *</span>
            </Label>
            <Input id="new-supplier-name" autoFocus {...form.register('name')} />
            {form.formState.errors.name && (
              <p className="text-xs text-danger">{form.formState.errors.name.message}</p>
            )}
          </div>

          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1.5">
              <Label htmlFor="new-supplier-phone1">
                الهاتف<span className="text-danger"> *</span>
              </Label>
              <Input id="new-supplier-phone1" dir="ltr" className="text-start" {...form.register('phone1')} />
              {form.formState.errors.phone1 && (
                <p className="text-xs text-danger">{form.formState.errors.phone1.message}</p>
              )}
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="new-supplier-branch">
                الفرع<span className="text-danger"> *</span>
              </Label>
              <select
                id="new-supplier-branch"
                className={selectClass}
                {...form.register('branchId')}
              >
                <option value="">اختر الفرع</option>
                {branches.map((b) => (
                  <option key={b.ID} value={b.ID}>
                    {b.Name}
                  </option>
                ))}
              </select>
              {form.formState.errors.branchId && (
                <p className="text-xs text-danger">{form.formState.errors.branchId.message}</p>
              )}
            </div>
          </div>

          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1.5">
              <Label htmlFor="new-supplier-group">المجموعة</Label>
              <select
                id="new-supplier-group"
                className={selectClass}
                {...form.register('groupId')}
              >
                <option value="">بدون مجموعة</option>
                {(groupsQuery.data?.data ?? []).map((g) => (
                  <option key={g.id} value={g.id}>
                    {g.name}
                  </option>
                ))}
              </select>
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="new-supplier-credit-limit">الحد الائتماني</Label>
              <Input
                id="new-supplier-credit-limit"
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
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="new-supplier-address">العنوان (اختياري)</Label>
            <Input id="new-supplier-address" {...form.register('address')} />
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
              إنشاء المورد
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
