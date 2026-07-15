import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { z } from 'zod'
import { toast } from 'sonner'
import { useNavigate } from 'react-router-dom'
import { ApiError } from '@/lib/api'
import { errorMessage } from '@/lib/auth'
import { useCatalogGroups, useCreateProduct } from '@/lib/hooks'
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

const KIND_OPTIONS = [
  { value: 0, label: 'منتج مخزون' },
  { value: 1, label: 'خدمة مباعة' },
  { value: 2, label: 'خدمة مشتراة' },
]

// v1 keeps this minimal, matching EditUnitPriceDialog's same scope decision:
// exactly one unit (factor fixed at 1 — no sub-unit hierarchy), Sale/Buy
// only (no price tiers), one optional barcode, no opening balance.
const schema = z.object({
  name: z.string().trim().min(1, 'اسم الصنف مطلوب').max(120, 'الاسم طويل جدًا'),
  groupId: z.string().optional(),
  kind: z.number(),
  unitName: z.string().trim().min(1, 'اسم الوحدة مطلوب').max(50, 'الاسم طويل جدًا'),
  buy: z.number('أدخل رقمًا صحيحًا').min(0, 'السعر يجب ألا يكون سالبًا'),
  sale: z.number('أدخل رقمًا صحيحًا').min(0, 'السعر يجب ألا يكون سالبًا'),
  barcode: z.string().trim().max(50, 'الرمز طويل جدًا').optional(),
})
type Form = z.infer<typeof schema>

/** Create-product dialog («منتج جديد» on the Catalog page). On success,
 * navigates straight to the new product's detail page with the write's
 * `written_at` seeded into router state so its propagation panel (T25's
 * component, shared) shows immediately. */
export function CreateProductDialog({
  tenantId,
  open,
  onOpenChange,
}: {
  tenantId: string
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const navigate = useNavigate()
  const groupsQuery = useCatalogGroups(tenantId)
  const create = useCreateProduct(tenantId)
  const form = useForm<Form>({
    resolver: zodResolver(schema),
    defaultValues: { name: '', groupId: '', kind: 0, unitName: 'قطعة', buy: 0, sale: 0, barcode: '' },
  })

  const submit = form.handleSubmit(async (values) => {
    try {
      const result = await create.mutateAsync({
        name: values.name,
        kind: values.kind,
        group_id: values.groupId || undefined,
        units: [
          {
            name: values.unitName,
            val_sub: 1,
            buy: values.buy,
            sale: values.sale,
            barcodes: values.barcode ? [values.barcode] : undefined,
          },
        ],
      })
      toast.success('تم إنشاء الصنف')
      onOpenChange(false)
      form.reset()
      navigate(`/tenants/${tenantId}/catalog/${result.id}`, {
        state: { writtenAt: result.written_at },
      })
    } catch (err) {
      if (err instanceof ApiError && err.status === 409) {
        toast.error('رمز الباركود مستخدم بالفعل في صنف آخر — جرّب رمزًا مختلفًا')
      } else {
        toast.error(errorMessage(err))
      }
    }
  })

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>منتج جديد</DialogTitle>
        </DialogHeader>
        <form onSubmit={submit} className="space-y-4" noValidate>
          <div className="space-y-1.5">
            <Label htmlFor="new-product-name">
              اسم الصنف<span className="text-danger"> *</span>
            </Label>
            <Input id="new-product-name" autoFocus {...form.register('name')} />
            {form.formState.errors.name && (
              <p className="text-xs text-danger">{form.formState.errors.name.message}</p>
            )}
          </div>

          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1.5">
              <Label htmlFor="new-product-group">المجموعة</Label>
              <select
                id="new-product-group"
                className="flex h-9 w-full rounded-md border border-input bg-background/40 px-3 py-1 text-sm shadow-sm transition-colors focus-visible:outline-none focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/30"
                {...form.register('groupId')}
              >
                <option value="">بدون مجموعة</option>
                {(groupsQuery.data?.data ?? []).map((g) => (
                  <option key={g.id} value={g.id}>
                    {g.name}
                  </option>
                ))}
              </select>
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="new-product-kind">النوع</Label>
              <select
                id="new-product-kind"
                className="flex h-9 w-full rounded-md border border-input bg-background/40 px-3 py-1 text-sm shadow-sm transition-colors focus-visible:outline-none focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/30"
                {...form.register('kind', { valueAsNumber: true })}
              >
                {KIND_OPTIONS.map((k) => (
                  <option key={k.value} value={k.value}>
                    {k.label}
                  </option>
                ))}
              </select>
            </div>
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="new-product-unit">
              الوحدة<span className="text-danger"> *</span>
            </Label>
            <Input id="new-product-unit" {...form.register('unitName')} />
            {form.formState.errors.unitName && (
              <p className="text-xs text-danger">{form.formState.errors.unitName.message}</p>
            )}
          </div>

          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1.5">
              <Label htmlFor="new-product-buy">سعر الشراء</Label>
              <Input
                id="new-product-buy"
                type="number"
                step="0.01"
                min="0"
                dir="ltr"
                className="text-start"
                {...form.register('buy', { valueAsNumber: true })}
              />
              {form.formState.errors.buy && (
                <p className="text-xs text-danger">{form.formState.errors.buy.message}</p>
              )}
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="new-product-sale">سعر البيع</Label>
              <Input
                id="new-product-sale"
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

          <div className="space-y-1.5">
            <Label htmlFor="new-product-barcode">الباركود (اختياري)</Label>
            <Input
              id="new-product-barcode"
              dir="ltr"
              className="text-start"
              {...form.register('barcode')}
            />
            {form.formState.errors.barcode && (
              <p className="text-xs text-danger">{form.formState.errors.barcode.message}</p>
            )}
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
              إنشاء الصنف
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
