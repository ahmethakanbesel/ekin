import { useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { ChevronRightIcon, LayersIcon, PlusIcon } from "lucide-react"

import { EmptyState, ErrorState, PageHeader } from "@/components/page"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { StandalonePage } from "@/features/auth/standalone-page"
import { m } from "@/i18n"
import { workspacesQuery } from "@/queries/workspaces"
import { CreateWorkspaceDialog } from "./create-workspace-dialog"

/** "/": the caller's workspaces, or an offer to create the first one. */
export function HomePage() {
  const { data, error, isPending, refetch } = useQuery(workspacesQuery())
  const [creating, setCreating] = useState(false)

  const newButton = (
    <Button onClick={() => setCreating(true)}>
      <PlusIcon data-icon="inline-start" />
      {m.nav.newWorkspace}
    </Button>
  )

  let body
  if (isPending) {
    body = (
      <div className="grid gap-2" aria-busy="true" aria-label={m.common.loading}>
        <Skeleton className="h-16 w-full rounded-3xl" />
        <Skeleton className="h-16 w-full rounded-3xl" />
      </div>
    )
  } else if (error) {
    body = <ErrorState error={error} onRetry={() => void refetch()} />
  } else if (data.length === 0) {
    body = (
      <EmptyState
        icon={<LayersIcon />}
        title={m.workspace.emptyTitle}
        body={m.workspace.emptyBody}
        action={newButton}
      />
    )
  } else {
    body = (
      <ul className="grid gap-2">
        {data.map((w) => (
          <li key={w.id}>
            <Link
              to="/w/$workspaceId/pipelines"
              params={{ workspaceId: w.id }}
              aria-label={m.workspace.open(w.name)}
              className="flex items-center gap-3 rounded-3xl border bg-card px-4 py-3 outline-none hover:bg-muted/50 focus-visible:ring-3 focus-visible:ring-ring/30"
            >
              <span className="flex size-9 shrink-0 items-center justify-center rounded-xl bg-primary text-sm font-semibold text-primary-foreground uppercase">
                {[...w.name][0] ?? "?"}
              </span>
              <span className="grid min-w-0 flex-1 leading-tight">
                <span className="truncate font-medium">{w.name}</span>
                <span className="truncate text-xs text-muted-foreground">{w.timezone}</span>
              </span>
              <Badge variant={w.role === "owner" ? "default" : "secondary"}>
                {m.roles[w.role]}
              </Badge>
              <ChevronRightIcon className="size-4 shrink-0 text-muted-foreground" />
            </Link>
          </li>
        ))}
      </ul>
    )
  }

  return (
    <StandalonePage>
      <PageHeader
        title={m.workspace.listTitle}
        description={m.workspace.listDescription}
        actions={data?.length ? newButton : null}
      />
      {body}
      <CreateWorkspaceDialog open={creating} onOpenChange={setCreating} />
    </StandalonePage>
  )
}
