import { useState } from 'react'
import { Link, Navigate, useNavigate } from 'react-router-dom'
import { useTenants } from '@/lib/hooks'
import type { Tenant } from '@/lib/types'
import { tenantStatusLabel, tenantStatusTone, toArabicDigits } from '@/lib/format'
import { TopBar } from '@/components/TopBar'
import { RouteLoader } from '@/components/RouteLoader'
import { EmptyState, ErrorState } from '@/components/States'
import { TenantIcon, AddIcon, ArrowLeading } from '@/components/icon'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { CreateTenantDialog } from '@/components/CreateTenantDialog'

/**
 * Tenant resolver. An account usually owns exactly one tenant, so we don't make
 * them pick: zero → create, one → straight in, many → a chooser. The setup gate
 * downstream decides whether each tenant lands on its overview or the wizard.
 */
export function Tenants() {
  const navigate = useNavigate()
  const { data, isLoading, isError, error, refetch } = useTenants()
  const [createOpen, setCreateOpen] = useState(false)

  const onCreated = (tenant: Tenant) => {
    setCreateOpen(false)
    navigate(`/tenants/${tenant.ID}/setup`)
  }

  return (
    <div className="min-h-screen">
      <TopBar subtitle="أنشطتي" />

      <main className="mx-auto w-full max-w-3xl px-5 py-10 sm:py-14">
        <div className="animate-rise">
          {isLoading ? (
            <RouteLoader label="جارٍ تحميل أنشطتك…" />
          ) : isError ? (
            <ErrorState
              message={error instanceof Error ? error.message : undefined}
              onRetry={() => void refetch()}
            />
          ) : !data || data.length === 0 ? (
            <EmptyState
              icon={TenantIcon}
              title="لا توجد أنشطة بعد"
              description="ابدأ بإنشاء نشاطك التجاري الأول لتجهيز شركتك وفروعك."
              action={
                <Button onClick={() => setCreateOpen(true)}>
                  <AddIcon className="size-4" />
                  إنشاء نشاط
                </Button>
              }
            />
          ) : data.length === 1 ? (
            <Navigate to={`/tenants/${data[0].ID}`} replace />
          ) : (
            <>
              <div className="mb-6 flex items-center justify-between">
                <div>
                  <h1 className="font-display text-2xl font-extrabold tracking-tight">
                    أنشطتي
                  </h1>
                  <p className="mt-1 text-sm text-muted-foreground">
                    اختر نشاطًا لإدارته أو أنشئ نشاطًا جديدًا.
                  </p>
                </div>
                <Button onClick={() => setCreateOpen(true)}>
                  <AddIcon className="size-4" />
                  نشاط جديد
                </Button>
              </div>

              <div className="grid gap-3 sm:grid-cols-2">
                {data.map((t) => (
                  <Link key={t.ID} to={`/tenants/${t.ID}`} className="group">
                    <Card className="flex items-center gap-3 p-4 transition-colors group-hover:border-primary/40">
                      <div className="grid size-11 shrink-0 place-items-center rounded-xl bg-accent text-primary">
                        <TenantIcon className="size-6" />
                      </div>
                      <div className="min-w-0 flex-1">
                        <div className="truncate font-display font-bold">{t.Name}</div>
                        <div className="mt-0.5">
                          <Badge tone={tenantStatusTone(t.Status)}>
                            {tenantStatusLabel(t.Status)}
                          </Badge>
                        </div>
                      </div>
                      <ArrowLeading className="size-5 text-muted-foreground transition-colors group-hover:text-primary" />
                    </Card>
                  </Link>
                ))}
              </div>

              <p className="mt-4 text-xs text-muted-foreground">
                {toArabicDigits(data.length)} نشاط
              </p>
            </>
          )}
        </div>
      </main>

      <CreateTenantDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        onCreated={onCreated}
      />
    </div>
  )
}
