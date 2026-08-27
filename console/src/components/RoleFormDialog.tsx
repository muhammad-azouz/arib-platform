import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { z } from 'zod'
import { toast } from 'sonner'
import { errorMessage } from '@/lib/auth'
import { useCreateRole, usePermissions, useUpdateRole } from '@/lib/hooks'
import { PERM } from '@/lib/perm'
import type { RoleView } from '@/lib/types'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { LoadingState } from '@/components/States'

// D3's catalog, grouped for the grid — order matches the spec table. A
// section with no `manage` (Inventory, Reports) renders only a عرض
// checkbox; النشاط التجاري has no `view` code at all, so it renders only
// إدارة (see lib/perm.ts's PERM and T110's nav-gating notes).
const SECTIONS: { label: string; view?: string; manage?: string }[] = [
  { label: 'الفروع', view: PERM.BranchesView, manage: PERM.BranchesManage },
  { label: 'الكتالوج', view: PERM.CatalogView, manage: PERM.CatalogManage },
  { label: 'المخزون', view: PERM.InventoryView },
  { label: 'العملاء', view: PERM.CustomersView, manage: PERM.CustomersManage },
  { label: 'الموردون', view: PERM.SuppliersView, manage: PERM.SuppliersManage },
  { label: 'الطلبات', view: PERM.OrdersView, manage: PERM.OrdersManage },
  { label: 'التقارير', view: PERM.ReportsView },
  { label: 'التعارضات', view: PERM.ConflictsView, manage: PERM.ConflictsManage },
  { label: 'النشاط التجاري', manage: PERM.CompanyManage },
]
const KNOWN_CODES = new Set(
  SECTIONS.flatMap((s) => [s.view, s.manage].filter((c): c is string => !!c)),
)

const schema = z.object({
  name: z.string().trim().min(1, 'اسم الدور مطلوب').max(60, 'الاسم طويل جدًا'),
})
type Form = z.infer<typeof schema>

// Mirrors perm.ErrEmptyPermissions's exact text (api/internal/perm/perm.go)
// — client-side rejection reads the same as a server 400 would, so nothing
// looks different depending on which side caught it.
const EMPTY_PERMISSIONS_MESSAGE = 'perm: at least one permission is required'

/**
 * Create/edit dialog for a custom role (T112). One instance is reused for
 * every row's edit action and for the "new role" button (see Settings.tsx)
 * — `role` selects the mode; its absence means create. The grid renders
 * every code `GET …/permissions` actually returns: known D3 sections get a
 * labeled عرض/إدارة pair, and anything else (a future code the console
 * hasn't been taught a label for yet) still renders, as its raw string,
 * rather than silently disappearing.
 */
