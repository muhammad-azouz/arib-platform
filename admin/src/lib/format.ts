import { format, formatDistanceToNowStrict, isPast } from 'date-fns'
import type {
  Account,
  DeviceStatus,
  LicenseStatus,
  LicenseType,
  SubscriptionState,
  TenantStatus,
} from './types'

const ZERO = '0001-01-01T00:00:00Z'

export function isZeroTime(iso?: string | null): boolean {
  return !iso || iso.startsWith('0001-01-01')
}

export function fmtDate(iso?: string | null): string {
  if (isZeroTime(iso)) return '—'
  return format(new Date(iso as string), 'dd MMM yyyy')
}

export function fmtDateTime(iso?: string | null): string {
  if (isZeroTime(iso)) return '—'
  return format(new Date(iso as string), 'dd MMM yyyy · HH:mm')
}

export function relative(iso?: string | null): string {
  if (isZeroTime(iso)) return '—'
  return `${formatDistanceToNowStrict(new Date(iso as string))} ago`
}

export function isExpired(iso?: string | null): boolean {
  if (isZeroTime(iso)) return false
  return isPast(new Date(iso as string))
}

export function fullName(a: Pick<Account, 'FirstName' | 'LastName'>): string {
  const n = `${a.FirstName ?? ''} ${a.LastName ?? ''}`.trim()
  return n || '—'
}

export type Tone = 'neutral' | 'success' | 'warning' | 'danger' | 'info' | 'muted'

export function licenseStatusTone(s: LicenseStatus): Tone {
  return s === 'active' ? 'success' : s === 'suspended' ? 'danger' : 'neutral'
}

export function licenseTypeTone(t: LicenseType): Tone {
  return t === 'paid' ? 'info' : 'warning'
}

export function deviceStatusTone(s: DeviceStatus): Tone {
  return s === 'active' ? 'success' : 'neutral'
}

export function tenantStatusTone(s: TenantStatus): Tone {
  return s === 'active' ? 'success' : 'danger'
}

/** amount is minor units (e.g. piasters); one currency unit = 100 minor units. */
export function fmtMoney(amountMinor: number, currency: string): string {
  return new Intl.NumberFormat('en', { style: 'currency', currency }).format(
    amountMinor / 100,
  )
}

const SUBSCRIPTION_LABELS: Record<SubscriptionState, string> = {
  none: 'No subscription',
  active: 'Active',
  expiring: 'Expiring soon',
  grace: 'Grace period',
  expired: 'Expired',
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

const ACTION_LABELS: Record<string, string> = {
  assign_licenses: 'Assigned licenses',
  set_license_status: 'Changed license status',
  force_release: 'Force-released device',
  sign_offline: 'Signed offline string',
  update_client: 'Updated client',
}

export function actionLabel(action: string): string {
  return ACTION_LABELS[action] ?? action.replace(/_/g, ' ')
}

export { ZERO }
