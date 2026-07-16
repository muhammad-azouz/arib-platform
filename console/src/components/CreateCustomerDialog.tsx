import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { z } from 'zod'
import { toast } from 'sonner'
import { useNavigate } from 'react-router-dom'
import { errorMessage } from '@/lib/auth'
import { useBundle, useCreateCustomer, useCustomerGroups } from '@/lib/hooks'
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

// v1 keeps this bounded, matching CreateProductDialog's same scope decision:
// no opening balance — a customer created from HQ starts at a zero ledger,
// same as an HQ-created product starts at zero stock.
const schema = z.object({
  name: z.string().trim().min(1, 'اسم العميل مطلوب').max(100, 'الاسم طويل جدًا'),
  phone1: z.string().trim().min(1, 'رقم الهاتف مطلوب').max(12, 'الرقم طويل جدًا'),
  address: z.string().trim().max(200, 'العنوان طويل جدًا').optional(),
  groupId: z.string().optional(),
  creditLimit: z.number('أدخل رقمًا صحيحًا').min(0, 'الحد الائتماني يجب ألا يكون سالبًا'),
  branchId: z.string().min(1, 'الفرع مطلوب'),
})
type Form = z.infer<typeof schema>

/** Create-customer dialog («عميل جديد» on the Customers page). On success,
 * navigates straight to the new customer's profile. */
export function CreateCustomerDialog({
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
  const create = useCreateCustomer(tenantId)
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
      toast.success('تم إنشاء العميل')
      onOpenChange(false)
      form.reset()
      navigate(`/tenants/${tenantId}/customers/${result.id}`)
    } catch (err) {
      toast.error(errorMessage(err))
    }
  })

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>عميل جديد</DialogTitle>
        </DialogHeader>
        <form onSubmit={submit} className="space-y-4" noValidate>
          <div className="space-y-1.5">
            <Label htmlFor="new-customer-name">
              اسم العميل<span className="text-danger"> *</span>
            </Label>
            <Input id="new-customer-name" autoFocus {...form.register('name')} />
            {form.formState.errors.name && (
              <p className="text-xs text-danger">{form.formState.errors.name.message}</p>
            )}
          </div>

          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1.5">
              <Label htmlFor="new-customer-phone1">
                الهاتف<span className="text-danger"> *</span>
              </Label>
              <Input id="new-customer-phone1" dir="ltr" className="text-start" {...form.register('phone1')} />
              {form.formState.errors.phone1 && (
                <p className="text-xs text-danger">{form.formState.errors.phone1.message}</p>
              )}
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="new-customer-branch">
                الفرع<span className="text-danger"> *</span>
              </Label>
              <select
                id="new-customer-branch"
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
              <Label htmlFor="new-customer-group">المجموعة</Label>
              <select
                id="new-customer-group"
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
              <Label htmlFor="new-customer-credit-limit">الحد الائتماني</Label>
              <Input
                id="new-customer-credit-limit"
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
            <Label htmlFor="new-customer-address">العنوان (اختياري)</Label>
            <Input id="new-customer-address" {...form.register('address')} />
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
              إنشاء العميل
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
