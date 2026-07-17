import { format, formatDistanceToNowStrict, isPast } from 'date-fns'
import { ar } from 'date-fns/locale'
import type { BranchStatus, DeviceStatus, SubscriptionState, TenantStatus } from './types'

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
