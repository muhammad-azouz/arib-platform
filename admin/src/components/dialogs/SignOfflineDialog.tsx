import { useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { Check, Copy } from 'lucide-react'
import { toast } from 'sonner'
import { adminApi } from '@/lib/api'
import { errorMessage } from '@/lib/auth'
import type { License } from '@/lib/types'
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

interface Props {
  license: License
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function SignOfflineDialog({ license, open, onOpenChange }: Props) {
  // The parent mounts this dialog fresh per license (keyed), so local state
  // starts clean on each open — no reset effect needed.
  const [machineId, setMachineId] = useState('')
  const [token, setToken] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)

  const mutation = useMutation({
    mutationFn: () => adminApi.signOffline(license.ID, machineId.trim()),
    onSuccess: (t) => {
      setToken(t)
      toast.success('Offline license generated')
    },
    onError: (e) => toast.error(errorMessage(e)),
  })

  async function copy() {
    if (!token) return
    await navigator.clipboard.writeText(token)
    setCopied(true)
    toast.success('License string copied')
    setTimeout(() => setCopied(false), 1200)
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Offline license string</DialogTitle>
          <DialogDescription>
            Signs an offline fallback for the hidden manual-entry screen in the
            POS app. Bound to the machine ID you enter.
          </DialogDescription>
        </DialogHeader>

        {!token ? (
          <form
            onSubmit={(e) => {
              e.preventDefault()
              if (machineId.trim()) mutation.mutate()
            }}
            className="grid gap-4"
          >
            <Field label="Machine ID" hint="From the POS activation screen.">
              <Input
                autoFocus
                placeholder="e.g. 3F2A…"
                value={machineId}
                onChange={(e) => setMachineId(e.target.value)}
                className="font-mono"
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
                type="submit"
                disabled={mutation.isPending || !machineId.trim()}
              >
                {mutation.isPending ? 'Signing…' : 'Generate'}
              </Button>
            </DialogFooter>
          </form>
        ) : (
          <div className="grid gap-3">
            <Textarea
              readOnly
              value={token}
              className="h-40 resize-none font-mono text-xs"
              onFocus={(e) => e.currentTarget.select()}
            />
            <DialogFooter>
              <Button variant="ghost" onClick={() => onOpenChange(false)}>
                Close
              </Button>
              <Button onClick={copy}>
                {copied ? (
                  <Check className="size-4" />
                ) : (
                  <Copy className="size-4" />
                )}
                Copy string
              </Button>
            </DialogFooter>
          </div>
        )}
      </DialogContent>
    </Dialog>
  )
}
