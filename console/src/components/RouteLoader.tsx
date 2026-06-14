/** Full-viewport loading indicator for route-level transitions (gate, boot). */
export function RouteLoader({ label = 'جارٍ التحميل…' }: { label?: string }) {
  return (
    <div className="grid min-h-screen place-items-center">
      <div className="flex items-center gap-3 text-sm text-muted-foreground">
        <span className="size-2 animate-ping rounded-full bg-primary" />
        {label}
      </div>
    </div>
  )
}
