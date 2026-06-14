import { TopBar } from '@/components/TopBar'
import { Tile } from '@/components/Tile'
import { DashboardIcon, AccountIcon, BillingIcon, HelpIcon } from '@/components/icon'

export function Home() {
  return (
    <div className="min-h-screen">
      <TopBar />

      <main className="mx-auto w-full max-w-4xl px-5 py-10 sm:py-16">
        <div className="animate-rise">
          <h1 className="font-display text-3xl font-extrabold tracking-tight sm:text-4xl">
            أهلاً بك في أريب
          </h1>
          <p className="mt-2 text-muted-foreground">
            من هنا تدير نشاطك التجاري وفروعك وأجهزتك. اختر وجهتك.
          </p>

          <div className="mt-10 grid grid-cols-2 gap-4 sm:grid-cols-4">
            <Tile
              to="/tenants"
              icon={DashboardIcon}
              label="لوحة التحكم"
              hint="النشاط والفروع"
              primary
            />
            <Tile to="/account" icon={AccountIcon} label="حسابي" hint="بياناتك" />
            <Tile to="/billing" icon={BillingIcon} label="الفوترة" hint="الاشتراك" />
            <Tile to="/help" icon={HelpIcon} label="المساعدة" hint="الدعم" />
          </div>
        </div>
      </main>
    </div>
  )
}
