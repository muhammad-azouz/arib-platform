import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { adminApi } from '@/lib/api'
import { errorMessage } from '@/lib/auth'
import { qk } from '@/lib/query'
import { selectionFor, useModuleCatalog } from '@/lib/modules'
import { ModuleTreePicker } from '@/components/ModuleTreePicker'
import { Button } from '@/components/ui/button'
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

interface Props {
  license: License
  accountId: string
  open: boolean
  onOpenChange: (open: boolean) => void
}

/**
 * Upgrade or downgrade an existing license's modules and terminal count.
 * Online devices pick the change up on their next check-in; offline-signed
 * devices need a fresh offline string.
 */
export function EditModulesDialog({
  license,
  accountId,
  open,
  onOpenChange,
}: Props) {
  const qc = useQueryClient()
  const { data: catalog } = useModuleCatalog()
  // Mounted per license (keyed) only while open, so local state starts fresh;
  // null = untouched, derived from the license once the catalog has loaded.
  const [edited, setModules] = useState<string[] | null>(null)
  const [seats, setSeats] = useState(license.Seats ?? 0)
  const modules = edited ?? (catalog ? selectionFor(license, catalog) : [])

  const mutation = useMutation({
    mutationFn: () => adminApi.updateLicenseModules(license.ID, modules, seats),
    onSuccess: () => {
      toast.success('Modules updated — devices pick it up on next check-in')
      qc.invalidateQueries({ queryKey: qk.client(accountId) })
      onOpenChange(false)
    },
    onError: (e) => toast.error(errorMessage(e)),
  })

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Edit modules</DialogTitle>
          <DialogDescription>
            License{' '}
            <span className="font-mono text-foreground/80">{license.Key}</span>.
            Removing a module locks its screens; no data is deleted.
          </DialogDescription>
        </DialogHeader>
        <div className="grid gap-4">
          <Field label="Modules" hint="Terminals count blank = desktop default">
            <ModuleTreePicker
              catalog={catalog}
              value={modules}
              onChange={setModules}
              seats={seats}
              onSeatsChange={setSeats}
            />
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
              type="button"
              disabled={!catalog || mutation.isPending}
              onClick={() => mutation.mutate()}
            >
              {mutation.isPending ? 'Saving…' : 'Save'}
            </Button>
          </DialogFooter>
        </div>
      </DialogContent>
    </Dialog>
  )
}
