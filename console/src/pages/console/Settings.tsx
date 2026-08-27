import { useState } from 'react'
import { useParams } from 'react-router-dom'
import { toast } from 'sonner'
import { cn } from '@/lib/utils'
import { errorMessage } from '@/lib/auth'
import { RoleAssignedError } from '@/lib/api'
import { useDeleteRole, useMe, useMembers, useRevokeMember, useRoles } from '@/lib/hooks'
import { fmtDate, memberRoleLabel, memberRoleTone, toArabicDigits } from '@/lib/format'
import type { Member, RoleView } from '@/lib/types'
import { PageHeader } from '@/components/PageHeader'
import { LoadingState, EmptyState, ErrorState } from '@/components/States'
import { InviteMemberDialog } from '@/components/InviteMemberDialog'
import { RoleFormDialog } from '@/components/RoleFormDialog'
import { AssignRoleDialog } from '@/components/AssignRoleDialog'
import { AddIcon, DeleteIcon, EditIcon, MenuIcon, SecurityIcon, UsersIcon } from '@/components/icon'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
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

type SettingsTab = 'members' | 'roles'

export function Settings() {
  const { tenantId } = useParams<'tenantId'>()
  const { data: me } = useMe()
  const members = useMembers(tenantId)
  const revoke = useRevokeMember(tenantId ?? '')
  const [inviteOpen, setInviteOpen] = useState(false)
  const [tab, setTab] = useState<SettingsTab>('members')
  const [assigning, setAssigning] = useState<Member | null>(null)

  // Only the tenant owner may invite/revoke members or manage roles at all
  // — enforced again server-side (403 on the writes; reads are allowed for
  // any member, but the roles tab itself stays off a plain member's screen,
  // per T112's acceptance).
  const isOwner =
    !!me &&
    (members.data ?? []).some((m) => m.account_id === me.account.ID && m.role === 'owner')
  const activeTab: SettingsTab = isOwner ? tab : 'members'

  const revokeMember = (m: Member) => {
    revoke.mutate(m.id, {
      onSuccess: () => toast.success(`تم إلغاء عضوية ${m.email}`),
      onError: (err) => toast.error(errorMessage(err)),
    })
  }

  return (
    <>
      <PageHeader
        title="الإعدادات"
        description="تفضيلات الحساب، والأعضاء الذين يمكنهم الوصول إلى لوحة التحكم."
        actions={
          isOwner &&
          (activeTab === 'members' ? (
            <Button onClick={() => setInviteOpen(true)}>
              <AddIcon className="size-4" />
              دعوة عضو
            </Button>
          ) : (
            <RoleActions tenantId={tenantId ?? ''} />
          ))
        }
      />

      {isOwner && (
        <div className="mb-4 inline-flex rounded-lg border border-border bg-card/50 p-1">
          {(
            [
              { key: 'members', label: 'الأعضاء' },
              { key: 'roles', label: 'الأدوار' },
            ] as const
          ).map((o) => (
            <button
              key={o.key}
              type="button"
              onClick={() => setTab(o.key)}
              className={cn(
                'rounded-md px-3.5 py-1.5 text-sm font-medium transition-colors',
                activeTab === o.key
                  ? 'bg-accent text-primary'
                  : 'text-muted-foreground hover:text-foreground',
              )}
            >
              {o.label}
            </button>
          ))}
        </div>
      )}

      {activeTab === 'members' ? (
        <Card>
          {members.isLoading ? (
            <div className="p-5">
              <LoadingState />
            </div>
          ) : members.isError ? (
            <ErrorState className="m-5" onRetry={() => void members.refetch()} />
          ) : !members.data || members.data.length === 0 ? (
            <EmptyState
              className="border-0"
              icon={UsersIcon}
              title="لا يوجد أعضاء"
              description="ادعُ زميلًا للوصول إلى لوحة التحكم لهذا النشاط."
            />
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>العضو</TableHead>
                  <TableHead>الدور</TableHead>
                  <TableHead>الدور المخصص</TableHead>
                  <TableHead>الفروع</TableHead>
                  <TableHead>تاريخ الانضمام</TableHead>
                  {isOwner && <TableHead className="w-10" />}
                </TableRow>
              </TableHeader>
              <TableBody>
                {members.data.map((m) => (
                  <TableRow key={m.id}>
                    <TableCell>
                      <div className="font-medium">
                        {[m.first_name, m.last_name].filter(Boolean).join(' ') || m.email}
                      </div>
                      <div className="dir-ltr text-start text-xs text-muted-foreground">
                        {m.email}
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center gap-1.5">
                        <Badge tone={memberRoleTone(m.role)}>{memberRoleLabel(m.role)}</Badge>
                        {m.role !== 'owner' && !m.accepted_at && (
                          <Badge tone="warning">بانتظار الانضمام</Badge>
                        )}
                      </div>
                    </TableCell>
                    <TableCell>
                      {m.role === 'owner' ? (
                        <Badge tone="info">المالك</Badge>
                      ) : m.role_id ? (
                        m.role_name
                      ) : (
                        <Badge tone="muted">بدون دور</Badge>
                      )}
                    </TableCell>
                    <TableCell className="text-muted-foreground">
                      {m.branch_ids.length === 0
                        ? 'كل الفروع'
                        : `${toArabicDigits(m.branch_ids.length)} فرع`}
                    </TableCell>
                    <TableCell className="text-muted-foreground">
                      {fmtDate(m.created_at)}
                    </TableCell>
                    {isOwner && (
                      <TableCell>
                        {m.role !== 'owner' && (
                          <DropdownMenu>
                            <DropdownMenuTrigger asChild>
                              <Button variant="ghost" size="icon" aria-label="إجراءات العضو">
                                <MenuIcon className="size-4" />
                              </Button>
                            </DropdownMenuTrigger>
                            <DropdownMenuContent align="end" className="w-44">
                              <DropdownMenuItem onSelect={() => setAssigning(m)}>
                                <SecurityIcon className="size-4" />
                                تعيين دور
                              </DropdownMenuItem>
                              <DropdownMenuItem
                                variant="destructive"
                                onSelect={() => revokeMember(m)}
                              >
                                <DeleteIcon className="size-4" />
                                إلغاء العضوية
                              </DropdownMenuItem>
                            </DropdownMenuContent>
                          </DropdownMenu>
                        )}
                      </TableCell>
                    )}
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </Card>
      ) : (
        <RolesPanel tenantId={tenantId ?? ''} />
      )}

      {tenantId && (
        <InviteMemberDialog
          tenantId={tenantId}
          open={inviteOpen}
          onOpenChange={setInviteOpen}
        />
      )}

      {tenantId && assigning && (
        <AssignRoleDialog
          key={assigning.id}
          tenantId={tenantId}
          member={assigning}
          open
          onOpenChange={(o) => {
            if (!o) setAssigning(null)
          }}
        />
      )}
    </>
  )
}

// Header "دور جديد" button + its dialog, split out so the dialog's own
// create-mode instance doesn't have to live inside RolesPanel (which also
// owns an edit-mode instance per row action) — two open dialogs would
// otherwise fight over the same local `role`/`open` state.
function RoleActions({ tenantId }: { tenantId: string }) {
  const [open, setOpen] = useState(false)
  return (
    <>
      <Button onClick={() => setOpen(true)}>
        <AddIcon className="size-4" />
        دور جديد
      </Button>
      <RoleFormDialog tenantId={tenantId} open={open} onOpenChange={setOpen} />
    </>
  )
}

function RolesPanel({ tenantId }: { tenantId: string }) {
  const roles = useRoles(tenantId)
  const deleteRole = useDeleteRole(tenantId)
  const [editing, setEditing] = useState<RoleView | null>(null)

  const handleDelete = (r: RoleView) => {
    deleteRole.mutate(r.id, {
      onSuccess: () => toast.success(`تم حذف دور ${r.name}`),
      onError: (err) => {
        if (err instanceof RoleAssignedError) {
          toast.error(`الدور مُسند إلى ${toArabicDigits(err.count)} عضو — أعد تعيين أعضائه أولًا`)
        } else {
          toast.error(errorMessage(err))
        }
      },
    })
  }

  return (
    <>
      <Card>
        {roles.isLoading ? (
          <div className="p-5">
            <LoadingState />
          </div>
        ) : roles.isError ? (
          <ErrorState className="m-5" onRetry={() => void roles.refetch()} />
        ) : !roles.data || roles.data.length === 0 ? (
          <EmptyState
            className="border-0"
            icon={SecurityIcon}
            title="لا توجد أدوار مخصصة بعد"
            description="أنشئ دورًا لمنح الأعضاء وصولًا محدودًا إلى أقسام بعينها."
          />
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>الدور</TableHead>
                <TableHead>الصلاحيات</TableHead>
                <TableHead>الأعضاء</TableHead>
                <TableHead className="w-10" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {roles.data.map((r) => (
                <TableRow key={r.id}>
                  <TableCell className="font-medium">{r.name}</TableCell>
                  <TableCell className="text-muted-foreground">
                    {toArabicDigits(r.permissions.length)} صلاحية
                  </TableCell>
                  <TableCell className="text-muted-foreground">
                    {toArabicDigits(r.assigned_count)}
                  </TableCell>
                  <TableCell>
                    <DropdownMenu>
                      <DropdownMenuTrigger asChild>
                        <Button variant="ghost" size="icon" aria-label="إجراءات الدور">
                          <MenuIcon className="size-4" />
                        </Button>
                      </DropdownMenuTrigger>
                      <DropdownMenuContent align="end" className="w-44">
                        <DropdownMenuItem onSelect={() => setEditing(r)}>
                          <EditIcon className="size-4" />
                          تعديل
                        </DropdownMenuItem>
                        <DropdownMenuItem
                          variant="destructive"
                          onSelect={() => handleDelete(r)}
                        >
                          <DeleteIcon className="size-4" />
                          حذف
                        </DropdownMenuItem>
                      </DropdownMenuContent>
                    </DropdownMenu>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </Card>

      <RoleFormDialog
        // Remounts whenever the edit target changes id, so its internal
        // permission-set state always starts from *this* role's own
        // permissions rather than whatever the previously edited role left
        // behind (see RoleFormDialog's onOpenChange for the same-role
        // reopen case this doesn't cover).
        key={editing?.id ?? 'create'}
        tenantId={tenantId}
        open={!!editing}
        onOpenChange={(o) => {
          if (!o) setEditing(null)
        }}
        role={editing ?? undefined}
      />
    </>
  )
}