export function RoleFormDialog({
  tenantId,
  open,
  onOpenChange,
  role,
}: {
  tenantId: string
  open: boolean
  onOpenChange: (open: boolean) => void
  role?: RoleView
}) {
  const permissionsQuery = usePermissions(tenantId)
  const create = useCreateRole(tenantId)
  const update = useUpdateRole(tenantId)
  const [selected, setSelected] = useState<Set<string>>(new Set(role?.permissions ?? []))
  const [permError, setPermError] = useState<string | null>(null)

  const form = useForm<Form>({
    resolver: zodResolver(schema),
    defaultValues: { name: role?.name ?? '' },
  })

  // إدارة implies عرض in both directions here: ticking إدارة auto-ticks
  // عرض, and unticking عرض auto-unticks إدارة — a member can't manage a
  // section it can't see. This is UI convenience only; the server
  // (perm.Normalize) is what actually enforces the rule on save.
  const toggleView = (code: string, manageCode: string | undefined) => {
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(code)) {
        next.delete(code)
        if (manageCode) next.delete(manageCode)
      } else {
        next.add(code)
      }
      return next
    })
  }

  const toggleManage = (code: string, viewCode: string | undefined) => {
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(code)) {
        next.delete(code)
      } else {
        next.add(code)
        if (viewCode) next.add(viewCode)
      }
      return next
    })
  }

  const toggleSimple = (code: string) => {
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(code)) next.delete(code)
      else next.add(code)
      return next
    })
  }

  // The grid renders exactly what the server's catalog contains — not the
  // static PERM mirror — so a code the server stops returning stops showing
  // up here too, and a code it doesn't yet know a label for still shows
  // (below) rather than vanishing.
  const catalog = permissionsQuery.data ?? []
  const catalogSet = new Set(catalog)
  const visibleSections = SECTIONS.filter(
    (s) => (s.view && catalogSet.has(s.view)) || (s.manage && catalogSet.has(s.manage)),
  )
  const extraCodes = catalog.filter((c) => !KNOWN_CODES.has(c))

  const submit = form.handleSubmit(async (values) => {
    if (selected.size === 0) {
      setPermError(EMPTY_PERMISSIONS_MESSAGE)
      return
    }
    setPermError(null)
    const permissions = [...selected]
    try {
      if (role) {
        await update.mutateAsync({ roleId: role.id, name: values.name, permissions })
        toast.success('تم حفظ التغييرات')
      } else {
        await create.mutateAsync({ name: values.name, permissions })
        toast.success('تم إنشاء الدور')
      }
      onOpenChange(false)
    } catch (err) {
      toast.error(errorMessage(err))
    }
  })

  const pending = create.isPending || update.isPending

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        if (!o) {
          // Reset back to this dialog's own current target (blank for
          // create, `role`'s own values for edit) — not on open, since a
          // fresh mount per target (Settings.tsx keys the edit instance by
          // role id) already seeds correct initial state there. This only
          // needs to undo an abandoned in-progress edit before the same
          // instance is reused for the next open.
          form.reset({ name: role?.name ?? '' })
          setSelected(new Set(role?.permissions ?? []))
          setPermError(null)
        }
        onOpenChange(o)
      }}
    >
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{role ? 'تعديل الدور' : 'دور جديد'}</DialogTitle>
          <DialogDescription>
            الأعضاء الذين يحملون هذا الدور يصلون فقط إلى الأقسام المحددة أدناه. تفعيل
            «إدارة» يفعّل «عرض» تلقائيًا.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={submit} className="space-y-4" noValidate>
          <div className="space-y-1.5">
            <Label htmlFor="role-name">
              اسم الدور<span className="text-danger"> *</span>
            </Label>
            <Input id="role-name" autoFocus {...form.register('name')} />
            {form.formState.errors.name && (
              <p className="text-xs text-danger">{form.formState.errors.name.message}</p>
            )}
          </div>

          <div className="space-y-1.5">
            <Label>الصلاحيات</Label>
            {permissionsQuery.isLoading ? (
              <LoadingState rows={2} />
            ) : (
              <div className="overflow-hidden rounded-lg border border-border">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b border-border bg-card/50 text-muted-foreground">
                      <th className="p-2.5 text-start font-medium">القسم</th>
                      <th className="w-20 p-2.5 text-center font-medium">عرض</th>
                      <th className="w-20 p-2.5 text-center font-medium">إدارة</th>
                    </tr>
                  </thead>
                  <tbody>
                    {visibleSections.map((s) => (
                      <tr key={s.label} className="border-b border-border last:border-0">
                        <td className="p-2.5">{s.label}</td>
                        <td className="p-2.5 text-center">
                          {s.view && catalogSet.has(s.view) && (
                            <input
                              type="checkbox"
                              aria-label={`عرض ${s.label}`}
                              checked={selected.has(s.view)}
                              onChange={() => toggleView(s.view as string, s.manage)}
                            />
                          )}
                        </td>
                        <td className="p-2.5 text-center">
                          {s.manage && catalogSet.has(s.manage) && (
                            <input
                              type="checkbox"
                              aria-label={`إدارة ${s.label}`}
                              checked={selected.has(s.manage)}
                              onChange={() => toggleManage(s.manage as string, s.view)}
                            />
                          )}
                        </td>
                      </tr>
                    ))}
                    {extraCodes.map((code) => (
                      <tr key={code} className="border-b border-border last:border-0">
                        <td className="dir-ltr p-2.5 text-start font-mono text-xs" colSpan={2}>
                          {code}
                        </td>
                        <td className="p-2.5 text-center">
                          <input
                            type="checkbox"
                            aria-label={code}
                            checked={selected.has(code)}
                            onChange={() => toggleSimple(code)}
                          />
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
            {permError && <p className="text-xs text-danger">{permError}</p>}
          </div>

          <DialogFooter>
            <Button type="submit" disabled={pending}>
              {role ? 'حفظ التغييرات' : 'إنشاء الدور'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
