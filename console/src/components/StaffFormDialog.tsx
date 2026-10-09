import { useMemo, useState } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { z } from 'zod'
import { toast } from 'sonner'
import { errorMessage } from '@/lib/auth'
import {
  useBundle,
  useClearStaffLockout,
  useCreateStaff,
  usePosRoles,
  useUpdateStaff,
} from '@/lib/hooks'
import { useScope } from '@/lib/perm'
import { markStaffSaved } from '@/lib/staffSync'
import type { StaffInput, StaffMember } from '@/lib/types'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

const selectClass =
  'flex h-9 w-full rounded-md border border-input bg-background/40 px-3 py-1 text-sm shadow-sm transition-colors focus-visible:outline-none focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/30'

/**
 * Mirrors the API's validation table (hq/service_staff.go `validateStaff`,
 * itself mirroring the gateway's ValidateStaff) so the manager sees the error
 * before the round trip. Password rules differ by mode: required on create,
 * optional on edit (blank keeps the current one).
 */
function makeSchema(isCreate: boolean) {
  return z.object({
    name: z.string().trim().min(1, 'اسم الموظف مطلوب').max(50, 'اسم الموظف لا يتجاوز ٥٠ حرفًا'),
    loginName: z
      .string()
      .trim()
      .min(3, 'اسم الدخول من ٣ إلى ٥٠ حرفًا')
      .max(50, 'اسم الدخول من ٣ إلى ٥٠ حرفًا')
      .refine((v) => !/\s/.test(v), 'اسم الدخول لا يحتوي على مسافات'),
    password: z
      .string()
      .refine((v) => (isCreate ? v.length > 0 : true), 'كلمة المرور مطلوبة')
      .refine((v) => v === '' || v.length >= 6, 'كلمة المرور لا تقل عن ٦ أحرف'),
    branchId: z.string().min(1, 'اختر الفرع'),
    pin: z
      .string()
      .refine((v) => v === '' || /^[0-9]+$/.test(v), 'رقم PIN يجب أن يتكون من أرقام فقط')
      .refine((v) => v === '' || v.length >= 4, 'رقم PIN يجب ألا يقل عن ٤ أرقام')
      .refine((v) => v.length <= 12, 'رقم PIN يجب ألا يزيد عن ١٢ رقمًا'),
  })
}
type Form = z.infer<ReturnType<typeof makeSchema>>

type PinMode = 'keep' | 'set' | 'clear'

/**
 * Create / edit a branch staff member — an AribOne POS user, NOT a console
 * member. One dialog for both; `existing` decides. Edit sends the whole form.
 *
 * Credentials: the password and PIN fields are write-only. Nothing is ever
 * prefilled (the server never returns them), a blank password on edit means
 * "keep", and the PIN has three explicit states on edit (keep / set / remove) so
 * a PIN can never be wiped by leaving a field empty.
 */
