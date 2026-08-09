import { useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { toast } from 'sonner'
import { ApiError } from '@/lib/api'
import { errorMessage } from '@/lib/auth'
import { useBundle, useCancelOrder, useOrderDetail, useTransferOrder } from '@/lib/hooks'
import {
  fmtDateTime,
  orderChannelLabel,
  orderModeLabel,
  orderStatusLabel,
  orderStatusTone,
  toArabicDigits,
} from '@/lib/format'
import { ORDER_STATUS, type OrderChainEntry, type OrderDetail as OrderDetailData } from '@/lib/types'
import { Breadcrumbs } from '@/components/Breadcrumbs'
import { Freshness } from '@/components/Freshness'
import { LoadingState, EmptyState, ErrorState } from '@/components/States'
import { BranchSelector } from '@/components/orders/BranchSelector'
import {
  AddressIcon,
  ArrowLeading,
  CloseIcon,
  DeliveryModeIcon,
  HistoryIcon,
  NotesIcon,
  OrdersIcon,
  PhoneIcon,
  ReceiptIcon,
  TransferIcon,
} from '@/components/icon'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

const money = new Intl.NumberFormat('ar', { maximumFractionDigits: 2 })

function CancelOrderDialog({
  tenantId,
  orderId,
  open,
  onOpenChange,
}: {
  tenantId: string
  orderId: string
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const [reason, setReason] = useState('')
  const cancel = useCancelOrder(tenantId)

  const submit = async () => {
    try {
      await cancel.mutateAsync({ orderId, reason: reason.trim() || undefined })
      toast.success('تم إلغاء الطلب')
      setReason('')
      onOpenChange(false)
    } catch (err) {
      // D4 refusal (order no longer New, or already delivered) — the
      // gateway's own Arabic message, forwarded verbatim.
      toast.error(errorMessage(err))
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>إلغاء الطلب</DialogTitle>
        </DialogHeader>
        <div className="space-y-1.5">
          <Label htmlFor="cancel-reason">سبب الإلغاء (اختياري)</Label>
          <Textarea
            id="cancel-reason"
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            rows={3}
          />
        </div>
        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            onClick={() => onOpenChange(false)}
            disabled={cancel.isPending}
          >
            تراجع
          </Button>
          <Button type="button" variant="destructive" disabled={cancel.isPending} onClick={submit}>
            تأكيد الإلغاء
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function TransferOrderDialog({
  tenantId,
  order,
  open,
  onOpenChange,
}: {
  tenantId: string
  order: OrderDetailData
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const { data: bundle } = useBundle(tenantId)
  const [toBranchId, setToBranchId] = useState<string | undefined>(undefined)
  const transfer = useTransferOrder(tenantId)
  const navigate = useNavigate()

  const eligible = (bundle?.Branches ?? []).filter((b) => b.ID !== order.branch_id)

  const submit = async () => {
    if (!toBranchId) return
    try {
      const result = await transfer.mutateAsync({ orderId: order.id, toBranchId })
      toast.success('تم تحويل الطلب')
      setToBranchId(undefined)
      onOpenChange(false)
      // D7: the current order closes as Transferred and a new leg opens at
      // the target branch under the same Ref — the new leg is the order
      // that's still actionable, so that's where the page should land.
      navigate(`/tenants/${tenantId}/orders/${result.id}`)
    } catch (err) {
      toast.error(errorMessage(err))
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>تحويل الطلب إلى فرع آخر</DialogTitle>
        </DialogHeader>
        <div className="space-y-1.5">
          <Label htmlFor="transfer-branch">الفرع المستقبل</Label>
          <BranchSelector branches={eligible} value={toBranchId} onChange={setToBranchId} className="w-full" />
        </div>
        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            onClick={() => onOpenChange(false)}
            disabled={transfer.isPending}
          >
            تراجع
          </Button>
          <Button type="button" disabled={!toBranchId || transfer.isPending} onClick={submit}>
            تأكيد التحويل
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function TransferChain({
  tenantId,
  history,
  currentId,
}: {
  tenantId: string
  history: OrderChainEntry[]
  currentId: string
}) {
  return (
    <div className="flex flex-wrap items-center gap-2">
      {history.map((leg, i) => (
        <div key={leg.id} className="flex items-center gap-2">
          <Link
            to={`/tenants/${tenantId}/orders/${leg.id}`}
            className={
              'flex items-center gap-2 rounded-lg border px-3 py-2 text-sm transition-colors ' +
              (leg.id === currentId
                ? 'border-primary/50 bg-primary/5 font-medium'
                : 'border-border hover:bg-accent/50')
            }
          >
            <span>{leg.branch_name}</span>
            <Badge tone={orderStatusTone(leg.status)}>{orderStatusLabel(leg.status)}</Badge>
            <span className="text-xs text-muted-foreground">{fmtDateTime(leg.created_at)}</span>
          </Link>
          {i < history.length - 1 && (
            <ArrowLeading className="size-4 text-muted-foreground" />
          )}
        </div>
      ))}
    </div>
  )
}

function Timeline({ order }: { order: OrderDetailData }) {
  const events: { label: string; at: string }[] = [{ label: 'تم إنشاء الطلب', at: order.created_at }]
  if (order.status_changed_at && order.status_changed_at !== order.created_at) {
    events.push({
      label: `آخر تحديث للحالة: ${orderStatusLabel(order.status)}`,
      at: order.status_changed_at,
    })
  }
  if (order.due_at) events.push({ label: 'الموعد المطلوب', at: order.due_at })
  events.sort((a, b) => new Date(a.at).getTime() - new Date(b.at).getTime())

  return (
    <ol className="space-y-3">
      {events.map((e, i) => (
        <li key={i} className="flex items-start gap-3 text-sm">
          <span className="mt-1.5 size-2 shrink-0 rounded-full bg-primary" />
          <div>
            <div className="font-medium">{e.label}</div>
            <div className="text-xs text-muted-foreground">{fmtDateTime(e.at)}</div>
          </div>
        </li>
      ))}
      {order.status === ORDER_STATUS.Cancelled && order.cancel_reason && (
        <li className="flex items-start gap-3 text-sm">
          <span className="mt-1.5 size-2 shrink-0 rounded-full bg-danger" />
          <div>
            <div className="font-medium">تم إلغاء الطلب</div>
            <div className="text-xs text-muted-foreground">{order.cancel_reason}</div>
          </div>
        </li>
      )}
    </ol>
  )
}

export function OrderDetail() {
  const { tenantId, orderId } = useParams<'tenantId' | 'orderId'>()
  const query = useOrderDetail(tenantId, orderId)
  const [cancelOpen, setCancelOpen] = useState(false)
  const [transferOpen, setTransferOpen] = useState(false)

  const crumbs = [
    { label: 'الطلبات', to: `/tenants/${tenantId}/orders` },
    { label: query.data?.data.ref ?? 'الطلب' },
  ]

  if (query.error instanceof ApiError && query.error.status === 402) {
    return (
      <>
        <Breadcrumbs className="mb-4" items={crumbs} />
        <EmptyState
          icon={OrdersIcon}
          title="لا يوجد اشتراك مزامنة"
          description="فعّل اشتراك المزامنة لعرض تفاصيل هذا الطلب."
        />
      </>
    )
  }

  if (query.error instanceof ApiError && query.error.status === 404) {
    return (
      <>
        <Breadcrumbs className="mb-4" items={crumbs} />
        <EmptyState
          icon={OrdersIcon}
          title="الطلب غير موجود"
          description="لم يعد هذا الطلب موجودًا."
          action={
            <Link to={`/tenants/${tenantId}/orders`} className="text-sm text-primary">
              العودة إلى الطلبات
            </Link>
          }
        />
      </>
    )
  }

  if (query.error) {
    return (
      <>
        <Breadcrumbs className="mb-4" items={crumbs} />
        <ErrorState message="تعذّر الوصول إلى بيانات الطلب الآن." onRetry={() => void query.refetch()} />
      </>
    )
  }

  if (!query.data) return <LoadingState />

  const o = query.data.data
  const isNew = o.status === ORDER_STATUS.New
  const subtotal = o.lines.reduce((sum, l) => sum + l.total, 0)

  return (
    <>
      <Breadcrumbs className="mb-4" items={crumbs} />

      <div className="mb-6 flex flex-col gap-3">
        <div className="flex flex-wrap items-center gap-3">
          <h1 className="dir-ltr font-display text-2xl font-bold">{o.ref}</h1>
          <Badge tone={orderStatusTone(o.status)}>{orderStatusLabel(o.status)}</Badge>
          <Badge tone="muted">{orderChannelLabel(o.channel)}</Badge>
          <Freshness source={query.data.source} asOf={query.data.as_of} />
          {isNew && (
            <div className="ms-auto flex items-center gap-2">
              <Button variant="outline" size="sm" onClick={() => setTransferOpen(true)}>
                <TransferIcon className="size-4" />
                تحويل
              </Button>
              <Button
                variant="outline"
                size="sm"
                className="text-danger hover:text-danger"
                onClick={() => setCancelOpen(true)}
              >
                <CloseIcon className="size-4" />
                إلغاء
              </Button>
            </div>
          )}
        </div>
        <div className="flex flex-wrap items-center gap-x-6 gap-y-1 text-sm text-muted-foreground">
          <span>
            العميل: <span className="font-medium text-foreground">{o.customer_name || 'تعامل نقدى'}</span>
          </span>
          {o.phone && (
            <span className="flex items-center gap-1.5">
              <PhoneIcon className="size-4" />
              <span dir="ltr" className="font-medium text-foreground">
                {o.phone}
              </span>
            </span>
          )}
          <span>
            الفرع: <span className="font-medium text-foreground">{o.branch_name}</span>
          </span>
          {o.created_by_name && (
            <span>
              أنشأه: <span className="font-medium text-foreground">{o.created_by_name}</span>
            </span>
          )}
          <span>{fmtDateTime(o.created_at)}</span>
        </div>
      </div>

      {o.history.length > 1 && (
        <Card className="mb-5 p-4">
          <div className="mb-3 text-xs font-medium uppercase tracking-wide text-muted-foreground">
            سلسلة التحويل
          </div>
          <TransferChain tenantId={tenantId ?? ''} history={o.history} currentId={o.id} />
        </Card>
      )}

      <div className="mb-5 grid gap-3 lg:grid-cols-3">
        <Card className="p-4 lg:col-span-2">
          <div className="mb-3 flex items-center gap-2 text-sm font-medium">
            <ReceiptIcon className="size-5 text-primary" />
            أصناف الطلب
          </div>
          <div className="-mx-4">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>الصنف</TableHead>
                  <TableHead>الوحدة</TableHead>
                  <TableHead>الكمية</TableHead>
                  <TableHead>السعر</TableHead>
                  <TableHead>الإجمالي</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {o.lines.map((l) => (
                  <TableRow key={l.product_id}>
                    <TableCell className="font-medium">{l.product_name}</TableCell>
                    <TableCell className="text-muted-foreground">{l.unit_name}</TableCell>
                    <TableCell className="tabular-nums">{toArabicDigits(l.qty)}</TableCell>
                    <TableCell className="tabular-nums">{money.format(l.price)}</TableCell>
                    <TableCell className="font-semibold tabular-nums">{money.format(l.total)}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
          <div className="mt-4 space-y-1.5 border-t border-border pt-3 text-sm">
            <div className="flex items-center justify-between text-muted-foreground">
              <span>الإجمالي الفرعي</span>
              <span className="tabular-nums">{money.format(subtotal)}</span>
            </div>
            {!!o.delivery_fee && (
              <div className="flex items-center justify-between text-muted-foreground">
                <span>رسوم التوصيل</span>
                <span className="tabular-nums">{money.format(o.delivery_fee)}</span>
              </div>
            )}
            <div className="flex items-center justify-between border-t border-border pt-1.5 font-display text-base font-bold">
              <span>الإجمالي</span>
              <span className="tabular-nums">{money.format(o.total)}</span>
            </div>
          </div>
        </Card>

        <div className="flex flex-col gap-3">
          <Card className="p-4">
            <div className="mb-3 flex items-center gap-2 text-sm font-medium">
              <DeliveryModeIcon className="size-5 text-primary" />
              التسليم
            </div>
            <div className="space-y-2 text-sm">
              <div className="flex items-center justify-between">
                <span className="text-muted-foreground">طريقة الاستلام</span>
                <span className="font-medium">{orderModeLabel(o.mode)}</span>
              </div>
              {o.address && (
                <div className="flex items-start justify-between gap-2">
                  <span className="flex shrink-0 items-center gap-1.5 text-muted-foreground">
                    <AddressIcon className="size-4" />
                    العنوان
                  </span>
                  <span className="text-end font-medium">{o.address}</span>
                </div>
              )}
              {o.due_at && (
                <div className="flex items-center justify-between">
                  <span className="text-muted-foreground">الموعد المطلوب</span>
                  <span className="font-medium">{fmtDateTime(o.due_at)}</span>
                </div>
              )}
            </div>
          </Card>

          {o.note && (
            <Card className="p-4">
              <div className="mb-2 flex items-center gap-2 text-sm font-medium">
                <NotesIcon className="size-5 text-primary" />
                ملاحظات
              </div>
              <p className="text-sm text-muted-foreground">{o.note}</p>
            </Card>
          )}

          <Card className="p-4">
            <div className="mb-3 flex items-center gap-2 text-sm font-medium">
              <HistoryIcon className="size-5 text-primary" />
              الجدول الزمني
            </div>
            <Timeline order={o} />
          </Card>
        </div>
      </div>

      {tenantId && orderId && (
        <CancelOrderDialog
          tenantId={tenantId}
          orderId={orderId}
          open={cancelOpen}
          onOpenChange={setCancelOpen}
        />
      )}
      {tenantId && (
        <TransferOrderDialog
          tenantId={tenantId}
          order={o}
          open={transferOpen}
          onOpenChange={setTransferOpen}
        />
      )}
    </>
  )
}
