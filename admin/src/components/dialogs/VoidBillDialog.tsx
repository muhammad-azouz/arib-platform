import { useEffect } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { z } from 'zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { adminApi } from '@/lib/api'
import { errorMessage } from '@/lib/auth'
import { qk } from '@/lib/query'
import { fmtMoney } from '@/lib/format'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Field } from './Field'
import type { Bill } from '@/lib/types'

const schema = z.object({
  reason: z.string().trim().min(1, 'A reason is required'),
})
type Values = z.input<typeof schema>

interface Props {
  bill: Bill
  open: boolean
  onOpenChange: (open: boolean) => void
}

// Voiding is the correction lever for a mis-entered bill — it never deletes
// the record (append-only), and can downgrade an otherwise-active tenant
// straight to expired if this was its only covering bill. The required
// reason keeps that intentional.
export function VoidBillDialog({ bill, open, onOpenChange }: Props) {
  const qc = useQueryClient()
  const { register, handleSubmit, reset, formState: { errors } } = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: { reason: '' },
  })

  useEffect(() => {
    if (open) reset({ reason: '' })
  }, [open, reset])

  const mutation = useMutation({
    mutationFn: (v: Values) => adminApi.voidBill(bill.ID, v.reason),
    onSuccess: () => {
      toast.success('Bill voided')
      qc.invalidateQueries({ queryKey: qk.bills(bill.TenantID) })
      onOpenChange(false)
    },
    onError: (e) => toast.error(errorMessage(e)),
  })

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Void bill?</DialogTitle>
          <DialogDescription>
            {fmtMoney(bill.Amount, bill.Currency)} stays on record as void — it
            is never deleted. If this was the tenant's only covering bill,
            voiding it can downgrade their subscription state immediately.
          </DialogDescription>
        </DialogHeader>
        <form
          onSubmit={handleSubmit((v) => mutation.mutate(v))}
          className="grid gap-4"
        >
          <Field label="Reason" error={errors.reason?.message}>
            <Textarea {...register('reason')} placeholder="e.g. wrong period entered" />
          </Field>
          <DialogFooter>
            <Button
              type="button"
              variant="ghost"
              onClick={() => onOpenChange(false)}
            >
              Cancel
            </Button>
            <Button
              type="submit"
              variant="destructive"
              disabled={mutation.isPending}
            >
              {mutation.isPending ? 'Voiding…' : 'Void bill'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
