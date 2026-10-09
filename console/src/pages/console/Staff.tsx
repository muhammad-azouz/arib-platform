import { useState } from 'react'
import { useParams, useSearchParams } from 'react-router-dom'
import { ApiError } from '@/lib/api'
import { useBranchActivity, useBundle, usePosRoles, useStaff } from '@/lib/hooks'
import { PERM, useCan, useScope } from '@/lib/perm'
import { isSyncPending, savedStaffTimes } from '@/lib/staffSync'
import { fmtDateOnly, toArabicDigits } from '@/lib/format'
import { cn } from '@/lib/utils'
import type { PosRole, StaffMember } from '@/lib/types'
import { PageHeader } from '@/components/PageHeader'
import { Freshness } from '@/components/Freshness'
import { LoadingState, EmptyState, ErrorState } from '@/components/States'
import { AddIcon, InfoIcon, StaffIcon } from '@/components/icon'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { StaffFormDialog } from '@/components/StaffFormDialog'
import { PosRoleFormDialog } from '@/components/PosRoleFormDialog'

const selectClass =
  'flex h-9 w-full rounded-md border border-input bg-background/40 px-3 py-1 text-sm shadow-sm transition-colors focus-visible:outline-none focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/30'

type Tab = 'staff' | 'roles'

/**
 * «موظفو الفروع» — the AribOne POS users and roles, managed remotely.
 *
 * These are NOT console members (Settings → الأعضاء): they are the people who
 * sign in to the AribOne desktop app at a branch. The two systems share the
 * word "user" and nothing else, so this page says so up front, and the console
 * never calls these people "users" or "members".
 */
export function Staff() {
  const { tenantId } = useParams<'tenantId'>()
  const { data: bundle } = useBundle(tenantId)
  const canManage = useCan(tenantId, PERM.StaffManage)
  const canManageRoles = useCan(tenantId, PERM.PosRolesManage)
  // Same narrowing as the form dialog: a branch the member cannot see would
  // return an empty list, which reads as "no staff here" rather than "not yours".
  const scope = useScope(tenantId)
  const branches = (bundle?.Branches ?? []).filter(
    (b) => !scope || scope.branch_ids.length === 0 || scope.branch_ids.includes(b.ID),
  )
  // POS roles are company-wide, so a role write is for members with no branch
  // allowlist only (the API answers 403 forbidden_unscoped to anyone else).
  const canEditRoles = canManageRoles && !!scope && scope.branch_ids.length === 0

  const [tab, setTab] = useState<Tab>('staff')
  // The branch detail page links here with ?branch=<id>.
  const [searchParams] = useSearchParams()
  const [branchId, setBranchId] = useState(searchParams.get('branch') ?? '')
  const [creating, setCreating] = useState(false)
  const [editing, setEditing] = useState<StaffMember>()
  const [creatingRole, setCreatingRole] = useState(false)
  const [editingRole, setEditingRole] = useState<PosRole>()

  const query = useStaff(tenantId, { branchId: branchId || undefined })
  const rolesQuery = usePosRoles(tenantId)
  const activity = useBranchActivity(tenantId)

  const notSubscribed = query.error instanceof ApiError && query.error.status === 402
  const gatewayError = query.error instanceof ApiError && query.error.status !== 402

  if (!bundle) return <LoadingState />

  const branchName = new Map((bundle.Branches ?? []).map((b) => [b.ID, b.Name]))
  const roleName = new Map((rolesQuery.data?.data.roles ?? []).map((r) => [r.id, r.name]))
  const lastSync = new Map(
    (activity.data?.branches ?? []).map((e) => [e.data.branch_id, Date.parse(e.data.last_sync_at)]),
  )
  const savedAt = tenantId ? savedStaffTimes(tenantId) : {}

  return (
    <>
      <PageHeader
        title="موظفو الفروع"
        description="حسابات الدخول إلى برنامج AribOne في الفروع وأدوارها."
        actions={
          <>
            {query.data && <Freshness source={query.data.source} asOf={query.data.as_of} />}
            {canManage && tab === 'staff' && (
              <Button onClick={() => setCreating(true)}>
                <AddIcon className="size-4" />
                موظف جديد
              </Button>
            )}
            {canEditRoles && tab === 'roles' && (
              <Button onClick={() => setCreatingRole(true)}>
                <AddIcon className="size-4" />
                دور جديد
              </Button>
            )}
          </>
        }
      />

      <div className="mb-4 flex items-start gap-2.5 rounded-xl border border-info/30 bg-info/10 p-3 text-sm text-info">
        <InfoIcon className="mt-0.5 size-4 shrink-0" />
        <p>
          هؤلاء هم مستخدمو برنامج AribOne في الفروع، وليسوا أعضاء لوحة التحكم (الأعضاء وأدوارهم من
          الإعدادات). أي تغيير هنا يصل إلى الفرع عند مزامنته القادمة.
        </p>
      </div>

      <div className="mb-4 flex flex-wrap items-center gap-3">
        <div role="tablist" className="inline-flex rounded-lg border border-border p-0.5">
          {(
            [
              ['staff', 'الموظفون'],
              ['roles', 'أدوار نقطة البيع'],
            ] as const
          ).map(([value, label]) => (
            <button
              key={value}
              role="tab"
              type="button"
              aria-selected={tab === value}
              onClick={() => setTab(value)}
              className={cn(
                'rounded-md px-3 py-1.5 text-sm transition-colors',
                tab === value ? 'bg-primary text-primary-foreground' : 'text-muted-foreground',
              )}
            >
              {label}
            </button>
          ))}
        </div>
        {tab === 'staff' && (
          <select
            className={cn(selectClass, 'max-w-56')}
            value={branchId}
            onChange={(e) => setBranchId(e.target.value)}
            aria-label="الفرع"
          >
            <option value="">كل الفروع</option>
            {branches.map((b) => (
              <option key={b.ID} value={b.ID}>
                {b.Name}
              </option>
            ))}
          </select>
        )}
      </div>

      {notSubscribed ? (
        <EmptyState
          icon={StaffIcon}
          title="لا يوجد اشتراك مزامنة"
          description="فعّل اشتراك المزامنة لإدارة موظفي فروعك."
        />
      ) : gatewayError ? (
        <ErrorState
          message="تعذّر الوصول إلى بيانات الموظفين الآن."
          onRetry={() => void query.refetch()}
        />
      ) : tab === 'staff' ? (
        <StaffTable
          items={query.data?.data.items}
          isLoading={query.isLoading}
          branchName={branchName}
          roleName={roleName}
          pendingFor={(m) => isSyncPending(savedAt[m.id], lastSync.get(m.branch_id))}
          onRowClick={canManage ? setEditing : undefined}
        />
      ) : (
        <PosRolesTable
          roles={rolesQuery.data?.data.roles}
          permissionName={
            new Map((rolesQuery.data?.data.permissions ?? []).map((p) => [p.id, p.name]))
          }
          isLoading={rolesQuery.isLoading}
          canEdit={canEditRoles}
          scopedNote={canManageRoles && !canEditRoles}
          onRowClick={canEditRoles ? setEditingRole : undefined}
        />
      )}

      {creating && tenantId && (
        <StaffFormDialog tenantId={tenantId} onClose={() => setCreating(false)} />
      )}
      {editing && tenantId && (
        <StaffFormDialog tenantId={tenantId} existing={editing} onClose={() => setEditing(undefined)} />
      )}
      {(creatingRole || editingRole) && tenantId && (
        <PosRoleFormDialog
          tenantId={tenantId}
          permissions={rolesQuery.data?.data.permissions ?? []}
          existing={editingRole}
          onClose={() => {
            setCreatingRole(false)
            setEditingRole(undefined)
          }}
        />
      )}
    </>
  )
}

