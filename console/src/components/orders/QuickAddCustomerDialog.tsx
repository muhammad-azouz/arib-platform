import { useEffect } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { z } from 'zod'
import { toast } from 'sonner'
import { errorMessage } from '@/lib/auth'
import { findDuplicateCustomerName } from '@/lib/customers'
import { useCreateCustomer } from '@/lib/hooks'
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

// Same bounds as CreateCustomerDialog's schema (T99) — the two forms stay
// honest with each other without sharing a schema, since they diverge on
// which fields exist (no group/credit-limit here) and drift can't get past
// tsc thanks to the one shared NewCustomerInput/useCreateCustomer contract.
const schema = z.object({
  name: z.string().trim().min(1, 'اسم العميل مطلوب').max(100, 'الاسم طويل جدًا'),
  phone1: z.string().trim().min(1, 'رقم الهاتف مطلوب').max(12, 'الرقم طويل جدًا'),
  address: z.string().trim().max(200, 'العنوان طويل جدًا').optional(),
})
type Form = z.infer<typeof schema>

export interface QuickAddCustomer {
  id: string
  name: string
  phone1: string
  address: string
  branchId: string
}

/**
 * Order-scoped quick-add («عميل جديد» inside the New Order picker, T100):
 * four fields, الفرع read-only from the order — a customer created at any
 * other branch would be instantly unusable for it (spec §Quick-add form).
 * No group/credit-limit inputs; a quick-added customer gets none and 0,
 * editable later on the profile. Never navigates, and holds no page state
 * of its own — the caller keeps `open` and the seeds, this component only
 * reports the created customer through `onCreated`.
 */
export function QuickAddCustomerDialog({
  tenantId,
  open,
  onOpenChange,
  branchId,
  branchName,
  defaultName,
  defaultPhone1,
  onCreated,
}: {
  tenantId: string
  open: boolean
  onOpenChange: (open: boolean) => void
  branchId: string
  branchName: string
  defaultName?: string
  defaultPhone1?: string
  onCreated: (customer: QuickAddCustomer) => void
}) {
  const create = useCreateCustomer(tenantId)

  const form = useForm<Form>({
    resolver: zodResolver(schema),
    defaultValues: { name: defaultName ?? '', phone1: defaultPhone1 ?? '', address: '' },
  })

  // The dialog stays mounted across opens (T100 toggles `open`), so a fresh
  // seed from a new search only reaches the form here — RHF's defaultValues
  // is a mount-time snapshot, not a subscription to these props.
  useEffect(() => {
    if (open) form.reset({ name: defaultName ?? '', phone1: defaultPhone1 ?? '', address: '' })
  }, [open, defaultName, defaultPhone1, form])

  const submit = form.handleSubmit(async (values) => {
    try {
      // Checked at submit time, not live-as-you-type: this is a create-only
      // dialog filled in seconds mid-call, and a per-keystroke query would
      // outrun its own debounce for no benefit — "before save" is exact here.
      if (await findDuplicateCustomerName(tenantId, branchId, values.name)) {
        form.setError('name', { message: 'يوجد عميل بهذا الاسم في هذا الفرع بالفعل' })
        return
      }
      form.clearErrors('name')
      const result = await create.mutateAsync({
        name: values.name,
        phone1: values.phone1,
        address: values.address || undefined,
        group_id: undefined,
        credit_limit: 0,
        branch_id: branchId,
      })
      toast.success('تم إنشاء العميل')
      onOpenChange(false)
      onCreated({
        id: result.id,
        name: values.name,
        phone1: values.phone1,
        address: values.address?.trim() ?? '',
        branchId,
      })
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
            <Label htmlFor="quick-add-customer-name">
              الاسم<span className="text-danger"> *</span>
            </Label>
            <Input id="quick-add-customer-name" autoFocus {...form.register('name')} />
            {form.formState.errors.name && (
              <p className="text-xs text-danger">{form.formState.errors.name.message}</p>
            )}
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="quick-add-customer-phone1">
              الهاتف<span className="text-danger"> *</span>
            </Label>
            <Input
              id="quick-add-customer-phone1"
              dir="ltr"
              className="text-start"
              {...form.register('phone1')}
            />
            {form.formState.errors.phone1 && (
              <p className="text-xs text-danger">{form.formState.errors.phone1.message}</p>
            )}
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="quick-add-customer-address">العنوان (اختياري)</Label>
            <Input id="quick-add-customer-address" {...form.register('address')} />
            {form.formState.errors.address && (
              <p className="text-xs text-danger">{form.formState.errors.address.message}</p>
            )}
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="quick-add-customer-branch">الفرع</Label>
            <Input id="quick-add-customer-branch" value={branchName} disabled readOnly />
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
