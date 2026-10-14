import { useQuery } from "@tanstack/react-query"

import { ErrorState, ReadOnlyNote } from "@/components/page"
import { Skeleton } from "@/components/ui/skeleton"
import { useCan, useWorkspace } from "@/features/workspaces/context"
import { m } from "@/i18n"
import { integrationsQuery } from "@/queries/integrations"
import { BlogCard } from "./blog-card"
import { YouTubeCard } from "./youtube-card"

/** Workspace settings → Publishing: the YouTube channel and the blog. */
export function PublishingSection() {
  const workspace = useWorkspace()
  const canManage = useCan("manage")
  const query = useQuery(integrationsQuery(workspace.id))

  let body
  if (query.isPending)
    body = (
      <div className="grid gap-6" aria-busy="true" aria-label={m.common.loading}>
        <Skeleton className="h-40 w-full rounded-4xl" />
        <Skeleton className="h-72 w-full rounded-4xl" />
      </div>
    )
  else if (query.isError)
    body = <ErrorState error={query.error} onRetry={() => void query.refetch()} />
  else
    body = (
      <div className="grid gap-6">
        <YouTubeCard youtube={query.data.youtube} />
        {/* Re-mount the form when the stored target changes underneath it. */}
        <BlogCard key={JSON.stringify(query.data.blog ?? null)} blog={query.data.blog} />
      </div>
    )

  return (
    <section id="publishing" className="mt-10 scroll-mt-8">
      <div className="mb-4 space-y-1">
        <h2 className="font-heading text-lg font-semibold tracking-tight">
          {m.workspace.publishingTitle}
        </h2>
        <p className="text-sm text-muted-foreground">{m.workspace.publishingDescription}</p>
      </div>
      {canManage ? null : <ReadOnlyNote>{m.workspace.publishingReadOnly}</ReadOnlyNote>}
      {body}
    </section>
  )
}
