import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { z } from 'zod'
import { toast } from 'sonner'
import { errorMessage } from '@/lib/auth'
import { useAddBranch } from '@/lib/hooks'
import { newGuid } from '@/lib/utils'
import type { Branch } from '@/lib/types'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

// Phone1 + Address are NOT NULL on the POS branch (and print on receipts), so
// they are required here. Phone2/3 are optional. Max lengths mirror the SQL
// columns (Phone* 50, Address 150) so the cloud value never overflows on sync.
const schema = z.object({
  name: z
    .string()
    .trim()
    .min(2, 'اسم الفرع مطلوب (حرفان على الأقل)')
    .max(120, 'الاسم طويل جدًا'),
  address: z.string().trim().min(1, 'العنوان مطلوب').max(150, 'العنوان طويل جدًا'),
  phone1: z.string().trim().min(1, 'رقم الهاتف مطلوب').max(50, 'الرقم طويل جدًا'),
  phone2: z.string().trim().max(50, 'الرقم طويل جدًا').optional(),
  phone3: z.string().trim().max(50, 'الرقم طويل جدًا').optional(),
})
type Form = z.infer<typeof schema>

/**
 * Create-branch form (`POST …/branches`). Used both for the first branch in the
 * setup wizard and the "add branch" dialog on the branches page. The branch id
 * is a client-minted lowercase GUID (the cloud adopts it as the SQL PK).
 *
 * Seats (the device-seat limit) are a paid licensing lever owned by the platform
 * admin, so the merchant does NOT set them here — the server defaults a new
 * branch to 1 seat and an admin raises it when the tenant buys more.
 */
export function BranchForm({
  tenantId,
  companyId,
  submitLabel,
  onSaved,
}: {
  tenantId: string
  companyId: string
  submitLabel: string
  onSaved?: (branch: Branch) => void
}) {
  const add = useAddBranch(tenantId)
  const form = useForm<Form>({
    resolver: zodResolver(schema),
    defaultValues: { name: '', address: '', phone1: '', phone2: '', phone3: '' },
  })

  const submit = form.handleSubmit(async (values) => {
    try {
      const branch = await add.mutateAsync({
        id: newGuid(),
        company_id: companyId,
        name: values.name,
        address: values.address,
        phone1: values.phone1,
        phone2: values.phone2,
        phone3: values.phone3,
      })
      toast.success('تم إنشاء الفرع')
      onSaved?.(branch)
    } catch (err) {
      toast.error(errorMessage(err))
    }
  })

  return (
    <form onSubmit={submit} className="space-y-4" noValidate>
      <div className="space-y-1.5">
        <Label htmlFor="branch-name">
          اسم الفرع<span className="text-danger"> *</span>
        </Label>
        <Input
          id="branch-name"
          autoFocus
          placeholder="مثال: الفرع الرئيسي"
          {...form.register('name')}
        />
        {form.formState.errors.name && (
          <p className="text-xs text-danger">{form.formState.errors.name.message}</p>
        )}
      </div>

      <div className="space-y-1.5">
        <Label htmlFor="branch-address">
          العنوان<span className="text-danger"> *</span>
        </Label>
        <Input
          id="branch-address"
          placeholder="مثال: شارع جامعة الدول العربية، المهندسين"
          {...form.register('address')}
        />
        {form.formState.errors.address && (
          <p className="text-xs text-danger">{form.formState.errors.address.message}</p>
        )}
      </div>

      <div className="space-y-1.5">
        <Label htmlFor="branch-phone1">
          رقم الهاتف<span className="text-danger"> *</span>
        </Label>
        <Input
          id="branch-phone1"
          dir="ltr"
          inputMode="tel"
          className="text-start"
          placeholder="01000000000"
          {...form.register('phone1')}
        />
        {form.formState.errors.phone1 && (
          <p className="text-xs text-danger">{form.formState.errors.phone1.message}</p>
        )}
      </div>

      <div className="grid grid-cols-2 gap-3">
        <div className="space-y-1.5">
          <Label htmlFor="branch-phone2">هاتف ٢ (اختياري)</Label>
          <Input
            id="branch-phone2"
            dir="ltr"
            inputMode="tel"
            className="text-start"
            {...form.register('phone2')}
          />
          {form.formState.errors.phone2 && (
            <p className="text-xs text-danger">{form.formState.errors.phone2.message}</p>
          )}
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="branch-phone3">هاتف ٣ (اختياري)</Label>
          <Input
            id="branch-phone3"
            dir="ltr"
            inputMode="tel"
            className="text-start"
            {...form.register('phone3')}
          />
          {form.formState.errors.phone3 && (
            <p className="text-xs text-danger">{form.formState.errors.phone3.message}</p>
          )}
        </div>
      </div>

      <div className="flex justify-end">
        <Button type="submit" disabled={form.formState.isSubmitting}>
          {submitLabel}
        </Button>
      </div>
    </form>
  )
}
