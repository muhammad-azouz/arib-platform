import { useEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { api, OrderUnavailableError } from '@/lib/api'
import { errorMessage } from '@/lib/auth'
import { qk } from '@/lib/query'
import {
  useBundle,
  useCatalogGroups,
  useCreateOrder,
  useCustomers,
  useMe,
  useOrderAvailability,
} from '@/lib/hooks'
import { toArabicDigits } from '@/lib/format'
import { ORDER_MODE, type OrderAvailabilityLine, type OrderModeValue, type OrderShortfall } from '@/lib/types'
import { PageHeader } from '@/components/PageHeader'
import { GroupDrill } from '@/components/GroupDrill'
import { BranchSelector } from '@/components/orders/BranchSelector'
import { ProductGrid } from '@/components/orders/ProductGrid'
import { OrderCart, type CartLine } from '@/components/orders/OrderCart'
import { DangerIcon, SearchIcon, UsersIcon, ZenCollapseIcon, ZenExpandIcon } from '@/components/icon'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

interface SelectedCustomer {
  id: string
  name: string
  phone1: string
}

export function NewOrder() {
  const { tenantId } = useParams<'tenantId'>()
  const { data: bundle } = useBundle(tenantId)
  const meQuery = useMe()
  const navigate = useNavigate()
  const qc = useQueryClient()

  // Branch selection is mirrored in ?branch= so a refresh survives — never
  // required to enter the screen (T21's own acceptance).
  const [searchParams, setSearchParams] = useSearchParams()
  const [branchId, setBranchId] = useState<string | undefined>(searchParams.get('branch') ?? undefined)
  const onBranchChange = (id: string | undefined) => {
    setBranchId(id)
    const next = new URLSearchParams(searchParams)
    if (id) next.set('branch', id)
    else next.delete('branch')
    setSearchParams(next, { replace: true })
  }

  const [groupId, setGroupId] = useState<string | undefined>(undefined)
  const groupsQuery = useCatalogGroups(tenantId)

  const [cart, setCart] = useState<CartLine[]>([])
  const [mode, setMode] = useState<OrderModeValue>(ORDER_MODE.Pickup)
  const [contactAddress, setContactAddress] = useState('')
  const [deliveryFee, setDeliveryFee] = useState('')
  const [note, setNote] = useState('')
  const [zen, setZen] = useState(false)
  const [shortfalls, setShortfalls] = useState<OrderShortfall[]>([])

  // Esc exits zen mode; body scroll is locked while it's open (the workspace
  // portals fullscreen, so the page behind it must not also scroll).
  useEffect(() => {
    if (!zen) return
    const prevOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setZen(false)
    }
    window.addEventListener('keydown', onKey)
    return () => {
      document.body.style.overflow = prevOverflow
      window.removeEventListener('keydown', onKey)
    }
  }, [zen])

  // --- customer picker (inline combobox, T21's "top bar carries the
  // customer picker" — no separate CreateCustomerDialog affordance here,
  // out of this task's Files/Acceptance scope; pick an existing customer) ---
  const [customer, setCustomer] = useState<SelectedCustomer | null>(null)
  const [customerOpen, setCustomerOpen] = useState(false)
  const [customerSearch, setCustomerSearch] = useState('')
  const [debouncedCustomerSearch, setDebouncedCustomerSearch] = useState('')
  const comboRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const t = window.setTimeout(() => setDebouncedCustomerSearch(customerSearch.trim()), 300)
    return () => window.clearTimeout(t)
  }, [customerSearch])

  useEffect(() => {
    if (!customerOpen) return
    const onMouseDown = (e: MouseEvent) => {
      if (comboRef.current && !comboRef.current.contains(e.target as Node)) setCustomerOpen(false)
    }
    document.addEventListener('mousedown', onMouseDown)
    return () => document.removeEventListener('mousedown', onMouseDown)
  }, [customerOpen])

  const customersQuery = useCustomers(customerOpen ? tenantId : undefined, {
    search: debouncedCustomerSearch || undefined,
    page: 1,
    pageSize: 8,
  })

  // --- cart mutation helpers ---
  // Resolved lazily on add: the product-list read (ProductGrid) only carries
  // a flat sale price/unit *name*, not a unit id — the order line API needs
  // a real UnitOfMeasure guid, so the first add fetches (and warms the cache
  // for) the product's full detail and takes its first/base unit. A product
  // with more than one unit is added at that base unit only; v1 has no
  // per-line unit switcher — out of this task's acceptance.
  const addToCart = async (productId: string) => {
    if (!tenantId) return
    try {
      const detail = await qc.fetchQuery({
        queryKey: qk.catalogProduct(tenantId, productId),
        queryFn: () => api.catalogProduct(tenantId, productId),
      })
      const unit = detail.data.units[0]
      if (!unit) {
        toast.error('هذا الصنف بلا وحدة بيع')
        return
      }
      setCart((prev) => {
        const existing = prev.find((l) => l.productId === productId && l.unitId === unit.id)
        if (existing) {
          return prev.map((l) => (l === existing ? { ...l, qty: l.qty + 1 } : l))
        }
        return [
          ...prev,
          {
            productId: detail.data.id,
            productName: detail.data.name,
            unitId: unit.id,
            unitName: unit.name,
            kind: detail.data.kind,
            qty: 1,
            price: unit.sale,
          },
        ]
      })
    } catch (err) {
      toast.error(errorMessage(err))
    }
  }

  const updateQty = (productId: string, unitId: string, qty: number) => {
    if (qty < 1) return
    setCart((prev) =>
      prev.map((l) => (l.productId === productId && l.unitId === unitId ? { ...l, qty } : l)),
    )
  }
  const updatePrice = (productId: string, unitId: string, price: number) => {
    setCart((prev) =>
      prev.map((l) =>
        l.productId === productId && l.unitId === unitId
          ? { ...l, price: Number.isFinite(price) ? price : 0 }
          : l,
      ),
    )
  }
  const removeLine = (productId: string, unitId: string) => {
    setCart((prev) => prev.filter((l) => !(l.productId === productId && l.unitId === unitId)))
  }

  // --- availability (T17) — Product-kind lines only; re-checked in place on
  // every branch switch, the cart itself never resets (criterion 7, UI half) ---
  const productIds = useMemo(
    () => Array.from(new Set(cart.filter((l) => l.kind === 0).map((l) => l.productId))),
    [cart],
  )
  const availabilityQuery = useOrderAvailability(tenantId, branchId, productIds)
  const availabilityMap = useMemo(() => {
    const m = new Map<string, OrderAvailabilityLine>()
    for (const line of availabilityQuery.data?.data.lines ?? []) m.set(line.product_id, line)
    return m
  }, [availabilityQuery.data])
  const isStale = branchId && availabilityQuery.data ? !availabilityQuery.data.data.is_fresh : false

  // --- save ---
  const createOrder = useCreateOrder(tenantId as string)
  const createdByName = meQuery.data
    ? `${meQuery.data.account.FirstName} ${meQuery.data.account.LastName}`.trim() ||
      meQuery.data.account.Email
    : ''

  const saveBlockedReason = !branchId
    ? 'اختر الفرع'
    : !customer
      ? 'اختر العميل'
      : cart.length === 0
        ? 'أضف صنفًا واحدًا على الأقل'
        : undefined

  const save = async () => {
    if (!tenantId || !branchId || !customer || cart.length === 0) return
    if (mode === ORDER_MODE.Delivery && !contactAddress.trim()) {
      toast.error('عنوان التوصيل مطلوب')
      return
    }
    setShortfalls([])
    try {
      const result = await createOrder.mutateAsync({
        branch_id: branchId,
        partner_id: customer.id,
        created_by_name: createdByName,
        mode,
        contact_address: mode === ORDER_MODE.Delivery ? contactAddress.trim() : undefined,
        delivery_fee:
          mode === ORDER_MODE.Delivery && deliveryFee ? Number(deliveryFee) || 0 : undefined,
        note: note.trim() || undefined,
        lines: cart.map((l) => ({
          product_id: l.productId,
          unit_id: l.unitId,
          qty: l.qty,
          price: l.price,
        })),
      })
      toast.success(`تم إنشاء الطلب ${result.ref}`)
      navigate(`/tenants/${tenantId}/orders`)
    } catch (err) {
      if (err instanceof OrderUnavailableError) {
        setShortfalls(err.shortfalls)
        toast.error(err.message)
      } else {
        toast.error(errorMessage(err))
      }
    }
  }

  if (!bundle) return null

  const topBar = (
    <div className="flex flex-wrap items-center gap-2 rounded-xl border border-border bg-card/50 p-2.5">
      <div ref={comboRef} className="relative">
        <button
          type="button"
          onClick={() => setCustomerOpen((v) => !v)}
          className="flex h-9 min-w-56 items-center gap-2 rounded-md border border-input bg-background/40 px-3 text-sm shadow-sm"
        >
          <UsersIcon className="size-4 shrink-0 text-muted-foreground" />
          <span className="min-w-0 flex-1 truncate text-start">
            {customer ? customer.name : 'اختر العميل'}
          </span>
        </button>
        {customerOpen && (
          <div className="absolute start-0 top-full z-20 mt-1 w-80 rounded-lg border border-border bg-popover p-2 shadow-2xl">
            <div className="relative mb-2">
              <SearchIcon className="pointer-events-none absolute start-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
              <Input
                autoFocus
                value={customerSearch}
                onChange={(e) => setCustomerSearch(e.target.value)}
                placeholder="ابحث بالاسم أو الهاتف"
                className="h-8 ps-9 text-sm"
              />
            </div>
            <div className="max-h-64 overflow-y-auto">
              {customersQuery.isLoading ? (
                <p className="px-2 py-4 text-center text-xs text-muted-foreground">جارٍ البحث…</p>
              ) : (customersQuery.data?.data.items.length ?? 0) === 0 ? (
                <p className="px-2 py-4 text-center text-xs text-muted-foreground">لا نتائج مطابقة</p>
              ) : (
                customersQuery.data?.data.items.map((c) => (
                  <button
                    key={c.id}
                    type="button"
                    onClick={() => {
                      setCustomer({ id: c.id, name: c.name, phone1: c.phone1 })
                      setCustomerOpen(false)
                      setCustomerSearch('')
                    }}
                    className="flex w-full items-center justify-between gap-2 rounded-md px-2.5 py-1.5 text-start text-sm hover:bg-accent"
                  >
                    <span className="min-w-0 flex-1 truncate">{c.name}</span>
                    <span className="dir-ltr shrink-0 text-xs text-muted-foreground">{c.phone1}</span>
                  </button>
                ))
              )}
            </div>
          </div>
        )}
      </div>

      <BranchSelector branches={bundle.Branches ?? []} value={branchId} onChange={onBranchChange} />

      {isStale && (
        <Badge tone="warning" className="gap-1.5">
          <DangerIcon className="size-3.5" />
          بيانات الفرع غير محدّثة — تحقق من المتاح قبل الحفظ
        </Badge>
      )}

      <div className="ms-auto flex items-center gap-2">
        {saveBlockedReason && (
          <span className="text-xs text-muted-foreground">{saveBlockedReason}</span>
        )}
        <Button
          type="button"
          disabled={!!saveBlockedReason || createOrder.isPending}
          onClick={() => void save()}
        >
          {createOrder.isPending ? 'جارٍ الحفظ…' : 'حفظ الطلب'}
        </Button>
      </div>
    </div>
  )

  const shortfallBanner = shortfalls.length > 0 && (
    <div className="rounded-lg border border-danger/30 bg-danger/5 p-3 text-sm text-danger">
      <p className="font-semibold">الكمية المطلوبة تتجاوز المتاح في هذا الفرع:</p>
      <ul className="mt-1.5 space-y-0.5">
        {shortfalls.map((s) => (
          <li key={s.product_id}>
            {s.product_name} — المطلوب {toArabicDigits(s.requested)}، المتاح{' '}
            {toArabicDigits(s.available)}
          </li>
        ))}
      </ul>
    </div>
  )

  const workspace = (
    <div
      className="grid h-full min-h-0 flex-1 gap-4"
      style={{ gridTemplateColumns: '220px 1fr 350px' }}
    >
      <GroupDrill
        groups={groupsQuery.data?.data ?? []}
        isLoading={groupsQuery.isLoading}
        selected={groupId}
        onSelect={setGroupId}
        className="h-full min-h-0 overflow-y-auto"
      />

      <div className="min-h-0">
        <ProductGrid tenantId={tenantId} groupId={groupId} onAdd={(id) => void addToCart(id)} />
      </div>

      <div className="min-h-0">
        <OrderCart
          lines={cart}
          availability={availabilityMap}
          hasBranch={!!branchId}
          mode={mode}
          onModeChange={setMode}
          contactAddress={contactAddress}
          onContactAddressChange={setContactAddress}
          deliveryFee={deliveryFee}
          onDeliveryFeeChange={setDeliveryFee}
          note={note}
          onNoteChange={setNote}
          onQtyChange={updateQty}
          onPriceChange={updatePrice}
          onRemove={removeLine}
        />
      </div>
    </div>
  )

  if (zen) {
    return createPortal(
      <div className="fixed inset-0 z-50 flex flex-col gap-3 bg-background p-4">
        <div className="flex items-center justify-between">
          <h1 className="font-display text-lg font-bold">طلب جديد</h1>
          <Button variant="outline" size="sm" className="gap-1.5" onClick={() => setZen(false)}>
            <ZenCollapseIcon className="size-4" />
            إنهاء وضع التركيز
          </Button>
        </div>
        {topBar}
        {shortfallBanner}
        {workspace}
      </div>,
      document.body,
    )
  }

  return (
    <div className="flex flex-col gap-4">
      <PageHeader
        title="طلب جديد"
        description="اختر العميل والفرع وابنِ الطلب من الكتالوج."
        actions={
          <Button variant="outline" size="sm" className="gap-1.5" onClick={() => setZen(true)}>
            <ZenExpandIcon className="size-4" />
            وضع التركيز
          </Button>
        }
      />
      {topBar}
      {shortfallBanner}
      <div className="h-[70vh]">{workspace}</div>
    </div>
  )
}
