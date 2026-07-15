import { useSearchParams } from 'react-router-dom'
import { cn } from '@/lib/utils'
import { Input } from '@/components/ui/input'

/**
 * Report period control: preset chips + custom from/to date inputs, all
 * URL-borne (`?from=&to=`, plain YYYY-MM-DD) so report views deep-link, same
 * pattern as Inventory's `?view=`. Dates are computed in the browser's local
 * calendar; the gateway interprets them in its own (tenant-region) day-scope —
 * the standing assumption that HQ staff and their branches share a region.
 * No params = the gateway's own default, the last 7 days.
 */

/** Local calendar date as YYYY-MM-DD (never toISOString — that shifts to UTC). */
function localISO(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

function daysAgo(n: number): Date {
  const d = new Date()
  d.setDate(d.getDate() - n)
  return d
}

const PRESETS: { key: string; label: string; range: () => { from: string; to: string } }[] = [
  {
    key: 'today',
    label: 'اليوم',
    range: () => ({ from: localISO(new Date()), to: localISO(new Date()) }),
  },
  {
    key: 'yesterday',
    label: 'أمس',
    range: () => ({ from: localISO(daysAgo(1)), to: localISO(daysAgo(1)) }),
  },
  {
    key: '7d',
    label: 'آخر ٧ أيام',
    range: () => ({ from: localISO(daysAgo(6)), to: localISO(new Date()) }),
  },
  {
    key: '30d',
    label: 'آخر ٣٠ يومًا',
    range: () => ({ from: localISO(daysAgo(29)), to: localISO(new Date()) }),
  },
  {
    key: 'month',
    label: 'هذا الشهر',
    range: () => {
      const now = new Date()
      return {
        from: localISO(new Date(now.getFullYear(), now.getMonth(), 1)),
        to: localISO(now),
      }
    },
  },
]

export function PeriodPicker({ className }: { className?: string }) {
  const [searchParams, setSearchParams] = useSearchParams()
  // Absent params mean the gateway default — identical to the 7-day preset,
  // which is therefore highlighted so the UI never shows "no period".
  const fallback = PRESETS[2].range()
  const from = searchParams.get('from') ?? fallback.from
  const to = searchParams.get('to') ?? fallback.to

  const setRange = (f: string, t: string) => {
    const next = new URLSearchParams(searchParams)
    next.set('from', f)
    next.set('to', t)
    setSearchParams(next, { replace: true })
  }

  return (
    <div className={cn('flex flex-wrap items-center gap-2', className)}>
      <div className="inline-flex rounded-lg border border-border bg-card/50 p-1">
        {PRESETS.map((p) => {
          const r = p.range()
          const active = r.from === from && r.to === to
          return (
            <button
              key={p.key}
              type="button"
              onClick={() => setRange(r.from, r.to)}
              className={cn(
                'rounded-md px-3 py-1.5 text-sm font-medium transition-colors',
                active ? 'bg-accent text-primary' : 'text-muted-foreground hover:text-foreground',
              )}
            >
              {p.label}
            </button>
          )
        })}
      </div>
      <div className="flex items-center gap-1.5 text-sm text-muted-foreground">
        <span>من</span>
        <Input
          type="date"
          value={from}
          max={to}
          onChange={(e) => e.target.value && setRange(e.target.value, to)}
          className="h-9 w-36"
        />
        <span>إلى</span>
        <Input
          type="date"
          value={to}
          min={from}
          max={localISO(new Date())}
          onChange={(e) => e.target.value && setRange(from, e.target.value)}
          className="h-9 w-36"
        />
      </div>
    </div>
  )
}
