import { useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { Navigate } from "@tanstack/react-router"
import { LayersIcon, PlusIcon } from "lucide-react"

import { EmptyState, ErrorState } from "@/components/page"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { m } from "@/i18n"
import { workspacesQuery } from "@/queries/workspaces"
import { lastWorkspace } from "./context"
import { CreateWorkspaceDialog } from "./create-workspace-dialog"

/** "/": opens the last-used (or first) workspace, or offers to create one. */
export function HomePage() {
  const { data, error, isPending, refetch } = useQuery(workspacesQuery())
  const [creating, setCreating] = useState(false)

  if (isPending) {
    return (
      <main className="mx-auto max-w-xl p-8">
        <Skeleton className="h-40 w-full" />
      </main>
    )
  }
  if (error) {
    return (
      <main className="mx-auto max-w-xl p-8">
        <ErrorState error={error} onRetry={() => void refetch()} />
      </main>
    )
  }
  if (data.length > 0) {
    const last = lastWorkspace()
    const target = data.find((w) => w.id === last) ?? data[0]
    return (
      <Navigate to="/w/$workspaceId/pipelines" params={{ workspaceId: target.id }} replace />
    )
  }

  return (
    <main className="flex min-h-svh items-center justify-center p-4">
      <div className="w-full max-w-lg space-y-6">
        <p className="text-center font-heading text-xl font-semibold tracking-tight">
          {m.app.name}
        </p>
        <EmptyState
          icon={<LayersIcon />}
          title={m.workspace.emptyTitle}
          body={m.workspace.emptyBody}
          action={
            <Button onClick={() => setCreating(true)}>
              <PlusIcon data-icon="inline-start" />
              {m.nav.newWorkspace}
            </Button>
          }
        />
      </div>
      <CreateWorkspaceDialog open={creating} onOpenChange={setCreating} />
    </main>
  )
}
