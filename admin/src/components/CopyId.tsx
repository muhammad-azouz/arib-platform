import { useState } from 'react'
import { Check, Copy } from 'lucide-react'
import { toast } from 'sonner'
import { cn } from '@/lib/utils'

interface CopyIdProps {
  value: string
  label?: string
  className?: string
  truncate?: boolean
}

/** Monospace machine-readable value with click-to-copy — used for IDs, keys, tokens. */
export function CopyId({ value, label, className, truncate }: CopyIdProps) {
  const [copied, setCopied] = useState(false)

  async function copy() {
    try {
      await navigator.clipboard.writeText(value)
      setCopied(true)
      toast.success(`${label ?? 'Value'} copied`)
      setTimeout(() => setCopied(false), 1200)
    } catch {
      toast.error('Could not copy')
    }
  }

  return (
    <button
      type="button"
      onClick={copy}
      title={value}
      className={cn(
        'group inline-flex max-w-full items-center gap-1.5 rounded font-mono text-xs text-foreground/80 transition-colors hover:text-foreground',
        className,
      )}
    >
      <span className={cn(truncate && 'truncate')}>{value}</span>
      {copied ? (
        <Check className="size-3 shrink-0 text-success" />
      ) : (
        <Copy className="size-3 shrink-0 opacity-40 transition-opacity group-hover:opacity-100" />
      )}
    </button>
  )
}
