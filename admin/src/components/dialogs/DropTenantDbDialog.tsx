import { useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { toast } from 'sonner'
import { adminApi } from '@/lib/api'
import { errorMessage } from '@/lib/auth'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Field } from './Field'
import type { Tenant } from '@/lib/types'

/** Confusable-free alphabet: no O/0, I/1, S/5 — the code is retyped by hand
 *  under pressure, and a misread character reads as "the code is wrong". */
const ALPHABET = 'ABCDEFGHJKLMNPQRTUVWXYZ2346789'

function newCode() {
  const bytes = new Uint32Array(4)
  crypto.getRandomValues(bytes)
  return Array.from(bytes, (b) => ALPHABET[b % ALPHABET.length]).join('')
}

interface Props {
  tenant: Tenant | null
  open: boolean
  onOpenChange: (open: boolean) => void
  onDropped?: () => void
}

// Dropping the central DB is the heaviest repair lever in this console: the
// tenant keeps working (branches are the source of truth) but everything merged
// into central — including other branches' history and whatever the console
// reports from — is gone until each branch re-uploads. The typed code exists to
// make that a deliberate act, not a mis-click next to "Re-provision sync".
export function DropTenantDbDialog({
  tenant,
  open,
  onOpenChange,
  onDropped,
}: Props) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      {/* Body is mounted only while open, so the challenge code and the typed
          input reset themselves on every opening — no effect needed, and no
          code carried over from a previous run. */}
      {open && tenant && (
        <Body tenant={tenant} onOpenChange={onOpenChange} onDropped={onDropped} />
      )}
    </Dialog>
  )
}

function Body({
  tenant,
  onOpenChange,
  onDropped,
}: {
  tenant: Tenant
  onOpenChange: (open: boolean) => void
  onDropped?: () => void
}) {
  const [code] = useState(newCode)
  const [typed, setTyped] = useState('')

  const mutation = useMutation({
    mutationFn: (tenantId: string) => adminApi.dropTenantDb(tenantId),
    onSuccess: (r) => {
      toast.success(`Central DB dropped: ${r.db_name}`)
      onDropped?.()
      onOpenChange(false)
    },
    onError: (e) => toast.error(errorMessage(e)),
  })

  const matches = typed.trim().toUpperCase() === code

  return (
    <DialogContent>
      <DialogHeader>
        <DialogTitle>Drop the central database?</DialogTitle>
        <DialogDescription>
          Deletes{' '}
          <span className="font-mono text-foreground/80">{tenant.DBName}</span>{' '}
          on the shard. The tenant, its company, branches and device seats are
          not touched, and it keeps the same DB name — the gateway recreates the
          database empty on the next sync and provisions the current schema from
          scratch.
        </DialogDescription>
      </DialogHeader>

      {/* Arabic operator warning — what this actually costs. */}
      <div
        dir="rtl"
        className="rounded-md border border-danger/40 bg-danger/5 p-3 text-sm leading-relaxed"
      >
        <div className="font-medium text-danger">حذف قاعدة البيانات المركزية</div>
        <p className="mt-1.5 text-muted-foreground">
          يحذف قاعدة بيانات المزامنة المركزية لهذا العميل نهائيًا. لا يُحذف العميل
          ولا شركته ولا فروعه ولا أجهزته، وتبقى القاعدة بنفس الاسم — تُعاد إنشاؤها{' '}
          <span className="text-foreground">فارغة</span> في أول مزامنة، وتُهيّأ على
          مخطط المزامنة الحالي من جديد. استخدمه عندما تكون القاعدة المركزية تالفة
          بما لا يمكن إصلاحه في مكانه.
        </p>
        <p className="mt-2 text-muted-foreground">
          <span className="text-danger">تنبيه:</span> البيانات
          <span className="text-foreground"> لا تعود تلقائيًا</span>. كل فرع يحتفظ
          بعلامة آخر مزامنة عنده، فلن يرفع شيئًا للقاعدة الجديدة من تلقاء نفسه —
          يجب على كل فرع تشغيل
          <span className="text-foreground"> «إعادة المزامنة الكاملة» </span>
          ثم <span className="text-foreground">«تمييز الصفوف غير المتتبعة»</span> من
          إعدادات المزامنة. وما كان في المركز من فروع لم تعد تزامن
          <span className="text-foreground"> يُفقد نهائيًا</span> (تقارير الكونسول
          تُبنى من هذه القاعدة).
        </p>
      </div>

      <Field
        label="Type the code to confirm"
        hint="Case-insensitive. A new code is generated every time this dialog opens."
      >
        <div className="grid gap-2">
          <div className="select-none rounded-md border border-border bg-muted/50 px-3 py-2 text-center font-mono text-2xl font-semibold tracking-[0.4em] text-foreground">
            {code}
          </div>
          <Input
            value={typed}
            onChange={(e) => setTyped(e.target.value)}
            placeholder="Type the code above"
            autoComplete="off"
            spellCheck={false}
            className="text-center font-mono uppercase tracking-[0.3em]"
          />
        </div>
      </Field>

      <DialogFooter>
        <Button
          type="button"
          variant="ghost"
          onClick={() => onOpenChange(false)}
          disabled={mutation.isPending}
        >
          Cancel
        </Button>
        <Button
          type="button"
          variant="destructive"
          disabled={!matches || mutation.isPending}
          onClick={() => mutation.mutate(tenant.ID)}
        >
          {mutation.isPending ? 'Dropping…' : 'Drop database'}
        </Button>
      </DialogFooter>
    </DialogContent>
  )
}
