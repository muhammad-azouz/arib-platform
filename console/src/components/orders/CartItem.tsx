import type { OrderAvailabilityLine } from '@/lib/types'
import { toArabicDigits } from '@/lib/format'
import { cn } from '@/lib/utils'
import { DeleteIcon, QtyDecreaseIcon, QtyIncreaseIcon } from '@/components/icon'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import type { CartLine } from './OrderCart'

const money = new Intl.NumberFormat('ar', { maximumFractionDigits: 2 })

/**
 * One cart line: qty stepper, an inline-editable price (D2 — the order's
 * `Price` is whatever the phone agent actually quotes, not a locked catalog
 * figure), and the availability read for this product at the chosen branch
 * (blank until a branch is picked — T21's own acceptance).
 */
export function CartItem({
  line,
  availability,
  hasBranch,
  onQtyChange,
  onPriceChange,
  onRemove,
}: {
  line: CartLine
  availability?: OrderAvailabilityLine
  hasBranch: boolean
  onQtyChange: (qty: number) => void
  onPriceChange: (price: number) => void
  onRemove: () => void
}) {
  // Only Product lines (kind 0) are ever stock-gated (D16) — a service line
  // never appears in the availability response at all (T17), so there's
  // nothing to render here for it, gated or not.
  const isGoods = line.kind === 0
  const short = isGoods && availability !== undefined && line.qty > availability.available

  return (
    <div className="rounded-lg border border-border bg-background/60 p-2.5">
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-medium">{line.productName}</div>
          <div className="text-xs text-muted-foreground">{line.unitName}</div>
        </div>
        <Button
          type="button"
          variant="ghost"
          size="icon"
          className="size-7 shrink-0 text-muted-foreground hover:text-danger"
          onClick={onRemove}
          aria-label="إزالة"
        >
          <DeleteIcon className="size-4" />
        </Button>
      </div>

      <div className="mt-2 flex items-center gap-2">
        <div className="flex items-center gap-1">
          <Button
            type="button"
            variant="outline"
            size="icon"
            className="size-7"
            disabled={line.qty <= 1}
            onClick={() => onQtyChange(line.qty - 1)}
          >
            <QtyDecreaseIcon className="size-4" />
          </Button>
          <span className="w-8 text-center text-sm tabular-nums">{toArabicDigits(line.qty)}</span>
          <Button
            type="button"
            variant="outline"
            size="icon"
            className="size-7"
            onClick={() => onQtyChange(line.qty + 1)}
          >
            <QtyIncreaseIcon className="size-4" />
          </Button>
        </div>

        <input
          type="number"
          step="0.01"
          min="0"
          dir="ltr"
          value={line.price}
          onChange={(e) => onPriceChange(Number(e.target.value))}
          className="h-7 w-20 rounded-md border border-input bg-background/40 px-1.5 text-center text-xs tabular-nums outline-none focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/30"
        />

        <span className="ms-auto text-sm font-semibold tabular-nums">
          {money.format(line.qty * line.price)}
        </span>
      </div>

      {isGoods && hasBranch && (
        <div className="mt-1.5">
          {availability ? (
            <Badge tone={short ? 'danger' : 'muted'} className={cn(short && 'font-semibold')}>
              متاح: {toArabicDigits(availability.available)}
            </Badge>
          ) : (
            <Badge tone="muted">جارٍ التحقق من المتاح…</Badge>
          )}
        </div>
      )}
    </div>
  )
}
