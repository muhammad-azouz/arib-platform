import { BranchForm } from '@/components/BranchForm'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'

/** Dialog wrapper around the create-branch form for the steady-state page. */
export function AddBranchDialog({
  tenantId,
  companyId,
  open,
  onOpenChange,
}: {
  tenantId: string
  companyId: string
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>فرع جديد</DialogTitle>
          <DialogDescription>
            أضف فرعًا جديدًا لنشاطك وحدّد عدد المقاعد المسموح بها.
          </DialogDescription>
        </DialogHeader>
        <BranchForm
          tenantId={tenantId}
          companyId={companyId}
          submitLabel="إنشاء الفرع"
          onSaved={() => onOpenChange(false)}
        />
      </DialogContent>
    </Dialog>
  )
}
