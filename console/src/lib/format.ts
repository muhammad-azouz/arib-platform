import { format, formatDistanceToNowStrict, isPast } from 'date-fns'
import { ar } from 'date-fns/locale'
import type {
  BranchStatus,
  DeviceStatus,
  DiscountTypeValue,
  MemberRole,
  OrderChannelValue,
  OrderModeValue,
  OrderStatusValue,
  PromotionLevelValue,
  PromotionStatus,
  SubscriptionState,
  TenantStatus,
} from './types'
import { DISCOUNT_TYPE, PROMOTION_LEVEL } from './types'

const ZERO = '0001-01-01T00:00:00Z'

export function isZeroTime(iso?: string | null): boolean {
  return !iso || iso.startsWith('0001-01-01')
}

export function fmtDate(iso?: string | null): string {
  if (isZeroTime(iso)) return '—'
  return format(new Date(iso as string), 'd MMM yyyy', { locale: ar })
}

export function fmtDateTime(iso?: string | null): string {
  if (isZeroTime(iso)) return '—'
  return format(new Date(iso as string), 'd MMM yyyy · HH:mm', { locale: ar })
}

/**
 * Formats a **date-only** value — one whose payload is the calendar date and
 * whose time component is meaningless (a promotion's starts_on/ends_on, spec
 * D8). Deliberately NOT `fmtDate`.
 *
 * `fmtDate` renders `new Date(iso)` in the browser's local timezone, which is
 * correct for an instant and wrong for a date: the gateway serializes these as
 * midnight UTC, so `2026-09-01T00:00:00Z` renders as **31 August** anywhere
 * west of Greenwich (verified: America/New_York). That is the same off-by-one
 * the gateway avoids by stamping rather than converting — reintroducing it in
 * the browser would undo the fix one layer later.
 *
 * Reading the date straight off the string takes the calendar date as written
 * and cannot shift it, whatever the viewer's timezone.
 */
export function fmtDateOnly(iso?: string | null): string {
  if (isZeroTime(iso)) return '—'
  const [y, m, d] = (iso as string).slice(0, 10).split('-').map(Number)
  if (!y || !m || !d) return '—'
  return format(new Date(y, m - 1, d), 'd MMM yyyy', { locale: ar })
}

/** The inverse of fmtDateOnly for a date input's `value` — same no-shift rule. */
export function toDateInput(iso?: string | null): string {
  if (isZeroTime(iso)) return ''
  return (iso as string).slice(0, 10)
}

/** A `YYYY-MM-DD` date input back to the midnight-UTC form the API expects. */
export function fromDateInput(value: string): string {
  return value ? `${value}T00:00:00Z` : ''
}

export function relative(iso?: string | null): string {
  if (isZeroTime(iso)) return '—'
  return `منذ ${formatDistanceToNowStrict(new Date(iso as string), { locale: ar })}`
}

export function isExpired(iso?: string | null): boolean {
  if (isZeroTime(iso)) return false
  return isPast(new Date(iso as string))
}

export type Tone = 'neutral' | 'success' | 'warning' | 'danger' | 'info' | 'muted'

// --- Arabic status labels + tones ---

export function tenantStatusLabel(s: TenantStatus): string {
  return s === 'active' ? 'نشط' : 'موقوف'
}
export function tenantStatusTone(s: TenantStatus): Tone {
  return s === 'active' ? 'success' : 'danger'
}

export function branchStatusLabel(s: BranchStatus): string {
  return s === 'active' ? 'مُفعّل' : 'مُعطّل'
}
export function branchStatusTone(s: BranchStatus): Tone {
  return s === 'active' ? 'success' : 'neutral'
}

export function deviceStatusLabel(s: DeviceStatus): string {
  return s === 'active' ? 'متصل' : 'مُحرّر'
}
export function deviceStatusTone(s: DeviceStatus): Tone {
  return s === 'active' ? 'success' : 'neutral'
}

export function memberRoleLabel(r: MemberRole): string {
  return r === 'owner' ? 'مالك' : 'عضو'
}
export function memberRoleTone(r: MemberRole): Tone {
  return r === 'owner' ? 'info' : 'muted'
}

