import { PageHeader } from '@/components/PageHeader'
import { EmptyState } from '@/components/States'
import { ReportsIcon } from '@/components/icon'

// Slice 6 fills this in: question-organized reports (المبيعات، الأصناف،
// الفروع، المخزون، الموظفون) over central-DB aggregates.
export function Reports() {
  return (
    <>
      <PageHeader
        title="التقارير"
        description="إجابات جاهزة عن أسئلة عملك."
      />
      <EmptyState
        icon={ReportsIcon}
        title="التقارير"
        description="تقارير المبيعات والأصناف والفروع ستتوفر قريبًا."
      />
    </>
  )
}
