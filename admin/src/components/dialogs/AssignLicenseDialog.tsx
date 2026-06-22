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
import { MODULES, type ModuleCode } from '@/lib/types'

const schema = z.object({
  modules: z.array(z.string()).min(1, 'Pick at least one module'),
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

const defaultModules: ModuleCode[] = [...MODULES]

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
    watch,
    setValue,
    formState: { errors },
  } = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: { modules: defaultModules, count: 1, expires_at: '', notes: '' },
  })
  const selectedModules = watch('modules') ?? []

  useEffect(() => {
    if (open) {
      reset({ modules: defaultModules, count: 1, expires_at: '', notes: '' })
    }
  }, [open, reset])

  function toggleModule(m: ModuleCode) {
    const next = selectedModules.includes(m)
      ? selectedModules.filter((x) => x !== m)
      : [...selectedModules, m]
    setValue('modules', next, { shouldValidate: true })
  }

  const mutation = useMutation({
    mutationFn: (v: Values) =>
      adminApi.assignLicenses({
        email,
        modules: v.modules,
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
          <Field label="Modules" error={errors.modules?.message}>
            <div className="flex flex-wrap gap-3">
              {MODULES.map((m) => (
                <label
                  key={m}
                  className="flex items-center gap-1.5 text-sm capitalize"
                >
                  <input
                    type="checkbox"
                    checked={selectedModules.includes(m)}
                    onChange={() => toggleModule(m)}
                    className="size-4 rounded border-input"
                  />
                  {m}
                </label>
              ))}
            </div>
          </Field>
          <div className="grid grid-cols-2 gap-3">
            <Field label="Seats" error={errors.count?.message}>
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
