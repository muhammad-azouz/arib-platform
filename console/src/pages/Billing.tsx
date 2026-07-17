import { useTenants, useSubscription } from '@/lib/hooks'
import {
  fmtDate,
  fmtMoneyMinor,
  isZeroTime,
  subscriptionStateLabel,
  subscriptionStateTone,
  toArabicDigits,
} from '@/lib/format'
import { TopBar } from '@/components/TopBar'
import { RouteLoader } from '@/components/RouteLoader'
import { EmptyState, ErrorState, LoadingState } from '@/components/States'
import { InfoIcon, TenantIcon, WalletIcon } from '@/components/icon'
import { Badge } from '@/components/ui/badge'
import { Card } from '@/components/ui/card'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import type { SubscriptionSummary } from '@/lib/types'

/**
 * Account-level standalone page (reached from the Home tile), like /account
 * and /help — but with real content instead of a placeholder (T88). An
 * account owns exactly one tenant in practice (Tenants.tsx resolves the same
 * way), so this page reads tenants[0] rather than asking the user to pick.
 */
export function Billing() {
  const tenantsQuery = useTenants()
  const tenant = tenantsQuery.data?.[0]
  const subQuery = useSubscription(tenant?.ID)

  if (tenantsQuery.isLoading) return <RouteLoader label="جارٍ التحميل…" />

  return (
    <div className="min-h-screen">
      <TopBar subtitle="الفوترة" />
      <main className="mx-auto w-full max-w-3xl px-5 py-10 sm:py-14">
        <div className="animate-rise">
          <h1 className="font-display text-2xl font-extrabold tracking-tight">
            الفوترة والاشتراك
          </h1>
          <p className="mt-1 text-sm text-muted-foreground">
            حالة اشتراك المزامنة وسجل الفواتير المسجّلة على نشاطك.
          </p>

          {tenantsQuery.isError ? (
            <ErrorState
              className="mt-8"
              message={
                tenantsQuery.error instanceof Error
                  ? tenantsQuery.error.message
                  : undefined
              }
              onRetry={() => void tenantsQuery.refetch()}
            />
          ) : !tenant ? (
            <EmptyState
              className="mt-8"
              icon={TenantIcon}
              title="لا يوجد نشاط تجاري بعد"
              description="أنشئ نشاطك التجاري أولًا لعرض حالة الفوترة."
            />
          ) : (
            <>
              <section className="mt-8">
                {subQuery.isLoading ? (
                  <LoadingState rows={1} />
                ) : subQuery.data ? (
                  <StateCard summary={subQuery.data.summary} />
                ) : (
                  <ErrorState message="تعذّر تحميل حالة الاشتراك." />
                )}
              </section>

              <section className="mt-8">
                <h2 className="mb-3 font-display text-base font-bold">
                  سجل الفواتير
                </h2>
                <Card className="overflow-hidden p-0">
                  {!subQuery.data || (subQuery.data.bills ?? []).length === 0 ? (
                    <p className="py-10 text-center text-sm text-muted-foreground">
                      لا توجد فواتير مسجّلة بعد.
                    </p>
                  ) : (
                    <Table>
                      <TableHeader>
                        <TableRow className="hover:bg-transparent">
                          <TableHead>المبلغ</TableHead>
                          <TableHead>الفترة</TableHead>
                          <TableHead>الحالة</TableHead>
                        </TableRow>
                      </TableHeader>
                      <TableBody>
                        {(subQuery.data.bills ?? []).map((b) => (
                          <TableRow key={b.ID} className="hover:bg-transparent">
                            <TableCell className="font-medium tabular-nums">
                              {fmtMoneyMinor(b.Amount, b.Currency)}
                            </TableCell>
                            <TableCell className="text-sm text-muted-foreground">
                              {fmtDate(b.StartsAt)} – {fmtDate(b.EndsAt)}
                            </TableCell>
                            <TableCell>
                              <Badge tone={b.Status === 'paid' ? 'success' : 'muted'}>
                                {b.Status === 'paid' ? 'مدفوعة' : 'ملغاة'}
                              </Badge>
                            </TableCell>
                          </TableRow>
                        ))}
                      </TableBody>
                    </Table>
                  )}
                </Card>
              </section>

              {/* Placeholder — owner-supplied payment instructions (bank/wallet
                  details) are not wired in yet; content here is a stand-in. */}
              <section className="mt-8">
                <h2 className="mb-3 font-display text-base font-bold">
                  طريقة الدفع
                </h2>
                <Card className="flex items-start gap-3 p-4 text-sm text-muted-foreground">
                  <InfoIcon className="mt-0.5 size-5 shrink-0 text-info" />
                  <p>
                    لتجديد الاشتراك أو الاستفسار عن الفوترة، تواصل مع فريق الدعم
                    وسنرسل لك تفاصيل الدفع وتأكيد السداد.
                  </p>
                </Card>
              </section>
            </>
          )}
        </div>
      </main>
    </div>
  )
}

function StateCard({ summary }: { summary: SubscriptionSummary }) {
  const message = (() => {
    switch (summary.state) {
      case 'active':
        return `اشتراك المزامنة نشط حتى ${fmtDate(summary.ends_at)}.`
      case 'expiring':
        return `ينتهي اشتراك المزامنة خلال ${toArabicDigits(summary.days_left)} يوم، بتاريخ ${fmtDate(summary.ends_at)}. جدّد الاشتراك لتجنّب توقف المزامنة.`
      case 'grace':
        return `انتهت فترة الاشتراك، والمزامنة تعمل حاليًا ضمن فترة سماح حتى ${fmtDate(summary.grace_until)}. جدّد الاشتراك الآن لتفادي توقف المزامنة.`
      case 'expired':
        return `توقفت المزامنة — انتهى الاشتراك ولم يُجدَّد خلال فترة السماح. سجّل فاتورة جديدة لاستئناف المزامنة.`
      default:
        return 'فعّل اشتراك المزامنة لربط أجهزة الفروع ومزامنة بياناتها.'
    }
  })()

  return (
    <Card
      className={
        summary.state === 'grace' || summary.state === 'expired'
          ? 'flex items-start gap-3 border-danger/30 bg-danger/5 p-4'
          : summary.state === 'expiring'
            ? 'flex items-start gap-3 border-warning/30 bg-warning/5 p-4'
            : 'flex items-start gap-3 p-4'
      }
    >
      <div className="grid size-10 shrink-0 place-items-center rounded-xl bg-accent text-primary">
        <WalletIcon className="size-5" />
      </div>
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="font-display text-sm font-bold">حالة الاشتراك</span>
          <Badge tone={subscriptionStateTone(summary.state)}>
            {subscriptionStateLabel(summary.state)}
          </Badge>
        </div>
        <p className="mt-1 text-sm text-foreground/80">{message}</p>
        {!isZeroTime(summary.ends_at) && summary.state === 'expired' && (
          <p className="mt-0.5 text-xs text-muted-foreground">
            {`انتهى في ${fmtDate(summary.ends_at)}`}
          </p>
        )}
      </div>
    </Card>
  )
}
