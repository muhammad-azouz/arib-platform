import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import {
  Activity,
  BadgeCheck,
  HardDrive,
  PauseCircle,
  Sparkles,
  Users,
} from 'lucide-react'
import { adminApi } from '@/lib/api'
import { qk } from '@/lib/query'
import { actionLabel, relative } from '@/lib/format'
import { PageHeader } from '@/components/PageHeader'
import { StatCard } from '@/components/StatCard'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'

export function Overview() {
  const stats = useQuery({ queryKey: qk.stats, queryFn: adminApi.stats })
  const audit = useQuery({ queryKey: qk.audit, queryFn: adminApi.audit })

  const s = stats.data
  const cards = [
    { label: 'Clients', value: s?.clients, icon: Users, accent: true },
    { label: 'Active licenses', value: s?.licenses_active, icon: BadgeCheck },
    { label: 'Bound devices', value: s?.devices_active, icon: HardDrive },
    { label: 'Suspended', value: s?.licenses_suspended, icon: PauseCircle },
    { label: 'Trials', value: s?.licenses_trial, icon: Sparkles },
    {
      label: 'Expiring · 30d',
      value: s?.licenses_expiring_30d,
      icon: Activity,
    },
  ]

  return (
    <div>
      <PageHeader
        title="Overview"
        description="Live snapshot of accounts, licenses, and device bindings."
      />

      <div className="grid grid-cols-2 gap-3 lg:grid-cols-3">
        {stats.isLoading
          ? Array.from({ length: 6 }).map((_, i) => (
              <Skeleton key={i} className="h-[116px]" />
            ))
          : cards.map((c, i) => (
              <StatCard
                key={c.label}
                label={c.label}
                value={c.value ?? 0}
                icon={c.icon}
                accent={c.accent}
                style={{ animationDelay: `${i * 45}ms` }}
              />
            ))}
      </div>

      <Card className="mt-6">
        <CardHeader className="flex-row items-center justify-between">
          <CardTitle>Recent activity</CardTitle>
          <Link
            to="/audit"
            className="text-xs text-muted-foreground hover:text-foreground"
          >
            View all →
          </Link>
        </CardHeader>
        <CardContent>
          {audit.isLoading ? (
            <div className="grid gap-2">
              {Array.from({ length: 5 }).map((_, i) => (
                <Skeleton key={i} className="h-9" />
              ))}
            </div>
          ) : audit.data && audit.data.length > 0 ? (
            <ul className="divide-y divide-border/70">
              {audit.data.slice(0, 8).map((entry) => (
                <li
                  key={entry.ID}
                  className="flex items-center justify-between gap-4 py-2.5 text-sm"
                >
                  <div className="flex min-w-0 items-center gap-3">
                    <span className="size-1.5 shrink-0 rounded-full bg-primary/70" />
                    <span className="truncate">
                      <span className="font-medium">
                        {actionLabel(entry.Action)}
                      </span>{' '}
                      <span className="text-muted-foreground">
                        by {entry.Actor}
                      </span>
                    </span>
                  </div>
                  <span className="shrink-0 text-xs text-muted-foreground">
                    {relative(entry.CreatedAt)}
                  </span>
                </li>
              ))}
            </ul>
          ) : (
            <p className="py-6 text-center text-sm text-muted-foreground">
              No activity yet.
            </p>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
