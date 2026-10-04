import { useQuery } from '@tanstack/react-query'
import { adminApi } from './api'
import { qk } from './query'
import type { License, ModuleDef } from './types'

/** The license module catalog, served by the API (single source of truth). */
export function useModuleCatalog() {
  return useQuery({
    queryKey: qk.modules,
    queryFn: adminApi.modules,
    staleTime: Infinity,
  })
}

/** Every module an admin can tick: everything except the always-on core. */
export function sellableCodes(catalog: ModuleDef[]): string[] {
  return catalog.filter((m) => m.kind !== 'core').map((m) => m.code)
}

/**
 * The ticked modules for an existing license. A license that predates the
 * catalog (no core "bills" code) was granted everything, so it opens fully
 * ticked — matching what the server mints for it.
 */
export function selectionFor(lic: License, catalog: ModuleDef[]): string[] {
  const mods = lic.Modules ?? []
  if (!mods.includes('bills')) return sellableCodes(catalog)
  const sellable = new Set(sellableCodes(catalog))
  return mods.filter((m) => sellable.has(m))
}

/** Short English summary of a license's modules for tables. */
export function describeModules(lic: License, catalog: ModuleDef[] | undefined): string {
  if (!catalog) return lic.Modules?.join(', ') || lic.Features
  const picked = new Set(selectionFor(lic, catalog))
  const names = catalog
    .filter((m) => picked.has(m.code))
    .map((m) =>
      m.valued && lic.Seats ? `${m.name_en} (${lic.Seats})` : m.name_en,
    )
  return names.length ? names.join(', ') : 'Bills only'
}
