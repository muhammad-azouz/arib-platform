/**
 * "Pending branch sync" bookkeeping for branch staff edits.
 *
 * A staff change lands in the tenant's central DB immediately, but the branch's
 * AribOne app only receives it on its next sync round (about every 5 minutes,
 * or when it is next online). The gateway keeps no per-row "last changed"
 * timestamp for users, so the console remembers, per browser, when IT saved each
 * staff member and compares that with the branch's last completed sync. That
 * covers the case that matters — the manager who just made the edit and wonders
 * "is it live at the branch yet?" — and honestly says nothing about edits made
 * from the branch itself or from another browser.
 *
 * Conservative on purpose: a round that completed shortly AFTER the save may
 * have pulled before the write landed, so a sync only clears the badge once it
 * finished at least SAFETY_MS after the save. Entries expire after a day.
 */

const SAFETY_MS = 2 * 60 * 1000
const TTL_MS = 24 * 60 * 60 * 1000

const key = (tenantId: string) => `arib-console:staff-saved:${tenantId}`

function read(tenantId: string): Record<string, number> {
  try {
    const raw = localStorage.getItem(key(tenantId))
    if (!raw) return {}
    const parsed: unknown = JSON.parse(raw)
    if (typeof parsed !== 'object' || parsed === null) return {}
    const now = Date.now()
    const out: Record<string, number> = {}
    for (const [id, at] of Object.entries(parsed)) {
      if (typeof at === 'number' && now - at < TTL_MS) out[id] = at
    }
    return out
  } catch {
    return {} // storage blocked or corrupt: the badge is a convenience, never required
  }
}

/** Records that this browser just saved `staffId`. */
export function markStaffSaved(tenantId: string, staffId: string, at: number = Date.now()): void {
  try {
    const map = read(tenantId)
    map[staffId] = at
    localStorage.setItem(key(tenantId), JSON.stringify(map))
  } catch {
    // ignore — see read()
  }
}

/** Pure rule: pending until a sync completed at least SAFETY_MS after the save. */
export function isSyncPending(savedAt: number | undefined, lastSyncAt: number | undefined): boolean {
  if (savedAt === undefined) return false
  if (lastSyncAt === undefined) return true
  return lastSyncAt < savedAt + SAFETY_MS
}

/** Saved-at timestamps this browser holds for the tenant, keyed by staff id. */
export function savedStaffTimes(tenantId: string): Record<string, number> {
  return read(tenantId)
}
