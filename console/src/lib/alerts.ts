import { relative, toArabicDigits } from './format'
import type { AttentionCounts, BranchView, SubscriptionSummary } from './types'

// One derived alert. The spec's rule: an alert with no destination doesn't
// ship — `to` is mandatory. Shared by the Overview panel and the
// notifications bell (T38) so both always agree, by construction.
export interface Alert {
  key: string
  tone: 'danger' | 'info'
  text: string
  to: string
}

export interface DeriveAlertsInput {
  branches: BranchView[]
  // Undefined while the attention/conflicts/subscription queries haven't
  // resolved yet — callers that mount cheaply (the bell) may omit them
  // entirely rather than block on extra requests.
  attention?: AttentionCounts
  conflictsUnacked?: number
  subscription?: SubscriptionSummary
}

/**
 * Ordered alert list: unacked sync conflicts and urgent stock (negative/out)
 * first (danger), then stale branches (danger), then low stock and
 * never-connected branches (info). Danger before info, otherwise stable
 * within each tone so the bell and Overview render identical order.
 */
export function deriveAlerts(tenantId: string, input: DeriveAlertsInput): Alert[] {
  const alerts: Alert[] = []

  if (input.conflictsUnacked && input.conflictsUnacked > 0) {
    alerts.push({
      key: 'conflicts',
      tone: 'danger',
      text: `${toArabicDigits(input.conflictsUnacked)} تعارض مزامنة بحاجة إلى مراجعة`,
      to: `/tenants/${tenantId}/conflicts`,
    })
  }

  if (input.subscription?.state === 'grace' || input.subscription?.state === 'expired') {
    alerts.push({
      key: 'subscription',
      tone: 'danger',
      text:
        input.subscription.state === 'grace'
          ? 'اشتراك المزامنة في فترة سماح — جدّد الآن لتفادي توقف المزامنة'
          : 'توقفت المزامنة — انتهى اشتراكك ولم يُجدَّد',
      to: '/billing',
    })
  }

  if (input.attention) {
    const urgent = input.attention.negative + input.attention.out
    if (urgent > 0) {
      alerts.push({
        key: 'stock-urgent',
        tone: 'danger',
        text: `${toArabicDigits(urgent)} صنف نفد أو بكمية سالبة`,
        to: `/tenants/${tenantId}/inventory?view=attention`,
      })
    }
  }

  for (const v of input.branches) {
    if (v.health === 'stale') {
      alerts.push({
        key: `stale-${v.id}`,
        tone: 'danger',
        text: `${v.name}: منقطع عن المزامنة — آخر مزامنة ${relative(v.last_sync_at)}`,
        to: `/tenants/${tenantId}/branches/${v.id}`,
      })
    }
  }

  if (input.attention && input.attention.low > 0) {
    alerts.push({
      key: 'stock-low',
      tone: 'info',
      text: `${toArabicDigits(input.attention.low)} صنف اقترب من حد إعادة الطلب`,
      to: `/tenants/${tenantId}/inventory?view=attention`,
    })
  }

  if (input.subscription?.state === 'expiring') {
    alerts.push({
      key: 'subscription',
      tone: 'info',
      text: `ينتهي اشتراك المزامنة خلال ${toArabicDigits(input.subscription.days_left)} يوم`,
      to: '/billing',
    })
  }

  for (const v of input.branches) {
    if (v.health === 'never') {
      alerts.push({
        key: `never-${v.id}`,
        tone: 'info',
        text: `${v.name}: لم يتصل بعد — ثبّت تطبيق سطح المكتب للبدء`,
        to: `/tenants/${tenantId}/download`,
      })
    }
  }

  return alerts
}
