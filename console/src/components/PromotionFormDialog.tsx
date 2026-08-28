import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { z } from 'zod'
import { toast } from 'sonner'
import { errorMessage } from '@/lib/auth'
import { useBundle, useCreatePromotion, useUpdatePromotion } from '@/lib/hooks'
import { PERM, useCanUnscoped, useScope } from '@/lib/perm'
import { fromDateInput, toDateInput } from '@/lib/format'
import {
  DISCOUNT_TYPE,
  PROMOTION_LEVEL,
  PROMOTION_SCOPE,
  type PromotionDetail,
  type PromotionInput,
} from '@/lib/types'
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
import { PromotionTargetPicker, type PickedTarget } from '@/components/PromotionTargetPicker'

const selectClass =
  'flex h-9 w-full rounded-md border border-input bg-background/40 px-3 py-1 text-sm shadow-sm transition-colors focus-visible:outline-none focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/30'

/**
 * Mirrors the server's validation table (hq/service_promotions.go's
 * `validatePromotion`, itself mirroring HqApi.ValidatePromotion) so the author
 * sees the error before the round trip — three copies on purpose: this one for
 * immediacy, the API's for the contract, the gateway's because the database is
 * the last line and a caller who skips the console must not be able to store a
 * 150% promotion.
 *
 * The level cross-checks live in `superRefine` because they are relations
 * between fields, not properties of one.
 */
const schema = z
  .object({
    name: z.string().trim().min(1, 'اسم العرض مطلوب').max(100, 'اسم العرض لا يتجاوز ١٠٠ حرف'),
    discountType: z.number(),
    value: z.number('أدخل رقمًا صحيحًا').positive('قيمة الخصم يجب أن تكون أكبر من صفر'),
    branchId: z.string(),
    startsOn: z.string().min(1, 'تاريخ البداية مطلوب'),
    endsOn: z.string().min(1, 'تاريخ الانتهاء مطلوب'),
    isActive: z.boolean(),
    minQty: z.string(),
    minBillTotal: z.string(),
  })
  .superRefine((v, ctx) => {
    if (v.discountType === DISCOUNT_TYPE.Percentage && v.value > 100) {
      ctx.addIssue({ code: 'custom', path: ['value'], message: 'نسبة الخصم لا تتجاوز ١٠٠٪' })
    }
    if (v.endsOn && v.startsOn && v.endsOn < v.startsOn) {
      ctx.addIssue({
        code: 'custom',
        path: ['endsOn'],
        message: 'تاريخ الانتهاء قبل تاريخ البداية',
      })
    }
    if (v.minQty !== '' && !(Number(v.minQty) > 0)) {
      ctx.addIssue({
        code: 'custom',
        path: ['minQty'],
        message: 'الحد الأدنى للكمية يجب أن يكون أكبر من صفر',
      })
    }
    if (v.minBillTotal !== '' && !(Number(v.minBillTotal) > 0)) {
      ctx.addIssue({
        code: 'custom',
        path: ['minBillTotal'],
        message: 'الحد الأدنى للفاتورة يجب أن يكون أكبر من صفر',
      })
    }
  })

type Form = z.infer<typeof schema>

/**
 * One dialog for both create and edit — `existing` decides which. A promotion
 * is small and wholly authored, so edit sends the complete form back rather
 * than a partial patch, matching the API's single `PromotionInput`.
 */
