import { useEffect, useMemo, useState, type KeyboardEvent as ReactKeyboardEvent } from 'react'
import * as DialogPrimitive from '@radix-ui/react-dialog'
import { useNavigate, useParams } from 'react-router-dom'
import { useBundle, useCatalogProducts } from '@/lib/hooks'
import { cn } from '@/lib/utils'
import { DialogOverlay, DialogTitle, DialogDescription } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import {
  AddIcon,
  BellIcon,
  BranchIcon,
  CatalogIcon,
  CompanyIcon,
  DashboardIcon,
  DownloadIcon,
  InventoryIcon,
  ReportsIcon,
  SearchIcon,
  SettingsIcon,
  type IconComponent,
} from '@/components/icon'

interface PaletteItem {
  id: string
  label: string
  sublabel?: string
  icon: IconComponent
  to: string
}
interface PaletteSection {
  key: string
  label: string
  items: PaletteItem[]
}

const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad|iPod/.test(navigator.platform)

/**
 * Search-button trigger + the palette itself. Self-contained: owns its own
 * open state and the global Ctrl+K/Cmd+K listener, so mounting it once in
 * AppShell is enough. Built directly on the Radix Dialog primitive rather
 * than the shared `ui/dialog` wrapper (top-aligned, wider, no visible
 * title/footer chrome) — no new dependency (no `cmdk`).
 */
export function CommandPalette() {
  const [open, setOpen] = useState(false)

  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      // The shortcut always wins, even while focus is inside a text input —
      // there's no console feature that also wants Ctrl+K.
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault()
        setOpen(true)
      }
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [])

  return (
    <DialogPrimitive.Root open={open} onOpenChange={setOpen}>
      <DialogPrimitive.Trigger asChild>
        <Button
          variant="outline"
          size="sm"
          className="gap-2 text-muted-foreground"
          aria-label="بحث سريع"
        >
          <SearchIcon className="size-4" />
          <span className="hidden sm:inline">بحث سريع</span>
          <kbd className="dir-ltr hidden rounded border border-border bg-muted px-1.5 py-0.5 font-mono text-[10px] sm:inline">
            {isMac ? '⌘K' : 'Ctrl K'}
          </kbd>
        </Button>
      </DialogPrimitive.Trigger>
      <DialogPrimitive.Portal>
        <DialogOverlay />
        <DialogPrimitive.Content
          className={cn(
            'fixed inset-x-0 top-[12vh] z-50 mx-auto w-full max-w-xl overflow-hidden rounded-xl border border-border bg-popover shadow-2xl',
            'data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95',
          )}
        >
          <DialogTitle className="sr-only">لوحة الأوامر</DialogTitle>
          <DialogDescription className="sr-only">
            ابحث عن صفحة أو فرع أو منتج للانتقال إليه مباشرة
          </DialogDescription>
          {/* Unmounts with the dialog (Radix's default), so query/selection
              always reset the next time it opens — no explicit reset code. */}
          {open && <PaletteBody onClose={() => setOpen(false)} />}
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  )
}

