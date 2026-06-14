import type { ReactNode } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { z } from 'zod'
import { toast } from 'sonner'
import { errorMessage } from '@/lib/auth'
import { useSetCompany } from '@/lib/hooks'
import { newGuid } from '@/lib/utils'
import type { Company } from '@/lib/types'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'

const schema = z.object({
  name: z
    .string()
    .trim()
    .min(2, 'اسم النشاط مطلوب (حرفان على الأقل)')
    .max(120, 'الاسم طويل جدًا'),
  phone: z.string().trim().max(32, 'رقم هاتف غير صالح').optional(),
  address: z.string().trim().max(240, 'العنوان طويل جدًا').optional(),
  tax_number: z.string().trim().max(40, 'الرقم الضريبي طويل جدًا').optional(),
})
type Form = z.infer<typeof schema>

/**
 * One-company-per-tenant create/edit form (`PUT …/company`). Shared by the setup
 * wizard (first registration) and the steady-state company page (editing). On
 * first create we mint a lowercase GUID client-side — the cloud adopts it as the
 * SQL-Server `uniqueidentifier` PK; on edit we keep the existing id.
 */
export function CompanyForm({
  tenantId,
  company,
  submitLabel,
  onSaved,
}: {
  tenantId: string
  company: Company | null
  submitLabel: string
  onSaved?: (company: Company) => void
}) {
  const save = useSetCompany(tenantId)
  const form = useForm<Form>({
    resolver: zodResolver(schema),
    defaultValues: {
      name: company?.Name ?? '',
      phone: company?.Phone ?? '',
      address: company?.Address ?? '',
      tax_number: company?.TaxNumber ?? '',
    },
  })

  const submit = form.handleSubmit(async (values) => {
    try {
      const saved = await save.mutateAsync({
        id: company?.ID ?? newGuid(),
        name: values.name,
        phone: values.phone || undefined,
        address: values.address || undefined,
        tax_number: values.tax_number || undefined,
      })
      toast.success(company ? 'تم حفظ التغييرات' : 'تم تسجيل النشاط التجاري')
      form.reset({
        name: saved.Name,
        phone: saved.Phone ?? '',
        address: saved.Address ?? '',
        tax_number: saved.TaxNumber ?? '',
      })
      onSaved?.(saved)
    } catch (err) {
      toast.error(errorMessage(err))
    }
  })

  const busy = form.formState.isSubmitting

  return (
    <form onSubmit={submit} className="space-y-4" noValidate>
      <Field
        id="company-name"
        label="اسم النشاط التجاري"
        error={form.formState.errors.name?.message}
        required
      >
        <Input
          id="company-name"
          autoFocus
          placeholder="مثال: مؤسسة النور التجارية"
          {...form.register('name')}
        />
      </Field>

      <div className="grid gap-4 sm:grid-cols-2">
        <Field
          id="company-phone"
          label="رقم الهاتف"
          error={form.formState.errors.phone?.message}
        >
          <Input
            id="company-phone"
            type="tel"
            inputMode="tel"
            dir="ltr"
            className="text-start"
            placeholder="+966 5XX XXX XXX"
            {...form.register('phone')}
          />
        </Field>

        <Field
          id="company-tax"
          label="الرقم الضريبي"
          error={form.formState.errors.tax_number?.message}
        >
          <Input
            id="company-tax"
            dir="ltr"
            className="text-start font-mono"
            placeholder="3XXXXXXXXXXXXXX"
            {...form.register('tax_number')}
          />
        </Field>
      </div>

      <Field
        id="company-address"
        label="العنوان"
        error={form.formState.errors.address?.message}
      >
        <Textarea
          id="company-address"
          placeholder="الحي، الشارع، المدينة"
          {...form.register('address')}
        />
      </Field>

      <div className="flex justify-end">
        <Button type="submit" disabled={busy}>
          {submitLabel}
        </Button>
      </div>
    </form>
  )
}

function Field({
  id,
  label,
  error,
  required,
  children,
}: {
  id: string
  label: string
  error?: string
  required?: boolean
  children: ReactNode
}) {
  return (
    <div className="space-y-1.5">
      <Label htmlFor={id}>
        {label}
        {required && <span className="text-danger"> *</span>}
      </Label>
      {children}
      {error && <p className="text-xs text-danger">{error}</p>}
    </div>
  )
}
