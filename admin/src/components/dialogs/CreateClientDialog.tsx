import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { z } from 'zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { UserPlus } from 'lucide-react'
import { toast } from 'sonner'
import { useNavigate } from 'react-router-dom'
import { adminApi } from '@/lib/api'
import { errorMessage } from '@/lib/auth'
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
  DialogTrigger,
} from '@/components/ui/dialog'
import { Field } from './Field'

const schema = z.object({
  email: z.string().email('Enter a valid email'),
  first_name: z.string().optional(),
  last_name: z.string().optional(),
  notes: z.string().optional(),
})
type Values = z.infer<typeof schema>

export function CreateClientDialog() {
  const [open, setOpen] = useState(false)
  const qc = useQueryClient()
  const navigate = useNavigate()
  const {
    register,
    handleSubmit,
    reset,
    formState: { errors },
  } = useForm<Values>({ resolver: zodResolver(schema) })

  const mutation = useMutation({
    mutationFn: (v: Values) =>
      adminApi.createClient({
        email: v.email,
        first_name: (v.first_name ?? '').trim(),
        last_name: (v.last_name ?? '').trim(),
        notes: (v.notes ?? '').trim(),
      }),
    onSuccess: (account) => {
      toast.success('Client ready')
      qc.invalidateQueries({ queryKey: ['clients'] })
      setOpen(false)
      reset()
      navigate(`/clients/${account.ID}`)
    },
    onError: (e) => toast.error(errorMessage(e)),
  })

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        setOpen(o)
        if (!o) reset()
      }}
    >
      <DialogTrigger asChild>
        <Button>
          <UserPlus className="size-4" />
          Add client
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Add client</DialogTitle>
          <DialogDescription>
            Creates the account if the email is new. No trial is auto-issued for
            admin-created clients.
          </DialogDescription>
        </DialogHeader>
        <form
          onSubmit={handleSubmit((v) => mutation.mutate(v))}
          className="grid gap-4"
        >
          <Field label="Email" error={errors.email?.message}>
            <Input placeholder="owner@business.com" {...register('email')} />
          </Field>
          <div className="grid grid-cols-2 gap-3">
            <Field label="First name">
              <Input {...register('first_name')} />
            </Field>
            <Field label="Last name">
              <Input {...register('last_name')} />
            </Field>
          </div>
          <Field label="Notes">
            <Textarea
              placeholder="Internal notes (replaces ad-hoc notes)"
              {...register('notes')}
            />
          </Field>
          <DialogFooter>
            <Button
              type="button"
              variant="ghost"
              onClick={() => setOpen(false)}
            >
              Cancel
            </Button>
            <Button type="submit" disabled={mutation.isPending}>
              {mutation.isPending ? 'Creating…' : 'Create client'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
