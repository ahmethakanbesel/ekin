import { useState } from "react"
import { useQuery } from "@tanstack/react-query"
import {
  Link,
  Outlet,
  useMatchRoute,
  useMatches,
  useParams,
} from "@tanstack/react-router"
import {
  InboxIcon,
  LibraryIcon,
  ListChecksIcon,
  PaletteIcon,
  SettingsIcon,
  TrendingUpIcon,
  UsersIcon,
  WorkflowIcon,
} from "lucide-react"

import { isApiError } from "@/api/client"
import { AppBreadcrumb } from "@/components/app-breadcrumb"
import { BrandMark } from "@/components/brand"
import { EmptyState, ErrorState } from "@/components/page"
import { ThemeToggle } from "@/components/theme-toggle"
import { Button } from "@/components/ui/button"
import { Separator } from "@/components/ui/separator"
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarInset,
  SidebarMenu,
  SidebarMenuBadge,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
  SidebarTrigger,
} from "@/components/ui/sidebar"
import { Skeleton } from "@/components/ui/skeleton"
import { UserMenu } from "@/features/auth/user-menu"
import { NotificationBell } from "@/features/notifications/notification-bell"
import { TAB_STATUSES } from "@/features/stories/model"
import { TemplateDraftWatcher } from "@/features/templates/draft-watch"
import { m } from "@/i18n"
import { cn } from "@/lib/utils"
import { STORY_LIST_LIMIT, storiesQuery } from "@/queries/stories"
import { workspaceQuery } from "@/queries/workspaces"
import { WorkspaceContext } from "./context"
import { WorkspaceSwitcher } from "./workspace-switcher"

const navItems = [
  { to: "/w/$workspaceId/stories", label: m.nav.review, icon: InboxIcon },
  {
    to: "/w/$workspaceId/pipelines",
    label: m.nav.pipelines,
    icon: WorkflowIcon,
  },
  {
    to: "/w/$workspaceId/knowledge",
    label: m.nav.knowledge,
    icon: LibraryIcon,
  },
  { to: "/w/$workspaceId/seo", label: m.nav.seo, icon: TrendingUpIcon },
  {
    to: "/w/$workspaceId/templates",
    label: m.nav.templates,
    icon: PaletteIcon,
  },
  { to: "/w/$workspaceId/jobs", label: m.nav.jobs, icon: ListChecksIcon },
  { to: "/w/$workspaceId/members", label: m.nav.members, icon: UsersIcon },
  { to: "/w/$workspaceId/settings", label: m.nav.settings, icon: SettingsIcon },
] as const

/** How often the nav refreshes the number of stories waiting for review. */
const REVIEW_COUNT_MS = 30_000

const STORY_ROUTE = "/w/$workspaceId/stories/$storyId"

/**
 * Whether the sidebar is expanded. A story page wants the width, so the
 * sidebar collapses to icons on entering one (it can still be opened) and
 * goes back to the reader's own choice on leaving.
 */
