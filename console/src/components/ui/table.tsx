import * as React from 'react'
import { cn } from '@/lib/utils'

function Table({
  className,
  containerClassName,
  ...props
}: React.ComponentProps<'table'> & {
  /**
   * Sizing/overflow for the wrapper that actually scrolls. Needed because a
   * sticky `<thead>` sticks to its nearest scrollport — which is this wrapper,
   * never an ancestor — so a vertically scrolling table has to be constrained
   * here rather than on some outer box.
   */
  containerClassName?: string
}) {
  return (
    <div className={cn('relative w-full overflow-x-auto', containerClassName)}>
      <table
        className={cn('w-full caption-bottom text-sm', className)}
        {...props}
      />
    </div>
  )
}

function TableHeader({ className, ...props }: React.ComponentProps<'thead'>) {
  return (
    <thead
      className={cn('[&_tr]:border-b [&_tr]:border-border', className)}
      {...props}
    />
  )
}

function TableBody({ className, ...props }: React.ComponentProps<'tbody'>) {
  return (
    <tbody
      className={cn('[&_tr:last-child]:border-0', className)}
      {...props}
    />
  )
}

function TableRow({ className, ...props }: React.ComponentProps<'tr'>) {
  return (
    <tr
      className={cn(
        'border-b border-border/70 transition-colors hover:bg-accent/40 data-[state=selected]:bg-accent',
        className,
      )}
      {...props}
    />
  )
}

function TableHead({ className, ...props }: React.ComponentProps<'th'>) {
  return (
    <th
      className={cn(
        'h-10 px-4 text-start align-middle text-xs font-medium uppercase tracking-wide text-muted-foreground',
        className,
      )}
      {...props}
    />
  )
}

function TableCell({ className, ...props }: React.ComponentProps<'td'>) {
  return (
    <td
      className={cn('px-4 py-3 align-middle', className)}
      {...props}
    />
  )
}

export { Table, TableHeader, TableBody, TableRow, TableHead, TableCell }
