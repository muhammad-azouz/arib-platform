import { toArabicDigits } from '@/lib/format'
import { ArrowLeading, ArrowTrailing } from '@/components/icon'
import { Button } from '@/components/ui/button'

/** Page controls for a server-paged table, shared across the console. */
export function Pagination({
  page,
  pageSize,
  total,
  itemLabel = 'صنف',
  onPageChange,
}: {
  page: number
  pageSize: number
  total: number
  itemLabel?: string
  onPageChange: (page: number) => void
}) {
  const totalPages = Math.max(1, Math.ceil(total / pageSize))
  return (
    <div className="mt-3 flex items-center justify-between text-sm text-muted-foreground">
      <span>
        صفحة {toArabicDigits(page)} من {toArabicDigits(totalPages)} ·{' '}
        {toArabicDigits(total)} {itemLabel}
      </span>
      <div className="flex items-center gap-2">
        <Button
          variant="outline"
          size="sm"
          disabled={page <= 1}
          onClick={() => onPageChange(page - 1)}
        >
          <ArrowTrailing className="size-4" />
          السابق
        </Button>
        <Button
          variant="outline"
          size="sm"
          disabled={page >= totalPages}
          onClick={() => onPageChange(page + 1)}
        >
          التالي
          <ArrowLeading className="size-4" />
        </Button>
      </div>
    </div>
  )
}
