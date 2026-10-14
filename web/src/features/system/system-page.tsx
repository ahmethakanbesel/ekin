import { useQuery } from "@tanstack/react-query"
import { CircleCheckIcon, CircleXIcon } from "lucide-react"

import { ErrorState, PageHeader } from "@/components/page"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { StandalonePage } from "@/features/auth/standalone-page"
import { m } from "@/i18n"
import { meQuery } from "@/queries/auth"
import { systemQuery } from "@/queries/system"
import { HostProfiles } from "./host-profiles"

/** /system: what this server can do; for admins also the host profiles. */
export function SystemPage() {
  const me = useQuery(meQuery())
  const status = useQuery(systemQuery())

  let checks
  if (status.isPending)
    checks = (
      <div className="grid gap-3" aria-busy="true" aria-label={m.common.loading}>
        {Array.from({ length: 6 }, (_, i) => (
          <Skeleton key={i} className="h-10 w-full" />
        ))}
      </div>
    )
  else if (status.isError)
    checks = <ErrorState error={status.error} onRetry={() => void status.refetch()} />
  else
    checks = (
      <ul className="divide-y">
        {status.data.checks.map((c) => (
          <li key={c.name} className="flex gap-3 py-3 first:pt-0 last:pb-0">
            {c.ok ? (
              <CircleCheckIcon
                className="mt-0.5 size-5 shrink-0 text-emerald-600 dark:text-emerald-400"
                aria-label={m.system.ok}
              />
            ) : (
              <CircleXIcon
                className="mt-0.5 size-5 shrink-0 text-destructive"
                aria-label={m.system.notOk}
              />
            )}
            <div className="min-w-0 space-y-0.5">
              <p className="font-medium">{c.name}</p>
              {c.detail ? (
                <p className="font-mono text-xs break-words text-muted-foreground">{c.detail}</p>
              ) : null}
            </div>
          </li>
        ))}
      </ul>
    )

  return (
    <StandalonePage>
      <PageHeader
        title={m.system.title}
        description={
          status.data
            ? `${m.system.description} ${m.system.version(status.data.version)}.`
            : m.system.description
        }
      />
      <div className="grid gap-10">
        <Card>
          <CardHeader>
            <CardTitle>{m.system.checksTitle}</CardTitle>
            <CardDescription>{m.system.checksDescription}</CardDescription>
          </CardHeader>
          <CardContent>{checks}</CardContent>
        </Card>
        {me.data?.isAdmin ? <HostProfiles /> : null}
      </div>
    </StandalonePage>
  )
}
