import { toArabicDigits } from '@/lib/format'
import type { CartLine } from './OrderCart'

const money = new Intl.NumberFormat('ar', { maximumFractionDigits: 2 })

/** Cart totals footer: item count, subtotal, delivery fee (if any), total. */
export function OrderSummary({
  lines,
  deliveryFee,
}: {
  lines: CartLine[]
  deliveryFee?: string
}) {
  const itemCount = lines.reduce((sum, l) => sum + l.qty, 0)
  const subtotal = lines.reduce((sum, l) => sum + l.qty * l.price, 0)
  const fee = deliveryFee ? Number(deliveryFee) || 0 : 0
  const total = subtotal + fee

  return (
    <div className="space-y-1.5 border-t border-border px-4 py-3 text-sm">
      <div className="flex items-center justify-between text-muted-foreground">
        <span>عدد الأصناف</span>
        <span className="tabular-nums">{toArabicDigits(itemCount)}</span>
      </div>
      <div className="flex items-center justify-between text-muted-foreground">
        <span>الإجمالي الفرعي</span>
        <span className="tabular-nums">{money.format(subtotal)}</span>
      </div>
      {fee > 0 && (
        <div className="flex items-center justify-between text-muted-foreground">
          <span>رسوم التوصيل</span>
          <span className="tabular-nums">{money.format(fee)}</span>
        </div>
      )}
      <div className="flex items-center justify-between border-t border-border pt-1.5 font-display text-base font-bold">
        <span>الإجمالي</span>
        <span className="tabular-nums">{money.format(total)}</span>
      </div>
    </div>
  )
}
