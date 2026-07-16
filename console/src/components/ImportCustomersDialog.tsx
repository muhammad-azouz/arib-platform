import { useState } from 'react'
import { toast } from 'sonner'
import { errorMessage } from '@/lib/auth'
import { useBundle, useImportCustomers } from '@/lib/hooks'
import { toArabicDigits } from '@/lib/format'
import type { ImportCustomersResult } from '@/lib/types'
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

// Fixed-column template (T59) — not the desktop's dynamic Excel column
// mapping. group_id/credit_limit are optional. branch_id is NOT a column —
// the importing user has no way to know a branch's GUID, so every row in
// the file is created under the single branch picked in this dialog.
const TEMPLATE_CSV = 'name,phone1,group_id,credit_limit\n'

function downloadTemplate() {
  const blob = new Blob(['﻿' + TEMPLATE_CSV], { type: 'text/csv;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = 'customers-template.csv'
  a.click()
  URL.revokeObjectURL(url)
}

/**
 * Import dialog («استيراد» on the Customers page). Reuses the create path
 * row-by-row on the gateway (T59) — one bad row never aborts the batch, so a
 * partial failure still creates every valid row and reports the rest here.
 */
export function ImportCustomersDialog({
  tenantId,
  open,
  onOpenChange,
}: {
  tenantId: string
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const importMutation = useImportCustomers(tenantId)
  const { data: bundle } = useBundle(tenantId)
  const branches = (bundle?.Branches ?? []).filter((b) => b.Status === 'active')
  const [file, setFile] = useState<File | null>(null)
  const [branchId, setBranchId] = useState('')
  const [result, setResult] = useState<ImportCustomersResult | null>(null)

  const reset = () => {
    setFile(null)
    setBranchId('')
    setResult(null)
  }

  const submit = async () => {
    if (!file || !branchId) return
    try {
      const res = await importMutation.mutateAsync({ file, branchId })
      setResult(res)
      if (res.errors.length === 0) {
        toast.success(`تم استيراد ${toArabicDigits(res.created)} عميل`)
      } else {
        toast.error(
          `تم استيراد ${toArabicDigits(res.created)} عميل، مع ${toArabicDigits(res.errors.length)} صف فشل — التفاصيل أدناه`,
        )
      }
    } catch (err) {
      toast.error(errorMessage(err))
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) reset()
        onOpenChange(next)
      }}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle>استيراد عملاء</DialogTitle>
        </DialogHeader>

        <div className="space-y-4">
          <div>
            <button
              type="button"
              onClick={downloadTemplate}
              className="text-sm text-primary hover:underline"
            >
              تنزيل قالب CSV
            </button>
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="import-customers-file">ملف CSV</Label>
            <Input
              id="import-customers-file"
              type="file"
              accept=".csv,text/csv"
              onChange={(e) => setFile(e.target.files?.[0] ?? null)}
            />
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="import-customers-branch">
              الفرع<span className="text-danger"> *</span>
            </Label>
            <select
              id="import-customers-branch"
              className={selectClass}
              value={branchId}
              onChange={(e) => setBranchId(e.target.value)}
            >
              <option value="">اختر الفرع</option>
              {branches.map((b) => (
                <option key={b.ID} value={b.ID}>
                  {b.Name}
                </option>
              ))}
            </select>
            <p className="text-xs text-muted-foreground">
              سيتم إنشاء جميع عملاء هذا الملف تحت الفرع المحدد هنا.
            </p>
          </div>

          {result && (
            <div className="rounded-lg border border-border">
              <div className="flex items-center justify-between border-b border-border px-3 py-2 text-sm">
                <span>تم إنشاء {toArabicDigits(result.created)} عميل</span>
                {result.errors.length > 0 && (
                  <span className="text-danger">{toArabicDigits(result.errors.length)} صف فشل</span>
                )}
              </div>
              {result.errors.length > 0 && (
                <div className="max-h-48 overflow-y-auto">
                  <table className="w-full text-start text-xs">
                    <thead>
                      <tr className="text-muted-foreground">
                        <th className="px-3 py-1.5 text-start font-medium">الصف</th>
                        <th className="px-3 py-1.5 text-start font-medium">الخطأ</th>
                      </tr>
                    </thead>
                    <tbody>
                      {result.errors.map((e) => (
                        <tr key={e.row} className="border-t border-border">
                          <td className="dir-ltr px-3 py-1.5 text-start font-mono">{e.row}</td>
                          <td className="px-3 py-1.5 text-danger">{e.message}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </div>
          )}
        </div>

        <DialogFooter>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
            إغلاق
          </Button>
          <Button
            type="button"
            disabled={!file || !branchId || importMutation.isPending}
            onClick={() => void submit()}
          >
            استيراد
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
