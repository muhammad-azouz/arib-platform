import { installerUrl } from '@/lib/api'
import { PageHeader } from '@/components/PageHeader'
import { DesktopIcon, DownloadIcon } from '@/components/icon'
import { Card } from '@/components/ui/card'
import { Button } from '@/components/ui/button'

export function Download() {
  return (
    <>
      <PageHeader
        title="تنزيل التطبيق"
        description="نزّل تطبيق أريب لسطح المكتب وشغّله على أجهزة فروعك."
      />

      <Card className="flex flex-col items-center gap-5 p-8 text-center">
        <div className="grid size-16 place-items-center rounded-2xl bg-accent text-primary">
          <DesktopIcon className="size-8" />
        </div>

        <div>
          <h3 className="font-display text-lg font-bold">أريب لويندوز</h3>
          <p className="mt-1.5 max-w-sm text-sm text-muted-foreground">
            بعد التثبيت، سجّل الدخول بنفس البريد الإلكتروني لحسابك هنا — سيتم
            ربط الجهاز بنشاطك التجاري تلقائيًا. التحديثات القادمة تُنزَّل
            وتُطبَّق من داخل التطبيق نفسه، دون الحاجة لتنزيل مُثبّت جديد.
          </p>
        </div>

        <Button asChild size="lg">
          <a href={installerUrl()}>
            <DownloadIcon className="size-4" />
            تنزيل للويندوز
          </a>
        </Button>
      </Card>
    </>
  )
}
