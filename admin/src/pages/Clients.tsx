import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { ChevronRight, Search } from 'lucide-react'
import { adminApi } from '@/lib/api'
import { qk } from '@/lib/query'
import { fmtDate, fullName } from '@/lib/format'
import { PageHeader } from '@/components/PageHeader'
import { CreateClientDialog } from '@/components/dialogs/CreateClientDialog'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import { Card } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

export function Clients() {
  const [term, setTerm] = useState('')
  const [debounced, setDebounced] = useState('')
  const navigate = useNavigate()

  useEffect(() => {
    const id = setTimeout(() => setDebounced(term.trim()), 250)
    return () => clearTimeout(id)
  }, [term])

  const clients = useQuery({
    queryKey: qk.clients(debounced),
    queryFn: () => adminApi.searchClients(debounced),
  })

  return (
    <div>
      <PageHeader
        title="Clients"
        description="Search accounts, open a client to manage licenses and devices."
      >
        <CreateClientDialog />
      </PageHeader>

      <div className="relative mb-4 max-w-md">
        <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
        <Input
          placeholder="Search by email or name…"
          className="pl-9"
          value={term}
          onChange={(e) => setTerm(e.target.value)}
        />
      </div>

      <Card className="overflow-hidden">
        <Table>
          <TableHeader>
            <TableRow className="hover:bg-transparent">
              <TableHead>Email</TableHead>
              <TableHead>Name</TableHead>
              <TableHead>Providers</TableHead>
              <TableHead>Joined</TableHead>
              <TableHead className="w-8" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {clients.isLoading ? (
              Array.from({ length: 6 }).map((_, i) => (
                <TableRow key={i} className="hover:bg-transparent">
                  <TableCell colSpan={5}>
                    <Skeleton className="h-6" />
                  </TableCell>
                </TableRow>
              ))
            ) : clients.data && clients.data.length > 0 ? (
              clients.data.map((c) => (
                <TableRow
                  key={c.ID}
                  className="cursor-pointer"
                  onClick={() => navigate(`/clients/${c.ID}`)}
                >
                  <TableCell className="font-medium">{c.Email}</TableCell>
                  <TableCell className="text-muted-foreground">
                    {fullName(c)}
                  </TableCell>
                  <TableCell>
                    <div className="flex flex-wrap gap-1">
                      {(c.Providers ?? []).length === 0 ? (
                        <span className="text-xs text-muted-foreground">—</span>
                      ) : (
                        (c.Providers ?? []).map((p) => (
                          <Badge key={p} tone="muted" className="capitalize">
                            {p}
                          </Badge>
                        ))
                      )}
                    </div>
                  </TableCell>
                  <TableCell className="text-sm text-muted-foreground">
                    {fmtDate(c.CreatedAt)}
                  </TableCell>
                  <TableCell>
                    <ChevronRight className="size-4 text-muted-foreground" />
                  </TableCell>
                </TableRow>
              ))
            ) : (
              <TableRow className="hover:bg-transparent">
                <TableCell
                  colSpan={5}
                  className="py-12 text-center text-sm text-muted-foreground"
                >
                  {debounced
                    ? `No clients match “${debounced}”.`
                    : 'No clients yet. Add your first one.'}
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </Card>

      {clients.data && clients.data.length > 0 && (
        <p className="mt-3 px-1 text-xs text-muted-foreground">
          {clients.data.length} result{clients.data.length > 1 ? 's' : ''}
        </p>
      )}
    </div>
  )
}