export function StaffFormDialog({
  tenantId,
  existing,
  onClose,
}: {
  tenantId: string
  existing?: StaffMember
  onClose: () => void
}) {
  const isCreate = !existing
  const { data: bundle } = useBundle(tenantId)
  const rolesQuery = usePosRoles(tenantId)
  const create = useCreateStaff(tenantId)
  const update = useUpdateStaff(tenantId)
  const clearLockout = useClearStaffLockout(tenantId)

  // A branch-scoped member can only place staff at their own branches; offering
  // others would hand them a 403 they could not have anticipated. (A row they
  // can reach is always at a branch they hold — the server 404s the others.)
  const scope = useScope(tenantId)
  const branches = (bundle?.Branches ?? []).filter(
    (b) => !scope || scope.branch_ids.length === 0 || scope.branch_ids.includes(b.ID),
  )

  const [roleIds, setRoleIds] = useState<string[]>(existing?.role_ids ?? [])
  const [pinMode, setPinMode] = useState<PinMode>(existing?.has_pin ? 'keep' : 'set')
  // Plain state, not a form field: the deactivate note below reacts to it, and
  // watching a form value is an API this repo avoids (React Compiler lint).
  const [active, setActive] = useState(existing?.is_active ?? true)
  const [lockedNow, setLockedNow] = useState(existing?.pin_locked ?? false)

  const schema = useMemo(() => makeSchema(isCreate), [isCreate])
  const form = useForm<Form>({
    resolver: zodResolver(schema),
    defaultValues: {
      name: existing?.name ?? '',
      loginName: existing?.login_name ?? '',
      password: '',
      branchId: existing?.branch_id ?? (branches.length === 1 ? branches[0].ID : ''),
      pin: '',
    },
  })

  function toggleRole(id: string) {
    setRoleIds((cur) => (cur.includes(id) ? cur.filter((r) => r !== id) : [...cur, id]))
  }

  async function onClearLockout() {
    if (!existing) return
    try {
      await clearLockout.mutateAsync(existing.id)
      setLockedNow(false)
      toast.success('تم إلغاء قفل الـ PIN')
    } catch (err) {
      toast.error(errorMessage(err))
    }
  }

  const submit = form.handleSubmit(async (values) => {
    const setting = pinMode === 'set' && values.pin !== ''
    if (existing && pinMode === 'set' && values.pin === '' && existing.has_pin) {
      form.setError('pin', { message: 'أدخل رقم PIN الجديد أو اختر الإبقاء عليه' })
      return
    }
    const input: StaffInput = {
      name: values.name,
      login_name: values.loginName,
      branch_id: values.branchId,
      is_active: active,
      role_ids: roleIds,
      // Absent, not empty, when unchanged: the gateway reads absence as "keep".
      ...(values.password !== '' ? { password: values.password } : {}),
      ...(setting ? { pin: values.pin } : {}),
      ...(existing && pinMode === 'clear' ? { clear_pin: true } : {}),
    }
    try {
      if (existing) {
        await update.mutateAsync({ staffId: existing.id, input })
        markStaffSaved(tenantId, existing.id)
        toast.success('تم حفظ بيانات الموظف — تصل إلى الفرع عند المزامنة القادمة')
      } else {
        const res = await create.mutateAsync(input)
        markStaffSaved(tenantId, res.id)
        toast.success('تم إنشاء الموظف — يصل إلى الفرع عند المزامنة القادمة')
      }
      onClose()
    } catch (err) {
      toast.error(errorMessage(err))
    }
  })

  const roles = rolesQuery.data?.data.roles ?? []
  const pending = create.isPending || update.isPending

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{existing ? 'تعديل موظف فرع' : 'موظف فرع جديد'}</DialogTitle>
        </DialogHeader>

        <p className="rounded-lg border border-border bg-muted/40 p-3 text-xs text-muted-foreground">
          هذا حساب يسجّل به الموظف الدخول إلى برنامج AribOne في الفرع، وليس عضوًا في لوحة التحكم.
        </p>

        <form onSubmit={submit} className="space-y-4" noValidate autoComplete="off">
          <div className="space-y-1.5">
            <Label htmlFor="staff-name">
              اسم الموظف<span className="text-danger"> *</span>
            </Label>
            <Input id="staff-name" autoFocus {...form.register('name')} />
            {form.formState.errors.name && (
              <p className="text-xs text-danger">{form.formState.errors.name.message}</p>
            )}
          </div>

          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1.5">
              <Label htmlFor="staff-login">
                اسم الدخول<span className="text-danger"> *</span>
              </Label>
              <Input
                id="staff-login"
                dir="ltr"
                className="text-start"
                autoComplete="off"
                {...form.register('loginName')}
              />
              {form.formState.errors.loginName && (
                <p className="text-xs text-danger">{form.formState.errors.loginName.message}</p>
              )}
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="staff-password">
                كلمة المرور{isCreate && <span className="text-danger"> *</span>}
              </Label>
              <Input
                id="staff-password"
                type="password"
                dir="ltr"
                className="text-start"
                autoComplete="new-password"
                placeholder={isCreate ? undefined : 'اتركه فارغًا للإبقاء على الحالية'}
                {...form.register('password')}
              />
              {form.formState.errors.password && (
                <p className="text-xs text-danger">{form.formState.errors.password.message}</p>
              )}
            </div>
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="staff-branch">
              الفرع<span className="text-danger"> *</span>
            </Label>
            <select id="staff-branch" className={selectClass} {...form.register('branchId')}>
              <option value="">اختر الفرع</option>
              {branches.map((b) => (
                <option key={b.ID} value={b.ID}>
                  {b.Name}
                </option>
              ))}
            </select>
            {form.formState.errors.branchId && (
              <p className="text-xs text-danger">{form.formState.errors.branchId.message}</p>
            )}
          </div>

          <fieldset className="space-y-2">
            <legend className="text-sm font-medium">رقم PIN لشاشات اللمس (AribLink)</legend>
            {existing?.has_pin && (
              <div className="flex flex-wrap gap-4 text-sm">
                {(
                  [
                    ['keep', 'الإبقاء على الحالي'],
                    ['set', 'تغيير'],
                    ['clear', 'حذف الـ PIN'],
                  ] as const
                ).map(([value, label]) => (
                  <label key={value} className="flex items-center gap-1.5">
                    <input
                      type="radio"
                      name="staff-pin-mode"
                      className="size-4"
                      checked={pinMode === value}
                      onChange={() => setPinMode(value)}
                    />
                    {label}
                  </label>
                ))}
              </div>
            )}
            {pinMode === 'set' && (
              <>
                <Input
                  id="staff-pin"
                  type="password"
                  inputMode="numeric"
                  dir="ltr"
                  className="text-start"
                  autoComplete="new-password"
                  placeholder="٤ أرقام على الأقل — اتركه فارغًا لموظف بلا PIN"
                  {...form.register('pin')}
                />
                {form.formState.errors.pin && (
                  <p className="text-xs text-danger">{form.formState.errors.pin.message}</p>
                )}
              </>
            )}
            {lockedNow && (
              <div className="flex items-center justify-between gap-2 rounded-lg border border-warning/30 bg-warning/10 p-2.5 text-xs text-warning">
                <span>الـ PIN مقفول بعد محاولات خاطئة متكررة.</span>
                <Button
                  type="button"
                  size="sm"
                  variant="outline"
                  disabled={clearLockout.isPending}
                  onClick={() => void onClearLockout()}
                >
                  إلغاء القفل
                </Button>
              </div>
            )}
          </fieldset>

          <fieldset className="space-y-2">
            <legend className="text-sm font-medium">أدوار نقطة البيع</legend>
            {rolesQuery.isLoading ? (
              <p className="text-xs text-muted-foreground">جارٍ تحميل الأدوار…</p>
            ) : roles.length === 0 ? (
              <p className="text-xs text-muted-foreground">لا توجد أدوار بعد.</p>
            ) : (
              <div className="grid gap-1.5 sm:grid-cols-2">
                {roles.map((r) => (
                  <label key={r.id} className="flex items-start gap-2 text-sm" title={r.description}>
                    <input
                      type="checkbox"
                      className="mt-0.5 size-4"
                      checked={roleIds.includes(r.id)}
                      onChange={() => toggleRole(r.id)}
                    />
                    <span>{r.name}</span>
                  </label>
                ))}
              </div>
            )}
          </fieldset>

          <div className="space-y-1.5">
            <label className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                className="size-4"
                checked={active}
                onChange={(e) => setActive(e.target.checked)}
              />
              مُفعَّل
            </label>
            {existing?.is_active && !active && (
              <p className="text-xs text-warning">
                إيقاف الموظف يمنع دخوله القادم بعد وصول التغيير إلى الفرع. من سجّل الدخول بالفعل يبقى
                مسجّلًا حتى يخرج. لا يُحذف الموظف: فواتيره وطلباته تبقى منسوبة إليه.
              </p>
            )}
          </div>

          <DialogFooter>
            <Button type="button" variant="ghost" onClick={onClose}>
              إلغاء
            </Button>
            <Button type="submit" disabled={pending}>
              {existing ? 'حفظ' : 'إنشاء'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
