import type { ReactNode } from 'react'
import { Label } from '@/components/ui/label'

interface FieldProps {
  label: string
  error?: string
  hint?: string
  children: ReactNode
}

export function Field({ label, error, hint, children }: FieldProps) {
  return (
    <div className="grid gap-1.5">
      <Label>{label}</Label>
      {children}
      {error ? (
        <p className="text-xs text-danger">{error}</p>
      ) : hint ? (
        <p className="text-xs text-muted-foreground">{hint}</p>
      ) : null}
    </div>
  )
}
