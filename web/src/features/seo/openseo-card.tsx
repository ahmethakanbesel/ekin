import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { ArrowUpRightIcon } from "lucide-react"

import { ErrorState } from "@/components/page"
import { Badge } from "@/components/ui/badge"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { useWorkspace } from "@/features/workspaces/context"
import { m } from "@/i18n"
import { formatInteger } from "@/lib/format"
import { meQuery } from "@/queries/auth"
import { openSeoStatusQuery } from "@/queries/seo"

/**
 * Whether refreshes call OpenSEO directly (an API key is set in the
 * instance credentials) or go through the worker's Claude Code plugin.
 * Owners only: the status call needs the manage role.
 */
export function OpenSeoCard() {
  const workspace = useWorkspace()
  const status = useQuery(openSeoStatusQuery(workspace.id))
  const me = useQuery(meQuery())
  const isAdmin = !!me.data?.isAdmin

  const credentialsLink = isAdmin ? (
    <Link
      to="/system"
      hash="credentials"
      className="inline-flex items-center gap-1 font-medium text-foreground underline-offset-4 hover:underline"
    >
      {m.seo.openseoCredentials}
      <ArrowUpRightIcon className="size-3.5" />
    </Link>
  ) : null

  let badge = null
  let body
  if (status.isPending) body = <Skeleton className="h-16 w-full rounded-lg" />
  else if (status.isError)
    body = <ErrorState error={status.error} onRetry={() => void status.refetch()} />
  else if (!status.data.configured) {
    badge = <Badge variant="outline">{m.seo.openseoNotConnected}</Badge>
    body = (
      <div className="grid gap-2 text-sm text-muted-foreground">
        <p>{m.seo.openseoPlugin}</p>
        <p>
          {m.seo.openseoSetUp} {isAdmin ? null : m.seo.openseoAskAdmin}
        </p>
        {credentialsLink ? <p>{credentialsLink}</p> : null}
      </div>
    )
  } else if (status.data.error) {
    badge = <Badge variant="destructive">{m.seo.openseoFailed}</Badge>
    body = (
      <div className="grid gap-2 text-sm">
        <p className="rounded-lg border border-destructive/30 bg-destructive/5 px-3 py-2 font-mono text-xs break-words">
          {status.data.error}
        </p>
        <p className="text-muted-foreground">
          {m.seo.openseoErrorHint} {credentialsLink}
        </p>
      </div>
    )
  } else {
    const s = status.data
    badge = (
      <Badge className="bg-emerald-500/15 text-emerald-800 dark:text-emerald-300">
        {m.seo.openseoConnected}
      </Badge>
    )
    body = (
      <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5 text-sm">
        {s.email ? (
          <>
            <dt className="text-muted-foreground">{m.seo.openseoAccount}</dt>
            <dd className="min-w-0 truncate font-medium" title={s.email}>
              {s.email}
            </dd>
          </>
        ) : null}
        {s.creditsRemaining !== undefined ? (
          <>
            <dt className="text-muted-foreground">{m.seo.openseoCredits}</dt>
            <dd className="tabular font-medium">{formatInteger(s.creditsRemaining)}</dd>
          </>
        ) : null}
        <dt className="text-muted-foreground">{m.seo.openseoProjects}</dt>
        <dd className="tabular">{s.projects.length}</dd>
      </dl>
    )
  }

  return (
    <Card>
      <CardHeader>
        <div className="flex flex-wrap items-center justify-between gap-2">
          <CardTitle>{m.seo.openseoTitle}</CardTitle>
          {badge}
        </div>
        <CardDescription>{m.seo.openseoDescription}</CardDescription>
      </CardHeader>
      <CardContent>{body}</CardContent>
    </Card>
  )
}
