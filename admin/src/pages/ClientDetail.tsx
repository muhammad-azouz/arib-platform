import { useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  ArrowLeft,
  Building2,
  DatabaseZap,
  FileSignature,
  HardDrive,
  KeyRound,
  MoreHorizontal,
  Pencil,
  Play,
  Plus,
  PowerOff,
  Trash2,
  Unplug,
} from 'lucide-react'
import { toast } from 'sonner'
import { adminApi } from '@/lib/api'
import { errorMessage } from '@/lib/auth'
import { qk } from '@/lib/query'
import {
  deviceStatusTone,
  fmtDate,
  fullName,
  isExpired,
  licenseStatusTone,
  licenseTypeTone,
  relative,
  tenantStatusTone,
} from '@/lib/format'
import type { Device, License, LicenseStatus, Tenant } from '@/lib/types'
import { PageHeader } from '@/components/PageHeader'
import { CopyId } from '@/components/CopyId'
import { ConfirmDialog } from '@/components/ConfirmDialog'
import { AssignLicenseDialog } from '@/components/dialogs/AssignLicenseDialog'
import { SignOfflineDialog } from '@/components/dialogs/SignOfflineDialog'
import { EditClientDialog } from '@/components/dialogs/EditClientDialog'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

export function ClientDetail() {
  const { id = '' } = useParams()
  const qc = useQueryClient()
  const query = useQuery({
    queryKey: qk.client(id),
    queryFn: () => adminApi.getClient(id),
    enabled: !!id,
  })

  const [assignOpen, setAssignOpen] = useState(false)
  const [editOpen, setEditOpen] = useState(false)
  const [signLicense, setSignLicense] = useState<License | null>(null)
  const [releaseDevice, setReleaseDevice] = useState<Device | null>(null)
  const [deleteTenant, setDeleteTenant] = useState<Tenant | null>(null)

  const invalidate = () => {
    qc.invalidateQueries({ queryKey: qk.client(id) })
    qc.invalidateQueries({ queryKey: qk.stats })
  }

  const statusMutation = useMutation({
    mutationFn: ({ lic, status }: { lic: License; status: LicenseStatus }) =>
      adminApi.setLicenseStatus(lic.ID, status),
    onSuccess: (_d, v) => {
      toast.success(`License ${v.status}`)
      invalidate()
    },
    onError: (e) => toast.error(errorMessage(e)),
  })

  const releaseMutation = useMutation({
    mutationFn: (deviceId: string) => adminApi.forceRelease(deviceId),
    onSuccess: () => {
      toast.success('Device released')
      invalidate()
    },
    onError: (e) => toast.error(errorMessage(e)),
  })

  const provisionMutation = useMutation({
    mutationFn: (tenantId: string) => adminApi.provisionSync(tenantId),
    onSuccess: (t) => {
      toast.success(`Sync provisioned: ${t.DBName}`)
      invalidate()
    },
    onError: (e) => toast.error(errorMessage(e)),
  })

  const deleteMutation = useMutation({
    mutationFn: (tenantId: string) => adminApi.deleteTenant(tenantId),
    onSuccess: (r) => {
      toast.success(
        r.db_dropped
          ? 'Tenant deleted'
          : 'Tenant deleted (no DB was provisioned)',
      )
      invalidate()
    },
    onError: (e) => toast.error(errorMessage(e)),
  })

  if (query.isLoading) {
    return (
      <div className="grid gap-4">
        <Skeleton className="h-9 w-40" />
        <Skeleton className="h-28" />
        <Skeleton className="h-64" />
      </div>
    )
  }

  if (query.isError || !query.data) {
    return (
      <div className="py-20 text-center">
        <p className="text-sm text-muted-foreground">
          {errorMessage(query.error) || 'Client not found.'}
        </p>
        <Button asChild variant="outline" className="mt-4">
          <Link to="/clients">
            <ArrowLeft className="size-4" />
            Back to clients
          </Link>
        </Button>
      </div>
    )
  }

  const { account, licenses = [], devices = [], tenants = [] } = query.data

  return (
    <div className="animate-rise">
      <Link
        to="/clients"
        className="mb-4 inline-flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground"
      >
        <ArrowLeft className="size-3.5" />
        Clients
      </Link>

      <PageHeader title={fullName(account)} description={account.Email}>
        <Button variant="outline" onClick={() => setEditOpen(true)}>
          <Pencil className="size-4" />
          Edit
        </Button>
      </PageHeader>

      {/* Meta */}
      <div className="mb-6 grid gap-3 sm:grid-cols-3">
        <MetaCell label="Account ID">
          <CopyId value={account.ID} label="Account ID" truncate />
        </MetaCell>
        <MetaCell label="Joined">
          <span className="text-sm">{fmtDate(account.CreatedAt)}</span>
        </MetaCell>
        <MetaCell label="Providers">
          <div className="flex flex-wrap gap-1">
            {(account.Providers ?? []).length === 0 ? (
              <span className="text-sm text-muted-foreground">—</span>
            ) : (
              (account.Providers ?? []).map((p) => (
                <Badge key={p} tone="muted" className="capitalize">
                  {p}
                </Badge>
              ))
            )}
          </div>
        </MetaCell>
      </div>

      {account.Notes && (
        <Card className="mb-6">
          <CardHeader>
            <CardTitle className="text-sm">Notes</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="whitespace-pre-wrap text-sm text-muted-foreground">
              {account.Notes}
            </p>
          </CardContent>
        </Card>
      )}

      {/* Tenants */}
      <Card className="mb-6 overflow-hidden">
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Building2 className="size-4 text-muted-foreground" />
            Tenants
            <span className="text-sm font-normal text-muted-foreground">
              ({tenants.length})
            </span>
          </CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          {tenants.length === 0 ? (
            <EmptyRow text="No tenants yet." />
          ) : (
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead>Name</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>DB name</TableHead>
                  <TableHead className="w-40" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {tenants.map((t) => (
                  <TableRow key={t.ID} className="hover:bg-transparent">
                    <TableCell className="font-medium">{t.Name}</TableCell>
                    <TableCell>
                      <Badge tone={tenantStatusTone(t.Status)} className="capitalize">
                        {t.Status}
                      </Badge>
                    </TableCell>
                    <TableCell>
                      {t.DBName ? (
                        <CopyId value={t.DBName} label="DB name" />
                      ) : (
                        <span className="text-sm text-muted-foreground">—</span>
                      )}
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex justify-end gap-2">
                        <Button
                          variant="outline"
                          size="sm"
                          disabled={
                            provisionMutation.isPending &&
                            provisionMutation.variables === t.ID
                          }
                          onClick={() => provisionMutation.mutate(t.ID)}
                        >
                          <DatabaseZap className="size-4" />
                          {t.DBName ? 'Re-provision sync' : 'Provision sync'}
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon"
                          className="size-8 text-danger hover:bg-danger/10 hover:text-danger"
                          title="Delete tenant"
                          onClick={() => setDeleteTenant(t)}
                        >
                          <Trash2 className="size-4" />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      {/* Licenses */}
      <Card className="mb-6 overflow-hidden">
        <CardHeader className="flex-row items-center justify-between">
          <CardTitle className="flex items-center gap-2">
            <KeyRound className="size-4 text-muted-foreground" />
            Licenses
            <span className="text-sm font-normal text-muted-foreground">
              ({licenses.length})
            </span>
          </CardTitle>
          <Button size="sm" onClick={() => setAssignOpen(true)}>
            <Plus className="size-4" />
            Assign
          </Button>
        </CardHeader>
        <CardContent className="p-0">
          {licenses.length === 0 ? (
            <EmptyRow text="No licenses yet." />
          ) : (
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead>Key</TableHead>
                  <TableHead>Type</TableHead>
                  <TableHead>Features</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Expires</TableHead>
                  <TableHead className="w-8" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {licenses.map((lic) => {
                  const expired = isExpired(lic.ExpiresAt)
                  return (
                    <TableRow key={lic.ID} className="hover:bg-transparent">
                      <TableCell>
                        <CopyId value={lic.Key} label="License key" />
                      </TableCell>
                      <TableCell>
                        <Badge tone={licenseTypeTone(lic.Type)} className="capitalize">
                          {lic.Type}
                        </Badge>
                      </TableCell>
                      <TableCell className="text-sm text-muted-foreground">
                        {lic.Features}
                      </TableCell>
                      <TableCell>
                        <Badge tone={licenseStatusTone(lic.Status)} className="capitalize">
                          {lic.Status}
                        </Badge>
                      </TableCell>
                      <TableCell
                        className={
                          expired
                            ? 'text-sm text-danger'
                            : 'text-sm text-muted-foreground'
                        }
                      >
                        {fmtDate(lic.ExpiresAt)}
                        {expired && (
                          <span className="ml-1 text-xs">(expired)</span>
                        )}
                      </TableCell>
                      <TableCell>
                        <DropdownMenu>
                          <DropdownMenuTrigger asChild>
                            <Button variant="ghost" size="icon" className="size-8">
                              <MoreHorizontal className="size-4" />
                            </Button>
                          </DropdownMenuTrigger>
                          <DropdownMenuContent align="end">
                            {lic.Status === 'active' ? (
                              <DropdownMenuItem
                                variant="destructive"
                                onSelect={() =>
                                  statusMutation.mutate({
                                    lic,
                                    status: 'suspended',
                                  })
                                }
                              >
                                <PowerOff className="size-4" />
                                Suspend
                              </DropdownMenuItem>
                            ) : (
                              <DropdownMenuItem
                                onSelect={() =>
                                  statusMutation.mutate({
                                    lic,
                                    status: 'active',
                                  })
                                }
                              >
                                <Play className="size-4" />
                                Activate
                              </DropdownMenuItem>
                            )}
                            <DropdownMenuItem
                              onSelect={() => setSignLicense(lic)}
                            >
                              <FileSignature className="size-4" />
                              Offline string…
                            </DropdownMenuItem>
                          </DropdownMenuContent>
                        </DropdownMenu>
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      {/* Devices */}
      <Card className="overflow-hidden">
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <HardDrive className="size-4 text-muted-foreground" />
            Devices
            <span className="text-sm font-normal text-muted-foreground">
              ({devices?.length ?? 0})
            </span>
          </CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          {devices?.length === 0 ? (
            <EmptyRow text="No devices bound." />
          ) : (
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead>Machine</TableHead>
                  <TableHead>Machine ID</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Last validated</TableHead>
                  <TableHead>Releases</TableHead>
                  <TableHead className="w-8" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {devices.map((dev) => (
                  <TableRow key={dev.ID} className="hover:bg-transparent">
                    <TableCell>
                      <div className="font-medium">
                        {dev.MachineName || '—'}
                      </div>
                      <div className="text-xs uppercase text-muted-foreground">
                        {dev.OS || 'unknown'}
                      </div>
                    </TableCell>
                    <TableCell>
                      <CopyId value={dev.MachineID} label="Machine ID" truncate />
                    </TableCell>
                    <TableCell>
                      <Badge tone={deviceStatusTone(dev.Status)} className="capitalize">
                        {dev.Status}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-sm text-muted-foreground">
                      {relative(dev.LastValidatedAt)}
                    </TableCell>
                    <TableCell className="text-sm text-muted-foreground tabular-nums">
                      {dev.ReleaseCount}
                    </TableCell>
                    <TableCell>
                      {dev.Status === 'active' && (
                        <Button
                          variant="ghost"
                          size="icon"
                          className="size-8 text-danger hover:bg-danger/10 hover:text-danger"
                          title="Force release"
                          onClick={() => setReleaseDevice(dev)}
                        >
                          <Unplug className="size-4" />
                        </Button>
                      )}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      {/* Dialogs */}
      <EditClientDialog
        account={account}
        open={editOpen}
        onOpenChange={setEditOpen}
      />
      <AssignLicenseDialog
        email={account.Email}
        accountId={account.ID}
        open={assignOpen}
        onOpenChange={setAssignOpen}
      />
      {signLicense && (
        <SignOfflineDialog
          key={signLicense.ID}
          license={signLicense}
          open={!!signLicense}
          onOpenChange={(o) => !o && setSignLicense(null)}
        />
      )}
      <ConfirmDialog
        open={!!releaseDevice}
        onOpenChange={(o) => !o && setReleaseDevice(null)}
        title="Force-release device?"
        description={
          <>
            This frees the seat on{' '}
            <span className="font-mono text-foreground/80">
              {releaseDevice?.MachineName || releaseDevice?.MachineID}
            </span>{' '}
            so the client can bind a new machine. Ignores the cooldown.
          </>
        }
        confirmLabel="Release"
        destructive
        onConfirm={async () => {
          if (releaseDevice) await releaseMutation.mutateAsync(releaseDevice.ID)
        }}
      />
      <ConfirmDialog
        open={!!deleteTenant}
        onOpenChange={(o) => !o && setDeleteTenant(null)}
        title="Delete tenant?"
        description={
          <>
            This permanently deletes{' '}
            <span className="font-mono text-foreground/80">
              {deleteTenant?.Name}
            </span>
            , its company, branches, device seat bindings, and its central
            sync database. This action cannot be undone.
          </>
        }
        confirmLabel="Delete"
        destructive
        onConfirm={async () => {
          if (deleteTenant) await deleteMutation.mutateAsync(deleteTenant.ID)
        }}
      />
    </div>
  )
}

function MetaCell({
  label,
  children,
}: {
  label: string
  children: React.ReactNode
}) {
  return (
    <div className="rounded-lg border border-border bg-card/50 px-4 py-3">
      <div className="text-[10px] uppercase tracking-wide text-muted-foreground">
        {label}
      </div>
      <div className="mt-1">{children}</div>
    </div>
  )
}

function EmptyRow({ text }: { text: string }) {
  return (
    <p className="py-10 text-center text-sm text-muted-foreground">{text}</p>
  )
}
