import { useEffect } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { z } from 'zod'
import { toast } from 'sonner'
import { errorMessage } from '@/lib/auth'
import { useUpdateBranch } from '@/lib/hooks'
import type { Branch } from '@/lib/types'
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

const schema = z.object({
  name: z.string().trim().min(2, 'اسم الفرع مطلوب (حرفان على الأقل)').max(120),
})
type Form = z.infer<typeof schema>

export function RenameBranchDialog({
  tenantId,
  branch,
  open,
  onOpenChange,
}: {
  tenantId: string
  branch: Branch | null
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const update = useUpdateBranch(tenantId)
  const form = useForm<Form>({
    resolver: zodResolver(schema),
    defaultValues: { name: '' },
  })

  // Seed the field with the branch name each time the dialog opens.
  useEffect(() => {
    if (open && branch) form.reset({ name: branch.Name })
  }, [open, branch, form])

  const submit = form.handleSubmit(async ({ name }) => {
    if (!branch) return
    try {
      await update.mutateAsync({ branchId: branch.ID, name })
      toast.success('تم تغيير اسم الفرع')
      onOpenChange(false)
    } catch (err) {
      toast.error(errorMessage(err))
    }
  })

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>إعادة تسمية الفرع</DialogTitle>
        </DialogHeader>
        <form onSubmit={submit} className="space-y-4" noValidate>
          <div className="space-y-1.5">
            <Label htmlFor="rename-branch">اسم الفرع</Label>
            <Input id="rename-branch" autoFocus {...form.register('name')} />
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
              حفظ
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
