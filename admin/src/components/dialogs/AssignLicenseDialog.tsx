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
  features: z.string().trim().min(1, 'Required'),
  expires_at: z.string().min(1, 'Pick an expiry date'),
  count: z.coerce.number().int().min(1, 'At least 1').max(50, 'Max 50'),
  notes: z.string().trim().optional().default(''),
})
type Values = z.input<typeof schema>

interface Props {
  email: string
  accountId: string
  open: boolean
  onOpenChange: (open: boolean) => void
}

function defaultExpiry(): string {
  const d = new Date()
  d.setFullYear(d.getFullYear() + 1)
  return d.toISOString().slice(0, 10)
}

export function AssignLicenseDialog({
  email,
  accountId,
  open,
  onOpenChange,
}: Props) {
  const qc = useQueryClient()
  const {
    register,
    handleSubmit,
    reset,
    formState: { errors },
  } = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: { features: 'Pro', count: 1, expires_at: defaultExpiry(), notes: '' },
  })

  useEffect(() => {
    if (open) {
      reset({ features: 'Pro', count: 1, expires_at: defaultExpiry(), notes: '' })
    }
  }, [open, reset])

  const mutation = useMutation({
    mutationFn: (v: Values) =>
      adminApi.assignLicenses({
        email,
        features: v.features,
        // Expire at end of the chosen day, in ISO form the Go API parses.
        expires_at: new Date(`${v.expires_at}T23:59:59Z`).toISOString(),
        count: Number(v.count),
        notes: v.notes ?? '',
      }),
    onSuccess: (lics) => {
      toast.success(
        `${lics.length} license${lics.length > 1 ? 's' : ''} assigned`,
      )
      qc.invalidateQueries({ queryKey: qk.client(accountId) })
      qc.invalidateQueries({ queryKey: qk.stats })
      onOpenChange(false)
    },
    onError: (e) => toast.error(errorMessage(e)),
  })

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Assign licenses</DialogTitle>
          <DialogDescription>
            Each license is one device seat for{' '}
            <span className="font-mono text-foreground/80">{email}</span>.
          </DialogDescription>
        </DialogHeader>
        <form
          onSubmit={handleSubmit((v) => mutation.mutate(v))}
          className="grid gap-4"
        >
          <div className="grid grid-cols-2 gap-3">
            <Field label="Features" error={errors.features?.message}>
              <Input {...register('features')} />
            </Field>
            <Field label="Seats" error={errors.count?.message}>
              <Input type="number" min={1} max={50} {...register('count')} />
            </Field>
          </div>
          <Field label="Expires" error={errors.expires_at?.message}>
            <Input
              type="date"
              min={new Date().toISOString().slice(0, 10)}
              {...register('expires_at')}
            />
          </Field>
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
              {mutation.isPending ? 'Assigning…' : 'Assign'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
