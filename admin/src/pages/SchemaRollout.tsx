import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import {
  AlertTriangle,
  CheckCircle2,
  DatabaseZap,
  RefreshCw,
  Server,
} from 'lucide-react'
import { adminApi } from '@/lib/api'
import { errorMessage } from '@/lib/auth'
import { qk } from '@/lib/query'
import { PageHeader } from '@/components/PageHeader'
import { StatCard } from '@/components/StatCard'
import { CopyId } from '@/components/CopyId'
import { ConfirmDialog } from '@/components/ConfirmDialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
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
import type { SchemaReport, TenantSchemaState } from '@/lib/types'

/** A tenant is only "done" when it sits exactly on the gateway's version and
 *  its last migrate didn't fail — the same exact-equality rule the gateway
 *  enforces on every /sync (no backward-compatibility band). */
function isAtTarget(t: TenantSchemaState, target: number) {
  return t.schema_version === target && t.rollout_status !== 'failed'
}

function statusBadge(t: TenantSchemaState, target: number) {
  if (t.rollout_status === 'failed') return <Badge tone="danger">Failed</Badge>
  if (t.rollout_status === 'migrating')
    return <Badge tone="info">Migrating…</Badge>
  if (t.schema_version !== target) return <Badge tone="warning">Behind</Badge>
  return <Badge tone="success">Up to date</Badge>
}

