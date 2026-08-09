import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { z } from 'zod'
import { toast } from 'sonner'
import { errorMessage } from '@/lib/auth'
import { useInviteMember } from '@/lib/hooks'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

const schema = z.object({
  email: z.string().min(1, 'البريد الإلكتروني مطلوب').email('بريد إلكتروني غير صالح'),
})
type Form = z.infer<typeof schema>

/**
 * Invite-by-email dialog (T14, `POST …/members`, owner-only server-side).
 * The invited email gets a bare Account now if it doesn't have one yet
 * (tenant/service_members.go InviteMember) — they reach the console with the
 * existing OTP sign-in flow, unchanged; there is no separate accept step.
 */
export function InviteMemberDialog({
  tenantId,
  open,
  onOpenChange,
}: {
  tenantId: string
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const invite = useInviteMember(tenantId)
  const form = useForm<Form>({
    resolver: zodResolver(schema),
    defaultValues: { email: '' },
  })

  const submit = form.handleSubmit(async (values) => {
    try {
      await invite.mutateAsync(values.email)
      toast.success('تم إرسال الدعوة')
      form.reset()
      onOpenChange(false)
    } catch (err) {
      toast.error(errorMessage(err))
    }
  })

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        if (!o) form.reset()
        onOpenChange(o)
      }}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle>دعوة عضو جديد</DialogTitle>
          <DialogDescription>
            يمكن للعضو المدعو الدخول إلى لوحة التحكم لهذا النشاط برمز تحقق يُرسل
            إلى بريده الإلكتروني.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={submit} className="space-y-4" noValidate>
          <div className="space-y-1.5">
            <Label htmlFor="member-email">
              البريد الإلكتروني<span className="text-danger"> *</span>
            </Label>
            <Input
              id="member-email"
              type="email"
              dir="ltr"
              className="text-start"
              autoFocus
              placeholder="name@example.com"
              {...form.register('email')}
            />
            {form.formState.errors.email && (
              <p className="text-xs text-danger">{form.formState.errors.email.message}</p>
            )}
          </div>
          <div className="flex justify-end">
            <Button type="submit" disabled={form.formState.isSubmitting}>
              إرسال الدعوة
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  )
}
