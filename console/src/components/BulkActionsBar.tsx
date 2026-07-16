import { useState } from 'react'
import { toast } from 'sonner'
import { errorMessage } from '@/lib/auth'
import { useBulkUpdateCustomers, useCustomerGroups } from '@/lib/hooks'
import { toArabicDigits } from '@/lib/format'
import { CloseIcon } from '@/components/icon'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

const selectClass =
  'flex h-9 rounded-md border border-input bg-background/40 px-3 py-1 text-sm shadow-sm transition-colors focus-visible:outline-none focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/30'

/**
 * Bulk group-assign / pricing-tier toolbar for the selected customer rows
 * (T58's `PUT .../customers/bulk`, all-or-nothing on the gateway side). Each
 * action applies independently — picking a group doesn't require also
 * setting a price tier, and vice versa.
 */
export function BulkActionsBar({
  tenantId,
  selectedIds,
  onClear,
}: {
  tenantId: string
  selectedIds: string[]
  onClear: () => void
}) {
  const groupsQuery = useCustomerGroups(tenantId)
  const bulk = useBulkUpdateCustomers(tenantId)
  const [groupId, setGroupId] = useState('')
  const [priceTier, setPriceTier] = useState('')

  const applyGroup = async () => {
    if (!groupId) return
    try {
      const result = await bulk.mutateAsync({ ids: selectedIds, group_id: groupId })
      toast.success(`تم تعيين المجموعة لـ ${toArabicDigits(result.updated)} عميل`)
    } catch (err) {
      toast.error(errorMessage(err))
    }
  }

  const applyPriceTier = async () => {
    const tier = Number(priceTier)
    if (!priceTier || Number.isNaN(tier)) return
    try {
      const result = await bulk.mutateAsync({ ids: selectedIds, price_tier: tier })
      toast.success(`تم تحديث فئة السعر لـ ${toArabicDigits(result.updated)} عميل`)
    } catch (err) {
      toast.error(errorMessage(err))
    }
  }

  return (
    <div className="mb-4 flex flex-wrap items-center gap-3 rounded-xl border border-primary/30 bg-primary/5 px-4 py-3">
      <span className="text-sm font-medium">
        {toArabicDigits(selectedIds.length)} عميل مُحدد
      </span>

      <div className="flex items-center gap-1.5">
        <select className={selectClass} value={groupId} onChange={(e) => setGroupId(e.target.value)}>
          <option value="">اختر مجموعة</option>
          {(groupsQuery.data?.data ?? []).map((g) => (
            <option key={g.id} value={g.id}>
              {g.name}
            </option>
          ))}
        </select>
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={!groupId || bulk.isPending}
          onClick={() => void applyGroup()}
        >
          تعيين مجموعة
        </Button>
      </div>

      <div className="flex items-center gap-1.5">
        <Input
          type="number"
          min="0"
          step="1"
          dir="ltr"
          placeholder="فئة السعر"
          value={priceTier}
          onChange={(e) => setPriceTier(e.target.value)}
          className="h-9 w-28 text-start"
        />
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={!priceTier || bulk.isPending}
          onClick={() => void applyPriceTier()}
        >
          تحديث فئة السعر
        </Button>
      </div>

      <Button type="button" variant="ghost" size="sm" className="ms-auto" onClick={onClear}>
        <CloseIcon className="size-4" />
        إلغاء التحديد
      </Button>
    </div>
  )
}