export function SchemaRollout() {
  const qc = useQueryClient()
  const [confirmOpen, setConfirmOpen] = useState(false)
  const report = useQuery({
    queryKey: qk.schemaReport,
    queryFn: adminApi.schemaReport,
  })

  const rollout = useMutation({
    mutationFn: adminApi.rollout,
    onSuccess: (rep: SchemaReport) => {
      qc.setQueryData(qk.schemaReport, rep)
      const tenants = rep.tenants ?? []
      const done = tenants.filter((t) => isAtTarget(t, rep.target_version)).length
      const failed = rep.failed?.length ?? 0
      if (failed > 0) {
        toast.error(
          `Rollout finished with ${failed} failed tenant${failed === 1 ? '' : 's'}`,
        )
      } else {
        toast.success(`Rollout complete — ${done}/${tenants.length} at v${rep.target_version}`)
      }
    },
    onError: (e) => toast.error(errorMessage(e)),
    // Refetch whichever way the request ends. Rollout() records each tenant's
    // result as it goes, and a fleet-wide run can outlive the reverse proxy's
    // response timeout — so a failed request does NOT mean a failed rollout.
    // Without this the operator reads a red toast over a stale table and runs
    // it a second time on top of one that is still working.
    onSettled: () => qc.invalidateQueries({ queryKey: qk.schemaReport }),
  })

  const data = report.data
  const target = data?.target_version ?? 0
  const tenants = data?.tenants ?? []
  const atTarget = tenants.filter((t) => isAtTarget(t, target)).length
  const behind = tenants.length - atTarget
  const failed = data?.failed?.length ?? 0
  const unreachable = data?.unreachable_shards ?? []

  return (
    <div>
      <PageHeader
        title="Schema rollout"
        description="Bring every sync tenant's central DB up to the gateway's schema version."
      >
        <Button
          variant="outline"
          onClick={() => report.refetch()}
          disabled={report.isFetching || rollout.isPending}
        >
          <RefreshCw className={report.isFetching ? 'animate-spin' : undefined} />
          Refresh
        </Button>
        <Button
          onClick={() => setConfirmOpen(true)}
          disabled={rollout.isPending || report.isLoading}
        >
          <DatabaseZap />
          {rollout.isPending ? 'Rolling out…' : 'Run rollout'}
        </Button>
        <ConfirmDialog
          open={confirmOpen}
          onOpenChange={setConfirmOpen}
          title="Run the fleet schema rollout?"
          confirmLabel="Run rollout"
          description={
            <span>
              Migrates every sync tenant that is behind v{target} (and retries
              previously failed ones). Each tenant's <code>/sync</code> is frozen
              only for the duration of its own migrate call. Safe to repeat —
              tenants already at the target are skipped. This can take several
              minutes on a large fleet.
            </span>
          }
          onConfirm={async () => {
            await rollout.mutateAsync()
          }}
        />
      </PageHeader>

      {/* Arabic operator brief — what this screen actually does, and the cost of
          skipping it. Kept RTL inside an otherwise LTR console. */}
      <Card className="mb-6 p-5" dir="rtl">
        <h2 className="font-display text-base font-semibold">
          ترقية مخطط المزامنة (Schema rollout)
        </h2>
        <p className="mt-2 text-sm leading-relaxed text-muted-foreground">
          عند صدور إصدار يغيّر <span className="text-foreground">شكل</span> المزامنة —
          إضافة جدول، حذفه، إعادة تسميته، أو تغيير الأعمدة المتزامنة — يرتفع رقم
          «إصدار المخطط» في البوابة. قاعدة البيانات المركزية لكل عميل
          <span className="text-foreground"> لا تلتقط الشكل الجديد من تلقاء نفسها</span>،
          وهذه الشاشة هي الخطوة التي تفرض ذلك.
        </p>
        <ul className="mt-3 space-y-2 text-sm leading-relaxed text-muted-foreground">
          <li>
            <span className="text-foreground">ماذا يفعل الزر بالضبط؟</span> يقرأ رقم
            الإصدار الذي تعلنه البوابة، يمرّ على كل عميل مشترك في المزامنة، ويستدعي
            <code className="mx-1 text-foreground">‎/admin/migrate‎</code>
            لكل عميل متأخّر. البوابة عندها تطبّق ترحيلات قاعدة البيانات، ثم
            <span className="text-foreground"> تعيد تهيئة نطاق المزامنة </span>
            (إعادة توليد المشغّلات والإجراءات المخزّنة) — وهي الخطوة التي لا تحدث
            أبدًا من تلقاء نفسها في أي مزامنة عادية.
          </li>
          <li>
            <span className="text-foreground">متى تشغّله؟</span> فور نشر إصدار جديد من
            البوابة يرفع رقم إصدار المخطط، وقبل أن تبدأ الفروع مزامنتها — لا تؤجّله
            إلى وقت ذروة العميل، فأول مزامنة للفرع بعد الترقية تستغرق وقتًا أطول من
            المعتاد بحسب حجم بياناته التاريخية.
          </li>
          <li>
            <span className="text-foreground">وإن لم تشغّله؟</span> الجداول الجديدة
            تُزامَن كعملية صامتة بلا أي أثر — لا خطأ ولا تحذير، وبيانات الميزة الجديدة
            لا تصل إلى المركز إطلاقًا. وإذا كان الإصدار يتضمّن
            <span className="text-foreground"> إعادة تسمية جدول</span>، فالنتيجة أسوأ:
            تتوقّف مزامنة العميل كليًا برسالة
            <code className="mx-1 text-foreground" dir="ltr">
              Invalid object name
            </code>
            لأن إجراءات المزامنة المخزّنة ما زالت تشير إلى الاسم القديم.
          </li>
          <li>
            <span className="text-foreground">هل هو آمن؟</span> نعم — لا يلمس بيانات
            العميل ولا يحذف سجلّ التتبّع، ويمكن تكراره بلا ضرر: من هو محدَّث بالفعل
            يُتخطّى، ومن فشل يُعاد عليه في التشغيل التالي. مزامنة العميل تتوقّف مؤقتًا
            أثناء ترحيله هو فقط. الفروع بعد ذلك
            <span className="text-foreground"> تصلح نفسها تلقائيًا </span>
            في أول مزامنة، بلا أي تدخّل على أجهزة العملاء.
          </li>
          <li>
            <span className="text-foreground">العميل الفاشل</span> يظهر في الجدول أدناه
            مع نص الخطأ. أصلح السبب ثم شغّل الترقية مرة أخرى — لن تمسّ إلا المتأخّرين
            والفاشلين.
          </li>
        </ul>
      </Card>

      <div className="mb-6 grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <StatCard
          label="Target version"
          value={report.isLoading ? '—' : `v${target}`}
          icon={Server}
          accent
          hint="Advertised by the gateway"
        />
        <StatCard
          label="Up to date"
          value={report.isLoading ? '—' : atTarget}
          icon={CheckCircle2}
          hint={`of ${tenants.length} sync tenants`}
        />
        <StatCard
          label="Behind"
          value={report.isLoading ? '—' : behind}
          icon={DatabaseZap}
          hint="Will be migrated by a rollout"
        />
        <StatCard
          label="Failed"
          value={report.isLoading ? '—' : failed}
          icon={AlertTriangle}
          hint="Retried on the next run"
        />
      </div>

      {unreachable.length > 0 && (
        <Card className="mb-6 border-warning/40 p-4 text-sm">
          <span className="font-medium text-warning">Unreachable shards:</span>{' '}
          <span className="text-muted-foreground">{unreachable.join(', ')}</span>{' '}
          <span className="text-muted-foreground">
            — tenants on these shards were skipped, not migrated.
          </span>
        </Card>
      )}

      <Card className="overflow-hidden">
        <Table>
          <TableHeader>
            <TableRow className="hover:bg-transparent">
              <TableHead>Database</TableHead>
              <TableHead>Tenant</TableHead>
              <TableHead>Version</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Last error</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {report.isLoading ? (
              Array.from({ length: 6 }).map((_, i) => (
                <TableRow key={i} className="hover:bg-transparent">
                  <TableCell colSpan={5}>
                    <Skeleton className="h-6" />
                  </TableCell>
                </TableRow>
              ))
            ) : report.isError ? (
              <TableRow className="hover:bg-transparent">
                <TableCell
                  colSpan={5}
                  className="py-12 text-center text-sm text-danger"
                >
                  {errorMessage(report.error)}
                </TableCell>
              </TableRow>
            ) : tenants.length > 0 ? (
              tenants.map((t) => (
                <TableRow key={t.tenant_id} className="hover:bg-transparent">
                  <TableCell className="font-mono text-xs">{t.db_name}</TableCell>
                  <TableCell>
                    <CopyId value={t.tenant_id} label="Tenant" truncate />
                  </TableCell>
                  <TableCell className="tabular-nums text-sm">
                    v{t.schema_version}
                  </TableCell>
                  <TableCell>{statusBadge(t, target)}</TableCell>
                  <TableCell className="max-w-md text-xs text-muted-foreground">
                    {t.rollout_error || '—'}
                  </TableCell>
                </TableRow>
              ))
            ) : (
              <TableRow className="hover:bg-transparent">
                <TableCell
                  colSpan={5}
                  className="py-12 text-center text-sm text-muted-foreground"
                >
                  No sync-subscribed tenants yet.
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </Card>
    </div>
  )
}
