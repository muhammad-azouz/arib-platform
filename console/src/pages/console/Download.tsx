import { installerUrl } from '@/lib/api'
import { PageHeader } from '@/components/PageHeader'
import {
  DesktopIcon,
  DownloadIcon,
  DatabaseIcon,
  RuntimeIcon,
  ExternalLinkIcon,
} from '@/components/icon'
import { Card } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import type { IconComponent } from '@/components/icon'

const DOTNET_RUNTIME_URL = 'https://dotnet.microsoft.com/en-us/download/dotnet/10.0'
const SQL_SERVER_URL =
  'https://www.dropbox.com/scl/fi/cskx7otkdvmz7096kz65d/SQLEXPR_x64_ENU.zip?rlkey=dnyz3qmqj8pypavzez8uajapj&st=196pokkm&dl=1'

type Requirement = {
  icon: IconComponent
  title: string
  description: string
  href: string
  cta: string
}

const requirements: Requirement[] = [
  {
    icon: RuntimeIcon,
    title: '.NET 10 Runtime',
    description:
      'بيئة التشغيل التي يعمل عليها أريب. من صفحة مايكروسوفت اختر نسخة Windows x64.',
    href: DOTNET_RUNTIME_URL,
    cta: 'تنزيل من مايكروسوفت',
  },
  {
    icon: DatabaseIcon,
    title: 'SQL Server 2022',
    description:
      'قاعدة البيانات المحلية لتخزين بيانات الفرع. هذه النسخة مُجهّزة بإعداداتها مسبقًا، فقط ثبّتها بالخيارات الافتراضية دون أي تعديل.',
    href: SQL_SERVER_URL,
    cta: 'تنزيل SQL Server',
  },
]

export function Download() {
  return (
    <>
      <PageHeader
        title="تنزيل التطبيق"
        description="ثبّت متطلبات التشغيل مرة واحدة على كل جهاز فرع، ثم نزّل تطبيق أريب."
      />

      <div className="flex flex-col gap-4">
        <Card className="p-5">
          <h3 className="font-display text-sm font-bold text-muted-foreground">
            الخطوة ١ — متطلبات التشغيل
          </h3>
          <p className="mt-1 text-sm text-muted-foreground">
            برنامجان لا بد من تثبيتهما مرة واحدة على كل جهاز قبل تشغيل أريب لأول مرة.
          </p>

          <div className="mt-4 divide-y divide-border">
            {requirements.map(({ icon: Icon, title, description, href, cta }) => (
              <div
                key={title}
                className="flex flex-col gap-3 py-4 first:pt-0 last:pb-0 sm:flex-row sm:items-center sm:justify-between sm:gap-4"
              >
                <div className="flex items-start gap-3">
                  <div className="grid size-10 shrink-0 place-items-center rounded-xl bg-accent text-primary">
                    <Icon className="size-5" />
                  </div>
                  <div>
                    <h4 className="font-display text-sm font-bold">{title}</h4>
                    <p className="mt-0.5 max-w-sm text-sm text-muted-foreground">
                      {description}
                    </p>
                  </div>
                </div>

                <Button asChild variant="outline" size="sm" className="shrink-0 self-start sm:self-center">
                  <a href={href} target="_blank" rel="noopener noreferrer">
                    <ExternalLinkIcon className="size-4" />
                    {cta}
                  </a>
                </Button>
              </div>
            ))}
          </div>
        </Card>

        <Card className="flex flex-col items-center gap-5 p-8 text-center">
          <h3 className="font-display text-sm font-bold text-muted-foreground">
            الخطوة ٢ — تطبيق أريب
          </h3>

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
      </div>
    </>
  )
}
