import { useEffect } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { z } from 'zod'
import { toast } from 'sonner'
import { errorMessage } from '@/lib/auth'
import { useChangeProductPrices } from '@/lib/hooks'
import type { ProductUnit } from '@/lib/types'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

const schema = z.object({
  sale: z.number('أدخل رقمًا صحيحًا').min(0, 'السعر يجب ألا يكون سالبًا'),
  buy: z.number('أدخل رقمًا صحيحًا').min(0, 'السعر يجب ألا يكون سالبًا'),
})
type Form = z.infer<typeof schema>

/**
 * Edit affordance for one unit's Sale/Buy price (the first HQ write UX). On
 * success, calls `onWritten` with the gateway's `written_at` so the caller
 * can drive the propagation panel.
 */
export function EditUnitPriceDialog({
  tenantId,
  productId,
  unit,
  open,
  onOpenChange,
  onWritten,
}: {
  tenantId: string
  productId: string
  unit: ProductUnit | null
  open: boolean
  onOpenChange: (open: boolean) => void
  onWritten: (writtenAt: string) => void
}) {
  const change = useChangeProductPrices(tenantId, productId)
  const form = useForm<Form>({
    resolver: zodResolver(schema),
    defaultValues: { sale: 0, buy: 0 },
  })

  // Seed the fields with the unit's current prices each time the dialog opens.
  useEffect(() => {
    if (open && unit) form.reset({ sale: unit.sale, buy: unit.buy })
  }, [open, unit, form])

  const submit = form.handleSubmit(async ({ sale, buy }) => {
    if (!unit) return
    try {
      const result = await change.mutateAsync([{ unit_id: unit.id, sale, buy }])
      toast.success('تم تحديث السعر')
      onWritten(result.written_at)
      onOpenChange(false)
    } catch (err) {
      toast.error(errorMessage(err))
    }
  })

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>تعديل السعر{unit ? ` — ${unit.name}` : ''}</DialogTitle>
        </DialogHeader>
        <form onSubmit={submit} className="space-y-4" noValidate>
          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1.5">
              <Label htmlFor="unit-buy">سعر الشراء</Label>
              <Input
                id="unit-buy"
                type="number"
                step="0.01"
                min="0"
                dir="ltr"
                className="text-start"
                autoFocus
                {...form.register('buy', { valueAsNumber: true })}
              />
              {form.formState.errors.buy && (
                <p className="text-xs text-danger">{form.formState.errors.buy.message}</p>
              )}
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="unit-sale">سعر البيع</Label>
              <Input
                id="unit-sale"
                type="number"
                step="0.01"
                min="0"
                dir="ltr"
                className="text-start"
                {...form.register('sale', { valueAsNumber: true })}
              />
              {form.formState.errors.sale && (
                <p className="text-xs text-danger">{form.formState.errors.sale.message}</p>
              )}
            </div>
          </div>
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={() => onOpenChange(false)}
              disabled={form.formState.isSubmitting}
            >
              إلغاء
            </Button>
            <Button type="submit" disabled={form.formState.isSubmitting}>
              حفظ
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
