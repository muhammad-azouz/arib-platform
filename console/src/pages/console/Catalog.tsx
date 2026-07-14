import { PageHeader } from '@/components/PageHeader'
import { EmptyState } from '@/components/States'
import { CatalogIcon } from '@/components/icon'

// Slice 3 fills this in: groups → products, prices, barcodes, per-branch
// availability — read from the tenant central DB via the HQ chain.
export function Catalog() {
  return (
    <>
      <PageHeader
        title="الكتالوج"
        description="المجموعات والأصناف والأسعار عبر كل الفروع."
      />
      <EmptyState
        icon={CatalogIcon}
        title="الكتالوج"
        description="إدارة الأصناف والأسعار من المركز الرئيسي ستتوفر قريبًا."
      />
    </>
  )
}
