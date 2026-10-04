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
import { ModuleTreePicker } from '@/components/ModuleTreePicker'
import { sellableCodes, useModuleCatalog } from '@/lib/modules'

const schema = z.object({
  modules: z.array(z.string()), // empty = Bills-only POS (core is implied)
  seats: z.coerce.number().int().min(0).max(100),
  expires_at: z.string().optional().default(''), // blank = perpetual
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

export function AssignLicenseDialog({
  email,
  accountId,
  open,
  onOpenChange,
}: Props) {
  const qc = useQueryClient()
  const { data: catalog } = useModuleCatalog()
  const {
    register,
    handleSubmit,
    reset,
    watch,
    setValue,
    formState: { errors },
  } = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: { modules: [], seats: 0, count: 1, expires_at: '', notes: '' },
  })
  const selectedModules = watch('modules') ?? []
  const seats = Number(watch('seats') ?? 0)

  // New licenses default to every module, as before; re-seeded once the
  // catalog has loaded.
  useEffect(() => {
    if (open) {
      reset({
        modules: catalog ? sellableCodes(catalog) : [],
        seats: 0,
        count: 1,
        expires_at: '',
        notes: '',
      })
    }
  }, [open, reset, catalog])

  const mutation = useMutation({
    mutationFn: (v: Values) =>
      adminApi.assignLicenses({
        email,
        modules: v.modules,
        seats: Number(v.seats),
        // Blank = perpetual; otherwise expire at end of the chosen day.
        expires_at: v.expires_at
          ? new Date(`${v.expires_at}T23:59:59Z`).toISOString()
          : null,
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
          <Field
            label="Modules"
            error={errors.modules?.message ?? errors.seats?.message}
            hint="Terminals count blank = desktop default"
          >
            <ModuleTreePicker
              catalog={catalog}
              value={selectedModules}
              onChange={(m) => setValue('modules', m, { shouldValidate: true })}
              seats={seats}
              onSeatsChange={(n) => setValue('seats', n, { shouldValidate: true })}
            />
          </Field>
          <div className="grid grid-cols-2 gap-3">
            <Field label="Licenses" error={errors.count?.message}>
              <Input type="number" min={1} max={50} {...register('count')} />
            </Field>
            <Field
              label="Expires"
              error={errors.expires_at?.message}
              hint="Blank = perpetual"
            >
              <Input
                type="date"
                min={new Date().toISOString().slice(0, 10)}
                {...register('expires_at')}
              />
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
              {mutation.isPending ? 'Assigning…' : 'Assign'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
