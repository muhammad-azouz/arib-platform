import { useEffect } from 'react'
import { useForm } from 'react-hook-form'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { adminApi } from '@/lib/api'
import { errorMessage } from '@/lib/auth'
import { qk } from '@/lib/query'
import type { Account } from '@/lib/types'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Field } from './Field'

interface Values {
  first_name: string
  last_name: string
  notes: string
}

interface Props {
  account: Account
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function EditClientDialog({ account, open, onOpenChange }: Props) {
  const qc = useQueryClient()
  const { register, handleSubmit, reset } = useForm<Values>({
    defaultValues: {
      first_name: account.FirstName,
      last_name: account.LastName,
      notes: account.Notes ?? '',
    },
  })

  useEffect(() => {
    if (open) {
      reset({
        first_name: account.FirstName,
        last_name: account.LastName,
        notes: account.Notes ?? '',
      })
    }
  }, [open, account, reset])

  const mutation = useMutation({
    mutationFn: (v: Values) => adminApi.updateClient(account.ID, v),
    onSuccess: () => {
      toast.success('Client updated')
      qc.invalidateQueries({ queryKey: qk.client(account.ID) })
      qc.invalidateQueries({ queryKey: ['clients'] })
      onOpenChange(false)
    },
    onError: (e) => toast.error(errorMessage(e)),
  })

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Edit client</DialogTitle>
        </DialogHeader>
        <form
          onSubmit={handleSubmit((v) => mutation.mutate(v))}
          className="grid gap-4"
        >
          <div className="grid grid-cols-2 gap-3">
            <Field label="First name">
              <Input {...register('first_name')} />
            </Field>
            <Field label="Last name">
              <Input {...register('last_name')} />
            </Field>
          </div>
          <Field label="Notes">
            <Textarea className="min-h-[96px]" {...register('notes')} />
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
              {mutation.isPending ? 'Saving…' : 'Save changes'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
