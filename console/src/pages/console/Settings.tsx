import { useState } from 'react'
import { useParams } from 'react-router-dom'
import { toast } from 'sonner'
import { errorMessage } from '@/lib/auth'
import { useMe, useMembers, useRevokeMember } from '@/lib/hooks'
import { fmtDate, memberRoleLabel, memberRoleTone } from '@/lib/format'
import type { Member } from '@/lib/types'
import { PageHeader } from '@/components/PageHeader'
import { LoadingState, EmptyState, ErrorState } from '@/components/States'
import { InviteMemberDialog } from '@/components/InviteMemberDialog'
import { AddIcon, DeleteIcon, MenuIcon, UsersIcon } from '@/components/icon'
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

export function Settings() {
  const { tenantId } = useParams<'tenantId'>()
  const { data: me } = useMe()
  const members = useMembers(tenantId)
  const revoke = useRevokeMember(tenantId ?? '')
  const [inviteOpen, setInviteOpen] = useState(false)

  // Only the tenant owner may invite/revoke — enforced again server-side
  // (403), this just keeps the actions off a plain member's screen.
  const isOwner =
    !!me &&
    (members.data ?? []).some((m) => m.account_id === me.account.ID && m.role === 'owner')

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
          isOwner && (
            <Button onClick={() => setInviteOpen(true)}>
              <AddIcon className="size-4" />
              دعوة عضو
            </Button>
          )
        }
      />

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
                    <Badge tone={memberRoleTone(m.role)}>{memberRoleLabel(m.role)}</Badge>
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

      {tenantId && (
        <InviteMemberDialog
          tenantId={tenantId}
          open={inviteOpen}
          onOpenChange={setInviteOpen}
        />
      )}
    </>
  )
}
