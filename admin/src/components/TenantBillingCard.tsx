import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Receipt, Plus } from 'lucide-react'
import { adminApi } from '@/lib/api'
import { qk } from '@/lib/query'
import {
  fmtDate,
  fmtMoney,
  isZeroTime,
  subscriptionStateLabel,
  subscriptionStateTone,
} from '@/lib/format'
import type { Bill, Tenant } from '@/lib/types'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { AddBillDialog } from '@/components/dialogs/AddBillDialog'
import { VoidBillDialog } from '@/components/dialogs/VoidBillDialog'

interface Props {
  tenant: Tenant
  accountId: string
}

// One tenant's subscription state + bill history — a small billing system
// (T87): amount + period, recorded manually, no plan catalog. Each tenant
// gets its own query since an account can in principle own more than one.
export function TenantBillingCard({ tenant, accountId }: Props) {
  const [addOpen, setAddOpen] = useState(false)
  const [voidBill, setVoidBill] = useState<Bill | null>(null)

  const query = useQuery({
    queryKey: qk.bills(tenant.ID),
    queryFn: () => adminApi.listBills(tenant.ID),
  })

  const bills = query.data?.bills ?? []
  const summary = query.data?.summary
  const defaultStartsAt =
    summary && !isZeroTime(summary.ends_at)
      ? summary.ends_at.slice(0, 10)
      : new Date().toISOString().slice(0, 10)

  return (
    <Card className="mb-6 overflow-hidden">
      <CardHeader className="flex-row items-center justify-between">
        <CardTitle className="flex items-center gap-2">
          <Receipt className="size-4 text-muted-foreground" />
          Billing
          <span className="text-sm font-normal text-muted-foreground">
            — {tenant.Name}
          </span>
          {summary && (
            <Badge tone={subscriptionStateTone(summary.state)}>
              {subscriptionStateLabel(summary.state)}
              {!isZeroTime(summary.ends_at) &&
                (summary.state === 'active' || summary.state === 'expiring'
                  ? ` until ${fmtDate(summary.ends_at)}`
                  : summary.state === 'grace'
                    ? ` until ${fmtDate(summary.grace_until)}`
                    : '')}
            </Badge>
          )}
        </CardTitle>
        <Button size="sm" onClick={() => setAddOpen(true)}>
          <Plus className="size-4" />
          Record bill
        </Button>
      </CardHeader>
      <CardContent className="p-0">
        {query.isLoading ? (
          <div className="p-4">
            <Skeleton className="h-10 w-full" />
          </div>
        ) : bills.length === 0 ? (
          <p className="py-10 text-center text-sm text-muted-foreground">
            No bills recorded yet.
          </p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead>Amount</TableHead>
                <TableHead>Period</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Recorded by</TableHead>
                <TableHead className="w-24" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {bills.map((b) => (
                <TableRow key={b.ID} className="hover:bg-transparent">
                  <TableCell className="font-medium tabular-nums">
                    {fmtMoney(b.Amount, b.Currency)}
                  </TableCell>
                  <TableCell className="text-sm text-muted-foreground">
                    {fmtDate(b.StartsAt)} – {fmtDate(b.EndsAt)}
                  </TableCell>
                  <TableCell>
                    <Badge tone={b.Status === 'paid' ? 'success' : 'muted'}>
                      {b.Status}
                    </Badge>
                    {b.Status === 'void' && b.VoidReason && (
                      <p className="mt-1 text-xs text-muted-foreground">
                        {b.VoidReason}
                      </p>
                    )}
                  </TableCell>
                  <TableCell className="text-sm text-muted-foreground">
                    {b.CreatedBy}
                  </TableCell>
                  <TableCell className="text-right">
                    {b.Status === 'paid' && (
                      <Button
                        variant="ghost"
                        size="sm"
                        className="text-danger hover:bg-danger/10 hover:text-danger"
                        onClick={() => setVoidBill(b)}
                      >
                        Void
                      </Button>
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>

      <AddBillDialog
        tenantId={tenant.ID}
        tenantName={tenant.Name}
        accountId={accountId}
        defaultStartsAt={defaultStartsAt}
        open={addOpen}
        onOpenChange={setAddOpen}
      />
      {voidBill && (
        <VoidBillDialog
          key={voidBill.ID}
          bill={voidBill}
          open={!!voidBill}
          onOpenChange={(o) => !o && setVoidBill(null)}
        />
      )}
    </Card>
  )
}
