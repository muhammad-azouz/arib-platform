import { useEffect } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { z } from 'zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { adminApi } from '@/lib/api'
import { errorMessage } from '@/lib/auth'
import { qk } from '@/lib/query'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
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

const schema = z.object({
  amount: z.coerce.number().positive('Amount must be greater than zero'),
  currency: z.string().trim().min(1, 'Required').default('EGP'),
  starts_at: z.string().min(1, 'Required'),
  ends_at: z.string().min(1, 'Required'),
  notes: z.string().trim().optional().default(''),
})
type Values = z.input<typeof schema>

interface Props {
  tenantId: string
  tenantName: string
  accountId: string
  /** Current coverage end (yyyy-mm-dd), if any — seeds the suggested period. */
  defaultStartsAt: string
  open: boolean
  onOpenChange: (open: boolean) => void
}

function addYear(dateStr: string): string {
  const d = new Date(`${dateStr}T00:00:00Z`)
  d.setUTCFullYear(d.getUTCFullYear() + 1)
  return d.toISOString().slice(0, 10)
}

export function AddBillDialog({
  tenantId,
  tenantName,
  accountId,
  defaultStartsAt,
  open,
  onOpenChange,
}: Props) {
  const qc = useQueryClient()
  const defaults = (): Values => ({
    amount: 0,
    currency: 'EGP',
    starts_at: defaultStartsAt,
    ends_at: addYear(defaultStartsAt),
    notes: '',
  })
  const {
    register,
    handleSubmit,
    reset,
    formState: { errors },
  } = useForm<Values>({ resolver: zodResolver(schema), defaultValues: defaults() })

  useEffect(() => {
    if (open) reset(defaults())
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, reset, defaultStartsAt])

  const mutation = useMutation({
    mutationFn: (v: Values) =>
      adminApi.createBill(tenantId, {
        amount: Math.round(Number(v.amount) * 100), // major units -> minor
        currency: (v.currency ?? 'EGP').toUpperCase(),
        starts_at: new Date(`${v.starts_at}T00:00:00Z`).toISOString(),
        ends_at: new Date(`${v.ends_at}T23:59:59Z`).toISOString(),
        notes: v.notes ?? '',
      }),
    onSuccess: (r) => {
      if (r.provisioned) {
        toast.success('Bill recorded — sync provisioned for this tenant')
      } else if (r.provision_err) {
        toast.warning(
          `Bill recorded, but auto-provisioning failed (${r.provision_err}). Use the "Provision sync" button.`,
        )
      } else {
        toast.success('Bill recorded')
      }
      qc.invalidateQueries({ queryKey: qk.bills(tenantId) })
      qc.invalidateQueries({ queryKey: qk.client(accountId) })
      onOpenChange(false)
    },
    onError: (e) => toast.error(errorMessage(e)),
  })

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Record a bill</DialogTitle>
          <DialogDescription>
            Records a payment already received against{' '}
            <span className="font-mono text-foreground/80">{tenantName}</span>.
            A tenant with no central DB yet is auto-provisioned.
          </DialogDescription>
        </DialogHeader>
        <form
          onSubmit={handleSubmit((v) => mutation.mutate(v))}
          className="grid gap-4"
        >
          <div className="grid grid-cols-3 gap-3">
            <Field label="Amount" error={errors.amount?.message}>
              <Input type="number" step="0.01" min={0} {...register('amount')} />
            </Field>
            <Field label="Currency" error={errors.currency?.message}>
              <Input {...register('currency')} className="uppercase" />
            </Field>
          </div>
          <div className="grid grid-cols-2 gap-3">
            <Field label="Period starts" error={errors.starts_at?.message}>
              <Input type="date" {...register('starts_at')} />
            </Field>
            <Field label="Period ends" error={errors.ends_at?.message}>
              <Input type="date" {...register('ends_at')} />
            </Field>
          </div>
          <Field label="Notes">
            <Textarea {...register('notes')} />
          </Field>
          <DialogFooter>
            <Button
              type="button"
              variant="ghost"
              onClick={() => onOpenChange(false)}
            >
              Cancel
            </Button>
            <Button type="submit" disabled={mutation.isPending}>
              {mutation.isPending ? 'Recording…' : 'Record bill'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
