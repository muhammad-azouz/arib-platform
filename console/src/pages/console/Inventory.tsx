import { PageHeader } from '@/components/PageHeader'
import { EmptyState } from '@/components/States'
import { InventoryIcon } from '@/components/icon'

// Slice 4 fills this in: one dataset, three perspectives — by product, by
// branch, and "needs attention" (low/out-of-stock, negative, stale data).
export function Inventory() {
  return (
    <>
      <PageHeader
        title="المخزون"
        description="مخزون كل الفروع في مكان واحد."
      />
      <EmptyState
        icon={InventoryIcon}
        title="المخزون"
        description="متابعة المخزون عبر الفروع ستتوفر قريبًا."
      />
    </>
  )
}
