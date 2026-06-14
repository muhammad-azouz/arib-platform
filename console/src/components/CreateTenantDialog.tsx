import { useEffect } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { z } from 'zod'
import { toast } from 'sonner'
import { errorMessage } from '@/lib/auth'
import { useCreateTenant } from '@/lib/hooks'
import type { Tenant } from '@/lib/types'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

const schema = z.object({
  name: z
    .string()
    .trim()
    .min(2, 'اسم النشاط مطلوب (حرفان على الأقل)')
    .max(80, 'الاسم طويل جدًا'),
})
type Form = z.infer<typeof schema>

/**
 * Create a tenant. On success the caller decides where to go — for first-time
 * onboarding that's straight into the setup wizard.
 */
export function CreateTenantDialog({
  open,
  onOpenChange,
  onCreated,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  onCreated: (tenant: Tenant) => void
}) {
  const create = useCreateTenant()
  const form = useForm<Form>({
    resolver: zodResolver(schema),
    defaultValues: { name: '' },
  })

  // Reset the field every time the dialog re-opens.
  useEffect(() => {
    if (open) form.reset({ name: '' })
  }, [open, form])

  const submit = form.handleSubmit(async ({ name }) => {
    try {
      const tenant = await create.mutateAsync(name)
      toast.success('تم إنشاء النشاط')
      onCreated(tenant)
    } catch (err) {
      toast.error(errorMessage(err))
    }
  })

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>نشاط تجاري جديد</DialogTitle>
          <DialogDescription>
            أدخل اسم نشاطك. يمكنك إضافة بيانات الشركة والفروع في الخطوة التالية.
          </DialogDescription>
        </DialogHeader>

        <form onSubmit={submit} className="space-y-4" noValidate>
          <div className="space-y-1.5">
            <Label htmlFor="tenant-name">اسم النشاط</Label>
            <Input
              id="tenant-name"
              autoFocus
              placeholder="مثال: مؤسسة النور التجارية"
              {...form.register('name')}
            />
            {form.formState.errors.name && (
              <p className="text-xs text-danger">{form.formState.errors.name.message}</p>
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
              إنشاء ومتابعة
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
