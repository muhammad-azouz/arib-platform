import type { Branch } from '@/lib/types'
import { cn } from '@/lib/utils'

// Duplicated rather than imported from a shared spot — same call as every
// other native-select page in this console (Orders.tsx, Catalog's group/kind
// selects): one small string isn't worth a shared component.
const selectClass =
  'flex h-9 w-full rounded-md border border-input bg-background/40 px-3 py-1 text-sm shadow-sm transition-colors focus-visible:outline-none focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/30'

/**
 * The order's fulfilling branch. Deliberately changeable at any point
 * (T21's own acceptance) — this is a plain controlled `<select>`, not a
 * one-shot picker, so switching it mid-cart is just a prop change the page
 * reacts to (re-running availability in place), never a reset.
 */
export function BranchSelector({
  branches,
  value,
  onChange,
  className,
}: {
  branches: Branch[]
  value: string | undefined
  onChange: (branchId: string | undefined) => void
  className?: string
}) {
  const active = branches.filter((b) => b.Status === 'active')
  return (
    <select
      className={cn(selectClass, 'sm:w-56', className)}
      value={value ?? ''}
      onChange={(e) => onChange(e.target.value || undefined)}
    >
      <option value="">اختر الفرع المسؤول عن التنفيذ</option>
      {active.map((b) => (
        <option key={b.ID} value={b.ID}>
          {b.Name}
        </option>
      ))}
    </select>
  )
}