function useSidebarOpen(onStory: boolean) {
  const [preferred, setPreferred] = useState(true)
  const [open, setOpen] = useState(!onStory)
  const [wasOnStory, setWasOnStory] = useState(onStory)
  if (onStory !== wasOnStory) {
    setWasOnStory(onStory)
    setOpen(onStory ? false : preferred)
  }
  const toggle = (next: boolean) => {
    setOpen(next)
    if (!onStory) setPreferred(next)
  }
  return { open, toggle }
}

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
  const onStory = useMatches({
    select: (ms) => ms.some((r) => r.routeId === STORY_ROUTE),
  })
  const { open, toggle } = useSidebarOpen(onStory)
  const matchRoute = useMatchRoute()

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
    const missing =
      isApiError(ws.error) &&
      (ws.error.status === 404 || ws.error.status === 400)
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
  const badge =
    reviewCount > 0
      ? reviewCount >= STORY_LIST_LIMIT
        ? `${STORY_LIST_LIMIT}+`
        : String(reviewCount)
      : null
  const params = { workspaceId }

  return (
    <WorkspaceContext.Provider value={workspace}>
      <TemplateDraftWatcher workspaceId={workspaceId} />
      <SidebarProvider open={open} onOpenChange={toggle}>
        <a
          href="#main"
          className="sr-only z-50 rounded-md bg-background px-3 py-2 text-sm font-medium focus:not-sr-only focus:fixed focus:top-2 focus:left-2 focus:ring-[3px] focus:ring-ring focus:outline-none"
        >
          {m.nav.skipToContent}
        </a>
        <Sidebar collapsible="icon" aria-label={m.nav.workspace}>
          <SidebarHeader>
            <SidebarMenu>
              <SidebarMenuItem>
                <SidebarMenuButton
                  size="lg"
                  render={<Link to="/" />}
                  tooltip={m.app.name}
                >
                  <BrandMark />
                  <span className="grid flex-1 text-left leading-tight">
                    <span className="truncate font-heading font-semibold">
                      {m.app.name}
                    </span>
                    <span className="truncate text-xs text-muted-foreground">
                      {m.app.tagline}
                    </span>
                  </span>
                </SidebarMenuButton>
              </SidebarMenuItem>
              <SidebarMenuItem>
                <WorkspaceSwitcher current={workspace} />
              </SidebarMenuItem>
            </SidebarMenu>
          </SidebarHeader>
          <SidebarContent className="scroll-area">
            <SidebarGroup>
              <SidebarGroupLabel>{m.nav.workspace}</SidebarGroupLabel>
              <SidebarMenu>
                {navItems.map((item) => (
                  <SidebarMenuItem key={item.to}>
                    <SidebarMenuButton
                      render={<Link to={item.to} params={params} />}
                      isActive={!!matchRoute({ to: item.to, params, fuzzy: true })}
                      tooltip={item.label}
                      className="data-active:[&>svg]:text-primary-ink"
                    >
                      <item.icon />
                      <span>{item.label}</span>
                    </SidebarMenuButton>
                    {item.to === "/w/$workspaceId/stories" && badge ? (
                      <SidebarMenuBadge
                        className="text-primary-ink peer-data-active/menu-button:text-primary-ink peer-hover/menu-button:text-primary-ink"
                        aria-label={m.nav.reviewCount(reviewCount)}
                      >
                        {badge}
                      </SidebarMenuBadge>
                    ) : null}
                  </SidebarMenuItem>
                ))}
              </SidebarMenu>
            </SidebarGroup>
          </SidebarContent>
          <SidebarFooter>
            <SidebarMenu>
              <SidebarMenuItem>
                <UserMenu />
              </SidebarMenuItem>
            </SidebarMenu>
          </SidebarFooter>
        </Sidebar>
        <SidebarInset className="min-w-0">
          <header className="sticky top-0 z-20 border-b bg-background">
            <div className="flex h-12 items-center gap-2 px-4">
              <div className="flex min-w-0 items-center gap-1 md:hidden">
                <Link
                  to="/"
                  className="shrink-0 rounded-lg outline-none focus-visible:ring-[3px] focus-visible:ring-ring"
                  aria-label={m.app.name}
                >
                  <BrandMark />
                </Link>
                <WorkspaceSwitcher current={workspace} compact />
              </div>
              <SidebarTrigger
                className="-ml-1 hidden md:inline-flex"
                aria-label={m.nav.toggleSidebar}
              />
              <Separator
                orientation="vertical"
                className="mr-2 hidden data-[orientation=vertical]:h-4 data-[orientation=vertical]:self-center md:block"
              />
              <AppBreadcrumb workspace={workspace} className="hidden md:block" />
              <div className="ml-auto flex shrink-0 items-center gap-1">
                {/* Keyed: its "last opened" mark belongs to one workspace. */}
                <NotificationBell
                  key={workspaceId}
                  workspaceId={workspaceId}
                  timezone={workspace.timezone}
                />
                <ThemeToggle />
                <div className="ml-1 md:hidden">
                  <UserMenu compact />
                </div>
              </div>
            </div>
            <nav
              aria-label={m.nav.sections}
              className="scroll-area flex gap-1 overflow-x-auto px-3 pb-2 md:hidden"
            >
              {navItems.map((item) => (
                <Link
                  key={item.to}
                  to={item.to}
                  params={params}
                  className="flex h-8 shrink-0 items-center gap-1.5 rounded-md px-2.5 text-sm font-medium text-muted-foreground outline-none hover:bg-muted hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring data-[status=active]:bg-secondary data-[status=active]:text-foreground [&_svg]:size-4"
                >
                  <item.icon />
                  {item.label}
                  {item.to === "/w/$workspaceId/stories" && badge ? (
                    <span
                      className="tabular min-w-5 rounded-full bg-primary px-1.5 text-center text-[11px] leading-5 font-semibold text-primary-foreground inset-ring inset-ring-primary-ink"
                      aria-label={m.nav.reviewCount(reviewCount)}
                    >
                      {badge}
                    </span>
                  ) : null}
                </Link>
              ))}
            </nav>
          </header>
          <main
            id="main"
            className={cn(
              "w-full min-w-0 flex-1",
              // The story page lays out its own feed and panel edge to edge.
              !onStory && "mx-auto max-w-7xl p-4 md:p-8"
            )}
          >
            <Outlet />
          </main>
        </SidebarInset>
      </SidebarProvider>
    </WorkspaceContext.Provider>
  )
}
