import { useEffect } from 'react'
import { useForm } from 'react-hook-form'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { adminApi } from '@/lib/api'
import { errorMessage } from '@/lib/auth'
import { qk } from '@/lib/query'
import { fmtDate } from '@/lib/format'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Field } from './Field'
import type { License } from '@/lib/types'

interface Values {
  updates_until: string // yyyy-mm-dd; blank = unlimited
}

interface Props {
  license: License
  accountId: string
  open: boolean
  onOpenChange: (open: boolean) => void
}

/** Renewal lever: releases published up to this date stay installable forever. */
export function ExtendUpdatesDialog({
  license,
  accountId,
  open,
  onOpenChange,
}: Props) {
  const qc = useQueryClient()
  const current = license.UpdatesUntil?.slice(0, 10) ?? ''
  const { register, handleSubmit, reset } = useForm<Values>({
    defaultValues: { updates_until: current },
  })

  useEffect(() => {
    if (open) reset({ updates_until: current })
  }, [open, reset, current])

  const mutation = useMutation({
    mutationFn: (v: Values) =>
      adminApi.extendUpdates(
        license.ID,
        // Blank = unlimited; otherwise the window closes at end of that day.
        v.updates_until
          ? new Date(`${v.updates_until}T23:59:59Z`).toISOString()
          : null,
      ),
    onSuccess: (l) => {
      toast.success(
        l.UpdatesUntil
          ? `Updates included until ${fmtDate(l.UpdatesUntil)}`
          : 'Updates set to unlimited',
      )
      qc.invalidateQueries({ queryKey: qk.client(accountId) })
      onOpenChange(false)
    },
    onError: (e) => toast.error(errorMessage(e)),
  })

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Updates window</DialogTitle>
          <DialogDescription>
            Releases published on or before this date are installable on{' '}
            <span className="font-mono text-foreground/80">{license.Key}</span>{' '}
            forever. Newer releases need a renewal.
          </DialogDescription>
        </DialogHeader>
        <form
          onSubmit={handleSubmit((v) => mutation.mutate(v))}
          className="grid gap-4"
        >
          <Field label="Updates until" hint="Blank = unlimited (grandfathered)">
            <Input type="date" {...register('updates_until')} />
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
              {mutation.isPending ? 'Saving…' : 'Save'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