export function PromotionFormDialog({
  tenantId,
  existing,
  onClose,
}: {
  tenantId: string
  existing?: PromotionDetail
  onClose: () => void
}) {
  const { data: bundle } = useBundle(tenantId)
  const create = useCreatePromotion(tenantId)
  const update = useUpdatePromotion(tenantId)

  // D14: a company-wide promotion is a Tier-A write landing at every branch,
  // so it takes an unscoped member. The option is HIDDEN rather than shown and
  // rejected — the console should not let a 403 be the first a branch-scoped
  // member hears of a rule, the same treatment product-create already gets.
  const canCompanyWide = useCanUnscoped(tenantId, PERM.PromotionsManage)

  // The branch list is narrowed to the member's own allowlist, for the same
  // reason the company-wide option is hidden: a scoped member picking a branch
  // they do not hold gets a 403 they had no way to anticipate. (Note the
  // existing CreateCustomerDialog does NOT do this — it filters on branch
  // status only. That is a pre-existing gap left alone here rather than fixed
  // in passing.) Any promotion a scoped member can reach is already at a branch
  // they hold — the server 404s the others — so editing can never lose a branch
  // this filter would have hidden.
  // Named memberScope, not scope: `scope` in this file is the PROMOTION's
  // scope (all-products / products / groups), an unrelated meaning.
  const memberScope = useScope(tenantId)
  const branches = (bundle?.Branches ?? []).filter(
    (b) =>
      !memberScope ||
      memberScope.branch_ids.length === 0 ||
      memberScope.branch_ids.includes(b.ID),
  )

  const [targets, setTargets] = useState<PickedTarget[]>(
    existing?.targets.map((t) => ({ kind: t.kind, ref_id: t.ref_id })) ?? [],
  )
  const [targetError, setTargetError] = useState<string>()

  // level and scope live in plain state, not in the form. Both are selects this
  // component already drives by hand (see setLevel/setScope, which cascade to
  // other fields), so registering them would have bought nothing but a
  // `form.watch` subscription — an API the repo uses nowhere else and which the
  // React Compiler lint rule flags as unmemoizable.
  const [level, setLevelState] = useState<number>(existing?.level ?? PROMOTION_LEVEL.Item)
  const [scope, setScopeState] = useState<number>(existing?.scope ?? PROMOTION_SCOPE.AllProducts)

  const form = useForm<Form>({
    resolver: zodResolver(schema),
    defaultValues: existing
      ? {
          name: existing.name,
          discountType: existing.discount_type,
          value: existing.value,
          branchId: existing.branch_id ?? '',
          startsOn: toDateInput(existing.starts_on),
          endsOn: toDateInput(existing.ends_on),
          isActive: existing.is_active,
          minQty: existing.min_qty == null ? '' : String(existing.min_qty),
          minBillTotal: existing.min_bill_total == null ? '' : String(existing.min_bill_total),
        }
      : {
          name: '',
          discountType: DISCOUNT_TYPE.Percentage,
          value: 10,
          // A scoped member cannot author company-wide, so their form opens on
          // their first branch rather than on an option they cannot pick.
          branchId: canCompanyWide ? '' : (branches[0]?.ID ?? ''),
          startsOn: toDateInput(new Date().toISOString()),
          endsOn: '',
          isActive: true,
          minQty: '',
          minBillTotal: '',
        },
  })

  const isBill = level === PROMOTION_LEVEL.Bill
  const needsTargets = !isBill && scope !== PROMOTION_SCOPE.AllProducts

  // Switching to a bill promotion, or to storewide, DROPS the target list
  // instead of carrying it invisibly in the payload (D3). The server refuses
  // both shapes, but a stale list surviving in state would mean the author's
  // next save fails for a reason nothing on screen explains.
  function setLevel(next: number) {
    setLevelState(next)
    if (next === PROMOTION_LEVEL.Bill) {
      setScopeState(PROMOTION_SCOPE.AllProducts)
      setTargets([])
      form.setValue('minQty', '')
    } else {
      form.setValue('minBillTotal', '')
    }
    setTargetError(undefined)
  }

  // Always clears the selection — including when moving between Products and
  // Groups, where keeping it would leave targets whose Kind no longer matches
  // the scope. The server refuses exactly that shape.
  function setScope(next: number) {
    setScopeState(next)
    setTargets([])
    setTargetError(undefined)
  }

  const submit = form.handleSubmit(async (values) => {
    if (needsTargets && targets.length === 0) {
      setTargetError('اختر صنفاً واحداً على الأقل')
      return
    }
    const input: PromotionInput = {
      name: values.name,
      level: level as PromotionInput['level'],
      scope: scope as PromotionInput['scope'],
      discount_type: values.discountType as PromotionInput['discount_type'],
      value: values.value,
      branch_id: values.branchId || null,
      starts_on: fromDateInput(values.startsOn),
      ends_on: fromDateInput(values.endsOn),
      is_active: values.isActive,
      min_qty: isBill || values.minQty === '' ? null : Number(values.minQty),
      min_bill_total: !isBill || values.minBillTotal === '' ? null : Number(values.minBillTotal),
      targets: isBill || scope === PROMOTION_SCOPE.AllProducts ? [] : targets,
    }
    try {
      if (existing) {
        await update.mutateAsync({ promotionId: existing.id, input })
        toast.success('تم تحديث العرض')
      } else {
        await create.mutateAsync(input)
        toast.success('تم إنشاء العرض')
      }
      onClose()
    } catch (err) {
      toast.error(errorMessage(err))
    }
  })

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{existing ? 'تعديل العرض' : 'عرض جديد'}</DialogTitle>
        </DialogHeader>

        <form onSubmit={submit} className="space-y-4" noValidate>
          <div className="space-y-1.5">
            <Label htmlFor="promo-name">
              اسم العرض<span className="text-danger"> *</span>
            </Label>
            <Input id="promo-name" autoFocus {...form.register('name')} />
            <p className="text-xs text-muted-foreground">يظهر على الفاتورة وعلى الإيصال.</p>
            {form.formState.errors.name && (
              <p className="text-xs text-danger">{form.formState.errors.name.message}</p>
            )}
          </div>

          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1.5">
              <Label htmlFor="promo-level">المستوى</Label>
              <select
                id="promo-level"
                className={selectClass}
                value={level}
                onChange={(e) => setLevel(Number(e.target.value))}
              >
                <option value={PROMOTION_LEVEL.Item}>خصم صنف</option>
                <option value={PROMOTION_LEVEL.Bill}>خصم فاتورة</option>
              </select>
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="promo-branch">النطاق</Label>
              <select id="promo-branch" className={selectClass} {...form.register('branchId')}>
                {/* D14: hidden, not disabled, for a branch-scoped member. */}
                {canCompanyWide && <option value="">الشركة كلها</option>}
                {branches.map((b) => (
                  <option key={b.ID} value={b.ID}>
                    {b.Name}
                  </option>
                ))}
              </select>
            </div>
          </div>

          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1.5">
              <Label htmlFor="promo-type">نوع الخصم</Label>
              <select
                id="promo-type"
                className={selectClass}
                {...form.register('discountType', { valueAsNumber: true })}
              >
                <option value={DISCOUNT_TYPE.Percentage}>نسبة مئوية</option>
                <option value={DISCOUNT_TYPE.Fixed}>مبلغ ثابت</option>
              </select>
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="promo-value">
                القيمة<span className="text-danger"> *</span>
              </Label>
              <Input
                id="promo-value"
                type="number"
                step="0.01"
                min="0"
                dir="ltr"
                className="text-start"
                {...form.register('value', { valueAsNumber: true })}
              />
              {form.formState.errors.value && (
                <p className="text-xs text-danger">{form.formState.errors.value.message}</p>
              )}
            </div>
          </div>

          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1.5">
              <Label htmlFor="promo-starts">
                من<span className="text-danger"> *</span>
              </Label>
              <Input id="promo-starts" type="date" dir="ltr" {...form.register('startsOn')} />
              {form.formState.errors.startsOn && (
                <p className="text-xs text-danger">{form.formState.errors.startsOn.message}</p>
              )}
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="promo-ends">
                إلى<span className="text-danger"> *</span>
              </Label>
              <Input id="promo-ends" type="date" dir="ltr" {...form.register('endsOn')} />
              {form.formState.errors.endsOn && (
                <p className="text-xs text-danger">{form.formState.errors.endsOn.message}</p>
              )}
            </div>
          </div>
          <p className="-mt-2 text-xs text-muted-foreground">
            التاريخان شاملان — عرض ينتهي ٣١ أغسطس يظل ساريًا طوال ذلك اليوم.
          </p>

          {/* Fields irrelevant to the chosen level are removed from the form,
              not greyed out: a disabled control still reads as "something I
              could turn on", which is exactly what a bill promotion's item
              threshold is not. */}
          {isBill ? (
            <div className="space-y-1.5">
              <Label htmlFor="promo-min-bill">حد أدنى لإجمالي الفاتورة</Label>
              <Input
                id="promo-min-bill"
                type="number"
                step="0.01"
                min="0"
                dir="ltr"
                className="text-start"
                placeholder="بدون حد أدنى"
                {...form.register('minBillTotal')}
              />
              {form.formState.errors.minBillTotal && (
                <p className="text-xs text-danger">
                  {form.formState.errors.minBillTotal.message}
                </p>
              )}
            </div>
          ) : (
            <>
              <div className="grid grid-cols-2 gap-3">
                <div className="space-y-1.5">
                  <Label htmlFor="promo-scope">يطبق على</Label>
                  <select
                    id="promo-scope"
                    className={selectClass}
                    value={scope}
                    onChange={(e) => setScope(Number(e.target.value))}
                  >
                    <option value={PROMOTION_SCOPE.AllProducts}>كل الأصناف</option>
                    <option value={PROMOTION_SCOPE.Products}>أصناف محددة</option>
                    <option value={PROMOTION_SCOPE.Groups}>مجموعات</option>
                  </select>
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="promo-min-qty">حد أدنى للكمية</Label>
                  <Input
                    id="promo-min-qty"
                    type="number"
                    step="0.001"
                    min="0"
                    dir="ltr"
                    className="text-start"
                    placeholder="بدون حد أدنى"
                    {...form.register('minQty')}
                  />
                  {form.formState.errors.minQty && (
                    <p className="text-xs text-danger">{form.formState.errors.minQty.message}</p>
                  )}
                </div>
              </div>

              {needsTargets && (
                <PromotionTargetPicker
                  tenantId={tenantId}
                  scope={scope as PromotionInput['scope']}
                  value={targets}
                  onChange={(next) => {
                    setTargets(next)
                    if (next.length > 0) setTargetError(undefined)
                  }}
                  error={targetError}
                />
              )}
            </>
          )}

          <label className="flex items-center gap-2 text-sm">
            <input type="checkbox" className="size-4" {...form.register('isActive')} />
            مُفعَّل
          </label>

          <DialogFooter>
            <Button type="button" variant="ghost" onClick={onClose}>
              إلغاء
            </Button>
            <Button type="submit" disabled={create.isPending || update.isPending}>
              {existing ? 'حفظ' : 'إنشاء'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
