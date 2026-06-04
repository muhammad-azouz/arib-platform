import { useQuery } from '@tanstack/react-query'
import { adminApi } from '@/lib/api'
import { qk } from '@/lib/query'
import { actionLabel, fmtDateTime } from '@/lib/format'
import { PageHeader } from '@/components/PageHeader'
import { CopyId } from '@/components/CopyId'
import { Card } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

export function Audit() {
  const audit = useQuery({ queryKey: qk.audit, queryFn: adminApi.audit })

  return (
    <div>
      <PageHeader
        title="Audit log"
        description="Every sensitive admin action, newest first."
      />

      <Card className="overflow-hidden">
        <Table>
          <TableHeader>
            <TableRow className="hover:bg-transparent">
              <TableHead>Action</TableHead>
              <TableHead>Actor</TableHead>
              <TableHead>Target</TableHead>
              <TableHead>When</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {audit.isLoading ? (
              Array.from({ length: 8 }).map((_, i) => (
                <TableRow key={i} className="hover:bg-transparent">
                  <TableCell colSpan={4}>
                    <Skeleton className="h-6" />
                  </TableCell>
                </TableRow>
              ))
            ) : audit.data && audit.data.length > 0 ? (
              audit.data.map((entry) => (
                <TableRow key={entry.ID} className="hover:bg-transparent">
                  <TableCell>
                    <Badge tone="muted">{actionLabel(entry.Action)}</Badge>
                  </TableCell>
                  <TableCell className="text-sm">{entry.Actor}</TableCell>
                  <TableCell>
                    {entry.Target ? (
                      <CopyId value={entry.Target} label="Target" truncate />
                    ) : (
                      <span className="text-sm text-muted-foreground">—</span>
                    )}
                  </TableCell>
                  <TableCell className="whitespace-nowrap text-sm text-muted-foreground">
                    {fmtDateTime(entry.CreatedAt)}
                  </TableCell>
                </TableRow>
              ))
            ) : (
              <TableRow className="hover:bg-transparent">
                <TableCell
                  colSpan={4}
                  className="py-12 text-center text-sm text-muted-foreground"
                >
                  No audit entries yet.
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </Card>
    </div>
  )
}
