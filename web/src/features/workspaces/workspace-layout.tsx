import { useQuery } from "@tanstack/react-query"
import { Link, Outlet, useParams } from "@tanstack/react-router"
import {
  InboxIcon,
  LibraryIcon,
  ListChecksIcon,
  SettingsIcon,
  TrendingUpIcon,
  UsersIcon,
  WorkflowIcon,
} from "lucide-react"

import { isApiError } from "@/api/client"
import { Brand } from "@/components/brand"
import { EmptyState, ErrorState } from "@/components/page"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { UserMenu } from "@/features/auth/user-menu"
import { NotificationBell } from "@/features/notifications/notification-bell"
import { TAB_STATUSES } from "@/features/stories/model"
import { m } from "@/i18n"
import { STORY_LIST_LIMIT, storiesQuery } from "@/queries/stories"
import { workspaceQuery } from "@/queries/workspaces"
import { WorkspaceContext } from "./context"
import { WorkspaceSwitcher } from "./workspace-switcher"

const navItems = [
  { to: "/w/$workspaceId/stories", label: m.nav.review, icon: InboxIcon },
  { to: "/w/$workspaceId/pipelines", label: m.nav.pipelines, icon: WorkflowIcon },
  { to: "/w/$workspaceId/knowledge", label: m.nav.knowledge, icon: LibraryIcon },
  { to: "/w/$workspaceId/seo", label: m.nav.seo, icon: TrendingUpIcon },
  { to: "/w/$workspaceId/jobs", label: m.nav.jobs, icon: ListChecksIcon },
  { to: "/w/$workspaceId/members", label: m.nav.members, icon: UsersIcon },
  { to: "/w/$workspaceId/settings", label: m.nav.settings, icon: SettingsIcon },
] as const

/** How often the nav refreshes the number of stories waiting for review. */
const REVIEW_COUNT_MS = 30_000

export function WorkspaceLayout() {
  const { workspaceId } = useParams({ from: "/w/$workspaceId" })
  const ws = useQuery(workspaceQuery(workspaceId))
  // Shares its cache with the "Needs review" tab.
  const review = useQuery({
    ...storiesQuery(workspaceId, TAB_STATUSES.review),
    enabled: ws.isSuccess,
    refetchInterval: REVIEW_COUNT_MS,
    refetchIntervalInBackground: true,
  })
  const reviewCount = review.data?.length ?? 0

  if (ws.isPending) {
    return (
      <div className="flex min-h-svh">
        <aside className="hidden w-64 shrink-0 border-r bg-sidebar p-3 md:block">
          <Skeleton className="h-12 w-full" />
        </aside>
        <main className="flex-1 p-8">
          <Skeleton className="h-8 w-48" />
        </main>
      </div>
    )
  }

  if (ws.isError) {
    // A workspace you are not a member of answers 404, like a missing one.
    const missing = isApiError(ws.error) && (ws.error.status === 404 || ws.error.status === 400)
    return (
      <main className="mx-auto max-w-xl p-8">
        {missing ? (
          <EmptyState
            title={m.workspace.notFound}
            action={
              <Button render={<Link to="/" />} nativeButton={false}>
                {m.errors.goHome}
              </Button>
            }
          />
        ) : (
          <ErrorState error={ws.error} onRetry={() => void ws.refetch()} />
        )}
      </main>
    )
  }

  const workspace = ws.data
  // Keyed: its "last opened" mark belongs to one workspace.
  const bell = (
    <NotificationBell key={workspaceId} workspaceId={workspaceId} timezone={workspace.timezone} />
  )
  const nav = navItems.map((item) => (
    <Link
      key={item.to}
      to={item.to}
      params={{ workspaceId }}
      className="flex items-center gap-2.5 rounded-2xl px-3 py-2 text-sm font-medium text-sidebar-foreground/80 outline-none hover:bg-sidebar-accent hover:text-sidebar-accent-foreground focus-visible:ring-3 focus-visible:ring-ring/30 data-[status=active]:bg-sidebar-accent data-[status=active]:text-sidebar-accent-foreground [&_svg]:size-4"
    >
      <item.icon />
      {item.label}
      {item.to === "/w/$workspaceId/stories" && reviewCount > 0 ? (
        <span
          className="ml-auto min-w-5 rounded-full bg-amber-500/20 px-1.5 text-center text-[11px] leading-5 font-semibold text-amber-800 tabular dark:text-amber-300"
          aria-label={m.nav.reviewCount(reviewCount)}
        >
          {reviewCount >= STORY_LIST_LIMIT ? `${STORY_LIST_LIMIT}+` : reviewCount}
        </span>
      ) : null}
    </Link>
  ))

  return (
    <WorkspaceContext.Provider value={workspace}>
      <div className="flex min-h-svh">
        <aside className="sticky top-0 hidden h-svh w-64 shrink-0 flex-col gap-4 border-r bg-sidebar p-3 md:flex">
          <div className="flex items-center justify-between gap-2">
            <Brand />
            {bell}
          </div>
          <WorkspaceSwitcher current={workspace} />
          <nav className="grid gap-0.5">{nav}</nav>
          <div className="mt-auto">
            <UserMenu />
          </div>
        </aside>
        <div className="flex min-w-0 flex-1 flex-col">
          <header className="flex flex-col gap-2 border-b bg-sidebar p-3 md:hidden">
            <div className="flex items-center gap-2">
              <Brand />
              <div className="ml-auto max-w-56 min-w-0 flex-1">
                <WorkspaceSwitcher current={workspace} />
              </div>
              {bell}
              <UserMenu compact />
            </div>
            <nav className="flex gap-1 overflow-x-auto">{nav}</nav>
          </header>
          <main className="mx-auto w-full max-w-7xl flex-1 p-4 md:p-8">
            <Outlet />
          </main>
        </div>
      </div>
    </WorkspaceContext.Provider>
  )
}
