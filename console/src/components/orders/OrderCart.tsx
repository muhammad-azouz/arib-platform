import { useState } from 'react'
import type { OrderAvailabilityLine, OrderModeValue } from '@/lib/types'
import { ORDER_MODE } from '@/lib/types'
import { toArabicDigits } from '@/lib/format'
import { BranchIcon, CartIcon, DeliveryModeIcon, NotesIcon } from '@/components/icon'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { EmptyState } from '@/components/States'
import { CartItem } from './CartItem'
import { OrderSummary } from './OrderSummary'

// One line of the order being built. `kind` mirrors the desktop's
// ProductKind (0 = Product/inventory, 1 = SalesService, 2 = PurchaseService)
// — only Product lines are ever stock-gated (D16), matching T17's own
// "service kinds are never gated" rule.
export interface CartLine {
  productId: string
  productName: string
  unitId: string
  unitName: string
  kind: number
  qty: number
  price: number
}

/**
 * The cart pane: count badge, the two D2/D6-allowed quick actions
 * (توصيل, ملاحظات — deliberately no خصم/no generic خدمات quick action, D2's
 * Order schema has no discount column and a delivery fee is the only
 * "extra" an order carries), the line list, and the totals footer.
 */
export function OrderCart({
  lines,
  availability,
  hasBranch,
  mode,
  onModeChange,
  contactAddress,
  onContactAddressChange,
  contactAddressHint,
  deliveryFee,
  onDeliveryFeeChange,
  deliveryFeeHint,
  note,
  onNoteChange,
  onQtyChange,
  onPriceChange,
  onRemove,
}: {
  lines: CartLine[]
  availability: Map<string, OrderAvailabilityLine>
  hasBranch: boolean
  mode: OrderModeValue
  onModeChange: (mode: OrderModeValue) => void
  contactAddress: string
  onContactAddressChange: (value: string) => void
  // T98: mirrors deliveryFeeHint one-to-one — shown only while the field
  // still holds the resolved profile address; a manual edit clears it.
  contactAddressHint?: string
  deliveryFee: string
  onDeliveryFeeChange: (value: string) => void
  // T3b: which of the three layers produced the current number — shown
  // only while it's still the resolved value; a manual edit clears it (the
  // page owns that decision, this component only renders whatever it's given).
  deliveryFeeHint?: string
  note: string
  onNoteChange: (value: string) => void
  onQtyChange: (productId: string, unitId: string, qty: number) => void
  onPriceChange: (productId: string, unitId: string, price: number) => void
  onRemove: (productId: string, unitId: string) => void
}) {
  const [notesOpen, setNotesOpen] = useState(false)
  const isDelivery = mode === ORDER_MODE.Delivery

  return (
    <div className="flex h-full flex-col rounded-xl border border-border bg-card/50">
      <div className="flex items-center gap-2 border-b border-border px-4 py-3">
        <CartIcon className="size-5 text-muted-foreground" />
        <h2 className="font-display text-sm font-semibold">سلة الطلب</h2>
        <Badge tone="neutral" className="ms-auto">
          {toArabicDigits(lines.length)}
        </Badge>
      </div>

      <div className="flex flex-wrap items-center gap-3 border-b border-border px-4 py-2.5">
        <div className="flex items-center gap-2">
          <Label className="text-xs">طريقة الاستلام</Label>
          <div className="flex gap-1">
            <Button
              type="button"
              variant={isDelivery ? 'outline' : 'default'}
              size="sm"
              className="gap-1.5"
              onClick={() => onModeChange(ORDER_MODE.Pickup)}
            >
              <BranchIcon className="size-4" />
              استلام من الفرع
            </Button>
            <Button
              type="button"
              variant={isDelivery ? 'default' : 'outline'}
              size="sm"
              className="gap-1.5"
              onClick={() => onModeChange(ORDER_MODE.Delivery)}
            >
              <DeliveryModeIcon className="size-4" />
              توصيل
            </Button>
          </div>
        </div>
        <Button
          type="button"
          variant={notesOpen || note ? 'default' : 'outline'}
          size="sm"
          className="gap-1.5 ms-auto"
          onClick={() => setNotesOpen((v) => !v)}
        >
          <NotesIcon className="size-4" />
          ملاحظات
        </Button>
      </div>

      {isDelivery && (
        <div className="space-y-2 border-b border-border bg-accent/30 px-4 py-3">
          <div className="space-y-1">
            <Label htmlFor="cart-address" className="text-xs">
              عنوان التوصيل<span className="text-danger"> *</span>
            </Label>
            <Input
              id="cart-address"
              value={contactAddress}
              onChange={(e) => onContactAddressChange(e.target.value)}
              placeholder="العنوان بالتفصيل"
              className="h-8 text-sm"
            />
            {contactAddressHint && (
              <p className="text-xs text-muted-foreground">{contactAddressHint}</p>
            )}
          </div>
          <div className="space-y-1">
            <Label htmlFor="cart-delivery-fee" className="text-xs">
              رسوم التوصيل
            </Label>
            <Input
              id="cart-delivery-fee"
              type="number"
              step="0.01"
              min="0"
              dir="ltr"
              className="h-8 text-start text-sm"
              value={deliveryFee}
              onChange={(e) => onDeliveryFeeChange(e.target.value)}
            />
            {deliveryFeeHint && (
              <p className="text-xs text-muted-foreground">{deliveryFeeHint}</p>
            )}
          </div>
        </div>
      )}

      {notesOpen && (
        <div className="border-b border-border bg-accent/30 px-4 py-3">
          <Label htmlFor="cart-note" className="text-xs">
            ملاحظة على الطلب
          </Label>
          <Textarea
            id="cart-note"
            value={note}
            onChange={(e) => onNoteChange(e.target.value)}
            placeholder="أي تفاصيل إضافية للفرع…"
            className="mt-1 min-h-[60px] text-sm"
          />
        </div>
      )}

      <div className="min-h-0 flex-1 overflow-y-auto p-2">
        {lines.length === 0 ? (
          <EmptyState icon={CartIcon} title="السلة فارغة" description="أضف أصنافًا من الكتالوج." />
        ) : (
          <div className="space-y-1.5">
            {lines.map((line) => (
              <CartItem
                key={`${line.productId}:${line.unitId}`}
                line={line}
                availability={hasBranch ? availability.get(line.productId) : undefined}
                hasBranch={hasBranch}
                onQtyChange={(qty) => onQtyChange(line.productId, line.unitId, qty)}
                onPriceChange={(price) => onPriceChange(line.productId, line.unitId, price)}
                onRemove={() => onRemove(line.productId, line.unitId)}
              />
            ))}
          </div>
        )}
      </div>

      <OrderSummary lines={lines} deliveryFee={isDelivery ? deliveryFee : undefined} />
    </div>
  )
}