function PaletteBody({ onClose }: { onClose: () => void }) {
  const { tenantId } = useParams<'tenantId'>()
  const navigate = useNavigate()
  const base = `/tenants/${tenantId}`

  const [query, setQuery] = useState('')
  const [debouncedQuery, setDebouncedQuery] = useState('')
  const [activeIndex, setActiveIndex] = useState(0)

  useEffect(() => {
    const t = window.setTimeout(() => setDebouncedQuery(query.trim()), 300)
    return () => window.clearTimeout(t)
  }, [query])

  const { data: bundle } = useBundle(tenantId)
  const productSearch = debouncedQuery.length >= 2 ? debouncedQuery : undefined
  const productsQuery = useCatalogProducts(
    productSearch ? tenantId : undefined,
    { search: productSearch, page: 1, pageSize: 8 },
  )

  const q = query.trim().toLocaleLowerCase('ar')

  const sections = useMemo<PaletteSection[]>(() => {
    const matches = (label: string) => q === '' || label.toLocaleLowerCase('ar').includes(q)
    const result: PaletteSection[] = []

    const pages: PaletteItem[] = [
      { id: 'page-overview', label: 'نظرة عامة', icon: DashboardIcon, to: base },
      { id: 'page-branches', label: 'الفروع', icon: BranchIcon, to: `${base}/branches` },
      { id: 'page-catalog', label: 'الكتالوج', icon: CatalogIcon, to: `${base}/catalog` },
      { id: 'page-inventory', label: 'المخزون', icon: InventoryIcon, to: `${base}/inventory` },
      { id: 'page-reports', label: 'التقارير', icon: ReportsIcon, to: `${base}/reports` },
      { id: 'page-company', label: 'النشاط التجاري', icon: CompanyIcon, to: `${base}/company` },
      { id: 'page-download', label: 'تنزيل التطبيق', icon: DownloadIcon, to: `${base}/download` },
      { id: 'page-settings', label: 'الإعدادات', icon: SettingsIcon, to: `${base}/settings` },
      { id: 'page-conflicts', label: 'التنبيهات والتعارضات', icon: BellIcon, to: `${base}/conflicts` },
    ].filter((p) => matches(p.label))
    if (pages.length > 0) result.push({ key: 'pages', label: 'الصفحات', items: pages })

    const branches: PaletteItem[] = (bundle?.Branches ?? [])
      .filter((b) => matches(b.Name))
      .map((b) => ({
        id: `branch-${b.ID}`,
        label: b.Name,
        icon: BranchIcon,
        to: `${base}/branches/${b.ID}`,
      }))
    if (branches.length > 0) result.push({ key: 'branches', label: 'الفروع', items: branches })

    if (productSearch) {
      const items = productsQuery.data?.data.items ?? []
      const products: PaletteItem[] = items.map((p) => ({
        id: `product-${p.id}`,
        label: p.name,
        sublabel: `#${p.code}`,
        icon: CatalogIcon,
        to: `${base}/catalog/${p.id}`,
      }))
      products.push({
        id: 'product-search-more',
        label: 'بحث في الكتالوج…',
        icon: SearchIcon,
        to: `${base}/catalog?search=${encodeURIComponent(productSearch)}`,
      })
      result.push({ key: 'products', label: 'المنتجات', items: products })
    }

    const actions: PaletteItem[] = [
      { id: 'action-download', label: 'تنزيل التطبيق', icon: DownloadIcon, to: `${base}/download` },
      { id: 'action-add-branch', label: 'إضافة فرع', icon: AddIcon, to: `${base}/branches` },
      { id: 'action-add-product', label: 'إضافة منتج', icon: AddIcon, to: `${base}/catalog` },
    ].filter((a) => matches(a.label))
    if (actions.length > 0) result.push({ key: 'actions', label: 'إجراءات', items: actions })

    return result
  }, [base, bundle, productSearch, productsQuery.data, q])

  const flat = useMemo(() => sections.flatMap((s) => s.items), [sections])

  // The result set changed under the highlight — snap back to the top
  // instead of pointing at a stale/missing index. Adjusted during render
  // (React's recommended pattern, not an effect) so it can't cascade an
  // extra render the way setState-in-effect would — same pattern Catalog.tsx
  // and Inventory.tsx use for their own filter-driven page resets.
  const resultsKey = flat.map((i) => i.id).join('\0')
  const [lastResultsKey, setLastResultsKey] = useState(resultsKey)
  if (resultsKey !== lastResultsKey) {
    setLastResultsKey(resultsKey)
    setActiveIndex(0)
  }

  const select = (item: PaletteItem) => {
    navigate(item.to)
    onClose()
  }

  const onKeyDown = (e: ReactKeyboardEvent<HTMLInputElement>) => {
    if (flat.length === 0) return
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      setActiveIndex((i) => (i + 1) % flat.length)
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      setActiveIndex((i) => (i - 1 + flat.length) % flat.length)
    } else if (e.key === 'Enter') {
      e.preventDefault()
      const item = flat[activeIndex]
      if (item) select(item)
    }
  }

  let index = -1

  return (
    <div>
      <div className="flex items-center gap-2 border-b border-border px-4">
        <SearchIcon className="size-4 shrink-0 text-muted-foreground" />
        <input
          autoFocus
          role="combobox"
          aria-expanded="true"
          aria-controls="palette-listbox"
          aria-activedescendant={flat[activeIndex] ? `palette-option-${flat[activeIndex].id}` : undefined}
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={onKeyDown}
          placeholder="ابحث عن صفحة أو فرع أو منتج…"
          className="h-12 w-full border-0 bg-transparent text-sm outline-none placeholder:text-muted-foreground/70"
        />
      </div>

      <div id="palette-listbox" role="listbox" className="max-h-[60vh] overflow-y-auto p-2">
        {flat.length === 0 ? (
          <p className="px-2 py-6 text-center text-sm text-muted-foreground">
            {productSearch && productsQuery.isLoading ? 'جارٍ البحث…' : 'لا نتائج مطابقة'}
          </p>
        ) : (
          sections.map((section) => (
            <div key={section.key} className="mb-2 last:mb-0">
              <div className="px-2 py-1 text-xs font-medium uppercase tracking-wide text-muted-foreground">
                {section.label}
              </div>
              {section.items.map((item) => {
                index += 1
                const isActive = index === activeIndex
                const IconCmp = item.icon
                return (
                  <button
                    key={item.id}
                    id={`palette-option-${item.id}`}
                    role="option"
                    aria-selected={isActive}
                    type="button"
                    onMouseEnter={() => setActiveIndex(index)}
                    onClick={() => select(item)}
                    className={cn(
                      'flex w-full items-center gap-2.5 rounded-lg px-2.5 py-2 text-start text-sm transition-colors',
                      isActive ? 'bg-accent text-accent-foreground' : 'text-foreground/90',
                    )}
                  >
                    <IconCmp className="size-4 shrink-0 text-muted-foreground" />
                    <span className="min-w-0 flex-1 truncate">{item.label}</span>
                    {item.sublabel && (
                      <span className="dir-ltr shrink-0 font-mono text-xs text-muted-foreground">
                        {item.sublabel}
                      </span>
                    )}
                  </button>
                )
              })}
            </div>
          ))
        )}
      </div>
    </div>
  )
}