function StaffTable({
  items,
  isLoading,
  branchName,
  roleName,
  pendingFor,
  onRowClick,
}: {
  items?: StaffMember[]
  isLoading: boolean
  branchName: Map<string, string>
  roleName: Map<string, string>
  pendingFor: (m: StaffMember) => boolean
  onRowClick?: (m: StaffMember) => void
}) {
  if (isLoading) return <LoadingState rows={5} />
  if (!items || items.length === 0) {
    return (
      <EmptyState
        icon={StaffIcon}
        title="لا يوجد موظفون"
        description="لا يوجد موظفو فروع مطابقون للفلتر المحدد."
      />
    )
  }
  return (
    <div className="rounded-xl border border-border">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>الاسم</TableHead>
            <TableHead>اسم الدخول</TableHead>
            <TableHead>الفرع</TableHead>
            <TableHead>الأدوار</TableHead>
            <TableHead>PIN</TableHead>
            <TableHead>الحالة</TableHead>
            <TableHead>أُنشئ</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {items.map((m) => (
            <TableRow
              key={m.id}
              tabIndex={onRowClick ? 0 : undefined}
              onClick={onRowClick ? () => onRowClick(m) : undefined}
              onKeyDown={
                onRowClick
                  ? (e) => {
                      if (e.key === 'Enter') onRowClick(m)
                    }
                  : undefined
              }
              className={cn(onRowClick && 'cursor-pointer', !m.is_active && 'opacity-60')}
            >
              <TableCell className="font-medium">
                {m.name}
                {m.uses_default_password && (
                  <Badge
                    tone="warning"
                    className="ms-2"
                    title="كلمة المرور ما زالت الافتراضية للنظام — غيّرها فورًا"
                  >
                    كلمة مرور افتراضية
                  </Badge>
                )}
              </TableCell>
              <TableCell dir="ltr" className="text-start font-mono text-xs">
                {m.login_name}
              </TableCell>
              <TableCell>{branchName.get(m.branch_id) ?? '—'}</TableCell>
              <TableCell>
                {m.role_ids.length === 0 ? (
                  <span className="text-muted-foreground">—</span>
                ) : (
                  <div className="flex flex-wrap gap-1">
                    {m.role_ids.map((id) => (
                      <Badge key={id} tone="muted">
                        {roleName.get(id) ?? '…'}
                      </Badge>
                    ))}
                  </div>
                )}
              </TableCell>
              <TableCell>
                {m.pin_locked ? (
                  <Badge tone="danger">مقفول</Badge>
                ) : m.has_pin ? (
                  <Badge tone="success">مُعيَّن</Badge>
                ) : (
                  <span className="text-muted-foreground">—</span>
                )}
              </TableCell>
              <TableCell>
                <div className="flex flex-wrap gap-1">
                  <Badge tone={m.is_active ? 'success' : 'muted'}>
                    {m.is_active ? 'مُفعَّل' : 'موقوف'}
                  </Badge>
                  {pendingFor(m) && (
                    <Badge tone="info" title="تم الحفظ من هنا ولم يُكمل الفرع مزامنة بعدها">
                      بانتظار مزامنة الفرع
                    </Badge>
                  )}
                </div>
              </TableCell>
              <TableCell className="whitespace-nowrap text-sm text-muted-foreground">
                {fmtDateOnly(m.created_at)}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

function PosRolesTable({
  roles,
  permissionName,
  isLoading,
  canEdit,
  scopedNote,
  onRowClick,
}: {
  roles?: PosRole[]
  permissionName: Map<string, string>
  isLoading: boolean
  canEdit: boolean
  /** The member holds the permission but is branch-scoped, so cannot edit. */
  scopedNote: boolean
  onRowClick?: (r: PosRole) => void
}) {
  if (isLoading) return <LoadingState rows={4} />
  if (!roles || roles.length === 0) {
    return (
      <EmptyState
        icon={StaffIcon}
        title="لا توجد أدوار"
        description="لم تُنشأ أدوار لنقطة البيع بعد."
      />
    )
  }
  return (
    <div className="space-y-3">
      <p className="text-xs text-muted-foreground">
        الأدوار مشتركة بين كل الفروع، وأي تعديل عليها يصل إلى جميعها عند المزامنة القادمة.
        {canEdit
          ? ' انقر على دور لتعديله أو حذفه. دور المدير ثابت ولا يمكن تغييره.'
          : scopedNote
            ? ' تعديل الأدوار متاح فقط للحسابات غير المقيّدة بفرع.'
            : ' تعديل الأدوار يتم من برنامج AribOne أو بصلاحية «أدوار نقطة البيع» في الإعدادات.'}
      </p>
      <div className="rounded-xl border border-border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>الدور</TableHead>
              <TableHead>الوصف</TableHead>
              <TableHead>الموظفون</TableHead>
              <TableHead>الصلاحيات</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {roles.map((r) => {
              const editable = !!onRowClick && !r.is_protected
              return (
                <TableRow
                  key={r.id}
                  tabIndex={editable ? 0 : undefined}
                  onClick={editable ? () => onRowClick(r) : undefined}
                  onKeyDown={
                    editable
                      ? (e) => {
                          if (e.key === 'Enter') onRowClick(r)
                        }
                      : undefined
                  }
                  className={cn(editable && 'cursor-pointer')}
                >
                  <TableCell className="font-medium">
                    {r.name}
                    {r.is_protected && (
                      <Badge tone="muted" className="ms-2" title="دور النظام الأساسي — لا يمكن تعديله أو حذفه">
                        ثابت
                      </Badge>
                    )}
                  </TableCell>
                  <TableCell className="text-sm text-muted-foreground">{r.description || '—'}</TableCell>
                  <TableCell>{toArabicDigits(r.staff_count)}</TableCell>
                  <TableCell>
                    <details>
                      <summary className="cursor-pointer text-sm">
                        {toArabicDigits(r.permission_ids.length)} صلاحية
                      </summary>
                      <ul className="mt-1 list-disc ps-5 text-xs text-muted-foreground">
                        {r.permission_ids.map((id) => (
                          <li key={id}>{permissionName.get(id) ?? id}</li>
                        ))}
                      </ul>
                    </details>
                  </TableCell>
                </TableRow>
              )
            })}
          </TableBody>
        </Table>
      </div>
    </div>
  )
}
