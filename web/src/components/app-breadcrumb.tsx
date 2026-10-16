import { Fragment } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link, useMatches } from "@tanstack/react-router"

import type { Workspace } from "@/api/client"
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "@/components/ui/breadcrumb"
import { m } from "@/i18n"
import { cn } from "@/lib/utils"
import { storyQuery } from "@/queries/stories"

const sections = {
  stories: { to: "/w/$workspaceId/stories", label: m.nav.review },
  pipelines: { to: "/w/$workspaceId/pipelines", label: m.nav.pipelines },
  knowledge: { to: "/w/$workspaceId/knowledge", label: m.nav.knowledge },
  seo: { to: "/w/$workspaceId/seo", label: m.nav.seo },
  templates: { to: "/w/$workspaceId/templates", label: m.nav.templates },
  jobs: { to: "/w/$workspaceId/jobs", label: m.nav.jobs },
  members: { to: "/w/$workspaceId/members", label: m.nav.members },
  settings: { to: "/w/$workspaceId/settings", label: m.nav.settings },
} as const

const STORY_ROUTE = "/w/$workspaceId/stories/$storyId"
const NEW_STORY_ROUTE = "/w/$workspaceId/stories/new"

const linkClass =
  "rounded-sm outline-none focus-visible:ring-[3px] focus-visible:ring-ring"

/** Workspace › section › story title, from the current route. */
export function AppBreadcrumb({
  workspace,
  className,
}: {
  workspace: Workspace
  className?: string
}) {
  const leaf = useMatches({
    select: (ms) => {
      const last = ms.at(-1)
      return {
        routeId: last?.routeId as string | undefined,
        pathname: last?.pathname ?? "",
        storyId: (last?.params as { storyId?: string } | undefined)?.storyId,
      }
    },
  })
  const onStory = leaf.routeId === STORY_ROUTE && leaf.storyId !== undefined
  // The story page fetches the story; this only reads what it cached.
  const story = useQuery({
    ...storyQuery(workspace.id, leaf.storyId ?? ""),
    enabled: false,
  })

  const part = leaf.pathname.split("/")[3]
  const section =
    part !== undefined && Object.hasOwn(sections, part)
      ? sections[part as keyof typeof sections]
      : undefined
  let detail: string | undefined
  if (onStory) detail = story.data?.title
  else if (leaf.routeId === NEW_STORY_ROUTE) detail = m.stories.newRequest
  // A deeper page of a section (a pipeline, a document) links back to it.
  const atSectionRoot =
    section !== undefined && detail === undefined && leaf.pathname.split("/").length <= 4
  const params = { workspaceId: workspace.id }

  const crumbs = [
    <BreadcrumbLink
      key="ws"
      className={linkClass}
      render={<Link to="/w/$workspaceId/stories" params={params} />}
    >
      {workspace.name}
    </BreadcrumbLink>,
  ]
  if (section)
    crumbs.push(
      atSectionRoot ? (
        <BreadcrumbPage key="section">{section.label}</BreadcrumbPage>
      ) : (
        <BreadcrumbLink
          key="section"
          className={linkClass}
          render={<Link to={section.to} params={params} />}
        >
          {section.label}
        </BreadcrumbLink>
      )
    )
  if (detail)
    crumbs.push(
      <BreadcrumbPage key="detail" className="truncate">
        {detail}
      </BreadcrumbPage>
    )

  return (
    <Breadcrumb aria-label={m.nav.breadcrumb} className={cn("min-w-0", className)}>
      <BreadcrumbList className="flex-nowrap">
        {crumbs.map((c, i) => (
          <Fragment key={c.key}>
            {i > 0 ? <BreadcrumbSeparator /> : null}
            <BreadcrumbItem className={i === crumbs.length - 1 ? "min-w-0" : "shrink-0"}>
              {c}
            </BreadcrumbItem>
          </Fragment>
        ))}
      </BreadcrumbList>
    </Breadcrumb>
  )
}
