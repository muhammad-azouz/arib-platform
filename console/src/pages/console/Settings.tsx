import { PageHeader } from '@/components/PageHeader'
import { EmptyState } from '@/components/States'
import { SettingsIcon } from '@/components/icon'

export function Settings() {
  return (
    <>
      <PageHeader title="الإعدادات" description="تفضيلات الحساب والاشتراك." />
      <EmptyState
        icon={SettingsIcon}
        title="الإعدادات"
        description="ستتوفر خيارات الإعداد لاحقًا."
      />
    </>
  )
}