const SUBSCRIPTION_LABELS: Record<SubscriptionState, string> = {
  none: 'بدون اشتراك',
  active: 'نشط',
  expiring: 'ينتهي قريبًا',
  grace: 'فترة سماح',
  expired: 'منتهي',
}
export function subscriptionStateLabel(s: SubscriptionState): string {
  return SUBSCRIPTION_LABELS[s]
}
export function subscriptionStateTone(s: SubscriptionState): Tone {
  switch (s) {
    case 'active':
      return 'success'
    case 'expiring':
      return 'warning'
    case 'grace':
    case 'expired':
      return 'danger'
    default:
      return 'neutral'
  }
}

// Order status/channel Arabic labels — mirror the desktop's
// Services/ExtensionMethods.cs ToArabic(OrderStatus)/ToArabic(OrderChannel)
// verbatim, since both sides describe the same numeric enum (T19, no
// JsonStringEnumConverter either side).
const ORDER_STATUS_LABELS: Record<OrderStatusValue, string> = {
  0: 'جديد',
  1: 'قيد التجهيز',
  2: 'جاهز',
  3: 'خرج للتوصيل',
  4: 'تم التسليم',
  5: 'ملغي',
  6: 'تم التحويل',
}
export function orderStatusLabel(s: OrderStatusValue): string {
  return ORDER_STATUS_LABELS[s]
}
export function orderStatusTone(s: OrderStatusValue): Tone {
  switch (s) {
    case 0:
      return 'info'
    case 1:
    case 2:
    case 3:
      return 'warning'
    case 4:
      return 'success'
    case 5:
      return 'danger'
    case 6:
      return 'neutral'
  }
}

const ORDER_CHANNEL_LABELS: Record<OrderChannelValue, string> = {
  1: 'كول سنتر',
  2: 'الفرع',
  3: 'المبيعات',
}
export function orderChannelLabel(c: OrderChannelValue): string {
  return ORDER_CHANNEL_LABELS[c]
}

const ORDER_MODE_LABELS: Record<OrderModeValue, string> = {
  1: 'استلام من الفرع',
  2: 'توصيل',
}
export function orderModeLabel(m: OrderModeValue): string {
  return ORDER_MODE_LABELS[m]
}

/** amount is minor units (e.g. piasters); one currency unit = 100 minor units. */
export function fmtMoneyMinor(amountMinor: number, currency: string): string {
  const digits = toArabicDigits((amountMinor / 100).toLocaleString('en', { maximumFractionDigits: 2 }))
  return `${digits} ${currency}`
}

/** Convert western digits to Arabic-Indic for display where appropriate. */
const ARABIC_DIGITS = ['٠', '١', '٢', '٣', '٤', '٥', '٦', '٧', '٨', '٩']
export function toArabicDigits(value: string | number): string {
  return String(value).replace(/[0-9]/g, (d) => ARABIC_DIGITS[Number(d)])
}

export { ZERO }

// --- Promotions (T138) ------------------------------------------------------

/**
 * Labels the **server-derived** status. There is deliberately no function here
 * that computes a status from the dates: the gateway's PromotionStatusOf is the
 * single definition of "active" (spec D8), shared with the till, and a second
 * client-side one is a second thing that can disagree with what a cashier is
 * actually giving away.
 */
export function promotionStatusLabel(s: PromotionStatus): string {
  switch (s) {
    case 'active':
      return 'نشط'
    case 'scheduled':
      return 'مجدول'
    case 'expired':
      return 'منتهي'
    case 'paused':
      return 'موقوف'
  }
}

export function promotionStatusTone(s: PromotionStatus): Tone {
  switch (s) {
    case 'active':
      return 'success'
    case 'scheduled':
      return 'info'
    case 'expired':
      return 'neutral'
    case 'paused':
      return 'muted'
  }
}

export function promotionLevelLabel(level: PromotionLevelValue): string {
  return level === PROMOTION_LEVEL.Bill ? 'خصم فاتورة' : 'خصم صنف'
}

/** "١٥٪" for a percentage, a plain money figure for a fixed amount. */
export function promotionValueLabel(
  discountType: DiscountTypeValue,
  value: number,
): string {
  const n = toArabicDigits(value.toLocaleString('en', { maximumFractionDigits: 2 }))
  return discountType === DISCOUNT_TYPE.Percentage ? `${n}٪` : n
}

/** Company-wide vs. one branch — the D2 distinction, read off a nullable id. */
export function promotionReachLabel(branchName?: string | null): string {
  return branchName ?? 'الشركة كلها'
}
