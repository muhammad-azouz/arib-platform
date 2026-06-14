import { Fragment } from 'react'
import { cn } from '@/lib/utils'
import { toArabicDigits } from '@/lib/format'
import { SuccessIcon } from '@/components/icon'

export interface Step {
  key: string
  label: string
}

/** Horizontal step indicator. RTL-aware via flex/logical layout. */
export function Stepper({ steps, current }: { steps: Step[]; current: number }) {
  return (
    <ol className="flex items-center">
      {steps.map((s, i) => {
        const done = i < current
        const active = i === current
        return (
          <Fragment key={s.key}>
            <li className="flex items-center gap-2">
              <span
                className={cn(
                  'grid size-8 shrink-0 place-items-center rounded-full border text-sm font-semibold transition-colors',
                  done && 'border-primary bg-primary text-primary-foreground',
                  active && 'border-primary text-primary',
                  !done && !active && 'border-border text-muted-foreground',
                )}
              >
                {done ? <SuccessIcon className="size-4" /> : toArabicDigits(i + 1)}
              </span>
              <span
                className={cn(
                  'text-sm',
                  active ? 'font-semibold text-foreground' : 'text-muted-foreground',
                )}
              >
                {s.label}
              </span>
            </li>
            {i < steps.length - 1 && (
              <span className="mx-3 h-px w-8 shrink-0 bg-border sm:w-12" />
            )}
          </Fragment>
        )
      })}
    </ol>
  )
}
