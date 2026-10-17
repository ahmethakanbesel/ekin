import { useCallback } from "react"
import { useQuery } from "@tanstack/react-query"
import { useNavigate, useSearch } from "@tanstack/react-router"

import { ErrorState } from "@/components/page"
import { Skeleton } from "@/components/ui/skeleton"
import { SettingsSectionHeader } from "@/features/settings/settings-page"
import { useCan, useWorkspace } from "@/features/workspaces/context"
import { m } from "@/i18n"
import { integrationsQuery } from "@/queries/integrations"
import { BlogCard } from "./blog-card"
import { YouTubeCard } from "./youtube-card"
import { useYouTubeReturn } from "./youtube-return"

const ROUTE = "/w/$workspaceId/settings/publishing"

/**
 * Settings › Publishing: the YouTube channel and the blog. Google's
 * consent screen comes back here with ?youtube=connected|error.
 */
export function PublishingSection() {
  const workspace = useWorkspace()
  const canManage = useCan("manage")
  const search = useSearch({ from: ROUTE })
  const navigate = useNavigate({ from: ROUTE })
  const clear = useCallback(() => void navigate({ search: {}, replace: true }), [navigate])
  useYouTubeReturn(search, clear, workspace.id)
  const query = useQuery(integrationsQuery(workspace.id))

  let body
  if (query.isPending)
    body = (
      <div className="grid gap-6" aria-busy="true" aria-label={m.common.loading}>
        <Skeleton className="h-40 w-full rounded-xl" />
        <Skeleton className="h-72 w-full rounded-xl" />
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
    <div className="max-w-3xl">
      <SettingsSectionHeader
        section="publishing"
        readOnly={canManage ? undefined : m.workspace.publishingReadOnly}
      />
      {body}
    </div>
  )
}
