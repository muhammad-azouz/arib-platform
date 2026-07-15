import { useState } from 'react'
import { Link, useParams, useSearchParams } from 'react-router-dom'
import { toast } from 'sonner'
import { ApiError } from '@/lib/api'
import { errorMessage } from '@/lib/auth'
import { useAckConflicts, useConflicts } from '@/lib/hooks'
import { fmtDateTime, relative, toArabicDigits } from '@/lib/format'
import { cn } from '@/lib/utils'
import type { ConflictItem } from '@/lib/types'
import { Freshness } from '@/components/Freshness'
import { PageHeader } from '@/components/PageHeader'
import { Pagination } from '@/components/Pagination'
import { LoadingState, EmptyState, ErrorState } from '@/components/States'
import { DangerIcon, SuccessIcon } from '@/components/icon'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

const PAGE_SIZE = 20

// Synced table names (SyncScope.cs) → their review-page label. Anything else
// (there shouldn't be — ConflictLog only logs synced tables) falls back to
// the raw name rather than hiding the row.
const TABLE_LABELS: Record<string, string> = {
  Products: 'المنتجات',
  UnitOfMeasures: 'الوحدات',
  Barcodes: 'الباركود',
}
function tableLabel(name: string): string {
  return TABLE_LABELS[name] ?? name
}

// Common AribONE.Data column names → Arabic labels. Unmapped keys fall back
// to the raw column name so an unfamiliar/renamed column never hides a diff.
const FIELD_LABELS: Record<string, string> = {
  Name: 'الاسم',
  ProductCode: 'الكود',
  IsActive: 'مُفعّل',
  ReOrder: 'حد إعادة الطلب',
  MaxOrder: 'الحد الأقصى للطلب',
  IsExpire: 'له تاريخ صلاحية',
  GroupId: 'المجموعة',
  Vendor: 'المورّد',
  Customer: 'العميل',
  Buy: 'سعر الشراء',
  Sale: 'سعر البيع',
  ValSub: 'عامل التحويل',
  Level: 'المستوى',
  MasterBuy: 'شراء أساسي',
  MasterSale: 'بيع أساسي',
  Code: 'الباركود',
}
function fieldLabel(key: string): string {
  return FIELD_LABELS[key] ?? key
}

// The row's own id/FK columns are identical by definition (it's the same
// row) — never worth surfacing as a "difference".
const SKIP_FIELDS = new Set(['Id', 'ProductId', 'UnitOfMeasureId'])

function parseRow(raw?: string | null): Record<string, unknown> | null {
  if (!raw) return null
  try {
    const v: unknown = JSON.parse(raw)
    return v && typeof v === 'object' ? (v as Record<string, unknown>) : null
  } catch {
    return null
  }
}

function formatValue(v: unknown): string {
  if (v === null || v === undefined) return '—'
  if (typeof v === 'boolean') return v ? 'نعم' : 'لا'
  if (typeof v === 'number') return toArabicDigits(v)
  return String(v)
}

interface DiffRow {
  key: string
  local: unknown
  remote: unknown
}

/**
 * Differing fields between the kept central row and the branch's rejected
 * write. Returns null when the JSON can't be parsed — the caller degrades to
 * a plain note rather than crashing on a malformed or unfamiliar row shape.
 */
function diffFields(localRaw?: string | null, remoteRaw?: string | null): DiffRow[] | null {
  const local = parseRow(localRaw)
  if (!local) return null
  const remote = parseRow(remoteRaw)
  if (remoteRaw && !remote) return null

  const keys = new Set([...Object.keys(local), ...(remote ? Object.keys(remote) : [])])
  const rows: DiffRow[] = []
  for (const key of keys) {
    if (SKIP_FIELDS.has(key)) continue
    const l = local[key]
    const r = remote ? remote[key] : undefined
    if (JSON.stringify(l) !== JSON.stringify(r)) rows.push({ key, local: l, remote: r })
  }
  return rows
}

export function Conflicts() {
  const { tenantId } = useParams<'tenantId'>()
  const [searchParams, setSearchParams] = useSearchParams()
  const all = searchParams.get('all') === '1'
  const [page, setPage] = useState(1)

  const setAll = (value: boolean) => {
    const next = new URLSearchParams(searchParams)
    if (value) next.set('all', '1')
    else next.delete('all')
    setSearchParams(next, { replace: true })
    setPage(1)
  }

  const query = useConflicts(tenantId, { page, pageSize: PAGE_SIZE, all })
  const ack = useAckConflicts(tenantId ?? '')

  const notSubscribed = query.error instanceof ApiError && query.error.status === 402
  const gatewayError = query.error instanceof ApiError && query.error.status !== 402
  const data = query.data?.data
  // Pages are newest-first (Id DESC), so the first row of page 1 carries the
  // highest id currently on screen — the cutoff for "mark everything read".
  const newestId = page === 1 ? data?.items[0]?.id : undefined

  const handleAck = (input: { ids?: number[]; up_to_id?: number }) => {
    ack.mutate(input, {
      onError: (err) => toast.error(errorMessage(err)),
    })
  }

  return (
    <>
      <PageHeader
        title="التنبيهات والتعارضات"
        description="تعارضات المزامنة التي فازت فيها التغييرات المركزية — راجعها وأرشِفها."
        actions={query.data && <Freshness source={query.data.source} asOf={query.data.as_of} />}
      />

      <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
        <div className="inline-flex rounded-lg border border-border bg-card/50 p-1">
          <button
            type="button"
            onClick={() => setAll(false)}
            className={cn(
              'rounded-md px-3.5 py-1.5 text-sm font-medium transition-colors',
              !all ? 'bg-accent text-primary' : 'text-muted-foreground hover:text-foreground',
            )}
          >
            غير المُراجَعة فقط
          </button>
          <button
            type="button"
            onClick={() => setAll(true)}
            className={cn(
              'rounded-md px-3.5 py-1.5 text-sm font-medium transition-colors',
              all ? 'bg-accent text-primary' : 'text-muted-foreground hover:text-foreground',
            )}
          >
            الكل
          </button>
        </div>

        {data && (
          <div className="flex items-center gap-3 text-sm text-muted-foreground">
            <span>{toArabicDigits(data.unacked)} غير مُراجَع</span>
            {data.unacked > 0 && newestId !== undefined && (
              <Button
                variant="outline"
                size="sm"
                onClick={() => handleAck({ up_to_id: newestId })}
                disabled={ack.isPending}
              >
                تحديد الكل كمُراجَع
              </Button>
            )}
          </div>
        )}
      </div>

      {notSubscribed ? (
        <EmptyState
          icon={DangerIcon}
          title="لا يوجد اشتراك مزامنة"
          description="فعّل اشتراك المزامنة لعرض تعارضات فروعك."
        />
      ) : gatewayError ? (
        <ErrorState
          message="تعذّر الوصول إلى بيانات التعارضات الآن."
          onRetry={() => void query.refetch()}
        />
      ) : query.isLoading ? (
        <LoadingState rows={4} />
      ) : !data || data.items.length === 0 ? (
        <EmptyState
          icon={SuccessIcon}
          title={all ? 'لا توجد تعارضات' : 'لا توجد تعارضات بحاجة إلى مراجعة'}
          description={
            all ? 'لم يُسجَّل أي تعارض مزامنة بعد.' : 'كل التعارضات المُسجَّلة تمت مراجعتها.'
          }
        />
      ) : (
        <>
          <div className="space-y-3">
            {data.items.map((item) => (
              <ConflictCard
                key={item.id}
                item={item}
                tenantId={tenantId as string}
                onAck={(id) => handleAck({ ids: [id] })}
                ackPending={ack.isPending}
              />
            ))}
          </div>
          {data.total > 0 && (
            <Pagination
              page={page}
              pageSize={PAGE_SIZE}
              total={data.total}
              itemLabel="تعارض"
              onPageChange={setPage}
            />
          )}
        </>
      )}
    </>
  )
}

function ConflictCard({
  item,
  tenantId,
  onAck,
  ackPending,
}: {
  item: ConflictItem
  tenantId: string
  onAck: (id: number) => void
  ackPending: boolean
}) {
  const rows = item.remote_row != null ? diffFields(item.local_row, item.remote_row) : null

  return (
    <Card className="p-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <Badge tone="muted">{tableLabel(item.table_name)}</Badge>
            {item.branch_name && <span className="text-sm font-medium">{item.branch_name}</span>}
            {item.acknowledged_at && <Badge tone="success">تمت المراجعة</Badge>}
          </div>
          <div
            className="mt-1 text-xs text-muted-foreground"
            title={fmtDateTime(item.occurred_at)}
          >
            {relative(item.occurred_at)}
          </div>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          {item.product_id && (
            <Button asChild variant="outline" size="sm">
              <Link to={`/tenants/${tenantId}/catalog/${item.product_id}`}>افتح المنتج</Link>
            </Button>
          )}
          {!item.acknowledged_at && (
            <Button size="sm" onClick={() => onAck(item.id)} disabled={ackPending}>
              تمت المراجعة
            </Button>
          )}
        </div>
      </div>

      <div className="mt-3">
        {item.remote_row == null ? (
          <div className="rounded-lg border border-border bg-muted/30 px-3 py-2 text-sm text-muted-foreground">
            حذف من الفرع — تم إبقاء النسخة المركزية
          </div>
        ) : rows === null ? (
          <div className="rounded-lg border border-border bg-muted/30 px-3 py-2 text-sm text-muted-foreground">
            تعذّر عرض تفاصيل هذا التعارض.
          </div>
        ) : rows.length === 0 ? null : (
          <div className="overflow-x-auto rounded-lg border border-border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>الحقل</TableHead>
                  <TableHead>القيمة المحفوظة</TableHead>
                  <TableHead>قيمة الفرع (مرفوضة)</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.map((r) => (
                  <TableRow key={r.key}>
                    <TableCell className="text-muted-foreground">{fieldLabel(r.key)}</TableCell>
                    <TableCell className="font-medium">{formatValue(r.local)}</TableCell>
                    <TableCell className="text-danger">{formatValue(r.remote)}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </div>
    </Card>
  )
}
